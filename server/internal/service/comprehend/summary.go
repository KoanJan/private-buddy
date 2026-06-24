package comprehend

import (
	"context"
	"fmt"

	"private-buddy-server/internal/config"
	"private-buddy-server/internal/database"
	"private-buddy-server/internal/model"
	"private-buddy-server/internal/service"
	"private-buddy-server/internal/service/llm"

	applogger "private-buddy-server/internal/logger"
)

// summaryPrompt is the LLM prompt template for conversation summarization.
// It takes two parameters: baseline_summary and recent_messages.
const summaryPrompt = `Generate a summary based on the conversation history and baseline summary.

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

// generateSummary generates a session-level factual summary for the specified version.
//
// Summaries are scoped by session_id only — all agents in the same session
// share the same factual summary. Agent-specific perspective is handled by
// AgentNarrative, which is generated separately after the summary.
//
// Summary Generation Rules:
//   - V < N: No summary generated (not enough messages)
//   - N <= V < 2N: Full summary using all messages (1 to V) with empty baseline
//   - V >= 2N: Incremental summary using baseline (V-N) + recent messages (V-N+1 to V)
func generateSummary(ctx context.Context, sessionID int64, llmConfig *model.LLMConfig, version int, windowSize int) error {
	existing := getSessionSummary(sessionID, version)
	if existing != nil {
		applogger.Info("Summary already exists", "session_id", sessionID, "version", version)
		return nil
	}

	if version < windowSize {
		applogger.Info("Version < window_size, skipping summary generation",
			"session_id", sessionID, "version", version, "window_size", windowSize)
		return nil
	}

	var prompt string

	if version < 2*windowSize {
		messages := getMessagesByRange(sessionID, 1, version)
		if len(messages) == 0 {
			applogger.Warn("No messages found for session", "session_id", sessionID, "range", fmt.Sprintf("1-%d", version))
			return nil
		}

		messagesText := formatMessagesForSummaryGeneric(messages)
		prompt = fmt.Sprintf(summaryPrompt, "(No baseline summary, this is the first summary)", messagesText)
	} else {
		baselineVersion := version - windowSize

		baselineSummary := getSessionSummary(sessionID, baselineVersion)
		if baselineSummary == nil {
			applogger.Info("Baseline summary not found, generating recursively",
				"session_id", sessionID, "baseline_version", baselineVersion)
			if err := generateSummary(ctx, sessionID, llmConfig, baselineVersion, windowSize); err != nil {
				applogger.Error("Failed to generate baseline summary recursively",
					"session_id", sessionID, "baseline_version", baselineVersion, "error", err)
			}
			baselineSummary = getSessionSummary(sessionID, baselineVersion)
		}

		baselineText := "(No baseline summary)"
		if baselineSummary != nil {
			baselineText = baselineSummary.Content
		}

		startSeq := version - windowSize + 1
		messages := getMessagesByRange(sessionID, startSeq, version)
		if len(messages) == 0 {
			applogger.Warn("No messages found for session", "session_id", sessionID, "range", fmt.Sprintf("%d-%d", startSeq, version))
			return nil
		}

		messagesText := formatMessagesForSummaryGeneric(messages)
		prompt = fmt.Sprintf(summaryPrompt, baselineText, messagesText)
	}

	chatModel := llm.NewChatModelWithTemperature(llmConfig.BaseURL, llmConfig.APIKey, llmConfig.ModelID, llm.TemperatureCreative)
	summaryContent, err := chatModel.Chat(ctx, []llm.Message{
		{Role: "user", Content: prompt},
	})
	if err != nil {
		applogger.Error("Summary generation LLM call failed", "session_id", sessionID, "error", err)
		return fmt.Errorf("summary generation failed: %w", err)
	}

	applogger.Info("Generated summary content", "session_id", sessionID, "version", version)

	newSummary := model.Summary{
		SessionID: sessionID,
		Version:   version,
		Content:   summaryContent,
	}
	if err := database.DB.Create(&newSummary).Error; err != nil {
		return err
	}

	applogger.Info("Created session summary", "session_id", sessionID, "version", version)
	return nil
}

// getSessionSummary retrieves a specific session-level summary by (session_id, version).
func getSessionSummary(sessionID int64, version int) *model.Summary {
	var s model.Summary
	err := database.DB.Where("session_id = ? AND version = ?", sessionID, version).First(&s).Error
	if err != nil {
		return nil
	}
	return &s
}

// getLatestSummaryByID returns the latest summary for a session.
func getLatestSummaryByID(sessionID int64) *model.Summary {
	var s model.Summary
	err := database.DB.Where("session_id = ?", sessionID).Order("version DESC").First(&s).Error
	if err != nil {
		return nil
	}
	return &s
}

// getLatestNarrativeByIDs returns the latest narrative for a (session, agent).
func getLatestNarrativeByIDs(sessionID, agentID int64) *model.AgentNarrative {
	var n model.AgentNarrative
	err := database.DB.Where("session_id = ? AND agent_id = ?", sessionID, agentID).
		Order("summary_version DESC").First(&n).Error
	if err != nil {
		return nil
	}
	return &n
}

// getAgentNarrative retrieves a specific narrative by (session_id, agent_id, summary_version).
func getAgentNarrative(sessionID, agentID int64, summaryVersion int) *model.AgentNarrative {
	var n model.AgentNarrative
	err := database.DB.Where("session_id = ? AND agent_id = ? AND summary_version = ?",
		sessionID, agentID, summaryVersion).First(&n).Error
	if err != nil {
		return nil
	}
	return &n
}

// getMessagesByRange returns messages by session-internal sequence numbers (1-based, inclusive).
// Messages are ordered by their global ID, which corresponds to their insertion order.
func getMessagesByRange(sessionID int64, startSeq, endSeq int) []model.Message {
	var messages []model.Message
	if err := database.DB.Where("session_id = ?", sessionID).
		Order("id ASC").
		Offset(startSeq - 1).
		Limit(endSeq - startSeq + 1).
		Find(&messages).Error; err != nil {
		applogger.Warn("getMessagesByRange: failed to load messages", "session_id", sessionID, "error", err)
		return nil
	}
	return messages
}

// formatMessagesForSummary formats messages for the summary prompt.
// Converts message objects into a human-readable format suitable for LLM summarization.
// userName is the actual name of the other party, agentName is the agent's own name.
// Kept for backward compatibility with narrative generation which needs named roles.
func formatMessagesForSummary(messages []model.Message, personName, agentName string) string {
	personRole := personName
	var formatted []string
	for _, msg := range messages {
		role := personRole
		if msg.Role != model.MessageRoleUser {
			role = agentName
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

// formatMessagesForSummaryGeneric formats messages using role-based labels
// (User/Assistant) suitable for a session-level factual summary.
func formatMessagesForSummaryGeneric(messages []model.Message) string {
	userName := service.GetUserName()
	var formatted []string
	for _, msg := range messages {
		role := userName
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

// generateSummaryForSession generates a session-level summary, then generates
// an agent-specific narrative from the summary. This is the entry point for
// background summary+narrative generation triggered after message commits.
func generateSummaryForSession(ctx context.Context, sessionID, agentID int64, version int, windowSize int) {
	var llmConfig model.LLMConfig
	var agent model.Agent
	if err := database.DB.First(&agent, agentID).Error; err != nil {
		applogger.Error("Agent not found for summary generation", "agent_id", agentID, "error", err)
		return
	}
	if err := database.DB.First(&llmConfig, agent.LLMConfigID).Error; err != nil {
		applogger.Error("LLMConfig not found for summary generation", "config_id", agent.LLMConfigID, "error", err)
		return
	}

	// Step 1: Generate session-level summary (idempotent — skips if exists)
	if err := generateSummary(ctx, sessionID, &llmConfig, version, windowSize); err != nil {
		applogger.Error("Summary generation failed", "session_id", sessionID, "error", err)
		return
	}

	// Step 2: Generate agent-specific narrative from the summary (idempotent — skips if exists)
	if err := generateNarrativeForAgent(ctx, sessionID, agentID, &llmConfig, version); err != nil {
		applogger.Error("Narrative generation failed", "session_id", sessionID, "agent_id", agentID, "error", err)
	}
}

// MaybeTriggerSummary checks if summary generation should be triggered after
// a new message is committed. Summary generation is purely based on message count:
// it triggers when the total message count in the session is a multiple of the
// configured window size. This is sender-agnostic — user and agent messages are
// treated equally.
//
// After the summary is generated, an agent-specific narrative is also generated
// for the given agent. In future multi-agent scenarios, this would iterate over
// all agents in the session.
//
// This function should be called after ANY message is created (user or agent).
func MaybeTriggerSummary(ctx context.Context, sessionID, agentID int64) {
	settings := config.Get()
	windowSize := settings.SummaryWindowSize

	var messageCount int64
	if err := database.DB.Model(&model.Message{}).Where("session_id = ?", sessionID).Count(&messageCount).Error; err != nil {
		applogger.Warn("MaybeTriggerSummary: failed to count messages", "session_id", sessionID, "error", err)
		return
	}

	if messageCount >= int64(windowSize) && messageCount%int64(windowSize) == 0 {
		applogger.Info("Triggering summary generation",
			"session_id", sessionID, "agent_id", agentID, "V", messageCount)
		go generateSummaryForSession(ctx, sessionID, agentID, int(messageCount), windowSize)
	}
}
