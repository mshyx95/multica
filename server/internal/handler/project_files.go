package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

// ---------------------------------------------------------------------------
// Public endpoints — serve files from the database (pushed by daemon)
// ---------------------------------------------------------------------------

// ListProjectFiles returns a flat list of files for a project.
//
// GET /api/v2/projects/{projectId}/files
func (h *Handler) ListProjectFiles(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectId")
	workspaceID := resolveWorkspaceID(r)
	if !h.projectV2Exists(r, projectID, workspaceID) {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}

	const q = `SELECT path, name, size, is_dir, mod_time, category
		FROM project_file WHERE project_id = $1 ORDER BY path`
	rows, err := h.DB.Query(r.Context(), q, projectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()

	type fileEntry struct {
		Path     string `json:"path"`
		Name     string `json:"name"`
		Size     int64  `json:"size"`
		IsDir    bool   `json:"is_dir"`
		ModTime  string `json:"mod_time"`
		Category string `json:"category"`
	}

	files := []fileEntry{}
	for rows.Next() {
		var f fileEntry
		var modTime time.Time
		if err := rows.Scan(&f.Path, &f.Name, &f.Size, &f.IsDir, &modTime, &f.Category); err != nil {
			continue
		}
		f.ModTime = modTime.Format(time.RFC3339)
		files = append(files, f)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(files)
}

// ReadProjectFile returns the content of a specific file.
//
// GET /api/v2/projects/{projectId}/files/*
func (h *Handler) ReadProjectFile(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectId")
	workspaceID := resolveWorkspaceID(r)
	if !h.projectV2Exists(r, projectID, workspaceID) {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}

	filePath := chi.URLParam(r, "*")
	if filePath == "" {
		writeError(w, http.StatusBadRequest, "file path is required")
		return
	}

	const q = `SELECT content FROM project_file WHERE project_id = $1 AND path = $2`
	var content *string
	if err := h.DB.QueryRow(r.Context(), q, projectID, filePath).Scan(&content); err != nil {
		writeError(w, http.StatusNotFound, "file not found")
		return
	}

	c := ""
	if content != nil {
		c = *content
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"path":    filePath,
		"content": c,
	})
}

// WriteProjectFile is a no-op placeholder — writes go through the daemon.
//
// PUT /api/v2/projects/{projectId}/files/*
func (h *Handler) WriteProjectFile(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotImplemented, "file writes go through the daemon")
}

// ---------------------------------------------------------------------------
// Daemon endpoint — receive file sync from daemon
// ---------------------------------------------------------------------------

type syncFileEntry struct {
	Path     string  `json:"path"`
	Name     string  `json:"name"`
	Size     int64   `json:"size"`
	IsDir    bool    `json:"is_dir"`
	ModTime  string  `json:"mod_time"`
	Category string  `json:"category"`
	Content  *string `json:"content,omitempty"`
}

// DaemonSyncProjectFiles receives file metadata+content pushed by the daemon.
//
// POST /api/daemon/projects/{projectId}/files
func (h *Handler) DaemonSyncProjectFiles(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectId")

	var req struct {
		Files []syncFileEntry `json:"files"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	ctx := r.Context()

	// Delete files that no longer exist on the daemon side, then upsert current files.
	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "tx begin failed")
		return
	}
	defer tx.Rollback(ctx)

	// Delete all existing files for this project and re-insert (full sync).
	if _, err := tx.Exec(ctx, `DELETE FROM project_file WHERE project_id = $1`, projectID); err != nil {
		writeError(w, http.StatusInternalServerError, "delete failed")
		return
	}

	const insertSQL = `INSERT INTO project_file (project_id, path, name, size, is_dir, mod_time, category, content, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW())`

	for _, f := range req.Files {
		modTime, err := time.Parse(time.RFC3339, f.ModTime)
		if err != nil {
			modTime = time.Now()
		}
		if _, err := tx.Exec(ctx, insertSQL,
			projectID, f.Path, f.Name, f.Size, f.IsDir, modTime, f.Category, f.Content,
		); err != nil {
			writeError(w, http.StatusInternalServerError, "insert failed")
			return
		}
	}

	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "tx commit failed")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"synced": len(req.Files),
	})
}
