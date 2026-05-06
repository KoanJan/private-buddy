package schema

import (
	"time"

	"private-buddy-server/internal/model"
)

type SearchConfigUpdate struct {
	Provider    *string `json:"provider"`
	APIKey      *string `json:"api_key"`
	Description *string `json:"description"`
	IsActive    *bool   `json:"is_active"`
}

type SearchConfigResponse struct {
	ID          int64     `json:"id"`
	Provider    string    `json:"provider"`
	APIKey      string    `json:"api_key"`
	Description string    `json:"description"`
	IsActive    bool      `json:"is_active"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func NewSearchConfigResponse(m *model.SearchConfig) *SearchConfigResponse {
	return &SearchConfigResponse{
		ID:          m.ID,
		Provider:    m.Provider,
		APIKey:      m.APIKey,
		Description: m.Description,
		IsActive:    m.IsActive,
		UpdatedAt:   m.UpdatedAt,
	}
}
