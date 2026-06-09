package runtime

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/sashabaranov/go-openai/jsonschema"

	"private-buddy-server/internal/model"
	"private-buddy-server/internal/service/eventqueue"
	"private-buddy-server/internal/service/llm"

	applogger "private-buddy-server/internal/logger"
)

// DecisionAction represents the type of action the agent should take.
type DecisionAction int

const (
	ActionReplyNow      DecisionAction = iota // Immediate reply (simple Q&A)
	ActionReplyThenWork                       // Acknowledge first, then execute task
	ActionWorkOnly                            // Execute task without pre-reply
	ActionIgnore                              // No response needed
	ActionDefer                               // Acknowledge but defer (future: multi-agent)
)

// Decision represents the agent's decision on how to respond to an event.
//
// The decision model distinguishes five actions based on message type,
// estimated task duration, and relevance. In 1v1 scenarios, ActionIgnore
// is never produced — every user message deserves a response.
type Decision struct {
	Action         DecisionAction
	Reasoning      string  // Why the agent made this decision
	RelevanceScore float64 // 0-1, how relevant the message is to the agent's role
}

// decisionOutput is the structured output schema for the LLM decision call.
type decisionOutput struct {
	Action         string  `json:"action"`
	Reasoning      string  `json:"reasoning"`
	RelevanceScore float64 `json:"relevance_score"`
}

// decidePromptTemplate is the LLM prompt template for decision making.
// Parameters: agent_description, message_content
const decidePromptTemplate = `You are an AI assistant deciding how to respond to a user message.

Agent role: %s

User message: %s

Decide the appropriate action:
- "reply_now": Simple question, greeting, or casual chat. The agent can answer directly from its knowledge without tools or long processing.
- "reply_then_work": A task or request that requires significant time or tool usage. The agent should acknowledge first ("Got it, I'll work on that"), then execute the task.
- "work_only": A continuation or correction of an ongoing task. No acknowledgment needed — just continue working.
- "ignore": The message is completely outside the agent's scope or irrelevant. (Use sparingly — most messages deserve some response.)

Guidelines:
- When in doubt, prefer "reply_now" over "reply_then_work"
- "reply_then_work" is for requests that clearly need tool usage, file operations, web searches, or multi-step execution
- "work_only" is for follow-up instructions like "continue", "skip that step", "try a different approach"
- "ignore" should almost never be used in 1v1 conversations

Also rate the relevance of this message to the agent's role (0.0 to 1.0).`

// Decide determines how the agent should respond to an event.
//
// For EventTypeNewMessage, the decision is made by LLM based on message content
// and agent role. For other event types, simple rule-based decisions are used.
//
// The LLM call uses TemperatureDeterministic for consistent decision making.
// On LLM failure, falls back to ActionReplyNow (always respond) — safe default
// for 1v1 scenarios where ignoring a message is worse than an unnecessary reply.
func Decide(ctx context.Context, event eventqueue.AgentEvent, agent *model.Agent, llmConfig *model.LLMConfig) Decision {
	// Non-message events use simple rule-based decisions
	switch event.Type {
	case eventqueue.EventTypeSessionJoined:
		return Decision{Action: ActionDefer, Reasoning: "Agent joined session", RelevanceScore: 1.0}
	case eventqueue.EventTypeSessionLeft, eventqueue.EventTypeSystemNotification:
		return Decision{Action: ActionIgnore, Reasoning: "Informational event, no response needed", RelevanceScore: 0.0}
	case eventqueue.EventTypeScheduled:
		// Scheduled events are self-wake alarms — always respond
		return Decision{Action: ActionReplyNow, Reasoning: "Scheduled event (self-wake alarm)", RelevanceScore: 1.0}
	case eventqueue.EventTypeNewMessage:
		// Proceed to LLM-based decision below
	default:
		applogger.L.Warn("Unknown event type in Decide",
			"event_type", event.Type,
			"agent_id", agent.ID,
		)
		return Decision{Action: ActionIgnore, Reasoning: "Unknown event type", RelevanceScore: 0.0}
	}

	// Extract message content for LLM decision
	messageContent := extractMessageContent(event)
	if messageContent == "" {
		return Decision{Action: ActionReplyNow, Reasoning: "Empty message, default to reply", RelevanceScore: 0.5}
	}

	// LLM-based decision for new messages
	agentDescription := agent.CharacterSettings
	if agent.Description != "" {
		agentDescription = agent.Description
	}

	prompt := fmt.Sprintf(decidePromptTemplate, agentDescription, messageContent)

	chatModel := llm.NewChatModelWithTemperature(
		llmConfig.BaseURL, llmConfig.APIKey, llmConfig.ModelID, llm.TemperatureDeterministic,
	)

	result, err := chatModel.ChatWithJSONSchema(ctx, []llm.Message{
		{Role: "user", Content: prompt},
	}, llm.JSONSchemaDefinition{
		Name:        "Decision",
		Description: "Agent's decision on how to respond to a user message",
		Strict:      true,
		Schema: jsonschema.Definition{
			Type: jsonschema.Object,
			Properties: map[string]jsonschema.Definition{
				"action": {
					Type: jsonschema.String,
					Enum: []string{"reply_now", "reply_then_work", "work_only", "ignore"},
					Description: "The action the agent should take: " +
						"'reply_now' for simple Q&A, " +
						"'reply_then_work' for tasks needing acknowledgment then execution, " +
						"'work_only' for task continuations, " +
						"'ignore' for irrelevant messages",
				},
				"reasoning": {
					Type:        jsonschema.String,
					Description: "Brief explanation of why this action was chosen",
				},
				"relevance_score": {
					Type:        jsonschema.Number,
					Description: "How relevant the message is to the agent's role, from 0.0 (irrelevant) to 1.0 (highly relevant)",
				},
			},
			Required: []string{"action", "reasoning", "relevance_score"},
		},
	})

	if err != nil {
		applogger.L.Error("Decision LLM call failed, falling back to reply_now",
			"agent_id", agent.ID,
			"error", err,
		)
		return Decision{Action: ActionReplyNow, Reasoning: "LLM decision failed, safe fallback", RelevanceScore: 0.5}
	}

	var output decisionOutput
	if err := json.Unmarshal([]byte(result), &output); err != nil {
		applogger.L.Error("Decision LLM output parse failed, falling back to reply_now",
			"agent_id", agent.ID,
			"error", err,
		)
		return Decision{Action: ActionReplyNow, Reasoning: "LLM output parse failed, safe fallback", RelevanceScore: 0.5}
	}

	action := parseAction(output.Action)
	relevance := output.RelevanceScore
	if relevance < 0 {
		relevance = 0
	}
	if relevance > 1 {
		relevance = 1
	}

	applogger.L.Info("Decision made",
		"agent_id", agent.ID,
		"action", output.Action,
		"reasoning", output.Reasoning,
		"relevance", relevance,
	)

	return Decision{
		Action:         action,
		Reasoning:      output.Reasoning,
		RelevanceScore: relevance,
	}
}

// parseAction converts a string action name to DecisionAction.
func parseAction(s string) DecisionAction {
	switch s {
	case "reply_now":
		return ActionReplyNow
	case "reply_then_work":
		return ActionReplyThenWork
	case "work_only":
		return ActionWorkOnly
	case "ignore":
		return ActionIgnore
	default:
		applogger.L.Warn("Unknown decision action string, defaulting to reply_now", "action", s)
		return ActionReplyNow
	}
}

// extractMessageContent extracts the message content from an event payload.
func extractMessageContent(event eventqueue.AgentEvent) string {
	if event.Payload == nil {
		return ""
	}
	type messagePayload interface{ GetMessageContent() string }
	if mp, ok := event.Payload.(messagePayload); ok {
		return mp.GetMessageContent()
	}
	return ""
}
