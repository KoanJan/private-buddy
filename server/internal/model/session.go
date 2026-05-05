package model

import "time"

// Session status constants.
const (
	SessionStatusStreaming = 0 // Session is currently processing a message
	SessionStatusIdle      = 1 // Session is idle and ready for new messages
)

// Session represents a conversation session between a user and an agent.
// Each session belongs to one agent and contains a series of messages.
type Session struct {
	ID        int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	Title     string    `gorm:"type:varchar(255);not null;default:''" json:"title"`
	AgentID   int64     `gorm:"not null;index;column:agent_id" json:"agent_id"`
	Status    int       `gorm:"not null;default:1" json:"status"`
	CreatedAt time.Time `gorm:"not null;autoCreateTime" json:"created_at"`
	UpdatedAt time.Time `gorm:"not null;autoUpdateTime" json:"updated_at"`
}

func (Session) TableName() string { return "sessions" }
