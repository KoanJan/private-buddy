package handler

import (
	"private-buddy-server/internal/api/response"
	"private-buddy-server/internal/database"
	"private-buddy-server/internal/dops"
	applogger "private-buddy-server/internal/logger"
	"private-buddy-server/internal/model"
	"private-buddy-server/internal/schema"
	"private-buddy-server/internal/service/workspace"

	"github.com/gin-gonic/gin"
)

// ListSessions handles listing all sessions.
func (h *Handler) ListSessions(c *gin.Context) {
	skip, limit := getPagination(c)
	entities, err := dops.GetMulti[model.Session](skip, limit)
	if err != nil {
		response.InternalError(c, err.Error())
		return
	}
	response.Success(c, schema.NewSessionResponseList(database.DB, entities))
}

// GetSession handles retrieving a single session by ID.
func (h *Handler) GetSession(c *gin.Context) {
	id := getPathID(c)
	entity, err := dops.Get[model.Session](id)
	if err != nil {
		handleNotFound(c, "Session", id)
		return
	}
	personID := dops.GetFirstAIParticipantID(id)
	response.Success(c, schema.NewSessionResponse(entity, personID))
}

// UpdateSession handles updating an existing session.
func (h *Handler) UpdateSession(c *gin.Context) {
	id := getPathID(c)
	entity, err := dops.Get[model.Session](id)
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
		dops.Update(entity, updates)
		refreshed, err := dops.Get[model.Session](id)
		if err != nil {
			applogger.Error("failed to refresh session after update", "id", id, "error", err)
		} else {
			entity = refreshed
		}
	}
	personID := dops.GetFirstAIParticipantID(id)
	response.Success(c, schema.NewSessionResponse(entity, personID))
}

// DeleteSession handles deleting a session and its resources.
func (h *Handler) DeleteSession(c *gin.Context) {
	id := getPathID(c)

	personID, _, err := dops.DeleteSessionCascade(id)
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
