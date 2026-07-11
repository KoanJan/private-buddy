package model

import "time"

// UploadedSkill stores the original SKILL.md file uploaded by a user for ingestion.
// It is a stateless record — concurrency control during distillation is handled
// in-memory (sync.Map), not via a DB status field. The distillation outcome
// (success/failure/generating) is tracked on the linked PublicExperience.Status.
type UploadedSkill struct {
	ID         int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	FileName   string    `gorm:"type:varchar(500);not null;default:''" json:"file_name"`
	Title      string    `gorm:"type:varchar(500);not null;default:''" json:"title"`
	RawContent string    `gorm:"type:text;not null" json:"raw_content"`
	CreatedAt  time.Time `gorm:"not null;autoCreateTime" json:"created_at"`
}

// TableName returns the database table name for UploadedSkill.
func (UploadedSkill) TableName() string { return "uploaded_skills" }
