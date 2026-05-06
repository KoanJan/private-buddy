// Package context implements the context engineering pipeline for chat processing.
//
// This package provides the context assembly services that build the LLM message
// sequence from various context sources: summaries, narratives, retrieval results,
// user state, and task results. It matches Python's chat/context module.
package chatctx

import (
	stdctx "context"
	"fmt"

	"private-buddy-server/internal/model"
	"private-buddy-server/internal/service/llm"

	applogger "private-buddy-server/internal/logger"

	"gorm.io/gorm"
)

// summaryPrompt is the LLM prompt template for conversation summarization.
// It takes two parameters: baseline_summary and recent_messages.
const summaryPrompt = `You are a conversation summary assistant. Generate a new summary based on the conversation history and baseline summary.

Baseline summary (if exists):
%s

Recent conversation:
%s

Generate a concise but complete summary that includes key information, decisions, and context from the conversation. The summary should help understand the background for subsequent conversations.

IMPORTANT: The summary MUST preserve the original language of the conversation.
- If the conversation is in Chinese, write the summary in Chinese.
- If the conversation is in English, write the summary in English.
- If the conversation contains multiple languages, the summary may also contain multiple languages.
- Do NOT translate between languages. Maintain information fidelity.`

// SummaryService manages conversation summary generation and retrieval.
//
// Summary Generation Rules:
//   - V < N: No summary generated (not enough messages)
//   - N <= V < 2N: Full summary using all messages (1 to V) with empty baseline
//   - V >= 2N: Incremental summary using baseline (V-N) + recent messages (V-N+1 to V)
//
// After summary content is generated, a narrative is immediately generated from
// the summary content. Both are written to the database in a single atomic
// operation, ensuring no intermediate state exists where summary exists but
// narrative is empty. This eliminates the real-time narrative generation
// bottleneck during chat processing while maintaining data consistency.
type SummaryService struct {
	db        *gorm.DB
	session   *model.Session
	agent     *model.Agent
	llmConfig *model.LLMConfig
}

// NewSummaryService creates a SummaryService bound to a specific session.
func NewSummaryService(db *gorm.DB, session *model.Session, agent *model.Agent, llmConfig *model.LLMConfig) *SummaryService {
	return &SummaryService{
		db:        db,
		session:   session,
		agent:     agent,
		llmConfig: llmConfig,
	}
}

// Generate generates a summary for the specified version, matching Python's generate_summary logic.
//
// This method implements the summary generation logic:
//  1. Check if summary already exists (idempotent)
//  2. Validate version >= window_size
//  3. Determine generation strategy based on version:
//     - N <= V < 2N: Full summary with all messages
//     - V >= 2N: Incremental with baseline + recent messages
//  4. Recursively generate missing baseline if needed
//  5. Call LLM to generate summary content
//  6. Generate cached narrative from summary content
//  7. Atomically persist summary + narrative in a single write
//
// Atomic write design: summary and narrative are both generated before
// writing to the database. This eliminates the intermediate state where
// summary exists but narrative is empty, avoiding race conditions during
// concurrent reads. If narrative generation fails, the entire operation
// is aborted and no record is written — the next trigger will retry.
func (ss *SummaryService) Generate(version int, windowSize int) error {
	sessionID := ss.session.ID

	// Check if summary already exists (idempotent)
	existing := ss.getSummary(sessionID, version)
	if existing != nil {
		applogger.L.Info("Summary already exists", "session_id", sessionID, "version", version)
		return nil
	}

	// Validate minimum version
	if version < windowSize {
		applogger.L.Info("Version < window_size, skipping summary generation",
			"session_id", sessionID, "version", version, "window_size", windowSize)
		return nil
	}

	var prompt string

	// Branch 1: N <= V < 2N - Full summary with all messages
	if version < 2*windowSize {
		messages := ss.getMessagesByRange(sessionID, 1, version)
		if len(messages) == 0 {
			applogger.L.Warn("No messages found for session", "session_id", sessionID, "range", fmt.Sprintf("1-%d", version))
			return nil
		}

		messagesText := ss.formatMessagesForSummary(messages)
		prompt = fmt.Sprintf(summaryPrompt, "(No baseline summary, this is the first summary)", messagesText)
	} else {
		// Branch 2: V >= 2N - Incremental summary with baseline
		baselineVersion := version - windowSize

		// Get or recursively generate baseline summary
		baselineSummary := ss.getSummary(sessionID, baselineVersion)
		if baselineSummary == nil {
			applogger.L.Info("Baseline summary not found, generating recursively",
				"session_id", sessionID, "baseline_version", baselineVersion)
			if err := ss.Generate(baselineVersion, windowSize); err != nil {
				applogger.L.Error("Failed to generate baseline summary recursively",
					"session_id", sessionID, "baseline_version", baselineVersion, "error", err)
			}
			baselineSummary = ss.getSummary(sessionID, baselineVersion)
		}

		baselineText := "(No baseline summary)"
		if baselineSummary != nil {
			baselineText = baselineSummary.Content
		}

		// Get recent messages for the window
		startSeq := version - windowSize + 1
		messages := ss.getMessagesByRange(sessionID, startSeq, version)
		if len(messages) == 0 {
			applogger.L.Warn("No messages found for session", "session_id", sessionID, "range", fmt.Sprintf("%d-%d", startSeq, version))
			return nil
		}

		messagesText := ss.formatMessagesForSummary(messages)
		prompt = fmt.Sprintf(summaryPrompt, baselineText, messagesText)
	}

	// Generate summary content using LLM
	chatModel := ss.createChatModel()
	summaryContent, err := chatModel.Chat(stdctx.Background(), []llm.ChatMessage{
		{Role: "user", Content: prompt},
	})
	if err != nil {
		applogger.L.Error("Summary generation LLM call failed", "session_id", sessionID, "error", err)
		return fmt.Errorf("summary generation failed: %w", err)
	}

	applogger.L.Info("Generated summary content", "session_id", sessionID, "version", version)

	// Generate cached narrative from summary content (before DB write)
	narrativeSvc := NewNarrativeService()
	narrativeResult := narrativeSvc.GenerateNarrativeFromSummary(ss.llmConfig, summaryContent)
	if narrativeResult == "" {
		applogger.L.Error("Narrative generation failed, aborting atomic write", "session_id", sessionID, "version", version)
		return fmt.Errorf("narrative generation failed")
	}

	// Atomically persist summary + narrative in a single write
	newSummary := model.HistoricalSummary{
		SessionID: sessionID,
		Version:   version,
		Content:   summaryContent,
		Narrative: narrativeResult,
	}
	if err := ss.db.Create(&newSummary).Error; err != nil {
		return err
	}

	applogger.L.Info("Atomically created summary+record", "session_id", sessionID, "version", version)
	return nil
}

// getSummary retrieves a specific summary by session ID and version.
func (ss *SummaryService) getSummary(sessionID int64, version int) *model.HistoricalSummary {
	var summary model.HistoricalSummary
	err := ss.db.Where("session_id = ? AND version = ?", sessionID, version).First(&summary).Error
	if err != nil {
		return nil
	}
	return &summary
}

// getMessagesByRange returns messages by session-internal sequence numbers (1-based, inclusive).
// Messages are ordered by their global ID, which corresponds to their insertion order.
func (ss *SummaryService) getMessagesByRange(sessionID int64, startSeq, endSeq int) []model.Message {
	var messages []model.Message
	ss.db.Where("session_id = ?", sessionID).
		Order("id ASC").
		Offset(startSeq - 1).
		Limit(endSeq - startSeq + 1).
		Find(&messages)
	return messages
}

// formatMessagesForSummary formats messages for the summary prompt.
// Converts message objects into a human-readable format suitable for LLM summarization.
func (ss *SummaryService) formatMessagesForSummary(messages []model.Message) string {
	var formatted []string
	for _, msg := range messages {
		role := "User"
		if msg.Role != "user" {
			role = "Assistant"
		}
		formatted = append(formatted, fmt.Sprintf("%s: %s", role, msg.Content))
	}
	result := ""
	for i, s := range formatted {
		if i > 0 {
			result += "\n\n"
		}
		result += s
	}
	return result
}

// createChatModel creates a ChatModel for summary generation with default temperature.
func (ss *SummaryService) createChatModel() *llm.ChatModel {
	return llm.NewChatModelWithTemperature(ss.llmConfig.BaseURL, ss.llmConfig.APIKey, ss.llmConfig.ModelID, llm.TemperatureCreative)
}

// GetLatestSummary returns the latest summary for the session.
// Used during context assembly to get the most recent summary available,
// even if it's older than the current message count.
// The narrative field is included in the returned record.
func (ss *SummaryService) GetLatestSummary() *model.HistoricalSummary {
	var summary model.HistoricalSummary
	err := ss.db.Where("session_id = ?", ss.session.ID).Order("version DESC").First(&summary).Error
	if err != nil {
		return nil
	}
	return &summary
}

// GetLatestSummaryByID returns the latest summary for a session by ID,
// used when SummaryService was created without a session reference.
func (ss *SummaryService) GetLatestSummaryByID(sessionID int64) *model.HistoricalSummary {
	var summary model.HistoricalSummary
	err := ss.db.Where("session_id = ?", sessionID).Order("version DESC").First(&summary).Error
	if err != nil {
		return nil
	}
	return &summary
}

// GenerateSummaryForSession is a shared function for triggering summary generation
// from both the API handler and ChatService, matching Python's generate_summary_task.
// It loads the session, agent, and LLM config before delegating to SummaryService.Generate.
func GenerateSummaryForSession(db *gorm.DB, dataService DataServiceInterface, sessionID int64, version int, windowSize int) {
	session := dataService.GetSession(db, sessionID)
	if session == nil {
		return
	}
	agent := dataService.GetAgent(db, session.AgentID)
	if agent == nil {
		return
	}
	llmConfig := dataService.GetLLMConfig(db, agent.LLMConfigID)
	if llmConfig == nil {
		return
	}

	summaryService := NewSummaryService(db, session, agent, llmConfig)
	if err := summaryService.Generate(version, windowSize); err != nil {
		applogger.L.Error("Summary generation failed", "session_id", sessionID, "error", err)
	}
}

// DataServiceInterface defines the data access methods needed for summary generation.
type DataServiceInterface interface {
	GetSession(db *gorm.DB, sessionID int64) *model.Session
	GetAgent(db *gorm.DB, agentID int64) *model.Agent
	GetLLMConfig(db *gorm.DB, configID int64) *model.LLMConfig
}

// GetContextMessages retrieves messages for task context assembly.
// If a summary exists, only messages after the summary are returned;
// otherwise, all session messages are returned.
func GetContextMessages(db *gorm.DB, sessionID int64, maxIterations int) []llm.ChatMessage {
	var summary model.HistoricalSummary
	err := db.Where("session_id = ?", sessionID).Order("version DESC").First(&summary).Error

	var messages []model.Message
	if err == nil {
		db.Where("session_id = ? AND id > ?", sessionID, summary.ID).
			Order("created_at ASC").Find(&messages)
	} else {
		db.Where("session_id = ?", sessionID).
			Order("created_at ASC").Find(&messages)
	}

	// Limit to the most recent messages based on max iterations
	if len(messages) > maxIterations*2 {
		messages = messages[len(messages)-maxIterations*2:]
	}

	result := make([]llm.ChatMessage, 0, len(messages))
	for _, m := range messages {
		result = append(result, llm.ChatMessage{
			Role:    m.Role,
			Content: m.Content,
		})
	}

	return result
}

// BuildSystemPrompt constructs the system prompt from agent character settings
// and cached narrative context.
func BuildSystemPrompt(agent *model.Agent, summary *model.HistoricalSummary) string {
	prompt := agent.CharacterSettings

	if summary != nil && summary.Narrative != "" {
		prompt += fmt.Sprintf("\n\n[Narrative Context]\n%s", summary.Narrative)
	}

	return prompt
}
