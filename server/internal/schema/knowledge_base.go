package schema

import (
	"time"

	"private-buddy-server/internal/model"
)

type KnowledgeBaseCreate struct {
	Name              string `json:"name" binding:"required"`
	Description       string `json:"description"`
	EmbeddingConfigID int64  `json:"embedding_config_id" binding:"required"`
}

type KnowledgeBaseUpdate struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
}

type KnowledgeBaseResponse struct {
	ID                int64     `json:"id"`
	Name              string    `json:"name"`
	Description       string    `json:"description"`
	EmbeddingConfigID int64     `json:"embedding_config_id"`
	IndexType         int       `json:"index_type"`
	IndexFilePath     string    `json:"index_file_path"`
	DocumentCount     int       `json:"document_count"`
	VectorCount       int       `json:"vector_count"`
	DeletedCount      int       `json:"deleted_count"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

func NewKnowledgeBaseResponse(m *model.KnowledgeBase) *KnowledgeBaseResponse {
	return &KnowledgeBaseResponse{
		ID:                m.ID,
		Name:              m.Name,
		Description:       m.Description,
		EmbeddingConfigID: m.EmbeddingConfigID,
		IndexType:         m.IndexType,
		IndexFilePath:     m.IndexFilePath,
		DocumentCount:     m.DocumentCount,
		VectorCount:       m.VectorCount,
		DeletedCount:      m.DeletedCount,
		CreatedAt:         m.CreatedAt,
		UpdatedAt:         m.UpdatedAt,
	}
}

func NewKnowledgeBaseResponseList(entities []model.KnowledgeBase) []*KnowledgeBaseResponse {
	result := make([]*KnowledgeBaseResponse, 0, len(entities))
	for i := range entities {
		result = append(result, NewKnowledgeBaseResponse(&entities[i]))
	}
	return result
}

func (req *KnowledgeBaseUpdate) BuildUpdates() map[string]interface{} {
	updates := make(map[string]interface{})
	if req.Name != nil {
		updates["name"] = *req.Name
	}
	if req.Description != nil {
		updates["description"] = *req.Description
	}
	return updates
}
