package handler

import (
	"errors"

	"private-buddy-server/internal/api/response"
	"private-buddy-server/internal/database"
	applogger "private-buddy-server/internal/logger"
	"private-buddy-server/internal/model"
	"private-buddy-server/internal/schema"
	"private-buddy-server/internal/service"
	"private-buddy-server/internal/service/experience"

	"github.com/gin-gonic/gin"
)

// ListPublicExperiences returns a paginated list of all public experiences.
// GET /api/public-experiences
func (h *Handler) ListPublicExperiences(c *gin.Context) {
	skip, limit := getPagination(c)

	var entities []model.PublicExperience
	if err := database.DB.Order("updated_at DESC").Offset(skip).Limit(limit).Find(&entities).Error; err != nil {
		applogger.Error("failed to list public experiences", "error", err)
		response.InternalError(c, "Failed to list public experiences")
		return
	}

	response.Success(c, schema.NewPublicExperienceResponseList(entities))
}

// GetPublicExperience returns a single public experience by ID.
// GET /api/public-experiences/:id
func (h *Handler) GetPublicExperience(c *gin.Context) {
	id := getPathID(c)

	var entity model.PublicExperience
	if err := database.DB.First(&entity, id).Error; err != nil {
		handleNotFound(c, "Public experience", id)
		return
	}

	response.Success(c, schema.NewPublicExperienceResponse(&entity))
}

// DeletePublicExperience deletes a public experience by ID.
// DELETE /api/public-experiences/:id
func (h *Handler) DeletePublicExperience(c *gin.Context) {
	id := getPathID(c)

	var entity model.PublicExperience
	if err := database.DB.First(&entity, id).Error; err != nil {
		handleNotFound(c, "Public experience", id)
		return
	}

	// Delete the vector first (1:1 relationship, experience_id is the PK)
	if err := database.DB.Where("experience_id = ?", id).Delete(&model.PublicExperienceVector{}).Error; err != nil {
		applogger.Error("DeletePublicExperience: failed to delete vector", "id", id, "error", err)
	}

	if err := database.DB.Delete(&entity).Error; err != nil {
		applogger.Error("DeletePublicExperience: failed to delete experience", "id", id, "error", err)
		response.InternalError(c, "Failed to delete public experience")
		return
	}

	applogger.Info("PublicExperience deleted", "id", id)
	response.SuccessMessage(c, "Public experience deleted successfully", nil)
}

// IngestPublicExperience submits a skill for async refinement.
// POST /api/public-experiences/ingest
func (h *Handler) IngestPublicExperience(c *gin.Context) {
	var req schema.PublicExperienceIngest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	uploaded, err := experience.IngestSkill(c.Request.Context(), experience.IngestSkillParams{
		SourceName: req.SourceName,
		RawContent: req.RawContent,
	})
	if err != nil {
		if errors.Is(err, experience.ErrDuplicateSkill) {
			response.BadRequest(c, "This skill has already been ingested")
			return
		}
		applogger.Error("IngestPublicExperience failed", "source_name", req.SourceName, "error", err)
		response.InternalError(c, "Failed to submit skill: "+err.Error())
		return
	}

	response.Success(c, schema.NewUploadedSkillResponse(uploaded))
}

// SearchPublicExperiences performs semantic search against public experiences.
// POST /api/public-experiences/search
func (h *Handler) SearchPublicExperiences(c *gin.Context) {
	var req schema.PublicExperienceSearch
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	topN := req.TopN
	if topN <= 0 {
		topN = 10
	}
	minScore := req.MinScore
	if minScore <= 0 {
		minScore = 0.3
	}

	results, err := experience.SearchPublicExperiences(c.Request.Context(), req.Query, topN, minScore)
	if err != nil {
		applogger.Error("SearchPublicExperiences failed", "error", err)
		response.InternalError(c, "Search failed: "+err.Error())
		return
	}

	if results == nil {
		results = []experience.PublicSearchResult{}
	}

	formatted := make([]schema.PublicExperienceSearchResult, len(results))
	for i, r := range results {
		formatted[i] = schema.PublicExperienceSearchResult{
			PublicExperienceResponse: schema.NewPublicExperienceResponse(&r.Experience),
			Score:                    r.Score,
		}
	}

	response.Success(c, formatted)
}

// GetSystemLLMConfigHandler returns the current system-level LLM config.
// GET /api/public-experiences/system-llm-config
func (h *Handler) GetSystemLLMConfigHandler(c *gin.Context) {
	cfg := service.GetSystemLLMConfig()
	if cfg == nil {
		response.Success(c, gin.H{})
		return
	}
	response.Success(c, gin.H{
		"llm_config_id": cfg.ID,
		"name":          cfg.Name,
		"model_id":      cfg.ModelID,
	})
}

// UpdateSystemLLMConfigHandler updates the system-level LLM config.
// PUT /api/public-experiences/system-llm-config
func (h *Handler) UpdateSystemLLMConfigHandler(c *gin.Context) {
	var req struct {
		LLMConfigID int64 `json:"llm_config_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	// Validate that the referenced LLM config exists
	if _, err := service.GetLLMConfig(req.LLMConfigID); err != nil {
		response.BadRequest(c, "LLM config not found")
		return
	}

	if err := service.UpdateSystemLLMConfig(req.LLMConfigID); err != nil {
		applogger.Error("Failed to update system LLM config", "error", err)
		response.InternalError(c, "Failed to update system LLM config")
		return
	}

	cfg := service.GetSystemLLMConfig()
	if cfg == nil {
		response.Success(c, gin.H{})
		return
	}
	response.Success(c, gin.H{
		"llm_config_id": cfg.ID,
		"name":          cfg.Name,
		"model_id":      cfg.ModelID,
	})
}
