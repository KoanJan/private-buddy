package chatcontext

import (
	"context"

	"private-buddy-server/internal/database"
	"private-buddy-server/internal/model"
	"private-buddy-server/internal/service"
	"private-buddy-server/internal/service/comprehend"
	"private-buddy-server/internal/service/llm"
	"private-buddy-server/internal/service/vectorstore"

	applogger "private-buddy-server/internal/logger"
)

// RetrievalResult holds all context components retrieved for chat processing.
type RetrievalResult struct {
	RecentMessages   []model.Message     `json:"recent_messages"`
	RelevantSegments []comprehend.Segment `json:"relevant_segments"`
	SummaryVersion   int                 `json:"summary_version"`
	Narrative        string              `json:"narrative"`
	HasEmbedding     bool                `json:"has_embedding"`
}

// getEmbeddingConfig returns the global embedding config.
// Returns nil if no config exists.
func getEmbeddingConfig() *model.EmbeddingConfig {
	return service.GetEmbeddingConfig()
}

// buildSummaryAndNarrative extracts summary version and cached narrative from a HistoricalSummary.
// Returns (nil, nil) if latestSummary is nil.
func buildSummaryAndNarrative(latestSummary *model.HistoricalSummary) (int, string) {
	if latestSummary == nil {
		return -1, ""
	}

	return latestSummary.Version, latestSummary.Narrative
}

// getLatestSummaryByID returns the latest summary for a (session, agent).
func getLatestSummaryByID(sessionID, agentID int64) *model.HistoricalSummary {
	var summary model.HistoricalSummary
	err := database.DB.Where("session_id = ? AND agent_id = ?", sessionID, agentID).Order("version DESC").First(&summary).Error
	if err != nil {
		return nil
	}
	return &summary
}

// GetContextWithoutRAG retrieves context without RAG retrieval.
// Used for queries that don't need RAG (e.g., greetings, chitchat).
// Retrieves recent messages, latest summary, and cached narrative.
func GetContextWithoutRAG(sessionID, agentID int64, recentCount int) *RetrievalResult {
	result := &RetrievalResult{
		RecentMessages:   []model.Message{},
		RelevantSegments: []comprehend.Segment{},
	}

	result.RecentMessages = comprehend.GetRecentMessages(sessionID, recentCount, model.MessageStatusCompleted)

	latestSummary := getLatestSummaryByID(sessionID, agentID)
	result.SummaryVersion, result.Narrative = buildSummaryAndNarrative(latestSummary)

	return result
}

// GetContextForChat retrieves context for chat response generation.
// Returns:
//  1. Recent messages from the session
//  2. RAG segments relevant to the query (if embedding configured)
//  3. Latest summary (if available)
//  4. Cached narrative from summary record (if available)
func GetContextForChat(ctx context.Context, sessionID, agentID int64, query string, recentCount int, ragCount int) *RetrievalResult {
	result := &RetrievalResult{
		RecentMessages:   []model.Message{},
		RelevantSegments: []comprehend.Segment{},
		HasEmbedding:     false,
	}

	result.RecentMessages = comprehend.GetRecentMessages(sessionID, recentCount, model.MessageStatusCompleted)

	embeddingConfig := getEmbeddingConfig()
	if embeddingConfig != nil {
		result.HasEmbedding = true
		embeddingSvc := llm.NewEmbeddingService(embeddingConfig.BaseURL, embeddingConfig.APIKey, embeddingConfig.ModelID, 0)
		vectorStore := vectorstore.NewVectorStoreService(embeddingSvc)
		if err := vectorStore.Init(); err == nil {
			searchResults, err := vectorStore.Search(ctx, sessionID, query, ragCount)
			if err != nil {
				applogger.L.Error("RAG retrieval failed", "error", err)
			} else {
				for _, sr := range searchResults {
					result.RelevantSegments = append(result.RelevantSegments, comprehend.Segment{
						MessageID: sr.MessageID,
						Content:   sr.Content,
						Source:    comprehend.SourceChatHistory,
					})
				}
				applogger.L.Info("RAG retrieved segments",
					"session_id", sessionID,
					"count", len(searchResults),
				)
			}
			vectorStore.Close()
		}
	}

	latestSummary := getLatestSummaryByID(sessionID, agentID)
	result.SummaryVersion, result.Narrative = buildSummaryAndNarrative(latestSummary)

	return result
}

// IndexMessages adds messages to the vector store for RAG retrieval.
// This method adds messages to the vector store after they are completed,
// enabling future RAG retrieval. Returns true if indexing succeeded.
//
// NOTE: This only indexes the given messageIDs (typically the current round).
// Messages that existed before embedding was configured are NOT retroactively
// indexed. A batch re-index mechanism is needed to cover that case.
func IndexMessages(ctx context.Context, sessionID int64, messageIDs []int64) bool {
	embeddingConfig := getEmbeddingConfig()
	if embeddingConfig == nil {
		applogger.L.Info("No embedding config, skipping indexing", "session_id", sessionID)
		return false
	}

	var messages []model.Message
	if err := database.DB.Where("id IN ? AND session_id = ?", messageIDs, sessionID).Find(&messages).Error; err != nil {
		applogger.L.Warn("IndexMessages: failed to load messages", "session_id", sessionID, "error", err)
		return false
	}

	if len(messages) == 0 {
		applogger.L.Warn("No messages found for indexing", "session_id", sessionID)
		return false
	}

	embeddingSvc := llm.NewEmbeddingService(embeddingConfig.BaseURL, embeddingConfig.APIKey, embeddingConfig.ModelID, 0)
	vectorStore := vectorstore.NewVectorStoreService(embeddingSvc)
	if err := vectorStore.Init(); err != nil {
		applogger.L.Error("Failed to init vector store for indexing", "error", err)
		return false
	}
	defer vectorStore.Close()

	contents := make([]string, len(messages))
	metadatas := make([]vectorstore.VectorMetadata, len(messages))
	msgIDs := make([]int64, len(messages))

	for i, msg := range messages {
		role := "user"
		if msg.Role == model.MessageRoleAssistant {
			role = "assistant"
		}
		contents[i] = msg.Content
		metadatas[i] = vectorstore.VectorMetadata{
			MessageID: msg.ID,
			Role:      role,
			Content:   msg.Content,
		}
		msgIDs[i] = msg.ID
	}

	if err := vectorStore.AddMessages(ctx, sessionID, msgIDs, contents, metadatas); err != nil {
		applogger.L.Error("Failed to index messages", "error", err)
		return false
	}

	applogger.L.Info("Indexed messages for session", "session_id", sessionID, "count", len(messages))
	return true
}
