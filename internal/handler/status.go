package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/JeffResc/DisplayLoop/internal/db"
	"github.com/JeffResc/DisplayLoop/internal/player"
)

// StatusPayload is the JSON body sent over SSE.
type StatusPayload struct {
	Screens []ScreenStatusJSON `json:"screens"`
}

type ScreenStatusJSON struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Status      string `json:"status"`
	CurrentFile string `json:"current_file"`
	UptimeSecs  int    `json:"uptime_secs"`
}

func (a *App) HandleStatusJSON(w http.ResponseWriter, r *http.Request) {
	payload, err := a.buildStatusPayload()
	if err != nil {
		a.respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(payload)
}

// SSEHub manages Server-Sent Events clients.
type SSEHub struct {
	mu      sync.Mutex
	clients map[chan string]struct{}
}

func NewSSEHub() *SSEHub {
	return &SSEHub{clients: make(map[chan string]struct{})}
}

func (h *SSEHub) subscribe() chan string {
	ch := make(chan string, 4)
	h.mu.Lock()
	h.clients[ch] = struct{}{}
	h.mu.Unlock()
	return ch
}

func (h *SSEHub) unsubscribe(ch chan string) {
	h.mu.Lock()
	delete(h.clients, ch)
	h.mu.Unlock()
}

func (h *SSEHub) Broadcast(msg string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.clients {
		select {
		case ch <- msg:
		default:
		}
	}
}

func (a *App) HandleStatusStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	ch := a.Hub.subscribe()
	defer a.Hub.unsubscribe(ch)

	// Send initial payload immediately.
	if payload, err := a.buildStatusPayload(); err == nil {
		if data, err := json.Marshal(payload); err == nil {
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
	}

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-ch:
			fmt.Fprintf(w, "data: %s\n\n", msg)
			flusher.Flush()
		}
	}
}

// StartBroadcaster runs a goroutine that pushes status to all SSE clients every 3s.
func (a *App) StartBroadcaster(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				payload, err := a.buildStatusPayload()
				if err != nil {
					continue
				}
				data, err := json.Marshal(payload)
				if err != nil {
					continue
				}
				a.Hub.Broadcast(string(data))
			}
		}
	}()
}

func (a *App) buildStatusPayload() (*StatusPayload, error) {
	screens, err := db.ListScreens(a.DB)
	if err != nil {
		return nil, err
	}

	payload := &StatusPayload{}
	for _, s := range screens {
		st := a.Players.GetStatus(s.ID)
		var uptime int
		if st.Status != player.StatusStopped {
			uptime = int(time.Since(st.StartedAt).Seconds())
		}
		payload.Screens = append(payload.Screens, ScreenStatusJSON{
			ID:          s.ID,
			Name:        s.Name,
			Status:      st.Status.String(),
			CurrentFile: st.CurrentFile,
			UptimeSecs:  uptime,
		})
	}
	return payload, nil
}
