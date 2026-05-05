// Package model defines the database models for the application.
package model

import "time"

// Message status constants.
const (
	MessageStatusStreaming  = 0 // Message is currently being generated (SSE streaming)
	MessageStatusCompleted = 1 // Message generation is complete

	// HasInteractions values indicate whether a message has associated agent interactions.
	HasInteractionsPending = 0 // Not yet determined (will be checked during processing)
	HasInteractionsExists  = 1 // Message has associated world-interaction records
	HasInteractionsNone    = 2 // Message has no world-interaction records
)

// Message represents a chat message in a session.
// Messages can be from the user (role="user") or the AI assistant (role="assistant").
// Assistant messages go through a streaming phase before being completed.
type Message struct {
	ID              int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	SessionID       int64     `gorm:"not null;index;column:session_id" json:"session_id"`
	Role            string    `gorm:"type:varchar(20);not null" json:"role"`
	Content         string    `gorm:"type:text;not null" json:"content"`
	Status          int       `gorm:"not null;default:0" json:"status"`
	HasInteractions int       `gorm:"not null;default:0;column:has_interactions" json:"has_interactions"`
	CreatedAt       time.Time `gorm:"not null;autoCreateTime" json:"created_at"`
	UpdatedAt       time.Time `gorm:"not null;autoUpdateTime" json:"updated_at"`
}

func (Message) TableName() string { return "messages" }
