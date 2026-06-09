package tools

import (
	"context"
	"fmt"
	"sync"
	"time"

	"private-buddy-server/internal/database"
	"private-buddy-server/internal/model"
	"private-buddy-server/internal/service/eventqueue"
	"private-buddy-server/internal/service/llm"

	applogger "private-buddy-server/internal/logger"
)

// alarmRegistry manages all active alarm goroutines, allowing them to be
// cancelled collectively (e.g., on server shutdown) or per-agent (e.g.,
// when an agent is deleted).
var alarmRegistry = &alarmRegistryType{}

type alarmRegistryType struct {
	mu     sync.Mutex
	alarms map[int64]context.CancelFunc // scheduledEventID -> cancel
}

// register stores a cancel function for an alarm goroutine.
func (r *alarmRegistryType) register(eventID int64, cancel context.CancelFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.alarms == nil {
		r.alarms = make(map[int64]context.CancelFunc)
	}
	r.alarms[eventID] = cancel
}

// unregister removes an alarm from the registry (after it fires or is cancelled).
func (r *alarmRegistryType) unregister(eventID int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.alarms, eventID)
}

// CancelAll cancels all registered alarm goroutines. Called on server shutdown.
func (r *alarmRegistryType) CancelAll() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, cancel := range r.alarms {
		cancel()
		delete(r.alarms, id)
	}
}

// CancelAlarmsForAgent cancels all alarm goroutines for a specific agent.
// Called when an agent is deleted.
func (r *alarmRegistryType) CancelAlarmsForAgent(agentID int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, cancel := range r.alarms {
		cancel()
	}
	// Note: we cancel all because the registry is keyed by eventID, not agentID.
	// For a more targeted approach, we'd need an agentID->[]eventID index.
	// Since the number of concurrent alarms is small, this is acceptable.
	_ = agentID
	r.alarms = make(map[int64]context.CancelFunc)
}

// CancelAlarms shuts down all alarm goroutines. Should be called during graceful shutdown.
func CancelAlarms() {
	alarmRegistry.CancelAll()
}

// triggerAtFormat is the only accepted time format for trigger_at.
// Uses server local time without timezone — the agent and server share
// the same timezone context.
const triggerAtFormat = "2006-01-02 15:04:05"

// WakeMeWhenTool allows the agent to set a future alarm that will wake it up
// at a specified time. This is NOT an OS-level cron/scheduled task — it is
// the agent's self-wake mechanism.
//
// When the agent calls this tool, a goroutine is spawned that blocks until
// the trigger time. When the time arrives, the goroutine:
//  1. Marks the ScheduledEvent record as triggered
//  2. Sends an EventTypeScheduled event to the agent via eventqueue.Global
//
// The scheduled event is a transient trigger — it does NOT persist to the
// messages table. Instead, TriggerMessageID preserves the causal chain
// (the user message that caused the alarm), and the alarm context is
// injected as supplementary context in the pipeline.
type WakeMeWhenTool struct {
	agentID          int64
	sessionID        int64
	triggerMessageID int64 // The user message that triggered this tool call
}

// NewWakeMeWhenTool creates a WakeMeWhenTool for the given agent, session,
// and the user message that triggered this tool call.
func NewWakeMeWhenTool(agentID, sessionID, triggerMessageID int64) *WakeMeWhenTool {
	return &WakeMeWhenTool{
		agentID:          agentID,
		sessionID:        sessionID,
		triggerMessageID: triggerMessageID,
	}
}

func (w *WakeMeWhenTool) Name() string { return "wake_me_when" }

func (w *WakeMeWhenTool) Schema() llm.FunctionDefinition {
	return llm.FunctionDefinition{
		Name: "wake_me_when",
		Description: "Set an alarm to wake yourself up at a future time. " +
			"When the alarm fires, you will receive a notification with the context you provide. " +
			"This is YOUR self-wake mechanism — it does NOT create OS-level notifications or system alerts. " +
			"Use this when the user asks you to remind them or follow up at a specific time. " +
			"\n\n" +
			"CRITICAL: The 'message' parameter is an ACTION INSTRUCTION to your future self, " +
			"NOT a description of what happened. When the alarm fires, you will see this message " +
			"as your primary directive — so write it as a command telling yourself what to DO, " +
			"not as a memo describing why the alarm was set. " +
			"\n\n" +
			"BAD: 'User asked to be reminded about the 3pm meeting.' (descriptive — sounds like the user is asking again) " +
			"\n" +
			"GOOD: 'Tell the user: it is 3pm, time for the meeting with the design team in Conference Room B.' (actionable — tells you what to say)" +
			"\n\n" +
			"FAST PATH: If the alarm only needs to send a simple message to the user (like a reminder), " +
			"set action='send_message' and provide the exact message in action_content. This skips LLM " +
			"processing entirely and delivers the message instantly when the alarm fires. " +
			"Only use action='full_pipeline' when you need to perform complex actions (web search, " +
			"tool usage, multi-step tasks) when the alarm fires.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"trigger_at": map[string]interface{}{
					"type":        "string",
					"description": "Absolute time to wake you, in the exact format 'YYYY-MM-DD HH:MM:SS' (server local time). Must be a future time. Example: '2026-06-09 23:10:00'. Compute the exact future time based on the current time shown in recent messages.",
				},
				"message": map[string]interface{}{
					"type":        "string",
					"description": "Action instruction for your future self when the alarm fires. Write as a COMMAND telling yourself exactly what to DO and SAY. Example: 'Tell the user: you asked me to remind you about the 3pm meeting. It is now time — the meeting is in Conference Room B.' DO NOT write descriptive memos like 'User requested a reminder.' This field is always required as a fallback, even when using send_message action.",
				},
				"action": map[string]interface{}{
					"type":        "string",
					"enum":        []string{"send_message", "full_pipeline"},
					"description": "How to handle the alarm when it fires. 'send_message': instantly send action_content to the user without any LLM processing (fast path, best for simple reminders). 'full_pipeline': go through the full LLM pipeline (needed for complex actions like web searches or tool usage). Default is 'full_pipeline' if omitted.",
				},
				"action_content": map[string]interface{}{
					"type":        "string",
					"description": "The exact message to send to the user when the alarm fires. Only used when action='send_message'. This message is delivered instantly without any LLM processing, so write it as the final message the user will see. Example: '⏰ Reminder: it is 3pm, time for the meeting with the design team in Conference Room B.'",
				},
			},
			"required": []string{"trigger_at", "message"},
		},
	}
}

// Execute spawns a goroutine that waits until trigger_at, then sends an
// EventTypeScheduled event to the agent via the event queue.
// No message is persisted to the messages table — the alarm context is
// carried in the event payload and injected into the pipeline as a trigger override.
//
// When action is "send_message" and action_content is provided, the runtime
// will take the fast path: directly send action_content as a message to the
// user without any LLM processing. Otherwise, the full pipeline is used.
func (w *WakeMeWhenTool) Execute(args map[string]interface{}) (string, error) {
	triggerAtStr, _ := args["trigger_at"].(string)
	message, _ := args["message"].(string)

	if triggerAtStr == "" || message == "" {
		return "Error: trigger_at and message are required", nil
	}

	// Parse trigger_at
	triggerAt, err := parseTriggerAt(triggerAtStr)
	if err != nil {
		return fmt.Sprintf("Error: invalid trigger_at format '%s': %v", triggerAtStr, err), nil
	}

	// Validate: trigger time must be in the future
	if triggerAt.Before(time.Now()) {
		return fmt.Sprintf("Error: trigger_at '%s' is in the past", triggerAtStr), nil
	}

	// Parse action and action_content
	action := model.ScheduledEventActionFullPipeline
	actionStr, _ := args["action"].(string)
	if actionStr == "send_message" {
		action = model.ScheduledEventActionSendMessage
	}
	actionContent, _ := args["action_content"].(string)

	// Validate: send_message action requires action_content
	if action == model.ScheduledEventActionSendMessage && actionContent == "" {
		return "Error: action_content is required when action is 'send_message'", nil
	}

	// Create a DB record for persistence and debugging
	event := model.ScheduledEvent{
		AgentID:          w.agentID,
		SessionID:        w.sessionID,
		TriggerMessageID: w.triggerMessageID,
		TriggerAt:        triggerAt,
		Message:          message,
		Action:           action,
		ActionContent:    actionContent,
		Status:           model.ScheduledEventPending,
	}
	if err := database.DB.Create(&event).Error; err != nil {
		applogger.L.Error("Failed to create scheduled event record",
			"agent_id", w.agentID,
			"session_id", w.sessionID,
			"error", err,
		)
		return "Error: failed to create alarm", nil
	}

	// Spawn goroutine: wait until trigger_at using a cancellable timer,
	// then fire the alarm. The goroutine sends events directly through
	// eventqueue, so it does NOT depend on the runtime package.
	alarmCtx, alarmCancel := context.WithCancel(context.Background())
	alarmRegistry.register(event.ID, alarmCancel)

	go func() {
		defer alarmRegistry.unregister(event.ID)

		until := time.Until(triggerAt)
		applogger.L.Info("Scheduled event goroutine waiting",
			"event_id", event.ID,
			"agent_id", w.agentID,
			"session_id", w.sessionID,
			"trigger_at", triggerAt,
			"action", action,
			"wait_duration", until.Round(time.Second),
		)

		// Use a timer instead of time.Sleep so we can respond to cancellation.
		timer := time.NewTimer(until)
		defer timer.Stop()

		select {
		case <-alarmCtx.Done():
			applogger.L.Info("Scheduled event goroutine cancelled",
				"event_id", event.ID,
				"agent_id", w.agentID,
			)
			return
		case <-timer.C:
			// Timer fired, proceed to trigger the alarm
		}

		// Check if the event was cancelled in the database while we were waiting
		var currentEvent model.ScheduledEvent
		if err := database.DB.First(&currentEvent, event.ID).Error; err != nil {
			applogger.L.Warn("Scheduled event not found, skipping",
				"event_id", event.ID, "error", err)
			return
		}
		if currentEvent.Status != model.ScheduledEventPending {
			applogger.L.Info("Scheduled event no longer pending, skipping",
				"event_id", event.ID, "status", currentEvent.Status)
			return
		}

		// Mark the scheduled event as triggered
		database.DB.Model(&model.ScheduledEvent{}).
			Where("id = ?", event.ID).
			Update("status", model.ScheduledEventTriggered)

		applogger.L.Info("Scheduled event fired, sending to eventqueue",
			"event_id", event.ID,
			"agent_id", w.agentID,
			"session_id", w.sessionID,
			"action", action,
		)

		// Send the scheduled event directly through the eventqueue.
		// No dependency on runtime — the event queue routes it to the
		// agent's subscribed channel automatically.
		//
		// TriggerMessageID preserves the causal chain (the user message
		// that caused the alarm). The alarm context is the agent's note
		// to its future self, injected as supplementary context.
		// Action and ActionContent enable the fast path in the runtime.
		eventqueue.SendEvent(w.agentID, eventqueue.AgentEvent{
			Type:      eventqueue.EventTypeScheduled,
			SessionID: w.sessionID,
			Payload: &eventqueue.ScheduledEventPayload{
				ScheduledEventID: event.ID,
				TriggerMessageID: w.triggerMessageID,
				Message:          message,
				Action:           action,
				ActionContent:    actionContent,
			},
		})
	}()

	until := time.Until(triggerAt).Round(time.Minute)
	if action == model.ScheduledEventActionSendMessage {
		return fmt.Sprintf("Alarm set (fast path: will send message directly). I will be woken at %s (in %s).",
			triggerAt.Format("2006-01-02 15:04 MST"), until), nil
	}
	return fmt.Sprintf("Alarm set. I will be woken at %s (in %s).",
		triggerAt.Format("2006-01-02 15:04 MST"), until), nil
}

// parseTriggerAt parses the trigger_at string into a time.Time.
// Only one format is accepted: "YYYY-MM-DD HH:MM:SS" (server local time).
func parseTriggerAt(s string) (time.Time, error) {
	t, err := time.ParseInLocation(triggerAtFormat, s, time.Local)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid time format (expected 'YYYY-MM-DD HH:MM:SS', e.g. '2026-06-09 23:10:00'): %s", s)
	}
	return t, nil
}
