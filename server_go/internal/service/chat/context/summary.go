package context

import (
	"context"
	"fmt"

	"private-buddy-server/internal/model"
	"private-buddy-server/internal/service/llm"

	applogger "private-buddy-server/internal/logger"

	"gorm.io/gorm"
)

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

type SummaryService struct {
	db        *gorm.DB
	session   *model.Session
	agent     *model.Agent
	llmConfig *model.LLMConfig
}

func NewSummaryService(db *gorm.DB, session *model.Session, agent *model.Agent, llmConfig *model.LLMConfig) *SummaryService {
	return &SummaryService{
		db:        db,
		session:   session,
		agent:     agent,
		llmConfig: llmConfig,
	}
}

// Generate generates a summary for the specified version, matching Python's generate_summary logic.
// version = message count at the time of generation.
// windowSize = summary window size (N).
func (ss *SummaryService) Generate(version int, windowSize int) error {
	sessionID := ss.session.ID

	existing := ss.getSummary(sessionID, version)
	if existing != nil {
		applogger.L.Info("Summary already exists", "session_id", sessionID, "version", version)
		return nil
	}

	if version < windowSize {
		applogger.L.Info("Version < window_size, skipping summary generation",
			"session_id", sessionID, "version", version, "window_size", windowSize)
		return nil
	}

	var prompt string

	if version < 2*windowSize {
		messages := ss.getMessagesByRange(sessionID, 1, version)
		if len(messages) == 0 {
			applogger.L.Warn("No messages found for session", "session_id", sessionID, "range", fmt.Sprintf("1-%d", version))
			return nil
		}

		messagesText := ss.formatMessagesForSummary(messages)
		prompt = fmt.Sprintf(summaryPrompt, "(No baseline summary, this is the first summary)", messagesText)
	} else {
		baselineVersion := version - windowSize
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

		startSeq := version - windowSize + 1
		messages := ss.getMessagesByRange(sessionID, startSeq, version)
		if len(messages) == 0 {
			applogger.L.Warn("No messages found for session", "session_id", sessionID, "range", fmt.Sprintf("%d-%d", startSeq, version))
			return nil
		}

		messagesText := ss.formatMessagesForSummary(messages)
		prompt = fmt.Sprintf(summaryPrompt, baselineText, messagesText)
	}

	chatModel := ss.createChatModel()
	summaryContent, err := chatModel.Chat(context.Background(), []llm.ChatMessage{
		{Role: "user", Content: prompt},
	})
	if err != nil {
		applogger.L.Error("Summary generation LLM call failed", "session_id", sessionID, "error", err)
		return fmt.Errorf("summary generation failed: %w", err)
	}

	applogger.L.Info("Generated summary content", "session_id", sessionID, "version", version)

	narrativeSvc := NewNarrativeService()
	narrativeResult := narrativeSvc.GenerateNarrativeFromSummary(ss.llmConfig, summaryContent)
	if narrativeResult == "" {
		applogger.L.Error("Narrative generation failed, aborting atomic write", "session_id", sessionID, "version", version)
		return fmt.Errorf("narrative generation failed")
	}

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

func (ss *SummaryService) getSummary(sessionID int64, version int) *model.HistoricalSummary {
	var summary model.HistoricalSummary
	err := ss.db.Where("session_id = ? AND version = ?", sessionID, version).First(&summary).Error
	if err != nil {
		return nil
	}
	return &summary
}

// getMessagesByRange returns messages by session-internal sequence numbers (1-based, inclusive).
func (ss *SummaryService) getMessagesByRange(sessionID int64, startSeq, endSeq int) []model.Message {
	var messages []model.Message
	ss.db.Where("session_id = ?", sessionID).
		Order("id ASC").
		Offset(startSeq - 1).
		Limit(endSeq - startSeq + 1).
		Find(&messages)
	return messages
}

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

func (ss *SummaryService) createChatModel() *llm.ChatModel {
	return llm.NewChatModel(ss.llmConfig.BaseURL, ss.llmConfig.APIKey, ss.llmConfig.ModelID, 4096)
}

func (ss *SummaryService) GetLatestSummary() *model.HistoricalSummary {
	var summary model.HistoricalSummary
	err := ss.db.Where("session_id = ?", ss.session.ID).Order("version DESC").First(&summary).Error
	if err != nil {
		return nil
	}
	return &summary
}

// GetLatestSummaryByID returns the latest summary for a session by ID,
// used when SummaryService was created without a session.
func (ss *SummaryService) GetLatestSummaryByID(sessionID int64) *model.HistoricalSummary {
	var summary model.HistoricalSummary
	err := ss.db.Where("session_id = ?", sessionID).Order("version DESC").First(&summary).Error
	if err != nil {
		return nil
	}
	return &summary
}

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

func BuildSystemPrompt(agent *model.Agent, summary *model.HistoricalSummary) string {
	prompt := agent.CharacterSettings

	if summary != nil && summary.Narrative != "" {
		prompt += fmt.Sprintf("\n\n[Narrative Context]\n%s", summary.Narrative)
	}

	return prompt
}
