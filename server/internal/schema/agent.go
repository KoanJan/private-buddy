package schema

import (
	"time"

	"private-buddy-server/internal/model"
)

type AgentBase struct {
	Name              string `json:"name" binding:"required"`
	CharacterSettings string `json:"character_settings"`
	LLMConfigID       int64  `json:"llm_config_id" binding:"required"`
	EmbeddingConfigID int64  `json:"embedding_config_id"`
	Description       string `json:"description"`
	Avatar            string `json:"avatar"`
}

type AgentCreate AgentBase

type AgentUpdate struct {
	Name              *string `json:"name"`
	CharacterSettings *string `json:"character_settings"`
	LLMConfigID       *int64  `json:"llm_config_id"`
	EmbeddingConfigID *int64  `json:"embedding_config_id"`
	Description       *string `json:"description"`
	Avatar            *string `json:"avatar"`
}

type AgentResponse struct {
	ID                int64     `json:"id"`
	Name              string    `json:"name"`
	CharacterSettings string    `json:"character_settings"`
	LLMConfigID       int64     `json:"llm_config_id"`
	EmbeddingConfigID int64     `json:"embedding_config_id"`
	Description       string    `json:"description"`
	Avatar            string    `json:"avatar"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

type SessionBrief struct {
	ID        int64     `json:"id"`
	Title     string    `json:"title"`
	Status    int       `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type AgentWithSessions struct {
	AgentResponse
	Sessions []SessionBrief `json:"sessions"`
}

func NewAgentResponse(m *model.Agent) *AgentResponse {
	return &AgentResponse{
		ID:                m.ID,
		Name:              m.Name,
		CharacterSettings: m.CharacterSettings,
		LLMConfigID:       m.LLMConfigID,
		EmbeddingConfigID: m.EmbeddingConfigID,
		Description:       m.Description,
		Avatar:            m.Avatar,
		CreatedAt:         m.CreatedAt,
		UpdatedAt:         m.UpdatedAt,
	}
}

func NewAgentResponseList(entities []model.Agent) []*AgentResponse {
	result := make([]*AgentResponse, 0, len(entities))
	for i := range entities {
		result = append(result, NewAgentResponse(&entities[i]))
	}
	return result
}

func NewSessionBriefList(entities []model.Session) []SessionBrief {
	result := make([]SessionBrief, 0, len(entities))
	for _, m := range entities {
		result = append(result, SessionBrief{
			ID:        m.ID,
			Title:     m.Title,
			Status:    m.Status,
			CreatedAt: m.CreatedAt,
			UpdatedAt: m.UpdatedAt,
		})
	}
	return result
}

func (req *AgentUpdate) BuildUpdates() map[string]interface{} {
	updates := make(map[string]interface{})
	if req.Name != nil {
		updates["name"] = *req.Name
	}
	if req.CharacterSettings != nil {
		updates["character_settings"] = *req.CharacterSettings
	}
	if req.LLMConfigID != nil {
		updates["llm_config_id"] = *req.LLMConfigID
	}
	if req.EmbeddingConfigID != nil {
		updates["embedding_config_id"] = *req.EmbeddingConfigID
	}
	if req.Description != nil {
		updates["description"] = *req.Description
	}
	if req.Avatar != nil {
		updates["avatar"] = *req.Avatar
	}
	return updates
}
