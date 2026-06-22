package task

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"private-buddy-server/internal/config"
	"private-buddy-server/internal/database"
	"private-buddy-server/internal/model"
	"private-buddy-server/internal/service/llm"
	taskcontext "private-buddy-server/internal/service/task/context"
	"private-buddy-server/internal/service/task/tools"

	applogger "private-buddy-server/internal/logger"
)

// defaultMaxIterations is the default maximum number of ReAct loop iterations.
const defaultMaxIterations = 90

// TaskLoop implements the ReAct-style task loop for autonomous task execution.
//
// The loop iterates:
//   - Call LLM with current context (window-controlled by ContextManager)
//   - If LLM returns tool_calls: execute tools, append results, continue
//   - If LLM returns stop: deliver the content
//   - If max_iterations reached: deliver failure with reason
//
// Every iteration is recorded to the interactions table with:
//   - type=1 (request): the messages sent to the LLM
//   - type=2 (response): the LLM output (content, tool_calls, finish_reason)
//   - type=3 (guidance): external guidance directive received at iteration boundary
//
// Notes checkpoint strategy:
//   - Agent can voluntarily call write_notes at any time
//   - Forced checkpoint only when distance from last voluntary write >= window
//   - This respects agent's autonomy while ensuring memory persistence
//   - Final iteration always writes notes if task not completed
type TaskLoop struct {
	llmClient        *llm.ChatModel              // Main LLM client with tool binding
	llmConfig        *model.LLMConfig            // LLM config for creating checkpoint client
	toolRegistry     map[string]tools.Tool       // Tool name -> Tool mapping
	contextManager   *taskcontext.ContextManager // Context manager with window control
	maxIterations    int                         // Maximum number of loop iterations
	sessionID        int64                       // Session ID for interaction records
	userMsgID        int64                       // User message ID that triggered execution
	workID           int64                       // Work ID for interaction record association
	writeNotesTool   *tools.WriteNotesTool       // Write notes tool for checkpoint iterations
	checkpointClient *llm.ChatModel              // Lazy-initialized LLM client for checkpoint iterations
	lastNotesIter    int                         // Last iteration where write_notes was called (voluntary or forced)
	guidanceCh       <-chan GuidanceDirective    // Channel for observing new guidance during execution
}

// NewTaskLoop creates a new TaskLoop instance.
// The tool list is converted to a name-keyed registry for efficient lookup during execution.
func NewTaskLoop(
	llmClient *llm.ChatModel,
	llmConfig *model.LLMConfig,
	toolList []tools.Tool,
	contextManager *taskcontext.ContextManager,
	maxIterations int,
	sessionID, userMsgID, workID int64,
	writeNotesTool *tools.WriteNotesTool,
	guidanceCh <-chan GuidanceDirective,
) *TaskLoop {
	registry := make(map[string]tools.Tool)
	for _, t := range toolList {
		registry[t.Name()] = t
	}

	return &TaskLoop{
		llmClient:      llmClient,
		llmConfig:      llmConfig,
		toolRegistry:   registry,
		contextManager: contextManager,
		maxIterations:  maxIterations,
		sessionID:      sessionID,
		userMsgID:      userMsgID,
		workID:         workID,
		writeNotesTool: writeNotesTool,
		guidanceCh:     guidanceCh,
	}
}

// LoopResult represents the outcome of the task loop execution.
type LoopResult struct {
	Status string `json:"status"`           // "success" or "failure"
	Result string `json:"result,omitempty"` // Final content on success
	Reason string `json:"reason,omitempty"` // Failure reason on failure
}

// Run executes the agent loop.
//
// This is the main entry point. It runs the ReAct loop until:
//   - LLM returns a stop response (success)
//   - Max iterations reached (failure, after writing notes)
//
// The task requirement is already injected via ContextManager
// (as part of the system prompt with Guidance), so it is not passed
// as a parameter here.
func (tl *TaskLoop) Run(ctx context.Context) *LoopResult {
	applogger.L.Info("TaskLoop starting",
		"max_iterations", tl.maxIterations,
		"session_id", tl.sessionID,
		"work_id", tl.workID,
	)

	for iteration := 1; iteration <= tl.maxIterations; iteration++ {
		// Check if the task has been cancelled (e.g., session deleted)
		if ctx != nil && ctx.Err() != nil {
			applogger.L.Info("TaskLoop cancelled, stopping execution",
				"session_id", tl.sessionID,
				"iteration", iteration,
			)
			return &LoopResult{Status: "failure", Reason: "task cancelled"}
		}

		// Observe new guidance from the channel at each iteration boundary.
		// New guidance is an environment event that the agent must observe
		// in the ReAct cycle — it represents a change in execution intent
		// (e.g., user correction, approach change, cancellation).
		// Drain all pending guidance to handle multiple updates.
		tl.observeNewGuidance(iteration)

		applogger.L.Info("TaskLoop iteration", "iteration", iteration, "max", tl.maxIterations)

		if tl.writeNotesTool != nil {
			tl.writeNotesTool.TrimNotes()
			tl.contextManager.RefreshNotes(tl.writeNotesTool.ReadNotes())
		}

		messages := tl.contextManager.BuildMessages()

		isCheckpoint := tl.isCheckpointIteration(iteration)
		isFinal := iteration == tl.maxIterations

		if isCheckpoint || isFinal {
			result := tl.runNotesIteration(ctx, iteration, messages, isFinal)
			if result.Status == "failure" {
				return result
			}
			continue
		}

		tl.writeInteraction(iteration, model.InteractionTypeRequest, map[string]interface{}{
			"messages": messages,
		})

		response, err := tl.invokeLLM(ctx, messages)
		if err != nil {
			applogger.L.Error("TaskLoop LLM error", "iteration", iteration, "error", err)
			return &LoopResult{Status: "failure", Reason: fmt.Sprintf("LLM invocation failed at iteration %d: %s", iteration, err.Error())}
		}

		finishReason := response.FinishReason
		content := response.Content
		toolCalls := response.ToolCalls

		switch finishReason {
		case "stop":
			contentPreview := content
			if len(contentPreview) > 500 {
				contentPreview = contentPreview[:500]
			}
			applogger.L.Debug("TaskLoop LLM response",
				"finish_reason", "stop",
				"content", contentPreview,
			)
		case "tool_calls":
			tcSummary := make([]map[string]interface{}, 0, len(toolCalls))
			for _, tc := range toolCalls {
				argsPreview := tc.Function.Arguments
				if len(argsPreview) > 200 {
					argsPreview = argsPreview[:200]
				}
				tcSummary = append(tcSummary, map[string]interface{}{
					"id":   tc.ID,
					"name": tc.Function.Name,
					"args": argsPreview,
				})
			}
			contentPreview := content
			if len(contentPreview) > 500 {
				contentPreview = contentPreview[:500]
			}
			applogger.L.Debug("TaskLoop LLM response",
				"finish_reason", "tool_calls",
				"content", contentPreview,
				"tool_calls", fmt.Sprintf("%v", tcSummary),
			)
		case "length":
			contentPreview := content
			if len(contentPreview) > 500 {
				contentPreview = contentPreview[:500]
			}
			applogger.L.Debug("TaskLoop LLM response",
				"finish_reason", "length",
				"content", contentPreview,
			)
		}

		tl.writeInteraction(iteration, model.InteractionTypeResponse, map[string]interface{}{
			"content":       content,
			"tool_calls":    toolCalls,
			"finish_reason": finishReason,
		})

		switch finishReason {
		case "stop":
			applogger.L.Info("TaskLoop completed", "iteration", iteration)
			tl.updateNotesOnSuccess(ctx, iteration, content, messages)
			return &LoopResult{Status: "success", Result: content}

		case "tool_calls":
			if content != "" {
				applogger.L.Info("TaskLoop thoughts", "iteration", iteration, "thoughts", content[:min(500, len(content))])
			}

			// Discard reasoning content from tool_calls to establish an information
			// boundary between TaskLoop internals and the chat layer.
			//
			// When tool_calls are accompanied by reasoning content, that content
			// propagates into subsequent iterations and eventually leaks into
			// LoopResult.Result via the final stop response. The chat LLM then
			// misinterprets internal reasoning (e.g., "the command is correct")
			// as accomplished facts (e.g., "the service is running"), causing
			// hallucination in the final user-facing response.
			//
			// By discarding reasoning content here, we cut off the hallucination
			// at its source: internal process information stays inside TaskLoop,
			// and only tool calls and their results are carried forward. The LLM
			// can still reason about next steps from the task description and
			// tool results alone — the reasoning content is redundant signal.
			assistantMsg := llm.Message{
				Role:      "assistant",
				Content:   "",
				ToolCalls: toolCalls,
			}

			var toolResults []llm.Message
			hasWriteNotes := false
			for _, tc := range toolCalls {
				if tc.Function.Name == "write_notes" {
					hasWriteNotes = true
				}
				toolResult := tl.executeToolCall(tc)
				toolResults = append(toolResults, toolResult)
			}

			if hasWriteNotes {
				tl.lastNotesIter = iteration
				applogger.L.Info("Agent voluntarily called write_notes", "iteration", iteration)
			}

			tl.contextManager.AddIteration(assistantMsg, toolResults)

		case "length":
			applogger.L.Warn("TaskLoop finish_reason=length", "iteration", iteration)

			assistantMsg := llm.Message{
				Role:    "assistant",
				Content: content,
			}
			if len(toolCalls) > 0 {
				assistantMsg.ToolCalls = toolCalls
			}

			tl.contextManager.AddIteration(assistantMsg, nil)

			tl.contextManager.AddIteration(
				llm.Message{
					Role:    "user",
					Content: "[System] Your previous response was truncated due to length limits. Your tool calls were NOT executed. Please continue with a more concise response.",
				},
				nil,
			)

		default:
			applogger.L.Warn("TaskLoop unexpected finish_reason", "finish_reason", finishReason, "iteration", iteration)
		}
	}

	reason := fmt.Sprintf("Task did not complete within %d iterations", tl.maxIterations)
	return &LoopResult{Status: "failure", Reason: reason}
}

// observeNewGuidance drains all pending guidance from the channel and injects
// each as an observation (user message) into the ReAct cycle.
//
// In the ReAct paradigm, new guidance is an environment event — the agent
// must observe it, think about how it affects the current plan, and act
// accordingly. This is semantically different from a callback check: the
// guidance arrives as a channel event, modeling it as something that happens
// in the agent's environment that the agent must perceive.
//
// Each directive is written to two places to ensure full traceability:
// 1. Interactions table (type=3): audit trail and transition judgment record
// 2. Context manager: LLM sees it as a [New Directive] observation
//
// The iteration parameter is the current loop iteration number, so the
// guidance interaction record is grouped with the iteration that consumes it.
func (tl *TaskLoop) observeNewGuidance(iteration int) {
	if tl.guidanceCh == nil {
		return
	}
	for {
		select {
		case directive := <-tl.guidanceCh:
			applogger.L.Info("TaskLoop: observed new guidance",
				"session_id", tl.sessionID,
				"work_id", tl.workID,
				"iteration", iteration,
				"guidance", truncateString(directive.Guidance, 100),
				"reason", truncateString(directive.Reason, 100),
			)

			// 1. Write to interactions: this is a cognitive event that changes
			// the execution direction. Must be visible in the audit trail.
			tl.writeInteraction(iteration, model.InteractionTypeGuidance, map[string]interface{}{
				"guidance": directive.Guidance,
				"reason":   directive.Reason,
			})

			// 2. Inject as an observation in the ReAct cycle.
			// Both guidance (what to do) and reason (why) are included so the
			// LLM has the full cognitive context, not just the bare directive.
			tl.contextManager.AddIteration(llm.Message{
				Role:    "user",
				Content: fmt.Sprintf("[New Directive]\nGuidance: %s\nReason: %s", directive.Guidance, directive.Reason),
			}, nil)
		default:
			return
		}
	}
}

// isCheckpointIteration checks if this iteration should be a forced notes checkpoint.
//
// Checkpoint is triggered when:
//   - Distance from last voluntary write_notes >= window
//   - This respects agent's autonomy while ensuring memory persistence
//
// Final iteration is handled separately.
func (tl *TaskLoop) isCheckpointIteration(iteration int) bool {
	if iteration == tl.maxIterations {
		return false
	}
	window := tl.contextManager.IterationWindow()
	distance := iteration - tl.lastNotesIter
	return distance >= window
}

// runNotesIteration runs a notes checkpoint or final notes iteration.
//
// During this iteration, only write_notes tool is available.
// The agent must use it to persist information before older iterations
// are discarded from the context window.
//
// On final iteration (isFinal=true), returns failure result after saving notes.
// On checkpoint iteration, returns success to continue the loop.
func (tl *TaskLoop) runNotesIteration(ctx context.Context, iteration int, messages []llm.Message, isFinal bool) *LoopResult {
	if tl.writeNotesTool == nil {
		applogger.L.Error("Cannot run notes iteration: write_notes_tool not initialized")
		if isFinal {
			return &LoopResult{Status: "failure", Reason: "Task did not complete within max iterations"}
		}
		return &LoopResult{Status: "success"}
	}

	if tl.checkpointClient == nil {
		tl.checkpointClient = llm.NewChatModelWithTemperature(tl.llmConfig.BaseURL, tl.llmConfig.APIKey, tl.llmConfig.ModelID, llm.TemperatureCreative)
	}

	iterType := "checkpoint"
	if isFinal {
		iterType = "final"
	}
	applogger.L.Info("Running notes iteration", "type", iterType, "iteration", iteration)

	var checkpointMsg string
	if isFinal {
		checkpointMsg = `[Final Iteration - Save Your Progress]
You have reached the maximum number of iterations.
The task could not be completed in time.

MANDATORY: You must save your progress now using the write_notes tool.
This is the ONLY tool available to you.

Use write_notes to APPEND entries to your NOTES:
- entry_type: "progress" for current status
- entry_type: "finding" for key discoveries
- entry_type: "decision" for choices made

Example:
{
  "entry_type": "progress",
  "content": "Completed X, Y. Still need to do Z.",
  "references": ["result.json"]
}

Your notes will help the next execution continue from where you left off.`
	} else {
		checkpointMsg = `[Memory Checkpoint Required]
You have reached the limit of your working memory.
The oldest iterations are now invisible to you.

MANDATORY: You must write your notes now using the write_notes tool.
This is the ONLY tool available to you in this iteration.

Use write_notes to APPEND entries to your NOTES:
- entry_type: "progress" for current status and next steps
- entry_type: "finding" for key discoveries
- entry_type: "decision" for choices made and why
- entry_type: "observation" for important things noticed

Each entry is APPENDED, not overwritten. Include file references when relevant.

After writing notes, you will regain access to all tools.`
	}

	messagesWithCheckpoint := append(messages, llm.Message{
		Role:    "user",
		Content: checkpointMsg,
	})

	tl.writeInteraction(iteration, model.InteractionTypeRequest, map[string]interface{}{
		"messages":      messagesWithCheckpoint,
		"is_checkpoint": true,
	})

	toolDefs := []llm.FunctionDefinition{tl.writeNotesTool.Schema()}
	response, err := tl.checkpointClient.ChatWithTools(ctx, messagesWithCheckpoint, toolDefs)
	if err != nil {
		applogger.L.Error("Notes iteration LLM error", "error", err)
		if isFinal {
			return &LoopResult{Status: "failure", Reason: "Task did not complete within max iterations"}
		}
		return &LoopResult{Status: "failure", Reason: fmt.Sprintf("Notes iteration LLM invocation failed: %s", err.Error())}
	}

	finishReason := response.FinishReason
	content := response.Content
	toolCalls := response.ToolCalls

	tl.writeInteraction(iteration, model.InteractionTypeResponse, map[string]interface{}{
		"content":       content,
		"tool_calls":    toolCalls,
		"finish_reason": finishReason,
		"is_checkpoint": true,
	})

	if finishReason == "tool_calls" {
		var toolResults []llm.Message
		for _, tc := range toolCalls {
			toolCallID := tc.ID

			if tc.Function.Name != "write_notes" {
				applogger.L.Warn("Notes iteration: unexpected tool call", "tool", tc.Function.Name)
				toolResults = append(toolResults, llm.Message{
					Role:       "tool",
					ToolCallID: toolCallID,
					Content:    fmt.Sprintf("Error: tool '%s' is not available during notes iteration", tc.Function.Name),
				})
				continue
			}

			var args map[string]interface{}
			json.Unmarshal([]byte(tc.Function.Arguments), &args)

			applogger.L.Info("Notes iteration: executing write_notes")
			result, _ := tl.writeNotesTool.Execute(args)

			toolResults = append(toolResults, llm.Message{
				Role:       "tool",
				ToolCallID: toolCallID,
				Content:    result,
			})
		}

		tl.lastNotesIter = iteration
		tl.contextManager.RefreshNotes(tl.writeNotesTool.ReadNotes())

		assistantMsg := llm.Message{
			Role:      "assistant",
			Content:   content,
			ToolCalls: toolCalls,
		}

		tl.contextManager.AddIteration(assistantMsg, toolResults)
	}

	applogger.L.Info("Notes iteration completed", "iteration", iteration)

	if isFinal {
		return &LoopResult{Status: "failure", Reason: "Task did not complete within max iterations. Notes have been saved for next execution."}
	}

	return &LoopResult{Status: "success"}
}

// updateNotesOnSuccess updates notes after successful task completion.
// This ensures notes reflect the final state for future modifications.
// Uses the checkpoint client (lazy-initialized) with only write_notes tool available.
func (tl *TaskLoop) updateNotesOnSuccess(ctx context.Context, iteration int, finalContent string, messages []llm.Message) {
	if tl.writeNotesTool == nil {
		return
	}

	if tl.checkpointClient == nil {
		tl.checkpointClient = llm.NewChatModelWithTemperature(tl.llmConfig.BaseURL, tl.llmConfig.APIKey, tl.llmConfig.ModelID, llm.TemperatureCreative)
	}

	applogger.L.Info("Updating notes after successful completion", "iteration", iteration)

	successMsg := `[Task Completed - Update Your Notes]
The task has been completed successfully.

Please update your notes to reflect the final state.
Use write_notes to APPEND a summary entry:

{
  "entry_type": "progress",
  "content": "Task completed. Summary of what was done...",
  "references": ["file1.py", "file2.json"]
}

This will help you continue work if changes are requested later.`

	messagesWithUpdate := append(messages, llm.Message{
		Role:    "user",
		Content: successMsg,
	})

	toolDefs := []llm.FunctionDefinition{tl.writeNotesTool.Schema()}
	response, err := tl.checkpointClient.ChatWithTools(ctx, messagesWithUpdate, toolDefs)
	if err != nil {
		applogger.L.Error("Notes update on success failed", "error", err)
		return
	}

	if response.FinishReason == "tool_calls" {
		for _, tc := range response.ToolCalls {
			if tc.Function.Name != "write_notes" {
				continue
			}
			var args map[string]interface{}
			json.Unmarshal([]byte(tc.Function.Arguments), &args)
			tl.writeNotesTool.Execute(args)
		}

		tl.contextManager.RefreshNotes(tl.writeNotesTool.ReadNotes())
	}

	applogger.L.Info("Notes updated after successful completion")
}

// invokeLLM calls the LLM with the current messages and all registered tools.
// Converts internal message format and binds tool schemas.
func (tl *TaskLoop) invokeLLM(ctx context.Context, messages []llm.Message) (llm.ToolResponse, error) {
	msgSummary := make([]map[string]interface{}, 0, len(messages))
	for _, m := range messages {
		msgSummary = append(msgSummary, map[string]interface{}{
			"role":        m.Role,
			"content_len": len(m.Content),
			"tool_calls":  len(m.ToolCalls),
		})
	}
	applogger.L.Debug("TaskLoop invoking LLM",
		"message_count", len(messages),
		"detail", fmt.Sprintf("%v", msgSummary),
	)

	toolDefs := make([]llm.FunctionDefinition, 0, len(tl.toolRegistry))
	for _, t := range tl.toolRegistry {
		toolDefs = append(toolDefs, t.Schema())
	}
	return tl.llmClient.ChatWithTools(ctx, messages, toolDefs)
}

// executeToolCall executes a single tool call and returns the result.
// Looks up the tool in the registry, parses arguments, and calls Execute.
// Returns error messages for unknown tools or invalid arguments.
func (tl *TaskLoop) executeToolCall(tc llm.ToolCall) llm.Message {
	toolCallID := tc.ID
	toolName := tc.Function.Name
	argsStr := tc.Function.Arguments

	var args map[string]interface{}
	if err := json.Unmarshal([]byte(argsStr), &args); err != nil {
		return llm.Message{
			Role:       "tool",
			ToolCallID: toolCallID,
			Content:    fmt.Sprintf("Error: invalid arguments format - %s", err.Error()),
		}
	}

	tool, ok := tl.toolRegistry[toolName]
	if !ok {
		return llm.Message{
			Role:       "tool",
			ToolCallID: toolCallID,
			Content:    fmt.Sprintf("Error: unknown tool '%s'", toolName),
		}
	}

	applogger.L.Info("Executing tool", "tool", toolName)

	result, err := tool.Execute(args)
	if err != nil {
		applogger.L.Error("Tool execution error", "tool", toolName, "error", err)
		result = fmt.Sprintf("Error executing tool '%s': %s", toolName, err.Error())
	}

	return llm.Message{
		Role:       "tool",
		ToolCallID: toolCallID,
		Content:    result,
	}
}

// writeInteraction writes an interaction record to the database.
// Silently skips if session is not configured.
// Records are grouped by (session_id, work_id, iteration)
// to support both frontend display and debugging.
func (tl *TaskLoop) writeInteraction(iteration, interactionType int, data map[string]interface{}) {
	if tl.sessionID == 0 {
		return
	}

	dataJSON, _ := json.Marshal(data)
	record := model.Interaction{
		SessionID: tl.sessionID,
		WorkID:    tl.workID,
		Iteration: iteration,
		Type:      interactionType,
		Data:      string(dataJSON),
	}
	if err := database.DB.Create(&record).Error; err != nil {
		applogger.L.Error("Failed to write interaction record", "error", err)
	}
}

// getWorkspaceRoot returns the root directory for all session workspaces,
// resolved to an absolute path so that paths shown to the agent in prompts
// and used as bash CWD are unambiguous.
func getWorkspaceRoot() string {
	root := config.Get().GetWorkspaceRoot()
	if abs, err := filepath.Abs(root); err == nil {
		return abs
	}
	return root
}

// getSessionWorkspace returns the session-level workspace root directory.
// Path: {workspaceRoot}/{session_id}
func getSessionWorkspace(sessionID int64) string {
	return filepath.Join(getWorkspaceRoot(), strconv.FormatInt(sessionID, 10))
}

// getSessionMetaDir returns the .meta directory path for a session.
// Path: {workspaceRoot}/{session_id}/.meta
func getSessionMetaDir(sessionID int64) string {
	return filepath.Join(getSessionWorkspace(sessionID), ".meta")
}

// getSessionOutputDir returns the output directory path for a session.
// Path: {workspaceRoot}/{session_id}/output
func getSessionOutputDir(sessionID int64) string {
	return filepath.Join(getSessionWorkspace(sessionID), "output")
}

// initWorkspace creates the session-level workspace directory structure.
// Initializes notes.md in the .meta directory if it doesn't exist.
// The workspace is scoped to the session — no work-level subdirectories.
func initWorkspace(sessionID int64) string {
	workspace := getSessionWorkspace(sessionID)
	metaDir := filepath.Join(workspace, ".meta")
	os.MkdirAll(metaDir, 0755)

	notesFile := filepath.Join(metaDir, "notes.md")
	if _, err := os.Stat(notesFile); err != nil {
		os.WriteFile(notesFile, []byte("# Agent Notes\n\nStructured log of agent's work progress.\n\n"), 0644)
	}

	outputDir := getSessionOutputDir(sessionID)
	os.MkdirAll(outputDir, 0755)

	return workspace
}
