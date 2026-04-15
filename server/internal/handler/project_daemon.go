package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// ---------------------------------------------------------------------------
// ListRuntimeProjects — GET /api/daemon/runtimes/{runtimeId}/projects
// Returns projects assigned to this runtime, optionally filtered by status.
// ---------------------------------------------------------------------------

func (h *Handler) ListRuntimeProjects(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")

	if _, ok := h.requireDaemonRuntimeAccess(w, r, runtimeID); !ok {
		return
	}

	statusFilter := r.URL.Query().Get("status")
	var statuses []string
	if statusFilter != "" {
		statuses = strings.Split(statusFilter, ",")
	}

	// Build query: if statuses provided, filter by them; otherwise return all.
	var selectSQL string
	var args []any
	if len(statuses) > 0 {
		// Use ANY($2) with a text array for PostgreSQL.
		selectSQL = `
			SELECT id, name, COALESCE(goals, ''), COALESCE(skills::text, '[]'), status, COALESCE(config::text, '{}')
			FROM project_v2
			WHERE runtime_id = $1 AND status = ANY($2)`
		args = []any{runtimeID, statuses}
	} else {
		selectSQL = `
			SELECT id, name, COALESCE(goals, ''), COALESCE(skills::text, '[]'), status, COALESCE(config::text, '{}')
			FROM project_v2
			WHERE runtime_id = $1`
		args = []any{runtimeID}
	}

	rows, err := h.DB.Query(r.Context(), selectSQL, args...)
	if err != nil {
		slog.Error("failed to list runtime projects", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list projects")
		return
	}
	defer rows.Close()

	type projectResponse struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Goals  string `json:"goals"`
		Skills string `json:"skills"`
		Status string `json:"status"`
		Config string `json:"config"`
	}

	projects := []projectResponse{}
	for rows.Next() {
		var p projectResponse
		if err := rows.Scan(&p.ID, &p.Name, &p.Goals, &p.Skills, &p.Status, &p.Config); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to scan project")
			return
		}
		projects = append(projects, p)
	}

	writeJSON(w, http.StatusOK, projects)
}

// ---------------------------------------------------------------------------
// DaemonGetProjectMessages — GET /api/daemon/projects/{projectId}/messages
// Daemon polls for new messages since a given seq.
// ---------------------------------------------------------------------------

func (h *Handler) DaemonGetProjectMessages(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectId")

	sinceSeq := 0
	if s := r.URL.Query().Get("since_seq"); s != "" {
		if v, err := strconv.Atoi(s); err == nil {
			sinceSeq = v
		}
	}

	const selectSQL = `
		SELECT id, project_id, role, agent_id, content, phase, seq, created_at
		FROM project_message WHERE project_id = $1 AND seq > $2
		ORDER BY seq`

	rows, err := h.DB.Query(r.Context(), selectSQL, projectID, sinceSeq)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list messages")
		return
	}
	defer rows.Close()

	msgs := []projectMessageResponse{}
	for rows.Next() {
		m, err := scanProjectMessage(rows)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to scan message")
			return
		}
		msgs = append(msgs, m)
	}

	writeJSON(w, http.StatusOK, msgs)
}

// ---------------------------------------------------------------------------
// DaemonSendProjectMessage — POST /api/daemon/projects/{projectId}/messages
// Daemon reports planner/agent output back to the server.
// ---------------------------------------------------------------------------

func (h *Handler) DaemonSendProjectMessage(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectId")

	var req struct {
		Role    string `json:"role"`
		Content string `json:"content"`
		Phase   string `json:"phase"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Content == "" {
		writeError(w, http.StatusBadRequest, "content is required")
		return
	}
	if req.Role == "" {
		req.Role = "planner"
	}
	if req.Phase == "" {
		req.Phase = "planning"
	}

	const insertSQL = `
		INSERT INTO project_message (project_id, role, content, phase, seq)
		VALUES ($1, $2, $3, $4, COALESCE((SELECT MAX(seq) FROM project_message WHERE project_id = $1), 0) + 1)
		RETURNING id, project_id, role, agent_id, content, phase, seq, created_at`

	row := h.DB.QueryRow(r.Context(), insertSQL, projectID, req.Role, req.Content, req.Phase)

	var m projectMessageResponse
	var createdAt time.Time
	if err := row.Scan(&m.ID, &m.ProjectID, &m.Role, &m.AgentID, &m.Content, &m.Phase, &m.Seq, &createdAt); err != nil {
		slog.Error("failed to insert daemon project message", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to send message")
		return
	}
	m.CreatedAt = createdAt.Format(time.RFC3339)

	// Resolve workspace for broadcast.
	var workspaceID string
	const wsSQL = `SELECT workspace_id FROM project_v2 WHERE id = $1`
	_ = h.DB.QueryRow(r.Context(), wsSQL, projectID).Scan(&workspaceID)

	if workspaceID != "" {
		h.publish(protocol.EventProjectMessage, workspaceID, "daemon", "", m)
	}

	writeJSON(w, http.StatusCreated, m)
}

// DaemonUpdateProjectStatus — POST /api/daemon/projects/{projectId}/status
func (h *Handler) DaemonUpdateProjectStatus(w http.ResponseWriter, r *http.Request) {
projectID := chi.URLParam(r, "projectId")

var req struct {
Status string `json:"status"`
}
if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Status == "" {
writeError(w, http.StatusBadRequest, "status is required")
return
}

const updateSQL = `UPDATE project_v2 SET status = $1, updated_at = NOW() WHERE id = $2`
tag, err := h.DB.Exec(r.Context(), updateSQL, req.Status, projectID)
if err != nil || tag.RowsAffected() == 0 {
writeError(w, http.StatusNotFound, "project not found")
return
}

writeJSON(w, http.StatusOK, map[string]string{"status": req.Status})
}
