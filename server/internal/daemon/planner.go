package daemon

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	plannerPollInterval = 3 * time.Second
	plannerWindow       = "planner"
	multicaBaseDir      = "/mnt2/yuxuanhu/multica"
)

// ProjectDeployment represents an active project deployment on this runtime.
type ProjectDeployment struct {
	ProjectID   string
	SessionName string
	WorkDir     string
	Phase       string // "planning", "executing"
	LastSeq     int    // last message seq we've sent to planner
}

// startPlanner creates a tmux session and starts a copilot instance as the Planner.
func (d *Daemon) startPlanner(ctx context.Context, projectID, projectName, goals, skills string) error {
	sessionName := tmuxSessionName(projectID)

	// Create project directory structure.
	projectDir := filepath.Join(multicaBaseDir, "projects", projectID)
	for _, sub := range []string{"runtime", "tasks", "memories/session"} {
		if err := os.MkdirAll(filepath.Join(projectDir, sub), 0o755); err != nil {
			return fmt.Errorf("create project dirs: %w", err)
		}
	}

	// Write project context file.
	contextFile := filepath.Join(projectDir, "memories", "session", "project-context.md")
	contextContent := fmt.Sprintf("# Project: %s\n\n## Goals\n%s\n\n## Skills\n%s\n", projectName, goals, skills)
	if err := os.WriteFile(contextFile, []byte(contextContent), 0o644); err != nil {
		return fmt.Errorf("write context: %w", err)
	}

	// Create tmux session (kill stale one if present).
	if tmuxHasSession(sessionName) {
		tmuxKillSession(sessionName)
	}
	if err := tmuxCreateSession(sessionName); err != nil {
		return fmt.Errorf("create tmux session: %w", err)
	}

	// Build planner prompt.
	prompt := fmt.Sprintf(
		"You are a Planner agent for project '%s'. "+
			"Read the project context at %s. "+
			"Conduct a structured multi-round Q&A with the user to understand requirements. "+
			"Ask at minimum 3 rounds of questions covering: goals, constraints, scope, edge cases, success criteria. "+
			"After gathering enough information, produce a Task Requirements Document (TRD) and implementation plan. "+
			"Present the plan and wait for user to say APPROVE or REVISE.",
		projectName, contextFile,
	)

	// Get copilot CLI path.
	copilotEntry, ok := d.cfg.Agents["copilot"]
	if !ok {
		return fmt.Errorf("copilot agent not configured")
	}

	// Build the copilot command — use interactive mode (-i) so the session stays open
	// for multi-round conversation. Use text output (not json) for readability.
	cmd := fmt.Sprintf("%s -i %q --allow-all --model claude-opus-4.6-1m",
		copilotEntry.Path, prompt)

	// Rename default window and send the command.
	target := fmt.Sprintf("%s:0", sessionName)
	exec.Command("tmux", "rename-window", "-t", target, plannerWindow).Run()
	tmuxSendKeys(sessionName, plannerWindow, cmd)

	d.logger.Info("planner started", "project_id", projectID, "session", sessionName)
	return nil
}

// plannerMessageLoop polls for new user messages and relays them to the planner.
// It also reads planner output and reports back to the server.
// plannerMessageLoop is a no-op — users interact with the planner directly
// via `tmux attach -t <session>`. The planner tmux session name is logged
// on startup so the user (or the Web UI) can show the attach command.
func (d *Daemon) plannerMessageLoop(ctx context.Context, deployment *ProjectDeployment) {
	// Just keep the goroutine alive to track the deployment; no message relay.
	<-ctx.Done()
}

// projectLoop polls for projects assigned to this daemon's runtimes and manages their lifecycle.
func (d *Daemon) projectLoop(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	deployments := make(map[string]*ProjectDeployment)

	for {
		select {
		case <-ctx.Done():
			for _, dep := range deployments {
				tmuxKillSession(dep.SessionName)
			}
			return
		case <-ticker.C:
			runtimeIDs := d.allRuntimeIDs()

			for _, rid := range runtimeIDs {
				projects, err := d.client.GetPendingProjects(ctx, rid)
				if err != nil {
					d.logger.Debug("get pending projects failed", "runtime_id", rid, "error", err)
					continue
				}

				for _, proj := range projects {
					if _, exists := deployments[proj.ID]; exists {
						continue
					}

					d.logger.Info("found pending project", "project_id", proj.ID, "name", proj.Name, "status", proj.Status)

					if proj.Status == "planning" {
						if err := d.startPlanner(ctx, proj.ID, proj.Name, proj.Goals, proj.Skills); err != nil {
							d.logger.Error("start planner failed", "project_id", proj.ID, "error", err)
							continue
						}

						dep := &ProjectDeployment{
							ProjectID:   proj.ID,
							SessionName: tmuxSessionName(proj.ID),
							WorkDir:     filepath.Join(multicaBaseDir, "projects", proj.ID),
							Phase:       "planning",
						}
						deployments[proj.ID] = dep

						go d.plannerMessageLoop(ctx, dep)
					}
				}
			}
		}
	}
}

// isShellNoise returns true if the captured text is just shell prompts or trivial output.
func isShellNoise(s string) bool {
s = strings.TrimSpace(s)
if s == "" {
return true
}
for _, line := range strings.Split(s, "\n") {
line = strings.TrimSpace(line)
if line == "" || strings.HasSuffix(line, "$ ") || strings.HasSuffix(line, "$") {
continue
}
return false
}
return true
}
