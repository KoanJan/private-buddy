// Package model defines the database models for the application.
package model

import "time"

// Message role constants.
const (
	MessageRoleUser      = 1 // Human user message
	MessageRoleAssistant = 2 // AI assistant message
)

// Message status constants.
const (
	MessageStatusStreaming = 0 // Message is currently being generated (SSE streaming)
	MessageStatusCompleted = 1 // Message generation is complete
)

// Message represents a chat message in a session.
// Messages can be from the user (role=1) or the AI assistant (role=2).
// Assistant messages go through a streaming phase before being completed.
type Message struct {
	ID        int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	SessionID int64     `gorm:"not null;index;column:session_id" json:"session_id"`
	Role      int       `gorm:"not null" json:"role"` // 1=user, 2=assistant
	Content   string    `gorm:"type:text;not null" json:"content"`
	Status    int       `gorm:"not null;default:0" json:"status"`
	DraftID   *int64    `gorm:"column:draft_id" json:"draft_id"` // References message_drafts.id, NULL for user messages
	CreatedAt time.Time `gorm:"not null;autoCreateTime" json:"created_at"`
	UpdatedAt time.Time `gorm:"not null;autoUpdateTime" json:"updated_at"`
}

func (Message) TableName() string { return "messages" }
