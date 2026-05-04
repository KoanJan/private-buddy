package task

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sashabaranov/go-openai/jsonschema"

	"private-buddy-server/internal/model"
	"private-buddy-server/internal/service/llm"

	applogger "private-buddy-server/internal/logger"
)

const rewritePrompt = `You are a task requirement rewriter. Your job is to transform the user's message into a clear, self-contained task requirement that can be executed by an AI agent.

Conversation history:
%s

Current user message: %s

Your task:
1. Analyze the user's message in the context of the conversation history
2. Identify any references to previous content (files, code, topics discussed)
3. Extract the actual task the user wants to accomplish
4. Write a clear, complete task requirement that:
   - Can be understood without the conversation history
   - Specifies what needs to be done
   - Includes relevant details from the context (file paths, specific content, etc.)

IMPORTANT RULES:
- If the user's message is already clear and complete, output it as-is
- If the message references previous context, incorporate that context
- If the message is too vague even with context, state what information is missing
- Keep the rewritten requirement concise but complete
- The output should be in the SAME LANGUAGE as the user's message

Output a JSON object with:
- requirement: The rewritten task requirement (required)
- context_summary: Brief note on what context was used (optional)`

// RewrittenRequirement represents the structured output of task requirement rewriting.
type RewrittenRequirement struct {
	Requirement    string  `json:"requirement"`
	ContextSummary *string `json:"context_summary"`
}

// TaskRequirementRewriter rewrites user messages into clear task requirements.
type TaskRequirementRewriter struct{}

func NewTaskRequirementRewriter() *TaskRequirementRewriter {
	return &TaskRequirementRewriter{}
}

// FormatHistory formats conversation history for the rewrite prompt.
func (trw *TaskRequirementRewriter) FormatHistory(history []map[string]string, maxMessages int) string {
	if len(history) == 0 {
		return "(No conversation history)"
	}

	recent := history
	if len(history) > maxMessages {
		recent = history[len(history)-maxMessages:]
	}

	var formatted []string
	for _, msg := range recent {
		role := "User"
		if msg["role"] != "user" {
			role = "Assistant"
		}
		formatted = append(formatted, fmt.Sprintf("%s: %s", role, msg["content"]))
	}
	return strings.Join(formatted, "\n")
}

// Rewrite rewrites a user message into a clear task requirement.
func (trw *TaskRequirementRewriter) Rewrite(
	llmConfig *model.LLMConfig,
	userMessage string,
	history []map[string]string,
	maxHistoryMessages int,
) string {
	chatModel := llm.NewChatModelWithTemperature(llmConfig.BaseURL, llmConfig.APIKey, llmConfig.ModelID, 2048, 0.1)

	historyText := trw.FormatHistory(history, maxHistoryMessages)
	prompt := fmt.Sprintf(rewritePrompt, historyText, userMessage)

	result, err := chatModel.ChatWithJSONSchema(context.Background(), []llm.ChatMessage{
		{Role: "user", Content: prompt},
	}, llm.JSONSchemaDefinition{
		Name:        "RewrittenRequirement",
		Description: "The rewritten task requirement",
		Strict:      true,
		Schema: jsonschema.Definition{
			Type: jsonschema.Object,
			Properties: map[string]jsonschema.Definition{
				"requirement": {
					Type:        jsonschema.String,
					Description: "The rewritten, self-contained task requirement",
				},
				"context_summary": {
					Type:        jsonschema.String,
					Description: "Brief summary of relevant context used for rewriting",
				},
			},
			Required: []string{"requirement"},
		},
	})

	if err != nil {
		applogger.L.Error("Task requirement rewrite failed", "error", err)
		return userMessage
	}

	if result != "" {
		var rewritten RewrittenRequirement
		if err := json.Unmarshal([]byte(result), &rewritten); err == nil {
			applogger.L.Info("Task requirement rewritten",
				"original", userMessage[:minLen(50, len(userMessage))],
				"rewritten", rewritten.Requirement[:minLen(50, len(rewritten.Requirement))],
			)
			return rewritten.Requirement
		}
	}

	return userMessage
}

func minLen(a, b int) int {
	if a < b {
		return a
	}
	return b
}
