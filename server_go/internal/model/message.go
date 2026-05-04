package model

import "time"

const (
	MessageStatusStreaming = 0
	MessageStatusCompleted = 1

	HasInteractionsPending = 0
	HasInteractionsExists  = 1
	HasInteractionsNone    = 2
)

type Message struct {
	ID              int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	SessionID       int64     `gorm:"not null;index;column:session_id" json:"session_id"`
	Role            string    `gorm:"type:varchar(20);not null" json:"role"`
	Content         string    `gorm:"type:text;not null" json:"content"`
	Status          int       `gorm:"not null;default:0" json:"status"`
	HasInteractions int       `gorm:"not null;default:0;column:has_interactions" json:"has_interactions"`
	CreatedAt       time.Time `gorm:"not null;autoCreateTime" json:"created_at"`
	UpdatedAt       time.Time `gorm:"not null;autoUpdateTime" json:"updated_at"`
}

func (Message) TableName() string { return "messages" }
