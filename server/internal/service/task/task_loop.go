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
	agentMsgID       int64                       // Agent message ID for the delivery target
	writeNotesTool   *tools.WriteNotesTool       // Write notes tool for checkpoint iterations
	checkpointClient *llm.ChatModel              // Lazy-initialized LLM client for checkpoint iterations
	lastNotesIter    int                         // Last iteration where write_notes was called (voluntary or forced)
	ctx              context.Context             // Cancellation context from the caller
}

// NewTaskLoop creates a new TaskLoop instance.
// The tool list is converted to a name-keyed registry for efficient lookup during execution.
func NewTaskLoop(
	llmClient *llm.ChatModel,
	llmConfig *model.LLMConfig,
	toolList []tools.Tool,
	contextManager *taskcontext.ContextManager,
	maxIterations int,
	sessionID, userMsgID, agentMsgID int64,
	writeNotesTool *tools.WriteNotesTool,
	ctx context.Context,
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
		agentMsgID:     agentMsgID,
		writeNotesTool: writeNotesTool,
		ctx:            ctx,
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
// (as part of the fixed task.md content), so it is not passed
// as a parameter here.
func (tl *TaskLoop) Run() *LoopResult {
	applogger.L.Info("TaskLoop starting",
		"max_iterations", tl.maxIterations,
		"session_id", tl.sessionID,
		"agent_msg_id", tl.agentMsgID,
	)

	for iteration := 1; iteration <= tl.maxIterations; iteration++ {
		// Check if the task has been cancelled (e.g., session deleted)
		if tl.ctx != nil && tl.ctx.Err() != nil {
			applogger.L.Info("TaskLoop cancelled, stopping execution",
				"session_id", tl.sessionID,
				"iteration", iteration,
			)
			return &LoopResult{Status: "failure", Reason: "task cancelled"}
		}

		applogger.L.Info("TaskLoop iteration", "iteration", iteration, "max", tl.maxIterations)

		if tl.writeNotesTool != nil {
			tl.writeNotesTool.TrimNotes()
			tl.contextManager.RefreshNotes(tl.writeNotesTool.ReadNotes())
		}

		messages := tl.contextManager.BuildMessages()

		isCheckpoint := tl.isCheckpointIteration(iteration)
		isFinal := iteration == tl.maxIterations

		if isCheckpoint || isFinal {
			result := tl.runNotesIteration(iteration, messages, isFinal)
			if result.Status == "failure" {
				return result
			}
			continue
		}

		tl.writeInteraction(iteration, model.InteractionTypeRequest, map[string]interface{}{
			"messages": messages,
		})

		response, err := tl.invokeLLM(messages)
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
			tl.updateNotesOnSuccess(iteration, content, messages)
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
func (tl *TaskLoop) runNotesIteration(iteration int, messages []llm.Message, isFinal bool) *LoopResult {
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
	response, err := tl.checkpointClient.ChatWithTools(context.Background(), messagesWithCheckpoint, toolDefs)
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
func (tl *TaskLoop) updateNotesOnSuccess(iteration int, finalContent string, messages []llm.Message) {
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

This will help you continue work if the user requests changes later.`

	messagesWithUpdate := append(messages, llm.Message{
		Role:    "user",
		Content: successMsg,
	})

	toolDefs := []llm.FunctionDefinition{tl.writeNotesTool.Schema()}
	response, err := tl.checkpointClient.ChatWithTools(context.Background(), messagesWithUpdate, toolDefs)
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
func (tl *TaskLoop) invokeLLM(messages []llm.Message) (llm.ToolResponse, error) {
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
	return tl.llmClient.ChatWithTools(context.Background(), messages, toolDefs)
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
// Records are grouped by (session_id, user_msg_id, agent_msg_id, iteration)
// to support both frontend display and debugging.
func (tl *TaskLoop) writeInteraction(iteration, interactionType int, data map[string]interface{}) {
	if tl.sessionID == 0 {
		return
	}

	dataJSON, _ := json.Marshal(data)
	record := model.Interaction{
		SessionID:  tl.sessionID,
		UserMsgID:  tl.userMsgID,
		AgentMsgID: tl.agentMsgID,
		Iteration:  iteration,
		Type:       interactionType,
		Data:       string(dataJSON),
	}
	if err := database.DB.Create(&record).Error; err != nil {
		applogger.L.Error("Failed to write interaction record", "error", err)
	}
}

// getWorkspaceRoot returns the root directory for all session workspaces.
func getWorkspaceRoot() string {
	return config.Get().GetWorkspaceRoot()
}

// getSessionWorkspace returns the workspace directory path for a session.
func getSessionWorkspace(sessionID int64) string {
	return filepath.Join(getWorkspaceRoot(), strconv.FormatInt(sessionID, 10))
}

// getMetaDir returns the meta directory path for a session.
func getMetaDir(sessionID int64) string {
	return filepath.Join(getSessionWorkspace(sessionID), ".meta")
}

// getOutputDir returns the output directory path for a session.
func getOutputDir(sessionID int64) string {
	return filepath.Join(getSessionWorkspace(sessionID), "output")
}

// initSessionWorkspace creates the workspace directory structure for a session.
// Initializes task.md and notes.md in the .meta directory if they don't exist.
func initSessionWorkspace(sessionID int64, rewrittenRequirement string) string {
	workspace := getSessionWorkspace(sessionID)
	metaDir := filepath.Join(workspace, ".meta")
	os.MkdirAll(metaDir, 0755)

	// Always update task.md with the latest requirement for the current round.
	// Previous rounds may have written a different task; the new message must take precedence.
	taskFile := filepath.Join(metaDir, "task.md")
	os.WriteFile(taskFile, []byte(fmt.Sprintf("# Task\n\n%s", rewrittenRequirement)), 0644)

	notesFile := filepath.Join(metaDir, "notes.md")
	if _, err := os.Stat(notesFile); err != nil {
		os.WriteFile(notesFile, []byte("# Agent Notes\n\nStructured log of agent's work progress.\n\n"), 0644)
	}

	outputDir := getOutputDir(sessionID)
	os.MkdirAll(outputDir, 0755)

	return workspace
}

// readTaskMD reads the task.md content from a session's workspace.
func readTaskMD(sessionID int64) string {
	taskFile := filepath.Join(getMetaDir(sessionID), "task.md")
	data, err := os.ReadFile(taskFile)
	if err != nil {
		return ""
	}
	return string(data)
}

// removeSessionWorkspace removes the entire workspace directory for a session.
func removeSessionWorkspace(sessionID int64) {
	workspace := getSessionWorkspace(sessionID)
	os.RemoveAll(workspace)
	applogger.L.Info("Workspace removed", "session_id", sessionID, "path", workspace)
}
