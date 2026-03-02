package player

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/JeffResc/DisplayLoop/internal/db"
)

// vlcBackground is used as the context for VLC subprocesses; VLC is a
// long-running process managed by the Manager, not tied to any request context.
var vlcBackground = context.Background()

// Status describes the current state of a screen's VLC process.
type Status int

const (
	StatusStopped Status = iota
	StatusPlaying
	StatusOffHours
	StatusError
	StatusDisconnected
)

func (s Status) String() string {
	switch s {
	case StatusPlaying:
		return "Playing"
	case StatusOffHours:
		return "Off-hours"
	case StatusError:
		return "Error"
	case StatusDisconnected:
		return "Disconnected"
	default:
		return "Stopped"
	}
}

// ScreenStatus holds runtime info for one screen.
type ScreenStatus struct {
	ScreenID    int
	Status      Status
	CurrentFile string // display name of what's playing
	StartedAt   time.Time
	Err         string
}

type screenEntry struct {
	cmd        *exec.Cmd
	done       chan struct{} // closed when cmd.Wait() returns
	filePath   string
	status     Status
	startedAt  time.Time
	screen     db.Screen
	isOffHours bool
}

// Manager controls VLC subprocesses, one per screen.
type Manager struct {
	mu           sync.RWMutex
	entries      map[int]*screenEntry
	disconnected map[int]bool

	// blackPNGPath is the path to the bundled black.png extracted to a temp file.
	blackPNGPath string

	// dryRun skips actually spawning VLC — useful for UI development.
	dryRun bool
}

func New(blackPNGPath string, dryRun bool) *Manager {
	return &Manager{
		entries:      make(map[int]*screenEntry),
		disconnected: make(map[int]bool),
		blackPNGPath: blackPNGPath,
		dryRun:       dryRun,
	}
}

// SetDisconnected stops VLC for a screen and marks it as physically disconnected.
func (m *Manager) SetDisconnected(screenID int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopLocked(screenID)
	m.disconnected[screenID] = true
}

// ClearDisconnected removes the disconnected flag so the screen can play again.
func (m *Manager) ClearDisconnected(screenID int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.disconnected, screenID)
}

// IsDisconnected reports whether the display for a screen is currently offline.
func (m *Manager) IsDisconnected(screenID int) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.disconnected[screenID]
}

// Play starts (or restarts) VLC for the given screen with the specified file.
// isImage should be true for image files.
func (m *Manager) Play(screen db.Screen, filePath string, isImage bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.stopLocked(screen.ID)

	var cmd *exec.Cmd
	var done chan struct{}
	if !m.dryRun {
		var err error
		cmd, done, err = startVLC(screen, filePath, isImage)
		if err != nil {
			return fmt.Errorf("start vlc for screen %d: %w", screen.ID, err)
		}
	}

	m.entries[screen.ID] = &screenEntry{
		cmd:       cmd,
		done:      done,
		filePath:  filePath,
		status:    StatusPlaying,
		startedAt: time.Now(),
		screen:    screen,
	}
	return nil
}

// PlayOffHours switches a screen to its off-hours content (black or image).
func (m *Manager) PlayOffHours(screen db.Screen) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.stopLocked(screen.ID)

	var filePath string
	if screen.OffHoursMode == "image" && screen.OffHoursImagePath != "" {
		filePath = screen.OffHoursImagePath
	} else {
		filePath = m.blackPNGPath
	}

	var cmd *exec.Cmd
	var done chan struct{}
	if !m.dryRun {
		var err error
		cmd, done, err = startVLC(screen, filePath, true)
		if err != nil {
			return fmt.Errorf("start vlc off-hours for screen %d: %w", screen.ID, err)
		}
	}

	m.entries[screen.ID] = &screenEntry{
		cmd:        cmd,
		done:       done,
		filePath:   filePath,
		status:     StatusOffHours,
		startedAt:  time.Now(),
		screen:     screen,
		isOffHours: true,
	}
	return nil
}

// Stop kills the VLC process for a screen.
func (m *Manager) Stop(screenID int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopLocked(screenID)
}

// StopAll kills all VLC processes.
func (m *Manager) StopAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id := range m.entries {
		m.stopLocked(id)
	}
}

func (m *Manager) stopLocked(screenID int) {
	e, ok := m.entries[screenID]
	if !ok {
		return
	}
	if e.cmd != nil && e.cmd.Process != nil {
		_ = e.cmd.Process.Kill()
		// Wait for the background Wait() goroutine to finish so the process
		// is fully reaped before we replace the entry.
		if e.done != nil {
			<-e.done
		}
	}
	delete(m.entries, screenID)
}

// GetStatus returns the current status for a screen.
func (m *Manager) GetStatus(screenID int) ScreenStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.disconnected[screenID] {
		return ScreenStatus{ScreenID: screenID, Status: StatusDisconnected}
	}
	e, ok := m.entries[screenID]
	if !ok {
		return ScreenStatus{ScreenID: screenID, Status: StatusStopped}
	}
	return ScreenStatus{
		ScreenID:    screenID,
		Status:      e.status,
		CurrentFile: filepath.Base(e.filePath),
		StartedAt:   e.startedAt,
	}
}

// GetAllStatuses returns statuses for all known screens.
func (m *Manager) GetAllStatuses() []ScreenStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	statuses := make([]ScreenStatus, 0, len(m.entries))
	for _, e := range m.entries {
		statuses = append(statuses, ScreenStatus{
			ScreenID:    e.screen.ID,
			Status:      e.status,
			CurrentFile: filepath.Base(e.filePath),
			StartedAt:   e.startedAt,
		})
	}
	return statuses
}

// IsOffHours reports whether the screen is currently in off-hours mode.
func (m *Manager) IsOffHours(screenID int) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	e, ok := m.entries[screenID]
	return ok && e.isOffHours
}

// IsRunning reports whether VLC is running for the screen (any mode).
// Returns false for disconnected screens even if an entry somehow exists.
func (m *Manager) IsRunning(screenID int) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.disconnected[screenID] {
		return false
	}
	_, ok := m.entries[screenID]
	return ok
}

// StartHealthCheck runs a background goroutine that restarts crashed VLC processes.
func (m *Manager) StartHealthCheck(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.healthCheck()
			}
		}
	}()
}

func (m *Manager) healthCheck() {
	if m.dryRun {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	for screenID, e := range m.entries {
		if m.disconnected[screenID] || e.cmd == nil || e.done == nil {
			continue
		}
		// Non-blocking check: has the Wait() goroutine finished (i.e. VLC exited)?
		select {
		case <-e.done:
			// VLC exited unexpectedly; restart it.
			log.Printf("VLC for screen %d exited unexpectedly, restarting", screenID)
			cmd, done, err := startVLC(e.screen, e.filePath, isImageFile(e.filePath))
			if err != nil {
				log.Printf("restart VLC screen %d: %v", screenID, err)
				e.status = StatusError
				e.cmd = nil
				e.done = nil
				continue
			}
			e.cmd = cmd
			e.done = done
			e.startedAt = time.Now()
			if e.isOffHours {
				e.status = StatusOffHours
			} else {
				e.status = StatusPlaying
			}
		default:
			// Still running.
		}
	}
}

// startVLC starts a VLC subprocess and returns the cmd and a done channel that
// is closed when the process exits (i.e. when cmd.Wait() returns).
func startVLC(screen db.Screen, filePath string, isImage bool) (*exec.Cmd, chan struct{}, error) {
	cmd := buildVLCCommand(screen, filePath, isImage)
	if err := cmd.Start(); err != nil {
		return nil, nil, err
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = cmd.Wait()
	}()
	return cmd, done, nil
}

func buildVLCCommand(screen db.Screen, filePath string, isImage bool) *exec.Cmd {
	args := []string{
		"--intf", "dummy",
		"--no-interact",
		"--no-audio",
		"--no-video-title-show",
		"--no-metadata-network-access",
		"--no-osd",
		"--no-video-deco",
		fmt.Sprintf("--video-x=%d", screen.X),
		fmt.Sprintf("--video-y=%d", screen.Y),
		fmt.Sprintf("--width=%d", screen.Width),
		fmt.Sprintf("--height=%d", screen.Height),
		"--loop",
	}
	if isImage {
		args = append(args, "--image-duration=-1")
	}
	args = append(args, filePath)
	return exec.CommandContext(vlcBackground, "vlc", args...)
}

func isImageFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".jpg", ".jpeg", ".png", ".gif", ".bmp", ".webp":
		return true
	}
	return false
}
