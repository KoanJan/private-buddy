package service

import (
	"private-buddy-server/internal/database"
	"private-buddy-server/internal/model"

	applogger "private-buddy-server/internal/logger"
)

// GetSession retrieves a session by ID. Returns nil if not found.
func GetSession(sessionID int64) *model.Session {
	var session model.Session
	if err := database.DB.First(&session, sessionID).Error; err != nil {
		applogger.Error("Session not found", "session_id", sessionID, "error", err)
		return nil
	}
	return &session
}

// GetAgent retrieves an agent by ID. Returns nil if not found.
func GetAgent(agentID int64) *model.Agent {
	var agent model.Agent
	if err := database.DB.First(&agent, agentID).Error; err != nil {
		applogger.Error("Agent not found", "agent_id", agentID, "error", err)
		return nil
	}
	return &agent
}

// GetLLMConfig retrieves an LLM config by ID. Returns nil if not found.
func GetLLMConfig(llmConfigID int64) *model.LLMConfig {
	var config model.LLMConfig
	if err := database.DB.First(&config, llmConfigID).Error; err != nil {
		applogger.Error("LLM config not found", "llm_config_id", llmConfigID, "error", err)
		return nil
	}
	return &config
}

// GetSearchConfig retrieves the search configuration. Creates a default if not found.
func GetSearchConfig() *model.SearchConfig {
	var config model.SearchConfig
	if err := database.DB.Where("id = ?", 1).First(&config).Error; err != nil {
		applogger.Warn("SearchConfig not found, creating default")
		config = model.SearchConfig{
			Provider:    "tavily",
			APIKey:      "",
			Description: "",
			IsActive:    false,
		}
		if err := database.DB.Create(&config).Error; err != nil {
			applogger.Error("failed to create default search config", "error", err)
		}
	}
	return &config
}

// UpdateSearchConfig updates the search configuration with non-nil fields.
func UpdateSearchConfig(provider, apiKey, description *string, isActive *bool) *model.SearchConfig {
	config := GetSearchConfig()

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
		if err := database.DB.Model(config).Updates(updates).Error; err != nil {
			applogger.Error("failed to update search config", "error", err)
		}
		if err := database.DB.First(config, 1).Error; err != nil {
			applogger.Warn("failed to refresh search config after update", "error", err)
		}
	}

	applogger.Info("SearchConfig updated",
		"provider", config.Provider,
		"is_active", config.IsActive,
		"has_api_key", config.APIKey != "",
	)
	return config
}

// GetEmbeddingConfig retrieves the global embedding configuration (first row).
// Returns nil if no embedding config exists at all.
func GetEmbeddingConfig() *model.EmbeddingConfig {
	var config model.EmbeddingConfig
	if err := database.DB.Order("id ASC").First(&config).Error; err != nil {
		applogger.Warn("No embedding config found, embedding-dependent features unavailable")
		return nil
	}
	return &config
}

// IsEmbeddingConfigured returns true if an embedding config exists and has
// a non-empty API key.
func IsEmbeddingConfigured() bool {
	cfg := GetEmbeddingConfig()
	return cfg != nil && cfg.APIKey != ""
}

// UpdateEmbeddingConfig updates the global embedding configuration.
// If no config exists, creates one; otherwise updates the first row.
func UpdateEmbeddingConfig(req model.EmbeddingConfig) *model.EmbeddingConfig {
	config := GetEmbeddingConfig()
	if config == nil {
		if err := database.DB.Create(&req).Error; err != nil {
			applogger.Error("Failed to create embedding config", "error", err)
			return nil
		}
		config = &req
	} else {
		if err := database.DB.Model(config).Updates(req).Error; err != nil {
			applogger.Error("Failed to update embedding config", "error", err)
			return nil
		}
		if err := database.DB.First(config, config.ID).Error; err != nil {
			applogger.Warn("failed to refresh embedding config after update", "id", config.ID, "error", err)
		}
	}

	applogger.Info("Embedding config updated",
		"name", config.Name,
		"model", config.ModelID,
	)
	return config
}

// GetUserProfile retrieves the user profile for the primary user (id=1).
// Returns nil if the user has not been set up yet.
func GetUserProfile() *model.User {
	var user model.User
	if err := database.DB.Where("id = ?", 1).First(&user).Error; err != nil {
		return nil
	}
	return &user
}

// CreateUser creates the initial user profile.
// Name is immutable once set. Returns error on duplicate.
func CreateUser(name, bio string) (*model.User, error) {
	user := model.User{Name: name, Bio: bio}
	if err := database.DB.Create(&user).Error; err != nil {
		return nil, err
	}
	applogger.Info("User profile created", "name", name)
	return &user, nil
}

// GetUserName returns the primary user's name (id=1).
func GetUserName() string {
	var user model.User
	if err := database.DB.Where("id = ?", 1).Select("name").First(&user).Error; err != nil {
		applogger.Warn("failed to load user name", "error", err)
		return ""
	}
	return user.Name
}
