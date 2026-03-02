package scrubber

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/JeffResc/DisplayLoop/internal/db"
)

// Scrubber handles media file deletion and audit log pruning.
type Scrubber struct {
	database   *sql.DB
	uploadsDir string
	auditDays  int
	scrubDays  int
}

func New(database *sql.DB, uploadsDir string, auditDays, scrubDays int) *Scrubber {
	return &Scrubber{
		database:   database,
		uploadsDir: uploadsDir,
		auditDays:  auditDays,
		scrubDays:  scrubDays,
	}
}

// Run starts the scrubber. It runs immediately then waits until midnight.
func (s *Scrubber) Run(ctx context.Context) {
	s.runOnce(ctx)
	for {
		next := nextMidnight()
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Until(next)):
			s.runOnce(ctx)
		}
	}
}

func (s *Scrubber) runOnce(ctx context.Context) {
	now := time.Now()

	mediaCutoff := now.AddDate(0, 0, -s.scrubDays)
	if err := s.scrubMedia(ctx, mediaCutoff); err != nil {
		log.Printf("scrubber: media: %v", err)
	}

	auditCutoff := now.AddDate(0, 0, -s.auditDays)
	if n, err := db.DeleteOldAuditLog(ctx, s.database, auditCutoff); err != nil {
		log.Printf("scrubber: audit log: %v", err)
	} else if n > 0 {
		log.Printf("scrubber: pruned %d audit log entries", n)
	}
}

func (s *Scrubber) scrubMedia(ctx context.Context, cutoff time.Time) error {
	candidates, err := db.ScrubableMedia(ctx, s.database, cutoff)
	if err != nil {
		return fmt.Errorf("query scrubable media: %w", err)
	}

	for _, m := range candidates {
		filePath := filepath.Join(s.uploadsDir, fmt.Sprintf("%d", m.ScreenID), "content", m.Filename)
		if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
			log.Printf("scrubber: remove %s: %v", filePath, err)
			continue
		}
		if err := db.MarkMediaScrubbed(ctx, s.database, m.ID); err != nil {
			log.Printf("scrubber: mark scrubbed media %d: %v", m.ID, err)
			continue
		}
		log.Printf("scrubber: scrubbed media %d (%s)", m.ID, m.OriginalName)
	}
	return nil
}

func nextMidnight() time.Time {
	now := time.Now()
	return time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, now.Location())
}
