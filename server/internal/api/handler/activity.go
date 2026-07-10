package handler

import (
	"github.com/gin-gonic/gin"

	"private-buddy-server/internal/api/response"
	"private-buddy-server/internal/database"
	applogger "private-buddy-server/internal/logger"
	"private-buddy-server/internal/model"
	"private-buddy-server/internal/service/task"
)

// GetSessionActivities returns a flat activity timeline for all task works in a session.
//
// GET /api/sessions/:id/activities
//
// Queries works of type=2 (Task) for the session, collects all their interactions,
// and converts them into a flat timeline of human-readable activity events.
func (h *Handler) GetSessionActivities(c *gin.Context) {
	sessionID := getPathID(c)

	// Verify session exists
	var session model.Session
	if err := database.DB.First(&session, sessionID).Error; err != nil {
		response.NotFound(c, "Session not found")
		return
	}

	// Collect all task work IDs for this session
	var workIDs []int64
	if err := database.DB.Model(&model.Work{}).
		Where("session_id = ? AND type = ?", sessionID, model.WorkTypeTask).
		Pluck("id", &workIDs).Error; err != nil {
		applogger.Error("GetSessionActivities: failed to query works",
			"session_id", sessionID, "error", err)
		response.InternalError(c, "Failed to query activities")
		return
	}

	if len(workIDs) == 0 {
		response.Success(c, []struct{}{})
		return
	}

	// Query all interactions across all task works, ordered by creation time
	var interactions []model.Interaction
	if err := database.DB.Where("work_id IN ?", workIDs).
		Order("created_at ASC").Find(&interactions).Error; err != nil {
		applogger.Error("GetSessionActivities: failed to query interactions",
			"session_id", sessionID, "error", err)
		response.InternalError(c, "Failed to query activities")
		return
	}

	// Get the agent participant for this session by joining persons table (type=1=AI)
	var agentPersonID int64
	if err := database.DB.Model(&model.ParticipantSession{}).
		Joins("JOIN persons ON persons.id = participant_sessions.participant_id AND persons.type = ?", model.PersonTypeAI).
		Where("participant_sessions.session_id = ?", sessionID).
		Pluck("participant_sessions.participant_id", &agentPersonID).Error; err != nil {
		applogger.Error("GetSessionActivities: failed to query agent participant",
			"session_id", sessionID, "error", err)
		response.InternalError(c, "Failed to query activities")
		return
	}

	// Find the actual agent ID from the person_id
	var agentID int64
	if err := database.DB.Model(&model.Agent{}).
		Where("person_id = ?", agentPersonID).
		Pluck("id", &agentID).Error; err != nil {
		applogger.Error("GetSessionActivities: failed to find agent for person",
			"person_id", agentPersonID, "error", err)
		response.InternalError(c, "Failed to query activities")
		return
	}

	// Build flat timeline of events and inject agent_id
	events := task.BuildActivityEvents(interactions)
	for i := range events {
		events[i].AgentID = agentID
	}
	response.Success(c, events)
}
