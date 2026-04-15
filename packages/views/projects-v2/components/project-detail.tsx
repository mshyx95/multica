"use client";

import { useState, useRef, useEffect, useCallback } from "react";
import {
  ArrowLeft,
  Rocket,
  Pause,
  Play,
  Send,
  CheckCircle2,
  RefreshCw,
  Loader2,
} from "lucide-react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { useNavigation } from "../../navigation";
import { Button } from "@multica/ui/components/ui/button";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { toast } from "sonner";
import type {
  ProjectV2,
  ProjectMessage,
  Subtask,
  SubtaskStatus,
} from "@multica/core/types";
import { PROJECT_V2_STATUS_CONFIG } from "../config";
import { AgentCard } from "./agent-card";

const SUBTASK_STATUS_STYLES: Record<SubtaskStatus, string> = {
  pending: "text-muted-foreground bg-muted-foreground/20",
  running: "text-amber-600 dark:text-amber-400 bg-amber-500/20",
  completed: "text-green-600 dark:text-green-400 bg-green-500/20",
  failed: "text-destructive bg-destructive/20",
  blocked: "text-orange-600 dark:text-orange-400 bg-orange-500/20",
};

// ─── Planner chat ────────────────────────────────────────────────────────

function PlannerChat({
  status,
  messages,
  onSend,
  onApprove,
  onRevise,
  sending,
}: {
  status: ProjectV2["status"];
  messages: ProjectMessage[];
  onSend: (content: string) => void;
  onApprove: () => void;
  onRevise: () => void;
  sending: boolean;
}) {
  const scrollRef = useRef<HTMLDivElement>(null);
  const [input, setInput] = useState("");

  useEffect(() => {
    if (scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
    }
  }, [messages]);

  const handleSend = () => {
    const trimmed = input.trim();
    if (!trimmed || sending) return;
    onSend(trimmed);
    setInput("");
  };

  const canInput = status === "planning" || status === "executing";
  const hasPlanProposal =
    status === "planning" &&
    messages.some((m) => m.role === "planner" && m.content.length > 100);

  return (
    <div className="flex flex-col h-full border rounded-lg">
      {/* Messages */}
      <div ref={scrollRef} className="flex-1 overflow-y-auto p-4 space-y-3">
        {messages.length === 0 && (
          <p className="text-sm text-muted-foreground text-center py-8">
            No messages yet. Start a conversation with the planner.
          </p>
        )}
        {messages.map((msg) => (
          <MessageBubble key={msg.id} message={msg} />
        ))}
        {sending && (
          <div className="flex justify-start">
            <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />
          </div>
        )}
      </div>

      {/* Approve / Revise buttons */}
      {hasPlanProposal && (
        <div className="flex items-center gap-2 px-4 py-2 border-t bg-muted/30">
          <span className="text-xs text-muted-foreground mr-auto">Planner proposed a plan.</span>
          <Button size="xs" variant="outline" onClick={onRevise}>
            <RefreshCw className="h-3 w-3" />
            Revise
          </Button>
          <Button size="xs" onClick={onApprove}>
            <CheckCircle2 className="h-3 w-3" />
            Approve
          </Button>
        </div>
      )}

      {/* Input */}
      <div className="flex items-center gap-2 p-3 border-t">
        <input
          type="text"
          className="flex-1 min-w-0 rounded-md border border-border bg-background px-3 py-2 text-sm placeholder:text-muted-foreground focus:outline-none focus:ring-1 focus:ring-ring disabled:opacity-50"
          placeholder={canInput ? "Send a message to the planner…" : "Chat is disabled in this state"}
          value={input}
          onChange={(e) => setInput(e.target.value)}
          onKeyDown={(e) => e.key === "Enter" && handleSend()}
          disabled={!canInput || sending}
        />
        <Button
          size="icon-xs"
          onClick={handleSend}
          disabled={!canInput || !input.trim() || sending}
        >
          <Send className="h-3.5 w-3.5" />
        </Button>
      </div>
    </div>
  );
}

function MessageBubble({ message }: { message: ProjectMessage }) {
  if (message.role === "user") {
    return (
      <div className="flex justify-end">
        <div className="rounded-2xl bg-muted px-3.5 py-2 text-sm max-w-[80%] break-words">
          {message.content}
        </div>
      </div>
    );
  }

  if (message.role === "system") {
    return (
      <div className="flex justify-center">
        <span className="text-xs text-muted-foreground bg-muted/50 rounded-full px-3 py-1">
          {message.content}
        </span>
      </div>
    );
  }

  // planner / orchestrator / executor / evaluator
  return (
    <div className="flex justify-start">
      <div className="space-y-1 max-w-[85%]">
        <span className="text-[10px] font-medium text-muted-foreground capitalize">
          {message.role}{message.agent_id ? ` (${message.agent_id.slice(0, 6)})` : ""}
        </span>
        <div className="rounded-2xl bg-card border px-3.5 py-2 text-sm break-words whitespace-pre-wrap">
          {message.content}
        </div>
      </div>
    </div>
  );
}

// ─── Subtask table ───────────────────────────────────────────────────────

function SubtaskTable({ subtasks }: { subtasks: Subtask[] }) {
  if (subtasks.length === 0) return null;

  return (
    <div className="border rounded-lg overflow-hidden">
      <table className="w-full text-sm">
        <thead>
          <tr className="border-b bg-muted/30">
            <th className="text-left px-3 py-2 font-medium text-xs text-muted-foreground">Title</th>
            <th className="text-left px-3 py-2 font-medium text-xs text-muted-foreground w-24">Status</th>
            <th className="text-left px-3 py-2 font-medium text-xs text-muted-foreground w-28">Assigned</th>
            <th className="text-left px-3 py-2 font-medium text-xs text-muted-foreground">Result</th>
          </tr>
        </thead>
        <tbody className="divide-y">
          {subtasks.map((st) => {
            const styleCls = SUBTASK_STATUS_STYLES[st.status];
            return (
              <tr key={st.id} className="hover:bg-muted/20 transition-colors">
                <td className="px-3 py-2">
                  <span className="font-medium">{st.title}</span>
                  {st.description && (
                    <p className="text-xs text-muted-foreground mt-0.5 line-clamp-1">{st.description}</p>
                  )}
                </td>
                <td className="px-3 py-2">
                  <span className={`rounded-full px-2 py-0.5 text-[11px] font-medium ${styleCls}`}>
                    {st.status}
                  </span>
                </td>
                <td className="px-3 py-2 text-xs text-muted-foreground truncate">
                  {st.assigned_to ? st.assigned_to.slice(0, 8) : "—"}
                </td>
                <td className="px-3 py-2 text-xs text-muted-foreground truncate max-w-[200px]">
                  {st.result || "—"}
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}

// ─── Main detail page ────────────────────────────────────────────────────

export function ProjectV2Detail({ projectId }: { projectId: string }) {
  const nav = useNavigation();
  const qc = useQueryClient();
  const [sending, setSending] = useState(false);

  const { data: project, isLoading: projectLoading } = useQuery({
    queryKey: ["projects-v2", projectId],
    queryFn: () => api.getProjectV2(projectId),
    refetchInterval: 5000,
  });

  const { data: agents = [] } = useQuery({
    queryKey: ["projects-v2", projectId, "agents"],
    queryFn: () => api.listProjectAgents(projectId),
    refetchInterval: 5000,
  });

  const { data: messages = [] } = useQuery({
    queryKey: ["projects-v2", projectId, "messages"],
    queryFn: () => api.listProjectMessages(projectId),
    refetchInterval: 3000,
  });

  const { data: subtasks = [] } = useQuery({
    queryKey: ["projects-v2", projectId, "subtasks"],
    queryFn: () => api.listSubtasks(projectId),
    refetchInterval: 5000,
  });

  const handleSendMessage = useCallback(async (content: string) => {
    setSending(true);
    try {
      await api.sendProjectMessage(projectId, content);
      qc.invalidateQueries({ queryKey: ["projects-v2", projectId, "messages"] });
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to send message");
    } finally {
      setSending(false);
    }
  }, [projectId, qc]);

  const handleApprove = useCallback(async () => {
    try {
      await api.sendProjectMessage(projectId, "/approve");
      qc.invalidateQueries({ queryKey: ["projects-v2", projectId, "messages"] });
      qc.invalidateQueries({ queryKey: ["projects-v2", projectId] });
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to approve plan");
    }
  }, [projectId, qc]);

  const handleRevise = useCallback(async () => {
    try {
      await api.sendProjectMessage(projectId, "/revise");
      qc.invalidateQueries({ queryKey: ["projects-v2", projectId, "messages"] });
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to request revision");
    }
  }, [projectId, qc]);

  const handleDeploy = useCallback(async () => {
    if (!project?.runtime_id) {
      toast.error("No runtime assigned. Assign a runtime first.");
      return;
    }
    try {
      await api.deployProjectV2(projectId, { runtime_id: project.runtime_id });
      qc.invalidateQueries({ queryKey: ["projects-v2", projectId] });
      toast.success("Project deployed");
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to deploy");
    }
  }, [projectId, project, qc]);

  const handlePauseResume = useCallback(async () => {
    if (!project) return;
    const newStatus = project.status === "paused" ? "executing" : "paused";
    try {
      await api.updateProjectV2(projectId, { status: newStatus });
      qc.invalidateQueries({ queryKey: ["projects-v2", projectId] });
      toast.success(newStatus === "paused" ? "Project paused" : "Project resumed");
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to update status");
    }
  }, [projectId, project, qc]);

  if (projectLoading || !project) {
    return (
      <div className="flex flex-1 min-h-0 flex-col">
        <div className="flex h-12 shrink-0 items-center gap-2 border-b px-4">
          <Skeleton className="h-4 w-4 rounded" />
          <Skeleton className="h-4 w-48" />
        </div>
        <div className="flex-1 p-6 space-y-6">
          <Skeleton className="h-8 w-full rounded-lg" />
          <Skeleton className="h-64 w-full rounded-lg" />
        </div>
      </div>
    );
  }

  const statusCfg = PROJECT_V2_STATUS_CONFIG[project.status];
  const canPauseResume = project.status === "executing" || project.status === "paused";

  return (
    <div className="flex flex-1 min-h-0 flex-col">
      {/* Header */}
      <div className="flex h-12 shrink-0 items-center gap-2 border-b px-4">
        <Button variant="ghost" size="icon-xs" onClick={() => nav.push("/projects-v2")}>
          <ArrowLeft className="h-4 w-4" />
        </Button>
        <h1 className="text-sm font-semibold truncate">{project.name}</h1>
        <span className={`shrink-0 rounded-full px-2 py-0.5 text-[11px] font-medium ${statusCfg.color} ${statusCfg.bg}`}>
          {statusCfg.label}
        </span>
        <div className="ml-auto flex items-center gap-2">
          {project.status === "draft" && (
            <Button size="xs" onClick={handleDeploy}>
              <Rocket className="h-3 w-3" />
              Deploy
            </Button>
          )}
          {canPauseResume && (
            <Button
              size="xs"
              variant="outline"
              onClick={handlePauseResume}
            >
              {project.status === "paused" ? (
                <><Play className="h-3 w-3" /> Resume</>
              ) : (
                <><Pause className="h-3 w-3" /> Pause</>
              )}
            </Button>
          )}
        </div>
      </div>

      {/* Main content */}
      <div className="flex flex-1 min-h-0 overflow-hidden">
        {/* Left: Chat / log — 2/3 */}
        <div className="flex flex-col flex-1 min-w-0 p-4" style={{ flex: "2 1 0%" }}>
          <h2 className="text-xs font-medium text-muted-foreground mb-2">
            {project.status === "executing" ? "Execution Log" : "Planner Chat"}
          </h2>
          <div className="flex-1 min-h-0">
            <PlannerChat
              status={project.status}
              messages={messages}
              onSend={handleSendMessage}
              onApprove={handleApprove}
              onRevise={handleRevise}
              sending={sending}
            />
          </div>
        </div>

        {/* Right: Agent panel — 1/3 */}
        <div className="w-80 shrink-0 border-l overflow-y-auto p-4 space-y-3">
          <h2 className="text-xs font-medium text-muted-foreground">Agents</h2>
          {agents.length === 0 ? (
            <p className="text-xs text-muted-foreground">No agents assigned yet.</p>
          ) : (
            agents.map((agent) => (
              <AgentCard key={agent.id} agent={agent} />
            ))
          )}
        </div>
      </div>

      {/* Bottom: Subtasks */}
      {subtasks.length > 0 && (
        <div className="shrink-0 border-t p-4 max-h-64 overflow-y-auto">
          <h2 className="text-xs font-medium text-muted-foreground mb-2">
            Subtasks ({subtasks.length})
          </h2>
          <SubtaskTable subtasks={subtasks} />
        </div>
      )}
    </div>
  );
}
