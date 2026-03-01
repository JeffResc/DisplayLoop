package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/JeffResc/DisplayLoop/internal/db"
	"github.com/JeffResc/DisplayLoop/internal/player"
	"github.com/JeffResc/DisplayLoop/internal/scheduler"
)

// ScreenData is passed to the screen detail template.
type ScreenData struct {
	Screen       db.Screen
	Status       player.ScreenStatus
	CurrentMedia *db.Media
	AuditLog     []db.AuditEntry
	WeekSchedule scheduler.WeekSchedule
	DayOrder     []string
}

var dayOrder = []string{"monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday"}

func (a *App) HandleScreenDetail(w http.ResponseWriter, r *http.Request) {
	id := screenIDFromURL(r)
	screen, err := db.GetScreen(a.DB, id)
	if err != nil {
		a.respondError(w, http.StatusNotFound, "screen not found")
		return
	}

	media, _ := db.GetCurrentMedia(a.DB, id)
	audit, _ := db.ListAuditLog(a.DB, id, 50)
	ws, _ := scheduler.ParseWeekSchedule(screen.OperatingHours)

	data := ScreenData{
		Screen:       *screen,
		Status:       a.Players.GetStatus(id),
		CurrentMedia: media,
		AuditLog:     audit,
		WeekSchedule: ws,
		DayOrder:     dayOrder,
	}

	a.render(w, "screen.html", data)
}

func (a *App) HandleScreenEnable(w http.ResponseWriter, r *http.Request) {
	id := screenIDFromURL(r)
	if err := r.ParseForm(); err != nil {
		a.respondError(w, http.StatusBadRequest, "invalid form")
		return
	}
	enabled := r.FormValue("enabled") == "true"
	if err := db.UpdateScreenEnabled(a.DB, id, enabled); err != nil {
		a.respondError(w, http.StatusInternalServerError, "failed to update")
		return
	}
	if !enabled {
		a.Players.Stop(id)
	}
	a.Scheduler.Evaluate()
	a.redirect(w, r, "/screens/"+itoa(id))
}

func (a *App) HandleScreenUpdateHours(w http.ResponseWriter, r *http.Request) {
	id := screenIDFromURL(r)
	if err := r.ParseForm(); err != nil {
		a.respondError(w, http.StatusBadRequest, "invalid form")
		return
	}

	ws := make(scheduler.WeekSchedule)
	for _, day := range dayOrder {
		enabled := r.FormValue(day+"_enabled") == "on"
		start := r.FormValue(day + "_start")
		end := r.FormValue(day + "_end")
		if start == "" {
			start = "00:00"
		}
		if end == "" {
			end = "23:59"
		}
		ws[day] = scheduler.DaySchedule{Enabled: enabled, Start: start, End: end}
	}

	hoursJSON, err := encodeJSON(ws)
	if err != nil {
		a.respondError(w, http.StatusInternalServerError, "encode hours")
		return
	}

	if err := db.UpdateScreenHours(a.DB, id, hoursJSON); err != nil {
		a.respondError(w, http.StatusInternalServerError, "failed to update hours")
		return
	}

	_ = db.InsertAuditLog(a.DB, id, "hours_changed", nil, nil, "")
	a.Scheduler.Evaluate()
	a.redirect(w, r, "/screens/"+itoa(id))
}

func (a *App) HandleScreenUpdateOffHours(w http.ResponseWriter, r *http.Request) {
	id := screenIDFromURL(r)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		a.respondError(w, http.StatusBadRequest, "invalid form")
		return
	}

	mode := r.FormValue("off_hours_mode")
	if mode != "black" && mode != "image" {
		mode = "black"
	}

	screen, err := db.GetScreen(a.DB, id)
	if err != nil {
		a.respondError(w, http.StatusNotFound, "screen not found")
		return
	}

	imagePath := screen.OffHoursImagePath

	// If an image was uploaded, save it.
	if mode == "image" {
		file, header, err := r.FormFile("off_hours_image")
		if err == nil {
			defer file.Close()
			savedPath, err := saveOffHoursImage(a.UploadsDir, id, header.Filename, file)
			if err != nil {
				a.respondError(w, http.StatusInternalServerError, "failed to save image")
				return
			}
			imagePath = savedPath
		}
	}

	if err := db.UpdateScreenOffHours(a.DB, id, mode, imagePath); err != nil {
		a.respondError(w, http.StatusInternalServerError, "failed to update off-hours config")
		return
	}

	_ = db.InsertAuditLog(a.DB, id, "off_hours_changed", nil, nil, fmt.Sprintf("mode=%s", mode))
	a.Scheduler.Evaluate()
	a.redirect(w, r, "/screens/"+itoa(id))
}

func (a *App) HandleScreenDelete(w http.ResponseWriter, r *http.Request) {
	id := screenIDFromURL(r)
	a.Players.Stop(id)
	_ = db.InsertAuditLog(a.DB, id, "screen_removed", nil, nil, "")
	if err := db.DeleteScreen(a.DB, id); err != nil {
		a.respondError(w, http.StatusInternalServerError, "failed to delete screen")
		return
	}
	a.redirect(w, r, "/")
}

func (a *App) HandleRollback(w http.ResponseWriter, r *http.Request) {
	screenID := screenIDFromURL(r)
	auditIDStr := chi.URLParam(r, "auditID")
	auditID, err := strconv.Atoi(auditIDStr)
	if err != nil {
		a.respondError(w, http.StatusBadRequest, "invalid audit ID")
		return
	}

	// Load audit entry to find previous media.
	entries, err := db.ListAuditLog(a.DB, screenID, 0)
	if err != nil {
		a.respondError(w, http.StatusInternalServerError, "failed to load audit log")
		return
	}

	var target *db.AuditEntry
	for i := range entries {
		if entries[i].ID == auditID {
			target = &entries[i]
			break
		}
	}

	if target == nil || target.PrevMediaID == nil {
		a.respondError(w, http.StatusBadRequest, "no rollback target found")
		return
	}

	prevMedia, err := db.GetMedia(a.DB, *target.PrevMediaID)
	if err != nil || prevMedia.Scrubbed {
		a.respondError(w, http.StatusConflict, "content has been scrubbed and cannot be restored")
		return
	}

	currentState, _ := db.GetScreenState(a.DB, screenID)
	if err := db.SetScreenMedia(a.DB, screenID, target.PrevMediaID); err != nil {
		a.respondError(w, http.StatusInternalServerError, "failed to rollback")
		return
	}

	var prevID *int
	if currentState != nil {
		prevID = currentState.MediaID
	}
	_ = db.InsertAuditLog(a.DB, screenID, "rollback", target.PrevMediaID, prevID, fmt.Sprintf("from audit #%d", auditID))
	a.Scheduler.Evaluate()
	a.redirect(w, r, "/screens/"+itoa(screenID))
}

// --- helpers ---

func screenIDFromURL(r *http.Request) int {
	id, _ := strconv.Atoi(chi.URLParam(r, "id"))
	return id
}

func itoa(n int) string {
	return strconv.Itoa(n)
}

func encodeJSON(v any) (string, error) {
	b, err := json.Marshal(v)
	return string(b), err
}
