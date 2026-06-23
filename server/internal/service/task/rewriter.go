package task

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"private-buddy-server/internal/model"
	"private-buddy-server/internal/service/llm"

	applogger "private-buddy-server/internal/logger"
)

// rewritePrompt is the system prompt for task requirement rewriting.
// Transforms ambiguous user messages into explicit, self-contained task requirements
// by utilizing conversation history to resolve references and provide context.
const rewritePrompt = `You are a task requirement rewriter. Your job is to transform the original message into a clear, self-contained task requirement that can be executed by an AI agent.

Conversation history:
%s

Current message: %s

Your task:
1. Analyze the message in the context of the conversation history
2. Identify any references to previous content (files, code, topics discussed)
3. Extract the actual task to accomplish
4. Write a clear, complete task requirement that:
   - Can be understood without the conversation history
   - Specifies what needs to be done
   - Includes relevant details from the context (file paths, specific content, etc.)

IMPORTANT RULES:
- If the message is already clear and complete, output it as-is
- If the message references previous context, incorporate that context
- If the message is too vague even with context, state what information is missing
- Keep the rewritten requirement concise but complete
- The output should be in the SAME LANGUAGE as the original message

Output a JSON object with:
- requirement: The rewritten task requirement (required)
- context_summary: Brief note on what context was used (optional)`

// RewrittenRequirement represents the structured output of task requirement rewriting.
type RewrittenRequirement struct {
	Requirement    string `json:"requirement" jsonschema:"description=The rewritten, self-contained task requirement,required"`
	ContextSummary string `json:"context_summary" jsonschema:"description=Brief summary of relevant context used for rewriting"`
}

// FormatHistory formats conversation history for the rewrite prompt.
// Takes the last maxMessages messages and formats them with role prefixes.
// userName is the actual name of the other party, agentName is the agent's own name.
// Returns "(No conversation history)" if history is empty.
func FormatHistory(history []llm.Message, maxMessages int, userName, agentName string) string {
	if len(history) == 0 {
		return "(No conversation history)"
	}

	recent := history
	if len(history) > maxMessages {
		recent = history[len(history)-maxMessages:]
	}

	var formatted []string
	for _, msg := range recent {
		role := userName
		if msg.Role != "user" {
			role = agentName
		}
		formatted = append(formatted, fmt.Sprintf("%s: %s", role, msg.Content))
	}
	return strings.Join(formatted, "\n")
}

// Rewrite rewrites a user message into a clear task requirement.
// Uses conversation history to resolve references and create a self-contained task requirement.
// Returns the original userMessage on error (graceful degradation).
func Rewrite(
	ctx context.Context,
	llmConfig *model.LLMConfig,
	userMessage string,
	history []llm.Message,
	maxHistoryMessages int,
	agentName string,
	userName string,
) string {
	chatModel := llm.NewChatModelWithTemperature(llmConfig.BaseURL, llmConfig.APIKey, llmConfig.ModelID, llm.TemperatureDeterministic)

	historyText := FormatHistory(history, maxHistoryMessages, userName, agentName)
	prompt := fmt.Sprintf(rewritePrompt, historyText, userMessage)

	result, err := chatModel.ChatWithJSONSchema(ctx, []llm.Message{
		{Role: "user", Content: prompt},
	}, llm.JSONSchemaDefinition{
		Name:        "RewrittenRequirement",
		Description: "The rewritten task requirement",
		Strict:      true,
		Schema:      llm.GenerateSchema[RewrittenRequirement](),
	})

	if err != nil {
		applogger.Error("Task requirement rewrite failed", "error", err)
		return userMessage
	}

	if result != "" {
		var rewritten RewrittenRequirement
		if err := json.Unmarshal([]byte(result), &rewritten); err == nil {
			applogger.Info("Task requirement rewritten",
				"original", userMessage[:min(50, len(userMessage))],
				"rewritten", rewritten.Requirement[:min(50, len(rewritten.Requirement))],
			)
			return rewritten.Requirement
		}
	}

	return userMessage
}
