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

// TerminalWS opens a WebSocket for a terminal session.
// If a daemon relay connection exists for this session, it bridges the
// frontend WebSocket to the daemon's relay. Otherwise, falls back to
// attaching a local tmux session via PTY.
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

	// Try daemon relay first.
	if h.TermRelay != nil {
		// Parse base session name (strip :window suffix) to find relay.
		baseSession := sessionName
		if idx := strings.Index(sessionName, ":"); idx > 0 {
			baseSession = sessionName[:idx]
		}

		if daemonConn := h.TermRelay.Get(baseSession); daemonConn != nil {
			slog.Info("bridging terminal via daemon relay", "session", sessionName)
			bridgeWebSockets(conn, daemonConn)
			return
		}
	}

	// Fallback: local tmux (original behavior).
	h.terminalLocal(conn, sessionName)
}

// bridgeWebSockets bidirectionally copies messages between two WebSocket connections.
func bridgeWebSockets(frontend, daemon *websocket.Conn) {
	var once sync.Once
	done := make(chan struct{})

	// daemon → frontend
	go func() {
		for {
			msgType, msg, err := daemon.ReadMessage()
			if err != nil {
				once.Do(func() { close(done) })
				return
			}
			if err := frontend.WriteMessage(msgType, msg); err != nil {
				once.Do(func() { close(done) })
				return
			}
		}
	}()

	// frontend → daemon
	go func() {
		for {
			msgType, msg, err := frontend.ReadMessage()
			if err != nil {
				once.Do(func() { close(done) })
				return
			}
			if err := daemon.WriteMessage(msgType, msg); err != nil {
				once.Do(func() { close(done) })
				return
			}
		}
	}()

	<-done
	frontend.Close()
	daemon.Close()
}

// terminalLocal is the original local tmux attach behavior.
func (h *Handler) terminalLocal(conn *websocket.Conn, sessionName string) {
	baseSession := sessionName
	targetWindow := ""
	if idx := strings.Index(sessionName, ":"); idx > 0 {
		baseSession = sessionName[:idx]
		targetWindow = sessionName[idx+1:]
	}

	groupedName := fmt.Sprintf("%s-web-%d", baseSession, rand.Intn(99999))
	args := []string{"new-session", "-d", "-t", baseSession, "-s", groupedName}
	if err := exec.Command("tmux", args...).Run(); err != nil {
		slog.Error("tmux grouped session failed", "error", err, "base", baseSession)
		conn.WriteMessage(websocket.TextMessage, []byte("Error: tmux session not found"))
		return
	}

	if targetWindow != "" {
		exec.Command("tmux", "select-window", "-t", groupedName+":"+targetWindow).Run()
	}

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
	exec.Command("tmux", "kill-session", "-t", groupedName).Run()
}

// ListTmuxWindows returns the tmux windows for a session.
// GET /api/terminal-windows/{sessionName}
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
