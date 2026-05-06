package chatctx

import (
	stdctx "context"
	"fmt"

	"private-buddy-server/internal/model"
	"private-buddy-server/internal/service/llm"

	applogger "private-buddy-server/internal/logger"
)

// cachedNarrativePrompt generates a narrative from summary content only.
// Used for cached narrative generation after summary creation.
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

// narrativePrompt generates a narrative from summary + RAG segments.
// Used for legacy real-time narrative generation.
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

// NarrativeService generates background narratives from context components.
//
// Two generation modes are supported:
//
//  1. Cached narrative (from summary only, no segments):
//     - Generated in background immediately after summary generation
//     - Stored in historical_summaries.narrative field alongside the summary
//     - Retrieved at chat time without LLM call (major performance gain)
//     - Segments are handled as an independent section in context assembly
//
//  2. Legacy real-time narrative (from summary + segments):
//     - Generated on-the-fly during chat processing
//     - Segments are naturally integrated into the narrative
//     - Kept for backward compatibility but no longer used in main flow
//
// Narrative Perspective Design:
//   - Uses internal focalization (agent's viewpoint) rather than external focalization
//   - The agent is addressed as "You" to enhance immersion and continuity
//   - This helps the LLM naturally continue the conversation rather than "retell" it
type NarrativeService struct{}

// NewNarrativeService creates a NarrativeService instance.
func NewNarrativeService() *NarrativeService {
	return &NarrativeService{}
}

// GenerateNarrativeFromSummary generates a cached narrative from summary content only.
//
// This is the cached narrative generation method, called in background
// immediately after summary generation. The narrative is stored alongside
// the summary and retrieved at chat time without LLM call.
// Uses TemperatureControlled for creative but controlled output.
func (ns *NarrativeService) GenerateNarrativeFromSummary(llmConfig *model.LLMConfig, summaryContent string) string {
	if summaryContent == "" {
		return ""
	}

	prompt := fmt.Sprintf(cachedNarrativePrompt, summaryContent)

	chatModel := llm.NewChatModelWithTemperature(llmConfig.BaseURL, llmConfig.APIKey, llmConfig.ModelID, llm.TemperatureControlled)

	result, err := chatModel.Chat(stdctx.Background(), []llm.ChatMessage{
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
//
// Legacy real-time generation method. Segments are naturally integrated
// into the narrative for maximum coherence, but this requires an LLM
// call during chat processing. Uses TemperatureControlled for creative but controlled output.
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

	chatModel := llm.NewChatModelWithTemperature(llmConfig.BaseURL, llmConfig.APIKey, llmConfig.ModelID, llm.TemperatureControlled)

	result, err := chatModel.Chat(stdctx.Background(), []llm.ChatMessage{
		{Role: "user", Content: prompt},
	})
	if err != nil {
		applogger.L.Error("Failed to generate background story", "error", err)
		return ""
	}

	applogger.L.Info("Generated background story", "length", len(result))
	return result
}
