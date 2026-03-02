package scheduler

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"time"

	"github.com/JeffResc/DisplayLoop/internal/db"
	"github.com/JeffResc/DisplayLoop/internal/player"
)

// DaySchedule holds on/off and time range for a single day.
type DaySchedule struct {
	Enabled bool   `json:"enabled"`
	Start   string `json:"start"` // "HH:MM"
	End     string `json:"end"`   // "HH:MM"
}

// WeekSchedule maps lowercase day names to their schedule.
type WeekSchedule map[string]DaySchedule

// DefaultWeekSchedule returns an all-day-enabled schedule.
func DefaultWeekSchedule() WeekSchedule {
	days := []string{"monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday"}
	s := make(WeekSchedule, 7)
	for _, d := range days {
		s[d] = DaySchedule{Enabled: true, Start: "00:00", End: "23:59"}
	}
	return s
}

// ParseWeekSchedule decodes a JSON operating hours string.
func ParseWeekSchedule(jsonStr string) (WeekSchedule, error) {
	if jsonStr == "" || jsonStr == "{}" {
		return DefaultWeekSchedule(), nil
	}
	var ws WeekSchedule
	if err := json.Unmarshal([]byte(jsonStr), &ws); err != nil {
		return nil, err
	}
	return ws, nil
}

// IsWithinHours reports whether now falls within the schedule for today.
func IsWithinHours(ws WeekSchedule, now time.Time) bool {
	dayName := strings.ToLower(now.Weekday().String())
	day, ok := ws[dayName]
	if !ok || !day.Enabled {
		return false
	}
	startH, startM, err := parseHM(day.Start)
	if err != nil {
		return false
	}
	endH, endM, err := parseHM(day.End)
	if err != nil {
		return false
	}
	start := time.Date(now.Year(), now.Month(), now.Day(), startH, startM, 0, 0, now.Location())
	end := time.Date(now.Year(), now.Month(), now.Day(), endH, endM, 59, 0, now.Location())
	return !now.Before(start) && !now.After(end)
}

func parseHM(s string) (int, int, error) {
	var h, m int
	_, err := fmt.Sscanf(s, "%d:%d", &h, &m)
	return h, m, err
}

// Scheduler evaluates operating hours for all screens every minute.
type Scheduler struct {
	database   *sql.DB
	players    *player.Manager
	uploadsDir string
}

func New(database *sql.DB, players *player.Manager, uploadsDir string) *Scheduler {
	return &Scheduler{database: database, players: players, uploadsDir: uploadsDir}
}

// Run starts the scheduler loop. Call Evaluate to trigger an immediate check.
func (s *Scheduler) Run(ctx context.Context) {
	s.Evaluate()
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.Evaluate()
		}
	}
}

// Evaluate checks all screens and updates VLC state as needed.
func (s *Scheduler) Evaluate() {
	screens, err := db.ListScreens(s.database)
	if err != nil {
		log.Printf("scheduler: list screens: %v", err)
		return
	}

	now := time.Now()
	for _, screen := range screens {
		if err := s.evaluateScreen(screen, now); err != nil {
			log.Printf("scheduler: screen %d (%s): %v", screen.ID, screen.Name, err)
		}
	}
}

func (s *Scheduler) evaluateScreen(screen db.Screen, now time.Time) error {
	if !screen.Enabled {
		s.players.Stop(screen.ID)
		return nil
	}

	ws, err := ParseWeekSchedule(screen.OperatingHours)
	if err != nil {
		return fmt.Errorf("parse hours: %w", err)
	}

	withinHours := IsWithinHours(ws, now)

	if withinHours {
		// Should be playing content.
		if s.players.IsOffHours(screen.ID) || !s.players.IsRunning(screen.ID) {
			// Need to start or switch to content.
			currentMedia, err := db.GetCurrentMedia(s.database, screen.ID)
			if err != nil {
				return fmt.Errorf("get current media: %w", err)
			}
			if currentMedia == nil || currentMedia.Scrubbed {
				// No content assigned — stop anything that's running.
				s.players.Stop(screen.ID)
				return nil
			}
			filePath := filepath.Join(s.uploadsDir, fmt.Sprintf("%d", screen.ID), "content", currentMedia.Filename)
			isImage := currentMedia.MediaType == "image"
			return s.players.Play(screen, filePath, isImage)
		}
	} else {
		// Outside operating hours — switch to off-hours mode.
		if !s.players.IsOffHours(screen.ID) {
			return s.players.PlayOffHours(screen)
		}
	}
	return nil
}

