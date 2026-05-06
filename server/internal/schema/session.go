package schema

import (
	"time"

	"private-buddy-server/internal/model"
)

type SessionBase struct {
	Title   *string `json:"title"`
	AgentID int64   `json:"agent_id" binding:"required"`
}

type SessionCreate SessionBase

type SessionUpdate struct {
	Title   *string `json:"title"`
	AgentID *int64  `json:"agent_id"`
}

type SessionResponse struct {
	ID        int64     `json:"id"`
	Title     string    `json:"title"`
	AgentID   int64     `json:"agent_id"`
	Status    int       `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func NewSessionResponse(m *model.Session) *SessionResponse {
	return &SessionResponse{
		ID:        m.ID,
		Title:     m.Title,
		AgentID:   m.AgentID,
		Status:    m.Status,
		CreatedAt: m.CreatedAt,
		UpdatedAt: m.UpdatedAt,
	}
}

func NewSessionResponseList(entities []model.Session) []*SessionResponse {
	result := make([]*SessionResponse, 0, len(entities))
	for i := range entities {
		result = append(result, NewSessionResponse(&entities[i]))
	}
	return result
}

func (req *SessionUpdate) BuildUpdates() map[string]interface{} {
	updates := make(map[string]interface{})
	if req.Title != nil {
		updates["title"] = *req.Title
	}
	if req.AgentID != nil {
		updates["agent_id"] = *req.AgentID
	}
	return updates
}
