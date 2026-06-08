package model

import "time"

// MessageDraft status constants.
const (
	DraftStatusBuilding  = 0  // Draft content is being generated
	DraftStatusCommitted = 1  // Draft has been committed to messages table
	DraftStatusDiscarded = -1 // Draft was discarded (e.g., Work abandoned)
)

// MessageDraft represents an in-progress message being constructed by an agent.
//
// Drafts decouple the message construction process from the messages table.
// An agent builds content in a draft without polluting the message stream.
// Only when the draft is complete is it atomically committed to messages.
//
// Key principle: Agent's message construction process should NOT affect the
// session's message stream. Only the final result should appear in messages.
type MessageDraft struct {
	ID                 int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	AgentID            int64     `gorm:"not null;index:idx_agent_session_status" json:"agent_id"`
	SessionID          int64     `gorm:"not null;index:idx_agent_session_status" json:"session_id"`
	LastReadMessageID  int64     `gorm:"not null;default:0;column:last_read_message_id" json:"last_read_message_id"` // Context snapshot boundary
	Content            string    `gorm:"type:text;not null;default:''" json:"content"`
	Status             int       `gorm:"not null;default:0;index:idx_agent_session_status" json:"status"` // 0=building, 1=committed, -1=discarded
	CreatedAt          time.Time `gorm:"not null;autoCreateTime" json:"created_at"`
	UpdatedAt          time.Time `gorm:"not null;autoUpdateTime" json:"updated_at"`
}

func (MessageDraft) TableName() string { return "message_drafts" }
