"use client";

import { useState, useMemo } from "react";
import {
  Plus,
  FolderGit2,
  ChevronRight,
  Trash2,
  FileEdit,
  MessageSquare,
  Play,
  CheckCircle2,
  Pause,
  XCircle,
} from "lucide-react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspaceStore } from "@multica/core/workspace";
import { useWSEvent } from "@multica/core/realtime";
import { runtimeListOptions } from "@multica/core/runtimes/queries";
import { useNavigation } from "../../navigation";
import { WorkspaceAvatar } from "../../workspace/workspace-avatar";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { Button } from "@multica/ui/components/ui/button";
import { toast } from "sonner";
import type { ProjectV2, ProjectV2Status, CreateProjectV2Request, TmuxWindow } from "@multica/core/types";
import { PROJECT_V2_STATUS_CONFIG } from "../config";
import { CreateProjectDialog } from "./create-project-dialog";

// ─── Status column definitions ──────────────────────────────────────────────

const STATUS_COLUMNS: {
  status: ProjectV2Status;
  label: string;
  icon: typeof FileEdit;
}[] = [
  { status: "draft", label: "Draft", icon: FileEdit },
  { status: "planning", label: "Planning", icon: MessageSquare },
  { status: "executing", label: "Executing", icon: Play },
  { status: "completed", label: "Completed", icon: CheckCircle2 },
  { status: "paused", label: "Paused", icon: Pause },
  { status: "failed", label: "Failed", icon: XCircle },
];

const ALWAYS_VISIBLE_STATUSES = new Set<ProjectV2Status>(["draft", "planning", "executing"]);

// ─── Helpers ────────────────────────────────────────────────────────────────

function formatRelativeDate(date: string): string {
  const diff = Date.now() - new Date(date).getTime();
  const minutes = Math.floor(diff / (1000 * 60));
  if (minutes < 1) return "just now";
  if (minutes < 60) return `${minutes}m ago`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h ago`;
  const days = Math.floor(hours / 24);
  if (days === 1) return "1d ago";
  if (days < 30) return `${days}d ago`;
  const months = Math.floor(days / 30);
  return `${months}mo ago`;
}

// ─── Agent dots for executing projects ──────────────────────────────────────

function AgentDots({ projectId }: { projectId: string }) {
  const sessionName = `multica-${projectId.slice(0, 8)}`;
  const { data: windows = [] } = useQuery({
    queryKey: ["tmux-windows", sessionName],
    queryFn: () => api.listTmuxWindows(sessionName),
    refetchInterval: 5000,
  });

  if (windows.length === 0) return null;

  const busy = windows.filter((w: TmuxWindow) => w.status === "busy").length;
  const idle = windows.filter((w: TmuxWindow) => w.status === "idle").length;

  return (
    <div className="flex items-center gap-1.5 text-[10px] text-muted-foreground">
      <div className="flex items-center gap-0.5">
        {windows.map((w: TmuxWindow) => (
          <span
            key={w.name}
            title={`${w.name}: ${w.status}`}
            className={`h-1.5 w-1.5 rounded-full ${
              w.status === "busy" ? "bg-green-500" : w.status === "idle" ? "bg-muted-foreground" : "bg-destructive"
            }`}
          />
        ))}
      </div>
      <span>{busy} busy, {idle} idle</span>
    </div>
  );
}

// ─── Project Card ───────────────────────────────────────────────────────────

function ProjectCard({
  project,
  onDelete,
}: {
  project: ProjectV2;
  onDelete: (id: string) => void;
}) {
  const nav = useNavigation();
  const statusCfg = PROJECT_V2_STATUS_CONFIG[project.status];
  const isExecuting = project.status === "executing";

  return (
    <button
      onClick={() => nav.push(`/projects-v2/${project.id}`)}
      className="group flex w-full flex-col gap-1.5 rounded-lg border bg-card p-3 text-left transition-colors hover:bg-muted/50"
    >
      {/* Header: name + delete */}
      <div className="flex items-start justify-between gap-1">
        <h3 className="text-xs font-semibold truncate flex-1">{project.name}</h3>
        <span
          role="button"
          tabIndex={0}
          onClick={(e) => { e.stopPropagation(); onDelete(project.id); }}
          onKeyDown={(e) => { if (e.key === "Enter") { e.stopPropagation(); onDelete(project.id); } }}
          className="hidden group-hover:inline-flex items-center justify-center rounded p-0.5 text-destructive hover:bg-destructive/10 transition-colors shrink-0"
        >
          <Trash2 className="h-3 w-3" />
        </span>
      </div>

      {/* Description */}
      {project.description && (
        <p className="text-[11px] text-muted-foreground line-clamp-2 leading-relaxed">
          {project.description.split("\n")[0]}
        </p>
      )}

      {/* Agent dots for executing projects */}
      {isExecuting && <AgentDots projectId={project.id} />}

      {/* Footer: runtime + time */}
      <div className="flex items-center gap-2 text-[10px] text-muted-foreground mt-auto pt-1">
        {project.runtime_id && (
          <span className="truncate">
            <span className={`inline-block h-1.5 w-1.5 rounded-full mr-1 ${
              statusCfg.color.includes("green") ? "bg-green-500" : "bg-muted-foreground/50"
            }`} />
            {project.runtime_id.slice(0, 8)}
          </span>
        )}
        <span className="ml-auto shrink-0">{formatRelativeDate(project.updated_at)}</span>
      </div>
    </button>
  );
}

// ─── Status Column ──────────────────────────────────────────────────────────

function StatusColumn({
  column,
  projects,
  onDelete,
}: {
  column: (typeof STATUS_COLUMNS)[number];
  projects: ProjectV2[];
  onDelete: (id: string) => void;
}) {
  const Icon = column.icon;
  const statusCfg = PROJECT_V2_STATUS_CONFIG[column.status];

  return (
    <div className="flex w-[260px] shrink-0 flex-col min-h-0">
      {/* Column header */}
      <div className="mb-2 flex items-center gap-2 px-1">
        <Icon className={`h-3.5 w-3.5 ${statusCfg.color}`} />
        <span className="text-xs font-semibold">{column.label}</span>
        <span className="ml-auto rounded-full bg-muted px-1.5 py-0.5 text-[10px] font-medium text-muted-foreground">
          {projects.length}
        </span>
      </div>

      {/* Cards */}
      <div className="flex-1 space-y-2 overflow-y-auto pr-1">
        {projects.length === 0 ? (
          <div className="rounded-lg border border-dashed p-4 text-center">
            <p className="text-[11px] text-muted-foreground">No projects</p>
          </div>
        ) : (
          projects.map((p) => (
            <ProjectCard key={p.id} project={p} onDelete={onDelete} />
          ))
        )}
      </div>
    </div>
  );
}

// ─── Main Page ──────────────────────────────────────────────────────────────

export function ProjectsV2Page() {
  const wsId = useWorkspaceId();
  const workspace = useWorkspaceStore((s) => s.workspace);
  const qc = useQueryClient();
  const [showCreate, setShowCreate] = useState(false);

  const { data: projects = [], isLoading } = useQuery({
    queryKey: ["projects-v2", wsId],
    queryFn: () => api.listProjectsV2(),
    refetchInterval: 5000,
    enabled: !!wsId,
  });

  const { data: runtimes = [], isLoading: runtimesLoading } = useQuery(runtimeListOptions(wsId));

  useWSEvent("project_v2:updated" as any, () => {
    qc.invalidateQueries({ queryKey: ["projects-v2", wsId] });
  });
  useWSEvent("project_v2:deleted" as any, () => {
    qc.invalidateQueries({ queryKey: ["projects-v2", wsId] });
  });

  const handleCreate = async (data: CreateProjectV2Request) => {
    await api.createProjectV2(data);
    qc.invalidateQueries({ queryKey: ["projects-v2", wsId] });
  };

  const handleDelete = async (id: string) => {
    if (!confirm("Delete this project?")) return;
    try {
      await api.deleteProjectV2(id);
      qc.invalidateQueries({ queryKey: ["projects-v2", wsId] });
      toast.success("Project deleted");
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to delete project");
    }
  };

  // Group projects by status
  const grouped = useMemo(() => {
    const map = new Map<ProjectV2Status, ProjectV2[]>();
    for (const col of STATUS_COLUMNS) map.set(col.status, []);
    for (const p of projects) {
      const list = map.get(p.status);
      if (list) list.push(p);
      else map.set(p.status, [p]);
    }
    return map;
  }, [projects]);

  // Only show columns that have projects or are always-visible
  const visibleColumns = useMemo(
    () =>
      STATUS_COLUMNS.filter(
        (col) =>
          ALWAYS_VISIBLE_STATUSES.has(col.status) ||
          (grouped.get(col.status)?.length ?? 0) > 0,
      ),
    [grouped],
  );

  if (isLoading) {
    return (
      <div className="flex flex-1 min-h-0 flex-col">
        <div className="flex h-12 shrink-0 items-center gap-1.5 border-b px-4">
          <Skeleton className="h-5 w-5 rounded" />
          <Skeleton className="h-4 w-32" />
        </div>
        <div className="flex gap-4 p-6">
          {Array.from({ length: 3 }).map((_, i) => (
            <div key={i} className="w-[260px] space-y-3">
              <Skeleton className="h-5 w-24 rounded" />
              <Skeleton className="h-24 w-full rounded-lg" />
              <Skeleton className="h-24 w-full rounded-lg" />
            </div>
          ))}
        </div>
      </div>
    );
  }

  return (
    <div className="flex flex-1 min-h-0 flex-col">
      {/* Breadcrumb header */}
      <div className="flex h-12 shrink-0 items-center gap-1.5 border-b px-4">
        <WorkspaceAvatar name={workspace?.name ?? "W"} size="sm" />
        <span className="text-sm text-muted-foreground">{workspace?.name ?? "Workspace"}</span>
        <ChevronRight className="h-3 w-3 text-muted-foreground" />
        <span className="text-sm font-medium">Projects V2</span>
      </div>

      {/* Toolbar */}
      <div className="flex h-12 shrink-0 items-center justify-between border-b px-4">
        <span className="text-xs text-muted-foreground">
          {projects.length} project{projects.length !== 1 ? "s" : ""}
        </span>
        <Button size="xs" onClick={() => setShowCreate(true)}>
          <Plus className="h-3 w-3" />
          New Project
        </Button>
      </div>

      {/* Board content */}
      {projects.length === 0 ? (
        <div className="flex flex-1 min-h-0 flex-col items-center justify-center gap-2 text-muted-foreground">
          <FolderGit2 className="h-10 w-10 text-muted-foreground/40" />
          <p className="text-sm">No projects yet</p>
          <p className="text-xs">Create a multi-agent project to get started.</p>
          <Button size="xs" className="mt-2" onClick={() => setShowCreate(true)}>
            <Plus className="h-3 w-3" />
            New Project
          </Button>
        </div>
      ) : (
        <div className="flex flex-1 min-h-0 gap-4 overflow-x-auto p-4">
          {visibleColumns.map((col) => (
            <StatusColumn
              key={col.status}
              column={col}
              projects={grouped.get(col.status) ?? []}
              onDelete={handleDelete}
            />
          ))}
        </div>
      )}

      {showCreate && (
        <CreateProjectDialog
          runtimes={runtimes}
          runtimesLoading={runtimesLoading}
          onClose={() => setShowCreate(false)}
          onCreate={handleCreate}
        />
      )}
    </div>
  );
}
