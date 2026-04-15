"use client";

import { useState, useEffect, useCallback, useRef } from "react";
import { Cpu, Loader2 } from "lucide-react";
import { api } from "@multica/core/api";
import { useWSEvent } from "@multica/core/realtime";
import type { GPUStatus } from "@multica/core/types";

const REFRESH_INTERVAL_MS = 30_000;

function getUtilColor(pct: number) {
  if (pct > 80) return "bg-destructive/10 text-destructive";
  if (pct > 40) return "bg-warning/10 text-warning";
  return "bg-success/10 text-success";
}

function GPUCard({ gpu }: { gpu: GPUStatus }) {
  const memPct = gpu.memory_total_mb > 0
    ? (gpu.memory_used_mb / gpu.memory_total_mb) * 100
    : 0;

  return (
    <div className="rounded-lg border p-4 space-y-3">
      <div className="flex items-center justify-between">
        <div className="font-medium text-sm truncate">
          GPU {gpu.gpu_index}: {gpu.gpu_name}
        </div>
        <span
          className={`text-xs px-1.5 py-0.5 rounded shrink-0 ${getUtilColor(gpu.utilization_pct)}`}
        >
          {gpu.utilization_pct}%
        </span>
      </div>

      {/* Utilization bar */}
      <div>
        <div className="flex justify-between text-xs text-muted-foreground mb-1">
          <span>GPU Utilization</span>
          <span>{gpu.utilization_pct}%</span>
        </div>
        <div className="h-2 bg-muted rounded-full overflow-hidden">
          <div
            className="h-full bg-primary rounded-full transition-all"
            style={{ width: `${gpu.utilization_pct}%` }}
          />
        </div>
      </div>

      {/* Memory bar */}
      <div>
        <div className="flex justify-between text-xs text-muted-foreground mb-1">
          <span>Memory</span>
          <span>
            {(gpu.memory_used_mb / 1024).toFixed(1)} /{" "}
            {(gpu.memory_total_mb / 1024).toFixed(1)} GB
          </span>
        </div>
        <div className="h-2 bg-muted rounded-full overflow-hidden">
          <div
            className="h-full bg-blue-500 rounded-full transition-all"
            style={{ width: `${memPct}%` }}
          />
        </div>
      </div>

      {/* Temperature + Power */}
      <div className="flex gap-4 text-xs text-muted-foreground">
        <span>🌡 {gpu.temperature_c}°C</span>
        <span>⚡ {gpu.power_draw_w}W</span>
      </div>
    </div>
  );
}

export function GPUStatusPanel({ runtimeId }: { runtimeId: string }) {
  const [gpus, setGpus] = useState<GPUStatus[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(false);
  const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null);

  const fetchStatus = useCallback(async () => {
    try {
      const data = await api.getGPUStatus(runtimeId);
      setGpus(data);
      setError(false);
    } catch {
      setGpus([]);
      setError(true);
    } finally {
      setLoading(false);
    }
  }, [runtimeId]);

  // Initial fetch + polling
  useEffect(() => {
    setLoading(true);
    fetchStatus();

    intervalRef.current = setInterval(fetchStatus, REFRESH_INTERVAL_MS);
    return () => {
      if (intervalRef.current) clearInterval(intervalRef.current);
    };
  }, [fetchStatus]);

  // Refresh immediately on WebSocket event
  const handleWSEvent = useCallback(
    (payload: unknown) => {
      const p = payload as { runtime_id?: string };
      if (!p.runtime_id || p.runtime_id === runtimeId) {
        fetchStatus();
      }
    },
    [runtimeId, fetchStatus],
  );

  useWSEvent("gpu_status_updated", handleWSEvent);

  if (loading) {
    return (
      <div className="flex items-center gap-2 py-4 text-xs text-muted-foreground">
        <Loader2 className="h-4 w-4 animate-spin" />
        Loading GPU status…
      </div>
    );
  }

  if (error || gpus.length === 0) {
    return (
      <div className="flex flex-col items-center rounded-lg border border-dashed py-6">
        <Cpu className="h-5 w-5 text-muted-foreground/40" />
        <p className="mt-2 text-xs text-muted-foreground">
          No GPU data available
        </p>
      </div>
    );
  }

  return (
    <div className="grid grid-cols-1 xl:grid-cols-2 gap-3">
      {gpus.map((gpu) => (
        <GPUCard key={gpu.gpu_index} gpu={gpu} />
      ))}
    </div>
  );
}
