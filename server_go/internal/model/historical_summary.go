package model

import "time"

type HistoricalSummary struct {
	ID        int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	SessionID int64     `gorm:"not null;index;column:session_id" json:"session_id"`
	Version   int       `gorm:"not null" json:"version"`
	Content   string    `gorm:"type:text;not null" json:"content"`
	Narrative string    `gorm:"type:text;not null;default:''" json:"narrative"`
	CreatedAt time.Time `gorm:"not null;autoCreateTime" json:"created_at"`
}

func (HistoricalSummary) TableName() string { return "historical_summaries" }
