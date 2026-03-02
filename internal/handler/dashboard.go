package handler

import (
	"net/http"
	"strconv"

	"github.com/JeffResc/DisplayLoop/internal/db"
	"github.com/JeffResc/DisplayLoop/internal/display"
	"github.com/JeffResc/DisplayLoop/internal/player"
	"github.com/JeffResc/DisplayLoop/internal/scheduler"
)

// DashboardData is passed to the dashboard template.
type DashboardData struct {
	Screens []ScreenSummary
}

type ScreenSummary struct {
	Screen       db.Screen
	Status       player.ScreenStatus
	CurrentMedia *db.Media
}

func (a *App) HandleDashboard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	screens, err := db.ListScreens(ctx, a.DB)
	if err != nil {
		a.respondError(w, http.StatusInternalServerError, "failed to list screens")
		return
	}

	ids := make([]int, len(screens))
	for i, s := range screens {
		ids[i] = s.ID
	}
	mediaMap, err := db.GetCurrentMediaBulk(ctx, a.DB, ids)
	if err != nil {
		a.respondError(w, http.StatusInternalServerError, "failed to load media")
		return
	}

	var summaries []ScreenSummary
	for _, s := range screens {
		summaries = append(summaries, ScreenSummary{
			Screen:       s,
			Status:       a.Players.GetStatus(s.ID),
			CurrentMedia: mediaMap[s.ID],
		})
	}

	a.render(w, "dashboard.html", DashboardData{Screens: summaries})
}

// DetectData is passed to the detect-displays template fragment.
type DetectData struct {
	Displays     []display.Display
	Error        string
	SkippedCount int // displays already configured as screens
}

func (a *App) HandleDetectDisplays(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	displays, err := display.Detect(ctx)
	var errMsg string
	if err != nil {
		errMsg = err.Error()
	}

	// Build a set of display_name values that are already configured.
	taken := make(map[string]struct{})
	if screens, err := db.ListScreens(ctx, a.DB); err == nil {
		for _, s := range screens {
			taken[s.DisplayName] = struct{}{}
		}
	}

	var available []display.Display
	for _, d := range displays {
		if _, exists := taken[d.Name]; exists {
			continue
		}
		available = append(available, d)
	}

	data := DetectData{
		Displays:     available,
		Error:        errMsg,
		SkippedCount: len(displays) - len(available),
	}
	a.renderPartial(w, "detect.html", "detect.html", data)
}

func (a *App) HandleAddScreen(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		a.respondError(w, http.StatusBadRequest, "invalid form")
		return
	}

	name := r.FormValue("name")
	displayName := r.FormValue("display_name")
	if name == "" || displayName == "" {
		a.respondError(w, http.StatusBadRequest, "name and display_name required")
		return
	}

	x, _ := strconv.Atoi(r.FormValue("x"))
	y, _ := strconv.Atoi(r.FormValue("y"))
	width, _ := strconv.Atoi(r.FormValue("width"))
	height, _ := strconv.Atoi(r.FormValue("height"))

	ws := scheduler.DefaultWeekSchedule()
	hoursJSON, _ := encodeJSON(ws)

	s := db.Screen{
		Name:           name,
		DisplayName:    displayName,
		X:              x,
		Y:              y,
		Width:          width,
		Height:         height,
		Enabled:        true,
		OffHoursMode:   "black",
		OperatingHours: hoursJSON,
	}

	ctx := r.Context()
	id, err := db.InsertScreen(ctx, a.DB, s)
	if err != nil {
		a.respondError(w, http.StatusInternalServerError, "failed to add screen")
		return
	}

	_ = db.InsertAuditLog(ctx, a.DB, int(id), "screen_added", nil, nil, displayName)
	a.Scheduler.Evaluate(ctx)
	a.redirect(w, r, "/screens/"+itoa(int(id)))
}
