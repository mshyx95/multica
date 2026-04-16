package handler

import (
	"log/slog"
	"net/http"
	"sync"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
)

// TerminalRelay manages daemon-side terminal WebSocket connections.
// Daemons register their tmux relay connections here, and the server
// bridges frontend terminal WebSockets to the matching daemon connection.
type TerminalRelay struct {
	mu     sync.Mutex
	active map[string]*websocket.Conn // sessionName → daemon WS connection
}

// NewTerminalRelay creates a new relay registry.
func NewTerminalRelay() *TerminalRelay {
	return &TerminalRelay{
		active: make(map[string]*websocket.Conn),
	}
}

// Register stores a daemon connection for the given session.
func (tr *TerminalRelay) Register(sessionName string, conn *websocket.Conn) {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	// Close any previous connection for this session.
	if old, ok := tr.active[sessionName]; ok {
		old.Close()
	}
	tr.active[sessionName] = conn
}

// Get retrieves and removes a daemon connection for the given session.
// The caller takes ownership of the connection.
func (tr *TerminalRelay) Get(sessionName string) *websocket.Conn {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	conn := tr.active[sessionName]
	if conn != nil {
		delete(tr.active, sessionName)
	}
	return conn
}

// Unregister removes a daemon connection.
func (tr *TerminalRelay) Unregister(sessionName string) {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	delete(tr.active, sessionName)
}

// DaemonTerminalRelay handles a daemon connecting to relay terminal I/O.
// The daemon attaches to its local tmux session and sends PTY data over this WS.
// When a frontend client connects via TerminalWS, the server bridges the two.
//
// GET /api/daemon/terminal-relay/{sessionName}
func (h *Handler) DaemonTerminalRelay(w http.ResponseWriter, r *http.Request) {
	sessionName := chi.URLParam(r, "sessionName")
	if sessionName == "" {
		http.Error(w, "session name required", http.StatusBadRequest)
		return
	}

	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Error("daemon terminal relay upgrade failed", "error", err)
		return
	}

	slog.Info("daemon terminal relay connected", "session", sessionName)

	if h.TermRelay == nil {
		conn.WriteMessage(websocket.TextMessage, []byte("terminal relay not initialized"))
		conn.Close()
		return
	}

	h.TermRelay.Register(sessionName, conn)

	// The connection is now in the relay registry. It will be consumed by
	// bridgeWebSockets when a frontend connects. We keep this handler alive
	// by blocking until the request context is done (server shutdown or
	// client disconnect detected by net/http).
	<-r.Context().Done()

	h.TermRelay.Unregister(sessionName)
	slog.Info("daemon terminal relay disconnected", "session", sessionName)
}
