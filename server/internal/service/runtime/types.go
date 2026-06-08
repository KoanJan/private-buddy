// Package runtime implements the Agent Runtime: the event-driven execution
// engine that transforms an Agent from a passive configuration object into
// an active, stateful entity with its own lifecycle.
//
// Core concepts:
//   - AgentRuntime: the main event loop (for-select + eventCh + heartbeatTimer)
//   - Work: a coherent work unit that may absorb multiple events
//   - WorkRouter: determines whether an event routes to an active Work or starts a new one
//
// Note: Agent runtime status (idle/thinking/responding/interacting) is managed
// on ParticipantSession.Status, not here. The runtime updates the database
// directly when status changes occur.
package runtime

// AgentEventType represents the type of an external stimulus that requires
// a new agent decision. Events are agent-level, not LLM-level — LLM
// tool_call results are internal to a Work's ReAct loop, not cross-Work stimuli.
type AgentEventType int

const (
	EventTypeNewMessage         AgentEventType = iota // User or agent message in a session
	EventTypeSessionJoined                            // Agent was added to a session
	EventTypeSessionLeft                              // Agent was removed from a session
	EventTypeSystemNotification                       // System-level notification
)

// AgentEvent represents any external stimulus that requires a new agent decision.
// From the agent's perspective, it only perceives two things: heartbeat (self-preservation)
// and events (everything from the external world).
type AgentEvent struct {
	Type      AgentEventType
	SessionID int64 // 0 for agent-scoped events
	Payload   any
}
