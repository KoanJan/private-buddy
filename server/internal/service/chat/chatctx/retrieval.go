package chatctx

import (
	"context"

	"private-buddy-server/internal/database"
	"private-buddy-server/internal/model"
	"private-buddy-server/internal/service/llm"
	"private-buddy-server/internal/service/vectorstore"

	applogger "private-buddy-server/internal/logger"
)

// Segment source constants
const (
	SourceChatHistory = iota + 1
	SourceKnowledgeBase
)

// Segment represents a retrieved context segment used in prompt assembly.
type Segment struct {
	Content string `json:"content"`
	Source  int    `json:"source"`
}

// RetrievalResult holds all context components retrieved for chat processing.
type RetrievalResult struct {
	RecentMessages   []model.Message `json:"recent_messages"`
	RelevantSegments []Segment       `json:"relevant_segments"`
	SummaryVersion   int             `json:"summary_version"`
	Narrative        string          `json:"narrative"`
	HasEmbedding     bool            `json:"has_embedding"`
}

// getEmbeddingConfigForSession returns the embedding config for a session's agent.
// Traverses session -> agent -> embedding_config to find the configuration.
// Returns nil if any step fails (session not found, agent not found, no config).
func getEmbeddingConfigForSession(sessionID int64) *model.EmbeddingConfig {
	var session model.Session
	if err := database.DB.First(&session, sessionID).Error; err != nil {
		return nil
	}

	var agent model.Agent
	if err := database.DB.First(&agent, session.AgentID).Error; err != nil {
		return nil
	}

	if agent.EmbeddingConfigID > 0 {
		var config model.EmbeddingConfig
		if err := database.DB.First(&config, agent.EmbeddingConfigID).Error; err != nil {
			return nil
		}
		return &config
	}

	return nil
}

// GetRecentMessages returns recent messages from a session in chronological order.
// Messages are fetched in DESC order by ID and then reversed to ASC order.
// If status >= 0, only messages with that status are returned; -1 means no filter.
func GetRecentMessages(sessionID int64, limit int, status int) []model.Message {
	query := database.DB.Model(&model.Message{}).Where("session_id = ?", sessionID)

	if status >= 0 {
		query = query.Where("status = ?", status)
	}

	var messages []model.Message
	query.Order("id DESC").Limit(limit).Find(&messages)

	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}

	return messages
}

// buildSummaryAndNarrative extracts summary version and cached narrative from a HistoricalSummary.
// Returns (nil, nil) if latestSummary is nil.
func buildSummaryAndNarrative(latestSummary *model.HistoricalSummary) (int, string) {
	if latestSummary == nil {
		return -1, ""
	}

	return latestSummary.Version, latestSummary.Narrative
}

// GetContextWithoutRAG retrieves context without RAG retrieval.
// Used for queries that don't need RAG (e.g., greetings, chitchat).
// Retrieves recent messages, latest summary, and cached narrative.
func GetContextWithoutRAG(sessionID, agentID int64, recentCount int) *RetrievalResult {
	result := &RetrievalResult{
		RecentMessages:   []model.Message{},
		RelevantSegments: []Segment{},
	}

	result.RecentMessages = GetRecentMessages(sessionID, recentCount, model.MessageStatusCompleted)

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
		RelevantSegments: []Segment{},
		HasEmbedding:     false,
	}

	result.RecentMessages = GetRecentMessages(sessionID, recentCount, model.MessageStatusCompleted)

	embeddingConfig := getEmbeddingConfigForSession(sessionID)
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
					result.RelevantSegments = append(result.RelevantSegments, Segment{
						Content: sr.Content,
						Source:  SourceChatHistory,
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
	embeddingConfig := getEmbeddingConfigForSession(sessionID)
	if embeddingConfig == nil {
		applogger.L.Info("No embedding config for session, skipping indexing", "session_id", sessionID)
		return false
	}

	var messages []model.Message
	database.DB.Where("id IN ? AND session_id = ?", messageIDs, sessionID).Find(&messages)

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
