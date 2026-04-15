"use client";

import { useCallback, useState, useMemo, useEffect } from "react";
import {
  ArrowLeft,
  Rocket,
  Pause,
  Play,
  Trash2,
  Terminal as TerminalIcon,
  FileText,
  ScrollText,
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
  ProjectAgent,
  Subtask,
  SubtaskStatus,
} from "@multica/core/types";
import { runtimeListOptions } from "@multica/core/runtimes/queries";
import { useWorkspaceId } from "@multica/core/hooks";
import { PROJECT_V2_STATUS_CONFIG } from "../config";
import { AgentCard } from "./agent-card";
import { WebTerminal } from "./web-terminal";

// ─── Agent roles definition ─────────────────────────────────────────────────

interface AgentRoleDef {
  role: string;
  label: string;
  defaultModel: string;
}

const AGENT_ROLES: AgentRoleDef[] = [
  { role: "planner", label: "Planner", defaultModel: "claude-opus-4.6-1m" },
  { role: "orchestrator", label: "Orchestrator", defaultModel: "claude-opus-4.6-1m" },
  { role: "executor-0", label: "Executor 0", defaultModel: "claude-sonnet-4" },
  { role: "executor-1", label: "Executor 1", defaultModel: "gpt-5.3-codex" },
  { role: "evaluator", label: "Evaluator", defaultModel: "gpt-5.3-codex" },
];

type ContentTab = "terminal" | "logs" | "results";

// ─── Subtask status styles ──────────────────────────────────────────────────

const SUBTASK_STATUS_STYLES: Record<SubtaskStatus, string> = {
  pending: "text-muted-foreground bg-muted-foreground/20",
  running: "text-amber-600 dark:text-amber-400 bg-amber-500/20",
  completed: "text-green-600 dark:text-green-400 bg-green-500/20",
  failed: "text-destructive bg-destructive/20",
  blocked: "text-orange-600 dark:text-orange-400 bg-orange-500/20",
};

// ─── Helper: match project agents to role defs ──────────────────────────────

function buildAgentWindowName(agent: ProjectAgent): string {
  if (agent.tmux_window) return agent.tmux_window;
  return agent.agent_index > 0
    ? `${agent.role}-${agent.agent_index}`
    : agent.role;
}

function getVisibleRoles(status: ProjectV2["status"]): AgentRoleDef[] {
  if (status === "planning") return AGENT_ROLES.filter((r) => r.role === "planner");
  if (status === "executing" || status === "paused") return AGENT_ROLES;
  if (status === "completed" || status === "failed") return AGENT_ROLES;
  return [];
}

// ─── Subtask sidebar list ───────────────────────────────────────────────────

function SubtaskList({ subtasks }: { subtasks: Subtask[] }) {
  if (subtasks.length === 0) return null;

  return (
    <div className="space-y-1.5">
      {subtasks.map((st) => {
        const styleCls = SUBTASK_STATUS_STYLES[st.status];
        return (
          <div
            key={st.id}
            className="rounded-md border border-border px-2.5 py-1.5 text-xs"
          >
            <div className="flex items-center gap-2">
              <span className={`shrink-0 rounded-full px-1.5 py-0.5 text-[10px] font-medium ${styleCls}`}>
                {st.status}
              </span>
              <span className="truncate font-medium">{st.title}</span>
            </div>
            {st.assigned_to && (
              <div className="text-[10px] text-muted-foreground mt-0.5 truncate">
                → {st.assigned_to.slice(0, 12)}
              </div>
            )}
          </div>
        );
      })}
    </div>
  );
}

// ─── Logs panel placeholder ─────────────────────────────────────────────────

function LogsPanel({ projectId, agentRole }: { projectId: string; agentRole: string }) {
  const [logs, setLogs] = useState<string>("Loading logs...");

  useEffect(() => {
    let cancelled = false;
    async function fetchLogs() {
      try {
        const res = await fetch(`/api/v2/projects/${projectId}/agents/${agentRole}/logs`);
        if (!res.ok) {
          setLogs(`No logs available (${res.status})`);
          return;
        }
        const text = await res.text();
        if (!cancelled) setLogs(text || "No log output yet.");
      } catch {
        if (!cancelled) setLogs("Failed to fetch logs.");
      }
    }
    fetchLogs();
    const interval = setInterval(fetchLogs, 5000);
    return () => {
      cancelled = true;
      clearInterval(interval);
    };
  }, [projectId, agentRole]);

  return (
    <div className="h-full overflow-auto rounded-lg border bg-[#0a0a0a] p-4">
      <pre className="text-xs text-green-400 font-mono whitespace-pre-wrap">{logs}</pre>
    </div>
  );
}

// ─── Results panel placeholder ──────────────────────────────────────────────

function ResultsPanel({ projectId, agentRole }: { projectId: string; agentRole: string }) {
  const [content, setContent] = useState<string>("Loading results...");

  useEffect(() => {
    let cancelled = false;
    async function fetchResults() {
      try {
        const res = await fetch(`/api/v2/projects/${projectId}/agents/${agentRole}/results`);
        if (!res.ok) {
          setContent("No results available yet.");
          return;
        }
        const text = await res.text();
        if (!cancelled) setContent(text || "No results yet.");
      } catch {
        if (!cancelled) setContent("Results endpoint not available.");
      }
    }
    fetchResults();
    return () => { cancelled = true; };
  }, [projectId, agentRole]);

  return (
    <div className="h-full overflow-auto rounded-lg border bg-background p-4">
      <pre className="text-xs text-foreground font-mono whitespace-pre-wrap">{content}</pre>
    </div>
  );
}

// ─── Main detail page ────────────────────────────────────────────────────────

export function ProjectV2Detail({ projectId }: { projectId: string }) {
  const nav = useNavigation();
  const qc = useQueryClient();
  const wsId = useWorkspaceId();

  const [selectedRole, setSelectedRole] = useState<string | null>(null);
  const [activeTab, setActiveTab] = useState<ContentTab>("terminal");

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

  // Build a map from window name → agent data for quick lookup
  const agentMap = useMemo(() => {
    const m = new Map<string, ProjectAgent>();
    for (const a of agents) {
      const win = buildAgentWindowName(a);
      m.set(win, a);
    }
    return m;
  }, [agents]);

  // Determine visible roles based on project status
  const visibleRoles = useMemo(
    () => (project ? getVisibleRoles(project.status) : []),
    [project],
  );

  // Auto-select planner on initial load
  useEffect(() => {
    if (selectedRole !== null) return;
    if (!project) return;
    if (project.status === "draft") return;
    const firstRole = visibleRoles[0];
    if (firstRole) {
      setSelectedRole(firstRole.role);
    }
  }, [project, visibleRoles, selectedRole]);

  // Compute the tmux session name for the selected agent
  const terminalSessionName = useMemo(() => {
    if (!selectedRole) return null;
    return `multica-${projectId.slice(0, 8)}:${selectedRole}`;
  }, [projectId, selectedRole]);

  // ── Actions ──

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

  // ── Loading state ──

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
  const isDraft = project.status === "draft";

  // ── Tab bar items ──
  const tabs: { id: ContentTab; label: string; icon: React.ReactNode }[] = [
    { id: "terminal", label: "Terminal", icon: <TerminalIcon className="h-3.5 w-3.5" /> },
    { id: "logs", label: "Logs", icon: <ScrollText className="h-3.5 w-3.5" /> },
    { id: "results", label: "Results", icon: <FileText className="h-3.5 w-3.5" /> },
  ];

  return (
    <div className="flex flex-1 min-h-0 flex-col">
      {/* ── Header ── */}
      <div className="flex h-12 shrink-0 items-center gap-2 border-b px-4">
        <Button variant="ghost" size="icon-xs" onClick={() => nav.push("/projects-v2")}>
          <ArrowLeft className="h-4 w-4" />
        </Button>
        <h1 className="text-sm font-semibold truncate">{project.name}</h1>
        <span className={`shrink-0 rounded-full px-2 py-0.5 text-[11px] font-medium ${statusCfg.color} ${statusCfg.bg}`}>
          {statusCfg.label}
        </span>
        <div className="ml-auto flex items-center gap-2">
          {isDraft && (
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
            <Button size="xs" variant="outline" onClick={handlePauseResume}>
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

      {/* ── Body: sidebar + main ── */}
      <div className="flex flex-1 min-h-0 overflow-hidden">
        {/* ── Left sidebar ── */}
        <div className="w-64 shrink-0 border-r flex flex-col min-h-0">
          {/* Agent list */}
          <div className="flex-1 overflow-y-auto p-3 space-y-2">
            <h2 className="text-xs font-medium text-muted-foreground mb-1">Agents</h2>
            {isDraft ? (
              <p className="text-xs text-muted-foreground py-2">Deploy to see agents</p>
            ) : visibleRoles.length === 0 ? (
              <p className="text-xs text-muted-foreground py-2">No agents</p>
            ) : (
              visibleRoles.map((roleDef) => {
                const agentData = agentMap.get(roleDef.role);
                return (
                  <AgentCard
                    key={roleDef.role}
                    role={roleDef.role}
                    label={roleDef.label}
                    model={agentData?.model || roleDef.defaultModel}
                    status={agentData?.status ?? "unknown"}
                    currentTask={agentData?.current_task}
                    tokenUsage={agentData?.token_usage}
                    isSelected={selectedRole === roleDef.role}
                    onClick={() => setSelectedRole(roleDef.role)}
                  />
                );
              })
            )}
          </div>

          {/* Subtask list */}
          {subtasks.length > 0 && (
            <div className="shrink-0 border-t p-3 max-h-72 overflow-y-auto">
              <h2 className="text-xs font-medium text-muted-foreground mb-2">
                Subtasks ({subtasks.length})
              </h2>
              <SubtaskList subtasks={subtasks} />
            </div>
          )}
        </div>

        {/* ── Main content area ── */}
        <div className="flex flex-1 min-w-0 flex-col">
          {isDraft || !selectedRole ? (
            <div className="flex flex-1 items-center justify-center text-sm text-muted-foreground">
              {isDraft
                ? "Deploy the project to start agents"
                : "Select an agent to view its terminal"}
            </div>
          ) : (
            <>
              {/* Tab bar */}
              <div className="flex shrink-0 items-center gap-1 border-b px-4 h-10">
                {tabs.map((tab) => (
                  <button
                    key={tab.id}
                    onClick={() => setActiveTab(tab.id)}
                    className={`flex items-center gap-1.5 rounded-md px-3 py-1.5 text-xs font-medium transition-colors ${
                      activeTab === tab.id
                        ? "bg-muted text-foreground"
                        : "text-muted-foreground hover:text-foreground hover:bg-muted/50"
                    }`}
                  >
                    {tab.icon}
                    {tab.label}
                  </button>
                ))}
                <span className="ml-auto text-[10px] text-muted-foreground font-mono">
                  {terminalSessionName}
                </span>
              </div>

              {/* Tab content */}
              <div className="flex-1 min-h-0 p-4">
                {activeTab === "terminal" && terminalSessionName && (
                  <WebTerminal
                    key={terminalSessionName}
                    sessionName={terminalSessionName}
                  />
                )}
                {activeTab === "logs" && (
                  <LogsPanel projectId={projectId} agentRole={selectedRole} />
                )}
                {activeTab === "results" && (
                  <ResultsPanel projectId={projectId} agentRole={selectedRole} />
                )}
              </div>
            </>
          )}
        </div>
      </div>
    </div>
  );
}
