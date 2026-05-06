package chatctx

import (
	stdctx "context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sashabaranov/go-openai/jsonschema"

	"private-buddy-server/internal/model"
	"private-buddy-server/internal/service/llm"

	applogger "private-buddy-server/internal/logger"
)

// userStateInferencePrompt is the LLM prompt template for user state inference.
// It takes one parameter: recent_messages (formatted dialog text).
const userStateInferencePrompt = `Based on the following recent conversation, infer the user's current state.

Recent conversation:
%s

Analyze the user's emotional tone, conversational purpose, and any clues about their physical situation.

Also determine if the user's request requires interaction with the external world:
- Set needs_world_interaction=true if the request needs: real-time information (news, weather, stock prices), 
  file operations (create, modify, delete files), code execution, web searches, or any tool usage.
- Set needs_world_interaction=false if the LLM can answer directly from its training data 
  (general knowledge, advice, explanations, casual conversation).`

// UserState represents the inferred user state from conversation context.
//
// Three-dimensional model:
//   - Emotion: user's current emotional state (affects response tone)
//   - Purpose: user's current conversational goal (affects response content direction)
//   - Situation: user's physical context (affects response constraints)
//
// Intent type is implicitly derived from purpose + situation, not modeled separately.
//
// Field descriptions serve dual purpose:
//  1. Guide LLM structured output generation
//  2. Provide natural language fragments for prompt template assembly
type UserState struct {
	Emotion               string `json:"emotion"`
	Purpose               string `json:"purpose"`
	Situation             string `json:"situation"`
	NeedsWorldInteraction bool   `json:"needs_world_interaction"`
}

// ToNaturalLanguage converts the structured user state into a natural language description
// suitable for injection into the prompt's instruction area.
func (us *UserState) ToNaturalLanguage() string {
	emotionMap := map[string]string{
		"calm":       "calm and relaxed",
		"anxious":    "anxious or worried",
		"frustrated": "frustrated or impatient",
		"urgent":     "under time pressure or in urgency",
		"curious":    "curious and exploratory",
	}
	purposeMap := map[string]string{
		"seek_help":         "seeking help with a problem",
		"seek_advice":       "looking for advice or recommendations",
		"seek_confirmation": "seeking confirmation or validation",
		"express_feeling":   "expressing feelings without expecting solutions",
		"casual_chat":       "engaging in casual conversation",
	}

	emotionDesc := us.Emotion
	if desc, ok := emotionMap[us.Emotion]; ok {
		emotionDesc = desc
	}
	purposeDesc := us.Purpose
	if desc, ok := purposeMap[us.Purpose]; ok {
		purposeDesc = desc
	}

	parts := []string{
		fmt.Sprintf("The user appears %s", emotionDesc),
		fmt.Sprintf("is %s", purposeDesc),
	}
	if us.Situation != "" && us.Situation != "unknown" {
		parts = append(parts, fmt.Sprintf("and is likely %s", us.Situation))
	}
	if us.NeedsWorldInteraction {
		parts = append(parts, "and needs to interact with the external world (tools, real-time data, or file operations)")
	}

	return strings.Join(parts, ", ") + "."
}

// UserStateService infers user state from recent conversation messages.
// Uses LLM structured output (JSON Schema response format) to produce a UserState.
// On failure, returns nil, allowing graceful degradation — the main chat flow
// continues without user state.
type UserStateService struct{}

// NewUserStateService creates a UserStateService instance.
func NewUserStateService() *UserStateService {
	return &UserStateService{}
}

// formatRecentMessages formats recent messages into text for the inference prompt.
func (uss *UserStateService) formatRecentMessages(recentMessages []map[string]interface{}) string {
	var lines []string
	for _, msg := range recentMessages {
		role := "User"
		if r, ok := msg["role"].(string); ok && r != "user" {
			role = "Assistant"
		}
		content, _ := msg["content"].(string)
		lines = append(lines, fmt.Sprintf("%s: %s", role, content))
	}
	return strings.Join(lines, "\n")
}

// InferUserState infers the user's current state from recent conversation messages.
// Uses TemperatureDeterministic for consistent, deterministic outputs.
// Returns nil if inference fails, allowing the chat flow to continue without user state.
func (uss *UserStateService) InferUserState(
	llmConfig *model.LLMConfig,
	recentMessages []map[string]interface{},
) *UserState {
	if len(recentMessages) == 0 {
		return nil
	}

	chatModel := llm.NewChatModelWithTemperature(llmConfig.BaseURL, llmConfig.APIKey, llmConfig.ModelID, llm.TemperatureDeterministic)

	dialogText := uss.formatRecentMessages(recentMessages)
	prompt := fmt.Sprintf(userStateInferencePrompt, dialogText)

	result, err := chatModel.ChatWithJSONSchema(stdctx.Background(), []llm.ChatMessage{
		{Role: "user", Content: prompt},
	}, llm.JSONSchemaDefinition{
		Name:        "UserState",
		Description: "Infer the user's current state from conversation context",
		Strict:      true,
		Schema: jsonschema.Definition{
			Type: jsonschema.Object,
			Properties: map[string]jsonschema.Definition{
				"emotion": {
					Type: jsonschema.String,
					Enum: []string{"calm", "anxious", "frustrated", "urgent", "curious"},
					Description: "The user's current emotional state: " +
						"'calm' for relaxed or neutral, " +
						"'anxious' for worried or uneasy, " +
						"'frustrated' for annoyed or impatient (e.g. repeated failed attempts), " +
						"'urgent' for time-pressured or emergency, " +
						"'curious' for inquisitive or exploratory",
				},
				"purpose": {
					Type: jsonschema.String,
					Enum: []string{"seek_help", "seek_advice", "seek_confirmation", "express_feeling", "casual_chat"},
					Description: "The user's current conversational goal: " +
						"'seek_help' for needing a solution or fix, " +
						"'seek_advice' for wanting recommendations or guidance, " +
						"'seek_confirmation' for validating a decision or understanding, " +
						"'express_feeling' for sharing emotions without expecting solutions, " +
						"'casual_chat' for social or non-goal-oriented conversation",
				},
				"situation": {
					Type:        jsonschema.String,
					Description: "Brief natural language description of the user's physical context if inferable from the conversation, such as time of day, device, environment, or activity. Use 'unknown' if not inferable. Examples: 'at work on desktop', 'late evening on mobile', 'in a meeting', 'commuting'",
				},
				"needs_world_interaction": {
					Type:        jsonschema.Boolean,
					Description: "Whether the user's request requires interaction with the external world: true if the request needs tools, real-time information, file operations, or any action beyond the LLM's parametric knowledge; false if the LLM can answer directly from its training data",
				},
			},
			Required: []string{"emotion", "purpose", "situation", "needs_world_interaction"},
		},
	})

	if err != nil {
		applogger.L.Error("Failed to infer user state", "error", err)
		return nil
	}

	if result != "" {
		var state UserState
		if err := json.Unmarshal([]byte(result), &state); err == nil {
			applogger.L.Info("Inferred user state",
				"emotion", state.Emotion,
				"purpose", state.Purpose,
				"situation", state.Situation,
				"needs_world_interaction", state.NeedsWorldInteraction,
			)
			return &state
		}
	}

	return nil
}
