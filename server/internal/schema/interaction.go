package schema

import (
	"time"

	"private-buddy-server/internal/model"
)

type InteractionResponse struct {
	ID         int64     `json:"id"`
	SessionID  int64     `json:"session_id"`
	UserMsgID  int64     `json:"user_msg_id"`
	AgentMsgID int64     `json:"agent_msg_id"`
	Iteration  int       `json:"iteration"`
	Type       int       `json:"type"`
	UpdatedAt  time.Time `json:"updated_at"`
	Data       string    `json:"data"`
	CreatedAt  time.Time `json:"created_at"`
}

type InteractionListResponse struct {
	Interactions []InteractionResponse `json:"interactions"`
}

type InteractionStatusResponse struct {
	HasInteractions int `json:"has_interactions"`
}

func NewInteractionResponseList(entities []model.Interaction) []InteractionResponse {
	result := make([]InteractionResponse, 0, len(entities))
	for _, m := range entities {
		result = append(result, InteractionResponse{
			ID:         m.ID,
			SessionID:  m.SessionID,
			UserMsgID:  m.UserMsgID,
			AgentMsgID: m.AgentMsgID,
			Iteration:  m.Iteration,
			Type:       m.Type,
			UpdatedAt:  m.UpdatedAt,
			Data:       m.Data,
			CreatedAt:  m.CreatedAt,
		})
	}
	return result
}
