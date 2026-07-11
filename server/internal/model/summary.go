package model

import "time"

// Summary stores a versioned factual summary of conversation history.
//
// Summaries are scoped by session_id only — one summary per version per session.
// All agents in the same session share the same factual summary.
// This is the session-level counterpart to AgentNarrative, which provides
// each agent's character-perspective retelling of the same summary.
//
// Version represents the number of messages this summary covers (messages 1 to Version).
type Summary struct {
	ID        int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	SessionID int64     `gorm:"not null;uniqueIndex:idx_session_version;column:session_id" json:"session_id"`
	Version   int       `gorm:"not null;uniqueIndex:idx_session_version" json:"version"`
	Content   string    `gorm:"type:text;not null" json:"content"`
	CreatedAt time.Time `gorm:"not null;autoCreateTime" json:"created_at"`
	UpdatedAt time.Time `gorm:"not null;autoUpdateTime" json:"updated_at"`
}

// TableName returns the database table name for Summary.
func (Summary) TableName() string { return "summaries" }
