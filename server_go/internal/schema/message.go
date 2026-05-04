package schema

import "time"

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
