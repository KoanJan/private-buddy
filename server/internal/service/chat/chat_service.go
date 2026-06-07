// Package chat implements the core chat processing pipeline.
//
// This package is designed as a package-level service: use the Process()
// function directly. No struct instances need to be created or passed around.
//
// The pipeline includes:
//   - User state inference (including needs_world_interaction detection)
//   - Query preprocessing (routing, clarification, RAG optimization)
//   - Agent execution for world-interaction requests
//   - Context engineering (summary, retrieval, assembly)
//   - LLM streaming responses
//   - Summary generation triggers
//
// Flow:
//  1. User sends message -> API creates user_msg + ai_msg placeholders
//  2. Process() infers user state (including needs_world_interaction)
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
	"private-buddy-server/internal/database"
	"private-buddy-server/internal/model"
	chatcontext "private-buddy-server/internal/service/chat/chatctx"
	"private-buddy-server/internal/service/kb"
	"private-buddy-server/internal/service/llm"
	"private-buddy-server/internal/service/task"

	applogger "private-buddy-server/internal/logger"
)

// User-friendly error message for unexpected failures
const userFriendlyErrorMessage = "抱歉，服务器遇到了一些问题，请稍后再试。"

// ChatCallbacks holds optional callbacks for notification events.
type ChatCallbacks struct {
	OnNotify func(data string)
}

// pipeline holds the state for a single chat processing execution.
// It is short-lived (only exists during one Process call) and carries
// shared data between pipeline stages.
type pipeline struct {
	session          *model.Session
	agent            *model.Agent
	llmConfig        *model.LLMConfig
	triggerMessageID int64
	aiMessageID      int64
	callbacks        *ChatCallbacks
	ctx              context.Context

	// Loaded in loadMessages
	triggerMessage model.Message
	aiMessage      model.Message
	sessionID      int64
	messageCount   int64
	windowSize     int
	kbIDs          []int64

	// Channel for async query preprocessing (signal only, data stored in preprocessingResult)
	preprocessingCh chan struct{}

	// Results from pipeline stages
	userStateResult       *chatcontext.UserState
	preprocessingResult   *chatcontext.PreprocessingResult
	needsWorldInteraction bool
	kbSegments            []chatcontext.Segment
	taskResult            *chatcontext.TaskResultForAssembly
	hasEmbedding          bool
}

// Process handles the complete chat processing pipeline.
//
// Context Engineering Variables:
//
//	V = current message count in session
//	N = summary window size (configurable via settings.summary_window_size)
//
//	- V < N: Skip context engineering, use all messages directly (no summary exists)
//	- V >= N: Apply full context engineering pipeline (summary + retrieval + assembly)
//
// Query preprocessing and user state inference run in parallel when either
// V >= N or knowledge bases are configured, since both are independent LLM calls.
func Process(
	ctx context.Context,
	session *model.Session,
	agent *model.Agent,
	llmConfig *model.LLMConfig,
	triggerMessageID, aiMessageID int64,
	callbacks *ChatCallbacks,
) (string, error) {
	p := &pipeline{
		session:          session,
		agent:            agent,
		llmConfig:        llmConfig,
		triggerMessageID: triggerMessageID,
		aiMessageID:      aiMessageID,
		callbacks:        callbacks,
		ctx:              ctx,
	}

	if err := p.loadMessages(); err != nil {
		return userFriendlyErrorMessage, err
	}

	// Start async preprocessing first so it runs in parallel with user state inference
	p.preprocessQuery()
	p.inferUserState()

	p.retrieveKnowledgeBases()
	p.executeAgentIfNeeded()

	messages, earlyContent, earlyReturn := p.assembleContext()
	if earlyReturn {
		return earlyContent, nil
	}

	fullContent, err := p.streamResponse(messages)
	if err != nil {
		return fullContent, err
	}

	p.postProcess()

	return fullContent, nil
}

// loadMessages loads trigger and AI messages from the database,
// and initializes session-level parameters (message count, window size, KB IDs).
func (p *pipeline) loadMessages() error {
	if err := database.DB.First(&p.triggerMessage, p.triggerMessageID).Error; err != nil {
		return fmt.Errorf("trigger message not found: %w", err)
	}

	if err := database.DB.First(&p.aiMessage, p.aiMessageID).Error; err != nil {
		return fmt.Errorf("AI message not found: %w", err)
	}

	p.sessionID = p.session.ID
	database.DB.Model(&model.Message{}).Where("session_id = ?", p.sessionID).Count(&p.messageCount)
	p.windowSize = config.Get().SummaryWindowSize
	p.kbIDs = getKnowledgeBaseIDs(p.agent)

	applogger.L.Info("Starting chat processing",
		"session_id", p.sessionID,
		"trigger_message_id", p.triggerMessageID,
		"ai_message_id", p.aiMessageID,
		"message_count", p.messageCount,
		"window_size", p.windowSize,
		"kb_count", len(p.kbIDs),
	)
	return nil
}

// preprocessQuery starts query preprocessing asynchronously when needed.
// Preprocessing runs when V >= N (for context engineering) or when knowledge
// bases are configured (for KB retrieval optimization).
// The channel is signal-only; the result is stored in p.preprocessingResult.
// On failure, p.preprocessingResult defaults to nil (degrades to original query).
func (p *pipeline) preprocessQuery() {
	if p.messageCount < int64(p.windowSize) && len(p.kbIDs) == 0 {
		applogger.L.Info("Skipping query preprocessing", "reason", "V < N and no KBs", "V", p.messageCount, "N", p.windowSize, "kb_count", len(p.kbIDs))
		return
	}

	p.preprocessingCh = make(chan struct{})
	preprocessingHistory := getPreprocessingHistory(p.sessionID, p.aiMessageID, p.windowSize)

	go func() {
		defer close(p.preprocessingCh)
		characterSettings := p.agent.CharacterSettings
		result := chatcontext.PreprocessQuery(
			p.llmConfig,
			p.triggerMessage.Content,
			preprocessingHistory,
			characterSettings,
			p.windowSize,
		)
		p.preprocessingResult = result
	}()
}

// inferUserState infers the user's state from recent messages.
// This runs synchronously; query preprocessing runs in parallel via preprocessQuery().
func (p *pipeline) inferUserState() {
	recentMessagesForState := chatcontext.GetRecentMessages(
		p.sessionID, min(int(p.messageCount), p.windowSize), model.MessageStatusCompleted,
	)

	p.userStateResult = chatcontext.InferUserState(p.llmConfig, recentMessagesForState)

	if p.userStateResult != nil {
		p.needsWorldInteraction = p.userStateResult.NeedsWorldInteraction
	}
	applogger.L.Info("User state inference",
		"needs_world_interaction", p.needsWorldInteraction,
		"session_id", p.sessionID,
		"trigger_message_id", p.triggerMessageID,
	)
}

// retrieveKnowledgeBases searches knowledge bases associated with the agent.
// Uses the preprocessed query when available for better retrieval quality.
// Waits for async preprocessing to complete before constructing the query.
func (p *pipeline) retrieveKnowledgeBases() {
	if len(p.kbIDs) == 0 {
		applogger.L.Info("Skipping KB retrieval", "reason", "no KBs configured")
		return
	}

	if p.preprocessingCh != nil {
		<-p.preprocessingCh
	}

	query := p.triggerMessage.Content
	if p.preprocessingResult != nil && p.preprocessingResult.ProcessedQuery != "" {
		query = p.preprocessingResult.ProcessedQuery
	}

	kbResults, err := kb.SearchMultiKB(context.Background(), p.kbIDs, query, 5)
	if err != nil {
		applogger.L.Error("KB retrieval failed", "session_id", p.sessionID, "error", err)
		return
	}

	for _, kr := range kbResults {
		p.kbSegments = append(p.kbSegments, chatcontext.Segment{
			Content: kr.Content,
			Source:  chatcontext.SourceKnowledgeBase,
		})
	}
	applogger.L.Info("KB retrieved segments",
		"session_id", p.sessionID,
		"count", len(kbResults),
	)
}

// executeAgentIfNeeded executes the agent path when needs_world_interaction=true,
// otherwise marks the AI message as having no interactions.
func (p *pipeline) executeAgentIfNeeded() {
	if p.needsWorldInteraction {
		database.DB.Model(&model.Message{}).Where("id = ?", p.aiMessageID).
			Update("has_interactions", model.HasInteractionsExists)
		p.taskResult = executeAgent(
			p.ctx, p.sessionID, p.agent, p.llmConfig,
			p.triggerMessage, p.aiMessageID,
			int(p.messageCount), p.windowSize, p.callbacks,
		)
	} else {
		database.DB.Model(&model.Message{}).Where("id = ?", p.aiMessageID).
			Update("has_interactions", model.HasInteractionsNone)
	}
}

// assembleContext assembles the LLM prompt messages based on context engineering rules.
// Returns (messages, earlyContent, earlyReturn). When earlyReturn is true,
// earlyContent contains the response string and the pipeline should terminate early.
func (p *pipeline) assembleContext() ([]llm.Message, string, bool) {
	if p.messageCount < int64(p.windowSize) {
		return p.assembleSimpleContext()
	}
	return p.assembleEngineeredContext()
}

// assembleSimpleContext handles the V < N branch: skip context engineering,
// use all messages directly without summary or narrative.
func (p *pipeline) assembleSimpleContext() ([]llm.Message, string, bool) {
	applogger.L.Info("V < N, skipping context engineering",
		"V", p.messageCount, "N", p.windowSize,
	)

	recentMessages := chatcontext.GetRecentMessages(
		p.sessionID, int(p.messageCount), model.MessageStatusCompleted,
	)

	// Verify that the trigger message exists in the recent completed messages.
	// We no longer require it to be the last one, because in concurrent or
	// multi-agent scenarios other completed messages (e.g., AI responses from
	// another goroutine or agent) may appear after the trigger message.
	triggerFound := false
	for _, msg := range recentMessages {
		if msg.ID == p.triggerMessageID {
			triggerFound = true
			break
		}
	}
	if !triggerFound {
		applogger.L.Error("Trigger message not found in recent completed messages",
			"session_id", p.sessionID,
			"trigger_message_id", p.triggerMessageID,
		)
		return nil, userFriendlyErrorMessage, true
	}

	characterSettings := p.agent.CharacterSettings
	messages := chatcontext.AssembleContext(
		characterSettings,
		"",
		recentMessages,
		p.kbSegments,
		-1,
		1,
		len(recentMessages),
		"",
		p.taskResult,
	)
	p.hasEmbedding = len(p.kbSegments) > 0
	return messages, "", false
}

// assembleEngineeredContext handles the V >= N branch: apply full context
// engineering pipeline including summary, retrieval, and assembly.
// Waits for async preprocessing to complete before using the result.
func (p *pipeline) assembleEngineeredContext() ([]llm.Message, string, bool) {
	if p.preprocessingCh != nil {
		<-p.preprocessingCh
	}

	// Handle clarification needed case
	if p.preprocessingResult != nil && p.preprocessingResult.NeedsClarification {
		database.DB.Model(&model.Message{}).Where("id = ?", p.aiMessageID).Updates(map[string]interface{}{
			"content": p.preprocessingResult.Clarification,
			"status":  model.MessageStatusCompleted,
		})
		database.DB.Model(&model.Session{}).Where("id = ?", p.sessionID).Update("status", model.SessionStatusIdle)
		applogger.L.Info("Query needed clarification", "session_id", p.sessionID)
		return []llm.Message{}, p.preprocessingResult.Clarification, true
	}

	processedQuery := p.triggerMessage.Content
	if p.preprocessingResult != nil {
		processedQuery = p.preprocessingResult.ProcessedQuery
		applogger.L.Info("Query type and processed",
			"type", p.preprocessingResult.QueryType,
			"processed", processedQuery[:min(50, len(processedQuery))],
		)
	}

	// Context retrieval (with or without RAG)
	var contextResult *chatcontext.RetrievalResult
	if p.preprocessingResult != nil && p.preprocessingResult.SkipRetrieval {
		contextResult = chatcontext.GetContextWithoutRAG(p.sessionID, p.windowSize)
		p.hasEmbedding = false
	} else {
		contextResult = chatcontext.GetContextForChat(p.sessionID, processedQuery, p.windowSize, 5)
		p.hasEmbedding = contextResult.HasEmbedding
	}

	// Merge knowledge base segments with chat history segments
	relevantSegments := contextResult.RelevantSegments
	if len(p.kbSegments) > 0 {
		relevantSegments = append(relevantSegments, p.kbSegments...)
		p.hasEmbedding = true
	}

	// Validate trigger_message is the latest completed message
	if len(contextResult.RecentMessages) == 0 || contextResult.RecentMessages[len(contextResult.RecentMessages)-1].ID != p.triggerMessageID {
		applogger.L.Error("Trigger message is not the latest completed message",
			"session_id", p.sessionID,
			"trigger_message_id", p.triggerMessageID,
		)
		return nil, userFriendlyErrorMessage, true
	}

	// Use cached narrative (generated in background with summary)
	var backgroundStory string
	if contextResult.Narrative != "" {
		backgroundStory = contextResult.Narrative
	}

	// Convert user state to natural language description for prompt injection
	var userStateDescription string
	if p.userStateResult != nil {
		userStateDescription = p.userStateResult.ToNaturalLanguage()
	}

	// Calculate message sequence numbers for metadata
	var summaryVersion int
	if contextResult.SummaryVersion != -1 {
		summaryVersion = contextResult.SummaryVersion
	}

	recentStart := int(p.messageCount) - len(contextResult.RecentMessages) + 1

	characterSettings := p.agent.CharacterSettings
	messages := chatcontext.AssembleContext(
		characterSettings,
		backgroundStory,
		contextResult.RecentMessages,
		relevantSegments,
		summaryVersion,
		recentStart,
		int(p.messageCount),
		userStateDescription,
		p.taskResult,
	)
	return messages, "", false
}

// streamResponse sends the assembled messages to the LLM and collects the
// complete response. The LLM stream API is still used (to avoid long blocking),
// but chunks are accumulated internally without per-chunk callbacks or DB updates.
// The AI message is finalized with the complete content in the handler layer.
func (p *pipeline) streamResponse(messages []llm.Message) (string, error) {
	// Check cancellation before starting the LLM call
	if p.ctx.Err() != nil {
		return "", p.ctx.Err()
	}

	chatModel := llm.NewChatModelWithTemperature(
		p.llmConfig.BaseURL, p.llmConfig.APIKey, p.llmConfig.ModelID, llm.TemperatureCreative,
	)

	stream, err := chatModel.ChatStream(p.ctx, messages)
	if err != nil {
		return "", fmt.Errorf("failed to start stream: %w", err)
	}
	applogger.L.Info("Starting LLM stream", "session_id", p.sessionID)

	fullContent, err := chatModel.ConsumeStream(stream, nil)
	if err != nil {
		return fullContent, err
	}

	applogger.L.Info("Chat processing completed",
		"session_id", p.sessionID,
		"response_length", len(fullContent),
	)
	return fullContent, nil
}

// postProcess handles post-response tasks: RAG indexing and summary generation.
func (p *pipeline) postProcess() {
	if p.hasEmbedding {
		go func() {
			chatcontext.IndexMessages(p.sessionID, []int64{p.triggerMessageID, p.aiMessageID})
		}()
	}

	// Trigger summary generation if V >= N
	var updatedCount int64
	database.DB.Model(&model.Message{}).Where("session_id = ?", p.sessionID).Count(&updatedCount)
	if updatedCount >= int64(p.windowSize) {
		go generateSummary(p.sessionID, int(updatedCount), p.windowSize)
	}
}

// executeAgent handles the agent execution path when needs_world_interaction=true.
//
// This function handles the agent execution path:
//  1. Notify frontend that agent is processing
//  2. Rewrite user message into clear task requirement
//  3. Execute agent via TaskExecutor
//  4. Return TaskResult for context assembly
func executeAgent(
	ctx context.Context,
	sessionID int64,
	agent *model.Agent,
	llmConfig *model.LLMConfig,
	triggerMessage model.Message,
	aiMessageID int64,
	messageCount int,
	windowSize int,
	callbacks *ChatCallbacks,
) *chatcontext.TaskResultForAssembly {
	applogger.L.Info("Agent execution path",
		"session_id", sessionID,
		"ai_msg_id", aiMessageID,
	)

	// Notify frontend that agent is processing
	if callbacks != nil && callbacks.OnNotify != nil {
		notifyData, _ := json.Marshal(map[string]string{
			"type":    "agent_processing",
			"message": "Agent is processing your request...",
		})
		callbacks.OnNotify(string(notifyData))
	}

	// Rewrite user message into clear task requirement
	recentMessages := chatcontext.GetRecentMessages(sessionID, min(messageCount, windowSize), model.MessageStatusCompleted)

	history := make([]llm.Message, 0, len(recentMessages))
	for _, msg := range recentMessages {
		history = append(history, llm.Message{
			Role:    msg.Role,
			Content: msg.Content,
		})
	}

	rewrittenRequirement := task.Rewrite(llmConfig, triggerMessage.Content, history, 10)
	applogger.L.Info("Task requirement rewritten",
		"session_id", sessionID,
		"original", triggerMessage.Content[:min(50, len(triggerMessage.Content))],
		"rewritten", rewrittenRequirement[:min(50, len(rewrittenRequirement))],
	)

	// Execute agent via TaskExecutor
	var searchConfig model.SearchConfig
	database.DB.Where("is_active = ?", true).First(&searchConfig)

	taskResult := task.Execute(task.TaskParams{
		TaskRequirement: rewrittenRequirement,
		LLMConfig:       llmConfig,
		MaxIterations:   0,
		SessionID:       sessionID,
		UserMsgID:       triggerMessage.ID,
		AgentMsgID:      aiMessageID,
		SearchConfig:    &searchConfig,
		Ctx:             ctx,
	})

	result := &chatcontext.TaskResultForAssembly{
		Status: taskResult.Status,
	}
	if taskResult.Output != "" {
		result.Result = taskResult.Output
	}
	if taskResult.Error != "" {
		result.Reason = taskResult.Error
	}
	if taskResult.Notes != "" {
		result.Notes = taskResult.Notes
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
func generateSummary(sessionID int64, version int, windowSize int) {
	chatcontext.GenerateSummaryForSession(sessionID, version, windowSize)
}

// getPreprocessingHistory retrieves messages before the AI message for preprocessing context.
// Returns messages in chronological order (oldest first), limited to the specified count.
func getPreprocessingHistory(sessionID int64, beforeMessageID int64, limit int) []llm.Message {
	var messages []model.Message
	database.DB.Where("session_id = ? AND id < ?", sessionID, beforeMessageID).
		Order("id DESC").Limit(limit).Find(&messages)

	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}

	history := make([]llm.Message, 0, len(messages))
	for _, msg := range messages {
		history = append(history, llm.Message{
			Role:    msg.Role,
			Content: msg.Content,
		})
	}
	return history
}

// getKnowledgeBaseIDs returns the knowledge base IDs associated with the agent.
func getKnowledgeBaseIDs(agent *model.Agent) []int64 {
	if agent.KnowledgeBaseIDs == "" || agent.KnowledgeBaseIDs == "[]" {
		applogger.L.Info("Agent has no KBs configured", "agent_id", agent.ID, "knowledge_base_ids", agent.KnowledgeBaseIDs)
		return nil
	}

	var ids []int64
	if err := json.Unmarshal([]byte(agent.KnowledgeBaseIDs), &ids); err != nil {
		applogger.L.Error("Failed to parse agent knowledge_base_ids", "agent_id", agent.ID, "raw", agent.KnowledgeBaseIDs, "error", err)
		return nil
	}

	var validIDs []int64
	for _, id := range ids {
		var kb model.KnowledgeBase
		if err := database.DB.First(&kb, id).Error; err == nil {
			validIDs = append(validIDs, id)
		} else {
			applogger.L.Warn("KB ID not found in database", "agent_id", agent.ID, "kb_id", id, "error", err)
		}
	}

	applogger.L.Info("Agent KB IDs resolved", "agent_id", agent.ID, "raw_ids", ids, "valid_ids", validIDs)
	return validIDs
}
