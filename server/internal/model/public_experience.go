package model

import "time"

// PublicExperience source type constants.
const (
	PublicExperienceSourceIngestion = 1 // Imported from external skill files
	PublicExperienceSourceShare     = 2 // Shared by an agent from private experience (reserved)
)

// PublicExperience status constants.
const (
	PublicExperienceStatusGenerating = 1 // LLM distillation in progress
	PublicExperienceStatusActive     = 2 // Distillation complete, experience is usable
	PublicExperienceStatusError      = 3 // Distillation failed; can be retried via redistill
)

// PublicExperience represents a host-agnostic cognitive asset accessible to all agents.
// It mirrors AgentExperience field-for-field so that when an agent learns a public
// experience, field mapping is 1:1.
//
// When source_type=1 (Ingestion), a PublicExperience row is pre-written at upload
// time with Status=Generating and empty content fields. The background distillation
// goroutine later fills in the content and sets Status=Active (or Status=Error on
// failure). This gives the frontend immediate feedback after skill upload.
type PublicExperience struct {
	ID                int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	Title             string    `gorm:"type:varchar(500);not null" json:"title"`
	Description       string    `gorm:"type:text;not null" json:"description"`
	WhenToUse         string    `gorm:"type:text;not null;default:''" json:"when_to_use"`
	Guidelines        string    `gorm:"type:text;not null;default:''" json:"guidelines"`
	Pitfalls          string    `gorm:"type:text;not null;default:''" json:"pitfalls"`
	Procedure         string    `gorm:"type:text;not null;default:''" json:"procedure"`
	SourceType        int       `gorm:"not null;default:1" json:"source_type"`
	SourceID          int64     `gorm:"not null;default:0;index;column:source_id" json:"source_id"`
	SourceFingerprint string    `gorm:"type:varchar(64);not null;default:'';index;column:source_fingerprint" json:"source_fingerprint"`
	Status            int       `gorm:"not null;default:2" json:"status"`
	CreatedAt         time.Time `gorm:"not null;autoCreateTime" json:"created_at"`
	UpdatedAt         time.Time `gorm:"not null;autoUpdateTime" json:"updated_at"`
}

func (PublicExperience) TableName() string { return "public_experiences" }
