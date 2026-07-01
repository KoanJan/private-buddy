package service

import (
	"fmt"
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

// GetAgent retrieves an agent by ID.
func GetAgent(agentID int64) (*model.Agent, error) {
	var agent model.Agent
	if err := database.DB.First(&agent, agentID).Error; err != nil {
		return nil, fmt.Errorf("agent %d: %w", agentID, err)
	}
	return &agent, nil
}

// GetLLMConfig retrieves an LLM config by ID.
func GetLLMConfig(llmConfigID int64) (*model.LLMConfig, error) {
	var config model.LLMConfig
	if err := database.DB.First(&config, llmConfigID).Error; err != nil {
		return nil, fmt.Errorf("llm_config %d: %w", llmConfigID, err)
	}
	return &config, nil
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

// GetSystemLLMConfig returns the system-level LLM config (id=1 row).
// Returns nil if no system LLM config has been configured.
func GetSystemLLMConfig() *model.LLMConfig {
	var sysCfg model.SystemLLMConfig
	if err := database.DB.Where("id = ?", 1).First(&sysCfg).Error; err != nil {
		applogger.Warn("System LLM config not configured, system-level LLM operations unavailable")
		return nil
	}

	cfg, err := GetLLMConfig(sysCfg.LLMConfigID)
	if err != nil {
		applogger.Error("System LLM config references invalid llm_config_id",
			"llm_config_id", sysCfg.LLMConfigID,
			"error", err,
		)
		return nil
	}
	return cfg
}

// IsSystemLLMConfigured returns true if a system-level LLM config has been set
// and its referenced LLM config is still valid.
func IsSystemLLMConfigured() bool {
	return GetSystemLLMConfig() != nil
}

// UpdateSystemLLMConfig upserts the system-level LLM config (single row, id=1).
// llmConfigID is the ID of an existing LLM config in the llm_configs table.
func UpdateSystemLLMConfig(llmConfigID int64) error {
	var sysCfg model.SystemLLMConfig
	err := database.DB.Where("id = ?", 1).First(&sysCfg).Error
	if err != nil {
		// Create the row if it doesn't exist
		sysCfg = model.SystemLLMConfig{
			LLMConfigID: llmConfigID,
		}
		if createErr := database.DB.Create(&sysCfg).Error; createErr != nil {
			return createErr
		}
	} else {
		sysCfg.LLMConfigID = llmConfigID
		if updateErr := database.DB.Save(&sysCfg).Error; updateErr != nil {
			return updateErr
		}
	}
	applogger.Info("System LLM config updated", "llm_config_id", llmConfigID)
	return nil
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
