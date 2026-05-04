package schema

import "time"

type EmbeddingConfigBase struct {
	Name        string `json:"name" binding:"required"`
	ModelID     string `json:"model_id" binding:"required"`
	BaseURL     string `json:"base_url" binding:"required"`
	APIKey      string `json:"api_key" binding:"required"`
	Description string `json:"description"`
}

type EmbeddingConfigCreate EmbeddingConfigBase

type EmbeddingConfigUpdate struct {
	Name        *string `json:"name"`
	ModelID     *string `json:"model_id"`
	BaseURL     *string `json:"base_url"`
	APIKey      *string `json:"api_key"`
	Description *string `json:"description"`
}

type EmbeddingConfigResponse struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	ModelID     string    `json:"model_id"`
	BaseURL     string    `json:"base_url"`
	APIKey      string    `json:"api_key"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}
