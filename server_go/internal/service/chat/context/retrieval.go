package context

import (
	"private-buddy-server/internal/model"
	"private-buddy-server/internal/service/llm"

	applogger "private-buddy-server/internal/logger"

	"gorm.io/gorm"
)

// RetrievalResult holds all context components retrieved for chat processing.
type RetrievalResult struct {
	RecentMessages   []map[string]interface{} `json:"recent_messages"`
	RelevantSegments []map[string]interface{} `json:"relevant_segments"`
	Summary          map[string]interface{}   `json:"summary"`
	Narrative        *string                  `json:"narrative"`
	HasEmbedding     bool                     `json:"has_embedding"`
}

// RetrievalService retrieves context components for chat processing.
type RetrievalService struct {
	db *gorm.DB
}

func NewRetrievalService(db *gorm.DB) *RetrievalService {
	return &RetrievalService{db: db}
}

// GetEmbeddingConfigForSession returns the embedding config for a session's agent.
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
		vectorStore := NewVectorStoreService(embeddingSvc)
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
	vectorStore := NewVectorStoreService(embeddingSvc)
	if err := vectorStore.Init(); err != nil {
		applogger.L.Error("Failed to init vector store for indexing", "error", err)
		return false
	}
	defer vectorStore.Close()

	contents := make([]string, len(messages))
	metadatas := make([]VectorMetadata, len(messages))
	msgIDs := make([]int64, len(messages))

	for i, msg := range messages {
		contents[i] = msg.Content
		metadatas[i] = VectorMetadata{
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
