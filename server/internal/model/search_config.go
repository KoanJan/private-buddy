package model

import "time"

// SearchConfig stores the web search provider configuration.
// Currently supports Tavily as the search provider.
type SearchConfig struct {
	ID          int64     `gorm:"primaryKey;default:1" json:"id"`
	Provider    string    `gorm:"type:varchar(50);not null;default:'tavily'" json:"provider"`
	APIKey      string    `gorm:"type:varchar(255);not null;default:'';column:api_key" json:"api_key"`
	Description string    `gorm:"type:text;not null;default:''" json:"description"`
	IsActive    bool      `gorm:"not null;default:false;column:is_active" json:"is_active"`
	UpdatedAt   time.Time `gorm:"not null;autoUpdateTime" json:"updated_at"`
}

func (SearchConfig) TableName() string { return "search_config" }

// IsAvailable returns true if the search provider is active and has a valid API key.
func (sc *SearchConfig) IsAvailable() bool {
	return sc.IsActive && sc.APIKey != ""
}
