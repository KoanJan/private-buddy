// Package task implements the autonomous task execution system for world-interaction requests.
//
// This package provides the task execution pipeline that handles agent-based
// task execution when the chat system determines that a user request requires
// world interaction (e.g., file operations, web searches, code execution).
//
// The main entry point is TaskExecutor.Execute, which:
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
	"fmt"
	"strings"

	"private-buddy-server/internal/config"
	"private-buddy-server/internal/model"
	"private-buddy-server/internal/service/llm"
	taskcontext "private-buddy-server/internal/service/task/context"
	"private-buddy-server/internal/service/task/tools"

	applogger "private-buddy-server/internal/logger"

	"gorm.io/gorm"
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

// TaskExecutor is the self-contained task execution service.
//
// This is the only public interface of the task module.
// It accepts a task requirement and an LLM configuration,
// runs the task loop internally, and returns a TaskResult.
// The task executor is self-contained and autonomous — it creates its own
// Task Loop, LLM client, tools, and context manager for each execution.
// Nothing from the internal execution leaks into the chat context.
type TaskExecutor struct {
	db *gorm.DB
}

func NewTaskExecutor(db *gorm.DB) *TaskExecutor {
	return &TaskExecutor{db: db}
}

// TaskParams contains all parameters needed for task execution.
type TaskParams struct {
	TaskRequirement string              // The rewritten task description to execute
	LLMConfig       *model.LLMConfig    // LLM configuration for the task
	MaxIterations   int                 // Override for max loop iterations (0 = use default)
	SessionID       int64               // Session ID for interaction records and workspace
	UserMsgID       int64               // User message ID that triggered execution
	AgentMsgID      int64               // Agent message ID for the result target
	SearchConfig    *model.SearchConfig // Search configuration for web search tool
	DeliveryType    string              // Expected delivery type ("text" or "file"), affects system prompt
}

// Execute runs a task and returns the result.
//
// This method is the single entry point for task execution.
// It creates all necessary components internally and runs
// the task loop to completion.
//
// Execution steps:
//  1. Initialize workspace structure (.meta/task.md, .meta/notes.md, output/)
//  2. Read workspace files (task content, notes content)
//  3. Build system prompt with delivery type and available tools
//  4. Create tool list (bash, write_notes, optionally web_search)
//  5. Create ContextManager with iteration window
//  6. Create LLM client with tool binding
//  7. Run task loop to completion
//  8. Read final notes and return TaskResult
func (te *TaskExecutor) Execute(params TaskParams) *TaskResult {
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

	systemPrompt := te.buildSystemPrompt(params.SessionID, params.DeliveryType)

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

	toolList := te.buildToolList(workspace, params.SessionID, params.SearchConfig, workspaceRoot, notesMaxChars)

	taskLoop := NewTaskLoop(
		llmClient,
		params.LLMConfig,
		toolList,
		contextManager,
		maxIterations,
		te.db,
		params.SessionID,
		params.UserMsgID,
		params.AgentMsgID,
		writeNotesTool,
	)

	loopResult := taskLoop.Run()

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

	if loopResult.Status == "success" && loopResult.Result != nil {
		result.Status = "success"
		result.Output = *loopResult.Result
		applogger.L.Info("TaskExecutor completed successfully",
			"session_id", params.SessionID,
			"output_len", len(result.Output),
		)
	} else {
		result.Status = "failure"
		if loopResult.Reason != nil {
			result.Error = *loopResult.Reason
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
func (te *TaskExecutor) buildSystemPrompt(sessionID int64, deliveryType string) string {
	workspace := getSessionWorkspace(sessionID)
	workingDir := fmt.Sprintf("%s/output", workspace)
	hasWebSearch := te.hasWebSearch()

	parts := []string{
		"You are a helpful AI agent that can execute tasks using tools.",
		"",
		"Available tools:",
		"- bash: Execute shell commands in your working directory",
		"- write_notes: Append structured entries to your notes.md",
	}

	if hasWebSearch {
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
func (te *TaskExecutor) hasWebSearch() bool {
	var searchConfig model.SearchConfig
	if err := te.db.First(&searchConfig).Error; err != nil {
		return false
	}
	return searchConfig.IsAvailable()
}

// buildToolList creates the list of available tools for the task loop.
// Always includes bash and write_notes; adds web_search if search config is available.
func (te *TaskExecutor) buildToolList(workspace string, sessionID int64, searchConfig *model.SearchConfig, workspaceRoot string, notesMaxChars int) []tools.Tool {
	toolList := []tools.Tool{
		tools.NewBashTool(workspace),
		tools.NewWriteNotesTool(sessionID, workspaceRoot, notesMaxChars),
	}

	if searchConfig != nil && searchConfig.IsAvailable() {
		toolList = append(toolList, tools.NewWebSearchTool(searchConfig))
	}

	return toolList
}
