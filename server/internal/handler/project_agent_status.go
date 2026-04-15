package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// ---------------------------------------------------------------------------
// DaemonReportAgentStatus — POST /api/daemon/projects/{projectId}/agent-status
// Called by the daemon to upsert agent states for a project execution.
// ---------------------------------------------------------------------------

func (h *Handler) DaemonReportAgentStatus(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectId")
	if projectID == "" {
		writeError(w, http.StatusBadRequest, "projectId is required")
		return
	}

	var req struct {
		Agents map[string]string `json:"agents"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.Agents) == 0 {
		writeError(w, http.StatusBadRequest, "agents map is required")
		return
	}

	// Look up the project to get workspace_id for auth & broadcasting.
	const projectSQL = `SELECT workspace_id FROM project_v2 WHERE id = $1`
	var workspaceID string
	if err := h.DB.QueryRow(r.Context(), projectSQL, projectID).Scan(&workspaceID); err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}

	if !h.requireDaemonWorkspaceAccess(w, r, workspaceID) {
		return
	}

	// Upsert each agent's status into project_agent.
	const upsertSQL = `
		INSERT INTO project_agent (project_id, role, agent_index, status, last_heartbeat)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (project_id, role, agent_index) DO UPDATE SET
			status = EXCLUDED.status,
			last_heartbeat = EXCLUDED.last_heartbeat`

	now := time.Now()
	for agentName, status := range req.Agents {
		role, idx := parseAgentName(agentName)
		if _, err := h.DB.Exec(r.Context(), upsertSQL, projectID, role, idx, status, now); err != nil {
			slog.Error("failed to upsert project_agent status",
				"project_id", projectID, "agent", agentName, "error", err)
		}
	}

	// Broadcast to WebSocket subscribers.
	h.publish(protocol.EventProjectAgentStatus, workspaceID, "system", "", map[string]any{
		"project_id": projectID,
		"agents":     req.Agents,
	})

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// parseAgentName splits "executor-2" into ("executor", 2) or ("orchestrator", 0).
func parseAgentName(name string) (string, int) {
	// Try to split on last '-' for indexed agents.
	for i := len(name) - 1; i >= 0; i-- {
		if name[i] == '-' {
			numStr := name[i+1:]
			num := 0
			valid := len(numStr) > 0
			for _, c := range numStr {
				if c < '0' || c > '9' {
					valid = false
					break
				}
				num = num*10 + int(c-'0')
			}
			if valid {
				return name[:i], num
			}
		}
	}
	return name, 0
}
