// Package handler implements the HTTP API handlers for the chat system.
//
// This package provides the Gin-based HTTP handlers that expose the chat
// functionality via REST API endpoints. It handles:
//   - Creating new sessions and sending the first message
//   - Sending messages to existing sessions
//   - Streaming AI responses via Server-Sent Events (SSE)
//   - Managing SSE connection lifecycle
//   - Triggering background summary generation
//
// The handler layer is responsible for:
//   - Request validation and parameter extraction
//   - Database record creation (session, messages)
//   - Asynchronous chat processing via goroutines
//   - SSE event broadcasting to connected clients
//   - Error handling and graceful degradation
package handler

import (
	"encoding/json"
	"io"
	"strconv"
	"time"

	"private-buddy-server/internal/database"
	"private-buddy-server/internal/model"
	"private-buddy-server/internal/service/eventqueue"
	"private-buddy-server/internal/service/memory"

	applogger "private-buddy-server/internal/logger"

	"private-buddy-server/internal/api/response"

	"github.com/gin-gonic/gin"
)

// userFriendlyErrorMessage is the default error message shown to users on internal errors.
const userFriendlyErrorMessage = "Sorry, something went wrong on the server. Please try again later."

// connectionManager manages SSE connections per session.
// Each session can have multiple connected clients (e.g., multiple browser tabs).
// Messages are broadcast to all connections of the same session.
type connectionManager struct {
	connections map[int64][]chan string // sessionID -> list of SSE channels
}

// connManager is the global singleton for managing SSE connections.
var connManager = &connectionManager{
	connections: make(map[int64][]chan string),
}

// Register creates and registers a new SSE channel for a session.
// Returns the channel for the caller to listen on.
func (cm *connectionManager) Register(sessionID int64) chan string {
	ch := make(chan string, 256)
	cm.connections[sessionID] = append(cm.connections[sessionID], ch)
	return ch
}

// Unregister removes an SSE channel from a session and closes it.
// Cleans up the session entry if no connections remain.
func (cm *connectionManager) Unregister(sessionID int64, ch chan string) {
	conns := cm.connections[sessionID]
	for i, c := range conns {
		if c == ch {
			cm.connections[sessionID] = append(conns[:i], conns[i+1:]...)
			close(c)
			break
		}
	}
	if len(cm.connections[sessionID]) == 0 {
		delete(cm.connections, sessionID)
	}
}

// PushToSession sends a message to all SSE channels of a session.
// Drops the message if a channel is full (non-blocking send).
func (cm *connectionManager) PushToSession(sessionID int64, data string) {
	conns := cm.connections[sessionID]
	for _, ch := range conns {
		select {
		case ch <- data:
		default:
			// TODO: Notify the client to refresh and reset the SSE connection when messages
			// are dropped. Without this, the client will have an incomplete message list,
			// causing a cognitive gap for the user who sees stale/partial data.
			applogger.Warn("SSE channel full, dropping message", "session_id", sessionID)
		}
	}
}

// PushSSEToSession is the exported wrapper for pushing SSE events to a session.
// Used by the runtime package to push agent_status and message events.
func PushSSEToSession(sessionID int64, data string) {
	connManager.PushToSession(sessionID, data)
}

// CreateAndSend creates a new session and sends the first message.
//
// This is the entry point for new conversations. It:
//  1. Creates a new session with the message as title
//  2. Creates the user message record
//  3. Triggers summary generation if needed
//  4. Sends an event to the Agent Runtime (no placeholder AI message)
//
// The Agent Runtime will create a Work, which uses a draft for content
// accumulation. When the Work completes, the draft is committed to the
// messages table and pushed via SSE.
//
// Returns session_id and trigger_message_id.
func (h *Handler) CreateAndSend(c *gin.Context) {
	message := c.Query("message")
	if message == "" {
		response.BadRequest(c, "message is required")
		return
	}

	agentIDStr := c.Query("agent_id")
	var agentID int64
	if agentIDStr != "" {
		agentID, _ = strconv.ParseInt(agentIDStr, 10, 64)
	}
	applogger.Info("CreateAndSend received agent_id param", "raw", agentIDStr, "parsed", agentID)
	if agentID == 0 {
		var defaultAgent model.Agent
		if err := database.DB.First(&defaultAgent).Error; err != nil {
			response.InternalError(c, "No default agent found")
			return
		}
		agentID = defaultAgent.ID
	}

	title := c.Query("title")
	if title == "" {
		runes := []rune(message)
		if len(runes) > 15 {
			title = string(runes[:15]) + "..."
		} else {
			title = message
		}
	}

	session := model.Session{
		Title:   title,
		AgentID: agentID,
	}
	if err := database.DB.Create(&session).Error; err != nil {
		response.InternalError(c, err.Error())
		return
	}

	// Create participant_sessions records for user and agent
	if err := database.DB.Create(&model.ParticipantSession{
		SessionID:       session.ID,
		ParticipantType: model.ParticipantTypeUser,
		ParticipantID:   1, // TODO: replace with actual user ID from auth context
		Role:            model.ParticipantRoleOwner,
		Status:          model.ParticipantStatusIdle,
	}).Error; err != nil {
		applogger.Error("failed to create user participant for session", "session_id", session.ID, "error", err)
	}
	if err := database.DB.Create(&model.ParticipantSession{
		SessionID:       session.ID,
		ParticipantType: model.ParticipantTypeAgent,
		ParticipantID:   agentID,
		Role:            model.ParticipantRoleMember,
		Status:          model.ParticipantStatusIdle,
	}).Error; err != nil {
		applogger.Error("failed to create agent participant for session", "session_id", session.ID, "error", err)
	}

	userMsg := model.Message{
		SessionID:       session.ID,
		Role:            model.MessageRoleUser,
		Content:         message,
		Status:          model.MessageStatusCompleted,
		HasInteractions: model.HasInteractionsNone,
	}
	if err := database.DB.Select("SessionID", "Role", "Content", "Status", "HasInteractions").Create(&userMsg).Error; err != nil {
		response.InternalError(c, err.Error())
		return
	}

	// Submit to the event vectorization service for embedding + observation.
	memory.SubmitVectorization(memory.VectorizationTask{
		MessageID: userMsg.ID,
		SessionID: userMsg.SessionID,
		Content:   userMsg.Content,
	})

	// Update user's last_read_message_id — user has seen all messages up to this point
	if err := database.DB.Model(&model.ParticipantSession{}).
		Where("session_id = ? AND participant_type = ? AND participant_id = ?",
			session.ID, model.ParticipantTypeUser, 1).
		Update("last_read_message_id", userMsg.ID).Error; err != nil {
		applogger.Warn("failed to update last_read_message_id on session create", "session_id", session.ID, "error", err)
	}

	// Send event to Agent Runtime instead of creating placeholder AI message
	h.sendEventToRuntime(agentID, session.ID, userMsg.ID, message)

	response.Success(c, gin.H{
		"session_id":         session.ID,
		"trigger_message_id": userMsg.ID,
	})
}

// SendMessage sends a message to an existing session.
//
// This is the entry point for continuing conversations. It:
//  1. Validates the session exists
//  2. Creates the user message record
//  3. Triggers summary generation if needed
//  4. Sends an event to the Agent Runtime (no placeholder AI message)
//
// The Agent Runtime handles the event asynchronously — if an active Work
// exists in this session, the event is absorbed; otherwise a new Work is created.
//
// Returns trigger_message_id.
func (h *Handler) SendMessage(c *gin.Context) {
	sessionID := getPathIDByParam(c, "session_id")

	var session model.Session
	if err := database.DB.First(&session, sessionID).Error; err != nil {
		response.NotFound(c, "Session not found")
		return
	}

	message := c.Query("message")
	if message == "" {
		response.BadRequest(c, "message is required")
		return
	}

	userMsg := model.Message{
		SessionID:       sessionID,
		Role:            model.MessageRoleUser,
		Content:         message,
		Status:          model.MessageStatusCompleted,
		HasInteractions: model.HasInteractionsNone,
	}
	if err := database.DB.Select("SessionID", "Role", "Content", "Status", "HasInteractions").Create(&userMsg).Error; err != nil {
		response.InternalError(c, err.Error())
		return
	}

	// Submit to the event vectorization service for embedding + observation.
	memory.SubmitVectorization(memory.VectorizationTask{
		MessageID: userMsg.ID,
		SessionID: userMsg.SessionID,
		Content:   userMsg.Content,
	})

	// Update user's last_read_message_id — user has seen all messages up to this point
	if err := database.DB.Model(&model.ParticipantSession{}).
		Where("session_id = ? AND participant_type = ? AND participant_id = ?",
			sessionID, model.ParticipantTypeUser, 1).
		Update("last_read_message_id", userMsg.ID).Error; err != nil {
		applogger.Warn("failed to update last_read_message_id on continue", "session_id", sessionID, "error", err)
	}

	// Send event to Agent Runtime instead of creating placeholder AI message
	h.sendEventToRuntime(session.AgentID, sessionID, userMsg.ID, message)

	response.Success(c, gin.H{
		"trigger_message_id": userMsg.ID,
	})
}

// StreamMessages handles SSE streaming for a session.
//
// Establishes a Server-Sent Events connection that:
//  1. Sends any existing streaming message content (reconnection support)
//  2. Registers an SSE channel for real-time updates
//  3. Streams chunks, notifications, and done/error events
//  4. Sends heartbeat keep-alive every 30 seconds
//  5. Cleans up on client disconnect or stream completion
func (h *Handler) StreamMessages(c *gin.Context) {
	sessionID := getPathIDByParam(c, "session_id")

	c.Header("Content-Type", "text/event-stream; charset=utf-8")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	var session model.Session
	if err := database.DB.First(&session, sessionID).Error; err != nil {
		errorData, _ := json.Marshal(map[string]string{"type": "error", "message": "Session not found"})
		c.SSEvent("", string(errorData))
		return
	}

	ch := connManager.Register(sessionID)
	defer connManager.Unregister(sessionID, ch)

	c.Stream(func(w io.Writer) bool {
		heartbeat := time.NewTimer(30 * time.Second)
		defer heartbeat.Stop()

		select {
		case data, ok := <-ch:
			if !ok {
				return false
			}
			c.SSEvent("", data)
			var parsed map[string]interface{}
			if json.Unmarshal([]byte(data), &parsed) == nil {
				if t, ok := parsed["type"].(string); ok && (t == "done" || t == "error") {
					return false
				}
			}
			return true
		case <-c.Request.Context().Done():
			return false
		case <-heartbeat.C:
			c.Writer.WriteString(": heartbeat\n\n")
			c.Writer.Flush()
			return true
		}
	})
}

// sendEventToRuntime ensures the agent runtime is running and sends a new
// message event to the agent via the global event queue.
// This is the only path for user messages to reach the agent.
func (h *Handler) sendEventToRuntime(agentID, sessionID, messageID int64, messageContent string) {
	event := &eventqueue.AgentEvent{
		Type:      eventqueue.EventTypeNewMessage,
		SessionID: sessionID,
		Payload: &eventqueue.NewMessagePayload{
			MessageID:      messageID,
			MessageContent: messageContent,
		},
	}
	eventqueue.SendEvent(agentID, event)
}

// sessionAgentStatus represents an agent's status within a session.
type sessionAgentStatus struct {
	AgentID int64  `json:"agent_id"`
	Name    string `json:"name"`
	Avatar  string `json:"avatar"`
	Status  int    `json:"status"` // 0=idle, 1=working
}

// GetSessionAgents returns all agents in a session with their current status.
// Used by the frontend to display agent status indicators.
func (h *Handler) GetSessionAgents(c *gin.Context) {
	sessionIDStr := c.Param("session_id")
	sessionID, err := strconv.ParseInt(sessionIDStr, 10, 64)
	if err != nil {
		response.BadRequest(c, "invalid session_id")
		return
	}

	// Find all agent participants in this session
	var participants []model.ParticipantSession
	if err := database.DB.Where("session_id = ? AND participant_type = ?",
		sessionID, model.ParticipantTypeAgent).
		Find(&participants).Error; err != nil {
		response.InternalError(c, "failed to query participants")
		return
	}

	result := make([]sessionAgentStatus, 0, len(participants))
	for _, p := range participants {
		var agent model.Agent
		if err := database.DB.First(&agent, p.ParticipantID).Error; err != nil {
			continue
		}

		result = append(result, sessionAgentStatus{
			AgentID: agent.ID,
			Name:    agent.Name,
			Avatar:  agent.Avatar,
			Status:  p.Status, // Read directly from ParticipantSession.Status
		})
	}

	response.Success(c, result)
}
