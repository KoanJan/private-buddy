package schema

import (
	"time"

	"private-buddy-server/internal/model"
)

// InteractionResponse is the API response schema for an interaction record.
type InteractionResponse struct {
	ID        int64     `json:"id"`
	SessionID int64     `json:"session_id"`
	DraftID   int64     `json:"draft_id"`
	Iteration int       `json:"iteration"`
	Type      int       `json:"type"`
	UpdatedAt time.Time `json:"updated_at"`
	Data      string    `json:"data"`
	CreatedAt time.Time `json:"created_at"`
}

// InteractionListResponse is the API response for a list of interactions.
type InteractionListResponse struct {
	Interactions []InteractionResponse `json:"interactions"`
}

// InteractionStatusResponse is the API response for interaction status.
type InteractionStatusResponse struct {
	HasInteractions int `json:"has_interactions"`
}

// NewInteractionResponseList converts model interactions to API response list.
func NewInteractionResponseList(entities []model.Interaction) []InteractionResponse {
	result := make([]InteractionResponse, 0, len(entities))
	for _, m := range entities {
		result = append(result, InteractionResponse{
			ID:        m.ID,
			SessionID: m.SessionID,
			DraftID:   m.DraftID,
			Iteration: m.Iteration,
			Type:      m.Type,
			UpdatedAt: m.UpdatedAt,
			Data:      m.Data,
			CreatedAt: m.CreatedAt,
		})
	}
	return result
}
