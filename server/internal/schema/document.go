package schema

import (
	"time"

	"private-buddy-server/internal/model"
)

type DocumentResponse struct {
	ID              int64     `json:"id"`
	KnowledgeBaseID int64     `json:"knowledge_base_id"`
	Title           string    `json:"title"`
	Source          string    `json:"source"`
	FilePath        string    `json:"file_path"`
	FileSize        int64     `json:"file_size"`
	FileType        string    `json:"file_type"`
	ChunkCount      int       `json:"chunk_count"`
	Status          int       `json:"status"`
	ErrorMessage    string    `json:"error_message"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

func NewDocumentResponse(m *model.Document) *DocumentResponse {
	return &DocumentResponse{
		ID:              m.ID,
		KnowledgeBaseID: m.KnowledgeBaseID,
		Title:           m.Title,
		Source:          m.Source,
		FilePath:        m.FilePath,
		FileSize:        m.FileSize,
		FileType:        m.FileType,
		ChunkCount:      m.ChunkCount,
		Status:          m.Status,
		ErrorMessage:    m.ErrorMessage,
		CreatedAt:       m.CreatedAt,
		UpdatedAt:       m.UpdatedAt,
	}
}

func NewDocumentResponseList(entities []model.Document) []*DocumentResponse {
	result := make([]*DocumentResponse, 0, len(entities))
	for i := range entities {
		result = append(result, NewDocumentResponse(&entities[i]))
	}
	return result
}

type SearchRequest struct {
	Query string `json:"query" binding:"required"`
	TopK  int    `json:"top_k"`
}

type MultiKBSearchRequest struct {
	KBIDs []int64 `json:"kb_ids" binding:"required"`
	Query string  `json:"query" binding:"required"`
	TopK  int     `json:"top_k"`
}

type SearchResult struct {
	ChunkID         int64   `json:"chunk_id"`
	DocumentID      int64   `json:"document_id"`
	DocumentTitle   string  `json:"document_title"`
	Content         string  `json:"content"`
	Score           float64 `json:"score"`
	KnowledgeBaseID int64   `json:"knowledge_base_id"`
}
