package schema

import "time"

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
