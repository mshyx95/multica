import type { ProjectV2Status, ProjectAgentStatus } from "@multica/core/types";

export const PROJECT_V2_STATUS_CONFIG: Record<ProjectV2Status, { label: string; color: string; bg: string }> = {
  draft: { label: "Draft", color: "text-muted-foreground", bg: "bg-muted-foreground/20" },
  planning: { label: "Planning", color: "text-blue-600 dark:text-blue-400", bg: "bg-blue-500/20" },
  executing: { label: "Executing", color: "text-amber-600 dark:text-amber-400", bg: "bg-amber-500/20" },
  paused: { label: "Paused", color: "text-orange-600 dark:text-orange-400", bg: "bg-orange-500/20" },
  completed: { label: "Completed", color: "text-green-600 dark:text-green-400", bg: "bg-green-500/20" },
  failed: { label: "Failed", color: "text-destructive", bg: "bg-destructive/20" },
};

export const AGENT_STATUS_CONFIG: Record<ProjectAgentStatus, { label: string; dot: string }> = {
  idle: { label: "Idle", dot: "bg-muted-foreground" },
  busy: { label: "Busy", dot: "bg-green-500" },
  blocked: { label: "Blocked", dot: "bg-amber-500" },
  error: { label: "Error", dot: "bg-destructive" },
  stopped: { label: "Stopped", dot: "bg-muted-foreground/40" },
};

export const COPILOT_MODELS = [
  { id: "claude-opus-4.6-1m", name: "Claude Opus 4.6 (1M)(Internal)", cost: "6x", isDefault: true },
  { id: "claude-sonnet-4.6", name: "Claude Sonnet 4.6", cost: "1x" },
  { id: "claude-sonnet-4.5", name: "Claude Sonnet 4.5", cost: "1x" },
  { id: "claude-haiku-4.5", name: "Claude Haiku 4.5", cost: "0.33x" },
  { id: "claude-opus-4.6", name: "Claude Opus 4.6", cost: "3x" },
  { id: "claude-opus-4.5", name: "Claude Opus 4.5", cost: "3x" },
  { id: "claude-sonnet-4", name: "Claude Sonnet 4", cost: "1x" },
  { id: "goldeneye", name: "Goldeneye (Internal)", cost: "1x" },
  { id: "gpt-5.4", name: "GPT-5.4", cost: "1x" },
  { id: "gpt-5.3-codex", name: "GPT-5.3-Codex", cost: "1x" },
  { id: "gpt-5.2-codex", name: "GPT-5.2-Codex", cost: "1x" },
  { id: "gpt-5.2", name: "GPT-5.2", cost: "1x" },
  { id: "gpt-5.1", name: "GPT-5.1", cost: "1x" },
  { id: "gpt-5.4-mini", name: "GPT-5.4 mini", cost: "0.33x" },
  { id: "gpt-5-mini", name: "GPT-5 mini", cost: "0x" },
  { id: "gpt-4.1", name: "GPT-4.1", cost: "0.33x" },
];

export const EFFORT_LEVELS = [
  { id: "low", label: "Low", description: "1 executor, faster" },
  { id: "medium", label: "Medium", description: "2 executors" },
  { id: "high", label: "High", description: "3 executors" },
  { id: "xhigh", label: "Extra High", description: "4 executors, thorough" },
] as const;
