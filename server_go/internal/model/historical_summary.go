package model

import "time"

// HistoricalSummary stores a versioned summary of conversation history.
//
// Each summary covers messages from the beginning up to a specific version number.
// The narrative field contains a cached narrative (generated alongside the summary)
// that provides a story-style retelling of the conversation from the character's perspective.
type HistoricalSummary struct {
	ID        int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	SessionID int64     `gorm:"not null;index;column:session_id" json:"session_id"`
	Version   int       `gorm:"not null" json:"version"`            // Covers messages 1 to Version
	Content   string    `gorm:"type:text;not null" json:"content"`  // Summary text
	Narrative string    `gorm:"type:text;not null;default:''" json:"narrative"` // Cached narrative from character's perspective
	CreatedAt time.Time `gorm:"not null;autoCreateTime" json:"created_at"`
}

func (HistoricalSummary) TableName() string { return "historical_summaries" }
