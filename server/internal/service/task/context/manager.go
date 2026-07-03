// Package taskcontext manages the agent's internal message history within a task execution.
//
// This package implements the "fixed part + dynamic part" architecture with
// iteration window control for the task loop's context management.
//
// Fixed part (always fully included):
//   - system prompt: basic rules + context information
//   - Notes content: agent's structured working notes (system-managed)
//
// Dynamic part (window-controlled):
//   - Recent interaction rounds (assistant + tool messages)
//   - Only the last w iterations are visible to the LLM
//   - Older iterations are discarded from context
//
// Context information is merged into the system prompt so the agent
// always sees it as top-level instructions.
package taskcontext

import (
	"fmt"
	"strings"

	"private-buddy-server/internal/config"
	"private-buddy-server/internal/service/llm"
)

// ContextManager manages the internal message history for a single task execution.
//
// Window applies to the dynamic part only. The fixed part
// (system prompt with context info, Notes) is always
// fully included because these are essential prerequisites for
// the agent's work.
type ContextManager struct {
	systemPrompt    string          // Static system prompt (basic rules)
	iterationWindow int             // Number of recent iterations to keep visible
	notesContent    string          // Full content of agent's notes
	totalIterations int             // Total iterations accumulated
	dynamicMessages [][]llm.Message // Groups of (assistant_msg + tool_results) per iteration
}

// NewContextManager creates a new ContextManager.
// Context information will be appended to the system prompt at build time,
// so the agent always sees it as part of the system-level instructions.
func NewContextManager(systemPrompt string, iterationWindow int, notesContent string) *ContextManager {
	return &ContextManager{
		systemPrompt:    systemPrompt,
		iterationWindow: iterationWindow,
		notesContent:    notesContent,
	}
}

// IterationWindow returns the iteration window size.
func (cm *ContextManager) IterationWindow() int {
	return cm.iterationWindow
}

// RefreshNotes updates notes content (agent may have appended via write_notes tool).
func (cm *ContextManager) RefreshNotes(newNotesContent string) {
	cm.notesContent = newNotesContent
}

// AddIteration adds a complete iteration (assistant message + tool results).
//
// An iteration is a group of messages that must be kept together
// to maintain conversation coherence. The assistant message and
// its associated tool results are always included or excluded as a unit.
func (cm *ContextManager) AddIteration(assistantMsg llm.Message, toolResults []llm.Message) {
	group := []llm.Message{assistantMsg}
	group = append(group, toolResults...)
	cm.dynamicMessages = append(cm.dynamicMessages, group)
	cm.totalIterations++
}

// BuildMessages assembles the final message list for LLM call.
//
// Order:
//  1. system prompt (basic rules + context information)
//  2. user: Notes content
//  3. dynamic messages (recent iterations within window)
//
// Window applies to dynamic part only; fixed part is always fully included.
func (cm *ContextManager) BuildMessages() []llm.Message {
	window := cm.iterationWindow
	var visible [][]llm.Message
	if len(cm.dynamicMessages) > window {
		visible = cm.dynamicMessages[len(cm.dynamicMessages)-window:]
	} else {
		visible = cm.dynamicMessages
	}
	visibleIterations := len(visible)
	invisibleIterations := cm.totalIterations - visibleIterations

	fullSystemPrompt := cm.buildFullSystemPrompt(visibleIterations, invisibleIterations, len(cm.notesContent))

	messages := []llm.Message{
		{Role: "system", Content: fullSystemPrompt},
		{Role: "user", Content: fmt.Sprintf("[Your Notes]\n%s", cm.notesContent)},
	}

	for _, group := range visible {
		for _, msg := range group {
			messages = append(messages, msg)
		}
	}

	return messages
}

// buildFullSystemPrompt appends dynamic context information to the static
// system prompt. Only truly dynamic values (iteration counts, notes length)
// are included here so that the static prefix remains stable across iterations,
// preserving LLM provider prefix caching.
//
// Static instruction blocks ([Understanding Current State], [NOTES Usage Guide],
// [Critical Identifiers]) are part of the static system prompt built at task
// start — see task.buildSystemPrompt.
func (cm *ContextManager) buildFullSystemPrompt(visibleIterations, invisibleIterations, notesLength int) string {
	settings := config.Get()
	notesMaxChars := settings.NotesMaxChars

	contextParts := []string{
		"",
		"[Context Information]",
		fmt.Sprintf("Your working memory is limited. You can see the last %d iterations.", cm.iterationWindow),
		fmt.Sprintf("This task has produced %d iterations total, %d of which are outside your visible range.", cm.totalIterations, invisibleIterations),
		"",
		fmt.Sprintf("Your NOTES are currently %d chars (max: %d chars).", notesLength, notesMaxChars),
	}

	if notesLength > int(float64(notesMaxChars)*0.8) {
		contextParts = append(contextParts, "WARNING: Your NOTES are approaching the size limit. Consider consolidating older entries.")
	}

	return cm.systemPrompt + strings.Join(contextParts, "\n")
}
