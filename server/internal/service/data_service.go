package service

import (
	"encoding/json"
	"fmt"

	"private-buddy-server/internal/database"
	applogger "private-buddy-server/internal/logger"
	"private-buddy-server/internal/model"

	"gorm.io/gorm"
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

// GetAgentConfig retrieves an agent config by ID.
func GetAgentConfig(agentConfigID int64) (*model.AgentConfig, error) {
	var ac model.AgentConfig
	if err := database.DB.First(&ac, agentConfigID).Error; err != nil {
		return nil, fmt.Errorf("agent config %d: %w", agentConfigID, err)
	}
	return &ac, nil
}

// GetAgentConfigWithPerson retrieves an agent config with its associated Person.
func GetAgentConfigWithPerson(agentConfigID int64) (*model.AgentConfig, *model.Person, error) {
	agent, err := GetAgentConfig(agentConfigID)
	if err != nil {
		return nil, nil, err
	}
	person, err := GetPerson(agent.PersonID)
	if err != nil {
		return nil, nil, err
	}
	return agent, person, nil
}

// GetAgentConfigByPersonID retrieves an agent config by PersonID.
func GetAgentConfigByPersonID(personID int64) (*model.AgentConfig, error) {
	var ac model.AgentConfig
	if err := database.DB.Where("person_id = ?", personID).First(&ac).Error; err != nil {
		return nil, fmt.Errorf("agent config with person_id %d: %w", personID, err)
	}
	return &ac, nil
}

// GetAgentConfigName returns the name of an agent config via its Person record.
func GetAgentConfigName(agentConfigID int64) string {
	agent, err := GetAgentConfig(agentConfigID)
	if err != nil {
		return ""
	}
	person, err := GetPerson(agent.PersonID)
	if err != nil {
		return ""
	}
	return person.Name
}

// GetAgentConfigPersonID returns the PersonID associated with an agent config.
func GetAgentConfigPersonID(agentConfigID int64) int64 {
	agent, err := GetAgentConfig(agentConfigID)
	if err != nil {
		return 0
	}
	return agent.PersonID
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
		applogger.Error("SearchConfig not found, creating default")
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
			applogger.Error("failed to refresh search config after update", "error", err)
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
		applogger.Error("No embedding config found, embedding-dependent features unavailable")
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
			applogger.Error("failed to refresh embedding config after update", "id", config.ID, "error", err)
		}
	}

	applogger.Info("Embedding config updated",
		"name", config.Name,
		"model", config.ModelID,
	)
	return config
}

// Person

// GetPerson retrieves a person by ID.
func GetPerson(personID int64) (*model.Person, error) {
	var person model.Person
	if err := database.DB.First(&person, personID).Error; err != nil {
		return nil, fmt.Errorf("person %d: %w", personID, err)
	}
	return &person, nil
}

// GetPersonByName retrieves a person by name.
func GetPersonByName(name string) (*model.Person, error) {
	var person model.Person
	if err := database.DB.Where("name = ?", name).First(&person).Error; err != nil {
		return nil, fmt.Errorf("person %q: %w", name, err)
	}
	return &person, nil
}

// GetCurrentUserPerson returns the current human user's Person record.
// In the current single-user design, this returns the unique type=2 person.
func GetCurrentUserPerson() (*model.Person, error) {
	var person model.Person
	if err := database.DB.Where("type = ?", model.PersonTypeHuman).First(&person).Error; err != nil {
		return nil, err
	}
	return &person, nil
}

// GetCurrentUserPersonID returns the current human user's PersonID.
func GetCurrentUserPersonID() (int64, error) {
	person, err := GetCurrentUserPerson()
	if err != nil {
		return 0, err
	}
	return person.ID, nil
}

// GetUserName returns the primary human user's name.
func GetUserName() string {
	person, err := GetCurrentUserPerson()
	if err != nil {
		applogger.Error("failed to load current user person", "error", err)
		return ""
	}
	return person.Name
}

// GetSystemLLMConfig returns the system-level LLM config (id=1 row).
// Returns nil if no system LLM config has been configured.
func GetSystemLLMConfig() *model.LLMConfig {
	var sysCfg model.SystemLLMConfig
	if err := database.DB.Where("id = ?", 1).First(&sysCfg).Error; err != nil {
		applogger.Error("System LLM config not configured, system-level LLM operations unavailable")
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
		applogger.Error("system LLM config not found, creating new record", "error", err)
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

// ---- Transactional composite operations ----

// CreateAgentConfigWithPerson creates a Person (type=AI) and an AgentConfig in a single transaction.
// Returns the created AgentConfig and Person.
func CreateAgentConfigWithPerson(name, bio string, characterSettings string, llmConfigID int64, avatar string, knowledgeBaseIDs []int64) (*model.AgentConfig, *model.Person, error) {
	var agent *model.AgentConfig
	var person *model.Person

	err := database.DB.Transaction(func(tx *gorm.DB) error {
		p := model.Person{
			Name: name,
			Bio:  bio,
			Type: model.PersonTypeAI,
		}
		if err := tx.Select("Name", "Bio", "Type").Create(&p).Error; err != nil {
			return fmt.Errorf("create person: %w", err)
		}

		kbIDsJSON := "[]"
		if len(knowledgeBaseIDs) > 0 {
			data, _ := json.Marshal(knowledgeBaseIDs)
			kbIDsJSON = string(data)
		}

		a := model.AgentConfig{
			PersonID:          p.ID,
			CharacterSettings: characterSettings,
			LLMConfigID:       llmConfigID,
			Avatar:            avatar,
			KnowledgeBaseIDs:  kbIDsJSON,
		}
		if err := tx.Select("PersonID", "CharacterSettings", "LLMConfigID", "Avatar", "KnowledgeBaseIDs").Create(&a).Error; err != nil {
			return fmt.Errorf("create agent: %w", err)
		}

		agent = &a
		person = &p
		return nil
	})

	return agent, person, err
}

// DeleteAgentConfigCascade deletes an agent config and all associated data in a single transaction.
// Workspace cleanup (filesystem) remains the caller's responsibility.
func DeleteAgentConfigCascade(personID int64) (sessionIDs []int64, err error) {
	err = database.DB.Transaction(func(tx *gorm.DB) error {
		// Collect session IDs via participant_sessions.
		if err := tx.Raw(`SELECT ps.session_id FROM participant_sessions ps
			WHERE ps.participant_id = ?`, personID).Pluck("session_id", &sessionIDs).Error; err != nil {
			return fmt.Errorf("pluck sessions via participant_sessions: %w", err)
		}

		if len(sessionIDs) > 0 {
			// NOTE: This logic assumes 1v1 (one agent per session).
			// In multi-agent/group chat, deleting one agent should NOT cascade delete the entire session.
			tables := []interface{}{
				&model.Work{}, &model.MessageDraft{}, &model.Interaction{},
				&model.AgentNarrative{}, &model.Summary{},
				&model.ParticipantSession{}, &model.Message{},
			}
			for _, table := range tables {
				if err := tx.Where("session_id IN ?", sessionIDs).Delete(table).Error; err != nil {
					return err
				}
			}
			if err := tx.Where("id IN ?", sessionIDs).Delete(&model.Session{}).Error; err != nil {
				return err
			}
			if err := tx.Where("session_id IN ?", sessionIDs).Delete(&model.ScheduledEvent{}).Error; err != nil {
				return err
			}
		}

		// Agent-level memory and cognition — now keyed by person_id.
		if err := tx.Where("person_id = ?", personID).Delete(&model.AgentObservation{}).Error; err != nil {
			return err
		}
		if err := tx.Where("person_id = ?", personID).Delete(&model.EntityProfile{}).Error; err != nil {
			return err
		}

		// Delete agent config and person
		if err := tx.Where("person_id = ?", personID).Delete(&model.AgentConfig{}).Error; err != nil {
			return err
		}
		if err := tx.Delete(&model.Person{}, personID).Error; err != nil {
			return err
		}

		return nil
	})

	return sessionIDs, err
}

// DeleteSessionCascade deletes a session and all associated data in a transaction.
// Returns the first AI agent's PersonID for caller's workspace cleanup, and 0 for
// the legacy agentConfigID (caller ignores it).
func DeleteSessionCascade(sessionID int64) (personID int64, agentConfigID int64, err error) {
	err = database.DB.Transaction(func(tx *gorm.DB) error {
		var sess model.Session
		if err := tx.First(&sess, sessionID).Error; err != nil {
			return fmt.Errorf("session %d not found: %w", sessionID, err)
		}

		// Resolve the first AI agent's PersonID from participant_sessions
		// for workspace cleanup.
		var aiPersonID int64
		if err := tx.Raw(`SELECT ac.person_id FROM participant_sessions ps
			JOIN persons p ON p.id = ps.participant_id AND p.type = 1
			JOIN agent_configs ac ON ac.person_id = p.id
			WHERE ps.session_id = ?
			LIMIT 1`, sessionID).Scan(&aiPersonID).Error; err != nil {
			applogger.Error("failed to find agent person for session during cleanup",
				"session_id", sessionID, "error", err)
		}
		personID = aiPersonID

		tables := []interface{}{
			&model.Work{}, &model.MessageDraft{}, &model.Interaction{},
			&model.AgentNarrative{}, &model.Summary{},
			&model.ParticipantSession{}, &model.Message{},
		}
		for _, table := range tables {
			if err := tx.Where("session_id = ?", sessionID).Delete(table).Error; err != nil {
				return err
			}
		}
		if err := tx.Delete(&sess).Error; err != nil {
			return err
		}

		return nil
	})

	return personID, agentConfigID, err
}

// ---- Query helpers ----

// HasInteractions checks whether any task interaction records exist for the given session.
func HasInteractions(sessionID int64) (bool, error) {
	var count int64
	if err := database.DB.Model(&model.Interaction{}).
		Where("session_id = ?", sessionID).
		Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

// GetSessionParticipantsByPersonType returns participant_sessions joined with persons filtered by type.
func GetSessionParticipantsByPersonType(sessionID int64, personType int) ([]model.ParticipantSession, error) {
	var p []model.ParticipantSession
	err := database.DB.Where(
		"session_id = ? AND participant_id IN (SELECT id FROM persons WHERE type = ?)",
		sessionID, personType,
	).Find(&p).Error
	return p, err
}

// GetSessionParticipantsByPersonTypeMulti is like GetSessionParticipantsByPersonType but for multiple sessions.
func GetSessionParticipantsByPersonTypeMulti(sessionIDs []int64, personType int) ([]model.ParticipantSession, error) {
	if len(sessionIDs) == 0 {
		return nil, nil
	}
	var p []model.ParticipantSession
	err := database.DB.Where(
		"session_id IN ? AND participant_id IN (SELECT id FROM persons WHERE type = ?)",
		sessionIDs, personType,
	).Find(&p).Error
	return p, err
}

// GetSessionAIParticipantIDs returns the person IDs of all AI participants in a session.
func GetSessionAIParticipantIDs(sessionID int64) ([]int64, error) {
	var ids []int64
	err := database.DB.Model(&model.ParticipantSession{}).
		Where("session_id = ? AND participant_id IN (SELECT id FROM persons WHERE type = ?)", sessionID, model.PersonTypeAI).
		Pluck("participant_id", &ids).Error
	return ids, err
}

// GetSessionAIParticipantIDsMulti is like GetSessionAIParticipantIDs but for multiple sessions.
func GetSessionAIParticipantIDsMulti(sessionIDs []int64) ([]int64, error) {
	if len(sessionIDs) == 0 {
		return nil, nil
	}
	var ids []int64
	err := database.DB.Model(&model.ParticipantSession{}).
		Where("session_id IN ? AND participant_id IN (SELECT id FROM persons WHERE type = ?)", sessionIDs, model.PersonTypeAI).
		Pluck("participant_id", &ids).Error
	return ids, err
}
