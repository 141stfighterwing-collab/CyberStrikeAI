//go:build !windows

package handler

import (
	"net/http"
	"os"
	"os/exec"
	"time"

	"github.com/creack/pty"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

// WsUpgrader is only used for terminal WebSocket in system settings and will reuse existing login protection (JWT middleware is in the upper routing group)
var wsUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		// Since authentication has been done at the Gin routing layer, Origin is relaxed here to facilitate access through HTTPS/WSS under the same domain name.
		return true
	},
}

// RunCommandWS provides a truly interactive Shell: long sessions based on WebSocket + PTY
// After the front-end establishes a WebSocket connection, all keyboard input will be transparently transmitted to the Shell, and the output of the Shell will be written back to the front-end in real time.
func (h *TerminalHandler) RunCommandWS(c *gin.Context) {
	conn, err := wsUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	// Start an interactive shell. Here, bash is used first. If not found, sh will be returned.
	shell := "bash"
	if _, err := exec.LookPath(shell); err != nil {
		shell = "sh"
	}
	cmd := exec.Command(shell)
	cmd.Env = append(os.Environ(),
		"COLUMNS=120",
		"LINES=40",
		"TERM=xterm-256color",
	)

	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: ptyCols, Rows: ptyRows})
	if err != nil {
		return
	}
	defer ptmx.Close()

	// Shell -> WebSocket: Send PTY output to the front end in real time
	doneChan := make(chan struct{})
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := ptmx.Read(buf)
			if n > 0 {
				_ = conn.WriteMessage(websocket.TextMessage, buf[:n])
			}
			if err != nil {
				break
			}
		}
		close(doneChan)
	}()

	// WebSocket -> Shell: Write front-end input to PTY (including sudo password, Ctrl+C, etc.)
	conn.SetReadLimit(64 * 1024)
	_ = conn.SetReadDeadline(time.Now().Add(terminalTimeout))
	conn.SetPongHandler(func(string) error {
		_ = conn.SetReadDeadline(time.Now().Add(terminalTimeout))
		return nil
	})

	for {
		msgType, data, err := conn.ReadMessage()
		if err != nil {
			_ = cmd.Process.Kill()
			break
		}
		if msgType != websocket.TextMessage && msgType != websocket.BinaryMessage {
			continue
		}
		if len(data) == 0 {
			continue
		}
		if _, err := ptmx.Write(data); err != nil {
			_ = cmd.Process.Kill()
			break
		}
	}

	<-doneChan
}

