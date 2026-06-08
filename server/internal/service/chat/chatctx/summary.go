// Package context implements the context engineering pipeline for chat processing.
//
// This package provides the context assembly services that build the LLM message
// sequence from various context sources: summaries, narratives, retrieval results,
// user state, and task results. It matches Python's chat/context module.
package chatctx

import (
	stdctx "context"
	"fmt"

	"private-buddy-server/internal/config"
	"private-buddy-server/internal/database"
	"private-buddy-server/internal/model"
	"private-buddy-server/internal/service/llm"

	applogger "private-buddy-server/internal/logger"
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

// GenerateSummary generates a summary for the specified (session, agent, version).
//
// Summaries are scoped by (session_id, agent_id) so that different agents
// maintain independent summaries and narratives for the same session.
//
// Summary Generation Rules:
//   - V < N: No summary generated (not enough messages)
//   - N <= V < 2N: Full summary using all messages (1 to V) with empty baseline
//   - V >= 2N: Incremental summary using baseline (V-N) + recent messages (V-N+1 to V)
//
// After summary content is generated, a narrative is immediately generated from
// the summary content. Both are written to the database in a single atomic
// operation, ensuring no intermediate state exists where summary exists but
// narrative is empty.
func GenerateSummary(sessionID, agentID int64, llmConfig *model.LLMConfig, version int, windowSize int) error {
	existing := getSummary(sessionID, agentID, version)
	if existing != nil {
		applogger.L.Info("Summary already exists", "session_id", sessionID, "agent_id", agentID, "version", version)
		return nil
	}

	if version < windowSize {
		applogger.L.Info("Version < window_size, skipping summary generation",
			"session_id", sessionID, "agent_id", agentID, "version", version, "window_size", windowSize)
		return nil
	}

	var prompt string

	if version < 2*windowSize {
		messages := getMessagesByRange(sessionID, 1, version)
		if len(messages) == 0 {
			applogger.L.Warn("No messages found for session", "session_id", sessionID, "range", fmt.Sprintf("1-%d", version))
			return nil
		}

		messagesText := formatMessagesForSummary(messages)
		prompt = fmt.Sprintf(summaryPrompt, "(No baseline summary, this is the first summary)", messagesText)
	} else {
		baselineVersion := version - windowSize

		baselineSummary := getSummary(sessionID, agentID, baselineVersion)
		if baselineSummary == nil {
			applogger.L.Info("Baseline summary not found, generating recursively",
				"session_id", sessionID, "agent_id", agentID, "baseline_version", baselineVersion)
			if err := GenerateSummary(sessionID, agentID, llmConfig, baselineVersion, windowSize); err != nil {
				applogger.L.Error("Failed to generate baseline summary recursively",
					"session_id", sessionID, "agent_id", agentID, "baseline_version", baselineVersion, "error", err)
			}
			baselineSummary = getSummary(sessionID, agentID, baselineVersion)
		}

		baselineText := "(No baseline summary)"
		if baselineSummary != nil {
			baselineText = baselineSummary.Content
		}

		startSeq := version - windowSize + 1
		messages := getMessagesByRange(sessionID, startSeq, version)
		if len(messages) == 0 {
			applogger.L.Warn("No messages found for session", "session_id", sessionID, "range", fmt.Sprintf("%d-%d", startSeq, version))
			return nil
		}

		messagesText := formatMessagesForSummary(messages)
		prompt = fmt.Sprintf(summaryPrompt, baselineText, messagesText)
	}

	chatModel := llm.NewChatModelWithTemperature(llmConfig.BaseURL, llmConfig.APIKey, llmConfig.ModelID, llm.TemperatureCreative)
	summaryContent, err := chatModel.Chat(stdctx.Background(), []llm.Message{
		{Role: "user", Content: prompt},
	})
	if err != nil {
		applogger.L.Error("Summary generation LLM call failed", "session_id", sessionID, "agent_id", agentID, "error", err)
		return fmt.Errorf("summary generation failed: %w", err)
	}

	applogger.L.Info("Generated summary content", "session_id", sessionID, "agent_id", agentID, "version", version)

	narrativeResult := GenerateNarrativeFromSummary(llmConfig, summaryContent)
	if narrativeResult == "" {
		applogger.L.Error("Narrative generation failed, aborting atomic write", "session_id", sessionID, "agent_id", agentID, "version", version)
		return fmt.Errorf("narrative generation failed")
	}

	newSummary := model.HistoricalSummary{
		SessionID: sessionID,
		AgentID:   agentID,
		Version:   version,
		Content:   summaryContent,
		Narrative: narrativeResult,
	}
	if err := database.DB.Create(&newSummary).Error; err != nil {
		return err
	}

	applogger.L.Info("Atomically created summary+record", "session_id", sessionID, "agent_id", agentID, "version", version)
	return nil
}

// getSummary retrieves a specific summary by (session_id, agent_id, version).
func getSummary(sessionID, agentID int64, version int) *model.HistoricalSummary {
	var summary model.HistoricalSummary
	err := database.DB.Where("session_id = ? AND agent_id = ? AND version = ?", sessionID, agentID, version).First(&summary).Error
	if err != nil {
		return nil
	}
	return &summary
}

// getMessagesByRange returns messages by session-internal sequence numbers (1-based, inclusive).
// Messages are ordered by their global ID, which corresponds to their insertion order.
func getMessagesByRange(sessionID int64, startSeq, endSeq int) []model.Message {
	var messages []model.Message
	database.DB.Where("session_id = ?", sessionID).
		Order("id ASC").
		Offset(startSeq - 1).
		Limit(endSeq - startSeq + 1).
		Find(&messages)
	return messages
}

// formatMessagesForSummary formats messages for the summary prompt.
// Converts message objects into a human-readable format suitable for LLM summarization.
func formatMessagesForSummary(messages []model.Message) string {
	var formatted []string
	for _, msg := range messages {
		role := "User"
		if msg.Role != model.MessageRoleUser {
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

// GetLatestSummaryByID returns the latest summary for a (session, agent).
func GetLatestSummaryByID(sessionID, agentID int64) *model.HistoricalSummary {
	var summary model.HistoricalSummary
	err := database.DB.Where("session_id = ? AND agent_id = ?", sessionID, agentID).Order("version DESC").First(&summary).Error
	if err != nil {
		return nil
	}
	return &summary
}

// GenerateSummaryForSession is a shared function for triggering summary generation
// from both the API handler and ChatService.
// It loads the session, agent, and LLM config before delegating to GenerateSummary.
func GenerateSummaryForSession(sessionID, agentID int64, version int, windowSize int) {
	var llmConfig model.LLMConfig
	var agent model.Agent
	if err := database.DB.First(&agent, agentID).Error; err != nil {
		applogger.L.Error("Agent not found for summary generation", "agent_id", agentID, "error", err)
		return
	}
	if err := database.DB.First(&llmConfig, agent.LLMConfigID).Error; err != nil {
		applogger.L.Error("LLMConfig not found for summary generation", "config_id", agent.LLMConfigID, "error", err)
		return
	}

	if err := GenerateSummary(sessionID, agentID, &llmConfig, version, windowSize); err != nil {
		applogger.L.Error("Summary generation failed", "session_id", sessionID, "agent_id", agentID, "error", err)
	}
}

// MaybeTriggerSummary checks if summary generation should be triggered after
// a new message is committed. Summary generation is purely based on message count:
// it triggers when the total message count in the session is a multiple of the
// configured window size. This is sender-agnostic — user and agent messages are
// treated equally.
//
// This function should be called after ANY message is created (user or agent).
func MaybeTriggerSummary(sessionID, agentID int64) {
	settings := config.Get()
	windowSize := settings.SummaryWindowSize

	var messageCount int64
	database.DB.Model(&model.Message{}).Where("session_id = ?", sessionID).Count(&messageCount)

	if messageCount >= int64(windowSize) && messageCount%int64(windowSize) == 0 {
		applogger.L.Info("Triggering summary generation",
			"session_id", sessionID, "agent_id", agentID, "V", messageCount)
		go GenerateSummaryForSession(sessionID, agentID, int(messageCount), windowSize)
	}
}

// GetContextMessages retrieves messages for task context assembly.
// If a summary exists, only messages after the summary are returned;
// otherwise, all session messages are returned.
func GetContextMessages(sessionID int64, maxIterations int) []llm.Message {
	var summary model.HistoricalSummary
	err := database.DB.Where("session_id = ?", sessionID).Order("version DESC").First(&summary).Error

	var messages []model.Message
	if err == nil {
		database.DB.Where("session_id = ? AND id > ?", sessionID, summary.ID).
			Order("created_at ASC").Find(&messages)
	} else {
		database.DB.Where("session_id = ?", sessionID).
			Order("created_at ASC").Find(&messages)
	}

	if len(messages) > maxIterations*2 {
		messages = messages[len(messages)-maxIterations*2:]
	}

	result := make([]llm.Message, 0, len(messages))
	for _, m := range messages {
		role := "user"
		if m.Role == model.MessageRoleAssistant {
			role = "assistant"
		}
		result = append(result, llm.Message{
			Role:    role,
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
