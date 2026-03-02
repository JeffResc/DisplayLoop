package vnc

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"sync"
	"time"

	"github.com/JeffResc/DisplayLoop/internal/db"
)

// Manager starts and supervises x11vnc subprocesses, one per screen.
// Each VNC instance is clipped to that screen's region and listens on
// localhost only at port (BasePort + screenID).
type Manager struct {
	mu       sync.RWMutex
	procs    map[int]*vncProc
	basePort int
}

type vncProc struct {
	cmd    *exec.Cmd
	cancel context.CancelFunc
	done   chan struct{} // closed when cmd.Wait() returns
	port   int
	screen db.Screen
}

// New creates a Manager. basePort defaults to 5900 if <= 0.
func New(basePort int) *Manager {
	if basePort <= 0 {
		basePort = 5900
	}
	return &Manager{
		procs:    make(map[int]*vncProc),
		basePort: basePort,
	}
}

// Port returns the VNC port for a given screen ID.
func (m *Manager) Port(screenID int) int {
	return m.basePort + screenID
}

// EnsureRunning starts x11vnc for the given screen if it is not already
// running, and returns its port. Safe to call concurrently.
func (m *Manager) EnsureRunning(screen db.Screen) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	port := m.basePort + screen.ID

	if p, ok := m.procs[screen.ID]; ok {
		select {
		case <-p.done:
			// process has exited — fall through to restart
		default:
			return port, nil // still running
		}
	}

	p, err := startX11VNC(screen, port)
	if err != nil {
		return 0, err
	}
	m.procs[screen.ID] = p
	return port, nil
}

// Stop kills the x11vnc process for a screen, if running.
func (m *Manager) Stop(screenID int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopLocked(screenID)
}

// StopAll kills all managed x11vnc processes. Called on app shutdown.
func (m *Manager) StopAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id := range m.procs {
		m.stopLocked(id)
	}
}

// StartHealthCheck runs a background goroutine that restarts any x11vnc
// processes that have exited unexpectedly.
func (m *Manager) StartHealthCheck(ctx context.Context) {
	go func() {
		t := time.NewTicker(15 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				m.mu.Lock()
				for id, p := range m.procs {
					select {
					case <-p.done:
						log.Printf("vnc: x11vnc for screen %d exited, restarting", id)
						np, err := startX11VNC(p.screen, p.port)
						if err != nil {
							log.Printf("vnc: restart screen %d: %v", id, err)
							delete(m.procs, id)
						} else {
							m.procs[id] = np
						}
					default:
					}
				}
				m.mu.Unlock()
			}
		}
	}()
}

// stopLocked kills a proc; must be called with m.mu held.
func (m *Manager) stopLocked(screenID int) {
	p, ok := m.procs[screenID]
	if !ok {
		return
	}
	p.cancel()
	<-p.done
	delete(m.procs, screenID)
}

// startX11VNC launches x11vnc clipped to the screen's region.
func startX11VNC(screen db.Screen, port int) (*vncProc, error) {
	clip := fmt.Sprintf("%dx%d+%d+%d", screen.Width, screen.Height, screen.X, screen.Y)
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, "x11vnc",
		"-display", ":0",
		"-nopw",
		"-listen", "127.0.0.1",
		"-rfbport", fmt.Sprintf("%d", port),
		"-clip", clip,
		"-forever",
		"-nossl",
		"-quiet",
	)
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("x11vnc: %w", err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = cmd.Wait()
	}()
	return &vncProc{cmd: cmd, cancel: cancel, done: done, port: port, screen: screen}, nil
}
