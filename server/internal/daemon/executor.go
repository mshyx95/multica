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
	ProjectID    string
	SessionName  string
	ProjectDir   string
	RepoURL      string            // git repo to checkout
	NumExecutors int
	Models       map[string]string // role -> model override
	Effort       string            // reasoning effort level
	Plan         string            // the approved plan content
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
		initContent := fmt.Sprintf(`你是 Harness Mode V2 的 Executor-%d。你的工作目录是 %s。

你的职责：
1. 监控 %s/tasks/executor-%d/ 目录中的 task-*.md 文件
2. 当出现新的 task 文件时，阅读并执行任务
3. 完成后写报告到同目录的 report-{id}.md（使用原子写入：先写 .tmp 再 mv）
4. 更新状态文件 %s/runtime/state-executor-%d.json
5. 自行监控进程（10分钟无输出则诊断）

阅读以下文件了解计划：
- %s/memories/session/harness-unified-plan.md

现在将状态设为 idle，等待任务分配。
`, i, wtDir, baseDir, i, baseDir, i, baseDir)
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
	evalInitContent := fmt.Sprintf(`你是 Harness Mode V2 的 Evaluator。你的工作目录是 %s。

你的职责：
1. 监控 %s/tasks/evaluator/ 目录中的 task-*.md 文件
2. 当 Orchestrator 发来审查任务时，阅读 executor 的报告并审查代码质量
3. 将批准的代码合并到 integration 分支 (%s)
4. 写审查结果到 %s/tasks/evaluator/review-{id}.md（使用原子写入）
5. 更新状态文件 %s/runtime/state-evaluator.json
6. 回答 executor 的技术问题

阅读以下文件了解计划：
- %s/memories/session/harness-unified-plan.md

现在将状态设为 idle，等待任务。
`, intDir, baseDir, intDir, baseDir, baseDir, baseDir)
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
	orchInitContent := fmt.Sprintf(`你是 Harness Mode V2 的 Orchestrator Brain。

你管理 %d 个 Executor 和 1 个 Evaluator。

输出目录: %s
计划文件: %s/memories/session/harness-unified-plan.md
状态文件: %s/runtime/agents-status.json

你的职责是运行心跳循环（每15-30秒）：
1. 读取 agents-status.json
2. 对每个 idle 的 executor：分配下一个 subtask（写 task file + tmux 命令）
3. 对每个 report_ready 的 executor：读取报告 → 派 evaluator 审查
4. 对每个 blocked 的 agent：派 evaluator 回答问题
5. 检查文档合规和时间阈值
6. 更新 orchestrator-heartbeat.json

分配任务方式：
- 写任务文件到 %s/tasks/executor-{N}/task-{id}.md
- 通过 tmux 通知 executor: tmux send-keys -t %s:executor-{N} "阅读 tasks/executor-{N}/task-{id}.md 并执行" 然后再发 Enter
- 注意：tmux send-keys 文本和 Enter 必须分开发送！

关键规则：
- 绝对不要问用户任何问题
- 所有问题路由给 Evaluator
- 使用原子文件操作（.tmp + mv）
- 每完成一个 subtask，更新 optimization_results.md（你是唯一写入者）
- 持续运行直到所有任务完成或紧急停止条件触发

现在阅读计划并开始心跳循环。
`, cfg.NumExecutors, baseDir, baseDir, baseDir, baseDir, cfg.SessionName)
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
	cmd := fmt.Sprintf("cd %s && %s", shellEscape(cwd), copilotPath)
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
