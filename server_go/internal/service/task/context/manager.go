package taskcontext

import (
	"fmt"
	"strings"

	"private-buddy-server/internal/config"
)

type ContextManager struct {
	systemPrompt    string
	iterationWindow int
	taskContent     string
	notesContent    string
	totalIterations int
	dynamicMessages [][]map[string]interface{}
}

func NewContextManager(systemPrompt string, iterationWindow int, taskContent, notesContent string) *ContextManager {
	return &ContextManager{
		systemPrompt:    systemPrompt,
		iterationWindow: iterationWindow,
		taskContent:     taskContent,
		notesContent:    notesContent,
	}
}

func (cm *ContextManager) IterationWindow() int {
	return cm.iterationWindow
}

func (cm *ContextManager) RefreshNotes(newNotesContent string) {
	cm.notesContent = newNotesContent
}

func (cm *ContextManager) AddIteration(assistantMsg map[string]interface{}, toolResults []map[string]interface{}) {
	group := []map[string]interface{}{assistantMsg}
	group = append(group, toolResults...)
	cm.dynamicMessages = append(cm.dynamicMessages, group)
	cm.totalIterations++
}

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
