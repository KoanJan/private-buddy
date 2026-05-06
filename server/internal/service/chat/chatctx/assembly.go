// Package context implements the context engineering pipeline for chat processing.
//
// This package provides the context assembly services that build the LLM message
// sequence from various context sources: summaries, narratives, retrieval results,
// user state, and task results. It matches Python's chat/context module.
package chatctx

import (
	"fmt"
	"strings"

	"private-buddy-server/internal/service/llm"

	applogger "private-buddy-server/internal/logger"
)

// Template for full context with background story, segments, and character settings.
// Uses narrative-style section headers instead of bracketed labels:
//   - "Background context from earlier" and "Recent conversation" create temporal flow
//   - Segments section uses narrative transition to reduce abruptness
//   - Metadata (message numbers) preserved for debugging and context clarity
//   - User state placed in instruction area (after narrative, before response directive)
//     to preserve narrative flow while guiding response strategy
const oneBigMessageTemplate = `%sBackground context from earlier in the conversation (messages 1-%d):

%s

%s---

Recent conversation (messages %d-%d):

%s

---

%s%sPlease respond directly to the user. Do not use parenthetical action descriptions or non-verbal content.`

// Template for simple context without background story (V < N case).
// Used when there are not enough messages to generate a summary.
const oneBigMessageNoStoryTemplate = `%sConversation record (messages %d-%d):

%s

---

%s%sPlease respond directly to the user. Do not use parenthetical action descriptions or non-verbal content.`

// TaskResultForAssembly represents the task execution result for context assembly.
// Mirrors Python's TaskResult DTO used in context assembly.
// Status is "success" or "failure"; Result/Reason/Notes are populated accordingly.
type TaskResultForAssembly struct {
	Status string  `json:"status"`
	Result *string `json:"result"`
	Reason *string `json:"reason"`
	Notes  *string `json:"notes"`
}

// ContextAssemblyService assembles context components into LLM-ready messages.
//
// This service implements the "one big message" pattern where character settings,
// background story (cached narrative), and recent messages are combined into
// a single prompt. The assembly process includes:
//   - Character settings (personality, style, identity) integration
//   - Background story (cached narrative) formatting with metadata
//   - Relevant segments as independent section with narrative transition
//   - Recent message formatting with sequence numbers
//   - User state injection as natural language in instruction area
//   - Agent delivery integration for world-interaction results
//
// Performance Optimization (0.0.7):
//   - Background story is now a cached narrative (generated in background with summary)
//   - Segments are no longer fused into narrative; they appear as an independent section
//   - This eliminates the 40-66s real-time narrative generation bottleneck
type ContextAssemblyService struct{}

func NewContextAssemblyService() *ContextAssemblyService {
	return &ContextAssemblyService{}
}

// formatCharacterSection formats character settings section for the prompt.
// Returns "[Your Character]\n{settings}\n\n---\n\n" or empty string if nil/empty.
func (cas *ContextAssemblyService) formatCharacterSection(characterSettings *string) string {
	if characterSettings == nil || *characterSettings == "" {
		return ""
	}
	return fmt.Sprintf("[Your Character]\n%s\n\n---\n\n", *characterSettings)
}

// formatSegmentsSection formats relevant segments as an independent section.
// Segments are RAG-retrieved historical fragments placed with narrative transition,
// since they could not be fused into the pre-generated cached narrative.
// Returns "Some additional details from earlier conversations...\n{items}\n\n" or empty string.
func (cas *ContextAssemblyService) formatSegmentsSection(relevantSegments []map[string]interface{}) string {
	if len(relevantSegments) == 0 {
		return ""
	}

	var segmentsText []string
	for _, seg := range relevantSegments {
		if content, ok := seg["content"].(string); ok {
			segmentsText = append(segmentsText, fmt.Sprintf("- %s", content))
		}
	}

	if len(segmentsText) == 0 {
		return ""
	}

	return fmt.Sprintf("Some additional details from earlier conversations that may be relevant:\n\n%s\n\n", strings.Join(segmentsText, "\n"))
}

// formatUserStateInstruction formats user state as natural language instruction.
// Placed in the instruction area (after narrative, before response directive)
// to preserve narrative flow while guiding response strategy.
// Returns "{description}\nAdjust your response tone, detail level, and strategy accordingly.\n\n" or empty string.
func (cas *ContextAssemblyService) formatUserStateInstruction(userStateDescription *string) string {
	if userStateDescription == nil || *userStateDescription == "" {
		return ""
	}
	return fmt.Sprintf("%s\nAdjust your response tone, detail level, and strategy accordingly.\n\n", *userStateDescription)
}

// formatTaskResultSection formats agent delivery section for the prompt.
// Provides execution status and results for LLM to formulate response:
//   - success: "[Task Execution Result]\nThe following task was completed successfully:\n\n{result}\n\n---\n\n"
//   - failure: "[Task Execution Interrupted]\nThe task could not be completed.\n\nReason: {reason}\n\n---\n\n"
func (cas *ContextAssemblyService) formatTaskResultSection(taskResult *TaskResultForAssembly) string {
	if taskResult == nil {
		return ""
	}

	if taskResult.Status == "success" {
		result := "Task completed."
		if taskResult.Result != nil {
			result = *taskResult.Result
		}
		return fmt.Sprintf("[Task Execution Result]\nThe following task was completed successfully:\n\n%s\n\n---\n\n", result)
	}

	notesSection := ""
	if taskResult.Notes != nil {
		notesSection = fmt.Sprintf("\n\nProgress notes:\n%s", *taskResult.Notes)
	}

	reason := "Unknown error"
	if taskResult.Reason != nil {
		reason = *taskResult.Reason
	}

	return fmt.Sprintf("[Task Execution Interrupted]\nThe task could not be completed.\n\nReason: %s%s\n\n---\n\n", reason, notesSection)
}

// AssembleContext assembles context into one big message for LLM processing.
//
// This method combines character settings, background story (cached narrative),
// relevant segments, and recent messages into a unified message format.
//
// The background story is a cached narrative generated in background alongside
// the summary. Segments are RAG-retrieved fragments placed as an independent
// section with narrative transition, since they could not be fused into the
// pre-generated narrative.
//
// Parameters:
//   - characterSettings: agent's personality, style, and identity settings
//   - backgroundStory: cached narrative from summary record
//   - recentMessages: recent completed messages (including trigger_message as the latest)
//   - relevantSegments: RAG-retrieved historical segments (independent section)
//   - summaryVersion: version number of the summary (covers messages 1 to summaryVersion)
//   - recentStart: starting message sequence number for recent messages
//   - recentEnd: ending message sequence number for recent messages
//   - userStateDescription: natural language description of inferred user state,
//     placed in instruction area to guide response strategy
//   - taskResult: agent execution result for world-interaction tasks,
//     provides execution status and results for LLM to formulate response
func (cas *ContextAssemblyService) AssembleContext(
	characterSettings *string,
	backgroundStory *string,
	recentMessages []map[string]interface{},
	relevantSegments []map[string]interface{},
	summaryVersion *int,
	recentStart int,
	recentEnd int,
	userStateDescription *string,
	taskResult *TaskResultForAssembly,
) []llm.ChatMessage {
	characterSection := cas.formatCharacterSection(characterSettings)
	userStateInstruction := cas.formatUserStateInstruction(userStateDescription)
	taskResultSection := cas.formatTaskResultSection(taskResult)

	var dialogLines []string
	for _, msg := range recentMessages {
		role := "User"
		if r, ok := msg["role"].(string); ok && r != "user" {
			role = "You"
		}
		content, _ := msg["content"].(string)
		dialogLines = append(dialogLines, fmt.Sprintf("%s: %s", role, content))
	}
	dialogSection := strings.Join(dialogLines, "\n")

	var oneBigMessage string
	if backgroundStory != nil && *backgroundStory != "" && summaryVersion != nil {
		segmentsSection := cas.formatSegmentsSection(relevantSegments)
		oneBigMessage = fmt.Sprintf(oneBigMessageTemplate,
			characterSection,
			*summaryVersion,
			*backgroundStory,
			segmentsSection,
			recentStart,
			recentEnd,
			dialogSection,
			taskResultSection,
			userStateInstruction,
		)
	} else {
		oneBigMessage = fmt.Sprintf(oneBigMessageNoStoryTemplate,
			characterSection,
			recentStart,
			recentEnd,
			dialogSection,
			taskResultSection,
			userStateInstruction,
		)
	}

	messages := []llm.ChatMessage{
		{Role: "user", Content: oneBigMessage},
	}

	applogger.L.Info("Assembled context",
		"message_count", len(messages),
		"has_user_state", userStateDescription != nil,
		"has_task_result", taskResult != nil,
		"segments", len(relevantSegments),
	)

	return messages
}
