package model

import "time"

// Interaction type constants.
const (
	InteractionTypeRequest  = 1 // Messages sent to the LLM
	InteractionTypeResponse = 2 // LLM output including thoughts, tool_calls, finish_reason
)

// Interaction captures one step of the ReAct loop for agent-world interactions.
//
// Interactions are grouped by (session_id, user_msg_id, agent_msg_id, iteration)
// to support both frontend display and debugging.
// Each iteration produces two records: a request (type=1) and a response (type=2).
type Interaction struct {
	ID         int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	SessionID  int64     `gorm:"not null;index;column:session_id" json:"session_id"`
	UserMsgID  int64     `gorm:"not null;column:user_msg_id" json:"user_msg_id"`
	AgentMsgID int64     `gorm:"not null;column:agent_msg_id" json:"agent_msg_id"`
	Iteration  int       `gorm:"not null" json:"iteration"`
	Type       int       `gorm:"not null" json:"type"`
	UpdatedAt  time.Time `gorm:"not null;autoUpdateTime" json:"updated_at"`
	Data       string    `gorm:"type:text;not null" json:"data"`
	CreatedAt  time.Time `gorm:"not null;autoCreateTime" json:"created_at"`
}

func (Interaction) TableName() string { return "interactions" }
