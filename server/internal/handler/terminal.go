package handler

import (
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
)

var wsUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// TerminalWS opens a WebSocket for a terminal session.
// Bridges the frontend WebSocket to the daemon's relay connection.
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

	if h.TermRelay == nil {
		conn.WriteMessage(websocket.TextMessage, []byte("Error: terminal relay not configured"))
		return
	}

	// Use full sessionName (including :window) as relay key.
	// Try up to 10 seconds for a daemon relay to appear.
	for i := 0; i < 20; i++ {
		if daemonConn := h.TermRelay.Get(sessionName); daemonConn != nil {
			slog.Info("bridging terminal via daemon relay", "session", sessionName)
			bridgeWebSockets(conn, daemonConn)
			return
		}
		time.Sleep(500 * time.Millisecond)
	}

	slog.Warn("no daemon relay found after timeout", "session", sessionName)
	conn.WriteMessage(websocket.TextMessage, []byte("Error: terminal not available (daemon relay timeout)"))
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

// ListTmuxWindows returns the tmux windows for a session.
// Reads from project_agent DB table instead of local tmux commands.
// GET /api/terminal-windows/{sessionName}
func (h *Handler) ListTmuxWindows(w http.ResponseWriter, r *http.Request) {
	sessionName := chi.URLParam(r, "sessionName")
	if sessionName == "" {
		writeJSON(w, http.StatusOK, []any{})
		return
	}

	type windowInfo struct {
		Name   string `json:"name"`
		Active bool   `json:"active"`
		Status string `json:"status"`
	}

	// Find the project that uses this tmux session.
	const findProjectSQL = `SELECT id FROM project_v2 WHERE tmux_session = $1 LIMIT 1`
	var projectID string
	if err := h.DB.QueryRow(r.Context(), findProjectSQL, sessionName).Scan(&projectID); err != nil {
		// No project found — return planner as default window.
		writeJSON(w, http.StatusOK, []windowInfo{{Name: "planner", Active: true, Status: "busy"}})
		return
	}

	// Get agents for this project.
	const agentsSQL = `SELECT role, agent_index, status, tmux_window FROM project_agent WHERE project_id = $1 ORDER BY role, agent_index`
	rows, err := h.DB.Query(r.Context(), agentsSQL, projectID)
	if err != nil {
		writeJSON(w, http.StatusOK, []windowInfo{{Name: "planner", Active: true, Status: "busy"}})
		return
	}
	defer rows.Close()

	var windows []windowInfo
	// Always include planner.
	windows = append(windows, windowInfo{Name: "planner", Active: false, Status: "idle"})

	for rows.Next() {
		var role, status string
		var agentIndex int
		var tmuxWindow *string
		if err := rows.Scan(&role, &agentIndex, &status, &tmuxWindow); err != nil {
			continue
		}
		name := role
		if tmuxWindow != nil && *tmuxWindow != "" {
			name = *tmuxWindow
		} else if role == "executor" {
			name = fmt.Sprintf("executor-%d", agentIndex)
		}
		windows = append(windows, windowInfo{Name: name, Active: false, Status: status})
	}

	if len(windows) == 1 {
		windows[0].Active = true
	}

	writeJSON(w, http.StatusOK, windows)
}
