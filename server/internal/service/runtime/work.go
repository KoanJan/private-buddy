package runtime

import (
	"context"
	"fmt"
	"time"

	"private-buddy-server/internal/database"
	"private-buddy-server/internal/model"
	"private-buddy-server/internal/service"
	"private-buddy-server/internal/service/chat"

	applogger "private-buddy-server/internal/logger"
)

// Work represents a unit of work for an agent within a session.
// It is created when an agent decides to act on an event, and it may
// absorb subsequent events (e.g., user corrections) during its execution.
//
// Three-layer model: Agent (long-lived) → Work (coherent goal) → Iteration (atomic ReAct step)
//
// Work unifies the Chat path (single LLM call) and Task path (ReAct loop):
//   - Chat Work: single LLM call, absorbs events before context assembly
//   - Task Work: ReAct loop, absorbs events at each iteration boundary
type Work struct {
	ID                 int64
	agent              *AgentRuntime
	sessionID          int64
	draft              *model.MessageDraft
	workType           int
	description        string
	maxIterations      int
	pendingEvents      chan AgentEvent
	initialPayload     any // The payload from the event that created this Work
	ctx                context.Context
	cancel             context.CancelFunc
	absorbedMessageIDs []int64 // Message IDs absorbed during execution (for merged reply)
}

// Run executes the work. For chat-type work, this runs the existing
// chat pipeline. For task-type work, this runs the ReAct loop.
// On completion, commits the draft and signals the event loop to remove this work.
func (w *Work) Run() {
	defer func() {
		// Signal event loop to remove this work
		w.agent.workDoneCh <- w

		// Update work status in database
		database.DB.Model(&model.Work{}).Where("id = ?", w.ID).
			Update("status", model.WorkStatusCompleted)
	}()

	applogger.L.Info("Work started",
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
	triggerMessageID := w.getTriggerMessageID()
	if len(w.absorbedMessageIDs) > 0 {
		triggerMessageID = w.absorbedMessageIDs[len(w.absorbedMessageIDs)-1]
		applogger.L.Info("Work using absorbed message as trigger",
			"work_id", w.ID,
			"original_trigger", w.getTriggerMessageID(),
			"new_trigger", triggerMessageID,
			"absorbed_count", len(w.absorbedMessageIDs),
		)
	}
	if triggerMessageID == 0 {
		applogger.L.Error("No trigger message ID for work", "work_id", w.ID)
		w.handleChatError()
		return
	}

	// Run the chat pipeline with draft-based architecture
	ctx := context.Background()
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

	result, err := chat.Process(ctx, session, agent, llmConfig, triggerMessageID, contextBoundary, draftID, callbacks)
	if err != nil {
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

	applogger.L.Info("Work completed",
		"work_id", w.ID,
		"session_id", w.sessionID,
		"draft_id", draftID,
	)
}

// FeedEvent feeds an event to the work's pending events channel.
// Non-blocking: drops the event if the channel is full.
func (w *Work) FeedEvent(event AgentEvent) {
	select {
	case w.pendingEvents <- event:
		applogger.L.Debug("Event fed to work",
			"work_id", w.ID,
			"event_type", event.Type,
		)
	default:
		applogger.L.Warn("Work pending events channel full, dropping event",
			"work_id", w.ID,
			"event_type", event.Type,
		)
	}
}

// absorbPendingEvents drains all pending events from the channel.
// Called at each iteration boundary — the Work voluntarily checks for
// new input, like a human checking for new instructions between steps.
func (w *Work) absorbPendingEvents() {
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
func (w *Work) handleEvent(event AgentEvent) {
	if payload, ok := event.Payload.(*NewMessagePayload); ok {
		w.absorbedMessageIDs = append(w.absorbedMessageIDs, payload.MessageID)
		applogger.L.Info("Work absorbed message",
			"work_id", w.ID,
			"message_id", payload.MessageID,
			"total_absorbed", len(w.absorbedMessageIDs),
		)
	} else {
		applogger.L.Info("Work absorbing event",
			"work_id", w.ID,
			"event_type", event.Type,
		)
	}
}

// commitDraft commits the draft by sending it through the serialized commit channel.
// The commit handler creates a message from the draft content and pushes it to SSE clients.
func (w *Work) commitDraft(content string, hasInteractions int) {
	if w.draft == nil {
		applogger.L.Error("Work.commitDraft called with nil draft", "work_id", w.ID)
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
func (w *Work) updateDraftContent(content string) {
	if w.draft == nil {
		return
	}
	w.draft.Content = content
	database.DB.Model(&model.MessageDraft{}).Where("id = ?", w.draft.ID).
		Update("content", content)
}

// abandon marks the work and its draft as abandoned.
func (w *Work) abandon() {
	database.DB.Model(&model.Work{}).Where("id = ?", w.ID).
		Update("status", model.WorkStatusAbandoned)

	if w.draft != nil {
		database.DB.Model(&model.MessageDraft{}).Where("id = ?", w.draft.ID).
			Update("status", model.DraftStatusDiscarded)
	}
}

// loadChatDependencies loads session, agent, and LLM config from the database.
func (w *Work) loadChatDependencies() (*model.Session, *model.Agent, *model.LLMConfig) {
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
func (w *Work) getTriggerMessageID() int64 {
	if payload, ok := w.initialPayload.(*NewMessagePayload); ok {
		return payload.MessageID
	}
	return 0
}

// handleChatError handles errors during chat processing by committing
// the draft with an error message.
func (w *Work) handleChatError() {
	if w.draft != nil {
		w.commitDraft(userFriendlyErrorMsg, model.HasInteractionsNone)
	}
}

const userFriendlyErrorMsg = "抱歉，服务器遇到了一些问题，请稍后再试。"

// NewMessagePayload is the payload type for EventTypeNewMessage events.
type NewMessagePayload struct {
	MessageID      int64
	MessageContent string
}

// GetMessageContent returns the message content for description extraction.
func (p *NewMessagePayload) GetMessageContent() string {
	return p.MessageContent
}

// RecoverActiveWorks loads active works from the database for agent recovery
// after a service restart. Returns Work objects ready to be added to activeWorks.
func RecoverActiveWorks(agentID int64, runtime *AgentRuntime) []*Work {
	var workRecords []model.Work
	database.DB.Where("agent_id = ? AND status = ?", agentID, model.WorkStatusRunning).Find(&workRecords)

	for _, wr := range workRecords {
		// Mark recovered works as abandoned since we can't resume mid-execution
		database.DB.Model(&model.Work{}).Where("id = ?", wr.ID).
			Update("status", model.WorkStatusAbandoned)

		if wr.DraftID != nil {
			database.DB.Model(&model.MessageDraft{}).Where("id = ?", *wr.DraftID).
				Update("status", model.DraftStatusDiscarded)
		}

		applogger.L.Info("Recovered work marked as abandoned",
			"work_id", wr.ID,
			"agent_id", agentID,
		)
	}

	return nil
}

// StartAgentRuntime creates and starts an AgentRuntime for the given agent.
func StartAgentRuntime(
	agentID int64,
	onStatusChange func(agentID, sessionID int64, status int),
) *AgentRuntime {
	router := NewSemanticWorkRouter()
	runtime := NewAgentRuntime(agentID, router, 30*time.Second, onStatusChange)

	// Recover any active works from previous run
	RecoverActiveWorks(agentID, runtime)

	ctx, cancel := context.WithCancel(context.Background())
	_ = cancel // Will be used for graceful shutdown in future iterations

	go runtime.Run(ctx)

	applogger.L.Info("AgentRuntime started", "agent_id", agentID)
	return runtime
}

// fmtWorkID returns a formatted work ID string for logging.
func fmtWorkID(workID int64) string {
	return fmt.Sprintf("work-%d", workID)
}
