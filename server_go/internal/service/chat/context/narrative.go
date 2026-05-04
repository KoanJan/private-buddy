package context

import (
	"context"
	"fmt"

	"private-buddy-server/internal/model"
	"private-buddy-server/internal/service/llm"

	applogger "private-buddy-server/internal/logger"
)

const cachedNarrativePrompt = `You are a conversation background narrative assistant. Generate a coherent background narrative based on the summary.

Summary:
%s

Requirements:
1. Use second-person perspective (address the agent as "You"). For example: "You have been discussing X with the user. The user mentioned..."
2. Preserve ALL key information from the summary
3. Transform the summary into a flowing narrative
4. Do NOT add interpretations, judgments, or assumptions
5. Maintain information fidelity

IMPORTANT: The narrative MUST preserve the original language of the conversation.
- If the conversation is in Chinese, write the narrative in Chinese.
- If the conversation is in English, write the narrative in English.
- If the conversation contains multiple languages, the narrative may also contain multiple languages.
- Do NOT translate between languages. Maintain information fidelity.

Output only the narrative content.`

const narrativePrompt = `You are a conversation background narrative assistant. Generate a coherent background narrative based on the following information.

%s

%s

Integrate the above information into a coherent background narrative with the following requirements:
1. Use second-person perspective (address the agent as "You"). For example: "You have been discussing X with the user. The user mentioned..."
2. Preserve key information and context
3. The narrative should be coherent and flowing, not a simple list
4. Output only the narrative content, without additional explanations

IMPORTANT: The narrative MUST preserve the original language of the conversation.
- If the conversation is in Chinese, write the narrative in Chinese.
- If the conversation is in English, write the narrative in English.
- If the conversation contains multiple languages, the narrative may also contain multiple languages.
- Do NOT translate between languages. Maintain information fidelity.`

type NarrativeService struct{}

func NewNarrativeService() *NarrativeService {
	return &NarrativeService{}
}

// GenerateNarrativeFromSummary generates a cached narrative from summary content only.
// Called in background immediately after summary generation. The narrative is stored
// alongside the summary and retrieved at chat time without LLM call.
func (ns *NarrativeService) GenerateNarrativeFromSummary(llmConfig *model.LLMConfig, summaryContent string) string {
	if summaryContent == "" {
		return ""
	}

	prompt := fmt.Sprintf(cachedNarrativePrompt, summaryContent)

	chatModel := llm.NewChatModelWithTemperature(llmConfig.BaseURL, llmConfig.APIKey, llmConfig.ModelID, 4096, 0.3)

	result, err := chatModel.Chat(context.Background(), []llm.ChatMessage{
		{Role: "user", Content: prompt},
	})
	if err != nil {
		applogger.L.Error("Failed to generate cached narrative", "error", err)
		return ""
	}

	applogger.L.Info("Generated cached narrative from summary", "length", len(result))
	return result
}

// GenerateBackgroundStory generates a background story from summary and RAG segments.
// Legacy real-time generation method for backward compatibility.
func (ns *NarrativeService) GenerateBackgroundStory(
	llmConfig *model.LLMConfig,
	summary map[string]interface{},
	relevantSegments []map[string]interface{},
) string {
	if summary == nil && len(relevantSegments) == 0 {
		return ""
	}

	summarySection := ""
	if summary != nil {
		if content, ok := summary["content"].(string); ok {
			summarySection = fmt.Sprintf("[Conversation Summary]\n%s", content)
		}
	}

	segmentsSection := ""
	if len(relevantSegments) > 0 {
		segmentsText := ""
		for _, seg := range relevantSegments {
			if content, ok := seg["content"].(string); ok {
				segmentsText += fmt.Sprintf("- %s\n", content)
			}
		}
		if segmentsText != "" {
			segmentsSection = fmt.Sprintf("[Relevant Historical Segments]\n%s", segmentsText)
		}
	}

	prompt := fmt.Sprintf(narrativePrompt, summarySection, segmentsSection)

	chatModel := llm.NewChatModelWithTemperature(llmConfig.BaseURL, llmConfig.APIKey, llmConfig.ModelID, 4096, 0.3)

	result, err := chatModel.Chat(context.Background(), []llm.ChatMessage{
		{Role: "user", Content: prompt},
	})
	if err != nil {
		applogger.L.Error("Failed to generate background story", "error", err)
		return ""
	}

	applogger.L.Info("Generated background story", "length", len(result))
	return result
}
