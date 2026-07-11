package schema

import (
	"time"

	"private-buddy-server/internal/model"
)

// SessionBase contains the common fields for session creation.
type SessionBase struct {
	Title   *string `json:"title"`
	AgentID int64   `json:"agent_id" binding:"required"`
}

// SessionCreate is an alias of SessionBase for creating sessions.
type SessionCreate SessionBase

// SessionUpdate contains the mutable fields for updating a session.
type SessionUpdate struct {
	Title   *string `json:"title"`
	AgentID *int64  `json:"agent_id"`
}

// SessionResponse represents the API response for a session.
type SessionResponse struct {
	ID        int64     `json:"id"`
	Title     string    `json:"title"`
	AgentID   int64     `json:"agent_id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// NewSessionResponse converts a model.Session to a SessionResponse.
func NewSessionResponse(m *model.Session) *SessionResponse {
	return &SessionResponse{
		ID:        m.ID,
		Title:     m.Title,
		AgentID:   m.AgentID,
		CreatedAt: m.CreatedAt,
		UpdatedAt: m.UpdatedAt,
	}
}

// NewSessionResponseList converts a list of model.Session to SessionResponse list.
func NewSessionResponseList(entities []model.Session) []*SessionResponse {
	result := make([]*SessionResponse, 0, len(entities))
	for i := range entities {
		result = append(result, NewSessionResponse(&entities[i]))
	}
	return result
}

// BuildUpdates builds a map of non-nil update fields from SessionUpdate.
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
