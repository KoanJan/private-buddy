package model

import "time"

// Participant type constants for ParticipantSession.
const (
	ParticipantTypeUser  = 1 // Human user
	ParticipantTypeAgent = 2 // AI agent
)

// Participant role constants.
const (
	ParticipantRoleOwner   = 1 // Session creator
	ParticipantRoleMember  = 2 // Regular participant
	ParticipantRoleWatcher = 3 // Read-only observer (future: multi-agent)
)

// Participant status constants.
// Status represents the real-time interaction state of a participant within
// a session. The distinction is simple: idle (not busy) vs working (busy).
// For agents, "working" covers any active processing — thinking, responding,
// or executing interactions. For users, this is always "idle".
const (
	ParticipantStatusIdle    = 0 // Not actively engaged
	ParticipantStatusWorking = 1 // Agent is actively processing
)

// ParticipantSession tracks the relationship between a participant (user or agent)
// and a session. It supports polymorphic participants via participant_type + participant_id.
//
// The record's existence implies active participation. When a participant leaves
// or is removed, the record is deleted — no soft-delete field needed.
//
// Key responsibilities:
//   - Track read progress: last_read_message_id for each participant
//   - Support multi-participant sessions (1v1 and future multi-agent)
//   - Enable "unread count" computation
//   - Provide the foundation for event routing (who should receive events)
//   - Track real-time interaction status (idle/working)
type ParticipantSession struct {
	ID                int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	SessionID         int64     `gorm:"not null;index;column:session_id" json:"session_id"`
	ParticipantType   int       `gorm:"not null;column:participant_type" json:"participant_type"`                       // 1=user, 2=agent
	ParticipantID     int64     `gorm:"not null;column:participant_id" json:"participant_id"`                           // user_id or agent_id
	Role              int       `gorm:"not null;default:2;column:role" json:"role"`                                     // 1=owner, 2=member, 3=watcher
	Status            int       `gorm:"not null;default:0;column:status" json:"status"`                                 // 0=idle, 1=working
	LastReadMessageID int64     `gorm:"not null;default:0;column:last_read_message_id" json:"last_read_message_id"`     // Last message ID the participant has read
	LastActiveAt      time.Time `gorm:"not null;default:CURRENT_TIMESTAMP;column:last_active_at" json:"last_active_at"` // Last time the participant performed an action
	CreatedAt         time.Time `gorm:"not null;autoCreateTime" json:"created_at"`
	UpdatedAt         time.Time `gorm:"not null;autoUpdateTime" json:"updated_at"`
}

func (ParticipantSession) TableName() string { return "participant_sessions" }
