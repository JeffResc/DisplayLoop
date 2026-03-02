// Package monitor watches for physical display connect/disconnect events via
// xrandr and updates the player manager accordingly.
package monitor

import (
	"context"
	"database/sql"
	"log"
	"time"

	"github.com/JeffResc/DisplayLoop/internal/db"
	"github.com/JeffResc/DisplayLoop/internal/display"
	"github.com/JeffResc/DisplayLoop/internal/player"
	"github.com/JeffResc/DisplayLoop/internal/scheduler"
)

// Monitor polls xrandr on a regular interval and reconciles display connection
// state with the player manager.
type Monitor struct {
	database  *sql.DB
	players   *player.Manager
	scheduler *scheduler.Scheduler
}

func New(database *sql.DB, players *player.Manager, sched *scheduler.Scheduler) *Monitor {
	return &Monitor{database: database, players: players, scheduler: sched}
}

// Run starts the polling loop. It performs an immediate check on start.
func (m *Monitor) Run(ctx context.Context) {
	m.check(ctx)
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.check(ctx)
		}
	}
}

func (m *Monitor) check(ctx context.Context) {
	connected, err := display.ConnectedNames(ctx)
	if err != nil {
		// xrandr may not be available in --no-vlc dev mode; log and continue.
		log.Printf("monitor: xrandr query failed: %v", err)
		return
	}

	screens, err := db.ListScreens(ctx, m.database)
	if err != nil {
		log.Printf("monitor: list screens: %v", err)
		return
	}

	for _, screen := range screens {
		if !screen.Enabled {
			continue
		}
		_, isConnected := connected[screen.DisplayName]
		wasDisconnected := m.players.IsDisconnected(screen.ID)

		switch {
		case !isConnected && !wasDisconnected:
			// Newly disconnected — kill VLC, mark offline.
			log.Printf("monitor: display %q (%s) disconnected", screen.Name, screen.DisplayName)
			m.players.SetDisconnected(screen.ID)

		case isConnected && wasDisconnected:
			// Reconnected — clear flag and let the scheduler restart playback.
			log.Printf("monitor: display %q (%s) reconnected", screen.Name, screen.DisplayName)
			m.players.ClearDisconnected(screen.ID)
			m.scheduler.Evaluate(ctx)
		}
	}
}
