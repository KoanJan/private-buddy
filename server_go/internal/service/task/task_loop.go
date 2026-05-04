package task

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"private-buddy-server/internal/config"
	"private-buddy-server/internal/model"
	"private-buddy-server/internal/service/llm"
	taskcontext "private-buddy-server/internal/service/task/context"
	"private-buddy-server/internal/service/task/tools"

	openai "github.com/sashabaranov/go-openai"

	applogger "private-buddy-server/internal/logger"

	"gorm.io/gorm"
)

const defaultMaxIterations = 90

type TaskLoop struct {
	llmClient        *llm.ChatModel
	llmConfig        *model.LLMConfig
	toolRegistry     map[string]tools.Tool
	contextManager   *taskcontext.ContextManager
	maxIterations    int
	db               *gorm.DB
	sessionID        int64
	userMsgID        int64
	agentMsgID       int64
	writeNotesTool   *tools.WriteNotesTool
	checkpointClient *llm.ChatModel
	lastNotesIter    int
}

func NewTaskLoop(
	llmClient *llm.ChatModel,
	llmConfig *model.LLMConfig,
	toolList []tools.Tool,
	contextManager *taskcontext.ContextManager,
	maxIterations int,
	db *gorm.DB,
	sessionID, userMsgID, agentMsgID int64,
	writeNotesTool *tools.WriteNotesTool,
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
		db:             db,
		sessionID:      sessionID,
		userMsgID:      userMsgID,
		agentMsgID:     agentMsgID,
		writeNotesTool: writeNotesTool,
	}
}

type LoopResult struct {
	Status string  `json:"status"`
	Result *string `json:"result,omitempty"`
	Reason *string `json:"reason,omitempty"`
}

func (tl *TaskLoop) Run() *LoopResult {
	applogger.L.Info("TaskLoop starting",
		"max_iterations", tl.maxIterations,
		"session_id", tl.sessionID,
		"agent_msg_id", tl.agentMsgID,
	)

	for iteration := 1; iteration <= tl.maxIterations; iteration++ {
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
			reason := fmt.Sprintf("LLM invocation failed at iteration %d: %s", iteration, err.Error())
			return &LoopResult{Status: "failure", Reason: &reason}
		}

		finishReason := string(response.Choices[0].FinishReason)
		content := response.Choices[0].Message.Content
		toolCalls := response.Choices[0].Message.ToolCalls

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
			return &LoopResult{Status: "success", Result: &content}

		case "tool_calls":
			if content != "" {
				applogger.L.Info("TaskLoop thoughts", "iteration", iteration, "thoughts", content[:minInt(500, len(content))])
			}

			assistantMsg := map[string]interface{}{
				"role":       "assistant",
				"tool_calls": toolCalls,
			}
			if content != "" {
				assistantMsg["content"] = content
			}

			var toolResults []map[string]interface{}
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

			assistantMsg := map[string]interface{}{"role": "assistant"}
			if content != "" {
				assistantMsg["content"] = content
			}
			if len(toolCalls) > 0 {
				assistantMsg["tool_calls"] = toolCalls
			}

			tl.contextManager.AddIteration(assistantMsg, []map[string]interface{}{})

			tl.contextManager.AddIteration(
				map[string]interface{}{
					"role":    "user",
					"content": "[System] Your previous response was truncated due to length limits. Your tool calls were NOT executed. Please continue with a more concise response.",
				},
				[]map[string]interface{}{},
			)

		default:
			applogger.L.Warn("TaskLoop unexpected finish_reason", "finish_reason", finishReason, "iteration", iteration)
		}
	}

	reason := fmt.Sprintf("Task did not complete within %d iterations", tl.maxIterations)
	return &LoopResult{Status: "failure", Reason: &reason}
}

func (tl *TaskLoop) isCheckpointIteration(iteration int) bool {
	if iteration == tl.maxIterations {
		return false
	}
	window := tl.contextManager.IterationWindow()
	distance := iteration - tl.lastNotesIter
	return distance >= window
}

func (tl *TaskLoop) runNotesIteration(iteration int, messages []map[string]interface{}, isFinal bool) *LoopResult {
	if tl.writeNotesTool == nil {
		applogger.L.Error("Cannot run notes iteration: write_notes_tool not initialized")
		if isFinal {
			reason := "Task did not complete within max iterations"
			return &LoopResult{Status: "failure", Reason: &reason}
		}
		return &LoopResult{Status: "success"}
	}

	if tl.checkpointClient == nil {
		tl.checkpointClient = llm.NewChatModel(tl.llmConfig.BaseURL, tl.llmConfig.APIKey, tl.llmConfig.ModelID, 4096)
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

	messagesWithCheckpoint := append(messages, map[string]interface{}{
		"role":    "user",
		"content": checkpointMsg,
	})

	tl.writeInteraction(iteration, model.InteractionTypeRequest, map[string]interface{}{
		"messages":      messagesWithCheckpoint,
		"is_checkpoint": true,
	})

	schemas := []openai.FunctionDefinition{tl.writeNotesTool.Schema()}
	toolDefs := []openai.Tool{{
		Type:     openai.ToolTypeFunction,
		Function: &schemas[0],
	}}
	response, err := tl.checkpointClient.ChatWithTools(context.Background(), toOpenAIMessages(messagesWithCheckpoint), toolDefs)
	if err != nil {
		applogger.L.Error("Notes iteration LLM error", "error", err)
		if isFinal {
			reason := "Task did not complete within max iterations"
			return &LoopResult{Status: "failure", Reason: &reason}
		}
		reason := fmt.Sprintf("Notes iteration LLM invocation failed: %s", err.Error())
		return &LoopResult{Status: "failure", Reason: &reason}
	}

	finishReason := string(response.Choices[0].FinishReason)
	content := response.Choices[0].Message.Content
	toolCalls := response.Choices[0].Message.ToolCalls

	tl.writeInteraction(iteration, model.InteractionTypeResponse, map[string]interface{}{
		"content":       content,
		"tool_calls":    toolCalls,
		"finish_reason": finishReason,
		"is_checkpoint": true,
	})

	if finishReason == "tool_calls" {
		var toolResults []map[string]interface{}
		for _, tc := range toolCalls {
			toolCallID := tc.ID

			if tc.Function.Name != "write_notes" {
				applogger.L.Warn("Notes iteration: unexpected tool call", "tool", tc.Function.Name)
				toolResults = append(toolResults, map[string]interface{}{
					"role":         "tool",
					"tool_call_id": toolCallID,
					"content":      fmt.Sprintf("Error: tool '%s' is not available during notes iteration", tc.Function.Name),
				})
				continue
			}

			var args map[string]interface{}
			json.Unmarshal([]byte(tc.Function.Arguments), &args)

			applogger.L.Info("Notes iteration: executing write_notes")
			result, _ := tl.writeNotesTool.Execute(args)

			toolResults = append(toolResults, map[string]interface{}{
				"role":         "tool",
				"tool_call_id": toolCallID,
				"content":      result,
			})
		}

		tl.lastNotesIter = iteration
		tl.contextManager.RefreshNotes(tl.writeNotesTool.ReadNotes())

		assistantMsg := map[string]interface{}{
			"role":       "assistant",
			"tool_calls": toolCalls,
		}
		if content != "" {
			assistantMsg["content"] = content
		}

		tl.contextManager.AddIteration(assistantMsg, toolResults)
	}

	applogger.L.Info("Notes iteration completed", "iteration", iteration)

	if isFinal {
		reason := "Task did not complete within max iterations. Notes have been saved for next execution."
		return &LoopResult{Status: "failure", Reason: &reason}
	}

	return &LoopResult{Status: "success"}
}

func (tl *TaskLoop) updateNotesOnSuccess(iteration int, finalContent string, messages []map[string]interface{}) {
	if tl.writeNotesTool == nil {
		return
	}

	if tl.checkpointClient == nil {
		tl.checkpointClient = llm.NewChatModel(tl.llmConfig.BaseURL, tl.llmConfig.APIKey, tl.llmConfig.ModelID, 4096)
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

	messagesWithUpdate := append(messages, map[string]interface{}{
		"role":    "user",
		"content": successMsg,
	})

	schemas := []openai.FunctionDefinition{tl.writeNotesTool.Schema()}
	toolDefs := []openai.Tool{{
		Type:     openai.ToolTypeFunction,
		Function: &schemas[0],
	}}
	response, err := tl.checkpointClient.ChatWithTools(context.Background(), toOpenAIMessages(messagesWithUpdate), toolDefs)
	if err != nil {
		applogger.L.Error("Notes update on success failed", "error", err)
		return
	}

	finishReason := string(response.Choices[0].FinishReason)
	toolCalls := response.Choices[0].Message.ToolCalls

	if finishReason == "tool_calls" {
		for _, tc := range toolCalls {
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

func (tl *TaskLoop) invokeLLM(messages []map[string]interface{}) (*openai.ChatCompletionResponse, error) {
	msgSummary := make([]map[string]interface{}, 0, len(messages))
	for _, m := range messages {
		role, _ := m["role"].(string)
		content, _ := m["content"].(string)
		toolCallsRaw, _ := m["tool_calls"]
		tcCount := 0
		if tcSlice, ok := toolCallsRaw.([]openai.ToolCall); ok {
			tcCount = len(tcSlice)
		} else if tcSlice, ok := toolCallsRaw.([]interface{}); ok {
			tcCount = len(tcSlice)
		}
		msgSummary = append(msgSummary, map[string]interface{}{
			"role":        role,
			"content_len": len(content),
			"tool_calls":  tcCount,
		})
	}
	applogger.L.Debug("TaskLoop invoking LLM",
		"message_count", len(messages),
		"detail", fmt.Sprintf("%v", msgSummary),
	)

	chatMessages := toOpenAIMessages(messages)
	toolDefs := make([]openai.Tool, 0, len(tl.toolRegistry))
	for _, t := range tl.toolRegistry {
		schema := t.Schema()
		toolDefs = append(toolDefs, openai.Tool{
			Type:     openai.ToolTypeFunction,
			Function: &schema,
		})
	}
	return tl.llmClient.ChatWithTools(context.Background(), chatMessages, toolDefs)
}

func (tl *TaskLoop) executeToolCall(tc openai.ToolCall) map[string]interface{} {
	toolCallID := tc.ID
	toolName := tc.Function.Name
	argsStr := tc.Function.Arguments

	var args map[string]interface{}
	if err := json.Unmarshal([]byte(argsStr), &args); err != nil {
		return map[string]interface{}{
			"role":         "tool",
			"tool_call_id": toolCallID,
			"content":      fmt.Sprintf("Error: invalid arguments format - %s", err.Error()),
		}
	}

	tool, ok := tl.toolRegistry[toolName]
	if !ok {
		return map[string]interface{}{
			"role":         "tool",
			"tool_call_id": toolCallID,
			"content":      fmt.Sprintf("Error: unknown tool '%s'", toolName),
		}
	}

	applogger.L.Info("Executing tool", "tool", toolName)

	result, err := tool.Execute(args)
	if err != nil {
		applogger.L.Error("Tool execution error", "tool", toolName, "error", err)
		result = fmt.Sprintf("Error executing tool '%s': %s", toolName, err.Error())
	}

	return map[string]interface{}{
		"role":         "tool",
		"tool_call_id": toolCallID,
		"content":      result,
	}
}

func (tl *TaskLoop) writeInteraction(iteration, interactionType int, data map[string]interface{}) {
	if tl.db == nil || tl.sessionID == 0 {
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
	if err := tl.db.Create(&record).Error; err != nil {
		applogger.L.Error("Failed to write interaction record", "error", err)
	}
}

func toOpenAIMessages(messages []map[string]interface{}) []openai.ChatCompletionMessage {
	result := make([]openai.ChatCompletionMessage, 0, len(messages))
	for _, m := range messages {
		role, _ := m["role"].(string)
		content, _ := m["content"].(string)

		msg := openai.ChatCompletionMessage{
			Role:    role,
			Content: content,
		}

		if toolCallsRaw, ok := m["tool_calls"]; ok {
			switch tc := toolCallsRaw.(type) {
			case []openai.ToolCall:
				msg.ToolCalls = tc
			}
		}

		if toolCallID, ok := m["tool_call_id"].(string); ok && toolCallID != "" {
			msg.ToolCallID = toolCallID
		}

		result = append(result, msg)
	}
	return result
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func getWorkspaceRoot() string {
	return config.Get().GetWorkspaceRoot()
}

func getSessionWorkspace(sessionID int64) string {
	return filepath.Join(getWorkspaceRoot(), strconv.FormatInt(sessionID, 10))
}

func getMetaDir(sessionID int64) string {
	return filepath.Join(getSessionWorkspace(sessionID), ".meta")
}

func getOutputDir(sessionID int64) string {
	return filepath.Join(getSessionWorkspace(sessionID), "output")
}

func ensureSessionWorkspace(sessionID int64) string {
	root := getWorkspaceRoot()
	os.MkdirAll(root, 0755)

	workspace := getSessionWorkspace(sessionID)
	os.MkdirAll(workspace, 0755)

	applogger.L.Info("Workspace ensured for session", "session_id", sessionID, "path", workspace)
	return workspace
}

func initSessionWorkspace(sessionID int64, rewrittenRequirement string) string {
	workspace := ensureSessionWorkspace(sessionID)

	metaDir := getMetaDir(sessionID)
	os.MkdirAll(metaDir, 0755)

	taskFile := filepath.Join(metaDir, "task.md")
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	if _, err := os.Stat(taskFile); err == nil {
		f, _ := os.OpenFile(taskFile, os.O_APPEND|os.O_WRONLY, 0644)
		if f != nil {
			f.WriteString(fmt.Sprintf("\n\n---\n\n## [%s] Task Update\n\n%s", timestamp, rewrittenRequirement))
			f.Close()
		}
	} else {
		os.WriteFile(taskFile, []byte(fmt.Sprintf("# Task\n\n%s", rewrittenRequirement)), 0644)
	}

	notesFile := filepath.Join(metaDir, "notes.md")
	if _, err := os.Stat(notesFile); err != nil {
		os.WriteFile(notesFile, []byte("# Agent Notes\n\nStructured log of agent's work progress.\n\n"), 0644)
	}

	outputDir := getOutputDir(sessionID)
	os.MkdirAll(outputDir, 0755)

	return workspace
}

func readTaskMD(sessionID int64) string {
	taskFile := filepath.Join(getMetaDir(sessionID), "task.md")
	data, err := os.ReadFile(taskFile)
	if err != nil {
		return ""
	}
	return string(data)
}

func removeSessionWorkspace(sessionID int64) {
	workspace := getSessionWorkspace(sessionID)
	os.RemoveAll(workspace)
	applogger.L.Info("Workspace removed", "session_id", sessionID, "path", workspace)
}
