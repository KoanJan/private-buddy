package runtime

import (
	"context"
	"fmt"
	"strings"

	"private-buddy-server/internal/database"
	"private-buddy-server/internal/model"
	"private-buddy-server/internal/service/comprehend"
	"private-buddy-server/internal/service/eventqueue"
	"private-buddy-server/internal/service/kb"
	"private-buddy-server/internal/service/llm"

	applogger "private-buddy-server/internal/logger"
)

// ComprehensionResult holds the outcome of the comprehension phase.
// It represents the agent's understanding of the incoming event,
// including what the other party means and what information is relevant.
//
// Comprehend only collects information — it does not make judgments.
// NeedsWorldInteraction is an objective property of the message
// ("does this message involve tools/external data?"), not a decision
// on how to respond.
type ComprehensionResult struct {
	// ProcessedQuery is the rewritten query for better retrieval.
	// Empty if preprocessing was skipped.
	ProcessedQuery string

	// QueryType is the classified query type from preprocessing
	// (clear, ambiguous, vague, no_query).
	QueryType string

	// NeedsWorldInteraction indicates whether the message requires
	// interaction with the external world (tools, real-time data,
	// file operations). This is an objective property of the message,
	// not a decision on how to respond.
	NeedsWorldInteraction bool

	// NeedsClarification indicates the query is too vague and needs
	// a clarification question before proceeding.
	NeedsClarification bool

	// Clarification contains the generated clarification question
	// when NeedsClarification is true.
	Clarification string

	// SkipRetrieval indicates that RAG retrieval should be skipped
	// (e.g., for greetings, chitchat).
	SkipRetrieval bool

	// PreprocessingResult holds the original preprocessing output,
	// preserved for downstream consumers (e.g., chat execution)
	// that need the full structured result.
	PreprocessingResult *comprehend.PreprocessingResult

	// KBSegments holds knowledge base retrieval results.
	KBSegments []comprehend.Segment

	// PersonState holds the inferred state of the other party
	// (emotion, purpose, situation).
	PersonState *comprehend.PersonState

	// ActiveWorksSummary is a natural language description of the agent's
	// currently running works. This gives the Comprehend phase self-awareness:
	// when the user says "change the approach" or "stop", the agent knows
	// what it is currently doing and can understand the reference.
	ActiveWorksSummary string
}

// SessionInfo holds session-level parameters needed for comprehension.
// These are loaded once from the database and passed to Comprehend
// to avoid repeated queries.
type SessionInfo struct {
	SessionID    int64
	MessageCount int64
	WindowSize   int
	KBIDs        []int64
	UserName     string
}

// Comprehend performs the comprehension phase: understanding what the
// other party means before making any judgment.
//
// This function extracts the information-gathering logic that was previously
// inside chat.Process(), making it available to the Decide phase.
// Prompt semantics are preserved — the same LLM calls are made, just in
// a different order (before Decide instead of after).
//
// Parameters:
//   - ctx: context for cancellation
//   - event: the incoming event to comprehend
//   - agent: the agent receiving the event
//   - llmConfig: LLM configuration for comprehension calls
//   - sessionInfo: session-level parameters (message count, window size, KB IDs)
//   - activeWorks: the agent's currently running works (for self-awareness)
func Comprehend(
	ctx context.Context,
	event eventqueue.AgentEvent,
	agent *model.Agent,
	llmConfig *model.LLMConfig,
	sessionInfo *SessionInfo,
	activeWorks []*work,
) *ComprehensionResult {
	result := &ComprehensionResult{}

	// Build active works summary for self-awareness.
	// This allows the agent to understand references like "change the approach"
	// or "stop" by knowing what it is currently doing.
	result.ActiveWorksSummary = buildActiveWorksSummary(activeWorks, sessionInfo.SessionID)

	eventDescription := event.FormatDescription()
	if eventDescription == "" {
		applogger.L.Info("Comprehend: empty event, skipping",
			"agent_id", agent.ID,
			"session_id", sessionInfo.SessionID,
		)
		return result
	}

	// For non-message events (work completed, scheduled, session joined/left),
	// skip the full comprehension pipeline — there is no "other party" to
	// understand, no query to preprocess, no KB to retrieve from.
	// The event description carries all the context the Decide phase needs.
	if event.Type != eventqueue.EventTypeNewMessage {
		result.ProcessedQuery = eventDescription
		result.QueryType = "clear"
		applogger.L.Info("Comprehend completed (non-message event, skipped pipeline)",
			"agent_id", agent.ID,
			"session_id", sessionInfo.SessionID,
			"event_type", event.Type,
		)
		return result
	}

	// Step 1: Query preprocessing (conditional — same conditions as before)
	// Runs when V >= N (for context engineering) or when knowledge bases
	// are configured (for KB retrieval optimization).
	var preprocessingResult *comprehend.PreprocessingResult
	if sessionInfo.MessageCount >= int64(sessionInfo.WindowSize) || len(sessionInfo.KBIDs) > 0 {
		preprocessingHistory := getPreprocessingHistory(sessionInfo.SessionID, sessionInfo.WindowSize)
		preprocessingResult = comprehend.PreprocessQuery(
			ctx,
			llmConfig,
			eventDescription,
			preprocessingHistory,
			agent.CharacterSettings,
			sessionInfo.WindowSize,
			sessionInfo.UserName,
			agent.Name,
		)
	}

	// Extract preprocessing results
	if preprocessingResult != nil {
		result.ProcessedQuery = preprocessingResult.ProcessedQuery
		result.QueryType = preprocessingResult.QueryType
		result.NeedsClarification = preprocessingResult.NeedsClarification
		result.Clarification = preprocessingResult.Clarification
		result.SkipRetrieval = preprocessingResult.SkipRetrieval
		result.PreprocessingResult = preprocessingResult
	} else {
		result.ProcessedQuery = eventDescription
		result.QueryType = "clear"
	}

	// Step 2: Person state inference (always runs — same as before)
	recentMessagesForState := comprehend.GetRecentMessages(
		sessionInfo.SessionID,
		min(int(sessionInfo.MessageCount), sessionInfo.WindowSize),
		model.MessageStatusCompleted,
	)

	result.PersonState = comprehend.InferPersonState(
		ctx,
		llmConfig,
		recentMessagesForState,
		sessionInfo.UserName,
		agent.Name,
		agent.CharacterSettings,
		result.ActiveWorksSummary,
	)

	if result.PersonState != nil {
		result.NeedsWorldInteraction = result.PersonState.NeedsWorldInteraction
	}

	// Step 3: Knowledge base retrieval (conditional — same as before)
	if len(sessionInfo.KBIDs) > 0 {
		// Wait for preprocessing to complete before using the processed query
		query := eventDescription
		if result.ProcessedQuery != "" {
			query = result.ProcessedQuery
		}

		kbResults, err := kb.SearchMultiKB(ctx, sessionInfo.KBIDs, query, 5)
		if err != nil {
			applogger.L.Error("Comprehend: KB retrieval failed",
				"session_id", sessionInfo.SessionID,
				"error", err,
			)
		} else {
			for _, kr := range kbResults {
				result.KBSegments = append(result.KBSegments, comprehend.Segment{
					Content: kr.Content,
					Source:  comprehend.SourceKnowledgeBase,
				})
			}
			applogger.L.Info("Comprehend: KB retrieved segments",
				"session_id", sessionInfo.SessionID,
				"count", len(kbResults),
			)
		}
	}

	applogger.L.Info("Comprehend completed",
		"agent_id", agent.ID,
		"session_id", sessionInfo.SessionID,
		"needs_world_interaction", result.NeedsWorldInteraction,
		"query_type", result.QueryType,
		"needs_clarification", result.NeedsClarification,
		"kb_segments", len(result.KBSegments),
	)

	return result
}

// getPreprocessingHistory retrieves recent messages for preprocessing context.
// Returns messages as llm.Message slice in chronological order.
// This is the same logic that was in chat.getPreprocessingHistory.
func getPreprocessingHistory(sessionID int64, limit int) []llm.Message {
	var messages []model.Message
	if err := database.DB.Where("session_id = ?", sessionID).
		Order("id DESC").Limit(limit).Find(&messages).Error; err != nil {
		applogger.L.Warn("getPreprocessingHistory: failed to load messages",
			"session_id", sessionID, "error", err,
		)
		return nil
	}

	// Reverse to chronological order
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}

	history := make([]llm.Message, 0, len(messages))
	for _, msg := range messages {
		role := "user"
		if msg.Role == model.MessageRoleAssistant {
			role = "assistant"
		}
		history = append(history, llm.Message{
			Role:    role,
			Content: msg.Content,
		})
	}
	return history
}

// buildActiveWorksSummary generates a natural language description of the
// agent's currently running works in the given session.
// Returns empty string if no works are active.
func buildActiveWorksSummary(works []*work, sessionID int64) string {
	var sameSessionWorks []*work
	for _, w := range works {
		if w.sessionID == sessionID {
			sameSessionWorks = append(sameSessionWorks, w)
		}
	}
	if len(sameSessionWorks) == 0 {
		return ""
	}

	var parts []string
	for _, w := range sameSessionWorks {
		typeName := "chat"
		if w.plan.Type == model.WorkTypeTask {
			typeName = "task"
		}
		parts = append(parts, fmt.Sprintf("- [%s] %s", typeName, w.plan.Guidance))
	}
	return fmt.Sprintf("Agent's current active works:\n%s", strings.Join(parts, "\n"))
}
