package schema

import "time"

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
