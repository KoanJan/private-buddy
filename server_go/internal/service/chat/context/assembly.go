package context

import (
	"fmt"
	"strings"

	"private-buddy-server/internal/service/llm"

	applogger "private-buddy-server/internal/logger"
)

const oneBigMessageTemplate = `%sBackground context from earlier in the conversation (messages 1-%d):

%s

%s---

Recent conversation (messages %d-%d):

%s

---

%s%sPlease respond directly to the user. Do not use parenthetical action descriptions or non-verbal content.`

const oneBigMessageNoStoryTemplate = `%sConversation record (messages %d-%d):

%s

---

%s%sPlease respond directly to the user. Do not use parenthetical action descriptions or non-verbal content.`

// TaskResultForAssembly represents the task execution result for context assembly.
type TaskResultForAssembly struct {
	Status string  `json:"status"`
	Result *string `json:"result"`
	Reason *string `json:"reason"`
	Notes  *string `json:"notes"`
}

// ContextAssemblyService assembles context components into LLM-ready messages.
type ContextAssemblyService struct{}

func NewContextAssemblyService() *ContextAssemblyService {
	return &ContextAssemblyService{}
}

// formatCharacterSection formats character settings section for the prompt.
func (cas *ContextAssemblyService) formatCharacterSection(characterSettings *string) string {
	if characterSettings == nil || *characterSettings == "" {
		return ""
	}
	return fmt.Sprintf("[Your Character]\n%s\n\n---\n\n", *characterSettings)
}

// formatSegmentsSection formats relevant segments as an independent section.
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
func (cas *ContextAssemblyService) formatUserStateInstruction(userStateDescription *string) string {
	if userStateDescription == nil || *userStateDescription == "" {
		return ""
	}
	return fmt.Sprintf("%s\nAdjust your response tone, detail level, and strategy accordingly.\n\n", *userStateDescription)
}

// formatTaskResultSection formats agent delivery section for the prompt.
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
