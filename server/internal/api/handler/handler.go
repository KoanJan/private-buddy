package handler

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"private-buddy-server/internal/api/response"
	"private-buddy-server/internal/database"
	applogger "private-buddy-server/internal/logger"
	"private-buddy-server/internal/model"
	"private-buddy-server/internal/schema"
	"private-buddy-server/internal/service"
	"private-buddy-server/internal/service/memory"
	"private-buddy-server/internal/service/runtime"
)

type Handler struct {
	crudLLM     *service.CRUDBase[model.LLMConfig]
	crudAgent   *service.CRUDBase[model.Agent]
	crudSession *service.CRUDBase[model.Session]
}

func NewHandler() *Handler {
	return &Handler{
		crudLLM:     service.NewCRUDBase[model.LLMConfig]("LLM config"),
		crudAgent:   service.NewCRUDBase[model.Agent]("Agent"),
		crudSession: service.NewCRUDBase[model.Session]("Session"),
	}
}

func (h *Handler) Root(c *gin.Context) {
	response.SuccessMessage(c, "Private Buddy API is running", nil)
}

func (h *Handler) GetVersion(c *gin.Context) {
	var versionRecord model.DBVersion
	err := database.DB.Order("id DESC").First(&versionRecord).Error
	version := "0.0.0"
	if err == nil {
		version = versionRecord.Version
	}
	response.Success(c, gin.H{"version": version})
}

func (h *Handler) CreateLLMConfig(c *gin.Context) {
	var req schema.LLMConfigCreate
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	entity := model.LLMConfig{
		Name:        req.Name,
		ModelID:     req.ModelID,
		BaseURL:     req.BaseURL,
		APIKey:      req.APIKey,
		Description: derefString(req.Description),
	}
	if err := database.DB.Select(
		"Name", "ModelID", "BaseURL", "APIKey", "Description",
	).Create(&entity).Error; err != nil {
		response.InternalError(c, err.Error())
		return
	}
	response.Success(c, schema.NewLLMConfigResponse(&entity))
}

func (h *Handler) ListLLMConfigs(c *gin.Context) {
	skip, limit := getPagination(c)
	entities, err := h.crudLLM.GetMulti(skip, limit)
	if err != nil {
		response.InternalError(c, err.Error())
		return
	}
	response.Success(c, schema.NewLLMConfigResponseList(entities))
}

func (h *Handler) GetLLMConfig(c *gin.Context) {
	id := getPathID(c)
	entity, err := h.crudLLM.Get(id)
	if err != nil {
		handleNotFound(c, "LLM config", id)
		return
	}
	response.Success(c, schema.NewLLMConfigResponse(entity))
}

func (h *Handler) UpdateLLMConfig(c *gin.Context) {
	id := getPathID(c)
	entity, err := h.crudLLM.Get(id)
	if err != nil {
		handleNotFound(c, "LLM config", id)
		return
	}
	var req schema.LLMConfigUpdate
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	updates := req.BuildUpdates()
	if len(updates) > 0 {
		h.crudLLM.Update(entity, updates)
		if err := database.DB.First(entity, id).Error; err != nil {
			applogger.Warn("failed to refresh LLM config after update", "id", id, "error", err)
		}
	}
	response.Success(c, schema.NewLLMConfigResponse(entity))
}

func (h *Handler) DeleteLLMConfig(c *gin.Context) {
	id := getPathID(c)
	_, err := h.crudLLM.Get(id)
	if err != nil {
		handleNotFound(c, "LLM config", id)
		return
	}
	var referencingAgents []model.Agent
	if err := database.DB.Where("llm_config_id = ?", id).Find(&referencingAgents).Error; err != nil {
		applogger.Warn("failed to check referencing agents for LLM config", "id", id, "error", err)
	}
	if len(referencingAgents) > 0 {
		names := make([]string, len(referencingAgents))
		for i, a := range referencingAgents {
			names[i] = a.Name
		}
		response.BadRequest(c, "Cannot delete LLM config: it is referenced by "+strconv.Itoa(len(referencingAgents))+" agent(s): "+strings.Join(names, ", "))
		return
	}
	h.crudLLM.Delete(id)
	response.SuccessMessage(c, "LLM config deleted successfully", nil)
}

// GetEmbeddingConfig returns the global embedding configuration.
// Returns nil fields (zero values) if no config exists.
func (h *Handler) GetEmbeddingConfig(c *gin.Context) {
	config := service.GetEmbeddingConfig()
	if config == nil {
		// Return empty config so the UI can show an empty form for initial setup
		response.Success(c, schema.EmbeddingConfigResponse{})
		return
	}
	response.Success(c, schema.NewEmbeddingConfigResponse(config))
}

// UpdateEmbeddingConfig updates the global embedding configuration (upsert).
func (h *Handler) UpdateEmbeddingConfig(c *gin.Context) {
	var req schema.EmbeddingConfigUpdate
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	config := service.GetEmbeddingConfig()
	if config == nil {
		// Create new config
		entity := model.EmbeddingConfig{
			Name:        derefString(req.Name),
			ModelID:     derefString(req.ModelID),
			BaseURL:     derefString(req.BaseURL),
			APIKey:      derefString(req.APIKey),
			Description: derefString(req.Description),
		}
		config = service.UpdateEmbeddingConfig(entity)
	} else {
		entity := *config
		if req.Name != nil {
			entity.Name = *req.Name
		}
		if req.ModelID != nil {
			entity.ModelID = *req.ModelID
		}
		if req.BaseURL != nil {
			entity.BaseURL = *req.BaseURL
		}
		if req.APIKey != nil {
			entity.APIKey = *req.APIKey
		}
		if req.Description != nil {
			entity.Description = *req.Description
		}
		config = service.UpdateEmbeddingConfig(entity)
	}

	if config == nil {
		response.InternalError(c, "Failed to update embedding config")
		return
	}
	response.Success(c, schema.NewEmbeddingConfigResponse(config))
}

// GetUserProfile returns the current user's profile.
// Returns zero-value response if user hasn't been set up yet.
func (h *Handler) GetUserProfile(c *gin.Context) {
	user := service.GetUserProfile()
	if user == nil {
		response.Success(c, schema.UserProfileResponse{})
		return
	}
	response.Success(c, schema.NewUserProfileResponse(user))
}

// CreateOrUpdateUserProfile creates or updates the user profile.
// Name is immutable once set (controlled via UNIQUE constraint on name column).
// Bio can be updated at any time.
func (h *Handler) CreateOrUpdateUserProfile(c *gin.Context) {
	var req struct {
		Name string `json:"name" binding:"required"`
		Bio  string `json:"bio"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	existing := service.GetUserProfile()
	if existing != nil {
		// Update bio only (name is immutable)
		updates := map[string]interface{}{"bio": req.Bio}
		if err := database.DB.Model(existing).Updates(updates).Error; err != nil {
			response.InternalError(c, err.Error())
			return
		}
		if err := database.DB.First(existing, existing.ID).Error; err != nil {
			applogger.Warn("failed to refresh user profile after update", "id", existing.ID, "error", err)
		}
		response.Success(c, schema.NewUserProfileResponse(existing))
		return
	}

	user, err := service.CreateUser(req.Name, req.Bio)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint") {
			response.BadRequest(c, fmt.Sprintf("User name '%s' already exists", req.Name))
			return
		}
		response.InternalError(c, err.Error())
		return
	}
	response.Success(c, schema.NewUserProfileResponse(user))
}

func (h *Handler) CreateAgent(c *gin.Context) {
	var req schema.AgentCreate
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	kbIDsJSON := "[]"
	if len(req.KnowledgeBaseIDs) > 0 {
		data, _ := json.Marshal(req.KnowledgeBaseIDs)
		kbIDsJSON = string(data)
	}
	entity := model.Agent{
		Name:              req.Name,
		CharacterSettings: req.CharacterSettings,
		LLMConfigID:       req.LLMConfigID,
		Description:       req.Description,
		Avatar:            req.Avatar,
		KnowledgeBaseIDs:  kbIDsJSON,
	}
	if err := database.DB.Select(
		"Name", "CharacterSettings", "LLMConfigID", "Description", "Avatar", "KnowledgeBaseIDs",
	).Create(&entity).Error; err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint") {
			response.BadRequest(c, fmt.Sprintf("Agent name '%s' already exists", req.Name))
			return
		}
		response.InternalError(c, err.Error())
		return
	}

	// Register and start the agent's runtime so it can receive events immediately.
	runtime.StartRuntime(entity.ID)

	response.Success(c, schema.NewAgentResponse(&entity))
}

func (h *Handler) ListAgents(c *gin.Context) {
	skip, limit := getPagination(c)
	entities, err := h.crudAgent.GetMulti(skip, limit)
	if err != nil {
		response.InternalError(c, err.Error())
		return
	}
	response.Success(c, schema.NewAgentResponseList(entities))
}

func (h *Handler) ListAgentsWithSessions(c *gin.Context) {
	var agents []model.Agent
	if err := database.DB.Order("updated_at DESC").Find(&agents).Error; err != nil {
		applogger.Error("failed to list agents with sessions", "error", err)
		response.InternalError(c, "Failed to list agents")
		return
	}

	if len(agents) == 0 {
		response.Success(c, []schema.AgentWithSessions{})
		return
	}

	agentIDs := make([]int64, len(agents))
	for i, a := range agents {
		agentIDs[i] = a.ID
	}

	var allSessions []model.Session
	if err := database.DB.Where("agent_id IN ?", agentIDs).Order("updated_at DESC").Find(&allSessions).Error; err != nil {
		applogger.Warn("failed to load sessions for agent list, returning without sessions", "error", err)
	}

	sessionsByAgent := make(map[int64][]model.Session)
	for _, s := range allSessions {
		sessionsByAgent[s.AgentID] = append(sessionsByAgent[s.AgentID], s)
	}

	result := make([]schema.AgentWithSessions, 0, len(agents))
	for _, agent := range agents {
		sessions := sessionsByAgent[agent.ID]
		if sessions == nil {
			sessions = []model.Session{}
		}
		result = append(result, schema.AgentWithSessions{
			AgentResponse: *schema.NewAgentResponse(&agent),
			Sessions:      schema.NewSessionBriefList(sessions),
		})
	}
	response.Success(c, result)
}

func (h *Handler) GetAgent(c *gin.Context) {
	id := getPathID(c)
	entity, err := h.crudAgent.Get(id)
	if err != nil {
		handleNotFound(c, "Agent", id)
		return
	}
	response.Success(c, schema.NewAgentResponse(entity))
}

func (h *Handler) UpdateAgent(c *gin.Context) {
	id := getPathID(c)
	entity, err := h.crudAgent.Get(id)
	if err != nil {
		handleNotFound(c, "Agent", id)
		return
	}
	var req schema.AgentUpdate
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	updates := req.BuildUpdates()
	if len(updates) > 0 {
		h.crudAgent.Update(entity, updates)
		if err := database.DB.First(entity, id).Error; err != nil {
			applogger.Warn("failed to refresh agent after update", "id", id, "error", err)
		}
	}
	response.Success(c, schema.NewAgentResponse(entity))
}

func (h *Handler) DeleteAgent(c *gin.Context) {
	id := getPathID(c)
	agent, err := h.crudAgent.Get(id)
	if err != nil {
		handleNotFound(c, "Agent", id)
		return
	}

	if agent.Avatar != "" {
		avatarPath := getAvatarsDir() + "/" + agent.Avatar
		osRemoveIfExists(avatarPath)
	}

	var sessionIDs []int64
	if err := database.DB.Model(&model.Session{}).Where("agent_id = ?", id).Pluck("id", &sessionIDs).Error; err != nil {
		applogger.Warn("failed to pluck session IDs for agent deletion", "agent_id", id, "error", err)
	}

	if len(sessionIDs) > 0 {
		// NOTE: This logic assumes 1v1 (one agent per session).
		// In multi-agent/group chat, deleting one agent should NOT cascade delete the entire session.
		// This will need to be revisited when group chat is implemented.

		// Delete all agent-related resources in dependency order.
		// Each delete is checked independently — one failure should not block the rest.
		// 1. Works (may reference drafts)
		// 2. Message drafts
		// 3. Interactions
		// 4. Agent narratives + session summaries
		// 5. Participant sessions
		// 6. Messages
		// 7. Sessions
		if err := database.DB.Where("session_id IN ?", sessionIDs).Delete(&model.Work{}).Error; err != nil {
			applogger.Error("DeleteAgent: failed to delete works", "agent_id", id, "error", err)
		}
		if err := database.DB.Where("session_id IN ?", sessionIDs).Delete(&model.MessageDraft{}).Error; err != nil {
			applogger.Error("DeleteAgent: failed to delete message drafts", "agent_id", id, "error", err)
		}
		if err := database.DB.Where("session_id IN ?", sessionIDs).Delete(&model.Interaction{}).Error; err != nil {
			applogger.Error("DeleteAgent: failed to delete interactions", "agent_id", id, "error", err)
		}
		if err := database.DB.Where("session_id IN ?", sessionIDs).Delete(&model.AgentNarrative{}).Error; err != nil {
			applogger.Error("DeleteAgent: failed to delete agent narratives", "agent_id", id, "error", err)
		}
		if err := database.DB.Where("session_id IN ?", sessionIDs).Delete(&model.Summary{}).Error; err != nil {
			applogger.Error("DeleteAgent: failed to delete session summaries", "agent_id", id, "error", err)
		}
		if err := database.DB.Where("session_id IN ?", sessionIDs).Delete(&model.ParticipantSession{}).Error; err != nil {
			applogger.Error("DeleteAgent: failed to delete participant sessions", "agent_id", id, "error", err)
		}
		if err := database.DB.Where("session_id IN ?", sessionIDs).Delete(&model.Message{}).Error; err != nil {
			applogger.Error("DeleteAgent: failed to delete messages", "agent_id", id, "error", err)
		}
		if err := database.DB.Where("agent_id = ?", id).Delete(&model.Session{}).Error; err != nil {
			applogger.Error("DeleteAgent: failed to delete sessions", "agent_id", id, "error", err)
		}
		if err := database.DB.Where("session_id IN ?", sessionIDs).Delete(&model.ScheduledEvent{}).Error; err != nil {
			applogger.Error("DeleteAgent: failed to delete scheduled events", "agent_id", id, "error", err)
		}

		for _, sid := range sessionIDs {
			removeSessionWorkspace(sid)
		}
	}

	// Delete agent-level memory and cognition (not session-scoped)
	if err := database.DB.Where("agent_id = ?", id).Delete(&model.AgentObservation{}).Error; err != nil {
		applogger.Error("DeleteAgent: failed to delete agent observations", "agent_id", id, "error", err)
	}
	if err := database.DB.Where("agent_id = ?", id).Delete(&model.EntityProfile{}).Error; err != nil {
		applogger.Error("DeleteAgent: failed to delete entity profiles", "agent_id", id, "error", err)
	}

	h.crudAgent.Delete(id)
	response.SuccessMessage(c, "Agent deleted successfully", nil)
}

func (h *Handler) CreateSession(c *gin.Context) {
	var req schema.SessionCreate
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	title := ""
	if req.Title != nil {
		title = *req.Title
	}
	entity := model.Session{
		Title:   title,
		AgentID: req.AgentID,
	}
	if err := database.DB.Select("Title", "AgentID").Create(&entity).Error; err != nil {
		response.InternalError(c, err.Error())
		return
	}
	response.Success(c, schema.NewSessionResponse(&entity))
}

func (h *Handler) ListSessions(c *gin.Context) {
	skip, limit := getPagination(c)
	entities, err := h.crudSession.GetMulti(skip, limit)
	if err != nil {
		response.InternalError(c, err.Error())
		return
	}
	response.Success(c, schema.NewSessionResponseList(entities))
}

func (h *Handler) GetSession(c *gin.Context) {
	id := getPathID(c)
	entity, err := h.crudSession.Get(id)
	if err != nil {
		handleNotFound(c, "Session", id)
		return
	}
	response.Success(c, schema.NewSessionResponse(entity))
}

func (h *Handler) UpdateSession(c *gin.Context) {
	id := getPathID(c)
	entity, err := h.crudSession.Get(id)
	if err != nil {
		handleNotFound(c, "Session", id)
		return
	}
	var req schema.SessionUpdate
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	updates := req.BuildUpdates()
	if len(updates) > 0 {
		h.crudSession.Update(entity, updates)
		if err := database.DB.First(entity, id).Error; err != nil {
			applogger.Warn("failed to refresh session after update", "id", id, "error", err)
		}
	}
	response.Success(c, schema.NewSessionResponse(entity))
}

func (h *Handler) DeleteSession(c *gin.Context) {
	id := getPathID(c)
	_, err := h.crudSession.Get(id)
	if err != nil {
		handleNotFound(c, "Session", id)
		return
	}

	// Delete all session-related resources in dependency order:
	// 1. Works (may reference drafts)
	// 2. Message drafts
	// 3. Interactions
	// 4. Agent narratives + session summaries
	// 5. Participant sessions
	// 6. Messages
	// 7. Session itself
	if err := database.DB.Where("session_id = ?", id).Delete(&model.Work{}).Error; err != nil {
		applogger.Error("DeleteSession: failed to delete works", "session_id", id, "error", err)
	}
	if err := database.DB.Where("session_id = ?", id).Delete(&model.MessageDraft{}).Error; err != nil {
		applogger.Error("DeleteSession: failed to delete message drafts", "session_id", id, "error", err)
	}
	if err := database.DB.Where("session_id = ?", id).Delete(&model.Interaction{}).Error; err != nil {
		applogger.Error("DeleteSession: failed to delete interactions", "session_id", id, "error", err)
	}
	if err := database.DB.Where("session_id = ?", id).Delete(&model.AgentNarrative{}).Error; err != nil {
		applogger.Error("DeleteSession: failed to delete agent narratives", "session_id", id, "error", err)
	}
	if err := database.DB.Where("session_id = ?", id).Delete(&model.Summary{}).Error; err != nil {
		applogger.Error("DeleteSession: failed to delete session summaries", "session_id", id, "error", err)
	}
	if err := database.DB.Where("session_id = ?", id).Delete(&model.ParticipantSession{}).Error; err != nil {
		applogger.Error("DeleteSession: failed to delete participant sessions", "session_id", id, "error", err)
	}
	if err := database.DB.Where("session_id = ?", id).Delete(&model.Message{}).Error; err != nil {
		applogger.Error("DeleteSession: failed to delete messages", "session_id", id, "error", err)
	}
	h.crudSession.Delete(id)
	removeSessionWorkspace(id)
	response.SuccessMessage(c, "Session deleted successfully", nil)
}

func (h *Handler) CreateMessage(c *gin.Context) {
	sessionID := getPathID(c)
	var session model.Session
	if err := database.DB.First(&session, sessionID).Error; err != nil {
		response.NotFound(c, "Session not found")
		return
	}
	var req schema.MessageCreate
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	entity := model.Message{
		SessionID:       sessionID,
		Role:            model.MessageRoleUser,
		Content:         req.Content,
		Status:          model.MessageStatusCompleted,
		HasInteractions: model.HasInteractionsNone,
	}
	if err := database.DB.Create(&entity).Error; err != nil {
		response.InternalError(c, err.Error())
		return
	}

	// Submit to the event vectorization service for embedding + observation.
	memory.SubmitVectorization(memory.VectorizationTask{
		MessageID: entity.ID,
		SessionID: entity.SessionID,
		Content:   entity.Content,
	})

	response.Success(c, schema.NewMessageResponse(&entity))
}

func (h *Handler) ListMessages(c *gin.Context) {
	sessionID := getPathID(c)
	var session model.Session
	if err := database.DB.First(&session, sessionID).Error; err != nil {
		response.NotFound(c, "Session not found")
		return
	}
	var messages []model.Message
	if err := database.DB.Where("session_id = ?", sessionID).Order("created_at ASC").Find(&messages).Error; err != nil {
		applogger.Error("failed to list messages", "session_id", sessionID, "error", err)
		response.InternalError(c, "Failed to list messages")
		return
	}
	response.Success(c, schema.NewMessageResponseList(messages))
}

func (h *Handler) GetSearchConfig(c *gin.Context) {
	config := service.GetSearchConfig()
	response.Success(c, schema.NewSearchConfigResponse(config))
}

func (h *Handler) UpdateSearchConfig(c *gin.Context) {
	var req schema.SearchConfigUpdate
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	config := service.UpdateSearchConfig(req.Provider, req.APIKey, req.Description, req.IsActive)
	response.Success(c, schema.NewSearchConfigResponse(config))
}
