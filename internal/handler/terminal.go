package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

const (
	terminalMaxCommandLen = 4096
	terminalMaxOutputLen  = 256 * 1024 // 256KB
	terminalTimeout       = 120 * time.Second
)

// TerminalHandler handles terminal command execution in system settings
type TerminalHandler struct {
	logger *zap.Logger
}

// MaskTerminalCommand masks terminal commands that may contain sensitive information to avoid directly recording passwords and other content in the log.
func maskTerminalCommand(cmd string) string {
	trimmed := strings.TrimSpace(cmd)
	lower := strings.ToLower(trimmed)
	if strings.Contains(lower, "sudo") || strings.Contains(lower, "password") {
		return "[masked sensitive terminal command]"
	}
	if len(trimmed) > 256 {
		return trimmed[:256] + "..."
	}
	return trimmed
}

// NewTerminalHandler creates a terminal handler
func NewTerminalHandler(logger *zap.Logger) *TerminalHandler {
	return &TerminalHandler{logger: logger}
}

// RunCommandRequest execute command request
type RunCommandRequest struct {
	Command string `json:"command"`
	Shell   string `json:"shell,omitempty"`
	Cwd     string `json:"cwd,omitempty"`
}

// RunCommandResponse execution command response
type RunCommandResponse struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exit_code"`
	Error    string `json:"error,omitempty"`
}

// RunCommand executes terminal commands (login required)
func (h *TerminalHandler) RunCommand(c *gin.Context) {
	var req RunCommandRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "The request body is invalid, command field is required"})
		return
	}

	cmdStr := strings.TrimSpace(req.Command)
	if cmdStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Command cannot be empty"})
		return
	}
	if len(cmdStr) > terminalMaxCommandLen {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Command too long"})
		return
	}

	shell := req.Shell
	if shell == "" {
		if runtime.GOOS == "windows" {
			shell = "cmd"
		} else {
			shell = "sh"
		}
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), terminalTimeout)
	defer cancel()

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd", "/c", cmdStr)
	} else {
		cmd = exec.CommandContext(ctx, shell, "-c", cmdStr)
		// Set COLUMNS/TERM when there is no TTY to make the usage layout of tools such as ping consistent with the real terminal
		cmd.Env = append(os.Environ(), "COLUMNS=120", "LINES=40", "TERM=xterm-256color")
	}

	if req.Cwd != "" {
		absCwd, err := filepath.Abs(req.Cwd)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid working directory"})
			return
		}
		cur, _ := os.Getwd()
		curAbs, _ := filepath.Abs(cur)
		rel, err := filepath.Rel(curAbs, absCwd)
		if err != nil || strings.HasPrefix(rel, "..") || rel == ".." {
			c.JSON(http.StatusBadRequest, gin.H{"error": "The working directory must be under the current process directory"})
			return
		}
		cmd.Dir = absCwd
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	stdoutBytes := stdout.Bytes()
	stderrBytes := stderr.Bytes()

	// Limit the output length to prevent excessive memory usage (truncate after copying to avoid modifying the original buffer)
	truncSuffix := []byte("\n...(output truncated)\n")
	if len(stdoutBytes) > terminalMaxOutputLen {
		tmp := make([]byte, terminalMaxOutputLen+len(truncSuffix))
		n := copy(tmp, stdoutBytes[:terminalMaxOutputLen])
		copy(tmp[n:], truncSuffix)
		stdoutBytes = tmp
	}
	if len(stderrBytes) > terminalMaxOutputLen {
		tmp := make([]byte, terminalMaxOutputLen+len(truncSuffix))
		n := copy(tmp, stderrBytes[:terminalMaxOutputLen])
		copy(tmp[n:], truncSuffix)
		stderrBytes = tmp
	}

	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
		if ctx.Err() == context.DeadlineExceeded {
			so := strings.ReplaceAll(string(stdoutBytes), "\r\n", "\n")
			so = strings.ReplaceAll(so, "\r", "\n")
			se := strings.ReplaceAll(string(stderrBytes), "\r\n", "\n")
			se = strings.ReplaceAll(se, "\r", "\n")
			resp := RunCommandResponse{
				Stdout:   so,
				Stderr:   se,
				ExitCode: -1,
				Error:    "Command execution timeout (" + terminalTimeout.String() + "）",
			}
			c.JSON(http.StatusOK, resp)
			return
		}
		h.logger.Debug("Terminal command execution exception", zap.String("command", maskTerminalCommand(cmdStr)), zap.Error(err))
	}

	// Unify to \n to avoid misalignment/diagonal typesetting on the front end due to \r
	stdoutStr := strings.ReplaceAll(string(stdoutBytes), "\r\n", "\n")
	stdoutStr = strings.ReplaceAll(stdoutStr, "\r", "\n")
	stderrStr := strings.ReplaceAll(string(stderrBytes), "\r\n", "\n")
	stderrStr = strings.ReplaceAll(stderrStr, "\r", "\n")

	resp := RunCommandResponse{
		Stdout:   stdoutStr,
		Stderr:   stderrStr,
		ExitCode: exitCode,
	}
	if err != nil && exitCode != 0 {
		resp.Error = err.Error()
	}
	c.JSON(http.StatusOK, resp)
}

// StreamEvent SSE event
type streamEvent struct {
	T string `json:"t"` // "out" | "err" | "exit"
	D string `json:"d,omitempty"`
	C int    `json:"c"` // Exit code (omitempty is not used, otherwise 0 will not be serialized and will cause [exit undefined] to be displayed on the front end)
}

// RunCommandStream streams execution commands, and the output is pushed to the front end (SSE) in real time.
func (h *TerminalHandler) RunCommandStream(c *gin.Context) {
	var req RunCommandRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "The request body is invalid, command field is required"})
		return
	}
	cmdStr := strings.TrimSpace(req.Command)
	if cmdStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Command cannot be empty"})
		return
	}
	if len(cmdStr) > terminalMaxCommandLen {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Command too long"})
		return
	}
	shell := req.Shell
	if shell == "" {
		if runtime.GOOS == "windows" {
			shell = "cmd"
		} else {
			shell = "sh"
		}
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), terminalTimeout)
	defer cancel()

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd", "/c", cmdStr)
	} else {
		cmd = exec.CommandContext(ctx, shell, "-c", cmdStr)
		cmd.Env = append(os.Environ(), "COLUMNS=120", "LINES=40", "TERM=xterm-256color")
	}
	if req.Cwd != "" {
		absCwd, err := filepath.Abs(req.Cwd)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid working directory"})
			return
		}
		cur, _ := os.Getwd()
		curAbs, _ := filepath.Abs(cur)
		rel, err := filepath.Rel(curAbs, absCwd)
		if err != nil || strings.HasPrefix(rel, "..") || rel == ".." {
			c.JSON(http.StatusBadRequest, gin.H{"error": "The working directory must be under the current process directory"})
			return
		}
		cmd.Dir = absCwd
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	c.Writer.WriteHeader(http.StatusOK)
	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		cancel()
		return
	}

	sendEvent := func(ev streamEvent) {
		body, _ := json.Marshal(ev)
		c.SSEvent("", string(body))
		flusher.Flush()
	}

	runCommandStreamImpl(cmd, sendEvent, ctx)
}
