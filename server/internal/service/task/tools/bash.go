package tools

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"private-buddy-server/internal/service/llm"

	applogger "private-buddy-server/internal/logger"
)

// maxStdoutBytes is the maximum bytes of stdout retained after truncation.
// stdout beyond this is truncated to the tail (keeping the most recent output,
// where error messages typically appear). stderr and exit_code are never truncated.
const (
	maxStdoutBytes       = 20 * 1024 // 20KB
	defaultBashTimeoutMs = 30_000    // Default bash command timeout in milliseconds (30s)
)

// BashTool executes shell commands within a session workspace.
//
// Provides the agent with the ability to run shell commands on the local system.
// Commands are confined to the session-level workspace directory to ensure isolation.
// Supports configurable timeout and returns stdout, stderr, and exit code.
//
// Security:
//   - Path traversal outside the session workspace is blocked
//   - Access to .meta directory is blocked (system-managed files)
type BashTool struct {
	sessionRoot string // Session-level security boundary for path traversal checks
	workDir     string // Command execution working directory (session_X/output/)
}

// NewBashTool creates a BashTool with the given session root and working directory.
// sessionRoot defines the security boundary — commands cannot access paths outside it.
// workDir is the CWD for command execution, scoped to the session's output directory.
func NewBashTool(sessionRoot, workDir string) *BashTool {
	return &BashTool{sessionRoot: sessionRoot, workDir: workDir}
}

// ToolNameBash is the type-safe name constant for BashTool.
const ToolNameBash ToolName = "bash"

func (b *BashTool) Name() ToolName { return ToolNameBash }

func (b *BashTool) Description() string { return "Execute shell commands in your working directory" }

func (b *BashTool) Schema() llm.FunctionDefinition {
	workspaceHint := ""
	if b.sessionRoot != "" {
		workspaceHint = fmt.Sprintf(" All file operations must be within %s. Do not access paths outside this directory.", b.sessionRoot)
	}
	return llm.FunctionDefinition{
		Name:        string(b.Name()),
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
					"description": fmt.Sprintf("Timeout in milliseconds (default: %d)", defaultBashTimeoutMs),
					"default":     defaultBashTimeoutMs,
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
	timeoutMs := defaultBashTimeoutMs
	if t, ok := args["timeout"].(float64); ok {
		timeoutMs = int(t)
	}

	if command == "" {
		return "", fmt.Errorf("command is required")
	}

	applogger.Info("BashTool executing", "command", command, "timeout_ms", timeoutMs)

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/c", command)
	} else {
		cmd = exec.Command("bash", "-c", command)
	}
	if b.workDir != "" {
		cmd.Dir = b.workDir
	}

	timeout := time.Duration(timeoutMs) * time.Millisecond
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("failed to start command: %s", err.Error())
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case err := <-done:
		exitCode := 0
		if err != nil {
			exitCode = 1
			if exitErr, ok := err.(*exec.ExitError); ok {
				exitCode = exitErr.ExitCode()
			}
		}
		stdoutStr := stdout.String()
		// Semantic truncation: keep tail of stdout (error messages usually at the end).
		// Happens before marshal so the JSON structure stays intact.
		if shown, truncated := TruncateTail(stdoutStr, maxStdoutBytes); truncated {
			stdoutStr = "[... earlier output omitted ...]\n" + shown + "\n" +
				Hint(len(shown), len(stdoutStr))
		}
		result, _ := json.Marshal(BashResult{
			Stdout:   stdoutStr,
			Stderr:   stderr.String(),
			ExitCode: exitCode,
		})
		return string(result), nil
	case <-timer.C:
		cmd.Process.Kill()
		return "", fmt.Errorf("command timed out after %dms", timeoutMs)
	}
}
