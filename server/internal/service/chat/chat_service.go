// Package chat implements the core chat processing pipeline.
//
// This package handles the complete chat processing flow, matching Python's
// background_tasks.process_chat_task. The pipeline includes:
//   - User state inference (including needs_world_interaction detection)
//   - Query preprocessing (routing, clarification, RAG optimization)
//   - Agent execution for world-interaction requests
//   - Context engineering (summary, retrieval, assembly)
//   - LLM streaming responses
//   - Summary generation triggers
//
// Flow:
//  1. User sends message -> API creates user_msg + ai_msg placeholders
//  2. ChatService.Process() infers user state (including needs_world_interaction)
//  3. If needs_world_interaction=true:
//     - Set ai_msg.has_interactions=1 (exists)
//     - Execute agent via TaskExecutor with session workspace
//     - Record interactions to database
//     - Pass task_result to context assembly for LLM response
//  4. If needs_world_interaction=false:
//     - Set ai_msg.has_interactions=2 (none)
//     - Continue with normal LLM chat flow (context engineering + streaming)
package chat

import (
	"context"
	"encoding/json"
	"fmt"

	"private-buddy-server/internal/config"
	"private-buddy-server/internal/model"
	"private-buddy-server/internal/service"
	chatcontext "private-buddy-server/internal/service/chat/chatctx"
	"private-buddy-server/internal/service/llm"
	"private-buddy-server/internal/service/task"

	applogger "private-buddy-server/internal/logger"

	"gorm.io/gorm"
)

// User-friendly error message for unexpected failures
const userFriendlyErrorMessage = "抱歉，服务器遇到了一些问题，请稍后再试。"

// ChatService handles the complete chat processing pipeline for a single session.
// It orchestrates user state inference, query preprocessing, agent execution,
// context engineering, LLM streaming, and summary generation.
type ChatService struct {
	db        *gorm.DB
	session   *model.Session
	agent     *model.Agent
	llmConfig *model.LLMConfig
	onChunk   func(chunk string)
	onNotify  func(data string)
}

// NewChatService creates a ChatService instance bound to a specific session.
func NewChatService(db *gorm.DB, session *model.Session, agent *model.Agent, llmConfig *model.LLMConfig) *ChatService {
	return &ChatService{
		db:        db,
		session:   session,
		agent:     agent,
		llmConfig: llmConfig,
	}
}

// SetOnChunk registers a callback invoked for each LLM streaming chunk (for SSE push).
func (cs *ChatService) SetOnChunk(fn func(chunk string)) {
	cs.onChunk = fn
}

// SetOnNotify registers a callback invoked for notification events (e.g., agent_processing).
func (cs *ChatService) SetOnNotify(fn func(data string)) {
	cs.onNotify = fn
}

// Process handles the complete chat processing pipeline, matching Python's process_chat_task.
//
// Context Engineering Variables:
//
//	V = current message count in session
//	N = summary window size (configurable via settings.summary_window_size)
//
//	- V < N: Skip context engineering, use all messages directly (no summary exists)
//	- V >= N: Apply full context engineering pipeline (summary + retrieval + assembly)
//
// When V >= N, user state inference and query preprocessing run in parallel since
// they have no data dependency. When V < N, only user state is invoked.
func (cs *ChatService) Process(triggerMessageID, aiMessageID int64) (string, error) {
	var triggerMessage model.Message
	if err := cs.db.First(&triggerMessage, triggerMessageID).Error; err != nil {
		return userFriendlyErrorMessage, fmt.Errorf("trigger message not found: %w", err)
	}

	var aiMessage model.Message
	if err := cs.db.First(&aiMessage, aiMessageID).Error; err != nil {
		return userFriendlyErrorMessage, fmt.Errorf("AI message not found: %w", err)
	}

	sessionID := cs.session.ID
	applogger.L.Info("Starting chat processing",
		"session_id", sessionID,
		"trigger_message_id", triggerMessageID,
		"ai_message_id", aiMessageID,
	)

	// Get message count in session (V = session message count, not global ID)
	var messageCount int64
	cs.db.Model(&model.Message{}).Where("session_id = ?", sessionID).Count(&messageCount)

	settings := config.Get()
	windowSize := settings.SummaryWindowSize

	// Prepare recent messages for user state inference
	retrievalSvc := chatcontext.NewRetrievalService(cs.db)
	completedStatus := model.MessageStatusCompleted
	recentMessagesForState := retrievalSvc.GetRecentMessages(
		sessionID, minInt(int(messageCount), windowSize), &completedStatus,
	)

	var userStateResult *chatcontext.UserState
	var preprocessingResult *chatcontext.PreprocessingResult

	// --- Parallel: User State + Query Preprocessing (when V >= N) ---
	// These two LLM calls have no data dependency and can run concurrently.
	// When V < N, preprocessing is not needed, so only user state is invoked.
	if messageCount >= int64(windowSize) {
		userStateSvc := chatcontext.NewUserStateService()
		preprocessingSvc := chatcontext.NewQueryPreprocessingService()

		preprocessingHistory := cs.getPreprocessingHistory(aiMessageID, windowSize)

		// Speculative parallel execution: both calls start simultaneously
		userStateCh := make(chan *chatcontext.UserState, 1)
		preprocessingCh := make(chan *chatcontext.PreprocessingResult, 1)

		go func() {
			userStateCh <- userStateSvc.InferUserState(cs.llmConfig, recentMessagesForState)
		}()

		go func() {
			characterSettings := cs.agent.CharacterSettings
			preprocessingCh <- preprocessingSvc.PreprocessQuery(
				cs.llmConfig,
				triggerMessage.Content,
				preprocessingHistory,
				&characterSettings,
				intPtr(windowSize),
			)
		}()

		userStateResult = <-userStateCh
		preprocessingResult = <-preprocessingCh

		applogger.L.Info("Parallel execution completed",
			"session_id", sessionID,
		)
	} else {
		// V < N: only user state is needed
		userStateSvc := chatcontext.NewUserStateService()
		userStateResult = userStateSvc.InferUserState(cs.llmConfig, recentMessagesForState)
		preprocessingResult = nil
	}

	needsWorldInteraction := false
	if userStateResult != nil {
		needsWorldInteraction = userStateResult.NeedsWorldInteraction
	}
	applogger.L.Info("User state inference",
		"needs_world_interaction", needsWorldInteraction,
		"session_id", sessionID,
		"trigger_message_id", triggerMessageID,
	)

	// --- Agent execution (if needed) ---
	var taskResult *chatcontext.TaskResultForAssembly
	if needsWorldInteraction {
		// Preprocessing result is discarded when agent path is taken
		cs.db.Model(&model.Message{}).Where("id = ?", aiMessageID).
			Update("has_interactions", model.HasInteractionsExists)
		taskResult = cs.executeAgent(triggerMessage, aiMessageID, int(messageCount), windowSize)
	} else {
		cs.db.Model(&model.Message{}).Where("id = ?", aiMessageID).
			Update("has_interactions", model.HasInteractionsNone)
	}

	chatModel := llm.NewChatModelWithTemperature(cs.llmConfig.BaseURL, cs.llmConfig.APIKey, cs.llmConfig.ModelID, llm.TemperatureCreative)

	var messages []llm.ChatMessage
	var hasEmbedding bool

	// Branch 1: V < N - Skip context engineering, use all messages directly
	if messageCount < int64(windowSize) {
		applogger.L.Info("V < N, skipping context engineering",
			"V", messageCount, "N", windowSize,
		)

		recentMessages := retrievalSvc.GetRecentMessages(
			sessionID, int(messageCount), &completedStatus,
		)

		// Validate trigger_message is the latest completed message
		if len(recentMessages) == 0 || getMessageID(recentMessages[len(recentMessages)-1]) != triggerMessageID {
			applogger.L.Error("Trigger message is not the latest completed message",
				"session_id", sessionID,
				"trigger_message_id", triggerMessageID,
			)
			return userFriendlyErrorMessage, nil
		}

		// Assemble simple context without background story
		assemblySvc := chatcontext.NewContextAssemblyService()
		characterSettings := cs.agent.CharacterSettings
		messages = assemblySvc.AssembleContext(
			&characterSettings,
			nil,
			recentMessages,
			nil,
			nil,
			1,
			len(recentMessages),
			nil,
			taskResult,
		)
		hasEmbedding = false
	} else {
		// Branch 2: V >= N - Apply full context engineering pipeline

		// Handle clarification needed case (from parallel preprocessing)
		if preprocessingResult != nil && preprocessingResult.NeedsClarification {
			cs.db.Model(&model.Message{}).Where("id = ?", aiMessageID).Updates(map[string]interface{}{
				"content": preprocessingResult.Clarification,
				"status":  model.MessageStatusCompleted,
			})
			cs.db.Model(&model.Session{}).Where("id = ?", sessionID).Update("status", model.SessionStatusIdle)
			applogger.L.Info("Query needed clarification", "session_id", sessionID)
			return preprocessingResult.Clarification, nil
		}

		processedQuery := triggerMessage.Content
		if preprocessingResult != nil {
			processedQuery = preprocessingResult.ProcessedQuery
			applogger.L.Info("Query type and processed",
				"type", preprocessingResult.QueryType,
				"processed", processedQuery[:minLen(50, len(processedQuery))],
			)
		}

		// Context retrieval (with or without RAG)
		var contextResult *chatcontext.RetrievalResult
		if preprocessingResult != nil && preprocessingResult.SkipRetrieval {
			contextResult = retrievalSvc.GetContextWithoutRAG(sessionID, windowSize)
			hasEmbedding = false
		} else {
			contextResult = retrievalSvc.GetContextForChat(sessionID, processedQuery, windowSize, 5)
			hasEmbedding = contextResult.HasEmbedding
		}

		// Validate trigger_message is the latest completed message
		if len(contextResult.RecentMessages) == 0 || getMessageID(contextResult.RecentMessages[len(contextResult.RecentMessages)-1]) != triggerMessageID {
			applogger.L.Error("Trigger message is not the latest completed message",
				"session_id", sessionID,
				"trigger_message_id", triggerMessageID,
			)
			return userFriendlyErrorMessage, nil
		}

		// Use cached narrative (generated in background with summary)
		var backgroundStory *string
		if contextResult.Narrative != nil {
			backgroundStory = contextResult.Narrative
		}

		// Convert user state to natural language description for prompt injection
		var userStateDescription *string
		if userStateResult != nil {
			desc := userStateResult.ToNaturalLanguage()
			userStateDescription = &desc
		}

		// Calculate message sequence numbers for metadata
		var summaryVersion *int
		if contextResult.Summary != nil {
			if v, ok := contextResult.Summary["version"].(int); ok {
				summaryVersion = &v
			}
		}

		recentStart := int(messageCount) - len(contextResult.RecentMessages) + 1

		// Assemble context with metadata, user state, and segments
		assemblySvc := chatcontext.NewContextAssemblyService()
		characterSettings := cs.agent.CharacterSettings
		messages = assemblySvc.AssembleContext(
			&characterSettings,
			backgroundStory,
			contextResult.RecentMessages,
			contextResult.RelevantSegments,
			summaryVersion,
			recentStart,
			int(messageCount),
			userStateDescription,
			taskResult,
		)
	}

	// Stream LLM response
	stream, err := chatModel.ChatStream(context.Background(), messages)
	if err != nil {
		return "", fmt.Errorf("failed to start stream: %w", err)
	}
	applogger.L.Info("Starting LLM stream", "session_id", cs.session.ID)

	var fullContent string
	var accumulatedContent string
	fullContent, err = chatModel.ConsumeStream(stream, func(chunk string) error {
		accumulatedContent += chunk
		// Update AI message content in database progressively
		cs.db.Model(&model.Message{}).Where("id = ?", aiMessageID).
			Update("content", accumulatedContent)
		// Notify client via SSE
		if cs.onChunk != nil {
			cs.onChunk(chunk)
		}
		return nil
	})
	if err != nil {
		return fullContent, err
	}

	// Mark AI message as completed
	cs.db.Model(&model.Message{}).Where("id = ?", aiMessageID).Updates(map[string]interface{}{
		"content": fullContent,
		"status":  model.MessageStatusCompleted,
	})

	applogger.L.Info("Chat processing completed",
		"session_id", sessionID,
		"response_length", len(fullContent),
	)

	// Index messages for RAG if embedding is available
	if hasEmbedding {
		go func() {
			retrievalSvc.IndexMessages(sessionID, []int64{triggerMessageID, aiMessageID})
		}()
	}

	// Trigger summary generation if V >= N (after AI response is complete)
	var updatedCount int64
	cs.db.Model(&model.Message{}).Where("session_id = ?", sessionID).Count(&updatedCount)
	if updatedCount >= int64(windowSize) {
		go cs.generateSummary(sessionID, int(updatedCount), windowSize)
	}

	return fullContent, nil
}

// executeAgent handles the agent execution path when needs_world_interaction=true.
//
// This function handles the agent execution path:
//  1. Notify frontend that agent is processing
//  2. Rewrite user message into clear task requirement
//  3. Execute agent via TaskExecutor
//  4. Return TaskResult for context assembly
func (cs *ChatService) executeAgent(triggerMessage model.Message, aiMessageID int64, messageCount int, windowSize int) *chatcontext.TaskResultForAssembly {
	sessionID := cs.session.ID
	applogger.L.Info("Agent execution path",
		"session_id", sessionID,
		"ai_msg_id", aiMessageID,
	)

	// Notify frontend that agent is processing
	if cs.onNotify != nil {
		notifyData, _ := json.Marshal(map[string]string{
			"type":    "agent_processing",
			"message": "Agent is processing your request...",
		})
		cs.onNotify(string(notifyData))
	}

	// Rewrite user message into clear task requirement
	retrievalSvc := chatcontext.NewRetrievalService(cs.db)
	completedStatus := model.MessageStatusCompleted
	recentMessages := retrievalSvc.GetRecentMessages(sessionID, minInt(messageCount, windowSize), &completedStatus)

	history := make([]map[string]string, 0, len(recentMessages))
	for _, msg := range recentMessages {
		role, _ := msg["role"].(string)
		content, _ := msg["content"].(string)
		history = append(history, map[string]string{
			"role":    role,
			"content": content,
		})
	}

	rewriter := task.NewTaskRequirementRewriter()
	rewrittenRequirement := rewriter.Rewrite(cs.llmConfig, triggerMessage.Content, history, 10)
	applogger.L.Info("Task requirement rewritten",
		"session_id", sessionID,
		"original", triggerMessage.Content[:minLen(50, len(triggerMessage.Content))],
		"rewritten", rewrittenRequirement[:minLen(50, len(rewrittenRequirement))],
	)

	// Execute agent via TaskExecutor
	var searchConfig model.SearchConfig
	cs.db.Where("is_active = ?", true).First(&searchConfig)

	taskExecutor := task.NewTaskExecutor(cs.db)
	taskResult := taskExecutor.Execute(task.TaskParams{
		TaskRequirement: rewrittenRequirement,
		LLMConfig:       cs.llmConfig,
		MaxIterations:   0,
		SessionID:       sessionID,
		UserMsgID:       triggerMessage.ID,
		AgentMsgID:      aiMessageID,
		SearchConfig:    &searchConfig,
	})

	result := &chatcontext.TaskResultForAssembly{
		Status: taskResult.Status,
	}
	if taskResult.Output != "" {
		result.Result = &taskResult.Output
	}
	if taskResult.Error != "" {
		result.Reason = &taskResult.Error
	}
	if taskResult.Notes != "" {
		result.Notes = &taskResult.Notes
	}

	applogger.L.Info("Agent execution completed",
		"session_id", sessionID,
		"status", taskResult.Status,
	)

	return result
}

// generateSummary generates a summary for the session.
// This is triggered after AI response is complete (second trigger point),
// ensuring the current round's messages are included in the summary version.
func (cs *ChatService) generateSummary(sessionID int64, version int, windowSize int) {
	chatcontext.GenerateSummaryForSession(cs.db, cs.dataService(), sessionID, version, windowSize)
}

// dataService creates a DataService instance for database queries.
func (cs *ChatService) dataService() *service.DataService {
	return service.NewDataService()
}

// getPreprocessingHistory retrieves messages before the AI message for preprocessing context.
// Returns messages in chronological order (oldest first), limited to the specified count.
func (cs *ChatService) getPreprocessingHistory(beforeMessageID int64, limit int) []map[string]string {
	var messages []model.Message
	cs.db.Where("session_id = ? AND id < ?", cs.session.ID, beforeMessageID).
		Order("id DESC").Limit(limit).Find(&messages)

	// Reverse to chronological order
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}

	history := make([]map[string]string, 0, len(messages))
	for _, msg := range messages {
		history = append(history, map[string]string{
			"role":    msg.Role,
			"content": msg.Content,
		})
	}
	return history
}

// getMessageID extracts the message ID from a message map.
func getMessageID(msg map[string]interface{}) int64 {
	if id, ok := msg["id"].(int64); ok {
		return id
	}
	return 0
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func minLen(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func intPtr(v int) *int {
	return &v
}
