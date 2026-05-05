package model

import "time"

// DBVersion tracks database schema migration versions.
// Used to ensure the database schema is at the expected version.
type DBVersion struct {
	ID          int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	Version     string    `gorm:"type:varchar(20);not null" json:"version"`
	Description string    `gorm:"type:text;not null;default:''" json:"description"`
	AppliedAt   time.Time `gorm:"not null;autoCreateTime;column:applied_at" json:"applied_at"`
}

func (DBVersion) TableName() string { return "db_versions" }
