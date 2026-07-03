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
	"encoding/json"
	"fmt"
	"runtime"
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
// Notes, Workspace, and NotesPath are always populated for observability.
type TaskResult struct {
	Status      string `json:"status"`
	Output      string `json:"output,omitempty"`
	Error       string `json:"error,omitempty"`
	Notes       string `json:"notes,omitempty"`
	Workspace   string `json:"workspace,omitempty"`
	NotesPath   string `json:"notes_path,omitempty"`
	NotesLength int    `json:"notes_length,omitempty"`
}

// GuidanceDirective is a structured guidance message sent from the Runtime
// to the TaskLoop during execution. It carries both the executable directive
// (Guidance) and the cognitive context explaining why (Reason).
//
// This struct is passed through the guidance channel instead of a bare string,
// so the TaskLoop's LLM can understand the full context of a route or cancel
// decision — not just the "what" but also the "why".
type GuidanceDirective struct {
	Guidance string // What to do: the executable directive
	Reason   string // Why: user's original message, inferred intent, and Decide's reasoning
}

// RunTaskParams contains all parameters needed for the full task pipeline.
// After the cognitive order refactoring, Guidance from the Decide phase
// replaces the old Rewrite step — comprehension and decision have already
// produced a clear execution intent, so rewriting is unnecessary.
type RunTaskParams struct {
	LLMConfig  *model.LLMConfig
	SessionID  int64
	AgentID    int64
	UserMsgID  int64
	WorkID     int64
	Guidance   string // Execution intent from Decide phase (replaces Rewrite)
	Ctx        context.Context
	OnNotify   func(data string)        // Optional callback for frontend notifications
	GuidanceCh <-chan GuidanceDirective // Channel for receiving new guidance during execution
}

// RunTask executes the full task pipeline using Guidance as the task requirement.
//
// After the cognitive order refactoring, the Comprehend-Decide pipeline has
// already produced a clear execution intent (Guidance). This replaces the
// old Rewrite step — there is no need to rewrite the user message because
// all cognitive work (understanding intent, resolving references, determining
// what to do) was completed before this point.
//
// New guidance can arrive via GuidanceCh during execution. The TaskLoop
// observes the channel at each iteration boundary and injects new directives
// as environment events in the ReAct cycle.
func RunTask(params RunTaskParams) *TaskResult {
	// Notify frontend that agent is processing
	if params.OnNotify != nil {
		notifyData, _ := json.Marshal(map[string]string{
			"type":    "agent_processing",
			"message": "Agent is processing your request...",
		})
		params.OnNotify(string(notifyData))
	}

	applogger.Info("RunTask: starting with Guidance",
		"session_id", params.SessionID,
		"guidance", params.Guidance,
	)

	// Load search config for web search tool
	var searchConfig model.SearchConfig
	if err := database.DB.Where("is_active = ?", true).First(&searchConfig).Error; err != nil {
		applogger.Warn("failed to load active search config, proceeding without search", "error", err)
	}

	return Execute(TaskParams{
		TaskRequirement: params.Guidance, // Guidance IS the task requirement
		Guidance:        params.Guidance,
		LLMConfig:       params.LLMConfig,
		MaxIterations:   0,
		SessionID:       params.SessionID,
		AgentID:         params.AgentID,
		UserMsgID:       params.UserMsgID,
		WorkID:          params.WorkID,
		SearchConfig:    &searchConfig,
		Ctx:             params.Ctx,
		GuidanceCh:      params.GuidanceCh,
	})
}

// TaskParams contains all parameters needed for task execution.
type TaskParams struct {
	TaskRequirement string                   // The task description to execute (from Decide phase Guidance)
	Guidance        string                   // Execution intent from Decide phase, injected into system prompt
	LLMConfig       *model.LLMConfig         // LLM configuration for the task
	MaxIterations   int                      // Override for max loop iterations (0 = use default)
	SessionID       int64                    // Session ID for interaction records and workspace
	AgentID         int64                    // Agent ID for tools that need agent context (e.g., wake_me_when)
	UserMsgID       int64                    // User message ID that triggered execution
	WorkID          int64                    // Work ID for interaction record association
	SearchConfig    *model.SearchConfig      // Search configuration for web search tool
	Ctx             context.Context          // Cancellation context from the caller
	GuidanceCh      <-chan GuidanceDirective // Channel for receiving new guidance during execution
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

	applogger.Info("TaskExecutor starting",
		"session_id", params.SessionID,
		"max_iterations", maxIterations,
	)

	workspace := initWorkspace(params.SessionID)

	settings := config.Get()
	iterationWindow := settings.ContextWindowIterations
	notesMaxChars := settings.NotesMaxChars
	workspaceRoot := settings.GetWorkspaceRoot()

	writeNotesTool := tools.NewWriteNotesTool(params.SessionID, workspaceRoot, notesMaxChars)
	notesContent := writeNotesTool.ReadNotes()

	sessionWorkspace := getSessionWorkspace(params.SessionID)
	sessionOutputDir := getSessionOutputDir(params.SessionID)
	toolList := buildToolList(sessionWorkspace, sessionOutputDir, params.SessionID, params.AgentID, params.UserMsgID, params.SearchConfig, workspaceRoot, notesMaxChars)

	// Build the system prompt AFTER the tool list so that tool descriptions
	// can be generated from the registered tools.
	systemPrompt := buildSystemPrompt(params.SessionID, params.Guidance, toolList)

	contextManager := taskcontext.NewContextManager(
		systemPrompt,
		iterationWindow,
		notesContent,
	)

	llmClient := llm.NewChatModelWithTemperature(
		params.LLMConfig.BaseURL,
		params.LLMConfig.APIKey,
		params.LLMConfig.ModelID,
		llm.TemperatureCreative,
	)

	taskLoop := NewTaskLoop(
		llmClient,
		params.LLMConfig,
		toolList,
		contextManager,
		maxIterations,
		params.SessionID,
		params.UserMsgID,
		params.WorkID,
		writeNotesTool,
		params.GuidanceCh,
	)

	loopResult := taskLoop.Run(params.Ctx)

	finalNotes := writeNotesTool.ReadNotes()

	// Note: <workspace>/.meta/fingerprint.txt is no longer written here.
	// Its responsibility moved to the reflection pipeline (reflectSession),
	// which writes it at the end of each reflection to mark "this is what
	// notes.md looked like when I last processed it". The heartbeat then
	// compares the current notes.md hash against this file to decide whether
	// to re-trigger reflection.

	result := &TaskResult{
		Workspace: workspace,
		NotesPath: fmt.Sprintf("%s/.meta/notes.md", workspace),
		Notes:     finalNotes,
	}

	if finalNotes != "" {
		result.NotesLength = len(finalNotes)
	}

	if loopResult.Status == "success" && loopResult.Result != "" {
		result.Status = "success"
		result.Output = loopResult.Result
		applogger.Info("TaskExecutor completed successfully",
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
		applogger.Error("TaskExecutor failed",
			"session_id", params.SessionID,
			"error", result.Error,
		)
	}

	return result
}

// buildSystemPrompt constructs the static system prompt for the task loop.
// Built once at task start; includes basic rules, tool descriptions (generated
// from the registered tool list), working directory, delivery guidance,
// static instruction blocks, an experience hint, and the execution directive.
//
// Dynamic content (iteration counts, notes length) is appended separately by
// ContextManager at each iteration to preserve LLM prefix caching.
func buildSystemPrompt(sessionID int64, guidance string, toolList []tools.Tool) string {
	sessionDir := getSessionWorkspace(sessionID)
	outputDir := getSessionOutputDir(sessionID)

	// Generate tool descriptions from the registered tools.
	toolLines := []string{"Available tools:"}
	for _, t := range toolList {
		toolLines = append(toolLines, fmt.Sprintf("- %s: %s", t.Name(), t.Description()))
	}

	parts := []string{
		"You can execute tasks using tools.",
		"",
	}
	parts = append(parts, toolLines...)

	parts = append(parts,
		"",
		"CRITICAL: Before calling any tool, you MUST first explain your reasoning",
		"in the content field. Describe what you plan to do and why.",
		"Only after explaining your thought process, make the tool call.",
		"",
		"Always verify your actions by checking the results.",
		"",
		fmt.Sprintf("Your session directory is: %s", sessionDir),
		fmt.Sprintf("Your default working directory is: %s", outputDir),
		fmt.Sprintf("Operating system: %s", runtime.GOOS),
		"",
		"WORKSPACE ORGANIZATION:",
		fmt.Sprintf("- Your default working directory is: %s", outputDir),
		"- Before creating files, consider whether this task relates to an existing project:",
		"  - If starting a new project (e.g., building an app, writing a report), create a dedicated subdirectory for it",
		"  - If continuing or modifying existing work, first check what subdirectories exist and work within the appropriate one",
		"- This keeps your workspace organized but is not enforced — use your judgment",
		"",
		"DELIVERY GUIDANCE:",
		"When the task is complete, your final output will be shown to the person you are working for.",
		"First, determine what kind of deliverable the task requires:",
		"- File deliverables (code, documents, etc.) → provide the full ABSOLUTE file path so they can click to open it",
		"- Information deliverables (answers, analysis, etc.) → present the information directly in your response",
		"- Mixed deliverables → provide both the information summary and the file paths",
		"",
		"They should be able to reach the deliverable directly from your response — zero friction:",
		"- ALWAYS provide ABSOLUTE file paths, not relative paths. Use `pwd` if unsure of the full path",
		"- Never make them guess where things are or manually type paths",
		"- Never fabricate or guess file paths — verify with `pwd` or `ls` if needed",
		"- If something was partially completed, clearly state what's done and what's remaining",
		"",
		"[Understanding Current State]",
		"To understand the current project state:",
		"- Use 'ls -la' to see files in your working directory",
		"- Use 'cat <filename>' to read file contents",
		"- Use 'find . -type f' to discover all files",
		"- Check your NOTES (provided above) for previous progress",
		"",
		"[NOTES Usage Guide]",
		"The write_notes tool appends structured entries to your notes.",
		"",
		"Entry types:",
		"- observation: Something you discovered",
		"- decision: A choice you made (explain why)",
		"- finding: A key result or conclusion",
		"- correction: A fix to a previous entry (use conflicts_with)",
		"- progress: Current status and next steps",
		"",
		"Best practices:",
		"- Each entry is APPENDED, not overwritten",
		"- Write CONCISE entries — notes have a size limit",
		"- Only write IMPORTANT information — skip trivial or obvious facts",
		"- Ask: would losing this information hurt the task? If not, skip it",
		"- Include file references when relevant",
		"- Use conflicts_with when correcting earlier decisions",
		"- Write self-contained entries (future LLM calls have no memory)",
		"",
		"[Critical Identifiers]",
		"- If you encounter an identifier that cannot be recovered through filesystem inspection (ls, find, git log, etc.), record it explicitly in your notes.",
		"- File paths are recoverable — you don't need to record them.",
		"- External API response IDs, user-provided tokens, and unique session identifiers should be preserved.",
		"",
		"[Past Experience]",
		"You have past experiences (lessons learned from prior tasks). Use scan_my_experience to search for relevant experiences by keyword, then recall_my_experience to read the full content of a specific one.",
	)

	// Inject Guidance as a self-directive in the system prompt.
	// This is the execution intent from the Decide phase — the agent's
	// own understanding of what it should accomplish.
	if guidance != "" {
		parts = append(parts,
			"",
			"[Execution Directive]",
			guidance,
		)
	}

	return strings.Join(parts, "\n")
}

// buildToolList creates the list of available tools for the task loop.
// Always includes bash, write_notes, wake_me_when, scan_my_experience, and
// recall_my_experience; adds web_search if search config is available.
func buildToolList(sessionWorkspace, workDir string, sessionID, agentID, triggerMessageID int64, searchConfig *model.SearchConfig, workspaceRoot string, notesMaxChars int) []tools.Tool {
	toolList := []tools.Tool{
		tools.NewBashTool(sessionWorkspace, workDir),
		tools.NewWriteNotesTool(sessionID, workspaceRoot, notesMaxChars),
		tools.NewWakeMeWhenTool(agentID, sessionID, triggerMessageID),
		tools.NewScanExperienceTool(agentID),
		tools.NewRecallExperienceTool(agentID),
	}

	if searchConfig != nil && searchConfig.IsAvailable() {
		toolList = append(toolList, tools.NewWebSearchTool(searchConfig))
	}

	return toolList
}
