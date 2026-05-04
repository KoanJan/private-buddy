package schema

import "time"

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
