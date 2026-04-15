package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

const (
	orchestratorWindow = "orchestrator"
	evaluatorWindow    = "evaluator"
	executorPrefix     = "executor"
	monitorInterval    = 5 * time.Second
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

// startExecution sets up git worktrees and launches all agents for the execution phase.
func (d *Daemon) startExecution(ctx context.Context, cfg ExecutionConfig) error {
	d.logger.Info("starting execution phase",
		"project_id", cfg.ProjectID,
		"executors", cfg.NumExecutors,
	)

	baseDir := cfg.ProjectDir

	// Create task exchange directories.
	for i := 0; i < cfg.NumExecutors; i++ {
		if err := os.MkdirAll(filepath.Join(baseDir, "tasks", fmt.Sprintf("executor-%d", i)), 0o755); err != nil {
			return fmt.Errorf("create executor task dir: %w", err)
		}
	}
	if err := os.MkdirAll(filepath.Join(baseDir, "tasks", "evaluator"), 0o755); err != nil {
		return fmt.Errorf("create evaluator task dir: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(baseDir, "runtime"), 0o755); err != nil {
		return fmt.Errorf("create runtime dir: %w", err)
	}

	// Write the approved plan.
	planPath := filepath.Join(baseDir, "memories", "session", "harness-unified-plan.md")
	if err := os.MkdirAll(filepath.Dir(planPath), 0o755); err != nil {
		return fmt.Errorf("create plan dir: %w", err)
	}
	if err := os.WriteFile(planPath, []byte(cfg.Plan), 0o644); err != nil {
		return fmt.Errorf("write plan: %w", err)
	}

	// Initialize agents-status.json.
	statusFile := filepath.Join(baseDir, "runtime", "agents-status.json")
	initStatus := map[string]string{}
	for i := 0; i < cfg.NumExecutors; i++ {
		initStatus[fmt.Sprintf("executor-%d", i)] = "idle"
	}
	initStatus["evaluator"] = "idle"
	initStatus["orchestrator"] = "running"
	statusJSON, _ := json.MarshalIndent(initStatus, "", "  ")
	if err := os.WriteFile(statusFile, statusJSON, 0o644); err != nil {
		return fmt.Errorf("write agents-status.json: %w", err)
	}

	copilotPath := d.cfg.Agents["copilot"].Path

	// Ensure tmux session exists.
	if !tmuxHasSession(cfg.SessionName) {
		if err := tmuxCreateSession(cfg.SessionName); err != nil {
			return fmt.Errorf("create tmux session: %w", err)
		}
	}

	// Setup git worktrees if repo URL provided.
	if cfg.RepoURL != "" {
		if err := d.setupWorktrees(ctx, cfg); err != nil {
			d.logger.Warn("git worktree setup had errors", "error", err)
		}
	}

	// Start Orchestrator Brain.
	orchModel := cfg.Models["orchestrator"]
	if orchModel == "" {
		orchModel = "claude-opus-4.6-1m"
	}
	orchPrompt := fmt.Sprintf(
		"You are the Orchestrator Brain for project '%s'. "+
			"Read the approved plan at %s. "+
			"Your job is to manage %d Executors and 1 Evaluator. "+
			"Dispatch subtasks to executors by writing task files to %s/tasks/executor-N/. "+
			"Monitor agent status at %s/runtime/agents-status.json. "+
			"Enter a heartbeat loop: every 15-30s check status, read reports, dispatch tasks, "+
			"until all subtasks are completed. "+
			"NEVER ask the user anything — route questions to the Evaluator. "+
			"Write progress updates to %s/memories/session/session_log.md.",
		cfg.ProjectID, planPath, cfg.NumExecutors,
		baseDir, baseDir, baseDir,
	)
	orchCmd := buildCopilotCmd(copilotPath, orchPrompt, orchModel, cfg.Effort, baseDir)
	if err := tmuxNewWindow(cfg.SessionName, orchestratorWindow, orchCmd); err != nil {
		return fmt.Errorf("start orchestrator: %w", err)
	}

	// Start Executors.
	for i := 0; i < cfg.NumExecutors; i++ {
		windowName := fmt.Sprintf("%s-%d", executorPrefix, i)
		wtDir := filepath.Join(baseDir, fmt.Sprintf("worktree-%d", i))
		if _, err := os.Stat(wtDir); os.IsNotExist(err) {
			wtDir = baseDir // fallback if no repo
		}

		execModel := cfg.Models[fmt.Sprintf("executor-%d", i)]
		if execModel == "" {
			execModel = cfg.Models["executor"]
		}
		if execModel == "" {
			execModel = "claude-sonnet-4"
		}

		execPrompt := fmt.Sprintf(
			"You are Executor-%d for project '%s'. "+
				"Your task directory is %s/tasks/executor-%d/. "+
				"Wait for task files to appear. When a task file arrives, read it and execute the work. "+
				"Write your results to a report file in the same directory. "+
				"Update your state file at %s/runtime/state-executor-%d.json. "+
				"After completing each task, set your status to 'idle' and wait for the next one. "+
				"Work autonomously — do NOT ask the user anything.",
			i, cfg.ProjectID,
			baseDir, i,
			baseDir, i,
		)

		execCmd := buildCopilotCmd(copilotPath, execPrompt, execModel, cfg.Effort, wtDir)
		if err := tmuxNewWindow(cfg.SessionName, windowName, execCmd); err != nil {
			d.logger.Warn("failed to start executor", "index", i, "error", err)
		}
	}

	// Start Evaluator.
	evalModel := cfg.Models["evaluator"]
	if evalModel == "" {
		evalModel = "gpt-5.3-codex"
	}
	intDir := filepath.Join(baseDir, "integration")
	if _, err := os.Stat(intDir); os.IsNotExist(err) {
		intDir = baseDir
	}

	evalPrompt := fmt.Sprintf(
		"You are the Evaluator for project '%s'. "+
			"Your task directory is %s/tasks/evaluator/. "+
			"When the Orchestrator sends you work, review it for correctness. "+
			"Merge approved code into the integration branch at %s. "+
			"Write results back to your task directory. "+
			"Update your state at %s/runtime/state-evaluator.json. "+
			"Work autonomously.",
		cfg.ProjectID, baseDir, intDir, baseDir,
	)
	evalCmd := buildCopilotCmd(copilotPath, evalPrompt, evalModel, cfg.Effort, intDir)
	if err := tmuxNewWindow(cfg.SessionName, evaluatorWindow, evalCmd); err != nil {
		d.logger.Warn("failed to start evaluator", "error", err)
	}

	d.logger.Info("execution phase started",
		"project_id", cfg.ProjectID,
		"session", cfg.SessionName,
		"orchestrator", orchModel,
		"executors", cfg.NumExecutors,
		"evaluator", evalModel,
	)

	return nil
}

// setupWorktrees creates a bare clone and per-executor worktrees.
func (d *Daemon) setupWorktrees(ctx context.Context, cfg ExecutionConfig) error {
	baseDir := cfg.ProjectDir
	bareDir := filepath.Join(baseDir, ".bare-repo")

	// Clone bare repo first.
	if _, err := os.Stat(bareDir); os.IsNotExist(err) {
		cmd := exec.CommandContext(ctx, "git", "clone", "--bare", cfg.RepoURL, bareDir)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("git bare clone: %w (%s)", err, string(out))
		}
	}

	// Create worktrees for each executor.
	for i := 0; i < cfg.NumExecutors; i++ {
		wtDir := filepath.Join(baseDir, fmt.Sprintf("worktree-%d", i))
		branch := fmt.Sprintf("executor-%d/%s", i, cfg.ProjectID[:min(8, len(cfg.ProjectID))])
		if _, err := os.Stat(wtDir); os.IsNotExist(err) {
			cmd := exec.CommandContext(ctx, "git", "-C", bareDir, "worktree", "add", "-b", branch, wtDir, "HEAD")
			if out, err := cmd.CombinedOutput(); err != nil {
				d.logger.Warn("git worktree failed", "executor", i, "error", err, "output", string(out))
			}
		}
	}

	// Create integration worktree for evaluator.
	intDir := filepath.Join(baseDir, "integration")
	if _, err := os.Stat(intDir); os.IsNotExist(err) {
		intBranch := fmt.Sprintf("integration/%s", cfg.ProjectID[:min(8, len(cfg.ProjectID))])
		cmd := exec.CommandContext(ctx, "git", "-C", bareDir, "worktree", "add", "-b", intBranch, intDir, "HEAD")
		if out, err := cmd.CombinedOutput(); err != nil {
			d.logger.Warn("git integration worktree failed", "error", err, "output", string(out))
		}
	}

	return nil
}

// buildCopilotCmd constructs the copilot CLI command string.
func buildCopilotCmd(copilotPath, prompt, model, effort, cwd string) string {
	cmd := fmt.Sprintf("cd %s && %s -p %q --output-format json --allow-all",
		shellEscape(cwd), copilotPath, prompt)
	if model != "" {
		cmd += fmt.Sprintf(" --model %s", model)
	}
	if effort != "" {
		cmd += fmt.Sprintf(" --effort %s", effort)
	}
	return cmd
}

// shellEscape wraps a path in single quotes for safe shell usage.
func shellEscape(s string) string {
	return "'" + s + "'"
}

// monitorExecution runs a background loop that reads agent status files and reports to the server.
func (d *Daemon) monitorExecution(ctx context.Context, projectID, projectDir string) {
	ticker := time.NewTicker(monitorInterval)
	defer ticker.Stop()

	statusFile := filepath.Join(projectDir, "runtime", "agents-status.json")

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			data, err := os.ReadFile(statusFile)
			if err != nil {
				continue
			}

			var status map[string]string
			if json.Unmarshal(data, &status) != nil {
				continue
			}

			if err := d.client.ReportProjectAgentStatus(ctx, projectID, status); err != nil {
				d.logger.Debug("failed to report agent status", "project_id", projectID, "error", err)
			}
		}
	}
}
