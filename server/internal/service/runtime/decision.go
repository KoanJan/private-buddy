package runtime

import (
	"private-buddy-server/internal/model"

	applogger "private-buddy-server/internal/logger"
)

// Decision represents the agent's decision on whether to respond to an event.
type Decision int

const (
	DecisionRespond Decision = iota // Agent should respond (create/continue Work)
	DecisionIgnore                  // Agent should ignore this event
	DecisionDefer                   // Agent acknowledges but defers action (future: multi-agent)
)

// Decide determines whether the agent should respond to an event.
//
// In the current 1v1 architecture, the agent always responds to user messages.
// This is the simplest correct behavior: every user message in a 1v1 session
// deserves a response.
//
// Future iterations will introduce:
//   - Multi-agent sessions where Decide uses LLM to judge relevance
//   - System events (e.g., file changes) where Decide filters noise
//   - Defer decisions for background processing
func Decide(event AgentEvent, agent *model.Agent) Decision {
	switch event.Type {
	case EventTypeNewMessage:
		// 1v1: always respond to user messages
		return DecisionRespond

	case EventTypeSessionJoined:
		// Agent was added to a session — may need to introduce itself
		return DecisionDefer

	case EventTypeSessionLeft, EventTypeSystemNotification:
		// Informational events, no response needed
		return DecisionIgnore

	default:
		applogger.L.Warn("Unknown event type in Decide",
			"event_type", event.Type,
			"agent_id", agent.ID,
		)
		return DecisionIgnore
	}
}
