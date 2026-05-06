package schema

import (
	"time"

	"private-buddy-server/internal/model"
)

type MessageCreate struct {
	Content string `json:"content" binding:"required"`
}

type MessageResponse struct {
	ID              int64     `json:"id"`
	SessionID       int64     `json:"session_id"`
	Role            string    `json:"role"`
	Content         string    `json:"content"`
	Status          int       `json:"status"`
	HasInteractions int       `json:"has_interactions"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

func NewMessageResponse(m *model.Message) *MessageResponse {
	return &MessageResponse{
		ID:              m.ID,
		SessionID:       m.SessionID,
		Role:            m.Role,
		Content:         m.Content,
		Status:          m.Status,
		HasInteractions: m.HasInteractions,
		CreatedAt:       m.CreatedAt,
		UpdatedAt:       m.UpdatedAt,
	}
}

func NewMessageResponseList(entities []model.Message) []*MessageResponse {
	result := make([]*MessageResponse, 0, len(entities))
	for i := range entities {
		result = append(result, NewMessageResponse(&entities[i]))
	}
	return result
}
