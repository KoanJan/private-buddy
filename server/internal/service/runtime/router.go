package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sashabaranov/go-openai/jsonschema"

	"private-buddy-server/internal/model"
	"private-buddy-server/internal/service/eventqueue"
	"private-buddy-server/internal/service/llm"

	applogger "private-buddy-server/internal/logger"
)

// workRouter determines whether an event should be routed to an active Work
// or start a new one. Returns nil to indicate a new Work should be created.
type workRouter interface {
	Route(ctx context.Context, event eventqueue.AgentEvent, activeWorks []*work) *work
}

// semanticWorkRouter uses LLM to judge whether an event is semantically
// related to an active Work, based on Work descriptions.
//
// Direct semantic routing replaces heuristic rules — designing a good
// heuristic is itself complex, and LLM judgment with Work descriptions
// is both simpler and more accurate.
type semanticWorkRouter struct {
	llmConfig *model.LLMConfig
}

// newSemanticWorkRouter creates a new semantic work router with LLM config.
func newSemanticWorkRouter(llmConfig *model.LLMConfig) *semanticWorkRouter {
	return &semanticWorkRouter{llmConfig: llmConfig}
}

// routePromptTemplate is the LLM prompt template for semantic routing.
// Parameters: active_works_description, message_content
const routePromptTemplate = `You are deciding whether a new message belongs to an ongoing task or starts a new one.

Active tasks:
%s

New message: %s

Which task does this message belong to? If it's related to an existing task, return that task's ID. If it's a new topic or unrelated to any active task, return null for target_work_id.

Guidelines:
- A message is related to a task if it refers to the same subject, provides additional input, corrects, or continues the task
- A simple greeting or unrelated question should start a new task (return null)
- When in doubt, start a new task (return null)`

// Route determines which active Work should handle the event.
// If no active Work is semantically related, returns nil to start a new Work.
//
// Routing logic:
//  1. No active works → nil (zero cost)
//  2. Only one active work in the same session → route to it (skip LLM call)
//  3. Multiple active works or cross-session scenario → LLM semantic routing
//
// The LLM call is short (Work descriptions + message content), so token cost
// is minimal. Routing only triggers when there are active Works.
func (r *semanticWorkRouter) Route(ctx context.Context, event eventqueue.AgentEvent, activeWorks []*work) *work {
	if len(activeWorks) == 0 {
		return nil
	}

	// Filter active works to those in the same session
	var sameSessionWorks []*work
	for _, w := range activeWorks {
		if w.sessionID == event.SessionID {
			sameSessionWorks = append(sameSessionWorks, w)
		}
	}

	// No active work in this session — start a new one
	if len(sameSessionWorks) == 0 {
		return nil
	}

	// Only one active work in the same session — route directly (skip LLM)
	if len(sameSessionWorks) == 1 {
		applogger.L.Debug("Single active work in session, routing directly",
			"work_id", sameSessionWorks[0].ID,
			"session_id", event.SessionID,
		)
		return sameSessionWorks[0]
	}

	// Multiple active works in the same session — use LLM semantic routing
	return r.semanticRoute(ctx, event, sameSessionWorks)
}

// semanticRoute uses LLM to determine which active Work the event belongs to.
// Falls back to the first same-session work on LLM failure.
func (r *semanticWorkRouter) semanticRoute(ctx context.Context, event eventqueue.AgentEvent, works []*work) *work {
	if r.llmConfig == nil {
		applogger.L.Warn("No LLM config for semantic routing, falling back to first work")
		return works[0]
	}

	messageContent := extractMessageContent(event)
	if messageContent == "" {
		return works[0]
	}

	// Build active works description for the prompt
	var workDescs []string
	for _, w := range works {
		typeName := "chat"
		if w.workType == model.WorkTypeTask {
			typeName = "task"
		}
		workDescs = append(workDescs, fmt.Sprintf("- [Work #%d, type=%s] \"%s\"", w.ID, typeName, w.description))
	}

	prompt := fmt.Sprintf(routePromptTemplate, strings.Join(workDescs, "\n"), messageContent)

	chatModel := llm.NewChatModelWithTemperature(
		r.llmConfig.BaseURL, r.llmConfig.APIKey, r.llmConfig.ModelID, llm.TemperatureDeterministic,
	)

	// Build valid work IDs for the enum
	workIDs := make([]string, 0, len(works)+1)
	for _, w := range works {
		workIDs = append(workIDs, fmt.Sprintf("%d", w.ID))
	}
	workIDs = append(workIDs, "new")

	result, err := chatModel.ChatWithJSONSchema(ctx, []llm.Message{
		{Role: "user", Content: prompt},
	}, llm.JSONSchemaDefinition{
		Name:        "RouteDecision",
		Description: "Decision on which active work the message belongs to",
		Strict:      true,
		Schema: jsonschema.Definition{
			Type: jsonschema.Object,
			Properties: map[string]jsonschema.Definition{
				"target_work_id": {
					Type:        jsonschema.String,
					Enum:        workIDs,
					Description: "The ID of the work this message belongs to, or 'new' if it should start a new work",
				},
				"reason": {
					Type:        jsonschema.String,
					Description: "Brief explanation of why this routing decision was made",
				},
			},
			Required: []string{"target_work_id", "reason"},
		},
	})

	if err != nil {
		applogger.L.Error("Semantic routing LLM call failed, falling back to first work",
			"error", err,
		)
		return works[0]
	}

	var output struct {
		TargetWorkID string `json:"target_work_id"`
		Reason       string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(result), &output); err != nil {
		applogger.L.Error("Semantic routing LLM output parse failed, falling back to first work",
			"error", err,
		)
		return works[0]
	}

	// "new" means start a new work
	if output.TargetWorkID == "new" {
		applogger.L.Info("Semantic routing: new work",
			"reason", output.Reason,
			"session_id", event.SessionID,
		)
		return nil
	}

	// Find the target work by ID
	var targetWorkID int64
	fmt.Sscanf(output.TargetWorkID, "%d", &targetWorkID)

	for _, w := range works {
		if w.ID == targetWorkID {
			applogger.L.Info("Semantic routing: matched to existing work",
				"work_id", w.ID,
				"reason", output.Reason,
			)
			return w
		}
	}

	applogger.L.Warn("Semantic routing: target work ID not found, falling back to first work",
		"target_work_id", output.TargetWorkID,
	)
	return works[0]
}
