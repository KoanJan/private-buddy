package runtime

import (
	"context"
	"fmt"

	"private-buddy-server/internal/database"
	"private-buddy-server/internal/model"
	"private-buddy-server/internal/service"
	"private-buddy-server/internal/service/chat"
	"private-buddy-server/internal/service/eventqueue"

	applogger "private-buddy-server/internal/logger"
)

// work represents a unit of work for an agent within a session.
// It is created when an agent decides to act on an event, and it may
// absorb subsequent events (e.g., user corrections) during its execution.
//
// Three-layer model: Agent (long-lived) → work (coherent goal) → Iteration (atomic ReAct step)
//
// work unifies the Chat path (single LLM call) and Task path (ReAct loop):
//   - Chat work: single LLM call, absorbs events before context assembly
//   - Task work: ReAct loop, absorbs events at each iteration boundary
type work struct {
	ID                 int64
	agent              *agentRuntime
	sessionID          int64
	draft              *model.MessageDraft
	workType           int
	description        string
	maxIterations      int
	pendingEvents      chan eventqueue.AgentEvent
	initialPayload     any     // The payload from the event that created this work
	absorbedMessageIDs []int64 // Message IDs absorbed during execution (for merged reply)
}

// Run executes the work. For chat-type work, this runs the existing
// chat pipeline. For task-type work, this runs the ReAct loop.
// On completion, commits the draft and signals the event loop to remove this work.
// Respects context cancellation: exits early if the work is cancelled.
func (w *work) Run(ctx context.Context) {
	defer func() {
		// Signal event loop to remove this work
		w.agent.workDoneCh <- w

		// Update work status in database
		database.DB.Model(&model.Work{}).Where("id = ?", w.ID).
			Update("status", model.WorkStatusCompleted)
	}()

	applogger.L.Info("work started",
		"work_id", w.ID,
		"session_id", w.sessionID,
		"type", w.workType,
		"description", w.description,
	)

	// Absorb any pending events before starting the pipeline.
	// This handles the case where the user sends multiple messages quickly —
	// all accumulated messages are collected so the pipeline can produce a
	// single merged response addressing all of them.
	w.absorbPendingEvents()

	// Check cancellation after absorbing events
	if ctx.Err() != nil {
		applogger.L.Info("work cancelled before pipeline", "work_id", w.ID)
		w.abandon()
		return
	}

	// Load session, agent, and LLM config for the chat pipeline
	session, agent, llmConfig := w.loadChatDependencies()
	if session == nil || agent == nil || llmConfig == nil {
		w.handleChatError()
		return
	}

	// Determine the effective trigger message ID.
	// If events were absorbed, use the last absorbed message as the trigger —
	// the pipeline loads recent messages from the database, so all intermediate
	// messages (including the original trigger) are naturally included in context.
	//
	// For scheduled events, there is no persisted trigger message (triggerMessageID=0).
	// The alarm context is passed as a TriggerOverride to the pipeline instead.
	triggerMessageID := w.getTriggerMessageID()
	if len(w.absorbedMessageIDs) > 0 {
		triggerMessageID = w.absorbedMessageIDs[len(w.absorbedMessageIDs)-1]
		applogger.L.Info("work using absorbed message as trigger",
			"work_id", w.ID,
			"original_trigger", w.getTriggerMessageID(),
			"new_trigger", triggerMessageID,
			"absorbed_count", len(w.absorbedMessageIDs),
		)
	}

	// Run the chat pipeline with the work's context for cancellation support.
	callbacks := &chat.ChatCallbacks{
		OnNotify: func(data string) {
			// Forward notifications to SSE clients
			pushSSEEvent(w.sessionID, data)
		},
	}

	contextBoundary := int64(0)
	if w.draft != nil {
		contextBoundary = w.draft.LastReadMessageID
	}

	draftID := int64(0)
	if w.draft != nil {
		draftID = w.draft.ID
	}

	// Build trigger override for scheduled events.
	// The trigger message is the user's original request (causal chain).
	// The override carries the agent's action instruction to its future self,
	// with a type marker so the pipeline assembles alarm-notification semantics
	// (rather than treating it as a new user request).
	var triggerOverride *chat.TriggerOverride
	if payload, ok := w.initialPayload.(*eventqueue.ScheduledEventPayload); ok {
		triggerOverride = &chat.TriggerOverride{
			Type:    chat.TriggerOverrideScheduledAlarm,
			Content: payload.Message,
		}
		applogger.L.Info("work using trigger override for scheduled event",
			"work_id", w.ID,
			"session_id", w.sessionID,
			"trigger_message_id", triggerMessageID,
		)
	}

	result, err := chat.Process(ctx, session, agent, llmConfig, triggerMessageID, contextBoundary, draftID, callbacks, triggerOverride)
	if err != nil {
		// Distinguish between cancellation and real errors
		if ctx.Err() != nil {
			applogger.L.Info("work cancelled during pipeline", "work_id", w.ID)
			w.abandon()
			return
		}
		applogger.L.Error("Chat processing failed in work",
			"work_id", w.ID,
			"session_id", w.sessionID,
			"error", err,
		)
		w.handleChatError()
		return
	}

	// Update draft content with the result
	if w.draft != nil {
		w.updateDraftContent(result.Content)
	}

	// Commit the draft: atomically create message from draft
	w.commitDraft(result.Content, result.HasInteractions)

	applogger.L.Info("work completed",
		"work_id", w.ID,
		"session_id", w.sessionID,
		"draft_id", draftID,
	)
}

// FeedEvent feeds an event to the work's pending events channel.
// Non-blocking: drops the event if the channel is full.
func (w *work) FeedEvent(event eventqueue.AgentEvent) {
	select {
	case w.pendingEvents <- event:
		applogger.L.Debug("Event fed to work",
			"work_id", w.ID,
			"event_type", event.Type,
		)
	default:
		applogger.L.Warn("work pending events channel full, dropping event",
			"work_id", w.ID,
			"event_type", event.Type,
		)
	}
}

// absorbPendingEvents drains all pending events from the channel.
// Called at each iteration boundary — the work voluntarily checks for
// new input, like a human checking for new instructions between steps.
func (w *work) absorbPendingEvents() {
	for {
		select {
		case event := <-w.pendingEvents:
			w.handleEvent(event)
		default:
			return
		}
	}
}

// handleEvent processes a single absorbed event.
// For new message events, the message ID is collected for merged reply —
// the chat pipeline will see all accumulated messages from the database
// and produce a single response that addresses them together.
func (w *work) handleEvent(event eventqueue.AgentEvent) {
	if payload, ok := event.Payload.(*eventqueue.NewMessagePayload); ok {
		w.absorbedMessageIDs = append(w.absorbedMessageIDs, payload.MessageID)
		applogger.L.Info("work absorbed message",
			"work_id", w.ID,
			"message_id", payload.MessageID,
			"total_absorbed", len(w.absorbedMessageIDs),
		)
	} else {
		applogger.L.Info("work absorbing event",
			"work_id", w.ID,
			"event_type", event.Type,
		)
	}
}

// commitDraft commits the draft by sending it through the serialized commit channel.
// The commit handler creates a message from the draft content and pushes it to SSE clients.
func (w *work) commitDraft(content string, hasInteractions int) {
	if w.draft == nil {
		applogger.L.Error("work.commitDraft called with nil draft", "work_id", w.ID)
		return
	}

	w.agent.commitCh <- commitRequest{
		draft:           w.draft,
		sessionID:       w.sessionID,
		content:         content,
		hasInteractions: hasInteractions,
	}
}

// updateDraftContent writes content to the draft in the database.
func (w *work) updateDraftContent(content string) {
	if w.draft == nil {
		return
	}
	w.draft.Content = content
	database.DB.Model(&model.MessageDraft{}).Where("id = ?", w.draft.ID).
		Update("content", content)
}

// abandon marks the work and its draft as abandoned.
func (w *work) abandon() {
	database.DB.Model(&model.Work{}).Where("id = ?", w.ID).
		Update("status", model.WorkStatusAbandoned)

	if w.draft != nil {
		database.DB.Model(&model.MessageDraft{}).Where("id = ?", w.draft.ID).
			Update("status", model.DraftStatusDiscarded)
	}
}

// loadChatDependencies loads session, agent, and LLM config from the database.
func (w *work) loadChatDependencies() (*model.Session, *model.Agent, *model.LLMConfig) {
	session := service.GetSession(w.sessionID)
	if session == nil {
		applogger.L.Error("Session not found", "session_id", w.sessionID)
		return nil, nil, nil
	}

	agent := service.GetAgent(session.AgentID)
	if agent == nil {
		applogger.L.Error("Agent not found", "agent_id", session.AgentID)
		return session, nil, nil
	}

	llmConfig := service.GetLLMConfig(agent.LLMConfigID)
	if llmConfig == nil {
		applogger.L.Error("LLM config not found", "config_id", agent.LLMConfigID)
		return session, agent, nil
	}

	return session, agent, llmConfig
}

// getTriggerMessageID extracts the trigger message ID from the work's
// initial event payload.
//   - For EventTypeNewMessage: the user message that triggered this work.
//   - For EventTypeScheduled: the user message that caused the alarm to be set
//     (preserving the causal chain).
func (w *work) getTriggerMessageID() int64 {
	if payload, ok := w.initialPayload.(*eventqueue.NewMessagePayload); ok {
		return payload.MessageID
	}
	if payload, ok := w.initialPayload.(*eventqueue.ScheduledEventPayload); ok {
		return payload.TriggerMessageID
	}
	return 0
}

// handleChatError handles errors during chat processing by committing
// the draft with an error message.
func (w *work) handleChatError() {
	if w.draft != nil {
		w.commitDraft(userFriendlyErrorMsg, model.HasInteractionsNone)
	}
}

// fmtworkID returns a formatted work ID string for logging.
func fmtworkID(workID int64) string {
	return fmt.Sprintf("work-%d", workID)
}
