package schema

import (
	"time"

	"private-buddy-server/internal/model"
)

type LLMConfigBase struct {
	Name        string  `json:"name" binding:"required"`
	ModelID     string  `json:"model_id" binding:"required"`
	BaseURL     string  `json:"base_url" binding:"required"`
	APIKey      string  `json:"api_key" binding:"required"`
	Description *string `json:"description"`
}

type LLMConfigCreate LLMConfigBase

type LLMConfigUpdate struct {
	Name        *string `json:"name"`
	ModelID     *string `json:"model_id"`
	BaseURL     *string `json:"base_url"`
	APIKey      *string `json:"api_key"`
	Description *string `json:"description"`
}

type LLMConfigResponse struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	ModelID     string    `json:"model_id"`
	BaseURL     string    `json:"base_url"`
	APIKey      string    `json:"api_key"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func NewLLMConfigResponse(m *model.LLMConfig) *LLMConfigResponse {
	return &LLMConfigResponse{
		ID:          m.ID,
		Name:        m.Name,
		ModelID:     m.ModelID,
		BaseURL:     m.BaseURL,
		APIKey:      m.APIKey,
		Description: m.Description,
		CreatedAt:   m.CreatedAt,
		UpdatedAt:   m.UpdatedAt,
	}
}

func NewLLMConfigResponseList(entities []model.LLMConfig) []*LLMConfigResponse {
	result := make([]*LLMConfigResponse, 0, len(entities))
	for i := range entities {
		result = append(result, NewLLMConfigResponse(&entities[i]))
	}
	return result
}

func (req *LLMConfigUpdate) BuildUpdates() map[string]interface{} {
	updates := make(map[string]interface{})
	if req.Name != nil {
		updates["name"] = *req.Name
	}
	if req.ModelID != nil {
		updates["model_id"] = *req.ModelID
	}
	if req.BaseURL != nil {
		updates["base_url"] = *req.BaseURL
	}
	if req.APIKey != nil {
		updates["api_key"] = *req.APIKey
	}
	if req.Description != nil {
		updates["description"] = *req.Description
	}
	return updates
}
