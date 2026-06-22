package runtime

import (
	"context"

	"private-buddy-server/internal/database"
	"private-buddy-server/internal/model"
	"private-buddy-server/internal/service"
	"private-buddy-server/internal/service/chat"
	"private-buddy-server/internal/service/eventqueue"
	"private-buddy-server/internal/service/task"

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
//
// After the cognitive order refactoring, work holds a WorkPlan (carrying the
// Decide phase's execution intent as Guidance) and comprehension results
// (carrying the Comprehend phase's understanding). This ensures the execution
// layer has full context without re-interpreting the event.
type work struct {
	ID             int64
	agent          *agentRuntime
	sessionID      int64
	draft          *model.MessageDraft
	plan           WorkPlan // From Decide phase: type + guidance
	maxIterations  int
	initialPayload any                         // The payload from the event that created this work
	comprehension  *ComprehensionResult        // Results from the Comprehend phase
	taskResult     *task.TaskResult            // Task execution result (only set for TaskWork)
	guidanceCh     chan task.GuidanceDirective // Channel for sending guidance/cancel directives to TaskLoop
}

// Run executes the work. After the cognitive order refactoring, the execution
// path is determined by plan.Type:
//   - WorkTypeTask: RunTask (execute with Guidance) only. No reply generation.
//     On completion, signals the event loop, which creates a ChatWork to inform the user.
//   - WorkTypeChat: ExecuteChat (context assembly + LLM response).
//     This is the only path that generates a reply.
//
// Both paths receive comprehension results from the Comprehend phase,
// skipping redundant preprocessing, person state inference, and KB retrieval.
// On completion, commits the draft and signals the event loop to remove this work.
// Respects context cancellation: exits early if the work is cancelled.
func (w *work) Run(ctx context.Context) {
	defer func() {
		// Only transition to Completed if still Running.
		// If abandon() already set Abandoned, this update is a no-op.
		if err := database.DB.Model(&model.Work{}).
			Where("id = ? AND status = ?", w.ID, model.WorkStatusRunning).
			Update("status", model.WorkStatusCompleted).Error; err != nil {
			applogger.L.Error("work: failed to update work status", "work_id", w.ID, "error", err)
		}

		// Send work completed event to the agent's event queue.
		// The agent processes this through the same Comprehend→Decide pipeline
		// as external events, deciding whether to inform the user.
		// This replaces the old workDoneCh + auto-create-ChatWork pattern.
		status := "success"
		var output, taskErr string
		if w.taskResult != nil {
			if w.taskResult.Status != "success" {
				status = "failure"
				taskErr = w.taskResult.Error
			} else {
				output = w.taskResult.Output
			}
		}

		eventqueue.SendEvent(w.agent.agentID, eventqueue.AgentEvent{
			Type:      eventqueue.EventTypeWorkCompleted,
			SessionID: w.sessionID,
			Payload: &eventqueue.WorkCompletedPayload{
				WorkID:           w.ID,
				WorkType:         int(w.plan.Type),
				Guidance:         w.plan.Guidance,
				Status:           status,
				TaskOutput:       output,
				TaskError:        taskErr,
				TriggerMessageID: w.getTriggerMessageID(),
			},
		})
	}()

	applogger.L.Info("work started",
		"work_id", w.ID,
		"session_id", w.sessionID,
		"type", w.plan.Type,
		"guidance", w.plan.Guidance,
	)

	// Check cancellation before starting
	if ctx.Err() != nil {
		applogger.L.Info("work cancelled before pipeline", "work_id", w.ID)
		w.abandon()
		return
	}

	switch w.plan.Type {
	case model.WorkTypeTask:
		w.runTask(ctx)
	default:
		w.runChat(ctx)
	}

	applogger.L.Info("work completed",
		"work_id", w.ID,
		"session_id", w.sessionID,
	)
}

// runTask executes the task path using Guidance from the Decide phase.
// TaskWork is pure execution — it does NOT generate a reply.
// The event loop's workDoneCh handler will create a ChatWork to inform the user.
func (w *work) runTask(ctx context.Context) {
	session, agent, llmConfig := w.loadChatDependencies()
	if session == nil || agent == nil || llmConfig == nil {
		w.abandon()
		return
	}

	triggerMessageID := w.getTriggerMessageID()

	w.taskResult = task.RunTask(task.RunTaskParams{
		LLMConfig:  llmConfig,
		SessionID:  w.sessionID,
		AgentID:    agent.ID,
		UserMsgID:  triggerMessageID,
		WorkID:     w.ID,
		Guidance:   w.plan.Guidance,
		Ctx:        ctx,
		OnNotify:   func(data string) { pushSSEEvent(w.sessionID, data) },
		GuidanceCh: w.guidanceCh,
	})

	applogger.L.Info("TaskWork completed",
		"work_id", w.ID,
		"session_id", w.sessionID,
		"status", w.taskResult.Status,
	)

	// TaskWork does not generate a reply, so discard the draft.
	// The ChatWork created by the event loop will have its own draft.
	w.discardDraft()
}

// discardDraft marks the draft as discarded. Used by TaskWork since it
// does not produce a reply — the draft was only needed for the audit trail.
func (w *work) discardDraft() {
	if w.draft != nil {
		if err := database.DB.Model(&model.MessageDraft{}).Where("id = ?", w.draft.ID).
			Update("status", model.DraftStatusDiscarded).Error; err != nil {
			applogger.L.Warn("work: failed to discard draft", "draft_id", w.draft.ID, "error", err)
		}
	}
}

// FeedGuidance sends a guidance directive to the work's guidance channel.
// This is called when the Decide phase routes an event to an existing
// TaskWork or cancels it — the directive becomes an environment event
// that the TaskLoop observes at the next iteration boundary.
//
// For cancel, the directive carries guidance like "save progress and stop"
// and the reason explaining why. The TaskLoop's LLM processes this and
// decides how to wrap up — this is "appealable" cancellation, not forceful kill.
func (w *work) FeedGuidance(directive task.GuidanceDirective) {
	if w.guidanceCh == nil {
		applogger.L.Warn("FeedGuidance called on work with nil guidanceCh",
			"work_id", w.ID,
		)
		return
	}
	select {
	case w.guidanceCh <- directive:
		applogger.L.Info("Guidance fed to work",
			"work_id", w.ID,
			"guidance", directive.Guidance,
			"reason", directive.Reason,
		)
	default:
		applogger.L.Warn("work guidanceCh full, dropping guidance",
			"work_id", w.ID,
			"guidance", directive.Guidance,
		)
	}
}

// runChat executes the chat path: context assembly + LLM response.
// This is the only path that generates a reply to the user.
func (w *work) runChat(ctx context.Context) {
	session, agent, llmConfig := w.loadChatDependencies()
	if session == nil || agent == nil || llmConfig == nil {
		w.handleChatError()
		return
	}

	triggerMessageID := w.getTriggerMessageID()

	draftID := int64(0)
	if w.draft != nil {
		draftID = w.draft.ID
	}

	var triggerOverride *chat.TriggerOverride
	if payload, ok := w.initialPayload.(*eventqueue.ScheduledEventPayload); ok {
		triggerOverride = &chat.TriggerOverride{
			Type:    chat.TriggerOverrideScheduledAlarm,
			Content: payload.Message,
		}
	}

	// Convert ComprehensionResult to ComprehensionInput for the chat package.
	var comprehensionInput *chat.ComprehensionInput
	if w.comprehension != nil {
		comprehensionInput = &chat.ComprehensionInput{
			PreprocessingResult: w.comprehension.PreprocessingResult,
			PersonState:         w.comprehension.PersonState,
			KBSegments:          w.comprehension.KBSegments,
			Guidance:            w.plan.Guidance,
			TaskResult:          w.taskResult,
		}
	}

	result, err := chat.ExecuteChat(
		ctx, session, agent, llmConfig,
		triggerMessageID, draftID,
		triggerOverride, comprehensionInput,
	)

	if err != nil {
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

	if w.draft != nil {
		w.updateDraftContent(result.Content)
	}

	w.commitDraft(result.Content, result.HasInteractions)
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
	if err := database.DB.Model(&model.MessageDraft{}).Where("id = ?", w.draft.ID).
		Update("content", content).Error; err != nil {
		applogger.L.Warn("work: failed to update draft content", "draft_id", w.draft.ID, "error", err)
	}
}

// abandon marks the work as abandoned and discards the draft.
// This is the fallback mechanism for when the guidance channel fails
// (e.g., TaskLoop unresponsive, channel full) or when the work's context
// is cancelled. Normal cancellation goes through FeedGuidance, allowing
// the TaskLoop's LLM to wrap up gracefully. This method is the safety net.
//
// Directly sets status to Abandoned in DB. The defer in Run() will not
// overwrite it because it only transitions from Running → Completed.
func (w *work) abandon() {
	if err := database.DB.Model(&model.Work{}).Where("id = ?", w.ID).
		Update("status", model.WorkStatusAbandoned).Error; err != nil {
		applogger.L.Error("work: failed to mark work as abandoned", "work_id", w.ID, "error", err)
	}

	if w.draft != nil {
		if err := database.DB.Model(&model.MessageDraft{}).Where("id = ?", w.draft.ID).
			Update("status", model.DraftStatusDiscarded).Error; err != nil {
			applogger.L.Warn("work: failed to discard draft on abandon", "draft_id", w.draft.ID, "error", err)
		}
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
//   - For EventTypeWorkCompleted: the user message that originally triggered
//     the completed work (preserving the causal chain across the
//     TaskWork → ChatWork boundary).
func (w *work) getTriggerMessageID() int64 {
	if payload, ok := w.initialPayload.(*eventqueue.NewMessagePayload); ok {
		return payload.MessageID
	}
	if payload, ok := w.initialPayload.(*eventqueue.ScheduledEventPayload); ok {
		return payload.TriggerMessageID
	}
	if payload, ok := w.initialPayload.(*eventqueue.WorkCompletedPayload); ok {
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
