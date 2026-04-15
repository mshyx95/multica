package daemon

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// GPUStatus represents the status of a single GPU.
type GPUStatus struct {
	GPUIndex       int    `json:"gpu_index"`
	GPUName        string `json:"gpu_name"`
	UtilizationPct int    `json:"utilization_pct"`
	MemoryUsedMB   int    `json:"memory_used_mb"`
	MemoryTotalMB  int    `json:"memory_total_mb"`
	TemperatureC   int    `json:"temperature_c"`
	PowerDrawW     int    `json:"power_draw_w"`
	ProcessInfo    any    `json:"process_info,omitempty"`
}

const gpuPollInterval = 30 * time.Second

// gpuLoop periodically collects GPU status and reports to the server.
func (d *Daemon) gpuLoop(ctx context.Context) {
	if _, err := exec.LookPath("nvidia-smi"); err != nil {
		d.logger.Info("nvidia-smi not found, GPU monitoring disabled")
		return
	}

	ticker := time.NewTicker(gpuPollInterval)
	defer ticker.Stop()

	d.collectAndReportGPU(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.collectAndReportGPU(ctx)
		}
	}
}

func (d *Daemon) collectAndReportGPU(ctx context.Context) {
	statuses, err := collectGPUStatus()
	if err != nil {
		d.logger.Debug("GPU collection failed", "error", err)
		return
	}
	if len(statuses) == 0 {
		return
	}

	d.mu.Lock()
	runtimeIDs := make([]string, 0, len(d.runtimeIndex))
	for id := range d.runtimeIndex {
		runtimeIDs = append(runtimeIDs, id)
	}
	d.mu.Unlock()

	for _, rid := range runtimeIDs {
		if err := d.client.ReportGPUStatus(ctx, rid, statuses); err != nil {
			d.logger.Debug("GPU status report failed", "runtime_id", rid, "error", err)
		}
	}
}

// collectGPUStatus runs nvidia-smi and parses the output.
func collectGPUStatus() ([]GPUStatus, error) {
	cmd := exec.Command("nvidia-smi",
		"--query-gpu=index,name,utilization.gpu,memory.used,memory.total,temperature.gpu,power.draw",
		"--format=csv,noheader,nounits",
	)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("nvidia-smi: %w", err)
	}

	var statuses []GPUStatus
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Split(line, ", ")
		if len(fields) < 7 {
			continue
		}

		idx, _ := strconv.Atoi(strings.TrimSpace(fields[0]))
		util, _ := strconv.Atoi(strings.TrimSpace(fields[2]))
		memUsed, _ := strconv.Atoi(strings.TrimSpace(fields[3]))
		memTotal, _ := strconv.Atoi(strings.TrimSpace(fields[4]))
		temp, _ := strconv.Atoi(strings.TrimSpace(fields[5]))
		power, _ := strconv.ParseFloat(strings.TrimSpace(fields[6]), 64)

		statuses = append(statuses, GPUStatus{
			GPUIndex:       idx,
			GPUName:        strings.TrimSpace(fields[1]),
			UtilizationPct: util,
			MemoryUsedMB:   memUsed,
			MemoryTotalMB:  memTotal,
			TemperatureC:   temp,
			PowerDrawW:     int(power),
		})
	}

	return statuses, nil
}
