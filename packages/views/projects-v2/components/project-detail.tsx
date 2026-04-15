"use client";

import { useCallback } from "react";
import {
  ArrowLeft,
  Rocket,
  Pause,
  Play,
  Trash2,
} from "lucide-react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { useNavigation } from "../../navigation";
import { Button } from "@multica/ui/components/ui/button";
import {
  Popover,
  PopoverTrigger,
  PopoverContent,
} from "@multica/ui/components/ui/popover";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { toast } from "sonner";
import type {
  ProjectV2,
  Subtask,
  SubtaskStatus,
} from "@multica/core/types";
import { runtimeListOptions } from "@multica/core/runtimes/queries";
import { useWorkspaceId } from "@multica/core/hooks";
import { PROJECT_V2_STATUS_CONFIG } from "../config";
import { AgentCard } from "./agent-card";
import { WebTerminal } from "./web-terminal";

const SUBTASK_STATUS_STYLES: Record<SubtaskStatus, string> = {
  pending: "text-muted-foreground bg-muted-foreground/20",
  running: "text-amber-600 dark:text-amber-400 bg-amber-500/20",
  completed: "text-green-600 dark:text-green-400 bg-green-500/20",
  failed: "text-destructive bg-destructive/20",
  blocked: "text-orange-600 dark:text-orange-400 bg-orange-500/20",
};

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
  const wsId = useWorkspaceId();

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

  const { data: subtasks = [] } = useQuery({
    queryKey: ["projects-v2", projectId, "subtasks"],
    queryFn: () => api.listSubtasks(projectId),
    refetchInterval: 5000,
  });

  const { data: runtimes = [] } = useQuery(runtimeListOptions(wsId));

  const handleDeploy = useCallback(async (runtimeId: string) => {
    try {
      await api.deployProjectV2(projectId, { runtime_id: runtimeId });
      qc.invalidateQueries({ queryKey: ["projects-v2", projectId] });
      toast.success("Project deployed");
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to deploy");
    }
  }, [projectId, qc]);

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

  const handleDelete = useCallback(async () => {
    if (!confirm("Delete this project?")) return;
    try {
      await api.deleteProjectV2(projectId);
      qc.invalidateQueries({ queryKey: ["projects-v2"] });
      nav.push("/projects-v2");
      toast.success("Project deleted");
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to delete project");
    }
  }, [projectId, qc, nav]);

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
            <Popover>
              <PopoverTrigger asChild>
                <Button size="xs">
                  <Rocket className="h-3 w-3" />
                  Deploy
                </Button>
              </PopoverTrigger>
              <PopoverContent className="w-64 p-2" align="end">
                <p className="text-xs text-muted-foreground mb-2">Select runtime:</p>
                {runtimes.filter((r) => r.status === "online").length === 0 && (
                  <p className="text-xs text-muted-foreground py-2 text-center">No online runtimes available</p>
                )}
                {runtimes.filter((r) => r.status === "online").map((r) => (
                  <button
                    key={r.id}
                    onClick={() => handleDeploy(r.id)}
                    className="flex w-full items-center gap-2 rounded px-2 py-1.5 text-sm hover:bg-accent transition-colors"
                  >
                    <span className="h-2 w-2 shrink-0 rounded-full bg-success" />
                    <span className="truncate">{r.name}</span>
                  </button>
                ))}
              </PopoverContent>
            </Popover>
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
          <Button variant="ghost" size="icon-xs" onClick={handleDelete} className="text-destructive">
            <Trash2 className="h-3.5 w-3.5" />
          </Button>
        </div>
      </div>

      {/* Main content */}
      <div className="flex flex-1 min-h-0 overflow-hidden">
        {/* Left: Terminal — 2/3 */}
        <div className="flex flex-col flex-1 min-w-0 p-4" style={{ flex: "2 1 0%" }}>
          <h2 className="text-xs font-medium text-muted-foreground mb-2">
            {project.status === "draft" ? "Deploy to start planner" : "Planner Terminal"}
          </h2>
          <div className="flex-1 min-h-0">
            {project.status !== "draft" ? (
              <WebTerminal sessionName={`multica-${projectId.slice(0, 8)}`} />
            ) : (
              <div className="flex items-center justify-center h-full text-sm text-muted-foreground">
                Deploy the project to start the planner terminal
              </div>
            )}
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
