package model

import "time"

// Session represents a conversation session between a user and an agent.
// Each session belongs to one agent and contains a series of messages.
// Session status is now managed by AgentRuntime (in-memory), not persisted.
type Session struct {
	ID        int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	Title     string    `gorm:"type:varchar(255);not null;default:''" json:"title"`
	AgentID   int64     `gorm:"not null;index;column:agent_id" json:"agent_id"`
	CreatedAt time.Time `gorm:"not null;autoCreateTime" json:"created_at"`
	UpdatedAt time.Time `gorm:"not null;autoUpdateTime" json:"updated_at"`
}

func (Session) TableName() string { return "sessions" }
