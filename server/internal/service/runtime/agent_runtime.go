package runtime

import (
	"context"
	"sync"
	"time"

	"private-buddy-server/internal/database"
	"private-buddy-server/internal/model"
	"private-buddy-server/internal/service"
	"private-buddy-server/internal/service/chat/chatctx"

	applogger "private-buddy-server/internal/logger"
)

// commitRequest represents a request to commit a draft to the messages table.
// Sent through commitCh to serialize message writes across concurrent Works.
type commitRequest struct {
	draft           *model.MessageDraft
	sessionID       int64
	content         string // Final content to write
	hasInteractions int    // HasInteractionsPending, HasInteractionsExists, or HasInteractionsNone
}

// AgentRuntime is the event-driven execution engine for an agent.
// It transforms an Agent from a passive configuration object into an active,
// stateful entity with its own lifecycle.
//
// The runtime runs a single goroutine event loop (for-select + eventCh + heartbeatTimer).
// Work execution runs in separate goroutines, allowing the event loop to remain responsive.
type AgentRuntime struct {
	agentID           int64
	workRouter        WorkRouter
	activeWorks       []*Work
	eventCh           chan AgentEvent
	commitCh          chan commitRequest
	workDoneCh        chan *Work
	heartbeatInterval time.Duration                              // Base heartbeat interval
	idleTicks         int                                        // Consecutive idle heartbeats (for adaptive backoff)
	mu                sync.Mutex                                 // Protects activeWrites for external queries
	onStatusChange    func(agentID, sessionID int64, status int) // Callback for SSE push
}

// NewAgentRuntime creates a new runtime for an agent.
func NewAgentRuntime(
	agentID int64,
	workRouter WorkRouter,
	heartbeatInterval time.Duration,
	onStatusChange func(agentID, sessionID int64, status int),
) *AgentRuntime {
	return &AgentRuntime{
		agentID:           agentID,
		workRouter:        workRouter,
		eventCh:           make(chan AgentEvent, 64),
		commitCh:          make(chan commitRequest, 16),
		workDoneCh:        make(chan *Work, 16),
		heartbeatInterval: heartbeatInterval,
		onStatusChange:    onStatusChange,
	}
}

// SendEvent sends an event to the agent's event channel.
// Non-blocking: drops the event if the channel is full.
func (r *AgentRuntime) SendEvent(event AgentEvent) {
	select {
	case r.eventCh <- event:
	default:
		applogger.L.Warn("Agent event channel full, dropping event",
			"agent_id", r.agentID,
			"event_type", event.Type,
			"session_id", event.SessionID,
		)
	}
}

// Run starts the agent's event loop. Blocks until context is cancelled.
func (r *AgentRuntime) Run(ctx context.Context) {
	heartbeatTimer := time.NewTimer(r.heartbeatInterval)

	// Start commit handler goroutine
	go r.handleCommits(ctx)

	for {
		select {
		case <-ctx.Done():
			heartbeatTimer.Stop()
			applogger.L.Info("AgentRuntime stopped", "agent_id", r.agentID)
			return

		case event := <-r.eventCh:
			// Reset idle counter on event arrival
			r.idleTicks = 0

			// Mark the message as read immediately upon receiving the event.
			// "Read" means the agent is aware of the message — this is separate
			// from "processed" (which happens after the work completes).
			if payload, ok := event.Payload.(*NewMessagePayload); ok && payload.MessageID > 0 {
				database.DB.Model(&model.ParticipantSession{}).
					Where("session_id = ? AND participant_type = ? AND participant_id = ?",
						event.SessionID, model.ParticipantTypeAgent, r.agentID).
					Update("last_read_message_id", payload.MessageID)
			}

			// Decision: should the agent respond to this event?
			agent := service.GetAgent(r.agentID)
			decision := Decide(event, agent)
			if decision == DecisionIgnore {
				applogger.L.Debug("Agent decided to ignore event",
					"agent_id", r.agentID,
					"event_type", event.Type,
				)
				r.resetHeartbeatTimer(heartbeatTimer)
				continue
			}

			// Routing + registration is ATOMIC within this case:
			// next event will see updated activeWorks
			target := r.workRouter.Route(event, r.activeWorks)
			if target != nil {
				target.FeedEvent(event)
			} else {
				w := r.newWork(event)
				r.activeWorks = append(r.activeWorks, w)
				go w.Run()
			}
			r.resetHeartbeatTimer(heartbeatTimer)

		case <-heartbeatTimer.C:
			r.handleHeartbeat()
			r.resetHeartbeatTimer(heartbeatTimer)

		case doneWork := <-r.workDoneCh:
			r.activeWorks = removeWork(r.activeWorks, doneWork)
			// Only set idle if no other works are active in this session
			if !r.hasActiveWorkInSession(doneWork.sessionID) {
				r.setStatus(doneWork.sessionID, model.ParticipantStatusIdle)
			}
		}
	}
}

// newWork creates a new Work from an event, persists it to the database,
// and sets the agent status to thinking.
func (r *AgentRuntime) newWork(event AgentEvent) *Work {
	sessionID := event.SessionID

	// Determine work type from event
	workType := model.WorkTypeChat
	description := extractDescription(event)

	// Create draft for this work, snapshotting the agent's current read position
	// as the context boundary. Messages up to this ID were visible when the
	// work started, ensuring preprocessing and context assembly have the
	// correct conversation history.
	var agentLastReadID int64
	var ps model.ParticipantSession
	if err := database.DB.Where("session_id = ? AND participant_type = ? AND participant_id = ?",
		sessionID, model.ParticipantTypeAgent, r.agentID).First(&ps).Error; err == nil {
		agentLastReadID = ps.LastReadMessageID
	}

	draft := &model.MessageDraft{
		AgentID:           r.agentID,
		SessionID:         sessionID,
		Status:            model.DraftStatusBuilding,
		LastReadMessageID: agentLastReadID,
	}
	if err := database.DB.Create(draft).Error; err != nil {
		applogger.L.Error("Failed to create draft", "agent_id", r.agentID, "session_id", sessionID, "error", err)
		draft = nil
	}

	// Persist work to database
	workRecord := &model.Work{
		AgentID:     r.agentID,
		SessionID:   sessionID,
		DraftID:     nilDraftID(draft),
		Type:        workType,
		Description: description,
		Status:      model.WorkStatusRunning,
	}
	if err := database.DB.Create(workRecord).Error; err != nil {
		applogger.L.Error("Failed to create work", "agent_id", r.agentID, "session_id", sessionID, "error", err)
	}

	w := &Work{
		ID:             workRecord.ID,
		agent:          r,
		sessionID:      sessionID,
		draft:          draft,
		workType:       workType,
		description:    description,
		maxIterations:  90, // Default max iterations for task works
		pendingEvents:  make(chan AgentEvent, 32),
		initialPayload: event.Payload,
		ctx:            context.Background(),
	}

	r.setStatus(sessionID, model.ParticipantStatusWorking)
	return w
}

// setStatus updates the agent's ParticipantSession.Status in the database
// and fires the SSE callback if the status actually changed.
func (r *AgentRuntime) setStatus(sessionID int64, status int) {
	// Read current status from DB to detect changes
	var ps model.ParticipantSession
	err := database.DB.Where(
		"session_id = ? AND participant_type = ? AND participant_id = ?",
		sessionID, model.ParticipantTypeAgent, r.agentID,
	).First(&ps).Error

	if err != nil {
		applogger.L.Error("Failed to read participant status",
			"agent_id", r.agentID, "session_id", sessionID, "error", err)
		return
	}

	if ps.Status == status {
		return // No change, skip update and callback
	}

	// Persist new status to database
	if err := database.DB.Model(&model.ParticipantSession{}).
		Where("session_id = ? AND participant_type = ? AND participant_id = ?",
			sessionID, model.ParticipantTypeAgent, r.agentID).
		Update("status", status).Error; err != nil {
		applogger.L.Error("Failed to update participant status",
			"agent_id", r.agentID, "session_id", sessionID, "error", err)
		return
	}

	// Fire SSE callback for status change
	if r.onStatusChange != nil {
		r.onStatusChange(r.agentID, sessionID, status)
	}
}

// hasActiveWorkInSession checks whether any active work exists for the
// given session. Used to determine if the agent can transition to idle
// when a work completes.
func (r *AgentRuntime) hasActiveWorkInSession(sessionID int64) bool {
	for _, w := range r.activeWorks {
		if w.sessionID == sessionID {
			return true
		}
	}
	return false
}

// resetHeartbeatTimer resets the heartbeat timer with adaptive backoff.
// On event arrival, the idle counter is reset and the timer uses the base interval.
// On idle heartbeats, the interval grows exponentially up to a maximum.
func (r *AgentRuntime) resetHeartbeatTimer(timer *time.Timer) {
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}

	// Compute adaptive interval: base * 2^idleTicks, capped at 5 minutes
	interval := r.heartbeatInterval
	for i := 0; i < r.idleTicks && interval < 5*time.Minute; i++ {
		interval *= 2
	}
	if interval > 5*time.Minute {
		interval = 5 * time.Minute
	}

	timer.Reset(interval)
}

// handleHeartbeat processes a heartbeat tick for self-reflection.
//
// Tickless pattern: the heartbeat only fires when no external events arrive.
// If the agent has active works, the heartbeat is a no-op (works drive themselves).
// If the agent is idle, the heartbeat increments the idle counter for adaptive backoff.
//
// Future iterations will use heartbeat for:
//   - Checking for stale sessions that need cleanup
//   - Proactive engagement based on conversation context
//   - Periodic knowledge base synchronization
func (r *AgentRuntime) handleHeartbeat() {
	if len(r.activeWorks) > 0 {
		// Agent is busy — no self-reflection needed
		return
	}

	r.idleTicks++
	applogger.L.Debug("Agent heartbeat (idle)",
		"agent_id", r.agentID,
		"idle_ticks", r.idleTicks,
	)
}

// handleCommits processes draft commit requests from the commitCh.
// Runs in a separate goroutine to serialize message writes.
func (r *AgentRuntime) handleCommits(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case req := <-r.commitCh:
			r.commitDraft(req)
		}
	}
}

// commitDraft atomically commits a draft to the messages table.
// This is the only path through which agent messages enter the messages table.
func (r *AgentRuntime) commitDraft(req commitRequest) {
	draft := req.draft
	if draft == nil {
		applogger.L.Error("commitDraft called with nil draft")
		return
	}

	// Create the message from the draft content
	msg := model.Message{
		SessionID:       draft.SessionID,
		Role:            model.MessageRoleAssistant,
		Content:         req.content,
		Status:          model.MessageStatusCompleted,
		HasInteractions: req.hasInteractions,
		DraftID:         &draft.ID,
	}
	if err := database.DB.Create(&msg).Error; err != nil {
		applogger.L.Error("Failed to commit draft to messages",
			"draft_id", draft.ID,
			"session_id", draft.SessionID,
			"error", err,
		)
		return
	}

	// Update draft status and content
	database.DB.Model(&model.MessageDraft{}).Where("id = ?", draft.ID).Updates(map[string]interface{}{
		"status":  model.DraftStatusCommitted,
		"content": req.content,
	})

	applogger.L.Info("Draft committed to messages",
		"draft_id", draft.ID,
		"message_id", msg.ID,
		"session_id", draft.SessionID,
	)

	// Push message event to SSE clients
	pushMessageEvent(draft.SessionID, msg.ID, msg.Content, msg.HasInteractions)

	// Trigger summary generation if needed (sender-agnostic, based on message count)
	chatctx.MaybeTriggerSummary(draft.SessionID, r.agentID)
}

// GetActiveWorkCount returns the number of currently active works.
// Used for monitoring and debugging.
func (r *AgentRuntime) GetActiveWorkCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.activeWorks)
}

// removeWork removes a work from the active works slice.
func removeWork(works []*Work, target *Work) []*Work {
	for i, w := range works {
		if w == target {
			return append(works[:i], works[i+1:]...)
		}
	}
	return works
}

// nilDraftID returns nil if draft is nil, otherwise returns a pointer to draft.ID.
func nilDraftID(draft *model.MessageDraft) *int64 {
	if draft == nil {
		return nil
	}
	return &draft.ID
}

// extractDescription extracts a short description from an event for work routing.
func extractDescription(event AgentEvent) string {
	if event.Payload == nil {
		return "Process event"
	}
	// For new message events, use the message content as description
	type messagePayload interface{ GetMessageContent() string }
	if mp, ok := event.Payload.(messagePayload); ok {
		content := mp.GetMessageContent()
		if len(content) > 200 {
			return content[:200]
		}
		return content
	}
	return "Process event"
}

// pushMessageEvent pushes a message event to SSE clients.
// This is a package-level function that will be connected to the
// handler's ConnectionManager during integration.
var pushMessageEvent = func(sessionID, messageID int64, content string, hasInteractions int) {
	// Default no-op; will be overridden during integration
	applogger.L.Debug("pushMessageEvent called (not integrated)",
		"session_id", sessionID,
		"message_id", messageID,
	)
}

// pushSSEEvent pushes a raw SSE event to all clients of a session.
// Used for notifications and other non-message events.
var pushSSEEvent = func(sessionID int64, data string) {
	// Default no-op; will be overridden during integration
	applogger.L.Debug("pushSSEEvent called (not integrated)",
		"session_id", sessionID,
	)
}

// SetPushMessageEvent sets the pushMessageEvent function.
// Called during integration with the handler layer.
func SetPushMessageEvent(fn func(sessionID, messageID int64, content string, hasInteractions int)) {
	pushMessageEvent = fn
}

// SetPushSSEEvent sets the pushSSEEvent function.
// Called during integration with the handler layer.
func SetPushSSEEvent(fn func(sessionID int64, data string)) {
	pushSSEEvent = fn
}
