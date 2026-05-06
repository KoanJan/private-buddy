package chatctx

import (
	"private-buddy-server/internal/model"
	"private-buddy-server/internal/service/llm"
	"private-buddy-server/internal/service/vectorstore"

	applogger "private-buddy-server/internal/logger"

	"gorm.io/gorm"
)

// RetrievalResult holds all context components retrieved for chat processing.
// Mirrors Python's retrieval result dictionary with typed fields.
type RetrievalResult struct {
	RecentMessages   []map[string]interface{} `json:"recent_messages"`
	RelevantSegments []map[string]interface{} `json:"relevant_segments"`
	Summary          map[string]interface{}   `json:"summary"`
	Narrative        *string                  `json:"narrative"`
	HasEmbedding     bool                     `json:"has_embedding"`
}

// RetrievalService retrieves context components for chat processing.
//
// This service coordinates the retrieval of:
//   - Recent messages (with optional status filter)
//   - RAG segments (if embedding is configured)
//   - Latest summary (if available)
//   - Cached narrative (from summary record, if available)
//
// The retrieval process supports both RAG-enabled and RAG-disabled modes.
// Narrative is retrieved from the summary record's narrative field (cached,
// generated in background alongside summary), eliminating the need for
// real-time narrative generation during chat processing.
type RetrievalService struct {
	db *gorm.DB
}

func NewRetrievalService(db *gorm.DB) *RetrievalService {
	return &RetrievalService{db: db}
}

// GetEmbeddingConfigForSession returns the embedding config for a session's agent.
// Traverses session -> agent -> embedding_config to find the configuration.
// Returns nil if any step fails (session not found, agent not found, no config).
func (rs *RetrievalService) GetEmbeddingConfigForSession(sessionID int64) *model.EmbeddingConfig {
	var session model.Session
	if err := rs.db.First(&session, sessionID).Error; err != nil {
		return nil
	}

	var agent model.Agent
	if err := rs.db.First(&agent, session.AgentID).Error; err != nil {
		return nil
	}

	if agent.EmbeddingConfigID > 0 {
		var config model.EmbeddingConfig
		if err := rs.db.First(&config, agent.EmbeddingConfigID).Error; err != nil {
			return nil
		}
		return &config
	}

	return nil
}

// GetRecentMessages returns recent messages from a session in chronological order.
// Messages are fetched in DESC order by ID and then reversed to ASC order.
// If status is provided, only messages with that status are returned.
func (rs *RetrievalService) GetRecentMessages(sessionID int64, limit int, status *int) []map[string]interface{} {
	query := rs.db.Model(&model.Message{}).Where("session_id = ?", sessionID)

	if status != nil {
		query = query.Where("status = ?", *status)
	}

	var messages []model.Message
	query.Order("id DESC").Limit(limit).Find(&messages)

	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}

	result := make([]map[string]interface{}, 0, len(messages))
	for _, msg := range messages {
		result = append(result, map[string]interface{}{
			"role":    msg.Role,
			"content": msg.Content,
			"id":      msg.ID,
		})
	}
	return result
}

// buildSummaryAndNarrative extracts summary dict and cached narrative from a HistoricalSummary.
// Returns (nil, nil) if latestSummary is nil.
// The narrative is only set if the HistoricalSummary has a non-empty Narrative field.
func (rs *RetrievalService) buildSummaryAndNarrative(latestSummary *model.HistoricalSummary) (map[string]interface{}, *string) {
	if latestSummary == nil {
		return nil, nil
	}

	summaryDict := map[string]interface{}{
		"version": latestSummary.Version,
		"content": latestSummary.Content,
	}

	var narrative *string
	if latestSummary.Narrative != "" {
		narrative = &latestSummary.Narrative
	}

	return summaryDict, narrative
}

// GetContextWithoutRAG retrieves context without RAG retrieval.
// Used for queries that don't need RAG (e.g., greetings, chitchat).
// Retrieves recent messages, latest summary, and cached narrative.
func (rs *RetrievalService) GetContextWithoutRAG(sessionID int64, recentCount int) *RetrievalResult {
	result := &RetrievalResult{
		RecentMessages:   []map[string]interface{}{},
		RelevantSegments: []map[string]interface{}{},
	}

	completedStatus := model.MessageStatusCompleted
	result.RecentMessages = rs.GetRecentMessages(sessionID, recentCount, &completedStatus)

	summarySvc := NewSummaryService(rs.db, nil, nil, nil)
	latestSummary := summarySvc.GetLatestSummaryByID(sessionID)
	result.Summary, result.Narrative = rs.buildSummaryAndNarrative(latestSummary)

	return result
}

// GetContextForChat retrieves full context for chat processing with RAG.
//
// This method retrieves all context components:
//  1. Recent messages from the session
//  2. RAG segments relevant to the query (if embedding configured)
//  3. Latest summary (if available)
//  4. Cached narrative from summary record (if available)
func (rs *RetrievalService) GetContextForChat(sessionID int64, query string, recentCount int, ragCount int) *RetrievalResult {
	result := &RetrievalResult{
		RecentMessages:   []map[string]interface{}{},
		RelevantSegments: []map[string]interface{}{},
		HasEmbedding:     false,
	}

	completedStatus := model.MessageStatusCompleted
	result.RecentMessages = rs.GetRecentMessages(sessionID, recentCount, &completedStatus)

	embeddingConfig := rs.GetEmbeddingConfigForSession(sessionID)
	if embeddingConfig != nil {
		result.HasEmbedding = true
		embeddingSvc := llm.NewEmbeddingService(embeddingConfig.BaseURL, embeddingConfig.APIKey, embeddingConfig.ModelID, 0)
		vectorStore := vectorstore.NewVectorStoreService(embeddingSvc)
		if err := vectorStore.Init(); err == nil {
			searchResults, err := vectorStore.Search(sessionID, query, ragCount)
			if err != nil {
				applogger.L.Error("RAG retrieval failed", "error", err)
			} else {
				for _, sr := range searchResults {
					result.RelevantSegments = append(result.RelevantSegments, map[string]interface{}{
						"content":    sr.Content,
						"message_id": sr.MessageID,
						"score":      sr.Score,
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

	summarySvc := NewSummaryService(rs.db, nil, nil, nil)
	latestSummary := summarySvc.GetLatestSummaryByID(sessionID)
	result.Summary, result.Narrative = rs.buildSummaryAndNarrative(latestSummary)

	return result
}

// IndexMessages adds messages to the vector store for RAG retrieval.
// This method adds messages to the vector store after they are completed,
// enabling future RAG retrieval. Returns true if indexing succeeded.
func (rs *RetrievalService) IndexMessages(sessionID int64, messageIDs []int64) bool {
	embeddingConfig := rs.GetEmbeddingConfigForSession(sessionID)
	if embeddingConfig == nil {
		applogger.L.Info("No embedding config for session, skipping indexing", "session_id", sessionID)
		return false
	}

	var messages []model.Message
	rs.db.Where("id IN ? AND session_id = ?", messageIDs, sessionID).Find(&messages)

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
		contents[i] = msg.Content
		metadatas[i] = vectorstore.VectorMetadata{
			MessageID: msg.ID,
			Role:      msg.Role,
			Content:   msg.Content,
		}
		msgIDs[i] = msg.ID
	}

	if err := vectorStore.AddMessages(sessionID, msgIDs, contents, metadatas); err != nil {
		applogger.L.Error("Failed to index messages", "error", err)
		return false
	}

	applogger.L.Info("Indexed messages for session", "session_id", sessionID, "count", len(messages))
	return true
}
