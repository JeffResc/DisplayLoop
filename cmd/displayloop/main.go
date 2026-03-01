package main

import (
	"context"
	"flag"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/JeffResc/DisplayLoop/assets"
	"github.com/JeffResc/DisplayLoop/internal/config"
	"github.com/JeffResc/DisplayLoop/internal/db"
	"github.com/JeffResc/DisplayLoop/internal/handler"
	"github.com/JeffResc/DisplayLoop/internal/monitor"
	"github.com/JeffResc/DisplayLoop/internal/player"
	"github.com/JeffResc/DisplayLoop/internal/scheduler"
	"github.com/JeffResc/DisplayLoop/internal/scrubber"
)

// Build information, injected at link time via -ldflags.
// Example: go build -ldflags "-X main.version=1.0.0 -X main.commit=abc1234"
var (
	version = "dev"
	commit  = "unknown"
)

func main() {
	noVLC := flag.Bool("no-vlc", false, "skip spawning VLC (for UI development)")
	flag.Parse()

	cfg, err := config.Load("config.toml")
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	database, err := db.Open("displayloop.db")
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer database.Close()

	if err := os.MkdirAll(cfg.Server.UploadsDir, 0755); err != nil {
		log.Fatalf("create uploads dir: %v", err)
	}

	// Extract black.png to a temp file so VLC can read it from the filesystem.
	blackPNGPath, err := extractBlackPNG()
	if err != nil {
		log.Fatalf("extract black.png: %v", err)
	}
	defer os.Remove(blackPNGPath)

	templateFS, templateFuncs, err := buildTemplateAssets()
	if err != nil {
		log.Fatalf("build templates: %v", err)
	}

	players := player.New(blackPNGPath, *noVLC)
	sched := scheduler.New(database, players, cfg.Server.UploadsDir)

	app := &handler.App{
		DB:            database,
		Players:       players,
		Scheduler:     sched,
		UploadsDir:    cfg.Server.UploadsDir,
		TemplateFS:    templateFS,
		TemplateFuncs: templateFuncs,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	players.StartHealthCheck(ctx)
	go sched.Run(ctx)
	go scrubber.New(database, cfg.Server.UploadsDir, cfg.Retention.AuditDays, cfg.Retention.ScrubDays).Run(ctx)
	go monitor.New(database, players, sched).Run(ctx)
	app.StartBroadcaster(ctx)

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	// Pages
	r.Get("/", app.HandleDashboard)
	r.Get("/screens/detect", app.HandleDetectDisplays)
	r.Post("/screens", app.HandleAddScreen)
	r.Get("/screens/{id}", app.HandleScreenDetail)
	r.Post("/screens/{id}/upload", app.HandleUpload)
	r.Post("/screens/{id}/enable", app.HandleScreenEnable)
	r.Post("/screens/{id}/hours", app.HandleScreenUpdateHours)
	r.Post("/screens/{id}/offhours", app.HandleScreenUpdateOffHours)
	r.Post("/screens/{id}/delete", app.HandleScreenDelete)
	r.Post("/screens/{id}/rollback/{auditID}", app.HandleRollback)
	r.Get("/audit", app.HandleAuditLog)

	// API
	r.Get("/api/status", app.HandleStatusJSON)
	r.Get("/api/status/stream", app.HandleStatusStream)

	// Uploaded media (for image thumbnails)
	r.Get("/media/{id}/{filename}", app.HandleMediaServe)

	addr := fmt.Sprintf(":%d", cfg.Server.Port)
	srv := &http.Server{Addr: addr, Handler: r}

	go func() {
		log.Printf("DisplayLoop listening on http://localhost%s", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("Shutting down…")
	players.StopAll()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
}

// buildTemplateAssets returns the template filesystem and shared func map.
// Templates are NOT pre-parsed here — each handler parses layout + page
// together at request time to keep {{define}} blocks isolated per page.
func buildTemplateAssets() (fs.FS, template.FuncMap, error) {
	sub, err := fs.Sub(assets.Templates, "templates")
	if err != nil {
		return nil, nil, err
	}
	funcs := template.FuncMap{
		"buildVersion": func() string { return version },
		"buildCommit":  func() string { return commit },
		"formatTime": func(t time.Time) string {
			return t.Format("Jan 2, 2006 3:04 PM")
		},
		"eventTypeLabel": func(et string) string {
			switch et {
			case "content_uploaded":
				return "Uploaded"
			case "content_changed":
				return "Changed"
			case "rollback":
				return "Rollback"
			case "hours_changed":
				return "Hours updated"
			case "screen_added":
				return "Screen added"
			case "screen_removed":
				return "Screen removed"
			case "screen_configured":
				return "Configured"
			case "off_hours_changed":
				return "Off-hours updated"
			default:
				return et
			}
		},
		"eventTypeClass": func(et string) string {
			switch et {
			case "content_uploaded", "content_changed":
				return "bg-indigo-900 text-indigo-300"
			case "rollback":
				return "bg-yellow-900 text-yellow-300"
			case "screen_added":
				return "bg-green-900 text-green-300"
			case "screen_removed":
				return "bg-red-900 text-red-300"
			default:
				return "bg-gray-800 text-gray-400"
			}
		},
	}
	return sub, funcs, nil
}

func extractBlackPNG() (string, error) {
	data, err := assets.Static.ReadFile("static/black.png")
	if err != nil {
		return "", err
	}
	f, err := os.CreateTemp("", "displayloop-black-*.png")
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := f.Write(data); err != nil {
		return "", err
	}
	return filepath.Abs(f.Name())
}
