package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// copilotBackend implements Backend by spawning the GitHub Copilot CLI
// with --output-format json (JSONL, one event per line).
type copilotBackend struct {
	cfg Config
}

func (b *copilotBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	execPath := b.cfg.ExecutablePath
	if execPath == "" {
		execPath = "copilot"
	}
	if _, err := exec.LookPath(execPath); err != nil {
		return nil, fmt.Errorf("copilot executable not found at %q: %w", execPath, err)
	}

	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 20 * time.Minute
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)

	args := []string{
		"--output-format", "json",
		"--allow-all",
	}
	if opts.Model != "" {
		args = append(args, "--model", opts.Model)
	}
	if opts.ResumeSessionID != "" {
		args = append(args, "--resume="+opts.ResumeSessionID)
	}
	args = append(args, "-i", prompt)

	cmd := exec.CommandContext(runCtx, execPath, args...)
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}
	cmd.Env = buildEnv(b.cfg.Env)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("copilot stdout pipe: %w", err)
	}
	cmd.Stderr = newLogWriter(b.cfg.Logger, "[copilot:stderr] ")

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start copilot: %w", err)
	}

	b.cfg.Logger.Info("copilot started", "pid", cmd.Process.Pid, "cwd", opts.Cwd, "model", opts.Model)

	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)

	go func() {
		defer cancel()
		defer close(msgCh)
		defer close(resCh)

		startTime := time.Now()
		var output strings.Builder
		var sessionID string
		finalStatus := "completed"
		var finalError string
		var totalOutputTokens int64
		var actualModel string
		var premiumRequests int64

		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)

		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}

			var evt copilotEvent
			if err := json.Unmarshal([]byte(line), &evt); err != nil {
				continue
			}

			switch evt.Type {
			case "assistant.turn_start":
				trySend(msgCh, Message{Type: MessageStatus, Status: "running"})

			case "session.tools_updated":
				var d struct {
					Model string `json:"model"`
				}
				if json.Unmarshal(evt.Data, &d) == nil && d.Model != "" {
					actualModel = d.Model
				}

			case "assistant.message":
				b.handleMessage(evt.Data, msgCh, &output, &totalOutputTokens, actualModel)

			case "tool.execution_start":
				b.handleToolStart(evt.Data, msgCh)

			case "tool.execution_complete":
				b.handleToolComplete(evt.Data, msgCh)

			case "result":
				sessionID = evt.SessionID
				if evt.ExitCode != 0 {
					finalStatus = "failed"
					finalError = fmt.Sprintf("copilot exited with code %d", evt.ExitCode)
				}
				// Parse usage from result
				var resultData struct {
					Usage struct {
						PremiumRequests    int64 `json:"premiumRequests"`
						TotalApiDurationMs int64 `json:"totalApiDurationMs"`
					} `json:"usage"`
				}
				if json.Unmarshal([]byte(line), &resultData) == nil {
					premiumRequests = resultData.Usage.PremiumRequests
				}
			}
		}

		exitErr := cmd.Wait()
		duration := time.Since(startTime)

		if runCtx.Err() == context.DeadlineExceeded {
			finalStatus = "timeout"
			finalError = fmt.Sprintf("copilot timed out after %s", timeout)
		} else if runCtx.Err() == context.Canceled {
			finalStatus = "aborted"
			finalError = "execution cancelled"
		} else if exitErr != nil && finalStatus == "completed" {
			finalStatus = "failed"
			finalError = fmt.Sprintf("copilot exited with error: %v", exitErr)
		}

		b.cfg.Logger.Info("copilot finished", "pid", cmd.Process.Pid, "status", finalStatus, "duration", duration.Round(time.Millisecond).String())

		// Emit final context status with premium requests
		if totalOutputTokens > 0 || premiumRequests > 0 {
			trySend(msgCh, Message{
				Type:   MessageStatus,
				Status: fmt.Sprintf("context:{\"output_tokens\":%d,\"model\":\"%s\",\"premium_requests\":%d}", totalOutputTokens, actualModel, premiumRequests),
			})
		}

		modelKey := actualModel
		if modelKey == "" {
			modelKey = "copilot"
		}

		resCh <- Result{
			Status:     finalStatus,
			Output:     output.String(),
			Error:      finalError,
			DurationMs: duration.Milliseconds(),
			SessionID:  sessionID,
			Usage: map[string]TokenUsage{
				modelKey: {
					OutputTokens: totalOutputTokens,
				},
			},
		}
	}()

	return &Session{Messages: msgCh, Result: resCh}, nil
}

func (b *copilotBackend) handleMessage(data json.RawMessage, ch chan<- Message, output *strings.Builder, totalOutputTokens *int64, model string) {
	var msg copilotMessageData
	if err := json.Unmarshal(data, &msg); err != nil {
		return
	}
	if msg.Content != "" {
		output.WriteString(msg.Content)
		trySend(ch, Message{Type: MessageText, Content: msg.Content})
	}
	if msg.OutputTokens > 0 {
		*totalOutputTokens += msg.OutputTokens
		trySend(ch, Message{
			Type:   MessageStatus,
			Status: fmt.Sprintf("context:{\"output_tokens\":%d,\"model\":\"%s\"}", *totalOutputTokens, model),
		})
	}
}

func (b *copilotBackend) handleToolStart(data json.RawMessage, ch chan<- Message) {
	var d copilotToolStartData
	if err := json.Unmarshal(data, &d); err != nil {
		return
	}
	trySend(ch, Message{
		Type:   MessageToolUse,
		Tool:   d.ToolName,
		CallID: d.ToolCallID,
		Input:  parseRawArgs(d.Arguments),
	})
}

func (b *copilotBackend) handleToolComplete(data json.RawMessage, ch chan<- Message) {
	var d copilotToolCompleteData
	if err := json.Unmarshal(data, &d); err != nil {
		return
	}
	resultContent := ""
	if d.Result != nil {
		resultContent = d.Result.Content
	}
	trySend(ch, Message{
		Type:   MessageToolResult,
		CallID: d.ToolCallID,
		Output: resultContent,
	})
}

// parseRawArgs handles tool arguments that may be a JSON object or a plain string.
func parseRawArgs(raw json.RawMessage) map[string]any {
	if len(raw) == 0 {
		return nil
	}
	var obj map[string]any
	if json.Unmarshal(raw, &obj) == nil {
		return obj
	}
	var s string
	if json.Unmarshal(raw, &s) == nil && s != "" {
		return map[string]any{"input": s}
	}
	return nil
}

// ── Copilot CLI JSONL types ──

type copilotEvent struct {
	Type      string          `json:"type"`
	Data      json.RawMessage `json:"data,omitempty"`
	SessionID string          `json:"sessionId,omitempty"` // result event only
	ExitCode  int             `json:"exitCode,omitempty"`  // result event only
}

type copilotMessageData struct {
	Content      string               `json:"content"`
	OutputTokens int64                `json:"outputTokens"`
	ToolRequests []copilotToolRequest `json:"toolRequests"`
}

type copilotToolRequest struct {
	ToolCallID string          `json:"toolCallId"`
	Name       string          `json:"name"`
	Arguments  json.RawMessage `json:"arguments"`
}

type copilotToolStartData struct {
	ToolCallID string          `json:"toolCallId"`
	ToolName   string          `json:"toolName"`
	Arguments  json.RawMessage `json:"arguments"`
}

type copilotToolCompleteData struct {
	ToolCallID string             `json:"toolCallId"`
	Success    bool               `json:"success"`
	Result     *copilotToolResult `json:"result,omitempty"`
}

type copilotToolResult struct {
	Content string `json:"content"`
}
