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
	projectDir := filepath.Join(d.cfg.WorkspacesRoot, "projects", projectID)
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

	// Build planner prompt — explicitly forbid execution, require plan file output.
	planFile := filepath.Join(projectDir, "memories", "session", "harness-unified-plan.md")
	prompt := fmt.Sprintf(
		"You are a PLANNER agent for project '%s'. "+
			"Read the project context at %s.\n\n"+
			"YOUR ROLE: You ONLY plan. You do NOT write code, create files, or execute commands (except reading files). "+
			"You are NOT an executor.\n\n"+
			"INTERVIEW PROTOCOL:\n"+
			"Conduct a deep interview with the user to fully understand their intent. "+
			"Ask questions ONE AT A TIME — never ask multiple questions in a single message. "+
			"Wait for the user's answer before asking the next question. "+
			"Continue asking until you have ZERO ambiguity about every detail: "+
			"goals, constraints, scope, edge cases, success criteria, technical approach, "+
			"dependencies, file structure, and expected output.\n\n"+
			"Do NOT rush. Be thorough. If the user's answer is vague, ask follow-up questions "+
			"to clarify. Every detail in the final plan must be crystal clear — no fuzzy areas.\n\n"+
			"MANDATORY QUESTION (ask before finalizing plan):\n"+
			"\"最小保障执行时间是多少？\" — This determines the minimum execution duration for the agents.\n\n"+
			"AFTER INTERVIEW:\n"+
			"1. Produce a detailed implementation plan with numbered subtasks, file ownership, dependencies, and acceptance criteria for each subtask.\n"+
			"2. Write the plan to: %s\n"+
			"3. Present the plan to the user and ask for APPROVE or REVISE.\n"+
			"4. When the user says APPROVE, write EXACTLY this line on its own: <<<APPROVED>>>\n"+
			"5. If the user says REVISE, update the plan based on feedback and repeat.\n\n"+
			"CRITICAL: When you see APPROVE, you MUST output <<<APPROVED>>> and then STOP. Do NOT start implementing.",
		projectName, contextFile, planFile,
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

	// Start terminal relay so the frontend can access this tmux session.
	go d.startTerminalRelay(ctx, sessionName+":"+plannerWindow)

	d.logger.Info("planner started", "project_id", projectID, "session", sessionName)
	return nil
}

// plannerMessageLoop polls for new user messages and relays them to the planner.
// It also reads planner output and reports back to the server.
// plannerMessageLoop monitors the planner tmux session for the <<<APPROVED>>> marker.
// When detected, it triggers the execution phase (Orchestrator + Executors + Evaluator).
func (d *Daemon) plannerMessageLoop(ctx context.Context, deployment *ProjectDeployment) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Check for approval via plan file existence + tmux capture.
			planPath := filepath.Join(deployment.WorkDir, "memories", "session", "harness-unified-plan.md")
			planExists := false
			if _, err := os.Stat(planPath); err == nil {
				planExists = true
			}

			capture, err := tmuxCapture(deployment.SessionName, plannerWindow)
			if err != nil {
				continue
			}

			approved := strings.Contains(capture, "<<<APPROVED>>>") || strings.Contains(capture, "APPROVED")
			if !approved || !planExists {
				continue
			}

			// Both conditions met: plan file exists and APPROVED detected.
			d.logger.Info("planner APPROVED detected, starting execution phase",
				"project_id", deployment.ProjectID)

			// Read the plan file (reuse planPath from above).
			planContent, _ := os.ReadFile(planPath)

			// Read project-context.md for injection into init files.
			contextPath := filepath.Join(deployment.WorkDir, "memories", "session", "project-context.md")
			projectContext, _ := os.ReadFile(contextPath)

			// Read skills from the skills directory (best-effort).
			skillsContent := loadSkillsContent(filepath.Join(d.cfg.WorkspacesRoot, "skills"))

			// Copy skills into the project directory for agent access.
			if skillsContent != "" {
				projSkillsDir := filepath.Join(deployment.WorkDir, "skills")
				os.MkdirAll(projSkillsDir, 0o755)
				srcSkillsDir := filepath.Join(d.cfg.WorkspacesRoot, "skills")
				if entries, err := os.ReadDir(srcSkillsDir); err == nil {
					for _, e := range entries {
						if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
							data, _ := os.ReadFile(filepath.Join(srcSkillsDir, e.Name()))
							if len(data) > 0 {
								os.WriteFile(filepath.Join(projSkillsDir, e.Name()), data, 0o644)
							}
						}
					}
				}
			}

			// Update project status to "executing" via API
			d.client.UpdateProjectStatus(ctx, deployment.ProjectID, "executing")

			// Start execution phase
			cfg := ExecutionConfig{
				ProjectID:      deployment.ProjectID,
				ProjectName:    deployment.ProjectID, // best available identifier
				SessionName:    deployment.SessionName,
				ProjectDir:     deployment.WorkDir,
				NumExecutors:   2, // TODO: read from project config
				Plan:           string(planContent),
				ProjectContext: string(projectContext),
				Skills:         skillsContent,
			}
			if err := d.startExecution(ctx, cfg); err != nil {
				d.logger.Error("start execution failed", "project_id", deployment.ProjectID, "error", err)
			}
			return // planner's job is done
		}
	}
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
							WorkDir:     filepath.Join(d.cfg.WorkspacesRoot, "projects", proj.ID),
							Phase:       "planning",
						}
						deployments[proj.ID] = dep

						go d.plannerMessageLoop(ctx, dep)
					}
				}
			}

			// Sync files for all active deployments
			for _, dep := range deployments {
				d.syncProjectFiles(ctx, dep.ProjectID)
			}
		}
	}
}

// syncProjectFiles scans the project directory and pushes file metadata+content to the server.
func (d *Daemon) syncProjectFiles(ctx context.Context, projectID string) {
	baseDir := filepath.Join(d.cfg.WorkspacesRoot, "projects", projectID)
	info, err := os.Stat(baseDir)
	if err != nil || !info.IsDir() {
		return
	}

	var files []FileEntry
	filepath.Walk(baseDir, func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(baseDir, path)
		if rel == "." {
			return nil
		}

		entry := FileEntry{
			Path:     rel,
			Name:     fi.Name(),
			Size:     fi.Size(),
			IsDir:    fi.IsDir(),
			ModTime:  fi.ModTime().Format(time.RFC3339),
			Category: categorizeFile(rel),
		}

		// Include content for non-directory files under 1MB
		if !fi.IsDir() && fi.Size() < 1<<20 {
			if data, err := os.ReadFile(path); err == nil {
				content := string(data)
				entry.Content = &content
			}
		}

		files = append(files, entry)
		return nil
	})

	if err := d.client.SyncProjectFiles(ctx, projectID, files); err != nil {
		d.logger.Debug("sync project files failed", "project_id", projectID, "error", err)
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

// loadSkillsContent reads all .md files from the given directory and
// concatenates their content. Returns empty string if dir does not exist.
func loadSkillsContent(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	var parts []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil || len(data) == 0 {
			continue
		}
		parts = append(parts, fmt.Sprintf("### Skill: %s\n%s", e.Name(), string(data)))
	}
	if len(parts) == 0 {
		return "(no skills loaded)"
	}
	return strings.Join(parts, "\n\n---\n\n")
}
