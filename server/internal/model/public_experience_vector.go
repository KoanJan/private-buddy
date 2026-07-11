package model

import "time"

// PublicExperienceVector stores the embedding of a public experience's description.
// experience_id is used as the primary key (1:1 relationship, same pattern as event_vectors).
type PublicExperienceVector struct {
	ExperienceID int64     `gorm:"primaryKey;column:experience_id" json:"experience_id"`
	Embedding    []byte    `gorm:"not null;type:blob" json:"embedding"` // float32 little-endian
	CreatedAt    time.Time `gorm:"not null;autoCreateTime" json:"created_at"`
}

// TableName returns the database table name for PublicExperienceVector.
func (PublicExperienceVector) TableName() string { return "public_experience_vectors" }
