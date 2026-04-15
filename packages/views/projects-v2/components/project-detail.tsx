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
  ChevronDown,
  ChevronRight,
  Files,
  ListTodo,
  BookOpen,
  Eye,
  Pencil,
  Save,
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
import {
  Collapsible,
  CollapsibleTrigger,
  CollapsibleContent,
} from "@multica/ui/components/ui/collapsible";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { toast } from "sonner";
import type {
  ProjectV2,
  ProjectAgent,
  Subtask,
  SubtaskStatus,
  ProjectFile,
  TmuxWindow,
} from "@multica/core/types";
import { runtimeListOptions } from "@multica/core/runtimes/queries";
import { useWorkspaceId } from "@multica/core/hooks";
import { PROJECT_V2_STATUS_CONFIG } from "../config";
import { WebTerminal } from "./web-terminal";
import { Markdown } from "@multica/ui/markdown";

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

type ContentTab = "brief" | "terminal" | "files" | "tasks";

// ─── File category config ───────────────────────────────────────────────────

const FILE_CATEGORY_CONFIG: Record<string, { label: string; icon: string }> = {
  plan: { label: "📋 Plans", icon: "📋" },
  task: { label: "📝 Tasks", icon: "📝" },
  report: { label: "📊 Reports", icon: "📊" },
  state: { label: "⚙️ State", icon: "⚙️" },
  other: { label: "📄 Other", icon: "📄" },
};

// ─── Subtask status styles ──────────────────────────────────────────────────

const SUBTASK_STATUS_STYLES: Record<SubtaskStatus, string> = {
  pending: "text-muted-foreground bg-muted-foreground/20",
  running: "text-amber-600 dark:text-amber-400 bg-amber-500/20",
  completed: "text-green-600 dark:text-green-400 bg-green-500/20",
  failed: "text-destructive bg-destructive/20",
  blocked: "text-orange-600 dark:text-orange-400 bg-orange-500/20",
};

// ─── Tmux window status → dot color ────────────────────────────────────────

const TMUX_STATUS_DOT: Record<string, string> = {
  busy: "bg-green-500",
  idle: "bg-muted-foreground",
  dead: "bg-destructive",
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

// ─── Left Panel: Agent List ─────────────────────────────────────────────────

function AgentList({
  visibleRoles,
  agentMap,
  tmuxWindows,
  selectedAgent,
  onSelectAgent,
  isDraft,
}: {
  visibleRoles: AgentRoleDef[];
  agentMap: Map<string, ProjectAgent>;
  tmuxWindows: TmuxWindow[];
  selectedAgent: string | null;
  onSelectAgent: (role: string) => void;
  isDraft: boolean;
}) {
  const tmuxMap = useMemo(() => {
    const m = new Map<string, TmuxWindow>();
    for (const w of tmuxWindows) m.set(w.name, w);
    return m;
  }, [tmuxWindows]);

  if (isDraft) {
    return <p className="text-xs text-muted-foreground py-2">Deploy to see agents</p>;
  }

  if (visibleRoles.length === 0) {
    return <p className="text-xs text-muted-foreground py-2">No agents</p>;
  }

  return (
    <div className="space-y-1.5">
      {visibleRoles.map((roleDef) => {
        const agentData = agentMap.get(roleDef.role);
        const tmuxWin = tmuxMap.get(roleDef.role);
        // Determine status: prefer tmux live status, fall back to agent DB status
        const liveStatus = tmuxWin?.status;
        const dotCls = liveStatus
          ? (TMUX_STATUS_DOT[liveStatus] ?? "bg-muted-foreground/30")
          : "bg-muted-foreground/30";
        const isSelected = selectedAgent === roleDef.role;

        return (
          <button
            key={roleDef.role}
            onClick={() => onSelectAgent(roleDef.role)}
            className={`flex w-full items-center gap-2.5 rounded-lg border px-2.5 py-2 text-left text-xs transition-colors ${
              isSelected
                ? "border-primary bg-primary/5"
                : "border-border hover:bg-muted"
            }`}
          >
            <span className={`h-2 w-2 shrink-0 rounded-full ${dotCls}`} />
            <span className="font-medium truncate flex-1">{roleDef.label}</span>
            {agentData?.current_task && (
              <span className="text-[10px] text-muted-foreground truncate max-w-[100px]">
                {agentData.current_task}
              </span>
            )}
          </button>
        );
      })}
    </div>
  );
}

// ─── Left Panel: File Tree ──────────────────────────────────────────────────

function FileTree({
  files,
  selectedFile,
  onFileClick,
}: {
  files: ProjectFile[];
  selectedFile: string | null;
  onFileClick: (path: string) => void;
}) {
  const grouped = useMemo(() => {
    const groups: Record<string, ProjectFile[]> = {};
    for (const f of files) {
      if (f.is_dir) continue;
      const cat = f.category || "other";
      if (!groups[cat]) groups[cat] = [];
      groups[cat].push(f);
    }
    return groups;
  }, [files]);

  const categoryOrder = ["plan", "task", "report", "state", "other"];

  if (files.length === 0) {
    return <p className="text-xs text-muted-foreground py-2">No files yet</p>;
  }

  return (
    <div className="space-y-0.5">
      {categoryOrder.map((cat) => {
        const catFiles = grouped[cat];
        if (!catFiles || catFiles.length === 0) return null;
        const cfg = FILE_CATEGORY_CONFIG[cat] || { label: cat, icon: "📄" };
        return (
          <div key={cat} className="mb-1.5">
            <div className="text-[10px] font-medium text-muted-foreground mb-0.5 px-1">
              {cfg.label}
            </div>
            {catFiles.map((f) => (
              <button
                key={f.path}
                onClick={() => onFileClick(f.path)}
                className={`flex w-full items-center gap-1.5 rounded px-1.5 py-1 text-xs transition-colors ${
                  selectedFile === f.path
                    ? "bg-primary/10 text-foreground"
                    : "text-muted-foreground hover:text-foreground hover:bg-muted"
                }`}
                title={f.path}
              >
                <FileText className="h-3 w-3 shrink-0" />
                <span className="truncate">{f.name}</span>
              </button>
            ))}
          </div>
        );
      })}
    </div>
  );
}

// ─── Left Panel: Subtask List (compact) ─────────────────────────────────────

function SubtaskList({ subtasks }: { subtasks: Subtask[] }) {
  if (subtasks.length === 0) return null;

  return (
    <div className="space-y-1">
      {subtasks.map((st) => {
        const styleCls = SUBTASK_STATUS_STYLES[st.status];
        return (
          <div
            key={st.id}
            className="rounded-md border border-border px-2 py-1 text-xs"
          >
            <div className="flex items-center gap-1.5">
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

// ─── Main Tab: Terminal View ────────────────────────────────────────────────

function TerminalView({
  sessionName,
  selectedAgent,
}: {
  sessionName: string | null;
  selectedAgent: string | null;
}) {
  if (!selectedAgent || !sessionName) {
    return (
      <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
        Select an agent to view its terminal
      </div>
    );
  }

  return (
    <WebTerminal
      key={sessionName}
      sessionName={sessionName}
    />
  );
}

// ─── Main Tab: File Viewer ──────────────────────────────────────────────────

function FileViewer({
  selectedFile,
  fileContent,
  isLoading,
}: {
  selectedFile: string | null;
  fileContent: string;
  isLoading: boolean;
}) {
  if (!selectedFile) {
    return (
      <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
        <div className="text-center">
          <Files className="h-8 w-8 mx-auto mb-2 opacity-40" />
          <p>Select a file to view its content</p>
        </div>
      </div>
    );
  }

  const isJson = selectedFile.endsWith(".json");
  const isMd = selectedFile.endsWith(".md");

  let displayContent = fileContent;
  if (isJson && fileContent) {
    try {
      displayContent = JSON.stringify(JSON.parse(fileContent), null, 2);
    } catch {
      // already a string, use as-is
    }
  }

  return (
    <div className="flex h-full flex-col min-h-0">
      {/* Breadcrumb */}
      <div className="shrink-0 flex items-center gap-1.5 px-1 pb-2 text-xs text-muted-foreground font-mono">
        {selectedFile.split("/").map((part, i, arr) => (
          <span key={i} className="flex items-center gap-1">
            {i > 0 && <span className="text-muted-foreground/40">/</span>}
            <span className={i === arr.length - 1 ? "text-foreground font-medium" : ""}>
              {part}
            </span>
          </span>
        ))}
      </div>

      {/* Content */}
      {isLoading ? (
        <div className="flex-1 flex items-center justify-center">
          <Skeleton className="h-40 w-full rounded-lg" />
        </div>
      ) : isMd && displayContent ? (
        <div className="flex-1 overflow-auto min-h-0 rounded-lg border bg-background p-4 prose prose-sm dark:prose-invert max-w-none">
          <Markdown>{displayContent}</Markdown>
        </div>
      ) : (
        <pre className="flex-1 whitespace-pre-wrap text-xs font-mono bg-muted rounded-lg p-4 overflow-auto min-h-0">
          {displayContent || "(empty file)"}
        </pre>
      )}
    </div>
  );
}

// ─── Main Tab: Brief Editor ─────────────────────────────────────────────────

function BriefEditor({
  project,
  onSave,
}: {
  project: ProjectV2;
  onSave: (data: { description: string; goals: string }) => Promise<void>;
}) {
  const [description, setDescription] = useState(project.description ?? "");
  const [goals, setGoals] = useState(project.goals ?? "");
  const [preview, setPreview] = useState(false);
  const [dirty, setDirty] = useState(false);
  const [saving, setSaving] = useState(false);

  const isReadOnly = project.status !== "draft";

  // Sync from project data when it changes externally
  useEffect(() => {
    if (!dirty) {
      setDescription(project.description ?? "");
      setGoals(project.goals ?? "");
    }
  }, [project.description, project.goals, dirty]);

  const handleSave = useCallback(async () => {
    setSaving(true);
    try {
      await onSave({ description, goals });
      setDirty(false);
      toast.success("Brief saved");
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to save");
    } finally {
      setSaving(false);
    }
  }, [description, goals, onSave]);

  if (isReadOnly) {
    // Read-only view for non-draft projects
    return (
      <div className="flex h-full flex-col min-h-0 overflow-auto">
        <div className="space-y-4 p-1">
          {/* Description */}
          <div>
            <h3 className="text-xs font-semibold text-muted-foreground mb-2">Description</h3>
            {description ? (
              <div className="prose prose-sm dark:prose-invert max-w-none rounded-lg border bg-background p-4">
                <Markdown>{description}</Markdown>
              </div>
            ) : (
              <p className="text-xs text-muted-foreground italic">No description</p>
            )}
          </div>

          {/* Goals */}
          <div>
            <h3 className="text-xs font-semibold text-muted-foreground mb-2">Goals</h3>
            {goals ? (
              <div className="prose prose-sm dark:prose-invert max-w-none rounded-lg border bg-background p-4">
                <Markdown>{goals}</Markdown>
              </div>
            ) : (
              <p className="text-xs text-muted-foreground italic">No goals defined</p>
            )}
          </div>
        </div>
      </div>
    );
  }

  // Editable view for draft projects
  return (
    <div className="flex h-full flex-col min-h-0">
      {/* Toolbar */}
      <div className="flex items-center gap-2 mb-3 shrink-0">
        <Button
          variant="ghost"
          size="xs"
          onClick={() => setPreview(!preview)}
          className="gap-1"
        >
          {preview ? <Pencil className="h-3 w-3" /> : <Eye className="h-3 w-3" />}
          {preview ? "Edit" : "Preview"}
        </Button>
        <Button
          size="xs"
          onClick={handleSave}
          disabled={!dirty || saving}
          className="gap-1"
        >
          <Save className="h-3 w-3" />
          {saving ? "Saving…" : "Save"}
        </Button>
        {dirty && (
          <span className="text-[10px] text-muted-foreground">Unsaved changes</span>
        )}
      </div>

      {/* Content */}
      <div className="flex-1 min-h-0 overflow-auto space-y-4">
        {/* Description */}
        <div className="flex flex-col min-h-0">
          <h3 className="text-xs font-semibold text-muted-foreground mb-1.5">Description</h3>
          {preview ? (
            <div className="prose prose-sm dark:prose-invert max-w-none rounded-lg border bg-background p-4">
              <Markdown>{description || "*No description yet*"}</Markdown>
            </div>
          ) : (
            <Textarea
              value={description}
              onChange={(e) => { setDescription(e.target.value); setDirty(true); }}
              className="min-h-[200px] resize-none font-mono text-sm"
              placeholder="Describe your project in detail. Use markdown…"
              onBlur={() => { if (dirty) handleSave(); }}
            />
          )}
        </div>

        {/* Goals */}
        <div className="flex flex-col min-h-0">
          <h3 className="text-xs font-semibold text-muted-foreground mb-1.5">Goals</h3>
          {preview ? (
            <div className="prose prose-sm dark:prose-invert max-w-none rounded-lg border bg-background p-4">
              <Markdown>{goals || "*No goals yet*"}</Markdown>
            </div>
          ) : (
            <Textarea
              value={goals}
              onChange={(e) => { setGoals(e.target.value); setDirty(true); }}
              className="min-h-[120px] resize-none font-mono text-sm"
              placeholder="What should this project accomplish? Use markdown…"
              onBlur={() => { if (dirty) handleSave(); }}
            />
          )}
        </div>
      </div>
    </div>
  );
}

// ─── Main Tab: Task Tracker ─────────────────────────────────────────────────

function TaskTracker({ files }: { files: ProjectFile[] }) {
  // Group task files by executor
  const executorTasks = useMemo(() => {
    const groups: Record<string, ProjectFile[]> = {};
    for (const f of files) {
      if (f.category !== "task" && f.category !== "report") continue;
      // Extract executor from path: tasks/executor-0/task-1-xxx.md → executor-0
      const match = f.path.match(/tasks\/(executor-\d+|evaluator)\//);
      if (match && match[1]) {
        const executor = match[1];
        if (!groups[executor]) groups[executor] = [];
        groups[executor]!.push(f);
      }
    }
    return groups;
  }, [files]);

  const executors = Object.keys(executorTasks).sort();

  if (executors.length === 0) {
    return (
      <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
        <div className="text-center">
          <ListTodo className="h-8 w-8 mx-auto mb-2 opacity-40" />
          <p>No task assignments yet</p>
          <p className="text-xs mt-1">Tasks appear once executors are assigned work</p>
        </div>
      </div>
    );
  }

  return (
    <div className="space-y-4 overflow-auto h-full">
      {executors.map((executor) => {
        const label = executor.startsWith("executor-")
          ? `Executor ${executor.split("-")[1]}`
          : executor.charAt(0).toUpperCase() + executor.slice(1);
        const taskFiles = executorTasks[executor] ?? [];

        return (
          <div key={executor} className="rounded-lg border">
            <div className="px-3 py-2 border-b bg-muted/50">
              <h3 className="text-xs font-semibold">{label}</h3>
            </div>
            <div className="p-3 space-y-2">
              {taskFiles.map((f) => {
                const isReport = f.name.startsWith("report-");
                const isTask = f.name.startsWith("task-");
                return (
                  <div key={f.path} className="rounded border px-2.5 py-2 text-xs">
                    <div className="flex items-center gap-2">
                      <span className="text-[10px]">
                        {isReport ? "📊" : isTask ? "📝" : "📄"}
                      </span>
                      <span className="font-medium truncate">{f.name}</span>
                      <span
                        className={`ml-auto shrink-0 rounded-full px-1.5 py-0.5 text-[10px] font-medium ${
                          isReport
                            ? "text-green-600 dark:text-green-400 bg-green-500/20"
                            : "text-amber-600 dark:text-amber-400 bg-amber-500/20"
                        }`}
                      >
                        {isReport ? "report" : "task"}
                      </span>
                    </div>
                    <div className="text-[10px] text-muted-foreground mt-1 font-mono truncate">
                      {f.path}
                    </div>
                  </div>
                );
              })}
            </div>
          </div>
        );
      })}
    </div>
  );
}

// ─── Main detail page ────────────────────────────────────────────────────────

export function ProjectV2Detail({ projectId }: { projectId: string }) {
  const nav = useNavigation();
  const qc = useQueryClient();
  const wsId = useWorkspaceId();

  const [selectedAgent, setSelectedAgent] = useState<string | null>(null);
  const [activeTab, setActiveTab] = useState<ContentTab>("brief");
  const [selectedFile, setSelectedFile] = useState<string | null>(null);
  const [fileContent, setFileContent] = useState<string>("");
  const [fileLoading, setFileLoading] = useState(false);

  // Section collapse state
  const [agentsOpen, setAgentsOpen] = useState(true);
  const [filesOpen, setFilesOpen] = useState(true);
  const [subtasksOpen, setSubtasksOpen] = useState(true);

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

  // Tmux session name
  const sessionName = useMemo(
    () => `multica-${projectId.slice(0, 8)}`,
    [projectId],
  );

  // Fetch tmux windows for live agent status
  const { data: tmuxWindows = [] } = useQuery({
    queryKey: ["tmux-windows", sessionName],
    queryFn: () => api.listTmuxWindows(sessionName),
    refetchInterval: 5000,
    enabled: project?.status !== "draft",
  });

  // Fetch project files
  const { data: files = [] } = useQuery({
    queryKey: ["projects-v2", projectId, "files"],
    queryFn: () => api.listProjectFiles(projectId),
    refetchInterval: 10000,
    enabled: project?.status !== "draft",
  });

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

  // Auto-select planner on initial load when planning
  useEffect(() => {
    if (selectedAgent !== null) return;
    if (!project) return;
    if (project.status === "draft") return;
    if (project.status === "planning") {
      setSelectedAgent("planner");
      return;
    }
    const firstRole = visibleRoles[0];
    if (firstRole) {
      setSelectedAgent(firstRole.role);
    }
  }, [project, visibleRoles, selectedAgent]);

  // Compute the tmux session:window name for the selected agent terminal
  const terminalSessionName = useMemo(() => {
    if (!selectedAgent) return null;
    return `${sessionName}:${selectedAgent}`;
  }, [sessionName, selectedAgent]);

  // ── File click handler ──
  const handleFileClick = useCallback(async (filePath: string) => {
    setActiveTab("files");
    setSelectedFile(filePath);
    setFileLoading(true);
    try {
      const result = await api.readProjectFile(projectId, filePath);
      setFileContent(result.content);
    } catch {
      setFileContent("Failed to load file content.");
    } finally {
      setFileLoading(false);
    }
  }, [projectId]);

  // ── Agent select handler ──
  const handleSelectAgent = useCallback((role: string) => {
    setSelectedAgent(role);
    setActiveTab("terminal");
  }, []);

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

  const handleSaveBrief = useCallback(async (data: { description: string; goals: string }) => {
    await api.updateProjectV2(projectId, data);
    qc.invalidateQueries({ queryKey: ["projects-v2", projectId] });
  }, [projectId, qc]);

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
    { id: "brief", label: "Brief", icon: <BookOpen className="h-3.5 w-3.5" /> },
    { id: "terminal", label: "Terminal", icon: <TerminalIcon className="h-3.5 w-3.5" /> },
    { id: "files", label: "Files", icon: <Files className="h-3.5 w-3.5" /> },
    { id: "tasks", label: "Tasks", icon: <ListTodo className="h-3.5 w-3.5" /> },
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
        {/* ── Left sidebar (w-72) ── */}
        <div className="w-72 shrink-0 border-r flex flex-col min-h-0 overflow-y-auto">
          {/* Section 1: Agents */}
          <Collapsible open={agentsOpen} onOpenChange={setAgentsOpen}>
            <CollapsibleTrigger className="flex w-full items-center gap-1.5 px-3 py-2 text-xs font-medium text-muted-foreground hover:text-foreground transition-colors">
              {agentsOpen ? <ChevronDown className="h-3 w-3" /> : <ChevronRight className="h-3 w-3" />}
              Agents
              {tmuxWindows.length > 0 && (
                <span className="ml-auto text-[10px] font-normal">
                  {tmuxWindows.filter((w) => w.status === "busy").length} active
                </span>
              )}
            </CollapsibleTrigger>
            <CollapsibleContent>
              <div className="px-3 pb-3">
                <AgentList
                  visibleRoles={visibleRoles}
                  agentMap={agentMap}
                  tmuxWindows={tmuxWindows}
                  selectedAgent={selectedAgent}
                  onSelectAgent={handleSelectAgent}
                  isDraft={isDraft}
                />
              </div>
            </CollapsibleContent>
          </Collapsible>

          {/* Section 2: Files */}
          <Collapsible open={filesOpen} onOpenChange={setFilesOpen}>
            <CollapsibleTrigger className="flex w-full items-center gap-1.5 px-3 py-2 text-xs font-medium text-muted-foreground hover:text-foreground transition-colors border-t">
              {filesOpen ? <ChevronDown className="h-3 w-3" /> : <ChevronRight className="h-3 w-3" />}
              Files
              {files.length > 0 && (
                <span className="ml-auto text-[10px] font-normal">{files.filter((f) => !f.is_dir).length}</span>
              )}
            </CollapsibleTrigger>
            <CollapsibleContent>
              <div className="px-3 pb-3">
                <FileTree
                  files={files}
                  selectedFile={selectedFile}
                  onFileClick={handleFileClick}
                />
              </div>
            </CollapsibleContent>
          </Collapsible>

          {/* Section 3: Subtasks */}
          {subtasks.length > 0 && (
            <Collapsible open={subtasksOpen} onOpenChange={setSubtasksOpen}>
              <CollapsibleTrigger className="flex w-full items-center gap-1.5 px-3 py-2 text-xs font-medium text-muted-foreground hover:text-foreground transition-colors border-t">
                {subtasksOpen ? <ChevronDown className="h-3 w-3" /> : <ChevronRight className="h-3 w-3" />}
                Subtasks
                <span className="ml-auto text-[10px] font-normal">{subtasks.length}</span>
              </CollapsibleTrigger>
              <CollapsibleContent>
                <div className="px-3 pb-3">
                  <SubtaskList subtasks={subtasks} />
                </div>
              </CollapsibleContent>
            </Collapsible>
          )}
        </div>

        {/* ── Main content area ── */}
        <div className="flex flex-1 min-w-0 flex-col">
          {/* Tab bar — always visible */}
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
              {activeTab === "terminal" && terminalSessionName}
              {activeTab === "files" && selectedFile}
            </span>
          </div>

          {/* Tab content */}
          <div className="flex-1 min-h-0 p-4">
            {activeTab === "brief" && project && (
              <BriefEditor project={project} onSave={handleSaveBrief} />
            )}
            {activeTab === "terminal" && (
              <TerminalView
                sessionName={terminalSessionName}
                selectedAgent={selectedAgent}
              />
            )}
            {activeTab === "files" && (
              <FileViewer
                selectedFile={selectedFile}
                fileContent={fileContent}
                isLoading={fileLoading}
              />
            )}
            {activeTab === "tasks" && (
              <TaskTracker files={files} />
            )}
          </div>
        </div>
      </div>
    </div>
  );
}
