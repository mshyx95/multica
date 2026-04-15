-- Multica V2: GPU monitoring, project-driven orchestration, multi-agent execution

-- Runtime GPU status (reported by daemon every 30s)
CREATE TABLE IF NOT EXISTS gpu_status (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    runtime_id UUID NOT NULL REFERENCES agent_runtime(id) ON DELETE CASCADE,
    gpu_index INT NOT NULL,
    gpu_name TEXT NOT NULL DEFAULT '',
    utilization_pct INT NOT NULL DEFAULT 0,
    memory_used_mb INT NOT NULL DEFAULT 0,
    memory_total_mb INT NOT NULL DEFAULT 0,
    temperature_c INT NOT NULL DEFAULT 0,
    power_draw_w INT NOT NULL DEFAULT 0,
    process_info JSONB,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (runtime_id, gpu_index)
);

-- Project V2 (core work unit replacing Issue as entry point)
CREATE TABLE IF NOT EXISTS project_v2 (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    runtime_id UUID REFERENCES agent_runtime(id) ON DELETE SET NULL,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    goals TEXT NOT NULL DEFAULT '',
    skills JSONB DEFAULT '[]',
    agent_rules JSONB DEFAULT '{}',
    status TEXT NOT NULL DEFAULT 'draft',
    trd_content TEXT NOT NULL DEFAULT '',
    plan_content TEXT NOT NULL DEFAULT '',
    config JSONB NOT NULL DEFAULT '{}',
    created_by UUID REFERENCES "user"(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_project_v2_workspace ON project_v2(workspace_id);
CREATE INDEX IF NOT EXISTS idx_project_v2_runtime ON project_v2(runtime_id);
CREATE INDEX IF NOT EXISTS idx_project_v2_status ON project_v2(status);

-- Project agents (planner/orchestrator/executor/evaluator instances)
CREATE TABLE IF NOT EXISTS project_agent (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL REFERENCES project_v2(id) ON DELETE CASCADE,
    role TEXT NOT NULL,
    agent_index INT NOT NULL DEFAULT 0,
    tmux_window TEXT NOT NULL DEFAULT '',
    pid INT,
    status TEXT NOT NULL DEFAULT 'idle',
    model TEXT NOT NULL DEFAULT '',
    current_task TEXT NOT NULL DEFAULT '',
    gpu_assignment TEXT NOT NULL DEFAULT '',
    worktree_path TEXT NOT NULL DEFAULT '',
    branch_name TEXT NOT NULL DEFAULT '',
    token_usage JSONB DEFAULT '{}',
    started_at TIMESTAMPTZ,
    last_heartbeat TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_project_agent_project ON project_agent(project_id);
CREATE INDEX IF NOT EXISTS idx_project_agent_status ON project_agent(status);

-- Project messages (planner multi-round Q&A + execution-phase user intervention)
CREATE TABLE IF NOT EXISTS project_message (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL REFERENCES project_v2(id) ON DELETE CASCADE,
    role TEXT NOT NULL,
    agent_id UUID REFERENCES project_agent(id) ON DELETE SET NULL,
    content TEXT NOT NULL,
    phase TEXT NOT NULL DEFAULT 'planning',
    seq INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_project_message_project ON project_message(project_id);
CREATE INDEX IF NOT EXISTS idx_project_message_seq ON project_message(project_id, seq);

-- Subtasks (created by Planner, assigned to Executors)
CREATE TABLE IF NOT EXISTS subtask (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL REFERENCES project_v2(id) ON DELETE CASCADE,
    title TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    assigned_to UUID REFERENCES project_agent(id) ON DELETE SET NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    file_ownership JSONB DEFAULT '[]',
    depends_on UUID[] DEFAULT '{}',
    result TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_subtask_project ON subtask(project_id);
CREATE INDEX IF NOT EXISTS idx_subtask_status ON subtask(status);
CREATE INDEX IF NOT EXISTS idx_subtask_assigned ON subtask(assigned_to);
