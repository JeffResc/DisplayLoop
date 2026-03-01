package handler

import (
	"fmt"
	"net/http"

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
	screens, err := db.ListScreens(a.DB)
	if err != nil {
		a.respondError(w, http.StatusInternalServerError, "failed to list screens")
		return
	}

	var summaries []ScreenSummary
	for _, s := range screens {
		media, _ := db.GetCurrentMedia(a.DB, s.ID)
		summaries = append(summaries, ScreenSummary{
			Screen:       s,
			Status:       a.Players.GetStatus(s.ID),
			CurrentMedia: media,
		})
	}

	a.render(w, "dashboard.html", DashboardData{Screens: summaries})
}

// DetectData is passed to the detect-displays template fragment.
type DetectData struct {
	Displays []display.Display
	Error    string
}

func (a *App) HandleDetectDisplays(w http.ResponseWriter, r *http.Request) {
	displays, err := display.Detect()
	var errMsg string
	if err != nil {
		errMsg = err.Error()
	}
	a.renderPartial(w, "detect.html", "detect.html", DetectData{Displays: displays, Error: errMsg})
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

	var x, y, width, height int
	fmt.Sscanf(r.FormValue("x"), "%d", &x)
	fmt.Sscanf(r.FormValue("y"), "%d", &y)
	fmt.Sscanf(r.FormValue("width"), "%d", &width)
	fmt.Sscanf(r.FormValue("height"), "%d", &height)

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

	id, err := db.InsertScreen(a.DB, s)
	if err != nil {
		a.respondError(w, http.StatusInternalServerError, "failed to add screen")
		return
	}

	_ = db.InsertAuditLog(a.DB, int(id), "screen_added", nil, nil, displayName)
	a.Scheduler.Evaluate()
	a.redirect(w, r, "/screens/"+itoa(int(id)))
}
