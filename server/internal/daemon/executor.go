package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	orchestratorWindow = "orchestrator"
	evaluatorWindow    = "evaluator"
	executorPrefix     = "executor"
	monitorInterval    = 10 * time.Second

	// Timeouts for tmux agent launch sequence.
	shellInitTimeout   = 30 * time.Second
	copilotReadyTimeout = 120 * time.Second
)

// ExecutionConfig holds the configuration for an execution phase.
type ExecutionConfig struct {
	ProjectID      string
	ProjectName    string            // human-readable project name
	SessionName    string
	ProjectDir     string
	RepoURL        string            // git repo to checkout
	NumExecutors   int
	Models         map[string]string // role -> model override
	Effort         string            // reasoning effort level
	Plan           string            // the approved plan content
	ProjectContext string            // content of project-context.md
	Skills         string            // concatenated skills content
}

// agentLaunchInfo describes one agent to launch in a tmux window.
type agentLaunchInfo struct {
	WindowName string
	WorkDir    string
	Model      string
	InitFile   string // path to the init prompt file
}

// startExecution sets up the full Harness Mode V2 execution phase:
// directory structure, git worktrees, tmux agent launch (with proper pitfall avoidance),
// init prompt files, bash monitor, and the monitoring goroutine.
func (d *Daemon) startExecution(ctx context.Context, cfg ExecutionConfig) error {
	d.logger.Info("starting execution phase",
		"project_id", cfg.ProjectID,
		"executors", cfg.NumExecutors,
	)

	baseDir := cfg.ProjectDir
	copilotEntry, ok := d.cfg.Agents["copilot"]
	if !ok {
		return fmt.Errorf("copilot agent not configured")
	}
	copilotPath := copilotEntry.Path

	// ── a) Create project directory structure ──────────────────────────
	dirs := []string{
		"runtime",
		"memories/session",
	}
	for i := 0; i < cfg.NumExecutors; i++ {
		dirs = append(dirs, fmt.Sprintf("tasks/executor-%d", i))
	}
	dirs = append(dirs, "tasks/evaluator")
	for _, sub := range dirs {
		if err := os.MkdirAll(filepath.Join(baseDir, sub), 0o755); err != nil {
			return fmt.Errorf("create dir %s: %w", sub, err)
		}
	}

	// Write the approved plan.
	planPath := filepath.Join(baseDir, "memories", "session", "harness-unified-plan.md")
	if err := atomicWriteFile(planPath, []byte(cfg.Plan)); err != nil {
		return fmt.Errorf("write plan: %w", err)
	}

	// Write initial harness-state.md.
	stateContent := fmt.Sprintf("# Harness State\n\nproject_id: %s\nexecutors: %d\nphase: executing\nstarted_at: %s\n",
		cfg.ProjectID, cfg.NumExecutors, time.Now().UTC().Format(time.RFC3339))
	statePath := filepath.Join(baseDir, "memories", "session", "harness-state.md")
	if err := atomicWriteFile(statePath, []byte(stateContent)); err != nil {
		return fmt.Errorf("write harness-state.md: %w", err)
	}

	// Write initial per-agent state files.
	for i := 0; i < cfg.NumExecutors; i++ {
		sf := filepath.Join(baseDir, "runtime", fmt.Sprintf("state-executor-%d.json", i))
		writeJSONFile(sf, map[string]any{"agent": fmt.Sprintf("executor-%d", i), "status": "idle", "updated_at": time.Now().UTC().Format(time.RFC3339)})
	}
	writeJSONFile(filepath.Join(baseDir, "runtime", "state-evaluator.json"),
		map[string]any{"agent": "evaluator", "status": "idle", "updated_at": time.Now().UTC().Format(time.RFC3339)})
	writeJSONFile(filepath.Join(baseDir, "runtime", "state-orchestrator.json"),
		map[string]any{"agent": "orchestrator", "status": "running", "updated_at": time.Now().UTC().Format(time.RFC3339)})

	// Write agents-status.json (atomic).
	initStatus := map[string]string{}
	for i := 0; i < cfg.NumExecutors; i++ {
		initStatus[fmt.Sprintf("executor-%d", i)] = "idle"
	}
	initStatus["evaluator"] = "idle"
	initStatus["orchestrator"] = "running"
	statusJSON, _ := json.MarshalIndent(initStatus, "", "  ")
	statusFile := filepath.Join(baseDir, "runtime", "agents-status.json")
	if err := atomicWriteFile(statusFile, statusJSON); err != nil {
		return fmt.Errorf("write agents-status.json: %w", err)
	}

	// Write orchestrator heartbeat initial file.
	writeJSONFile(filepath.Join(baseDir, "runtime", "orchestrator-heartbeat.json"),
		map[string]any{"status": "initializing", "updated_at": time.Now().UTC().Format(time.RFC3339)})

	// ── b) Ensure tmux session exists ──────────────────────────────────
	if !tmuxHasSession(cfg.SessionName) {
		if err := tmuxCreateSession(cfg.SessionName); err != nil {
			return fmt.Errorf("create tmux session: %w", err)
		}
	}

	// ── c) Git worktree setup ──────────────────────────────────────────
	if cfg.RepoURL != "" {
		if err := d.setupWorktrees(ctx, cfg); err != nil {
			d.logger.Warn("git worktree setup had errors", "error", err)
		}
	}

	// ── d) Write init prompt files ─────────────────────────────────────
	agents := make([]agentLaunchInfo, 0, cfg.NumExecutors+2)

	// Executors
	for i := 0; i < cfg.NumExecutors; i++ {
		windowName := fmt.Sprintf("%s-%d", executorPrefix, i)
		wtDir := filepath.Join(baseDir, fmt.Sprintf("worktree-%d", i))
		if _, err := os.Stat(wtDir); os.IsNotExist(err) {
			wtDir = baseDir
		}

		execModel := cfg.Models[fmt.Sprintf("executor-%d", i)]
		if execModel == "" {
			execModel = cfg.Models["executor"]
		}
		if execModel == "" {
			execModel = "claude-sonnet-4"
		}

		initFile := filepath.Join(baseDir, "tasks", fmt.Sprintf("executor-%d", i), "init.md")
		initContent := fmt.Sprintf(`# Executor-%d Operating Manual — Harness Mode V2

## Identity
You are Executor-%d in a multi-agent orchestration system. You work on ONE subtask at a time in your dedicated workspace.

## Project Context
%s

## Plan
%s

## Your Workspace
- Working directory: %s
- Task inbox: %s/tasks/executor-%d/
- Your state file: %s/runtime/state-executor-%d.json

## Task Execution Protocol

### Receiving Tasks
Monitor your task inbox for new `+"`task-*.md`"+` files. When one appears:
1. Read the task file completely
2. Update your state file: `+"`"+`{"agent_id":"executor-%d","status":"running","current_task_id":"{id}","heartbeat":"{timestamp}"}`+"`"+`
3. Execute the task following the acceptance criteria

### Completing Tasks
When you finish a task:
1. Write your report to `+"`report-{id}.md.tmp`"+` (ATOMIC — always write .tmp first!)
2. `+"`mv report-{id}.md.tmp report-{id}.md`"+`
3. Update state file: `+"`"+`{"status":"report_ready","last_report_path":"...","heartbeat":"{timestamp}"}`+"`"+`
4. Wait for Orchestrator to acknowledge (will set you back to "idle")

### Report Format (MANDATORY)
Every report MUST include:
`+"```"+`markdown
# Report: {TASK_ID}
## Metadata
- task_id: {id}
- agent_id: executor-%d
- status: complete | partial | blocked
## Changes
| File | Action | Summary |
## Self-Check
| Criterion | Passed | Evidence |
## Documentation
### optimization_results entry
(The Orchestrator will copy this to the main file)
## Notes
`+"```"+`

### When Blocked
If you cannot proceed:
1. Write a `+"`questions:`"+` block in your report
2. Set state to "blocked" with error field describing the issue
3. The Orchestrator will route your question to the Evaluator — do NOT ask the user
4. When answer arrives in a new task file, continue

### Critical Rules
- NEVER ask the user anything — route all questions through your report
- Always use atomic file operations (.tmp + mv)
- Commit code changes: `+"`git add -A && git commit -m \"[harness] {task_id}: {description}\"`"+`
- Update state file after EVERY status transition
- If a process produces no output for 10 minutes, diagnose: check `+"`ps aux`"+`, `+"`nvidia-smi`"+`, `+"`dmesg`"+`

## Skills
%s

You are now idle. Update your state file to idle and wait for your first task.
`, i, i, cfg.ProjectContext, cfg.Plan, wtDir,
			baseDir, i, baseDir, i,
			i, i, cfg.Skills)
		if err := atomicWriteFile(initFile, []byte(initContent)); err != nil {
			return fmt.Errorf("write executor-%d init file: %w", i, err)
		}

		agents = append(agents, agentLaunchInfo{
			WindowName: windowName,
			WorkDir:    wtDir,
			Model:      execModel,
			InitFile:   initFile,
		})
	}

	// Evaluator
	intDir := filepath.Join(baseDir, "integration")
	if _, err := os.Stat(intDir); os.IsNotExist(err) {
		intDir = baseDir
	}
	evalModel := cfg.Models["evaluator"]
	if evalModel == "" {
		evalModel = "gpt-5.3-codex"
	}
	evalInitFile := filepath.Join(baseDir, "tasks", "evaluator", "init.md")
	evalInitContent := fmt.Sprintf(`# Evaluator Operating Manual — Harness Mode V2

## Identity
You are the Evaluator — the QUALITY GATE and STRATEGIC ADVISOR. Your decisions determine whether code enters the main branch.

## Project Context
%s

## Plan
%s

## Your Workspace
- Integration worktree: %s
- Task inbox: %s/tasks/evaluator/
- Your state file: %s/runtime/state-evaluator.json

## Operating Modes

### Mode: EVALUATE (default)
Review an executor's work. Be ADVERSARIAL — assume mistakes until proven otherwise.
1. Read the executor's report
2. Review the diff: `+"`git diff harness/integration..harness/executor-{N}`"+`
3. Check EACH acceptance criterion
4. Check for regressions
5. Write verdict:
`+"```"+`yaml
verdict: ACCEPT | REJECT
next_action: CONTINUE | RETRY | SKIP
`+"```"+`
On REJECT: provide concrete fix instructions, 2-3 alternative approaches, specific files+lines.
On ACCEPT: explain what was good, suggest improvements for upcoming subtasks.
**REJECT if documentation section is missing.**

### Mode: MERGE
After ACCEPT:
1. `+"`git merge harness/executor-{N}`"+` in integration worktree
2. Resolve conflicts using context from evaluation
3. Run tests
4. Report merge result (success/conflict_resolved/failed)

### Mode: ANSWER
When an executor is blocked:
- Provide a DECISIVE, ACTIONABLE answer
- Include code snippets or step-by-step guidance
- Answer must be COMPLETE enough that executor never asks a follow-up

### Mode: DRIFT-CHECK
Monitor implementation drift from TRD:
`+"```"+`yaml
drift_report:
  overall_alignment: on-track | minor-drift | major-drift
  findings: [{area, severity, correction}]
`+"```"+`

## Report Format
Same as executor — include metadata, changes, self-check, documentation sections.

## Critical Rules
- YOU are the decision-maker — if you catch yourself writing a question to the user, STOP and rewrite as a decision with rationale
- NEVER give shallow ACCEPT ("looks good") — explain WHAT is good
- NEVER give vague REJECT ("needs fixes") — provide exact fix steps
- After 2+ failed attempts → recommend PIVOT to different strategy
- Use atomic file operations (.tmp + mv)

## Skills
%s

You are now idle. Update your state file and wait for evaluation tasks.
`, cfg.ProjectContext, cfg.Plan, intDir,
		baseDir, baseDir, cfg.Skills)
	if err := atomicWriteFile(evalInitFile, []byte(evalInitContent)); err != nil {
		return fmt.Errorf("write evaluator init file: %w", err)
	}
	agents = append(agents, agentLaunchInfo{
		WindowName: evaluatorWindow,
		WorkDir:    intDir,
		Model:      evalModel,
		InitFile:   evalInitFile,
	})

	// Orchestrator
	orchModel := cfg.Models["orchestrator"]
	if orchModel == "" {
		orchModel = "claude-opus-4.6-1m"
	}
	orchInitFile := filepath.Join(baseDir, "runtime", "orchestrator-init.md")
	orchInitContent := fmt.Sprintf(`# Orchestrator Brain Operating Manual — Harness Mode V2

## Identity
You are the Orchestrator Brain. You manage %d Executors and 1 Evaluator. You are a clear-headed strategic general who actively manages their soldiers.

## Project Context
%s

## Plan
%s

## Directory Layout
- Output directory: %s
- Plan: %s/memories/session/harness-unified-plan.md
- Status: %s/runtime/agents-status.json
- Your heartbeat: %s/runtime/orchestrator-heartbeat.json
- Optimization results: %s/memories/session/optimization_results.md (YOU are the sole writer)

## Heartbeat Loop Protocol

Run this loop CONTINUOUSLY (every 15-30 seconds):

### Step 1: READ STATUS
`+"`"+`cat %s/runtime/agents-status.json`+"`"+`

### Step 2: PROCESS REPORT_READY AGENTS
For each agent with status "report_ready":
1. Read their report file
2. Check documentation section exists (REJECT if missing)
3. If executor → dispatch evaluation task to Evaluator
4. If evaluator (eval complete) → ACCEPT: dispatch merge task. REJECT: send retry to executor
5. If evaluator (merge complete) → verify main branch updated, mark subtask done

### Step 3: PROCESS IDLE EXECUTORS
For each executor with status "idle":
1. Check subtask queue for next available task
2. Verify no file overlap with running tasks (disjoint parallelism)
3. Write task file: `+"`"+`%s/tasks/executor-{N}/task-{id}.md`+"`"+`
4. Include full optimization_results.md in the task file
5. Send tmux command (TEXT and ENTER separate!):
   `+"`"+`tmux send-keys -t %s:executor-{N} "阅读 task file 并执行"`+"`"+`
   sleep 1
   `+"`"+`tmux send-keys -t %s:executor-{N} Enter`+"`"+`
6. Verify task started (wait for response)

### Step 4: PROCESS BLOCKED AGENTS
Route questions to Evaluator in ANSWER mode.

### Step 5: CHECK TIME & DOCS
- Verify optimization_results.md was updated after each completion
- If missing → send "补充文档" task
- Check time threshold

### Step 6: UPDATE HEARTBEAT
Write to orchestrator-heartbeat.json: loop iteration, tasks completed/remaining, elapsed time

### Step 7: SLEEP 15s → GOTO Step 1

## Task File Format (when dispatching to executors)
`+"```"+`markdown
# Task: {ID}
## Metadata
- task_id: {id}
- agent_id: executor-{N}
- type: implement
## Subtask Details
{from plan}
## Acceptance Criteria
{from plan}
## Context
### optimization_results.md
{FULL content — MANDATORY}
## Instructions
Implement, commit, write report, update state.
`+"```"+`

## Critical Rules — NON-STOP AUTONOMY
- NEVER stop, pause, or ask the user anything after starting
- NEVER present partial progress — only the FINAL report
- Route ALL questions to Evaluator
- Continue until: all subtasks done, evaluator says stop, or 10 consecutive failures
- optimization_results.md: YOU are the SOLE WRITER (prevents race conditions)
- Give feedback to agents: positive for good work, negative with specifics for bad work

## Skills
%s

Now read the plan file and begin the heartbeat loop.
`, cfg.NumExecutors, cfg.ProjectContext, cfg.Plan,
		baseDir, baseDir, baseDir, baseDir, baseDir,
		baseDir, baseDir,
		cfg.SessionName, cfg.SessionName,
		cfg.Skills)
	if err := atomicWriteFile(orchInitFile, []byte(orchInitContent)); err != nil {
		return fmt.Errorf("write orchestrator init file: %w", err)
	}
	agents = append(agents, agentLaunchInfo{
		WindowName: orchestratorWindow,
		WorkDir:    baseDir,
		Model:      orchModel,
		InitFile:   orchInitFile,
	})

	// ── e) Launch agents in tmux (following all pitfalls) ──────────────
	for _, ag := range agents {
		if err := d.launchCopilotInWindow(ctx, cfg.SessionName, ag, copilotPath, cfg.Effort); err != nil {
			d.logger.Warn("failed to launch agent", "window", ag.WindowName, "error", err)
			// Continue — best effort for other agents.
		}
	}

	// Disable auto-rename so tmux doesn't overwrite our window names.
	exec.Command("tmux", "set-option", "-t", cfg.SessionName, "-g", "allow-rename", "off").Run()

	// ── f) Write and start bash monitor ────────────────────────────────
	monitorScript := generateMonitorScript(baseDir, cfg.SessionName, cfg.NumExecutors)
	monitorPath := filepath.Join(baseDir, "runtime", "harness-monitor.sh")
	if err := atomicWriteFile(monitorPath, []byte(monitorScript)); err != nil {
		return fmt.Errorf("write monitor script: %w", err)
	}
	os.Chmod(monitorPath, 0o755)

	// Launch the monitor in the orchestrator window background.
	monitorCmd := fmt.Sprintf("bash %s &", monitorPath)
	tmuxSendKeysV2(cfg.SessionName, orchestratorWindow, monitorCmd)

	d.logger.Info("execution phase started",
		"project_id", cfg.ProjectID,
		"session", cfg.SessionName,
		"orchestrator", orchModel,
		"executors", cfg.NumExecutors,
		"evaluator", evalModel,
	)

	// Start terminal relays for all agent windows.
	for _, ag := range agents {
		windowSession := fmt.Sprintf("%s:%s", cfg.SessionName, ag.WindowName)
		go d.startTerminalRelay(ctx, windowSession)
	}

	// Start the monitoring goroutine.
	go d.monitorExecution(ctx, cfg.ProjectID, baseDir)

	return nil
}

// launchCopilotInWindow creates a tmux window, waits for shell init,
// launches copilot, waits for it to be ready, sends /allow-all,
// then sends the init prompt. This follows all tmux pitfalls from the protocol.
func (d *Daemon) launchCopilotInWindow(ctx context.Context, sessionName string, ag agentLaunchInfo, copilotPath, effort string) error {
	target := fmt.Sprintf("%s:%s", sessionName, ag.WindowName)

	// 1. Create tmux window (empty shell).
	if err := exec.Command("tmux", "new-window", "-t", sessionName, "-n", ag.WindowName).Run(); err != nil {
		return fmt.Errorf("create window %s: %w", ag.WindowName, err)
	}

	// 2. Wait for shell init — poll until bash is idle (no child processes).
	if err := waitForShellInit(ctx, target); err != nil {
		d.logger.Warn("shell init wait failed, proceeding anyway", "window", ag.WindowName, "error", err)
	}

	// 3. Send copilot launch command (Pitfall 1: text and Enter SEPARATE).
	copilotCmd := buildCopilotLaunchCmd(copilotPath, ag.Model, effort, ag.WorkDir)
	if err := exec.Command("tmux", "send-keys", "-t", target, copilotCmd, "").Run(); err != nil {
		return fmt.Errorf("send copilot cmd text: %w", err)
	}
	sleepWithContext(ctx, 1*time.Second)
	if err := exec.Command("tmux", "send-keys", "-t", target, "Enter").Run(); err != nil {
		return fmt.Errorf("send copilot cmd enter: %w", err)
	}

	// 4. Wait for copilot ready — poll for "/ commands" in capture-pane.
	if err := waitForCopilotReady(ctx, target); err != nil {
		d.logger.Warn("copilot ready wait timed out, proceeding", "window", ag.WindowName, "error", err)
	}

	// 5. Send /allow-all (text, sleep, Enter, sleep).
	exec.Command("tmux", "send-keys", "-t", target, "/allow-all", "").Run()
	sleepWithContext(ctx, 1*time.Second)
	exec.Command("tmux", "send-keys", "-t", target, "Enter").Run()
	sleepWithContext(ctx, 5*time.Second)

	// 6. Send init prompt referencing the init file.
	initMsg := fmt.Sprintf("阅读 %s 并按照指示执行", ag.InitFile)
	exec.Command("tmux", "send-keys", "-t", target, initMsg, "").Run()
	sleepWithContext(ctx, 1*time.Second)
	exec.Command("tmux", "send-keys", "-t", target, "Enter").Run()

	d.logger.Info("agent launched", "window", ag.WindowName, "model", ag.Model)
	return nil
}

// buildCopilotLaunchCmd builds the copilot CLI launch command string.
// Unlike the old buildCopilotCmd, this does NOT include -i with a prompt
// since the prompt is sent via task files after launch.
func buildCopilotLaunchCmd(copilotPath, model, effort, cwd string) string {
	cmd := fmt.Sprintf("cd %s && %s --allow-all", shellEscape(cwd), copilotPath)
	if model != "" {
		cmd += fmt.Sprintf(" --model %s", model)
	}
	if effort != "" {
		cmd += fmt.Sprintf(" --effort %s", effort)
	}
	return cmd
}

// waitForShellInit polls until the tmux pane has an idle bash shell.
func waitForShellInit(ctx context.Context, target string) error {
	deadline := time.After(shellInitTimeout)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline:
			return fmt.Errorf("shell init timeout for %s", target)
		case <-ticker.C:
			capture, err := exec.Command("tmux", "capture-pane", "-t", target, "-p", "-S", "-5").Output()
			if err != nil {
				continue
			}
			text := strings.TrimSpace(string(capture))
			// Shell is ready if we see a prompt ending with $ or #
			if strings.HasSuffix(text, "$") || strings.HasSuffix(text, "#") {
				return nil
			}
		}
	}
}

// waitForCopilotReady polls until copilot is ready (shows "/ commands" or similar).
func waitForCopilotReady(ctx context.Context, target string) error {
	deadline := time.After(copilotReadyTimeout)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline:
			return fmt.Errorf("copilot ready timeout for %s", target)
		case <-ticker.C:
			capture, err := exec.Command("tmux", "capture-pane", "-t", target, "-p", "-S", "-30").Output()
			if err != nil {
				continue
			}
			text := string(capture)
			// Look for copilot ready indicators.
			if strings.Contains(text, "/ commands") ||
				strings.Contains(text, "/help") ||
				strings.Contains(text, "What would you like") ||
				strings.Contains(text, "How can I help") {
				return nil
			}
		}
	}
}

// tmuxSendKeysV2 sends text then Enter as separate commands (Pitfall 1).
func tmuxSendKeysV2(sessionName, windowName, text string) {
	target := fmt.Sprintf("%s:%s", sessionName, windowName)
	exec.Command("tmux", "send-keys", "-t", target, text, "").Run()
	time.Sleep(1 * time.Second)
	exec.Command("tmux", "send-keys", "-t", target, "Enter").Run()
}

// shellEscape wraps a path in single quotes for safe shell usage.
func shellEscape(s string) string {
	return "'" + s + "'"
}

// setupWorktrees creates a bare clone and per-executor worktrees with harness branch names.
func (d *Daemon) setupWorktrees(ctx context.Context, cfg ExecutionConfig) error {
	baseDir := cfg.ProjectDir
	bareDir := filepath.Join(baseDir, ".bare-repo")

	// Clone bare repo.
	if _, err := os.Stat(bareDir); os.IsNotExist(err) {
		cmd := exec.CommandContext(ctx, "git", "clone", "--bare", cfg.RepoURL, bareDir)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("git bare clone: %w (%s)", err, string(out))
		}
	}

	// Create worktrees for each executor.
	for i := 0; i < cfg.NumExecutors; i++ {
		wtDir := filepath.Join(baseDir, fmt.Sprintf("worktree-%d", i))
		branch := fmt.Sprintf("harness/executor-%d", i)
		if _, err := os.Stat(wtDir); os.IsNotExist(err) {
			cmd := exec.CommandContext(ctx, "git", "-C", bareDir, "worktree", "add", "-b", branch, wtDir, "HEAD")
			if out, err := cmd.CombinedOutput(); err != nil {
				d.logger.Warn("git worktree failed", "executor", i, "error", err, "output", string(out))
			}
		}
	}

	// Create integration worktree.
	intDir := filepath.Join(baseDir, "integration")
	if _, err := os.Stat(intDir); os.IsNotExist(err) {
		cmd := exec.CommandContext(ctx, "git", "-C", bareDir, "worktree", "add", "-b", "harness/integration", intDir, "HEAD")
		if out, err := cmd.CombinedOutput(); err != nil {
			d.logger.Warn("git integration worktree failed", "error", err, "output", string(out))
		}
	}

	return nil
}

// monitorExecution reads agents-status.json and orchestrator-heartbeat.json
// every 10s and reports status to the server. Detects completion or emergency stop.
func (d *Daemon) monitorExecution(ctx context.Context, projectID, projectDir string) {
	ticker := time.NewTicker(monitorInterval)
	defer ticker.Stop()

	statusFile := filepath.Join(projectDir, "runtime", "agents-status.json")
	heartbeatFile := filepath.Join(projectDir, "runtime", "orchestrator-heartbeat.json")

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Read agents-status.json
			data, err := os.ReadFile(statusFile)
			if err != nil {
				continue
			}

			var status map[string]string
			if json.Unmarshal(data, &status) != nil {
				continue
			}

			// Report to server.
			if err := d.client.ReportProjectAgentStatus(ctx, projectID, status); err != nil {
				d.logger.Debug("failed to report agent status", "project_id", projectID, "error", err)
			}

			// Read orchestrator heartbeat for staleness detection.
			if hbData, err := os.ReadFile(heartbeatFile); err == nil {
				var hb map[string]any
				if json.Unmarshal(hbData, &hb) == nil {
					if ts, ok := hb["updated_at"].(string); ok {
						if t, err := time.Parse(time.RFC3339, ts); err == nil {
							if time.Since(t) > 5*time.Minute {
								d.logger.Warn("orchestrator heartbeat stale",
									"project_id", projectID,
									"last_heartbeat", ts,
									"age", time.Since(t).String(),
								)
							}
						}
					}
				}
			}

			// Detect completion: check if orchestrator status is "completed".
			if status["orchestrator"] == "completed" || status["orchestrator"] == "done" {
				d.logger.Info("execution completed", "project_id", projectID)
				d.client.UpdateProjectStatus(ctx, projectID, "completed")
				return
			}

			// Detect emergency stop.
			if status["orchestrator"] == "emergency_stop" || status["orchestrator"] == "failed" {
				d.logger.Warn("execution emergency stop", "project_id", projectID)
				d.client.UpdateProjectStatus(ctx, projectID, "failed")
				return
			}
		}
	}
}

// generateMonitorScript returns the bash monitor script content.
func generateMonitorScript(baseDir, sessionName string, numExecutors int) string {
	// Build agent list for the script.
	agentList := ""
	for i := 0; i < numExecutors; i++ {
		agentList += fmt.Sprintf("executor-%d ", i)
	}
	agentList += "evaluator orchestrator"

	return fmt.Sprintf(`#!/bin/bash
# Harness Mode V2 — Agent Monitor Script
# Polls tmux windows and state files, writes aggregated agents-status.json

OUTPUT_DIR="%s"
SESSION="%s"
AGENTS=(%s)
STATUS_FILE="$OUTPUT_DIR/runtime/agents-status.json"

while true; do
    json="{"
    first=true

    for agent in "${AGENTS[@]}"; do
        # Read state file if it exists.
        state_file="$OUTPUT_DIR/runtime/state-${agent}.json"
        status="unknown"
        if [ -f "$state_file" ]; then
            file_status=$(cat "$state_file" 2>/dev/null | grep -o '"status"[[:space:]]*:[[:space:]]*"[^"]*"' | head -1 | sed 's/.*"status"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/')
            if [ -n "$file_status" ]; then
                status="$file_status"
            fi
        fi

        # Cross-reference with tmux capture (advisory).
        capture=$(tmux capture-pane -t "${SESSION}:${agent}" -p -S -10 2>/dev/null || echo "")
        if echo "$capture" | grep -qi "error\|panic\|fatal"; then
            if [ "$status" != "error" ] && [ "$status" != "failed" ]; then
                status="${status}_warn"
            fi
        fi

        if [ "$first" = true ]; then
            first=false
        else
            json+=","
        fi
        json+="\"${agent}\":\"${status}\""
    done

    json+="}"

    # Atomic write.
    echo "$json" > "${STATUS_FILE}.tmp"
    mv "${STATUS_FILE}.tmp" "$STATUS_FILE"

    sleep 5
done
`, baseDir, sessionName, agentList)
}

// atomicWriteFile writes data to a file using the .tmp + mv pattern.
func atomicWriteFile(path string, data []byte) error {
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

// writeJSONFile is a convenience to marshal and atomically write a JSON file.
func writeJSONFile(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return atomicWriteFile(path, data)
}
