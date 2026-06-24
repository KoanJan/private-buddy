package comprehend

import (
	"context"
	"fmt"

	"private-buddy-server/internal/database"
	"private-buddy-server/internal/model"
	"private-buddy-server/internal/service/llm"

	applogger "private-buddy-server/internal/logger"
)

// cachedNarrativePrompt generates a first-person experiential narrative from summary content.
// Used for cached narrative generation after summary creation.
//
// The narrative will be injected as "Background context from earlier" in the
// context assembly, positioned between character settings and recent conversation.
// It tells you what you have experienced and discussed — your own memory of the
// conversation, written from your perspective.
const cachedNarrativePrompt = `Rewrite the following conversation summary as a coherent first-hand background narrative — as if you are recalling your own experience.

Conversation summary:
%s

Requirements:
1. Write in first-person perspective. You ARE the person who lived through this conversation.
2. Preserve ALL key information from the summary
3. Transform the summary into a flowing, natural recollection
4. Do NOT add interpretations, judgments, or assumptions beyond what is stated
5. Maintain information fidelity

IMPORTANT: The narrative MUST preserve the original language of the conversation.
- If the conversation is in Chinese, write the narrative in Chinese.
- If the conversation is in English, write the narrative in English.
- If the conversation contains multiple languages, the narrative may also contain multiple languages.
- Do NOT translate between languages. Maintain information fidelity.

Output only the narrative content.`

// generateNarrativeFromSummary generates a cached narrative from summary content only.
//
// This is the cached narrative generation method, called after summary generation.
// The narrative is stored in agent_narratives alongside the summary version.
// Uses TemperatureControlled for creative but controlled output.
func generateNarrativeFromSummary(ctx context.Context, llmConfig *model.LLMConfig, summaryContent string) string {
	if summaryContent == "" {
		return ""
	}

	prompt := fmt.Sprintf(cachedNarrativePrompt, summaryContent)

	chatModel := llm.NewChatModelWithTemperature(llmConfig.BaseURL, llmConfig.APIKey, llmConfig.ModelID, llm.TemperatureControlled)

	result, err := chatModel.Chat(ctx, []llm.Message{
		{Role: "user", Content: prompt},
	})
	if err != nil {
		applogger.Error("Failed to generate cached narrative", "error", err)
		return ""
	}

	applogger.Info("Generated cached narrative from summary", "length", len(result))
	return result
}

// generateNarrativeForAgent generates an agent-specific narrative from the
// session-level summary at the given version. Idempotent — skips if a narrative
// already exists for this (session, agent, summary_version) combination.
func generateNarrativeForAgent(ctx context.Context, sessionID, agentID int64, llmConfig *model.LLMConfig, summaryVersion int) error {
	existing := getAgentNarrative(sessionID, agentID, summaryVersion)
	if existing != nil {
		applogger.Info("Agent narrative already exists",
			"session_id", sessionID, "agent_id", agentID, "summary_version", summaryVersion)
		return nil
	}

	// Load the session-level summary content
	summary := getSessionSummary(sessionID, summaryVersion)
	if summary == nil {
		applogger.Error("Summary not found for narrative generation",
			"session_id", sessionID, "summary_version", summaryVersion)
		return fmt.Errorf("summary not found for session %d version %d", sessionID, summaryVersion)
	}

	narrativeContent := generateNarrativeFromSummary(ctx, llmConfig, summary.Content)
	if narrativeContent == "" {
		return fmt.Errorf("narrative generation returned empty content")
	}

	narrative := model.AgentNarrative{
		SessionID:      sessionID,
		AgentID:        agentID,
		SummaryVersion: summaryVersion,
		Content:        narrativeContent,
	}
	if err := database.DB.Create(&narrative).Error; err != nil {
		return fmt.Errorf("failed to save agent narrative: %w", err)
	}

	applogger.Info("Created agent narrative",
		"session_id", sessionID, "agent_id", agentID, "summary_version", summaryVersion,
		"length", len(narrativeContent))
	return nil
}
