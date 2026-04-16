package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/creack/pty"
	"github.com/gorilla/websocket"
)

// startTerminalRelay connects to the server's terminal relay WebSocket endpoint
// and bridges it to the local tmux session. Reconnects automatically if the
// connection drops while the context is still active.
func (d *Daemon) startTerminalRelay(ctx context.Context, sessionName string) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		d.runTerminalRelay(ctx, sessionName)

		// If context is cancelled, exit. Otherwise wait and retry.
		select {
		case <-ctx.Done():
			return
		case <-time.After(3 * time.Second):
			d.logger.Info("reconnecting terminal relay", "session", sessionName)
		}
	}
}

// runTerminalRelay establishes one WebSocket connection to the server for
// terminal relay and attaches to the local tmux session, relaying PTY I/O.
func (d *Daemon) runTerminalRelay(ctx context.Context, sessionName string) {
	wsURL := d.client.baseURL
	wsURL = strings.Replace(wsURL, "https://", "wss://", 1)
	wsURL = strings.Replace(wsURL, "http://", "ws://", 1)
	wsURL = fmt.Sprintf("%s/api/daemon/terminal-relay/%s", wsURL, sessionName)

	header := http.Header{}
	if d.client.token != "" {
		header.Set("Authorization", "Bearer "+d.client.token)
	}

	d.logger.Info("connecting terminal relay", "session", sessionName, "url", wsURL)

	conn, _, err := websocket.DefaultDialer.DialContext(ctx, wsURL, header)
	if err != nil {
		d.logger.Error("terminal relay dial failed", "session", sessionName, "error", err)
		return
	}
	defer conn.Close()

	d.logger.Info("terminal relay connected, attaching to tmux", "session", sessionName)

	// Create a grouped tmux session for this relay connection.
	groupedName := fmt.Sprintf("%s-relay-%d", sessionName, rand.Intn(99999))
	args := []string{"new-session", "-d", "-t", sessionName, "-s", groupedName}
	if err := exec.Command("tmux", args...).Run(); err != nil {
		d.logger.Error("relay tmux grouped session failed", "error", err, "base", sessionName)
		conn.WriteMessage(websocket.TextMessage, []byte("Error: tmux session not found"))
		return
	}

	cmd := exec.Command("tmux", "attach-session", "-t", groupedName)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")

	ptmx, err := pty.Start(cmd)
	if err != nil {
		exec.Command("tmux", "kill-session", "-t", groupedName).Run()
		d.logger.Error("relay pty start failed", "error", err, "session", groupedName)
		conn.WriteMessage(websocket.TextMessage, []byte("Error: "+err.Error()))
		return
	}

	pty.Setsize(ptmx, &pty.Winsize{Rows: 40, Cols: 120})

	var once sync.Once
	done := make(chan struct{})

	// pty → websocket
	go func() {
		buf := make([]byte, 32*1024)
		for {
			n, err := ptmx.Read(buf)
			if err != nil {
				once.Do(func() { close(done) })
				return
			}
			if err := conn.WriteMessage(websocket.BinaryMessage, buf[:n]); err != nil {
				once.Do(func() { close(done) })
				return
			}
		}
	}()

	// websocket → pty (receives frontend input bridged through server)
	go func() {
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				once.Do(func() { close(done) })
				return
			}
			if len(msg) > 1 && msg[0] == 1 {
				var size struct {
					Cols uint16 `json:"cols"`
					Rows uint16 `json:"rows"`
				}
				if json.Unmarshal(msg[1:], &size) == nil && size.Cols > 0 && size.Rows > 0 {
					pty.Setsize(ptmx, &pty.Winsize{Rows: size.Rows, Cols: size.Cols})
				}
				continue
			}
			ptmx.Write(msg)
		}
	}()

	<-done
	cmd.Process.Kill()
	cmd.Wait()
	exec.Command("tmux", "kill-session", "-t", groupedName).Run()
	d.logger.Info("terminal relay session ended", "session", groupedName)
}
