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

type TaskExecutor struct {
	db *gorm.DB
}

func NewTaskExecutor(db *gorm.DB) *TaskExecutor {
	return &TaskExecutor{db: db}
}

type TaskParams struct {
	TaskRequirement string
	LLMConfig       *model.LLMConfig
	MaxIterations   int
	SessionID       int64
	UserMsgID       int64
	AgentMsgID      int64
	SearchConfig    *model.SearchConfig
	DeliveryType    string
}

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

	llmClient := llm.NewChatModel(
		params.LLMConfig.BaseURL,
		params.LLMConfig.APIKey,
		params.LLMConfig.ModelID,
		0,
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

func (te *TaskExecutor) hasWebSearch() bool {
	var searchConfig model.SearchConfig
	if err := te.db.First(&searchConfig).Error; err != nil {
		return false
	}
	return searchConfig.IsAvailable()
}

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
