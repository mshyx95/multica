package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// ---------------------------------------------------------------------------
// Project V2 response types
// ---------------------------------------------------------------------------

type projectV2Response struct {
	ID          string  `json:"id"`
	WorkspaceID string  `json:"workspace_id"`
	RuntimeID   *string `json:"runtime_id"`
	Name        string  `json:"name"`
	Description *string `json:"description"`
	Goals       *string `json:"goals"`
	Skills      *string `json:"skills"`
	AgentRules  *string `json:"agent_rules"`
	Status      string  `json:"status"`
	TRDContent  *string `json:"trd_content"`
	PlanContent *string `json:"plan_content"`
	Config      *string `json:"config"`
	CreatedBy   *string `json:"created_by"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
}

type projectAgentResponse struct {
	ID            string  `json:"id"`
	ProjectID     string  `json:"project_id"`
	Role          string  `json:"role"`
	AgentIndex    int     `json:"agent_index"`
	TmuxWindow    *string `json:"tmux_window"`
	PID           *int    `json:"pid"`
	Status        string  `json:"status"`
	Model         *string `json:"model"`
	CurrentTask   *string `json:"current_task"`
	GPUAssignment *string `json:"gpu_assignment"`
	WorktreePath  *string `json:"worktree_path"`
	BranchName    *string `json:"branch_name"`
	TokenUsage    *string `json:"token_usage"`
	StartedAt     *string `json:"started_at"`
	LastHeartbeat *string `json:"last_heartbeat"`
	CreatedAt     string  `json:"created_at"`
}

type projectMessageResponse struct {
	ID        string  `json:"id"`
	ProjectID string  `json:"project_id"`
	Role      string  `json:"role"`
	AgentID   *string `json:"agent_id"`
	Content   string  `json:"content"`
	Phase     *string `json:"phase"`
	Seq       int     `json:"seq"`
	CreatedAt string  `json:"created_at"`
}

type subtaskResponse struct {
	ID            string  `json:"id"`
	ProjectID     string  `json:"project_id"`
	Title         string  `json:"title"`
	Description   *string `json:"description"`
	AssignedTo    *string `json:"assigned_to"`
	Status        string  `json:"status"`
	FileOwnership *string `json:"file_ownership"`
	DependsOn     *string `json:"depends_on"`
	Result        *string `json:"result"`
	CreatedAt     string  `json:"created_at"`
	UpdatedAt     string  `json:"updated_at"`
}

// ---------------------------------------------------------------------------
// CreateProjectV2 — POST /api/projects-v2
// ---------------------------------------------------------------------------

func (h *Handler) CreateProjectV2(w http.ResponseWriter, r *http.Request) {
	workspaceID := resolveWorkspaceID(r)
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	var req struct {
		Name        string  `json:"name"`
		Description *string `json:"description"`
		Goals       *string `json:"goals"`
		Skills      *string `json:"skills"`
		AgentRules  *string `json:"agent_rules"`
		Config      *string `json:"config"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	// Default JSONB fields to avoid NULL constraint violations
	skills := derefOr(req.Skills, "[]")
	agentRules := derefOr(req.AgentRules, "{}")
	config := derefOr(req.Config, "{}")
	description := derefOr(req.Description, "")
	goals := derefOr(req.Goals, "")

	const insertSQL = `
		INSERT INTO project_v2 (workspace_id, name, description, goals, skills, agent_rules, status, config, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, 'draft', $7, $8)
		RETURNING id, workspace_id, runtime_id, name, description, goals, skills, agent_rules, status,
		          trd_content, plan_content, config, created_by, created_at, updated_at`

	row := h.DB.QueryRow(r.Context(), insertSQL,
		workspaceID, req.Name, description, goals, skills, agentRules, config, userID,
	)

	p, err := scanProjectV2(row)
	if err != nil {
		slog.Error("failed to create project_v2", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create project")
		return
	}

	h.publish(protocol.EventProjectV2Created, workspaceID, "member", userID, p)

	writeJSON(w, http.StatusCreated, p)
}

// ---------------------------------------------------------------------------
// ListProjectsV2 — GET /api/projects-v2
// ---------------------------------------------------------------------------

func (h *Handler) ListProjectsV2(w http.ResponseWriter, r *http.Request) {
	workspaceID := resolveWorkspaceID(r)

	const selectSQL = `
		SELECT id, workspace_id, runtime_id, name, description, goals, skills, agent_rules, status,
		       trd_content, plan_content, config, created_by, created_at, updated_at
		FROM project_v2 WHERE workspace_id = $1 ORDER BY created_at DESC`

	rows, err := h.DB.Query(r.Context(), selectSQL, workspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list projects")
		return
	}
	defer rows.Close()

	projects := []projectV2Response{}
	for rows.Next() {
		p, err := scanProjectV2FromRows(rows)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to scan project")
			return
		}
		projects = append(projects, p)
	}

	writeJSON(w, http.StatusOK, projects)
}

// ---------------------------------------------------------------------------
// GetProjectV2 — GET /api/projects-v2/{projectId}
// ---------------------------------------------------------------------------

func (h *Handler) GetProjectV2(w http.ResponseWriter, r *http.Request) {
	workspaceID := resolveWorkspaceID(r)
	projectID := chi.URLParam(r, "projectId")

	const selectSQL = `
		SELECT id, workspace_id, runtime_id, name, description, goals, skills, agent_rules, status,
		       trd_content, plan_content, config, created_by, created_at, updated_at
		FROM project_v2 WHERE id = $1 AND workspace_id = $2`

	row := h.DB.QueryRow(r.Context(), selectSQL, projectID, workspaceID)
	p, err := scanProjectV2(row)
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}

	writeJSON(w, http.StatusOK, p)
}

// ---------------------------------------------------------------------------
// UpdateProjectV2 — PATCH /api/projects-v2/{projectId}
// ---------------------------------------------------------------------------

func (h *Handler) UpdateProjectV2(w http.ResponseWriter, r *http.Request) {
	workspaceID := resolveWorkspaceID(r)
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	projectID := chi.URLParam(r, "projectId")

	var req struct {
		Name        *string `json:"name"`
		Description *string `json:"description"`
		Goals       *string `json:"goals"`
		Skills      *string `json:"skills"`
		AgentRules  *string `json:"agent_rules"`
		Status      *string `json:"status"`
		TRDContent  *string `json:"trd_content"`
		PlanContent *string `json:"plan_content"`
		Config      *string `json:"config"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	const updateSQL = `
		UPDATE project_v2 SET
			name         = COALESCE($3, name),
			description  = COALESCE($4, description),
			goals        = COALESCE($5, goals),
			skills       = COALESCE($6, skills),
			agent_rules  = COALESCE($7, agent_rules),
			status       = COALESCE($8, status),
			trd_content  = COALESCE($9, trd_content),
			plan_content = COALESCE($10, plan_content),
			config       = COALESCE($11, config),
			updated_at   = NOW()
		WHERE id = $1 AND workspace_id = $2
		RETURNING id, workspace_id, runtime_id, name, description, goals, skills, agent_rules, status,
		          trd_content, plan_content, config, created_by, created_at, updated_at`

	row := h.DB.QueryRow(r.Context(), updateSQL,
		projectID, workspaceID,
		req.Name, req.Description, req.Goals, req.Skills, req.AgentRules,
		req.Status, req.TRDContent, req.PlanContent, req.Config,
	)

	p, err := scanProjectV2(row)
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}

	h.publish(protocol.EventProjectV2Updated, workspaceID, "member", userID, p)

	writeJSON(w, http.StatusOK, p)
}

// ---------------------------------------------------------------------------
// DeployProjectV2 — POST /api/projects-v2/{projectId}/deploy
// ---------------------------------------------------------------------------

func (h *Handler) DeployProjectV2(w http.ResponseWriter, r *http.Request) {
	workspaceID := resolveWorkspaceID(r)
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	projectID := chi.URLParam(r, "projectId")

	var req struct {
		RuntimeID string `json:"runtime_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.RuntimeID == "" {
		writeError(w, http.StatusBadRequest, "runtime_id is required")
		return
	}

	const deploySQL = `
		UPDATE project_v2 SET runtime_id = $3, status = 'planning', updated_at = NOW()
		WHERE id = $1 AND workspace_id = $2
		RETURNING id, workspace_id, runtime_id, name, description, goals, skills, agent_rules, status,
		          trd_content, plan_content, config, created_by, created_at, updated_at`

	row := h.DB.QueryRow(r.Context(), deploySQL, projectID, workspaceID, req.RuntimeID)
	p, err := scanProjectV2(row)
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}

	h.publish(protocol.EventProjectV2Updated, workspaceID, "member", userID, p)

	writeJSON(w, http.StatusOK, p)
}

// ---------------------------------------------------------------------------
// ListProjectAgents — GET /api/projects-v2/{projectId}/agents
// ---------------------------------------------------------------------------

func (h *Handler) ListProjectAgents(w http.ResponseWriter, r *http.Request) {
	workspaceID := resolveWorkspaceID(r)
	projectID := chi.URLParam(r, "projectId")

	// Verify project belongs to workspace.
	if !h.projectV2Exists(r, projectID, workspaceID) {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}

	const selectSQL = `
		SELECT id, project_id, role, agent_index, tmux_window, pid, status, model,
		       current_task, gpu_assignment, worktree_path, branch_name, token_usage,
		       started_at, last_heartbeat, created_at
		FROM project_agent WHERE project_id = $1 ORDER BY agent_index`

	rows, err := h.DB.Query(r.Context(), selectSQL, projectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list agents")
		return
	}
	defer rows.Close()

	agents := []projectAgentResponse{}
	for rows.Next() {
		var a projectAgentResponse
		var startedAt, lastHB, createdAt *time.Time
		var pid *int
		if err := rows.Scan(
			&a.ID, &a.ProjectID, &a.Role, &a.AgentIndex,
			&a.TmuxWindow, &pid, &a.Status, &a.Model,
			&a.CurrentTask, &a.GPUAssignment, &a.WorktreePath, &a.BranchName,
			&a.TokenUsage, &startedAt, &lastHB, &createdAt,
		); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to scan agent")
			return
		}
		if pid != nil {
			a.PID = pid
		}
		if startedAt != nil {
			s := startedAt.Format(time.RFC3339)
			a.StartedAt = &s
		}
		if lastHB != nil {
			s := lastHB.Format(time.RFC3339)
			a.LastHeartbeat = &s
		}
		if createdAt != nil {
			a.CreatedAt = createdAt.Format(time.RFC3339)
		}
		agents = append(agents, a)
	}

	writeJSON(w, http.StatusOK, agents)
}

// ---------------------------------------------------------------------------
// GetProjectAgentLogs — GET /api/projects-v2/{projectId}/agents/{agentId}/logs
// Returns messages from project_message where agent_id matches.
// ---------------------------------------------------------------------------

func (h *Handler) GetProjectAgentLogs(w http.ResponseWriter, r *http.Request) {
	workspaceID := resolveWorkspaceID(r)
	projectID := chi.URLParam(r, "projectId")
	agentID := chi.URLParam(r, "agentId")

	if !h.projectV2Exists(r, projectID, workspaceID) {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}

	const selectSQL = `
		SELECT id, project_id, role, agent_id, content, phase, seq, created_at
		FROM project_message WHERE project_id = $1 AND agent_id = $2
		ORDER BY seq`

	rows, err := h.DB.Query(r.Context(), selectSQL, projectID, agentID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to query agent logs")
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
// ListProjectMessages — GET /api/projects-v2/{projectId}/messages
// Supports ?since_seq=N for incremental fetch.
// ---------------------------------------------------------------------------

func (h *Handler) ListProjectMessages(w http.ResponseWriter, r *http.Request) {
	workspaceID := resolveWorkspaceID(r)
	projectID := chi.URLParam(r, "projectId")

	if !h.projectV2Exists(r, projectID, workspaceID) {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}

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
// SendProjectMessage — POST /api/projects-v2/{projectId}/messages
// User sends message to Planner.
// ---------------------------------------------------------------------------

func (h *Handler) SendProjectMessage(w http.ResponseWriter, r *http.Request) {
	workspaceID := resolveWorkspaceID(r)
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	projectID := chi.URLParam(r, "projectId")

	if !h.projectV2Exists(r, projectID, workspaceID) {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}

	var req struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Content == "" {
		writeError(w, http.StatusBadRequest, "content is required")
		return
	}

	// Determine phase from project status.
	phase := h.projectV2Phase(r, projectID)

	const insertSQL = `
		INSERT INTO project_message (project_id, role, content, phase, seq)
		VALUES ($1, 'user', $2, $3, COALESCE((SELECT MAX(seq) FROM project_message WHERE project_id = $1), 0) + 1)
		RETURNING id, project_id, role, agent_id, content, phase, seq, created_at`

	row := h.DB.QueryRow(r.Context(), insertSQL, projectID, req.Content, phase)

	var m projectMessageResponse
	var createdAt time.Time
	if err := row.Scan(&m.ID, &m.ProjectID, &m.Role, &m.AgentID, &m.Content, &m.Phase, &m.Seq, &createdAt); err != nil {
		slog.Error("failed to insert project message", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to send message")
		return
	}
	m.CreatedAt = createdAt.Format(time.RFC3339)

	h.publish(protocol.EventProjectMessage, workspaceID, "member", userID, m)

	writeJSON(w, http.StatusCreated, m)
}

// ---------------------------------------------------------------------------
// ListSubtasks — GET /api/projects-v2/{projectId}/subtasks
// ---------------------------------------------------------------------------

func (h *Handler) ListSubtasks(w http.ResponseWriter, r *http.Request) {
	workspaceID := resolveWorkspaceID(r)
	projectID := chi.URLParam(r, "projectId")

	if !h.projectV2Exists(r, projectID, workspaceID) {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}

	const selectSQL = `
		SELECT id, project_id, title, description, assigned_to, status,
		       file_ownership, depends_on, result, created_at, updated_at
		FROM subtask WHERE project_id = $1 ORDER BY created_at`

	rows, err := h.DB.Query(r.Context(), selectSQL, projectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list subtasks")
		return
	}
	defer rows.Close()

	subtasks := []subtaskResponse{}
	for rows.Next() {
		var s subtaskResponse
		var createdAt, updatedAt time.Time
		if err := rows.Scan(
			&s.ID, &s.ProjectID, &s.Title, &s.Description, &s.AssignedTo,
			&s.Status, &s.FileOwnership, &s.DependsOn, &s.Result,
			&createdAt, &updatedAt,
		); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to scan subtask")
			return
		}
		s.CreatedAt = createdAt.Format(time.RFC3339)
		s.UpdatedAt = updatedAt.Format(time.RFC3339)
		subtasks = append(subtasks, s)
	}

	writeJSON(w, http.StatusOK, subtasks)
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// projectV2Exists checks whether the project belongs to the workspace.
func (h *Handler) projectV2Exists(r *http.Request, projectID, workspaceID string) bool {
	const q = `SELECT 1 FROM project_v2 WHERE id = $1 AND workspace_id = $2`
	var dummy int
	err := h.DB.QueryRow(r.Context(), q, projectID, workspaceID).Scan(&dummy)
	return err == nil
}

// projectV2Phase returns the current phase based on project status.
func (h *Handler) projectV2Phase(r *http.Request, projectID string) string {
	const q = `SELECT status FROM project_v2 WHERE id = $1`
	var status string
	if err := h.DB.QueryRow(r.Context(), q, projectID).Scan(&status); err != nil {
		return "planning"
	}
	switch status {
	case "executing":
		return "executing"
	default:
		return "planning"
	}
}

// scanProjectV2 scans a single project_v2 row from QueryRow.
func scanProjectV2(row interface{ Scan(dest ...any) error }) (projectV2Response, error) {
	var p projectV2Response
	var createdAt, updatedAt time.Time
	err := row.Scan(
		&p.ID, &p.WorkspaceID, &p.RuntimeID, &p.Name, &p.Description,
		&p.Goals, &p.Skills, &p.AgentRules, &p.Status,
		&p.TRDContent, &p.PlanContent, &p.Config, &p.CreatedBy,
		&createdAt, &updatedAt,
	)
	if err != nil {
		return p, err
	}
	p.CreatedAt = createdAt.Format(time.RFC3339)
	p.UpdatedAt = updatedAt.Format(time.RFC3339)
	return p, nil
}

// scanProjectV2FromRows scans a project_v2 from pgx.Rows (same field order).
func scanProjectV2FromRows(rows interface{ Scan(dest ...any) error }) (projectV2Response, error) {
	return scanProjectV2(rows)
}

// scanProjectMessage scans a project_message row.
func scanProjectMessage(rows interface{ Scan(dest ...any) error }) (projectMessageResponse, error) {
	var m projectMessageResponse
	var createdAt time.Time
	err := rows.Scan(&m.ID, &m.ProjectID, &m.Role, &m.AgentID, &m.Content, &m.Phase, &m.Seq, &createdAt)
	if err != nil {
		return m, err
	}
	m.CreatedAt = createdAt.Format(time.RFC3339)
	return m, nil
}

func derefOr(s *string, fallback string) string {
	if s == nil {
		return fallback
	}
	return *s
}
