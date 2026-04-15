"use client";

import type { ProjectAgent } from "@multica/core/types";
import { AGENT_STATUS_CONFIG } from "../config";

export function AgentCard({ agent }: { agent: ProjectAgent }) {
  const statusCfg = AGENT_STATUS_CONFIG[agent.status];
  const label = agent.agent_index > 0
    ? `${agent.role}-${agent.agent_index}`
    : agent.role;

  return (
    <div className="rounded-lg border p-3 space-y-2">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <span className={`h-2 w-2 rounded-full ${statusCfg.dot}`} />
          <span className="font-medium text-sm capitalize">{label}</span>
        </div>
        <span className="text-xs text-muted-foreground">{agent.model || "default"}</span>
      </div>
      {agent.current_task && (
        <p className="text-xs text-muted-foreground truncate">{agent.current_task}</p>
      )}
      {agent.token_usage && Object.keys(agent.token_usage).length > 0 && (
        <div className="flex gap-3 text-[10px] text-muted-foreground/70">
          {agent.token_usage.input_tokens != null && (
            <span>In: {Number(agent.token_usage.input_tokens).toLocaleString()}</span>
          )}
          {agent.token_usage.output_tokens != null && (
            <span>Out: {Number(agent.token_usage.output_tokens).toLocaleString()}</span>
          )}
        </div>
      )}
    </div>
  );
}
