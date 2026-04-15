"use client";

import { useState } from "react";
import { Plus, FolderGit2, ChevronRight } from "lucide-react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspaceStore } from "@multica/core/workspace";
import { useNavigation } from "../../navigation";
import { WorkspaceAvatar } from "../../workspace/workspace-avatar";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { Button } from "@multica/ui/components/ui/button";
import type { ProjectV2, CreateProjectV2Request } from "@multica/core/types";
import { PROJECT_V2_STATUS_CONFIG } from "../config";
import { CreateProjectDialog } from "./create-project-dialog";

function formatRelativeDate(date: string): string {
  const diff = Date.now() - new Date(date).getTime();
  const days = Math.floor(diff / (1000 * 60 * 60 * 24));
  if (days < 1) return "Today";
  if (days === 1) return "1d ago";
  if (days < 30) return `${days}d ago`;
  const months = Math.floor(days / 30);
  return `${months}mo ago`;
}

function ProjectCard({ project }: { project: ProjectV2 }) {
  const nav = useNavigation();
  const statusCfg = PROJECT_V2_STATUS_CONFIG[project.status];

  return (
    <button
      onClick={() => nav.push(`/projects-v2/${project.id}`)}
      className="flex flex-col gap-2 rounded-lg border p-4 text-left transition-colors hover:bg-muted/50"
    >
      <div className="flex items-center justify-between">
        <h3 className="font-medium text-sm truncate">{project.name}</h3>
        <span className={`shrink-0 rounded-full px-2 py-0.5 text-[11px] font-medium ${statusCfg.color} ${statusCfg.bg}`}>
          {statusCfg.label}
        </span>
      </div>
      {project.description && (
        <p className="text-xs text-muted-foreground line-clamp-2">{project.description}</p>
      )}
      <div className="flex items-center gap-3 text-[11px] text-muted-foreground">
        {project.runtime_id && (
          <span className="truncate">Runtime: {project.runtime_id.slice(0, 8)}</span>
        )}
        <span className="ml-auto shrink-0">{formatRelativeDate(project.created_at)}</span>
      </div>
    </button>
  );
}

export function ProjectsV2Page() {
  const wsId = useWorkspaceId();
  const workspace = useWorkspaceStore((s) => s.workspace);
  const qc = useQueryClient();
  const [showCreate, setShowCreate] = useState(false);

  const { data: projects = [], isLoading } = useQuery({
    queryKey: ["projects-v2", wsId],
    queryFn: () => api.listProjectsV2(),
    enabled: !!wsId,
  });

  const handleCreate = async (data: CreateProjectV2Request) => {
    await api.createProjectV2(data);
    qc.invalidateQueries({ queryKey: ["projects-v2", wsId] });
  };

  if (isLoading) {
    return (
      <div className="flex flex-1 min-h-0 flex-col">
        <div className="flex h-12 shrink-0 items-center gap-1.5 border-b px-4">
          <Skeleton className="h-5 w-5 rounded" />
          <Skeleton className="h-4 w-32" />
        </div>
        <div className="grid gap-4 p-6 sm:grid-cols-2 lg:grid-cols-3">
          {Array.from({ length: 6 }).map((_, i) => (
            <Skeleton key={i} className="h-28 rounded-lg" />
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
        <span className="text-xs text-muted-foreground">{projects.length} project{projects.length !== 1 ? "s" : ""}</span>
        <Button size="xs" onClick={() => setShowCreate(true)}>
          <Plus className="h-3 w-3" />
          New Project
        </Button>
      </div>

      {/* Content */}
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
        <div className="flex-1 overflow-y-auto p-4">
          <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
            {projects.map((project) => (
              <ProjectCard key={project.id} project={project} />
            ))}
          </div>
        </div>
      )}

      {showCreate && (
        <CreateProjectDialog
          onClose={() => setShowCreate(false)}
          onCreate={handleCreate}
        />
      )}
    </div>
  );
}
