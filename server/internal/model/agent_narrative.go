package model

import "time"

// AgentNarrative stores an agent's character-perspective retelling of a summary.
//
// Each agent in a session generates its own narrative from the shared Summary.
// The narrative uses second-person perspective ("You have been discussing X...")
// and reflects the agent's character-specific interpretation of the conversation.
//
// SummaryVersion links back to the Summary.Version this narrative was generated from,
// enabling regeneration when the summary changes.
type AgentNarrative struct {
	ID             int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	SessionID      int64     `gorm:"not null;index;column:session_id" json:"session_id"`
	AgentID        int64     `gorm:"not null;index;column:agent_id" json:"agent_id"`
	SummaryVersion int       `gorm:"not null" json:"summary_version"`
	Content        string    `gorm:"type:text;not null" json:"content"`
	CreatedAt      time.Time `gorm:"not null;autoCreateTime" json:"created_at"`
	UpdatedAt      time.Time `gorm:"not null;autoUpdateTime" json:"updated_at"`
}

func (AgentNarrative) TableName() string { return "agent_narratives" }
