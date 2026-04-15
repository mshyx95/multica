export type ProjectV2Status = 'draft' | 'planning' | 'executing' | 'paused' | 'completed' | 'failed';
export type ProjectAgentRole = 'planner' | 'orchestrator' | 'executor' | 'evaluator';
export type ProjectAgentStatus = 'idle' | 'busy' | 'blocked' | 'error' | 'stopped';
export type SubtaskStatus = 'pending' | 'running' | 'completed' | 'failed' | 'blocked';

export interface ProjectV2 {
  id: string;
  workspace_id: string;
  runtime_id: string | null;
  name: string;
  description: string;
  goals: string;
  skills: unknown[];
  agent_rules: Record<string, unknown>;
  status: ProjectV2Status;
  trd_content: string;
  plan_content: string;
  config: Record<string, unknown>;
  created_by: string | null;
  created_at: string;
  updated_at: string;
}

export interface ProjectAgent {
  id: string;
  project_id: string;
  role: ProjectAgentRole;
  agent_index: number;
  tmux_window: string;
  pid: number | null;
  status: ProjectAgentStatus;
  model: string;
  current_task: string;
  gpu_assignment: string;
  worktree_path: string;
  branch_name: string;
  token_usage: Record<string, unknown>;
  started_at: string | null;
  last_heartbeat: string | null;
  created_at: string;
}

export interface ProjectMessage {
  id: string;
  project_id: string;
  role: 'user' | 'planner' | 'orchestrator' | 'executor' | 'evaluator' | 'system';
  agent_id: string | null;
  content: string;
  phase: 'planning' | 'executing';
  seq: number;
  created_at: string;
}

export interface Subtask {
  id: string;
  project_id: string;
  title: string;
  description: string;
  assigned_to: string | null;
  status: SubtaskStatus;
  file_ownership: string[];
  depends_on: string[];
  result: string;
  created_at: string;
  completed_at: string | null;
}

export interface CreateProjectV2Request {
  name: string;
  description?: string;
  goals?: string;
  skills?: unknown[];
  agent_rules?: Record<string, unknown>;
  config?: Record<string, unknown>;
  runtime_id?: string;
}

export interface DeployProjectV2Request {
  runtime_id: string;
}
