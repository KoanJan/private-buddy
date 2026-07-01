package model

import "time"

// AgentExperienceVector stores the embedding of a private experience's description.
// experience_id is used as the primary key (1:1 relationship, same pattern as event_vectors).
type AgentExperienceVector struct {
	ExperienceID int64     `gorm:"primaryKey;column:experience_id" json:"experience_id"`
	Embedding    []byte    `gorm:"not null;type:blob" json:"embedding"` // float32 little-endian
	CreatedAt    time.Time `gorm:"not null;autoCreateTime" json:"created_at"`
}

func (AgentExperienceVector) TableName() string { return "agent_experience_vectors" }
