// Package taskcontext manages the agent's internal message history within a task execution.
//
// This package implements the "fixed part + dynamic part" architecture with
// iteration window control for the task loop's context management.
//
// Fixed part (always fully included):
//   - system prompt: basic rules + context information
//   - Task content: task requirements (system-managed)
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
)

// ContextManager manages the internal message history for a single task execution.
//
// Messages follow the OpenAI chat completion format:
//   - system: { role, content }
//   - user: { role, content }
//   - assistant: { role, content } or { role, tool_calls }
//   - tool: { role, tool_call_id, content }
//
// Window applies to the dynamic part only. The fixed part
// (system prompt with context info, Task, Notes) is always
// fully included because these are essential prerequisites for
// the agent's work.
type ContextManager struct {
	systemPrompt    string                     // Static system prompt (basic rules)
	iterationWindow int                        // Number of recent iterations to keep visible
	taskContent     string                     // Full content of task requirements
	notesContent    string                     // Full content of agent's notes
	totalIterations int                        // Total iterations accumulated
	dynamicMessages [][]map[string]interface{} // Groups of (assistant_msg + tool_results) per iteration
}

// NewContextManager creates a new ContextManager.
// Context information will be appended to the system prompt at build time,
// so the agent always sees it as part of the system-level instructions.
func NewContextManager(systemPrompt string, iterationWindow int, taskContent, notesContent string) *ContextManager {
	return &ContextManager{
		systemPrompt:    systemPrompt,
		iterationWindow: iterationWindow,
		taskContent:     taskContent,
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
func (cm *ContextManager) AddIteration(assistantMsg map[string]interface{}, toolResults []map[string]interface{}) {
	group := []map[string]interface{}{assistantMsg}
	group = append(group, toolResults...)
	cm.dynamicMessages = append(cm.dynamicMessages, group)
	cm.totalIterations++
}

// BuildMessages assembles the final message list for LLM call.
//
// Order:
//  1. system prompt (basic rules + context information)
//  2. user: Task content
//  3. user: Notes content
//  4. dynamic messages (recent iterations within window)
//
// Window applies to dynamic part only; fixed part is always fully included.
func (cm *ContextManager) BuildMessages() []map[string]interface{} {
	window := cm.iterationWindow
	var visible [][]map[string]interface{}
	if len(cm.dynamicMessages) > window {
		visible = cm.dynamicMessages[len(cm.dynamicMessages)-window:]
	} else {
		visible = cm.dynamicMessages
	}
	visibleIterations := len(visible)
	invisibleIterations := cm.totalIterations - visibleIterations

	fullSystemPrompt := cm.buildFullSystemPrompt(visibleIterations, invisibleIterations, len(cm.notesContent))

	messages := []map[string]interface{}{
		{"role": "system", "content": fullSystemPrompt},
		{"role": "user", "content": fmt.Sprintf("[Task]\n%s", cm.taskContent)},
		{"role": "user", "content": fmt.Sprintf("[Your Notes]\n%s", cm.notesContent)},
	}

	for _, group := range visible {
		for _, msg := range group {
			messages = append(messages, msg)
		}
	}

	return messages
}

// buildFullSystemPrompt builds the full system prompt by merging static rules
// with dynamic context information.
//
// Context information is appended to the system prompt so the agent always sees
// it as system-level instructions rather than a separate user message that may
// be overlooked. Includes:
//   - Working memory limit and visible iteration count
//   - Notes size warning if approaching limit (>80%)
//   - Instructions for understanding current project state
//   - NOTES usage guide (entry types and best practices)
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

	contextParts = append(contextParts,
		"",
		"[Understanding Current State]",
		"To understand the current project state:",
		"- Use 'ls -la' to see files in your working directory",
		"- Use 'cat <filename>' to read file contents",
		"- Use 'find . -type f' to discover all files",
		"- Check your NOTES (provided above) for previous progress",
		"",
		"[NOTES Usage Guide]",
		"The write_notes tool appends structured entries to your notes.",
		"",
		"Entry types:",
		"- observation: Something you discovered",
		"- decision: A choice you made (explain why)",
		"- finding: A key result or conclusion",
		"- correction: A fix to a previous entry (use conflicts_with)",
		"- progress: Current status and next steps",
		"",
		"Best practices:",
		"- Each entry is APPENDED, not overwritten",
		"- Write CONCISE entries — notes have a size limit",
		"- Only write IMPORTANT information — skip trivial or obvious facts",
		"- Ask: would losing this information hurt the task? If not, skip it",
		"- Include file references when relevant",
		"- Use conflicts_with when correcting earlier decisions",
		"- Write self-contained entries (future LLM calls have no memory)",
	)

	return cm.systemPrompt + strings.Join(contextParts, "\n")
}
