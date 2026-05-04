package model

import "time"

type SearchConfig struct {
	ID          int64     `gorm:"primaryKey;default:1" json:"id"`
	Provider    string    `gorm:"type:varchar(50);not null;default:'tavily'" json:"provider"`
	APIKey      string    `gorm:"type:varchar(255);not null;default:'';column:api_key" json:"api_key"`
	Description string    `gorm:"type:text;not null;default:''" json:"description"`
	IsActive    bool      `gorm:"not null;default:false;column:is_active" json:"is_active"`
	UpdatedAt   time.Time `gorm:"not null;autoUpdateTime" json:"updated_at"`
}

func (SearchConfig) TableName() string { return "search_config" }

func (sc *SearchConfig) IsAvailable() bool {
	return sc.IsActive && sc.APIKey != ""
}
