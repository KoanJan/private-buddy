// Package eventqueue provides a stateful, in-process event bus for the agent
// runtime system. It decouples event producers (handlers, tools, scheduled
// events) from event consumers (agent runtimes) so that neither side needs
// to depend on the other.
//
// Dependency direction:
//
//	handler  → eventqueue ← runtime
//	task/tools → eventqueue   (wake_me_when goroutine calls SendEvent directly)
//	chat     → eventqueue     (replaces the old agentevent.NotifyAgentNewMessage)
//
// The package owns the event type definitions (AgentEvent, payload types) and
// the per-agent channel lifecycle.  Producers call SendEvent; consumers call
// Subscribe to obtain a read-only channel and Unsubscribe when shutting down.
package eventqueue

import (
	"sync"

	applogger "private-buddy-server/internal/logger"
)

// ---------------------------------------------------------------------------
// Event types
// ---------------------------------------------------------------------------

// AgentEventType represents the type of an agent event.
type AgentEventType int

const (
	EventTypeNewMessage         AgentEventType = iota // User or agent message in a session
	EventTypeSessionJoined                            // Agent was added to a session
	EventTypeSessionLeft                              // Agent was removed from a session
	EventTypeSystemNotification                       // System-level notification
	EventTypeScheduled                                // Scheduled event (self-wake alarm) fired
)

// AgentEvent represents an event that should be processed by an agent.
type AgentEvent struct {
	Type      AgentEventType
	SessionID int64
	Payload   any // Type depends on the event type
}

// NewMessagePayload is the payload type for EventTypeNewMessage events.
type NewMessagePayload struct {
	MessageID      int64
	MessageContent string
}

// GetMessageContent returns the message content for description extraction.
func (p *NewMessagePayload) GetMessageContent() string {
	return p.MessageContent
}

// ScheduledEventPayload is the payload type for EventTypeScheduled events.
// When a scheduled alarm fires, the agent receives this payload so it can
// recall why it set the alarm and what to do.
//
// Scheduled events are transient triggers — they carry business context but
// do NOT persist records in the messages table. Instead:
//   - TriggerMessageID points to the original user message that caused the
//     alarm, preserving the causal chain
//   - Message carries the agent's note to its future self, injected as
//     supplementary context in the pipeline
//   - Action determines whether the runtime takes the fast path (direct
//     message) or the full pipeline path
//   - ActionContent carries the pre-computed message for the fast path
type ScheduledEventPayload struct {
	ScheduledEventID int64  // ID of the ScheduledEvent record
	TriggerMessageID int64  // The user message that caused this alarm (causal chain)
	Message          string // Agent's note to its future self when the alarm fires
	Action           int    // model.ScheduledEventAction* constant
	ActionContent    string // Pre-computed message content for fast path (ActionSendMessage)
}

// GetMessageContent returns the alarm message for description extraction.
func (p *ScheduledEventPayload) GetMessageContent() string {
	return p.Message
}

// ---------------------------------------------------------------------------
// eventQueue — the stateful event bus
// ---------------------------------------------------------------------------

const channelSize = 64 // Per-agent event channel buffer size

// eventQueue is a stateful, in-process event bus that maintains per-agent
// event channels.  It is the single routing point between event producers
// and agent runtime consumers.
//
// Thread-safe.  All public methods may be called concurrently.
type eventQueue struct {
	mu    sync.RWMutex
	chans map[int64]chan AgentEvent // agentID -> event channel
}

// global is the singleton eventQueue instance.
// Initialized once during application startup via InitGlobal.
var global *eventQueue

// Init creates and sets the global eventQueue singleton.
func Init() {
	global = newEventQueue()
	applogger.L.Info("Global event queue initialized")
}

// SendEvent sends an event to the given agent via the global event queue.
func SendEvent(agentID int64, event AgentEvent) {
	global.SendEvent(agentID, event)
}

// Subscribe returns the event channel for the given agent via the global queue.
func Subscribe(agentID int64) <-chan AgentEvent {
	return global.Subscribe(agentID)
}

// Unsubscribe removes the event channel for the given agent via the global queue.
func Unsubscribe(agentID int64) {
	global.Unsubscribe(agentID)
}

// newEventQueue creates a new eventQueue.
func newEventQueue() *eventQueue {
	return &eventQueue{
		chans: make(map[int64]chan AgentEvent),
	}
}

// Subscribe creates (if needed) and returns the event channel for the given
// agent.  The caller (typically AgentRuntime) should consume from this
// channel in its event loop.
//
// Returns a read-only channel.  The channel is buffered (channelSize) and is
// unique per agent — calling Subscribe multiple times for the same agent
// returns the same channel.
func (q *eventQueue) Subscribe(agentID int64) <-chan AgentEvent {
	q.mu.Lock()
	defer q.mu.Unlock()

	if ch, ok := q.chans[agentID]; ok {
		return ch
	}

	ch := make(chan AgentEvent, channelSize)
	q.chans[agentID] = ch
	return ch
}

// Unsubscribe removes the event channel for the given agent and drains any
// remaining events.  Called when an agent runtime shuts down.
func (q *eventQueue) Unsubscribe(agentID int64) {
	q.mu.Lock()
	defer q.mu.Unlock()

	ch, ok := q.chans[agentID]
	if !ok {
		return
	}

	// Drain remaining events to prevent goroutine leak
	for {
		select {
		case <-ch:
		default:
			close(ch)
			delete(q.chans, agentID)
			return
		}
	}
}

// SendEvent sends an event to the given agent's event channel.
// Non-blocking: drops the event if the channel is full.
// If no agent is subscribed, the event is silently dropped with a warning.
func (q *eventQueue) SendEvent(agentID int64, event AgentEvent) {
	q.mu.RLock()
	ch, ok := q.chans[agentID]
	q.mu.RUnlock()

	if !ok {
		applogger.L.Warn("No subscriber for agent event, dropping",
			"agent_id", agentID,
			"event_type", event.Type,
			"session_id", event.SessionID,
		)
		return
	}

	select {
	case ch <- event:
	default:
		applogger.L.Warn("Agent event channel full, dropping event",
			"agent_id", agentID,
			"event_type", event.Type,
			"session_id", event.SessionID,
		)
	}
}
