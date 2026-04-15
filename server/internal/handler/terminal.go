package handler

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/creack/pty"
	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
)

var wsUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// TerminalWS opens a WebSocket that proxies to a specific tmux window via a pty.
// The sessionName can be "session:window" to target a specific window.
// Each WebSocket connection creates its own grouped tmux session so
// multiple browser tabs can independently view different windows.
//
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

	// Parse session:window
	baseSession := sessionName
	targetWindow := ""
	if idx := strings.Index(sessionName, ":"); idx > 0 {
		baseSession = sessionName[:idx]
		targetWindow = sessionName[idx+1:]
	}

	// Create a grouped session so this connection gets its own independent view.
	// Grouped sessions share windows but can select different ones independently.
	groupedName := fmt.Sprintf("%s-web-%d", baseSession, rand.Intn(99999))
	args := []string{"new-session", "-d", "-t", baseSession, "-s", groupedName}
	if err := exec.Command("tmux", args...).Run(); err != nil {
		slog.Error("tmux grouped session failed", "error", err, "base", baseSession)
		conn.WriteMessage(websocket.TextMessage, []byte("Error: tmux session not found"))
		return
	}

	// Select the target window if specified
	if targetWindow != "" {
		exec.Command("tmux", "select-window", "-t", groupedName+":"+targetWindow).Run()
	}

	// Attach to the grouped session via pty
	cmd := exec.Command("tmux", "attach-session", "-t", groupedName)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")

	ptmx, err := pty.Start(cmd)
	if err != nil {
		exec.Command("tmux", "kill-session", "-t", groupedName).Run()
		slog.Error("pty start failed", "error", err, "session", groupedName)
		conn.WriteMessage(websocket.TextMessage, []byte("Error: "+err.Error()))
		return
	}

	pty.Setsize(ptmx, &pty.Winsize{Rows: 40, Cols: 120})

	var once sync.Once
	done := make(chan struct{})

	// pty → websocket
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

	// websocket → pty
	go func() {
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				once.Do(func() { close(done) })
				return
			}
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
	// Clean up the grouped session
	exec.Command("tmux", "kill-session", "-t", groupedName).Run()
}

// ListTmuxWindows returns the tmux windows for a session.
// GET /api/terminal/{sessionName}/windows
func (h *Handler) ListTmuxWindows(w http.ResponseWriter, r *http.Request) {
sessionName := chi.URLParam(r, "sessionName")
if sessionName == "" {
writeJSON(w, http.StatusOK, []any{})
return
}

out, err := exec.Command("tmux", "list-windows", "-t", sessionName, "-F", "#{window_name} #{window_active}").Output()
if err != nil {
writeJSON(w, http.StatusOK, []any{})
return
}

type windowInfo struct {
Name   string `json:"name"`
Active bool   `json:"active"`
Status string `json:"status"`
}

var windows []windowInfo
for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
parts := strings.SplitN(line, " ", 2)
if len(parts) < 1 || parts[0] == "" {
continue
}
active := len(parts) > 1 && parts[1] == "1"
// Check if agent is busy by peeking at capture-pane
status := "idle"
capture, _ := exec.Command("tmux", "capture-pane", "-t", sessionName+":"+parts[0], "-p", "-S", "-5").Output()
captureStr := string(capture)
if strings.Contains(captureStr, "Esc to cancel") || strings.Contains(captureStr, "background /tasks") {
status = "busy"
}
windows = append(windows, windowInfo{Name: parts[0], Active: active, Status: status})
}

writeJSON(w, http.StatusOK, windows)
}
