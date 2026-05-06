package handler

import (
	"net/http"
	"strconv"
	"strings"

	"private-buddy-server/internal/model"
	"private-buddy-server/internal/schema"
	"private-buddy-server/internal/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type Handler struct {
	db            *gorm.DB
	crudLLM       *service.CRUDBase[model.LLMConfig]
	crudEmbedding *service.CRUDBase[model.EmbeddingConfig]
	crudAgent     *service.CRUDBase[model.Agent]
	crudSession   *service.CRUDBase[model.Session]
	dataService   *service.DataService
	searchService *service.SearchService
}

func NewHandler(db *gorm.DB) *Handler {
	return &Handler{
		db:            db,
		crudLLM:       service.NewCRUDBase[model.LLMConfig](db, "LLM config"),
		crudEmbedding: service.NewCRUDBase[model.EmbeddingConfig](db, "Embedding config"),
		crudAgent:     service.NewCRUDBase[model.Agent](db, "Agent"),
		crudSession:   service.NewCRUDBase[model.Session](db, "Session"),
		dataService:   service.NewDataService(),
		searchService: service.NewSearchService(),
	}
}

func (h *Handler) Root(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"message": "Private Buddy API is running"})
}

func (h *Handler) GetVersion(c *gin.Context) {
	var versionRecord model.DBVersion
	err := h.db.Order("id DESC").First(&versionRecord).Error
	version := "0.0.0"
	if err == nil {
		version = versionRecord.Version
	}
	c.JSON(http.StatusOK, gin.H{"version": version})
}

func (h *Handler) CreateLLMConfig(c *gin.Context) {
	var req schema.LLMConfigCreate
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}
	entity := model.LLMConfig{
		Name:        req.Name,
		ModelID:     req.ModelID,
		BaseURL:     req.BaseURL,
		APIKey:      req.APIKey,
		Description: derefString(req.Description),
	}
	if err := h.db.Select(
		"Name", "ModelID", "BaseURL", "APIKey", "Description",
	).Create(&entity).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, schema.NewLLMConfigResponse(&entity))
}

func (h *Handler) ListLLMConfigs(c *gin.Context) {
	skip, limit := getPagination(c)
	entities, err := h.crudLLM.GetMulti(skip, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, schema.NewLLMConfigResponseList(entities))
}

func (h *Handler) GetLLMConfig(c *gin.Context) {
	id := getPathID(c)
	entity, err := h.crudLLM.Get(id)
	if err != nil {
		service.HandleNotFound(c, "LLM config", id)
		return
	}
	c.JSON(http.StatusOK, schema.NewLLMConfigResponse(entity))
}

func (h *Handler) UpdateLLMConfig(c *gin.Context) {
	id := getPathID(c)
	entity, err := h.crudLLM.Get(id)
	if err != nil {
		service.HandleNotFound(c, "LLM config", id)
		return
	}
	var req schema.LLMConfigUpdate
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}
	updates := req.BuildUpdates()
	if len(updates) > 0 {
		h.crudLLM.Update(entity, updates)
		h.db.First(entity, id)
	}
	c.JSON(http.StatusOK, schema.NewLLMConfigResponse(entity))
}

func (h *Handler) DeleteLLMConfig(c *gin.Context) {
	id := getPathID(c)
	_, err := h.crudLLM.Get(id)
	if err != nil {
		service.HandleNotFound(c, "LLM config", id)
		return
	}
	var referencingAgents []model.Agent
	h.db.Where("llm_config_id = ?", id).Find(&referencingAgents)
	if len(referencingAgents) > 0 {
		names := make([]string, len(referencingAgents))
		for i, a := range referencingAgents {
			names[i] = a.Name
		}
		c.JSON(http.StatusBadRequest, gin.H{
			"detail": "Cannot delete LLM config: it is referenced by " + strconv.Itoa(len(referencingAgents)) + " agent(s): " + strings.Join(names, ", "),
		})
		return
	}
	h.crudLLM.Delete(id)
	c.JSON(http.StatusOK, gin.H{"message": "LLM config deleted successfully"})
}

func (h *Handler) CreateEmbeddingConfig(c *gin.Context) {
	var req schema.EmbeddingConfigCreate
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}
	entity := model.EmbeddingConfig{
		Name:        req.Name,
		ModelID:     req.ModelID,
		BaseURL:     req.BaseURL,
		APIKey:      req.APIKey,
		Description: req.Description,
	}
	if err := h.db.Select(
		"Name", "ModelID", "BaseURL", "APIKey", "Description",
	).Create(&entity).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, schema.NewEmbeddingConfigResponse(&entity))
}

func (h *Handler) ListEmbeddingConfigs(c *gin.Context) {
	skip, limit := getPagination(c)
	entities, err := h.crudEmbedding.GetMulti(skip, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, schema.NewEmbeddingConfigResponseList(entities))
}

func (h *Handler) GetEmbeddingConfig(c *gin.Context) {
	id := getPathID(c)
	entity, err := h.crudEmbedding.Get(id)
	if err != nil {
		service.HandleNotFound(c, "Embedding config", id)
		return
	}
	c.JSON(http.StatusOK, schema.NewEmbeddingConfigResponse(entity))
}

func (h *Handler) UpdateEmbeddingConfig(c *gin.Context) {
	id := getPathID(c)
	entity, err := h.crudEmbedding.Get(id)
	if err != nil {
		service.HandleNotFound(c, "Embedding config", id)
		return
	}
	var req schema.EmbeddingConfigUpdate
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}
	updates := req.BuildUpdates()
	if len(updates) > 0 {
		h.crudEmbedding.Update(entity, updates)
		h.db.First(entity, id)
	}
	c.JSON(http.StatusOK, schema.NewEmbeddingConfigResponse(entity))
}

func (h *Handler) DeleteEmbeddingConfig(c *gin.Context) {
	id := getPathID(c)
	_, err := h.crudEmbedding.Get(id)
	if err != nil {
		service.HandleNotFound(c, "Embedding config", id)
		return
	}
	h.db.Model(&model.Agent{}).Where("embedding_config_id = ?", id).Update("embedding_config_id", 0)
	h.crudEmbedding.Delete(id)
	c.JSON(http.StatusOK, gin.H{"message": "Embedding config deleted successfully"})
}

func (h *Handler) CreateAgent(c *gin.Context) {
	var req schema.AgentCreate
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}
	entity := model.Agent{
		Name:              req.Name,
		CharacterSettings: req.CharacterSettings,
		LLMConfigID:       req.LLMConfigID,
		EmbeddingConfigID: req.EmbeddingConfigID,
		Description:       req.Description,
		Avatar:            req.Avatar,
	}
	if err := h.db.Select(
		"Name", "CharacterSettings", "LLMConfigID", "EmbeddingConfigID", "Description", "Avatar",
	).Create(&entity).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, schema.NewAgentResponse(&entity))
}

func (h *Handler) ListAgents(c *gin.Context) {
	skip, limit := getPagination(c)
	entities, err := h.crudAgent.GetMulti(skip, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, schema.NewAgentResponseList(entities))
}

func (h *Handler) ListAgentsWithSessions(c *gin.Context) {
	var agents []model.Agent
	h.db.Order("updated_at DESC").Find(&agents)

	if len(agents) == 0 {
		c.JSON(http.StatusOK, []schema.AgentWithSessions{})
		return
	}

	agentIDs := make([]int64, len(agents))
	for i, a := range agents {
		agentIDs[i] = a.ID
	}

	var allSessions []model.Session
	h.db.Where("agent_id IN ?", agentIDs).Order("updated_at DESC").Find(&allSessions)

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
	c.JSON(http.StatusOK, result)
}

func (h *Handler) GetAgent(c *gin.Context) {
	id := getPathID(c)
	entity, err := h.crudAgent.Get(id)
	if err != nil {
		service.HandleNotFound(c, "Agent", id)
		return
	}
	c.JSON(http.StatusOK, schema.NewAgentResponse(entity))
}

func (h *Handler) UpdateAgent(c *gin.Context) {
	id := getPathID(c)
	entity, err := h.crudAgent.Get(id)
	if err != nil {
		service.HandleNotFound(c, "Agent", id)
		return
	}
	var req schema.AgentUpdate
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}
	updates := req.BuildUpdates()
	if len(updates) > 0 {
		h.crudAgent.Update(entity, updates)
		h.db.First(entity, id)
	}
	c.JSON(http.StatusOK, schema.NewAgentResponse(entity))
}

func (h *Handler) DeleteAgent(c *gin.Context) {
	id := getPathID(c)
	agent, err := h.crudAgent.Get(id)
	if err != nil {
		service.HandleNotFound(c, "Agent", id)
		return
	}

	if agent.Avatar != "" {
		avatarPath := getAvatarsDir() + "/" + agent.Avatar
		osRemoveIfExists(avatarPath)
	}

	var sessionIDs []int64
	h.db.Model(&model.Session{}).Where("agent_id = ?", id).Pluck("id", &sessionIDs)

	if len(sessionIDs) > 0 {
		h.db.Where("session_id IN ?", sessionIDs).Delete(&model.Interaction{})
		h.db.Where("session_id IN ?", sessionIDs).Delete(&model.HistoricalSummary{})
		h.db.Where("session_id IN ?", sessionIDs).Delete(&model.Message{})
		h.db.Where("agent_id = ?", id).Delete(&model.Session{})

		for _, sid := range sessionIDs {
			removeSessionWorkspace(sid)
		}
	}

	h.crudAgent.Delete(id)
	c.JSON(http.StatusOK, gin.H{"message": "Agent deleted successfully"})
}

func (h *Handler) CreateSession(c *gin.Context) {
	var req schema.SessionCreate
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}
	title := ""
	if req.Title != nil {
		title = *req.Title
	}
	entity := model.Session{
		Title:   title,
		AgentID: req.AgentID,
		Status:  model.SessionStatusIdle,
	}
	if err := h.db.Select("Title", "AgentID", "Status").Create(&entity).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, schema.NewSessionResponse(&entity))
}

func (h *Handler) ListSessions(c *gin.Context) {
	skip, limit := getPagination(c)
	entities, err := h.crudSession.GetMulti(skip, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, schema.NewSessionResponseList(entities))
}

func (h *Handler) GetSession(c *gin.Context) {
	id := getPathID(c)
	entity, err := h.crudSession.Get(id)
	if err != nil {
		service.HandleNotFound(c, "Session", id)
		return
	}
	c.JSON(http.StatusOK, schema.NewSessionResponse(entity))
}

func (h *Handler) UpdateSession(c *gin.Context) {
	id := getPathID(c)
	entity, err := h.crudSession.Get(id)
	if err != nil {
		service.HandleNotFound(c, "Session", id)
		return
	}
	var req schema.SessionUpdate
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}
	updates := req.BuildUpdates()
	if len(updates) > 0 {
		h.crudSession.Update(entity, updates)
		h.db.First(entity, id)
	}
	c.JSON(http.StatusOK, schema.NewSessionResponse(entity))
}

func (h *Handler) DeleteSession(c *gin.Context) {
	id := getPathID(c)
	_, err := h.crudSession.Get(id)
	if err != nil {
		service.HandleNotFound(c, "Session", id)
		return
	}

	h.db.Where("session_id = ?", id).Delete(&model.Interaction{})
	h.db.Where("session_id = ?", id).Delete(&model.HistoricalSummary{})
	h.db.Where("session_id = ?", id).Delete(&model.Message{})
	h.crudSession.Delete(id)
	removeSessionWorkspace(id)
	c.JSON(http.StatusOK, gin.H{"message": "Session deleted successfully"})
}

func (h *Handler) CreateMessage(c *gin.Context) {
	sessionID := getPathID(c)
	var session model.Session
	if err := h.db.First(&session, sessionID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "Session not found"})
		return
	}
	var req schema.MessageCreate
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}
	entity := model.Message{
		SessionID:       sessionID,
		Role:            "user",
		Content:         req.Content,
		Status:          model.MessageStatusCompleted,
		HasInteractions: model.HasInteractionsNone,
	}
	if err := h.db.Create(&entity).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, schema.NewMessageResponse(&entity))
}

func (h *Handler) ListMessages(c *gin.Context) {
	sessionID := getPathID(c)
	var session model.Session
	if err := h.db.First(&session, sessionID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "Session not found"})
		return
	}
	var messages []model.Message
	h.db.Where("session_id = ?", sessionID).Order("created_at ASC").Find(&messages)
	c.JSON(http.StatusOK, schema.NewMessageResponseList(messages))
}

func (h *Handler) GetSearchConfig(c *gin.Context) {
	config := h.searchService.GetConfig(h.db)
	c.JSON(http.StatusOK, schema.NewSearchConfigResponse(config))
}

func (h *Handler) UpdateSearchConfig(c *gin.Context) {
	var req schema.SearchConfigUpdate
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}
	config := h.searchService.UpdateConfig(h.db, req.Provider, req.APIKey, req.Description, req.IsActive)
	c.JSON(http.StatusOK, schema.NewSearchConfigResponse(config))
}

func (h *Handler) GetInteractions(c *gin.Context) {
	agentMsgIDStr := c.Query("agent_msg_id")
	agentMsgID, err := strconv.ParseInt(agentMsgIDStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "Invalid agent_msg_id"})
		return
	}
	var interactions []model.Interaction
	h.db.Where("agent_msg_id = ?", agentMsgID).Order("iteration, type").Find(&interactions)
	c.JSON(http.StatusOK, schema.InteractionListResponse{
		Interactions: schema.NewInteractionResponseList(interactions),
	})
}

func (h *Handler) GetInteractionStatus(c *gin.Context) {
	id := getPathID(c)
	var message model.Message
	if err := h.db.First(&message, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "Message not found"})
		return
	}
	c.JSON(http.StatusOK, schema.InteractionStatusResponse{
		HasInteractions: message.HasInteractions,
	})
}
