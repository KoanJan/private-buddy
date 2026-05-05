package handler

import (
	"os"
	"strconv"

	"private-buddy-server/internal/config"
	"private-buddy-server/internal/model"
	"private-buddy-server/internal/schema"

	"github.com/gin-gonic/gin"
)

// getPathID extracts an int64 "id" path parameter from the URL.
func getPathID(c *gin.Context) int64 {
	idStr := c.Param("id")
	id, _ := strconv.ParseInt(idStr, 10, 64)
	return id
}

// getPagination extracts skip and limit query parameters with defaults (0, 100).
func getPagination(c *gin.Context) (skip, limit int) {
	skip = 0
	limit = 100
	if s := c.Query("skip"); s != "" {
		if n, err := strconv.Atoi(s); err == nil {
			skip = n
		}
	}
	if l := c.Query("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 {
			limit = n
		}
	}
	return
}

// derefString safely dereferences a string pointer, returning "" for nil.
func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// getAvatarsDir returns the avatars directory path from configuration.
func getAvatarsDir() string {
	return config.Get().GetAvatarsDir()
}

// osRemoveIfExists removes a file, ignoring errors if it doesn't exist.
func osRemoveIfExists(path string) {
	os.Remove(path)
}

// removeSessionWorkspace removes the workspace directory for a session.
func removeSessionWorkspace(sessionID int64) {
	settings := config.Get()
	workspaceDir := settings.GetWorkspaceRoot() + "/" + strconv.FormatInt(sessionID, 10)
	os.RemoveAll(workspaceDir)
}

// toLLMConfigResponse converts an LLMConfig model to its API response schema.
func toLLMConfigResponse(m *model.LLMConfig) *schema.LLMConfigResponse {
	return &schema.LLMConfigResponse{
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

// toLLMConfigResponseList converts a slice of LLMConfig models to API response schemas.
func toLLMConfigResponseList(entities []model.LLMConfig) []*schema.LLMConfigResponse {
	result := make([]*schema.LLMConfigResponse, 0, len(entities))
	for i := range entities {
		result = append(result, toLLMConfigResponse(&entities[i]))
	}
	return result
}

// buildLLMConfigUpdates builds a map of non-nil fields from an update request for partial updates.
func buildLLMConfigUpdates(req *schema.LLMConfigUpdate) map[string]interface{} {
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

func toEmbeddingConfigResponse(m *model.EmbeddingConfig) *schema.EmbeddingConfigResponse {
	return &schema.EmbeddingConfigResponse{
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

func toEmbeddingConfigResponseList(entities []model.EmbeddingConfig) []*schema.EmbeddingConfigResponse {
	result := make([]*schema.EmbeddingConfigResponse, 0, len(entities))
	for i := range entities {
		result = append(result, toEmbeddingConfigResponse(&entities[i]))
	}
	return result
}

func buildEmbeddingConfigUpdates(req *schema.EmbeddingConfigUpdate) map[string]interface{} {
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

func toAgentResponse(m *model.Agent) *schema.AgentResponse {
	return &schema.AgentResponse{
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

func toAgentResponseList(entities []model.Agent) []*schema.AgentResponse {
	result := make([]*schema.AgentResponse, 0, len(entities))
	for i := range entities {
		result = append(result, toAgentResponse(&entities[i]))
	}
	return result
}

func buildAgentUpdates(req *schema.AgentUpdate) map[string]interface{} {
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

func toSessionResponse(m *model.Session) *schema.SessionResponse {
	return &schema.SessionResponse{
		ID:        m.ID,
		Title:     m.Title,
		AgentID:   m.AgentID,
		Status:    m.Status,
		CreatedAt: m.CreatedAt,
		UpdatedAt: m.UpdatedAt,
	}
}

func toSessionResponseList(entities []model.Session) []*schema.SessionResponse {
	result := make([]*schema.SessionResponse, 0, len(entities))
	for i := range entities {
		result = append(result, toSessionResponse(&entities[i]))
	}
	return result
}

func toSessionBriefList(entities []model.Session) []schema.SessionBrief {
	result := make([]schema.SessionBrief, 0, len(entities))
	for _, m := range entities {
		result = append(result, schema.SessionBrief{
			ID:        m.ID,
			Title:     m.Title,
			Status:    m.Status,
			CreatedAt: m.CreatedAt,
			UpdatedAt: m.UpdatedAt,
		})
	}
	return result
}

func buildSessionUpdates(req *schema.SessionUpdate) map[string]interface{} {
	updates := make(map[string]interface{})
	if req.Title != nil {
		updates["title"] = *req.Title
	}
	if req.AgentID != nil {
		updates["agent_id"] = *req.AgentID
	}
	return updates
}

func toMessageResponse(m *model.Message) *schema.MessageResponse {
	return &schema.MessageResponse{
		ID:              m.ID,
		SessionID:       m.SessionID,
		Role:            m.Role,
		Content:         m.Content,
		Status:          m.Status,
		HasInteractions: m.HasInteractions,
		CreatedAt:       m.CreatedAt,
		UpdatedAt:       m.UpdatedAt,
	}
}

func toMessageResponseList(entities []model.Message) []*schema.MessageResponse {
	result := make([]*schema.MessageResponse, 0, len(entities))
	for i := range entities {
		result = append(result, toMessageResponse(&entities[i]))
	}
	return result
}

func toSearchConfigResponse(m *model.SearchConfig) *schema.SearchConfigResponse {
	return &schema.SearchConfigResponse{
		ID:          m.ID,
		Provider:    m.Provider,
		APIKey:      m.APIKey,
		Description: m.Description,
		IsActive:    m.IsActive,
		UpdatedAt:   m.UpdatedAt,
	}
}

func toInteractionResponseList(entities []model.Interaction) []schema.InteractionResponse {
	result := make([]schema.InteractionResponse, 0, len(entities))
	for _, m := range entities {
		result = append(result, schema.InteractionResponse{
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
