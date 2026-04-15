package daemon

import (
	"fmt"
	"os/exec"
	"strings"
)

const tmuxPrefix = "multica"

// tmuxSessionName returns the tmux session name for a project.
func tmuxSessionName(projectID string) string {
	short := projectID
	if len(short) > 8 {
		short = short[:8]
	}
	return fmt.Sprintf("%s-%s", tmuxPrefix, short)
}

// tmuxCreateSession creates a new tmux session.
func tmuxCreateSession(sessionName string) error {
	return exec.Command("tmux", "new-session", "-d", "-s", sessionName, "-x", "200", "-y", "50").Run()
}

// tmuxSendKeys sends keystrokes to a tmux window.
func tmuxSendKeys(sessionName, windowName, keys string) error {
	target := fmt.Sprintf("%s:%s", sessionName, windowName)
	return exec.Command("tmux", "send-keys", "-t", target, keys, "Enter").Run()
}

// tmuxCapture captures the current pane content.
func tmuxCapture(sessionName, windowName string) (string, error) {
	target := fmt.Sprintf("%s:%s", sessionName, windowName)
	out, err := exec.Command("tmux", "capture-pane", "-t", target, "-p", "-S", "-100").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// tmuxKillSession kills an entire tmux session.
func tmuxKillSession(sessionName string) error {
	return exec.Command("tmux", "kill-session", "-t", sessionName).Run()
}

// tmuxHasSession checks if a tmux session exists.
func tmuxHasSession(sessionName string) bool {
	return exec.Command("tmux", "has-session", "-t", sessionName).Run() == nil
}

// tmuxNewWindow creates a new window in the given tmux session and runs the command.
func tmuxNewWindow(sessionName, windowName, shellCmd string) error {
	return exec.Command("tmux", "new-window", "-t", sessionName, "-n", windowName, "bash", "-c", shellCmd).Run()
}

// tmuxListWindows returns the list of window names in a tmux session.
func tmuxListWindows(sessionName string) ([]string, error) {
	out, err := exec.Command("tmux", "list-windows", "-t", sessionName, "-F", "#{window_name}").Output()
	if err != nil {
		return nil, err
	}
	var names []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			names = append(names, line)
		}
	}
	return names, nil
}
