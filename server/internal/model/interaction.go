package model

import "time"

// Interaction type constants.
const (
	InteractionTypeRequest  = 1 // Messages sent to the LLM
	InteractionTypeResponse = 2 // LLM output including thoughts, tool_calls, finish_reason
)

// Interaction captures one step of the ReAct loop for agent-world interactions.
//
// In the draft-based architecture, interactions are grouped by draft_id,
// which represents the Work that produced them. This replaces the old
// (user_msg_id, agent_msg_id) pairing, since a Work can absorb multiple
// user messages and produces only one draft (and one final message).
//
// Each iteration produces two records: a request (type=1) and a response (type=2).
type Interaction struct {
	ID        int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	SessionID int64     `gorm:"not null;index;column:session_id" json:"session_id"`
	DraftID   int64     `gorm:"not null;index;column:draft_id" json:"draft_id"` // References message_drafts.id
	Iteration int       `gorm:"not null" json:"iteration"`
	Type      int       `gorm:"not null" json:"type"`
	Data      string    `gorm:"type:text;not null" json:"data"`
	CreatedAt time.Time `gorm:"not null;autoCreateTime" json:"created_at"`
	UpdatedAt time.Time `gorm:"not null;autoUpdateTime" json:"updated_at"`
}

func (Interaction) TableName() string { return "interactions" }
