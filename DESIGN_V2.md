# Multica v2 重构设计文档

## 1. 核心架构变化

### 从 Issue-driven → Project-driven Multi-Agent Orchestration

```
┌─────────────────────────────────────────────────────────────────┐
│                      Web UI (Next.js)                           │
│  ┌──────────┐  ┌──────────────┐  ┌────────────────────────┐    │
│  │ Runtime   │  │ Project 详情 │  │ GPU 监控               │    │
│  │ 列表      │  │ + Planner    │  │ (每台机器每张卡)        │    │
│  │           │  │   对话框     │  │                        │    │
│  │           │  │ + Agent 卡片 │  │                        │    │
│  └──────────┘  │   (状态/日志) │  └────────────────────────┘    │
│                └──────────────┘                                  │
└─────────────────────┬───────────────────────────────────────────┘
                      │ WebSocket + REST API
                      ▼
┌─────────────────────────────────────────────────────────────────┐
│                   Go Backend (multica server)                    │
│                                                                  │
│  ┌────────────┐  ┌──────────────┐  ┌────────────────────────┐   │
│  │ 基础调度    │  │ WebSocket Hub│  │ Project/Agent 管理     │   │
│  │ (Go 状态机) │  │ (已有，复用)  │  │ DB + API              │   │
│  └──────┬─────┘  └──────────────┘  └────────────────────────┘   │
│         │                                                        │
└─────────┼────────────────────────────────────────────────────────┘
          │ HTTP (daemon ↔ server)
          ▼
┌─────────────────────────────────────────────────────────────────┐
│              Runtime (每台 GPU 服务器)                            │
│                                                                  │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │ Daemon (改造后)                                           │   │
│  │ ├── GPU 状态采集 (nvidia-smi, 每30s汇报)                  │   │
│  │ ├── tmux session 管理                                     │   │
│  │ │   ├── planner (copilot 实例, 与用户多轮交互)             │   │
│  │ │   ├── orchestrator-brain (copilot 实例, 智能决策)        │   │
│  │ │   ├── executor-0 (copilot 实例, git worktree-0)         │   │
│  │ │   ├── executor-1 (copilot 实例, git worktree-1)         │   │
│  │ │   └── evaluator (copilot 实例, integration worktree)    │   │
│  │ └── 文件系统通信 (task files, state files)                 │   │
│  └──────────────────────────────────────────────────────────┘   │
│                                                                  │
│  /mnt2/yuxuanhu/multica/                                        │
│  ├── projects/{project-id}/                                      │
│  │   ├── worktree-0/  worktree-1/  integration/                 │
│  │   ├── runtime/agents-status.json                              │
│  │   ├── tasks/executor-0/  tasks/executor-1/  tasks/evaluator/ │
│  │   └── memories/session/ (TRD, plans, state)                  │
│  ├── tmp/                                                        │
│  ├── skills/                                                     │
│  └── agents/                                                     │
└─────────────────────────────────────────────────────────────────┘
```

## 2. 核心概念

### 2.1 Runtime = 一台 GPU 服务器
- 每台服务器跑一个 daemon
- daemon 汇报 GPU 状态 (利用率、显存、温度，每30s)
- 一个 runtime 可承载多个 projects
- Web UI 有独立 GPU 状态页，展示每张卡信息

### 2.2 Project = 核心工作单位（替代 Issue 作为入口）
- 用户在 Web UI 创建 Project（名字、描述、目标）
- 可预先编写 Skills 和 Agent Rules
- 通过 Web UI 分发 Project 到 Runtime
- 分发后自动启动 Harness Agent

### 2.3 Harness Agent = 项目入口 Agent
- 分配 Project 后，daemon 在 tmux 中启动一个 copilot 实例作为 Harness Agent
- Harness Agent 先读取 Project 内容
- 启动 Planner Phase：与用户多轮交互（通过 WebSocket 中转）
- 用户 APPROVE 后，进入 Execution Phase

### 2.4 Planner Phase（用户参与）
- 简化版流程：Clarification → 单模型出计划 → User Review
- 在 Project 详情页的内嵌对话框中进行
- Planner 主动提问，用户回答
- 最终产出：Task Requirements Document (TRD) + Unified Plan
- 用户 APPROVE / REVISE

### 2.5 Execution Phase（全自动，用户可介入）
- Go 后端做基础调度（状态机、心跳检查、超时处理）
- 一个 LLM agent (Orchestrator Brain) 做智能决策
- N 个 Executor (copilot 实例，各自独立 git worktree + 分支)
- 1 个 Evaluator (copilot 实例，integration worktree，负责合并+测试)
- 全部通过 tmux 窗口管理
- 通过文件系统通信 (task files, report files, state files)
- 用户可随时通过对话框发消息介入（Direction Correction）

### 2.6 Issue 降级为子任务
- 用户不直接创建 Issue
- Planner 拆解 Project 为子任务（内部用 Issue 或 subtask 表示）
- 分配给各 Executor 执行

## 3. 数据模型变化

### 新增表

```sql
-- Runtime GPU 状态
CREATE TABLE gpu_status (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    runtime_id UUID NOT NULL REFERENCES agent_runtimes(id),
    gpu_index INT NOT NULL,
    gpu_name TEXT,
    utilization_pct INT,        -- GPU 利用率 %
    memory_used_mb INT,
    memory_total_mb INT,
    temperature_c INT,
    power_draw_w INT,
    process_info JSONB,         -- 哪个 agent/进程在用这张卡
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

-- Project (v2 - 核心工作单位)
CREATE TABLE projects_v2 (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    runtime_id UUID,            -- 分配到哪台服务器
    name TEXT NOT NULL,
    description TEXT,
    goals TEXT,                  -- 项目目标
    skills JSONB,               -- 预设的 skills
    agent_rules JSONB,          -- agent 行为规则
    status TEXT DEFAULT 'draft', -- draft/planning/executing/paused/completed/failed
    trd_content TEXT,           -- Planner 产出的 TRD
    plan_content TEXT,          -- 最终计划
    config JSONB,               -- executor 数量、模型选择、effort level 等
    created_by UUID,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

-- Project Agents (Planner/Orchestrator/Executor/Evaluator)
CREATE TABLE project_agents (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL REFERENCES projects_v2(id),
    role TEXT NOT NULL,          -- planner/orchestrator/executor/evaluator
    index INT DEFAULT 0,        -- executor-0, executor-1, ...
    tmux_window TEXT,           -- tmux 窗口名
    pid INT,                    -- copilot 进程 PID
    status TEXT DEFAULT 'idle', -- idle/busy/blocked/error/stopped
    model TEXT,                 -- 使用的模型
    current_task TEXT,          -- 当前执行的 subtask 描述
    gpu_assignment TEXT,        -- 分配的 GPU (e.g. "1,2,3")
    worktree_path TEXT,         -- git worktree 路径
    branch_name TEXT,
    started_at TIMESTAMPTZ,
    last_heartbeat TIMESTAMPTZ,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

-- Project 对话消息（Planner 多轮 + 执行期间介入）
CREATE TABLE project_messages (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL REFERENCES projects_v2(id),
    role TEXT NOT NULL,          -- user/planner/orchestrator/executor/evaluator/system
    agent_id UUID,              -- 哪个 agent 发的
    content TEXT NOT NULL,
    phase TEXT,                 -- planning/executing
    seq INT,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

-- Subtasks（Planner 拆解出来的）
CREATE TABLE subtasks (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL REFERENCES projects_v2(id),
    title TEXT NOT NULL,
    description TEXT,
    assigned_to UUID,           -- project_agents.id
    status TEXT DEFAULT 'pending', -- pending/running/completed/failed/blocked
    file_ownership JSONB,       -- 哪些文件归这个 subtask
    depends_on UUID[],          -- 依赖的其他 subtask
    result TEXT,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    completed_at TIMESTAMPTZ
);
```

## 4. 文件系统布局

所有输出放在 `/mnt2/yuxuanhu/multica/`：

```
/mnt2/yuxuanhu/multica/
├── projects/
│   └── {project-id}/
│       ├── worktree-0/              # Executor-0 的 git worktree
│       ├── worktree-1/              # Executor-1 的 git worktree
│       ├── integration/             # Evaluator 的合并 worktree
│       ├── runtime/
│       │   ├── agents-status.json   # 所有 agent 的聚合状态
│       │   └── state-executor-0.json
│       ├── tasks/
│       │   ├── executor-0/          # Orchestrator → Executor-0 的任务文件
│       │   ├── executor-1/
│       │   └── evaluator/
│       └── memories/
│           └── session/
│               ├── harness-trd.md
│               ├── harness-unified-plan.md
│               ├── harness-state.md
│               ├── results.md       # 最终结果
│               └── session_log.md
├── skills/                          # 用户预写的 skills
│   ├── {skill-name}/SKILL.md
│   └── ...
├── agents/                          # Agent rule 模板
│   ├── executor.agent.md
│   ├── evaluator.agent.md
│   └── planner.agent.md
└── tmp/                             # 临时文件
```

## 5. 核心流程

### 5.1 Project 创建与分发

```
用户 Web UI → 创建 Project (名字、描述、goals)
           → 编写/选择 Skills
           → 选择 Runtime (GPU 服务器)
           → 配置: executor 数量、模型、effort level
           → 点击 "Deploy" 分发到 Runtime
```

### 5.2 Planner Phase (多轮交互)

```
用户点 Deploy → server 通知 daemon → daemon 在 tmux 启动 Planner copilot
Planner 读取 Project 内容 + Skills
Planner 提问 → daemon → server → WS → Web UI 对话框
用户回答 → WS → server → daemon → 写入 Planner stdin
... (多轮)
Planner 输出 TRD + Plan → 展示在 Web UI
用户 APPROVE → 进入 Execution Phase
用户 REVISE → Planner 修改计划
```

### 5.3 Execution Phase (全自动)

```
daemon 创建 tmux session
  → 启动 Orchestrator Brain (copilot 实例)
  → 启动 N 个 Executor (copilot 实例, 各自 git worktree)
  → 启动 1 个 Evaluator (copilot 实例)
  → 启动 bash 后台监控 (5s 采集 agent 状态)

Orchestrator 心跳循环 (15-30s):
  1. 读 agents-status.json
  2. 对空闲 executor: 分配下一个 subtask (写 task file)
  3. 对完成的 executor: 读 report → 派 evaluator 审查
  4. Evaluator 合并代码到 integration branch
  5. 检查文档合规、时间阈值
  6. 重复直到所有 subtask 完成

daemon 定期上报:
  - Agent 状态 (busy/idle/error) → server → WS → Web UI
  - 日志/输出 → server → 可在 Web UI 查看
  - 用户随时可发消息介入 → Orchestrator 处理
```

## 6. Daemon 改造要点

### 6.1 新增 GPU 采集器
- 每 30s 执行 `nvidia-smi --query-gpu=... --format=csv`
- 解析并上报到 server
- 包含：gpu_index, name, utilization, memory, temperature, power, processes

### 6.2 tmux Agent 管理
- 创建 tmux session: `multica-{project-id}`
- 每个 agent 一个 window: `planner`, `orchestrator`, `executor-0`, `executor-1`, `evaluator`
- 启动命令: `copilot -p "..." --output-format json --allow-all --model {model} --effort {effort}`
- 状态检测: 读 per-agent state files + tmux capture-pane
- 日志采集: 解析 JSONL 输出，上报到 server

### 6.3 Planner 双向通信
- Planner 输出 → daemon 解析 JSONL → 上报 server → WS push 到前端
- 用户输入 → WS → server → daemon → tmux send-keys 到 Planner 窗口
- 需要新的 daemon ↔ server 协议：`/api/daemon/projects/{id}/messages`

### 6.4 去掉 multica agent CLI 约束
- Agent 不再通过 `multica issue get` 等命令交互
- 通过文件系统 (task files) 和 tmux 通信
- Prompt 中不再注入 multica CLI 指令
- 改为注入 harness protocol 和 project context

## 7. Web UI 改造要点

### 7.1 新增页面/组件
- **Runtime GPU 状态页**: 每台服务器的 GPU 卡信息（利用率/显存/温度）
- **Project 详情页**: 
  - 内嵌对话框（Planner 多轮 + 执行期间介入）
  - Agent 卡片面板（显示所有 planner/executor/evaluator 的状态）
  - 点击 Agent 卡片 → 展开日志/输出
  - Subtask 列表（状态、分配情况）
  - results.md 查看
- **Project 创建/配置页**:
  - Skills 编辑器
  - Agent Rules 编辑器
  - Runtime 选择（带 GPU 状态预览）
  - Executor 数量、模型、effort 配置

### 7.2 保留/复用
- WebSocket Hub (实时推送)
- 认证系统
- 工作区隔离
- shadcn/ui 组件库
- 现有 Layout/导航结构

## 8. 实现分期

### Phase 1: 核心流程 (MVP)
1. 数据库 migration（新表）
2. Daemon: GPU 采集 + 上报
3. Server: GPU 状态 API + Project CRUD API
4. Web UI: Runtime GPU 状态页 + Project 创建页
5. Daemon: tmux Planner 启动 + 双向消息中转
6. Web UI: Project 对话框（Planner 多轮交互）
7. Daemon: Execution Phase（Orchestrator + Executor + Evaluator tmux 管理）
8. Web UI: Agent 状态卡片 + 日志查看

### Phase 2: 增强功能
9. Direction Correction（执行期间用户介入）
10. Subtask 可视化（依赖关系图）
11. results.md 渲染
12. 历史对话查看
13. Project 模板

### Phase 3: 高级功能
14. 多模型 Planner 对比（恢复 harness v2 的 multi-model planning）
15. 跨 Runtime 调度（一个 Project 跨多台 GPU 服务器）
16. 自动重试和回滚
