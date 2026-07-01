package handler

import (
	"private-buddy-server/internal/api/response"
	"private-buddy-server/internal/database"
	"private-buddy-server/internal/model"
	"private-buddy-server/internal/schema"

	"github.com/gin-gonic/gin"
)

// GetUploadedSkill returns a single uploaded skill by ID.
// GET /api/uploaded-skills/:id
func (h *Handler) GetUploadedSkill(c *gin.Context) {
	id := getPathID(c)

	var entity model.UploadedSkill
	if err := database.DB.First(&entity, id).Error; err != nil {
		handleNotFound(c, "Uploaded skill", id)
		return
	}

	response.Success(c, schema.NewUploadedSkillResponse(&entity))
}
