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

// Handler handles core API HTTP requests.
type Handler struct {
	crudLLM         *service.CRUDBase[model.LLMConfig]
	crudAgentConfig *service.CRUDBase[model.AgentConfig]
	crudSession     *service.CRUDBase[model.Session]
}

// NewHandler creates a new Handler instance.
func NewHandler() *Handler {
	return &Handler{
		crudLLM:         service.NewCRUDBase[model.LLMConfig]("LLM config"),
		crudAgentConfig: service.NewCRUDBase[model.AgentConfig]("AgentConfig"),
		crudSession:     service.NewCRUDBase[model.Session]("Session"),
	}
}

// Root handles the API root endpoint.
func (h *Handler) Root(c *gin.Context) {
	response.SuccessMessage(c, "Private Buddy API is running", nil)
}

// GetVersion handles retrieving the application version.
func (h *Handler) GetVersion(c *gin.Context) {
	var versionRecord model.DBVersion
	err := database.DB.Order("id DESC").First(&versionRecord).Error
	version := config.AppVersion
	if err == nil {
		version = versionRecord.Version
	}
	response.Success(c, gin.H{"version": version})
}

// CreateLLMConfig handles creating a new LLM configuration.
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

// ListLLMConfigs handles listing all LLM configurations.
func (h *Handler) ListLLMConfigs(c *gin.Context) {
	skip, limit := getPagination(c)
	entities, err := h.crudLLM.GetMulti(skip, limit)
	if err != nil {
		response.InternalError(c, err.Error())
		return
	}
	response.Success(c, schema.NewLLMConfigResponseList(entities))
}

// GetLLMConfig handles retrieving a single LLM configuration by ID.
func (h *Handler) GetLLMConfig(c *gin.Context) {
	id := getPathID(c)
	entity, err := h.crudLLM.Get(id)
	if err != nil {
		handleNotFound(c, "LLM config", id)
		return
	}
	response.Success(c, schema.NewLLMConfigResponse(entity))
}

// UpdateLLMConfig handles updating an existing LLM configuration.
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

// DeleteLLMConfig handles deleting an LLM configuration.
func (h *Handler) DeleteLLMConfig(c *gin.Context) {
	id := getPathID(c)
	_, err := h.crudLLM.Get(id)
	if err != nil {
		handleNotFound(c, "LLM config", id)
		return
	}
	var referencingAgents []model.AgentConfig
	if err := database.DB.Where("llm_config_id = ?", id).Find(&referencingAgents).Error; err != nil {
		applogger.Error("failed to check referencing agents for LLM config", "id", id, "error", err)
	}
	if len(referencingAgents) > 0 {
		names := make([]string, len(referencingAgents))
		for i, a := range referencingAgents {
			names[i] = service.GetAgentConfigName(a.ID)
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

// CreateAgentConfig handles creating a new agent config.
func (h *Handler) CreateAgentConfig(c *gin.Context) {
	var req schema.AgentConfigCreate
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	entity, person, err := service.CreateAgentConfigWithPerson(
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

// ListAgentConfigs handles listing all agent configs.
func (h *Handler) ListAgentConfigs(c *gin.Context) {
	skip, limit := getPagination(c)
	entities, err := h.crudAgentConfig.GetMulti(skip, limit)
	if err != nil {
		response.InternalError(c, err.Error())
		return
	}
	// Load all associated persons for names and bios
	personsMap := loadAgentConfigPersons(entities)
	response.Success(c, schema.NewAgentResponseList(entities, personsMap))
}

// ListAgentConfigsWithSessions handles listing all agent configs with their sessions.
func (h *Handler) ListAgentConfigsWithSessions(c *gin.Context) {
	var configs []model.AgentConfig
	if err := database.DB.Order("updated_at DESC").Find(&configs).Error; err != nil {
		applogger.Error("failed to list agent configs with sessions", "error", err)
		response.InternalError(c, "Failed to list agent configs")
		return
	}

	if len(configs) == 0 {
		response.Success(c, []schema.AgentWithSessions{})
		return
	}

	agentConfigIDs := make([]int64, len(configs))
	for i, a := range configs {
		agentConfigIDs[i] = a.ID
	}

	var allSessions []model.Session
	// Resolve sessions via participant_sessions instead of sessions.agent_config_id.
	if err := database.DB.
		Joins("JOIN participant_sessions ps ON ps.session_id = sessions.id").
		Joins("JOIN agent_configs ac ON ac.person_id = ps.participant_id").
		Where("ac.id IN ?", agentConfigIDs).
		Group("sessions.id").
		Order("MAX(sessions.updated_at) DESC").
		Find(&allSessions).Error; err != nil {
		applogger.Error("failed to load sessions for agent config list, returning without sessions", "error", err)
	}

	// Resolve session → agent mapping from participant_sessions.
	sids := make([]int64, len(allSessions))
	for i, s := range allSessions {
		sids[i] = s.ID
	}
	type sm struct {
		SessionID     int64
		AgentConfigID int64
	}
	var sessionAgents []sm
	database.DB.Raw(`SELECT ps.session_id, ac.id AS agent_config_id
		FROM participant_sessions ps
		JOIN agent_configs ac ON ac.person_id = ps.participant_id
		WHERE ps.session_id IN ?`, sids).Scan(&sessionAgents)

	agentSessions := make(map[int64]map[int64]bool) // agentConfigID → set of sessionIDs
	for _, sa := range sessionAgents {
		if agentSessions[sa.AgentConfigID] == nil {
			agentSessions[sa.AgentConfigID] = make(map[int64]bool)
		}
		agentSessions[sa.AgentConfigID][sa.SessionID] = true
	}

	sessionsByAgent := make(map[int64][]model.Session)
	for _, s := range allSessions {
		for agentConfigID, sessSet := range agentSessions {
			if sessSet[s.ID] {
				sessionsByAgent[agentConfigID] = append(sessionsByAgent[agentConfigID], s)
			}
		}
	}

	personsMap := loadAgentConfigPersons(configs)

	result := make([]schema.AgentWithSessions, 0, len(configs))
	for i := range configs {
		sessions := sessionsByAgent[configs[i].ID]
		if sessions == nil {
			sessions = []model.Session{}
		}
		result = append(result, schema.AgentWithSessions{
			AgentResponse: *schema.NewAgentResponse(&configs[i], personsMap[configs[i].PersonID]),
			Sessions:      schema.NewSessionBriefList(sessions),
		})
	}
	response.Success(c, result)
}

// GetAgentConfig handles retrieving a single agent config by ID.
// The ID in the URL is the person_id (frontend's "agent id").
func (h *Handler) GetAgentConfig(c *gin.Context) {
	personID := getPathID(c)
	ac, err := service.GetAgentConfigByPersonID(personID)
	if err != nil {
		handleNotFound(c, "AgentConfig", personID)
		return
	}
	person, err := service.GetPerson(personID)
	if err != nil {
		applogger.Error("GetAgentConfig: person not found", "person_id", personID, "error", err)
		response.InternalError(c, "Person not found")
		return
	}
	response.Success(c, schema.NewAgentResponse(ac, person))
}

// UpdateAgentConfig handles updating an existing agent config.
// The ID in the URL is the person_id (frontend's "agent id").
func (h *Handler) UpdateAgentConfig(c *gin.Context) {
	personID := getPathID(c)
	entity, err := service.GetAgentConfigByPersonID(personID)
	if err != nil {
		handleNotFound(c, "AgentConfig", personID)
		return
	}
	var req schema.AgentConfigUpdate
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
			person, err := service.GetPerson(personID)
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
		applogger.Error("UpdateAgentConfig: transaction failed", "person_id", personID, "error", err)
		response.InternalError(c, "Failed to update agent config")
		return
	}

	// Reload for response
	ac, err := service.GetAgentConfigByPersonID(personID)
	if err != nil {
		applogger.Error("UpdateAgentConfig: failed to reload agent config", "person_id", personID, "error", err)
		response.InternalError(c, "Failed to reload agent config")
		return
	}
	person, err := service.GetPerson(personID)
	if err != nil {
		applogger.Error("UpdateAgentConfig: failed to reload person", "person_id", personID, "error", err)
		response.InternalError(c, "Failed to reload person")
		return
	}
	response.Success(c, schema.NewAgentResponse(ac, person))
}

// DeleteAgentConfig handles deleting an agent config and its resources.
// The ID in the URL is the person_id (frontend's "agent id").
func (h *Handler) DeleteAgentConfig(c *gin.Context) {
	personID := getPathID(c)
	ac, err := service.GetAgentConfigByPersonID(personID)
	if err != nil {
		handleNotFound(c, "AgentConfig", personID)
		return
	}

	if ac.Avatar != "" {
		avatarPath := getAvatarsDir() + "/" + ac.Avatar
		osRemoveIfExists(avatarPath)
	}

	sessionIDs, err := service.DeleteAgentConfigCascade(personID)
	if err != nil {
		applogger.Error("DeleteAgentConfig: cascade delete failed", "person_id", personID, "error", err)
		response.InternalError(c, "Failed to delete agent config")
		return
	}

	// Filesystem cleanup (not transactional)
	for _, sid := range sessionIDs {
		workspace.RemoveWorkspace(personID, sid)
		workspace.RemoveAac(personID, sid)
	}

	response.SuccessMessage(c, "Agent config deleted successfully", nil)
}

// CreateSession handles creating a new session.
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

	// Use PersonID directly for participant_sessions record.
	userPersonID, err := service.GetCurrentUserPersonID()
	if err != nil {
		response.BadRequest(c, "no user profile found")
		return
	}

	entity := model.Session{Title: title}
	err = database.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&entity).Error; err != nil {
			return err
		}
		// Owner (human user)
		if err := tx.Create(&model.ParticipantSession{
			SessionID:     entity.ID,
			ParticipantID: userPersonID,
			Role:          model.ParticipantRoleOwner,
			Status:        model.ParticipantStatusIdle,
		}).Error; err != nil {
			return err
		}
		// Member (AI agent)
		if err := tx.Create(&model.ParticipantSession{
			SessionID:     entity.ID,
			ParticipantID: req.AgentID,
			Role:          model.ParticipantRoleMember,
			Status:        model.ParticipantStatusIdle,
		}).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		response.InternalError(c, err.Error())
		return
	}
	response.Success(c, schema.NewSessionResponse(&entity, req.AgentID))
}

// ListSessions handles listing all sessions.
func (h *Handler) ListSessions(c *gin.Context) {
	skip, limit := getPagination(c)
	entities, err := h.crudSession.GetMulti(skip, limit)
	if err != nil {
		response.InternalError(c, err.Error())
		return
	}
	response.Success(c, schema.NewSessionResponseList(database.DB, entities))
}

// GetSession handles retrieving a single session by ID.
func (h *Handler) GetSession(c *gin.Context) {
	id := getPathID(c)
	entity, err := h.crudSession.Get(id)
	if err != nil {
		handleNotFound(c, "Session", id)
		return
	}
	personID := resolveFirstAIPersonID(id)
	response.Success(c, schema.NewSessionResponse(entity, personID))
}

// UpdateSession handles updating an existing session.
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
	personID := resolveFirstAIPersonID(id)
	response.Success(c, schema.NewSessionResponse(entity, personID))
}

// DeleteSession handles deleting a session and its resources.
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

// resolveFirstAIPersonID returns the person ID for the first AI participant
// in a session, resolved via participant_sessions.
func resolveFirstAIPersonID(sessionID int64) int64 {
	var personID int64
	database.DB.Raw(`SELECT ps.participant_id FROM participant_sessions ps
		JOIN persons p ON p.id = ps.participant_id AND p.type = 1
		WHERE ps.session_id = ?
		LIMIT 1`, sessionID).Scan(&personID)
	return personID
}

// resolveFirstAIAgentConfigID returns the agent_configs.id for the first AI participant
// in a session. Used for event routing to the agent runtime.
func resolveFirstAIAgentConfigID(sessionID int64) int64 {
	var agentConfigID int64
	database.DB.Raw(`SELECT ac.id FROM participant_sessions ps
		JOIN persons p ON p.id = ps.participant_id AND p.type = 1
		JOIN agent_configs ac ON ac.person_id = p.id
		WHERE ps.session_id = ?
		LIMIT 1`, sessionID).Scan(&agentConfigID)
	return agentConfigID
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

// CreateMessage handles creating a new message in a session.
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

// ListMessages handles listing messages in a session.
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

// GetSearchConfig handles retrieving the search configuration.
func (h *Handler) GetSearchConfig(c *gin.Context) {
	config := service.GetSearchConfig()
	response.Success(c, schema.NewSearchConfigResponse(config))
}

// UpdateSearchConfig handles updating the search configuration.
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
