package handler

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

const projectsBaseDir = "/mnt2/yuxuanhu/multica/projects"

type fileEntry struct {
	Path     string `json:"path"`
	Name     string `json:"name"`
	Size     int64  `json:"size"`
	IsDir    bool   `json:"is_dir"`
	ModTime  string `json:"mod_time"`
	Category string `json:"category"`
}

// ListProjectFiles returns a flat list of files in the project output directory.
//
// GET /api/v2/projects/{projectId}/files
func (h *Handler) ListProjectFiles(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectId")
	workspaceID := resolveWorkspaceID(r)
	if !h.projectV2Exists(r, projectID, workspaceID) {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}

	baseDir := filepath.Join(projectsBaseDir, projectID)
	info, err := os.Stat(baseDir)
	if err != nil || !info.IsDir() {
		writeJSON(w, http.StatusOK, []fileEntry{})
		return
	}

	var files []fileEntry
	filepath.Walk(baseDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(baseDir, path)
		if rel == "." {
			return nil
		}

		category := categorizeFile(rel)

		files = append(files, fileEntry{
			Path:    rel,
			Name:    info.Name(),
			Size:    info.Size(),
			IsDir:   info.IsDir(),
			ModTime: info.ModTime().Format(time.RFC3339),
			Category: category,
		})
		return nil
	})

	if files == nil {
		files = []fileEntry{}
	}
	writeJSON(w, http.StatusOK, files)
}

// ReadProjectFile returns the content of a specific file in the project directory.
//
// GET /api/v2/projects/{projectId}/files/*
func (h *Handler) ReadProjectFile(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectId")
	workspaceID := resolveWorkspaceID(r)
	if !h.projectV2Exists(r, projectID, workspaceID) {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}

	// Extract the file path from the wildcard portion of the URL.
	filePath := chi.URLParam(r, "*")
	if filePath == "" {
		writeError(w, http.StatusBadRequest, "file path is required")
		return
	}

	baseDir := filepath.Join(projectsBaseDir, projectID)
	fullPath := filepath.Join(baseDir, filePath)

	// Security: prevent path traversal.
	absBase, err := filepath.Abs(baseDir)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	absPath, err := filepath.Abs(fullPath)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid path")
		return
	}
	if !strings.HasPrefix(absPath, absBase+string(os.PathSeparator)) && absPath != absBase {
		writeError(w, http.StatusForbidden, "path traversal not allowed")
		return
	}

	data, err := os.ReadFile(fullPath)
	if err != nil {
		writeError(w, http.StatusNotFound, "file not found")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"path":    filePath,
		"content": string(data),
	})
}

// categorizeFile determines a file category based on its relative path.
func categorizeFile(rel string) string {
	base := filepath.Base(rel)
	switch {
	case strings.Contains(rel, "memories/session/"):
		return "plan"
	case strings.HasPrefix(base, "task-"):
		return "task"
	case strings.HasPrefix(base, "report-"):
		return "report"
	case strings.HasPrefix(base, "state-") || rel == "runtime/agents-status.json":
		return "state"
	case strings.HasSuffix(rel, "init.md"):
		return "init"
	default:
		return "other"
	}
}
