package model

import "time"

// UploadedSkill status constants.
const (
	UploadedSkillStatusPending    = 0 // Awaiting LLM refinement
	UploadedSkillStatusProcessing = 1 // LLM refinement in progress — concurrency guard
	UploadedSkillStatusCompleted  = 2 // Public experience created successfully
)

// UploadedSkill stores the original SKILL.md file uploaded by a user for ingestion.
// It is a separate entity from PublicExperience — a single uploaded skill may produce
// one public experience (via LLM refinement), or may be rejected (skip=true).
// Deduplication is enforced by a unique index on the fingerprint column.
type UploadedSkill struct {
	ID          int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	SourceName  string    `gorm:"type:varchar(500);not null" json:"source_name"`
	RawContent  string    `gorm:"type:text;not null" json:"raw_content"`
	Fingerprint string    `gorm:"type:varchar(64);not null;uniqueIndex" json:"fingerprint"` // SHA-256 of RawContent
	Status      int       `gorm:"not null;default:0" json:"status"`
	CreatedAt   time.Time `gorm:"not null;autoCreateTime" json:"created_at"`
}

func (UploadedSkill) TableName() string { return "uploaded_skills" }
