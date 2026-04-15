package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"sync"

	"github.com/creack/pty"
	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
)

var wsUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// TerminalWS opens a WebSocket that proxies to a tmux session via a pty.
// GET /api/terminal/{sessionName}
func (h *Handler) TerminalWS(w http.ResponseWriter, r *http.Request) {
	sessionName := chi.URLParam(r, "sessionName")
	if sessionName == "" {
		http.Error(w, "session name required", http.StatusBadRequest)
		return
	}

	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Error("websocket upgrade failed", "error", err)
		return
	}
	defer conn.Close()

	// Spawn tmux attach with a pty
	cmd := exec.Command("tmux", "attach-session", "-t", sessionName)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")

	ptmx, err := pty.Start(cmd)
	if err != nil {
		slog.Error("pty start failed", "error", err, "session", sessionName)
		conn.WriteMessage(websocket.TextMessage, []byte("Error: "+err.Error()))
		return
	}
	defer ptmx.Close()

	// Set initial terminal size
	pty.Setsize(ptmx, &pty.Winsize{Rows: 40, Cols: 120})

	var once sync.Once
	done := make(chan struct{})

	// pty → websocket (terminal output → browser)
	go func() {
		buf := make([]byte, 32*1024)
		for {
			n, err := ptmx.Read(buf)
			if err != nil {
				once.Do(func() { close(done) })
				return
			}
			if err := conn.WriteMessage(websocket.BinaryMessage, buf[:n]); err != nil {
				once.Do(func() { close(done) })
				return
			}
		}
	}()

	// websocket → pty (browser input → terminal)
	go func() {
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				once.Do(func() { close(done) })
				return
			}

			// Handle resize messages: 0x01 prefix + JSON {cols, rows}
			if len(msg) > 1 && msg[0] == 1 {
				var size struct {
					Cols uint16 `json:"cols"`
					Rows uint16 `json:"rows"`
				}
				if json.Unmarshal(msg[1:], &size) == nil && size.Cols > 0 && size.Rows > 0 {
					pty.Setsize(ptmx, &pty.Winsize{Rows: size.Rows, Cols: size.Cols})
				}
				continue
			}

			ptmx.Write(msg)
		}
	}()

	<-done
	cmd.Process.Kill()
	cmd.Wait()
}
