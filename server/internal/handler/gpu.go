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
// GPU Status types
// ---------------------------------------------------------------------------

type gpuStatusEntry struct {
	GPUIndex       int     `json:"gpu_index"`
	GPUName        string  `json:"gpu_name"`
	UtilizationPct float64 `json:"utilization_pct"`
	MemoryUsedMB   float64 `json:"memory_used_mb"`
	MemoryTotalMB  float64 `json:"memory_total_mb"`
	TemperatureC   float64 `json:"temperature_c"`
	PowerDrawW     float64 `json:"power_draw_w"`
	ProcessInfo    *string `json:"process_info"`
}

type gpuStatusResponse struct {
	GPUIndex       int     `json:"gpu_index"`
	GPUName        string  `json:"gpu_name"`
	UtilizationPct float64 `json:"utilization_pct"`
	MemoryUsedMB   float64 `json:"memory_used_mb"`
	MemoryTotalMB  float64 `json:"memory_total_mb"`
	TemperatureC   float64 `json:"temperature_c"`
	PowerDrawW     float64 `json:"power_draw_w"`
	ProcessInfo    *string `json:"process_info"`
	UpdatedAt      string  `json:"updated_at"`
}

// ---------------------------------------------------------------------------
// ReportGPUStatus — POST /api/daemon/runtimes/{runtimeId}/gpu-status
// Called by the daemon to upsert GPU status.
// ---------------------------------------------------------------------------

func (h *Handler) ReportGPUStatus(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")
	if runtimeID == "" {
		writeError(w, http.StatusBadRequest, "runtimeId is required")
		return
	}

	rt, ok := h.requireDaemonRuntimeAccess(w, r, runtimeID)
	if !ok {
		return
	}

	var req struct {
		GPUs []gpuStatusEntry `json:"gpus"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	const upsertSQL = `
		INSERT INTO gpu_status (runtime_id, gpu_index, gpu_name, utilization_pct, memory_used_mb, memory_total_mb, temperature_c, power_draw_w, process_info, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NOW())
		ON CONFLICT (runtime_id, gpu_index) DO UPDATE SET
			gpu_name = EXCLUDED.gpu_name,
			utilization_pct = EXCLUDED.utilization_pct,
			memory_used_mb = EXCLUDED.memory_used_mb,
			memory_total_mb = EXCLUDED.memory_total_mb,
			temperature_c = EXCLUDED.temperature_c,
			power_draw_w = EXCLUDED.power_draw_w,
			process_info = EXCLUDED.process_info,
			updated_at = NOW()`

	for _, gpu := range req.GPUs {
		var processInfo *string
		if gpu.ProcessInfo != nil {
			processInfo = gpu.ProcessInfo
		}
		_, err := h.DB.Exec(r.Context(), upsertSQL,
			runtimeID, gpu.GPUIndex, gpu.GPUName,
			gpu.UtilizationPct, gpu.MemoryUsedMB, gpu.MemoryTotalMB,
			gpu.TemperatureC, gpu.PowerDrawW, processInfo,
		)
		if err != nil {
			slog.Error("failed to upsert gpu_status", "runtime_id", runtimeID, "gpu_index", gpu.GPUIndex, "error", err)
		}
	}

	workspaceID := uuidToString(rt.WorkspaceID)
	h.publish(protocol.EventGPUStatusUpdated, workspaceID, "system", "", map[string]any{
		"runtime_id": runtimeID,
		"gpus":       req.GPUs,
	})

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ---------------------------------------------------------------------------
// GetGPUStatus — GET /api/runtimes/{runtimeId}/gpu-status
// Returns current GPU status for a runtime.
// ---------------------------------------------------------------------------

func (h *Handler) GetGPUStatus(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")

	rt, err := h.Queries.GetAgentRuntime(r.Context(), parseUUID(runtimeID))
	if err != nil {
		writeError(w, http.StatusNotFound, "runtime not found")
		return
	}

	if _, ok := h.requireWorkspaceMember(w, r, uuidToString(rt.WorkspaceID), "runtime not found"); !ok {
		return
	}

	const selectSQL = `
		SELECT gpu_index, gpu_name, utilization_pct, memory_used_mb, memory_total_mb,
		       temperature_c, power_draw_w, process_info, updated_at
		FROM gpu_status WHERE runtime_id = $1 ORDER BY gpu_index`

	rows, err := h.DB.Query(r.Context(), selectSQL, runtimeID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to query gpu status")
		return
	}
	defer rows.Close()

	var gpus []gpuStatusResponse
	for rows.Next() {
		var g gpuStatusResponse
		var updatedAt time.Time
		if err := rows.Scan(
			&g.GPUIndex, &g.GPUName, &g.UtilizationPct,
			&g.MemoryUsedMB, &g.MemoryTotalMB, &g.TemperatureC,
			&g.PowerDrawW, &g.ProcessInfo, &updatedAt,
		); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to scan gpu status")
			return
		}
		g.UpdatedAt = updatedAt.Format(time.RFC3339)
		gpus = append(gpus, g)
	}

	if gpus == nil {
		gpus = []gpuStatusResponse{}
	}

	writeJSON(w, http.StatusOK, gpus)
}
