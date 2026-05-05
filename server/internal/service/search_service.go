package service

import (
	"private-buddy-server/internal/model"

	applogger "private-buddy-server/internal/logger"

	"gorm.io/gorm"
)

// SearchService manages web search configuration.
type SearchService struct{}

// NewSearchService creates a new SearchService instance.
func NewSearchService() *SearchService {
	return &SearchService{}
}

// GetConfig retrieves the search configuration. Creates a default if not found.
func (s *SearchService) GetConfig(db *gorm.DB) *model.SearchConfig {
	var config model.SearchConfig
	if err := db.Where("id = ?", 1).First(&config).Error; err != nil {
		applogger.L.Warn("SearchConfig not found, creating default")
		config = model.SearchConfig{
			Provider:    "tavily",
			APIKey:      "",
			Description: "",
			IsActive:    false,
		}
		db.Create(&config)
	}
	return &config
}

// UpdateConfig updates the search configuration with non-nil fields.
func (s *SearchService) UpdateConfig(db *gorm.DB, provider, apiKey, description *string, isActive *bool) *model.SearchConfig {
	config := s.GetConfig(db)

	updates := make(map[string]interface{})
	if provider != nil {
		updates["provider"] = *provider
	}
	if apiKey != nil {
		updates["api_key"] = *apiKey
	}
	if description != nil {
		updates["description"] = *description
	}
	if isActive != nil {
		updates["is_active"] = *isActive
	}

	if len(updates) > 0 {
		db.Model(config).Updates(updates)
		db.First(config, 1)
	}

	applogger.L.Info("SearchConfig updated",
		"provider", config.Provider,
		"is_active", config.IsActive,
		"has_api_key", config.APIKey != "",
	)
	return config
}
