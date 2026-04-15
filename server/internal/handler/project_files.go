package handler

import (
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/go-chi/chi/v5"
)

// daemonHealthURL is the default daemon health server address.
// When server and daemon are co-located this is localhost; when split the
// server will resolve the daemon's IP from the runtime record.
const daemonHealthURL = "http://localhost:19514"

// getDaemonURL returns the base URL for the daemon health server that owns the
// given project.  For now every project is served by the local daemon; this is
// the hook point for multi-runtime support.
func (h *Handler) getDaemonURL(r *http.Request, projectID, workspaceID string) string {
	// TODO: look up runtime_id from project_v2, then resolve the daemon's
	// external IP + health port.  For now fall back to localhost.
	return daemonHealthURL
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

	daemonURL := h.getDaemonURL(r, projectID, workspaceID)
	target := fmt.Sprintf("%s/files/%s/list", daemonURL, url.PathEscape(projectID))
	proxyDaemonGET(w, target)
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

	filePath := chi.URLParam(r, "*")
	if filePath == "" {
		writeError(w, http.StatusBadRequest, "file path is required")
		return
	}

	daemonURL := h.getDaemonURL(r, projectID, workspaceID)
	target := fmt.Sprintf("%s/files/%s/read/%s", daemonURL, url.PathEscape(projectID), filePath)
	proxyDaemonGET(w, target)
}

// WriteProjectFile saves content to a file in the project directory.
//
// PUT /api/v2/projects/{projectId}/files/*
func (h *Handler) WriteProjectFile(w http.ResponseWriter, r *http.Request) {
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

	daemonURL := h.getDaemonURL(r, projectID, workspaceID)
	target := fmt.Sprintf("%s/files/%s/write/%s", daemonURL, url.PathEscape(projectID), filePath)

	req, err := http.NewRequestWithContext(r.Context(), http.MethodPut, target, r.Body)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create request")
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		writeError(w, http.StatusBadGateway, "daemon unreachable")
		return
	}
	defer resp.Body.Close()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

// proxyDaemonGET forwards a GET request to the daemon and copies the response
// back to the client.
func proxyDaemonGET(w http.ResponseWriter, target string) {
	resp, err := http.Get(target)
	if err != nil {
		writeError(w, http.StatusBadGateway, "daemon unreachable")
		return
	}
	defer resp.Body.Close()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}
