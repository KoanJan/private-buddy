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
// Draft-based architecture:
// The pipeline does NOT write to the messages table directly. It returns all
// results through ChatResult, and the caller (Work) commits them to a draft
// and then to messages atomically. This eliminates the placeholder message pattern.
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
const userFriendlyErrorMessage = "Sorry, something went wrong on the server. Please try again later."

// ChatCallbacks holds optional callbacks for notification events.
type ChatCallbacks struct {
	OnNotify func(data string)
}

// ChatResult holds the output of the chat processing pipeline.
// In the draft-based architecture, the pipeline does not write to the messages
// table directly. Instead, it returns all results through this struct, and the
// caller (Work) commits them to a draft and then to messages atomically.
type ChatResult struct {
	Content         string // The generated response content
	HasInteractions int    // HasInteractionsPending, HasInteractionsExists, or HasInteractionsNone
}

// TriggerOverrideType identifies the kind of trigger override, which determines
// how the pipeline assembles context for the LLM.
type TriggerOverrideType int

const (
	// TriggerOverrideNone indicates no override (default user message flow).
	TriggerOverrideNone TriggerOverrideType = iota
	// TriggerOverrideScheduledAlarm indicates the trigger is a scheduled alarm
	// firing. The pipeline must present this as a system notification ("your
	// alarm went off"), NOT as a new user request.
	TriggerOverrideScheduledAlarm
)

// TriggerOverride provides supplementary context for non-direct triggers.
// Unlike the trigger message (which is the user message that caused the
// pipeline run), the override carries additional context from the trigger
// mechanism itself.
//
// The Type field determines how the pipeline assembles the final context:
//   - TriggerOverrideScheduledAlarm: the original user message is preserved as
//     reference only, and the override content (agent's self-reminder) becomes
//     the primary action context. The pipeline constructs a system-level alarm
//     notification semantic so the LLM understands "your alarm went off, act now"
//     rather than "the user is asking you to set an alarm again".
type TriggerOverride struct {
	Type    TriggerOverrideType // Kind of trigger override
	Content string              // Supplementary trigger context (e.g., alarm self-reminder)
}

// pipeline holds the state for a single chat processing execution.
// It is short-lived (only exists during one Process call) and carries
// shared data between pipeline stages.
type pipeline struct {
	session          *model.Session
	agent            *model.Agent
	llmConfig        *model.LLMConfig
	triggerMessageID int64
	triggerOverride  *TriggerOverride // Non-nil when the trigger is not a persisted message (e.g., scheduled event)
	draftID          int64            // Draft ID for interaction records
	contextBoundary  int64            // Last message ID visible when this pipeline started (from draft.last_read_message_id)
	callbacks        *ChatCallbacks

	// Loaded in loadMessages
	triggerMessage model.Message
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
//
// Parameters:
//   - contextBoundary: the last message ID visible when the draft was created
//     (from draft.last_read_message_id). Used for preprocessing history queries.
//   - draftID: the draft ID for interaction record association.
//   - triggerOverride: optional supplementary context for non-direct triggers
//     (e.g., scheduled events). When set, the override content is injected into
//     the pipeline alongside the normal trigger message.
func Process(
	ctx context.Context,
	session *model.Session,
	agent *model.Agent,
	llmConfig *model.LLMConfig,
	triggerMessageID int64,
	contextBoundary int64,
	draftID int64,
	callbacks *ChatCallbacks,
	triggerOverride *TriggerOverride,
) (*ChatResult, error) {
	p := &pipeline{
		session:          session,
		agent:            agent,
		llmConfig:        llmConfig,
		triggerMessageID: triggerMessageID,
		triggerOverride:  triggerOverride,
		contextBoundary:  contextBoundary,
		draftID:          draftID,
		callbacks:        callbacks,
	}

	if err := p.loadMessages(); err != nil {
		return &ChatResult{Content: userFriendlyErrorMessage, HasInteractions: model.HasInteractionsNone}, err
	}

	// Start async preprocessing first so it runs in parallel with user state inference
	p.preprocessQuery(ctx)
	p.inferUserState(ctx)

	p.retrieveKnowledgeBases(ctx)
	p.executeAgentIfNeeded(ctx)

	messages, earlyContent, earlyReturn := p.assembleContext(ctx)
	if earlyReturn {
		return &ChatResult{Content: earlyContent, HasInteractions: model.HasInteractionsNone}, nil
	}

	fullContent, err := p.streamResponse(ctx, messages)
	if err != nil {
		return &ChatResult{Content: fullContent, HasInteractions: model.HasInteractionsNone}, err
	}

	p.postProcess(ctx)

	hasInteractions := model.HasInteractionsNone
	if p.needsWorldInteraction {
		hasInteractions = model.HasInteractionsExists
	}

	return &ChatResult{
		Content:         fullContent,
		HasInteractions: hasInteractions,
	}, nil
}

// loadMessages loads the trigger message from the database,
// and initializes session-level parameters (message count, window size, KB IDs).
//
// When triggerOverride is set, its content is injected into the pipeline
// according to the override type:
//   - TriggerOverrideScheduledAlarm: the original user message is preserved as
//     reference, but the primary context is a system-level alarm notification.
//     This prevents the LLM from misinterpreting the alarm as a new user request
//     to set another alarm (which would cause an infinite loop).
func (p *pipeline) loadMessages() error {
	if p.triggerMessageID <= 0 {
		return fmt.Errorf("trigger message ID is required")
	}

	if err := database.DB.First(&p.triggerMessage, p.triggerMessageID).Error; err != nil {
		return fmt.Errorf("trigger message not found: %w", err)
	}

	// Inject trigger override based on type.
	if p.triggerOverride != nil && p.triggerOverride.Content != "" {
		switch p.triggerOverride.Type {
		case TriggerOverrideScheduledAlarm:
			// Scheduled alarm: construct a system notification semantic.
			// The LLM must understand "your alarm went off, act now",
			// NOT "the user is asking you to set an alarm again".
			// The original user message is preserved as reference only,
			// while the agent's self-reminder is the primary action context.
			p.triggerMessage.Content = fmt.Sprintf(
				"[ALARM NOTIFICATION] An alarm you set has just triggered. This is NOT a new user request — you set this alarm yourself earlier. Take action now based on your self-reminder below.\n\nYour self-reminder: %s\n\n[Original user message for reference: %s]",
				p.triggerOverride.Content,
				p.triggerMessage.Content,
			)
		default:
			// Unknown override type: fall back to appending as supplementary context
			p.triggerMessage.Content = fmt.Sprintf(
				"%s\n\n[Supplementary Context: %s]",
				p.triggerMessage.Content,
				p.triggerOverride.Content,
			)
		}
	}

	p.sessionID = p.session.ID
	database.DB.Model(&model.Message{}).Where("session_id = ?", p.sessionID).Count(&p.messageCount)
	p.windowSize = config.Get().SummaryWindowSize
	p.kbIDs = getKnowledgeBaseIDs(p.agent)

	applogger.L.Info("Starting chat processing",
		"session_id", p.sessionID,
		"trigger_message_id", p.triggerMessageID,
		"draft_id", p.draftID,
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
func (p *pipeline) preprocessQuery(ctx context.Context) {
	if p.messageCount < int64(p.windowSize) && len(p.kbIDs) == 0 {
		applogger.L.Info("Skipping query preprocessing", "reason", "V < N and no KBs", "V", p.messageCount, "N", p.windowSize, "kb_count", len(p.kbIDs))
		return
	}

	p.preprocessingCh = make(chan struct{})
	preprocessingHistory := getPreprocessingHistory(p.sessionID, p.windowSize)

	go func() {
		defer close(p.preprocessingCh)
		characterSettings := p.agent.CharacterSettings
		result := chatcontext.PreprocessQuery(
			ctx,
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
func (p *pipeline) inferUserState(ctx context.Context) {
	recentMessagesForState := chatcontext.GetRecentMessages(
		p.sessionID, min(int(p.messageCount), p.windowSize), model.MessageStatusCompleted,
	)

	p.userStateResult = chatcontext.InferUserState(ctx, p.llmConfig, recentMessagesForState)

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
func (p *pipeline) retrieveKnowledgeBases(ctx context.Context) {
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

	kbResults, err := kb.SearchMultiKB(ctx, p.kbIDs, query, 5)
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

// executeAgentIfNeeded executes the agent path when needs_world_interaction=true.
// In the draft-based architecture, this does NOT write to the messages table.
// The has_interactions flag is returned via ChatResult for the caller to handle.
func (p *pipeline) executeAgentIfNeeded(ctx context.Context) {
	if p.needsWorldInteraction {
		p.taskResult = executeAgent(
			ctx, p.sessionID, p.agent, p.llmConfig,
			p.triggerMessage, p.draftID,
			int(p.messageCount), p.windowSize, p.callbacks,
		)
	}
}

// assembleContext assembles the LLM prompt messages based on context engineering rules.
// Returns (messages, earlyContent, earlyReturn). When earlyReturn is true,
// earlyContent contains the response string and the pipeline should terminate early.
func (p *pipeline) assembleContext(ctx context.Context) ([]llm.Message, string, bool) {
	if p.messageCount < int64(p.windowSize) {
		return p.assembleSimpleContext()
	}
	return p.assembleEngineeredContext(ctx)
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
func (p *pipeline) assembleEngineeredContext(ctx context.Context) ([]llm.Message, string, bool) {
	if p.preprocessingCh != nil {
		<-p.preprocessingCh
	}

	// Handle clarification needed case — return clarification as content
	// without writing to messages table (caller handles draft commit)
	if p.preprocessingResult != nil && p.preprocessingResult.NeedsClarification {
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
		contextResult = chatcontext.GetContextWithoutRAG(p.sessionID, p.agent.ID, p.windowSize)
		p.hasEmbedding = false
	} else {
		contextResult = chatcontext.GetContextForChat(ctx, p.sessionID, p.agent.ID, processedQuery, p.windowSize, 5)
		p.hasEmbedding = contextResult.HasEmbedding
	}

	// Merge knowledge base segments with chat history segments
	relevantSegments := contextResult.RelevantSegments
	if len(p.kbSegments) > 0 {
		relevantSegments = append(relevantSegments, p.kbSegments...)
		p.hasEmbedding = true
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
func (p *pipeline) streamResponse(ctx context.Context, messages []llm.Message) (string, error) {
	// Check cancellation before starting the LLM call
	if ctx.Err() != nil {
		return "", ctx.Err()
	}

	chatModel := llm.NewChatModelWithTemperature(
		p.llmConfig.BaseURL, p.llmConfig.APIKey, p.llmConfig.ModelID, llm.TemperatureCreative,
	)

	stream, err := chatModel.ChatStream(ctx, messages)
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
// Note: message indexing uses triggerMessageID only, since the AI message
// doesn't exist yet (draft has not been committed).
func (p *pipeline) postProcess(ctx context.Context) {
	if p.hasEmbedding {
		go func() {
			chatcontext.IndexMessages(ctx, p.sessionID, []int64{p.triggerMessageID})
		}()
	}

	// Note: summary generation is now triggered at the message creation level
	// (after any message is committed, regardless of sender), not here.
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
	draftID int64,
	messageCount int,
	windowSize int,
	callbacks *ChatCallbacks,
) *chatcontext.TaskResultForAssembly {
	applogger.L.Info("Agent execution path",
		"session_id", sessionID,
		"draft_id", draftID,
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
		role := "user"
		if msg.Role == model.MessageRoleAssistant {
			role = "assistant"
		}
		history = append(history, llm.Message{
			Role:    role,
			Content: msg.Content,
		})
	}

	rewrittenRequirement := task.Rewrite(ctx, llmConfig, triggerMessage.Content, history, 10)
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
		AgentID:         agent.ID,
		UserMsgID:       triggerMessage.ID,
		DraftID:         draftID,
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

// getPreprocessingHistory retrieves recent messages for preprocessing context.
// Returns messages in chronological order (oldest first), limited to the specified count.
// Unlike context assembly (which uses contextBoundary to avoid overlap with summary),
// preprocessing needs the full recent conversation to correctly classify and rewrite queries.
func getPreprocessingHistory(sessionID int64, limit int) []llm.Message {
	var messages []model.Message
	database.DB.Where("session_id = ?", sessionID).
		Order("id DESC").Limit(limit).Find(&messages)

	// Reverse to chronological order
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}

	history := make([]llm.Message, 0, len(messages))
	for _, msg := range messages {
		role := "user"
		if msg.Role == model.MessageRoleAssistant {
			role = "assistant"
		}
		history = append(history, llm.Message{
			Role:    role,
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
