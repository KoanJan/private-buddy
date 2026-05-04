package tools

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/sashabaranov/go-openai"

	applogger "private-buddy-server/internal/logger"
)

type BashTool struct {
	workspace string
}

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

type BashResult struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exit_code"`
}

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
			return true
		}
	}
	return false
}

func stripHeredocContent(command string) string {
	idx := strings.Index(command, "<<")
	if idx < 0 {
		return command
	}
	return command[:idx]
}

func isSafeAbsolutePath(pathStr string) bool {
	safePrefixes := []string{"/bin/", "/usr/bin/", "/usr/local/bin/", "/sbin/", "/usr/sbin/", "/opt/homebrew/"}
	for _, prefix := range safePrefixes {
		if strings.HasPrefix(pathStr, prefix) {
			return true
		}
	}
	return false
}
