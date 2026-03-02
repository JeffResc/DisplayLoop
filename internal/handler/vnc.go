package handler

import (
	"errors"
	"io"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/JeffResc/DisplayLoop/internal/db"
)

var vncUpgrader = websocket.Upgrader{
	// Allow all origins — safe because x11vnc only binds to 127.0.0.1.
	CheckOrigin: func(_ *http.Request) bool { return true },
}

// HandleVNCPage renders the noVNC viewer for a specific screen.
func (a *App) HandleVNCPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := screenIDFromURL(r)
	screen, err := db.GetScreen(ctx, a.DB, id)
	if err != nil {
		a.respondError(w, http.StatusNotFound, "screen not found")
		return
	}
	a.render(w, "vnc.html", screen)
}

// HandleVNCProxy upgrades the connection to WebSocket and proxies it to the
// x11vnc TCP listener for the requested screen.
func (a *App) HandleVNCProxy(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := screenIDFromURL(r)
	screen, err := db.GetScreen(ctx, a.DB, id)
	if err != nil {
		http.Error(w, "screen not found", http.StatusNotFound)
		return
	}

	port, err := a.VNC.EnsureRunning(*screen)
	if err != nil {
		http.Error(w, "failed to start VNC: "+err.Error(), http.StatusInternalServerError)
		return
	}

	wsConn, err := vncUpgrader.Upgrade(w, r, nil)
	if err != nil {
		// Upgrade writes its own error response on failure.
		return
	}
	defer wsConn.Close()

	// Give x11vnc a moment to start listening if it was just spawned.
	addr := itoa(port)
	dialer := &net.Dialer{Timeout: 500 * time.Millisecond}
	var vncConn net.Conn
	for i := 0; i < 10; i++ {
		vncConn, err = dialer.DialContext(ctx, "tcp", "127.0.0.1:"+addr)
		if err == nil {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if err != nil {
		_ = wsConn.WriteMessage(websocket.CloseMessage,
			websocket.FormatCloseMessage(1011, "cannot connect to VNC server"))
		return
	}
	defer vncConn.Close()

	var wg sync.WaitGroup
	wg.Add(2)

	// Browser → VNC
	go func() {
		defer wg.Done()
		defer vncConn.Close()
		for {
			_, msg, err := wsConn.ReadMessage()
			if err != nil {
				return
			}
			if _, err := vncConn.Write(msg); err != nil {
				return
			}
		}
	}()

	// VNC → Browser
	go func() {
		defer wg.Done()
		defer wsConn.Close()
		buf := make([]byte, 65536)
		for {
			n, err := vncConn.Read(buf)
			if n > 0 {
				if werr := wsConn.WriteMessage(websocket.BinaryMessage, buf[:n]); werr != nil {
					return
				}
			}
			if err != nil {
				if !errors.Is(err, io.EOF) {
					_ = wsConn.WriteMessage(websocket.CloseMessage,
						websocket.FormatCloseMessage(1011, "VNC connection closed"))
				}
				return
			}
		}
	}()

	wg.Wait()
}
