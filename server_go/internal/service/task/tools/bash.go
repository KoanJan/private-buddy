package tools

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/sashabaranov/go-openai"

	applogger "private-buddy-server/internal/logger"
)

// BashTool executes shell commands within a workspace.
//
// Provides the agent with the ability to run shell commands on the local system.
// Commands are confined to the task's workspace directory to ensure isolation.
// Supports configurable timeout and returns stdout, stderr, and exit code.
//
// Security:
//   - Path traversal outside workspace is blocked
//   - Access to .meta directory is blocked (system-managed files)
type BashTool struct {
	workspace string // Working directory for command execution
}

// NewBashTool creates a BashTool with the given workspace directory.
// If workspace is set, commands run with CWD=workspace and path traversal is blocked.
func NewBashTool(workspace string) *BashTool {
	return &BashTool{workspace: workspace}
}

func (b *BashTool) Name() string { return "bash" }

func (b *BashTool) Schema() openai.FunctionDefinition {
	workspaceHint := ""
	if b.workspace != "" {
		workspaceHint = fmt.Sprintf(" All file operations must be within %s. Do not access paths outside this directory.", b.workspace)
	}
	return openai.FunctionDefinition{
		Name:        "bash",
		Description: "Execute a shell command. Use this tool to run commands, manage files, and interact with the system." + workspaceHint,
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"command": map[string]interface{}{
					"type":        "string",
					"description": "The shell command to execute",
				},
				"timeout": map[string]interface{}{
					"type":        "integer",
					"description": "Timeout in milliseconds (default: 30000)",
					"default":     30000,
				},
			},
			"required": []string{"command"},
		},
	}
}

// BashResult holds the structured output of a bash command execution.
type BashResult struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exit_code"`
}

// Execute runs a bash command and returns structured output.
// Handles timeout, security checks, and returns JSON with stdout/stderr/exit_code.
func (b *BashTool) Execute(args map[string]interface{}) (string, error) {
	command, _ := args["command"].(string)
	timeoutMs := 30000
	if t, ok := args["timeout"].(float64); ok {
		timeoutMs = int(t)
	}

	if command == "" {
		return `{"stdout": "", "stderr": "Error: empty command", "exit_code": 1}`, nil
	}

	if b.workspace != "" {
		if blocked := b.isBlockedCommand(command); blocked != "" {
			cmdPreview := command
			if len(cmdPreview) > 200 {
				cmdPreview = cmdPreview[:200]
			}
			applogger.L.Warn("BashTool blocked command", "command", cmdPreview, "reason", blocked)
			return fmt.Sprintf(`{"stdout": "", "stderr": "Error: %s", "exit_code": 1}`, blocked), nil
		}
	}

	cmdPreview := command
	if len(cmdPreview) > 200 {
		cmdPreview = cmdPreview[:200]
	}
	applogger.L.Info("BashTool executing", "command", cmdPreview, "timeout_ms", timeoutMs)

	ctx := exec.Command("bash", "-c", command)
	if b.workspace != "" {
		ctx.Dir = b.workspace
	}

	timeout := time.Duration(timeoutMs) * time.Millisecond
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	var stdout, stderr strings.Builder
	ctx.Stdout = &stdout
	ctx.Stderr = &stderr

	if err := ctx.Start(); err != nil {
		result, _ := json.Marshal(BashResult{Stderr: err.Error(), ExitCode: 1})
		return string(result), nil
	}

	done := make(chan error, 1)
	go func() { done <- ctx.Wait() }()

	select {
	case err := <-done:
		exitCode := 0
		if err != nil {
			exitCode = 1
			if exitErr, ok := err.(*exec.ExitError); ok {
				exitCode = exitErr.ExitCode()
			}
		}
		result, _ := json.Marshal(BashResult{
			Stdout:   stdout.String(),
			Stderr:   stderr.String(),
			ExitCode: exitCode,
		})
		return string(result), nil
	case <-timer.C:
		ctx.Process.Kill()
		result, _ := json.Marshal(BashResult{Stderr: "Error: command timed out", ExitCode: -1})
		return string(result), nil
	}
}

// isBlockedCommand checks if a command should be blocked for security reasons.
// Blocks access to .meta directory and path traversal outside workspace.
func (b *BashTool) isBlockedCommand(command string) string {
	checkPart := stripHeredocContent(command)

	if strings.Contains(checkPart, ".meta") {
		return "access to .meta directory is not allowed"
	}

	if b.isPathTraversal(checkPart) {
		return "command attempts to access paths outside workspace"
	}

	return ""
}

// isPathTraversal checks if a command attempts to access paths outside the workspace.
// Examines command parts for absolute paths and ".." traversal patterns.
func (b *BashTool) isPathTraversal(command string) bool {
	if b.workspace == "" {
		return false
	}
	parts := strings.Fields(command)
	for _, part := range parts {
		if strings.HasPrefix(part, "/") && !strings.HasPrefix(part, b.workspace) {
			if !isSafeAbsolutePath(part) {
				return true
			}
		}
		if strings.Contains(part, "..") {
			resolved := safeResolve(part, b.workspace)
			if resolved != "" && !strings.HasPrefix(resolved, b.workspace) {
				return true
			}
		}
	}
	return false
}

// safeResolve resolves a path relative to the workspace and returns the cleaned path.
func safeResolve(pathStr, workspace string) string {
	if filepath.IsAbs(pathStr) {
		return filepath.Clean(pathStr)
	}
	joined := filepath.Join(workspace, pathStr)
	return filepath.Clean(joined)
}

// stripHeredocContent removes heredoc content from a command for security checking.
// Heredoc content may contain paths that should not be checked for traversal.
func stripHeredocContent(command string) string {
	idx := strings.Index(command, "<<")
	if idx < 0 {
		return command
	}
	return command[:idx]
}

// isSafeAbsolutePath checks if an absolute path is a known safe system directory.
// Paths under /bin/, /usr/bin/, /usr/local/bin/, /sbin/, /usr/sbin/, /opt/homebrew/ are allowed.
func isSafeAbsolutePath(pathStr string) bool {
	safePrefixes := []string{"/bin/", "/usr/bin/", "/usr/local/bin/", "/sbin/", "/usr/sbin/", "/opt/homebrew/"}
	for _, prefix := range safePrefixes {
		if strings.HasPrefix(pathStr, prefix) {
			return true
		}
	}
	return false
}
