export interface GPUStatus {
  gpu_index: number;
  gpu_name: string;
  utilization_pct: number;
  memory_used_mb: number;
  memory_total_mb: number;
  temperature_c: number;
  power_draw_w: number;
  process_info?: Record<string, unknown>;
  updated_at: string;
}
