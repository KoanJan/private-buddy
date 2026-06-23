package tools

import (
	"fmt"
	"time"

	"private-buddy-server/internal/database"
	"private-buddy-server/internal/model"
	"private-buddy-server/internal/service/eventqueue"
	"private-buddy-server/internal/service/llm"

	applogger "private-buddy-server/internal/logger"
)

// triggerAtFormat is the only accepted time format for trigger_at.
// Uses server local time without timezone — the agent and server share
// the same timezone context.
const triggerAtFormat = "2006-01-02 15:04:05"

// WakeMeWhenTool allows the agent to set a future alarm that will wake it up
// at a specified time. This is NOT an OS-level cron/scheduled task — it is
// the agent's self-wake mechanism.
//
// The tool's sole responsibility is:
//  1. Create a ScheduledEvent DB record (status=Pending)
//  2. Send an EventTypeAlarmCreated event through eventqueue
//
// Goroutine management (waiting until trigger_at, firing) is handled by the
// runtime package, which receives the EventTypeAlarmCreated event and registers
// a goroutine. This separation keeps the tool layer thin and avoids circular
// dependencies (runtime → task → tools).
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
			"Use this when someone asks you to remind them or follow up at a specific time. " +
			"\n\n" +
			"CRITICAL: The 'message' parameter is an ACTION INSTRUCTION to your future self, " +
			"NOT a description of what happened. When the alarm fires, you will see this message " +
			"as your primary directive — so write it as a command telling yourself what to DO, " +
			"not as a memo describing why the alarm was set. " +
			"\n\n" +
			"BAD: 'Someone asked to be reminded about the 3pm meeting.' (descriptive — sounds like they are asking again) " +
			"\n" +
			"GOOD: 'Tell the person: it is 3pm, time for the meeting with the design team in Conference Room B.' (actionable — tells you what to say)" +
			"\n\n" +
			"FAST PATH: If the alarm only needs to send a simple message (like a reminder), " +
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
					"description": "Action instruction for your future self when the alarm fires. Write as a COMMAND telling yourself exactly what to DO and SAY. Example: 'Tell the person: you asked me to remind you about the 3pm meeting. It is now time — the meeting is in Conference Room B.' DO NOT write descriptive memos like 'Someone requested a reminder.' This field is always required as a fallback, even when using send_message action.",
				},
				"action": map[string]interface{}{
					"type":        "string",
					"enum":        []string{"send_message", "full_pipeline"},
					"description": "How to handle the alarm when it fires. 'send_message': instantly send action_content without any LLM processing (fast path, best for simple reminders). 'full_pipeline': go through the full LLM pipeline (needed for complex actions like web searches or tool usage). Default is 'full_pipeline' if omitted.",
				},
				"action_content": map[string]interface{}{
					"type":        "string",
					"description": "The exact message to send when the alarm fires. Only used when action='send_message'. This message is delivered instantly without any LLM processing, so write it as the final message that will be seen. Example: '⏰ Reminder: it is 3pm, time for the meeting with the design team in Conference Room B.'",
				},
			},
			"required": []string{"trigger_at", "message"},
		},
	}
}

// Execute creates a ScheduledEvent DB record and sends an EventTypeAlarmCreated
// event through eventqueue. The runtime receives the event and registers a
// goroutine to wait until trigger_at.
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
		applogger.Error("Failed to create scheduled event record",
			"agent_id", w.agentID,
			"session_id", w.sessionID,
			"error", err,
		)
		return "Error: failed to create alarm", nil
	}

	// Notify runtime to register a goroutine for this alarm.
	// The tool does NOT manage goroutines — it only creates the DB record
	// and sends an event. The runtime is responsible for goroutine lifecycle.
	eventqueue.SendEvent(w.agentID, &eventqueue.AgentEvent{
		Type:      eventqueue.EventTypeAlarmCreated,
		SessionID: w.sessionID,
		Payload: &eventqueue.AlarmCreatedPayload{
			ScheduledEventID: event.ID,
		},
	})

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
