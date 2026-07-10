package handler

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"private-buddy-server/internal/api/response"
	"private-buddy-server/internal/config"
	"private-buddy-server/internal/database"
	applogger "private-buddy-server/internal/logger"
	"private-buddy-server/internal/model"
	"private-buddy-server/internal/schema"
	"private-buddy-server/internal/service"
	"private-buddy-server/internal/service/memory"
	"private-buddy-server/internal/service/runtime"
	"private-buddy-server/internal/service/workspace"
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
	version := config.AppVersion
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
			applogger.Error("failed to refresh LLM config after update", "id", id, "error", err)
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
		applogger.Error("failed to check referencing agents for LLM config", "id", id, "error", err)
	}
	if len(referencingAgents) > 0 {
		names := make([]string, len(referencingAgents))
		for i, a := range referencingAgents {
			names[i] = service.GetAgentName(a.ID)
		}
		response.BadRequest(c, "Cannot delete LLM config: it is referenced by "+strconv.Itoa(len(referencingAgents))+" agent(s): "+strings.Join(names, ", "))
		return
	}
	// Check if this LLM config is set as the system LLM.
	sysCfg := service.GetSystemLLMConfig()
	if sysCfg != nil && sysCfg.ID == id {
		response.BadRequest(c, "Cannot delete LLM config: it is currently set as the system LLM")
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

// GetUserProfile returns the current user's person profile.
// Returns zero-value response if user hasn't been set up yet.
func (h *Handler) GetUserProfile(c *gin.Context) {
	person, err := service.GetCurrentUserPerson()
	if err != nil {
		response.Success(c, gin.H{})
		return
	}
	response.Success(c, gin.H{
		"id":   person.ID,
		"name": person.Name,
		"bio":  person.Bio,
		"type": person.Type,
	})
}

// CreateOrUpdateUserProfile creates or updates the user's person profile.
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

	existing, _ := service.GetCurrentUserPerson()
	if existing != nil {
		// Update bio only (name is immutable)
		updates := map[string]interface{}{"bio": req.Bio}
		if err := database.DB.Model(existing).Updates(updates).Error; err != nil {
			response.InternalError(c, err.Error())
			return
		}
		if err := database.DB.First(existing, existing.ID).Error; err != nil {
			applogger.Error("failed to refresh user profile after update", "id", existing.ID, "error", err)
		}
		response.Success(c, gin.H{
			"id":   existing.ID,
			"name": existing.Name,
			"bio":  existing.Bio,
			"type": existing.Type,
		})
		return
	}

	person := model.Person{
		Name: req.Name,
		Bio:  req.Bio,
		Type: model.PersonTypeHuman,
	}
	if err := database.DB.Create(&person).Error; err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint") {
			response.BadRequest(c, fmt.Sprintf("User name '%s' already exists", req.Name))
			return
		}
		response.InternalError(c, err.Error())
		return
	}
	response.Success(c, gin.H{
		"id":   person.ID,
		"name": person.Name,
		"bio":  person.Bio,
		"type": person.Type,
	})
}

func (h *Handler) CreateAgent(c *gin.Context) {
	var req schema.AgentCreate
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	entity, person, err := service.CreateAgentWithPerson(
		req.Name, req.Description, req.CharacterSettings,
		req.LLMConfigID, req.Avatar, req.KnowledgeBaseIDs,
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint") {
			response.BadRequest(c, fmt.Sprintf("Agent name '%s' already exists", req.Name))
			return
		}
		response.InternalError(c, err.Error())
		return
	}

	// Register and start the agent's runtime so it can receive events immediately.
	runtime.StartRuntime(entity.ID)

	response.Success(c, schema.NewAgentResponse(entity, person))
}

func (h *Handler) ListAgents(c *gin.Context) {
	skip, limit := getPagination(c)
	entities, err := h.crudAgent.GetMulti(skip, limit)
	if err != nil {
		response.InternalError(c, err.Error())
		return
	}
	// Load all associated persons for names and bios
	personsMap := loadAgentPersons(entities)
	response.Success(c, schema.NewAgentResponseList(entities, personsMap))
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
		applogger.Error("failed to load sessions for agent list, returning without sessions", "error", err)
	}

	sessionsByAgent := make(map[int64][]model.Session)
	for _, s := range allSessions {
		sessionsByAgent[s.AgentID] = append(sessionsByAgent[s.AgentID], s)
	}

	personsMap := loadAgentPersons(agents)

	result := make([]schema.AgentWithSessions, 0, len(agents))
	for i := range agents {
		sessions := sessionsByAgent[agents[i].ID]
		if sessions == nil {
			sessions = []model.Session{}
		}
		result = append(result, schema.AgentWithSessions{
			AgentResponse: *schema.NewAgentResponse(&agents[i], personsMap[agents[i].PersonID]),
			Sessions:      schema.NewSessionBriefList(sessions),
		})
	}
	response.Success(c, result)
}

func (h *Handler) GetAgent(c *gin.Context) {
	id := getPathID(c)
	agent, person, err := service.GetAgentWithPerson(id)
	if err != nil {
		handleNotFound(c, "Agent", id)
		return
	}
	response.Success(c, schema.NewAgentResponse(agent, person))
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

	// Wrap agent + person updates in a transaction
	err = database.DB.Transaction(func(tx *gorm.DB) error {
		// Update agent-level fields
		updates := req.BuildUpdates()
		if len(updates) > 0 {
			if err := tx.Model(entity).Updates(updates).Error; err != nil {
				return err
			}
		}

		// Update person-level fields (name and bio) if provided
		if req.Name != nil || req.Bio != nil {
			person, err := service.GetPerson(entity.PersonID)
			if err != nil {
				return err
			}
			personUpdates := make(map[string]interface{})
			if req.Name != nil {
				personUpdates["name"] = *req.Name
			}
			if req.Bio != nil {
				personUpdates["bio"] = *req.Bio
			}
			if len(personUpdates) > 0 {
				if err := tx.Model(person).Updates(personUpdates).Error; err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		applogger.Error("UpdateAgent: transaction failed", "id", id, "error", err)
		response.InternalError(c, "Failed to update agent")
		return
	}

	// Reload for response
	agent, person, err := service.GetAgentWithPerson(id)
	if err != nil {
		applogger.Error("UpdateAgent: failed to reload agent with person", "id", id, "error", err)
		response.Success(c, schema.NewAgentResponse(entity, nil))
		return
	}
	response.Success(c, schema.NewAgentResponse(agent, person))
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

	personID, sessionIDs, err := service.DeleteAgentCascade(id)
	if err != nil {
		applogger.Error("DeleteAgent: cascade delete failed", "agent_id", id, "error", err)
		response.InternalError(c, "Failed to delete agent")
		return
	}

	// Filesystem cleanup (not transactional)
	for _, sid := range sessionIDs {
		workspace.RemoveWorkspace(personID, sid)
		workspace.RemoveAac(personID, sid)
	}

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
			applogger.Error("failed to refresh session after update", "id", id, "error", err)
		}
	}
	response.Success(c, schema.NewSessionResponse(entity))
}

func (h *Handler) DeleteSession(c *gin.Context) {
	id := getPathID(c)

	personID, _, err := service.DeleteSessionCascade(id)
	if err != nil {
		applogger.Error("DeleteSession: cascade delete failed", "session_id", id, "error", err)
		response.InternalError(c, "Failed to delete session")
		return
	}

	// Filesystem cleanup
	if personID > 0 {
		workspace.RemoveWorkspace(personID, id)
		workspace.RemoveAac(personID, id)
	}
	response.SuccessMessage(c, "Session deleted successfully", nil)
}

// receivedFileEntry represents a file or directory in a delivery tree.
type receivedFileEntry struct {
	Name      string              `json:"name"`
	Path      string              `json:"path"`
	LocalPath string              `json:"local_path,omitempty"`
	Size      int64               `json:"size"`
	IsDir     bool                `json:"is_dir"`
	Children  []receivedFileEntry `json:"children"`
}

// receivedDeliveryEntry represents one delivery directory with its file tree.
type receivedDeliveryEntry struct {
	Name  string              `json:"name"`
	Files []receivedFileEntry `json:"files"`
}

// GetReceivedDeliveries lists all delivery directories and their file trees
// under the user's received/ directory for a given session.
func (h *Handler) GetReceivedDeliveries(c *gin.Context) {
	sessionID := getPathID(c)

	var session model.Session
	if err := database.DB.First(&session, sessionID).Error; err != nil {
		response.NotFound(c, "Session not found")
		return
	}

	// Get current user's PersonID for received directory
	userPerson, err := service.GetCurrentUserPerson()
	if err != nil {
		response.BadRequest(c, "No user profile found")
		return
	}

	receivedDir := workspace.GetReceivedDir(userPerson.ID, sessionID)

	var deliveries []receivedDeliveryEntry

	entries, err := os.ReadDir(receivedDir)
	if err != nil {
		// Directory doesn't exist yet — return empty list
		response.Success(c, []receivedDeliveryEntry{})
		return
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		deliveryPath := filepath.Join(receivedDir, entry.Name())
		files := walkDeliveryFiles(deliveryPath)

		deliveries = append(deliveries, receivedDeliveryEntry{
			Name:  entry.Name(),
			Files: files,
		})
	}

	response.Success(c, deliveries)
}

// walkDeliveryFiles builds a directory tree from the delivery directory.
func walkDeliveryFiles(dirPath string) []receivedFileEntry {
	var root []receivedFileEntry

	filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		// Skip the root dir itself
		if path == dirPath {
			return nil
		}

		relPath, _ := filepath.Rel(dirPath, path)
		parts := strings.Split(filepath.ToSlash(relPath), "/")
		node := receivedFileEntry{
			Name:  info.Name(),
			Path:  filepath.ToSlash(relPath),
			IsDir: info.IsDir(),
		}
		if info.IsDir() {
			node.Children = []receivedFileEntry{}
		} else {
			node.LocalPath = path
			node.Size = info.Size()
		}

		// Find or create parent directory nodes along the path
		current := &root
		for i := 0; i < len(parts)-1; i++ {
			dirName := parts[i]
			// Find existing dir or create one
			found := false
			for j := range *current {
				if (*current)[j].Name == dirName && (*current)[j].IsDir {
					current = &(*current)[j].Children
					found = true
					break
				}
			}
			if !found {
				dirPath := strings.Join(parts[:i+1], "/")
				newDir := receivedFileEntry{
					Name:     dirName,
					Path:     dirPath,
					IsDir:    true,
					Children: []receivedFileEntry{},
				}
				*current = append(*current, newDir)
				current = &(*current)[len(*current)-1].Children
			}
		}

		// Add the node itself (file or leaf directory)
		if !info.IsDir() || len(parts) > 0 {
			// Only append if not already added as intermediate dir
			alreadyAdded := false
			for _, c := range *current {
				if c.Name == node.Name && c.IsDir == node.IsDir {
					alreadyAdded = true
					break
				}
			}
			if !alreadyAdded {
				*current = append(*current, node)
			}
		}

		return nil
	})

	return root
}

// GetReceivedFile returns the content of a file in the user's received/ directory.
// Query params: delivery=N (required), path=xxx (required)
func (h *Handler) GetReceivedFile(c *gin.Context) {
	sessionID := getPathID(c)
	delivery := c.Query("delivery")
	filePath := c.Query("path")

	if delivery == "" || filePath == "" {
		response.BadRequest(c, "delivery and path query parameters are required")
		return
	}

	var session model.Session
	if err := database.DB.First(&session, sessionID).Error; err != nil {
		response.NotFound(c, "Session not found")
		return
	}

	userPerson, err := service.GetCurrentUserPerson()
	if err != nil {
		response.BadRequest(c, "No user profile found")
		return
	}

	// Security: validate delivery name contains only safe characters
	for _, ch := range delivery {
		if !((ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '_') {
			response.BadRequest(c, "invalid delivery name")
			return
		}
	}

	receivedDir := workspace.GetReceivedDir(userPerson.ID, sessionID)
	fullPath := filepath.Join(receivedDir, delivery, filepath.Clean(filePath))

	// Security: ensure the resolved path is within the received/ directory
	if !strings.HasPrefix(fullPath, receivedDir) {
		response.BadRequest(c, "invalid file path")
		return
	}

	data, err := os.ReadFile(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			response.NotFound(c, "File not found")
			return
		}
		response.InternalError(c, err.Error())
		return
	}

	c.Header("Content-Type", "application/octet-stream")
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filepath.Base(filePath)))
	c.Data(200, "application/octet-stream", data)
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
	userPersonID, err := service.GetCurrentUserPersonID()
	if err != nil {
		applogger.Error("CreateMessage: failed to get current user person ID", "error", err)
		response.InternalError(c, "Failed to identify current user")
		return
	}
	entity := model.Message{
		SessionID: sessionID,
		PersonID:  userPersonID,
		Content:   req.Content,
		Status:    model.MessageStatusCompleted,
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

// GetCurrentPerson returns the current user's person record.
// GET /api/persons/me
func (h *Handler) GetCurrentPerson(c *gin.Context) {
	person, err := service.GetCurrentUserPerson()
	if err != nil {
		response.NotFound(c, "No user person record found. Please create a user profile first.")
		return
	}
	response.Success(c, gin.H{
		"id":   person.ID,
		"name": person.Name,
		"type": person.Type,
	})
}
