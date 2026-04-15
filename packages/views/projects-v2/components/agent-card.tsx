"use client";

import type { ProjectAgentStatus } from "@multica/core/types";

interface AgentCardProps {
  role: string;
  label: string;
  model: string;
  status: ProjectAgentStatus | "unknown";
  currentTask?: string;
  tokenUsage?: Record<string, unknown>;
  isSelected: boolean;
  onClick: () => void;
}

const STATUS_DOT: Record<string, string> = {
  busy: "bg-green-500",
  idle: "bg-muted-foreground",
  blocked: "bg-amber-500",
  error: "bg-destructive",
  stopped: "bg-muted-foreground/40",
  unknown: "bg-muted-foreground/30",
};

export function AgentCard({
  role: _role,
  label,
  model,
  status,
  currentTask,
  tokenUsage,
  isSelected,
  onClick,
}: AgentCardProps) {
  const dotCls = STATUS_DOT[status] ?? STATUS_DOT.unknown;

  return (
    <button
      onClick={onClick}
      className={`flex w-full items-center gap-3 rounded-lg border p-3 text-left text-sm transition-colors ${
        isSelected
          ? "border-primary bg-primary/5"
          : "border-border hover:bg-muted"
      }`}
    >
      <span className={`h-2.5 w-2.5 shrink-0 rounded-full ${dotCls}`} />
      <div className="min-w-0 flex-1">
        <div className="flex items-center justify-between gap-2">
          <span className="font-medium capitalize">{label}</span>
          <span className="text-[10px] text-muted-foreground shrink-0">{model}</span>
        </div>
        {currentTask && (
          <div className="text-xs text-muted-foreground/70 truncate mt-0.5">
            {currentTask}
          </div>
        )}
        {tokenUsage && Object.keys(tokenUsage).length > 0 && (
          <div className="flex gap-3 text-[10px] text-muted-foreground/70 mt-0.5">
            {tokenUsage.input_tokens != null && (
              <span>In: {Number(tokenUsage.input_tokens).toLocaleString()}</span>
            )}
            {tokenUsage.output_tokens != null && (
              <span>Out: {Number(tokenUsage.output_tokens).toLocaleString()}</span>
            )}
          </div>
        )}
      </div>
    </button>
  );
}
