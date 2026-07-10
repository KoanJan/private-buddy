package schema

import (
	"encoding/json"
	"time"

	"private-buddy-server/internal/model"
)

type AgentBase struct {
	CharacterSettings string  `json:"character_settings"`
	LLMConfigID       int64   `json:"llm_config_id" binding:"required"`
	Avatar            string  `json:"avatar"`
	KnowledgeBaseIDs  []int64 `json:"knowledge_base_ids"`
}

type AgentCreate struct {
	Name              string  `json:"name" binding:"required"`
	Description       string  `json:"description"`
	CharacterSettings string  `json:"character_settings"`
	LLMConfigID       int64   `json:"llm_config_id" binding:"required"`
	Avatar            string  `json:"avatar"`
	KnowledgeBaseIDs  []int64 `json:"knowledge_base_ids"`
}

// AgentUpdate allows updating mutable agent fields and person-level fields.
type AgentUpdate struct {
	Name              *string  `json:"name"`
	Bio               *string  `json:"bio"`
	CharacterSettings *string  `json:"character_settings"`
	LLMConfigID       *int64   `json:"llm_config_id"`
	Avatar            *string  `json:"avatar"`
	KnowledgeBaseIDs  *[]int64 `json:"knowledge_base_ids"`
}

type AgentResponse struct {
	ID                int64     `json:"id"`
	PersonID          int64     `json:"person_id"`
	Name              string    `json:"name"`
	Bio               string    `json:"bio"`
	CharacterSettings string    `json:"character_settings"`
	LLMConfigID       int64     `json:"llm_config_id"`
	Avatar            string    `json:"avatar"`
	KnowledgeBaseIDs  []int64   `json:"knowledge_base_ids"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

type SessionBrief struct {
	ID        int64     `json:"id"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type AgentWithSessions struct {
	AgentResponse
	Sessions []SessionBrief `json:"sessions"`
}

func NewAgentResponse(m *model.Agent, person *model.Person) *AgentResponse {
	var kbIDs []int64
	if m.KnowledgeBaseIDs != "" && m.KnowledgeBaseIDs != "[]" {
		json.Unmarshal([]byte(m.KnowledgeBaseIDs), &kbIDs)
	}
	if kbIDs == nil {
		kbIDs = []int64{}
	}
	name := ""
	bio := ""
	if person != nil {
		name = person.Name
		bio = person.Bio
	}
	return &AgentResponse{
		ID:                m.ID,
		PersonID:          m.PersonID,
		Name:              name,
		Bio:               bio,
		CharacterSettings: m.CharacterSettings,
		LLMConfigID:       m.LLMConfigID,
		Avatar:            m.Avatar,
		KnowledgeBaseIDs:  kbIDs,
		CreatedAt:         m.CreatedAt,
		UpdatedAt:         m.UpdatedAt,
	}
}

func NewAgentResponseList(agents []model.Agent, persons map[int64]*model.Person) []*AgentResponse {
	result := make([]*AgentResponse, 0, len(agents))
	for i := range agents {
		result = append(result, NewAgentResponse(&agents[i], persons[agents[i].PersonID]))
	}
	return result
}

func NewSessionBriefList(entities []model.Session) []SessionBrief {
	result := make([]SessionBrief, 0, len(entities))
	for _, m := range entities {
		result = append(result, SessionBrief{
			ID:        m.ID,
			Title:     m.Title,
			CreatedAt: m.CreatedAt,
			UpdatedAt: m.UpdatedAt,
		})
	}
	return result
}

func (req *AgentUpdate) BuildUpdates() map[string]interface{} {
	updates := make(map[string]interface{})
	if req.CharacterSettings != nil {
		updates["character_settings"] = *req.CharacterSettings
	}
	if req.LLMConfigID != nil {
		updates["llm_config_id"] = *req.LLMConfigID
	}
	if req.Avatar != nil {
		updates["avatar"] = *req.Avatar
	}
	if req.KnowledgeBaseIDs != nil {
		data, _ := json.Marshal(*req.KnowledgeBaseIDs)
		updates["knowledge_base_ids"] = string(data)
	}
	return updates
}
