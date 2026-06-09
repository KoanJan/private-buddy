// Package task implements the autonomous task execution system for world-interaction requests.
//
// This package provides the task execution pipeline that handles agent-based
// task execution when the chat system determines that a user request requires
// world interaction (e.g., file operations, web searches, code execution).
//
// The main entry point is Execute, which:
//  1. Initializes the session workspace structure
//  2. Builds the system prompt and tool list
//  3. Creates the context manager with iteration window
//  4. Runs the ReAct task loop to completion
//  5. Returns a TaskResult with success/failure status
//
// Design principles:
//   - Input: task requirement (structured, not raw user message)
//   - Output: final result (success result or failure with reason)
//   - Internal isolation: all process info is hidden from the outside
//   - No pollution of the chat system
package task

import (
	"context"
	"fmt"
	"strings"

	"private-buddy-server/internal/config"
	"private-buddy-server/internal/database"
	"private-buddy-server/internal/model"
	"private-buddy-server/internal/service/llm"
	taskcontext "private-buddy-server/internal/service/task/context"
	"private-buddy-server/internal/service/task/tools"

	applogger "private-buddy-server/internal/logger"
)

// TaskResult represents the outcome of a task execution.
// On success, Output contains the final content. On failure, Error contains the reason.
// Notes, Workspace, NotesPath, and TaskPath are always populated for observability.
type TaskResult struct {
	Status      string `json:"status"`
	Output      string `json:"output,omitempty"`
	Error       string `json:"error,omitempty"`
	Notes       string `json:"notes,omitempty"`
	Workspace   string `json:"workspace,omitempty"`
	NotesPath   string `json:"notes_path,omitempty"`
	TaskPath    string `json:"task_path,omitempty"`
	NotesLength int    `json:"notes_length,omitempty"`
}

// TaskParams contains all parameters needed for task execution.
type TaskParams struct {
	TaskRequirement string              // The rewritten task description to execute
	LLMConfig       *model.LLMConfig    // LLM configuration for the task
	MaxIterations   int                 // Override for max loop iterations (0 = use default)
	SessionID       int64               // Session ID for interaction records and workspace
	AgentID         int64               // Agent ID for tools that need agent context (e.g., wake_me_when)
	UserMsgID       int64               // User message ID that triggered execution
	DraftID         int64               // Draft ID for interaction record association
	SearchConfig    *model.SearchConfig // Search configuration for web search tool
	DeliveryType    string              // Expected delivery type ("text" or "file"), affects system prompt
	Ctx             context.Context     // Cancellation context from the caller
}

// Execute runs a task and returns the result.
//
// This is the single entry point for task execution.
// It creates all necessary components internally and runs
// the task loop to completion.
func Execute(params TaskParams) *TaskResult {
	maxIterations := params.MaxIterations
	if maxIterations <= 0 {
		maxIterations = defaultMaxIterations
	}

	applogger.L.Info("TaskExecutor starting",
		"session_id", params.SessionID,
		"max_iterations", maxIterations,
	)

	workspace := initSessionWorkspace(params.SessionID, params.TaskRequirement)

	taskContent := readTaskMD(params.SessionID)

	settings := config.Get()
	iterationWindow := settings.ContextWindowIterations
	notesMaxChars := settings.NotesMaxChars
	workspaceRoot := settings.GetWorkspaceRoot()

	writeNotesTool := tools.NewWriteNotesTool(params.SessionID, workspaceRoot, notesMaxChars)
	notesContent := writeNotesTool.ReadNotes()

	systemPrompt := buildSystemPrompt(params.SessionID, params.DeliveryType)

	contextManager := taskcontext.NewContextManager(
		systemPrompt,
		iterationWindow,
		taskContent,
		notesContent,
	)

	llmClient := llm.NewChatModelWithTemperature(
		params.LLMConfig.BaseURL,
		params.LLMConfig.APIKey,
		params.LLMConfig.ModelID,
		llm.TemperatureCreative,
	)

	toolList := buildToolList(workspace, params.SessionID, params.AgentID, params.UserMsgID, params.SearchConfig, workspaceRoot, notesMaxChars)

	taskLoop := NewTaskLoop(
		llmClient,
		params.LLMConfig,
		toolList,
		contextManager,
		maxIterations,
		params.SessionID,
		params.UserMsgID,
		params.DraftID,
		writeNotesTool,
	)

	loopResult := taskLoop.Run(params.Ctx)

	finalNotes := writeNotesTool.ReadNotes()

	result := &TaskResult{
		Workspace: workspace,
		NotesPath: fmt.Sprintf("%s/.meta/notes.md", workspace),
		TaskPath:  fmt.Sprintf("%s/.meta/task.md", workspace),
		Notes:     finalNotes,
	}

	if finalNotes != "" {
		result.NotesLength = len(finalNotes)
	}

	if loopResult.Status == "success" && loopResult.Result != "" {
		result.Status = "success"
		result.Output = loopResult.Result
		applogger.L.Info("TaskExecutor completed successfully",
			"session_id", params.SessionID,
			"output_len", len(result.Output),
		)
	} else {
		result.Status = "failure"
		if loopResult.Reason != "" {
			result.Error = loopResult.Reason
		} else {
			result.Error = "Unknown error"
		}
		applogger.L.Error("TaskExecutor failed",
			"session_id", params.SessionID,
			"error", result.Error,
		)
	}

	return result
}

// buildSystemPrompt constructs the system prompt for the task loop.
// Includes basic rules, available tools, working directory, and delivery type guidance.
func buildSystemPrompt(sessionID int64, deliveryType string) string {
	workspace := getSessionWorkspace(sessionID)
	workingDir := fmt.Sprintf("%s/output", workspace)
	hasWS := hasWebSearch()

	parts := []string{
		"You are a helpful AI agent that can execute tasks using tools.",
		"",
		"Available tools:",
		"- bash: Execute shell commands in your working directory",
		"- write_notes: Append structured entries to your notes.md",
		"- wake_me_when: Set an alarm to wake yourself at a future time (e.g., reminders, follow-ups)",
	}

	if hasWS {
		parts = append(parts, "- web_search: Search the web for information")
	}

	parts = append(parts,
		"",
		"CRITICAL: Before calling any tool, you MUST first explain your reasoning",
		"in the content field. Describe what you plan to do and why.",
		"Only after explaining your thought process, make the tool call.",
		"",
		"Always verify your actions by checking the results.",
		"",
		fmt.Sprintf("Your working directory is: %s", workingDir),
		"All files you create MUST be within this directory.",
		"Do not write files to any other location.",
	)

	if deliveryType == "file" {
		parts = append(parts,
			"",
			"DELIVERY TYPE: file",
			"The user expects file deliverables (code, documents, etc.).",
			"Create the required files in your working directory.",
			"When finished, list all created files and provide a summary.",
		)
	} else if deliveryType == "text" {
		parts = append(parts,
			"",
			"DELIVERY TYPE: text",
			"The user expects a text answer as the deliverable.",
			"Provide a clear, concise text response.",
			"You may use tools to gather information, but the final",
			"output should be a direct text answer.",
		)
	}

	parts = append(parts,
		"",
		"When the task is complete, provide a clear and concise summary of what was accomplished.",
		"If the task cannot be completed, explain why and what was attempted.",
	)

	return strings.Join(parts, "\n")
}

// hasWebSearch checks if web search is available via an active search config.
func hasWebSearch() bool {
	var searchConfig model.SearchConfig
	if err := database.DB.First(&searchConfig).Error; err != nil {
		return false
	}
	return searchConfig.IsAvailable()
}

// buildToolList creates the list of available tools for the task loop.
// Always includes bash, write_notes, and wake_me_when; adds web_search if search config is available.
func buildToolList(workspace string, sessionID, agentID, triggerMessageID int64, searchConfig *model.SearchConfig, workspaceRoot string, notesMaxChars int) []tools.Tool {
	toolList := []tools.Tool{
		tools.NewBashTool(workspace),
		tools.NewWriteNotesTool(sessionID, workspaceRoot, notesMaxChars),
		tools.NewWakeMeWhenTool(agentID, sessionID, triggerMessageID),
	}

	if searchConfig != nil && searchConfig.IsAvailable() {
		toolList = append(toolList, tools.NewWebSearchTool(searchConfig))
	}

	return toolList
}
