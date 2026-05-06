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
	"net/http"
	"runtime/debug"
	"strconv"
	"time"

	"private-buddy-server/internal/config"
	"private-buddy-server/internal/model"
	"private-buddy-server/internal/service/chat"
	chatcontext "private-buddy-server/internal/service/chat/chatctx"

	applogger "private-buddy-server/internal/logger"

	"github.com/gin-gonic/gin"
)

// userFriendlyErrorMessage is the default error message shown to users on internal errors.
const userFriendlyErrorMessage = "抱歉，服务器遇到了一些问题，请稍后再试。"

// ConnectionManager manages SSE connections per session.
// Each session can have multiple connected clients (e.g., multiple browser tabs).
// Messages are broadcast to all connections of the same session.
type ConnectionManager struct {
	connections map[int64][]chan string // sessionID -> list of SSE channels
}

// connManager is the global singleton for managing SSE connections.
var connManager = &ConnectionManager{
	connections: make(map[int64][]chan string),
}

// Register creates and registers a new SSE channel for a session.
// Returns the channel for the caller to listen on.
func (cm *ConnectionManager) Register(sessionID int64) chan string {
	ch := make(chan string, 256)
	cm.connections[sessionID] = append(cm.connections[sessionID], ch)
	return ch
}

// Unregister removes an SSE channel from a session and closes it.
// Cleans up the session entry if no connections remain.
func (cm *ConnectionManager) Unregister(sessionID int64, ch chan string) {
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

// Broadcast sends a message to all SSE channels of a session.
// Drops the message if a channel is full (non-blocking send).
func (cm *ConnectionManager) Broadcast(sessionID int64, data string) {
	conns := cm.connections[sessionID]
	for _, ch := range conns {
		select {
		case ch <- data:
		default:
			applogger.L.Warn("SSE channel full, dropping message", "session_id", sessionID)
		}
	}
}

// CreateAndSend creates a new session and sends the first message.
//
// This is the entry point for new conversations. It:
//  1. Creates a new session with the message as title
//  2. Creates the user message record
//  3. Triggers summary generation if needed
//  4. Creates the AI message placeholder (streaming status)
//  5. Starts async chat processing
//
// Returns session_id, trigger_message_id, and ai_message_id.
func (h *Handler) CreateAndSend(c *gin.Context) {
	message := c.Query("message")
	if message == "" {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "message is required"})
		return
	}

	agentIDStr := c.Query("agent_id")
	var agentID int64
	if agentIDStr != "" {
		agentID, _ = strconv.ParseInt(agentIDStr, 10, 64)
	}
	if agentID == 0 {
		var defaultAgent model.Agent
		if err := h.db.First(&defaultAgent).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"detail": "No default agent found"})
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
		Status:  model.SessionStatusStreaming,
	}
	if err := h.db.Create(&session).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}

	userMsg := model.Message{
		SessionID:       session.ID,
		Role:            "user",
		Content:         message,
		Status:          model.MessageStatusCompleted,
		HasInteractions: model.HasInteractionsNone,
	}
	if err := h.db.Select("SessionID", "Role", "Content", "Status", "HasInteractions").Create(&userMsg).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}

	h.triggerSummaryIfNeeded(session.ID)

	aiMsg := model.Message{
		SessionID:       session.ID,
		Role:            "assistant",
		Content:         "",
		Status:          model.MessageStatusStreaming,
		HasInteractions: model.HasInteractionsPending,
	}
	if err := h.db.Select("SessionID", "Role", "Content", "Status", "HasInteractions").Create(&aiMsg).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}

	go h.processChatTask(userMsg.ID, aiMsg.ID, session.ID)

	c.JSON(http.StatusOK, gin.H{
		"session_id":         session.ID,
		"trigger_message_id": userMsg.ID,
		"ai_message_id":      aiMsg.ID,
	})
}

// SendMessage sends a message to an existing session.
//
// This is the entry point for continuing conversations. It:
//  1. Validates the session exists and is not busy
//  2. Creates the user message record
//  3. Triggers summary generation if needed
//  4. Creates the AI message placeholder (streaming status)
//  5. Updates session status to streaming
//  6. Starts async chat processing
//
// Returns trigger_message_id and ai_message_id.
func (h *Handler) SendMessage(c *gin.Context) {
	sessionID := getPathIDFromParam(c, "session_id")

	var session model.Session
	if err := h.db.First(&session, sessionID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "Session not found"})
		return
	}

	if session.Status == model.SessionStatusStreaming {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "Session is busy, please wait for current response to complete"})
		return
	}

	message := c.Query("message")
	if message == "" {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "message is required"})
		return
	}

	userMsg := model.Message{
		SessionID:       sessionID,
		Role:            "user",
		Content:         message,
		Status:          model.MessageStatusCompleted,
		HasInteractions: model.HasInteractionsNone,
	}
	if err := h.db.Select("SessionID", "Role", "Content", "Status", "HasInteractions").Create(&userMsg).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}

	h.triggerSummaryIfNeeded(sessionID)

	aiMsg := model.Message{
		SessionID:       sessionID,
		Role:            "assistant",
		Content:         "",
		Status:          model.MessageStatusStreaming,
		HasInteractions: model.HasInteractionsPending,
	}
	if err := h.db.Select("SessionID", "Role", "Content", "Status", "HasInteractions").Create(&aiMsg).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}

	h.db.Model(&session).Update("status", model.SessionStatusStreaming)

	go h.processChatTask(userMsg.ID, aiMsg.ID, sessionID)

	c.JSON(http.StatusOK, gin.H{
		"trigger_message_id": userMsg.ID,
		"ai_message_id":      aiMsg.ID,
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
	sessionID := getPathIDFromParam(c, "session_id")

	c.Header("Content-Type", "text/event-stream; charset=utf-8")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	var session model.Session
	if err := h.db.First(&session, sessionID).Error; err != nil {
		errorData, _ := json.Marshal(map[string]string{"type": "error", "message": "Session not found"})
		c.SSEvent("", string(errorData))
		return
	}

	var streamingMsg model.Message
	if err := h.db.Where("session_id = ? AND status = ?", sessionID, model.MessageStatusStreaming).
		Order("created_at DESC").First(&streamingMsg).Error; err == nil {
		existingData, _ := json.Marshal(map[string]interface{}{
			"type":       "existing",
			"content":    streamingMsg.Content,
			"message_id": streamingMsg.ID,
		})
		c.SSEvent("", string(existingData))
	}

	ch := connManager.Register(sessionID)
	defer connManager.Unregister(sessionID, ch)

	c.Stream(func(w io.Writer) bool {
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
		case <-time.After(30 * time.Second):
			c.Writer.WriteString(": heartbeat\n\n")
			c.Writer.Flush()
			return true
		}
	})
}

// processChatTask is the async chat processing goroutine.
//
// This method runs in a background goroutine and:
//  1. Retrieves the trigger message, session, agent, and LLM config
//  2. Creates a ChatService instance with SSE callbacks
//  3. Runs the full chat processing pipeline
//  4. Finalizes the AI message with the result
//  5. Broadcasts the "done" event to all SSE clients
//  6. Recovers from panics with graceful error handling
//
// Always sets session status back to idle on completion.
func (h *Handler) processChatTask(triggerMessageID, aiMessageID, sessionID int64) {
	applogger.L.Info("processChatTask started",
		"trigger_message_id", triggerMessageID,
		"ai_message_id", aiMessageID,
		"session_id", sessionID,
	)

	defer func() {
		if r := recover(); r != nil {
			applogger.L.Error("processChatTask panic recovered",
				"session_id", sessionID,
				"panic", r,
				"stack", string(debug.Stack()),
			)
			h.finalizeAIMessage(aiMessageID, userFriendlyErrorMessage)
			doneData, _ := json.Marshal(map[string]string{"type": "done"})
			connManager.Broadcast(sessionID, string(doneData))
		}
		h.db.Model(&model.Session{}).Where("id = ?", sessionID).Update("status", model.SessionStatusIdle)
		applogger.L.Info("processChatTask completed", "session_id", sessionID)
	}()

	var triggerMsg model.Message
	if err := h.db.First(&triggerMsg, triggerMessageID).Error; err != nil {
		h.finalizeAIMessage(aiMessageID, userFriendlyErrorMessage)
		doneData, _ := json.Marshal(map[string]string{"type": "done"})
		connManager.Broadcast(sessionID, string(doneData))
		return
	}

	if triggerMsg.Role != "user" {
		applogger.L.Error("Trigger message is not from user",
			"session_id", sessionID,
			"trigger_message_id", triggerMessageID,
			"role", triggerMsg.Role,
		)
		h.finalizeAIMessage(aiMessageID, userFriendlyErrorMessage)
		doneData, _ := json.Marshal(map[string]string{"type": "done"})
		connManager.Broadcast(sessionID, string(doneData))
		return
	}

	session := h.dataService.GetSession(h.db, sessionID)
	if session == nil {
		h.finalizeAIMessage(aiMessageID, userFriendlyErrorMessage)
		doneData, _ := json.Marshal(map[string]string{"type": "done"})
		connManager.Broadcast(sessionID, string(doneData))
		return
	}

	agent := h.dataService.GetAgent(h.db, session.AgentID)
	if agent == nil {
		h.finalizeAIMessage(aiMessageID, userFriendlyErrorMessage)
		doneData, _ := json.Marshal(map[string]string{"type": "done"})
		connManager.Broadcast(sessionID, string(doneData))
		return
	}

	llmConfig := h.dataService.GetLLMConfig(h.db, agent.LLMConfigID)
	if llmConfig == nil {
		h.finalizeAIMessage(aiMessageID, userFriendlyErrorMessage)
		doneData, _ := json.Marshal(map[string]string{"type": "done"})
		connManager.Broadcast(sessionID, string(doneData))
		return
	}

	chatService := chat.NewChatService(h.db, session, agent, llmConfig)
	chatService.SetOnChunk(func(chunk string) {
		data, _ := json.Marshal(map[string]string{"type": "chunk", "content": chunk})
		connManager.Broadcast(sessionID, string(data))
	})
	chatService.SetOnNotify(func(data string) {
		connManager.Broadcast(sessionID, data)
	})

	result, err := chatService.Process(triggerMessageID, aiMessageID)
	if err != nil {
		applogger.L.Error("Chat processing failed", "error", err)
		h.finalizeAIMessage(aiMessageID, userFriendlyErrorMessage)
		doneData, _ := json.Marshal(map[string]string{"type": "done"})
		connManager.Broadcast(sessionID, string(doneData))
		return
	}

	h.finalizeAIMessage(aiMessageID, result)

	doneData, _ := json.Marshal(map[string]string{"type": "done"})
	connManager.Broadcast(sessionID, string(doneData))
}

// finalizeAIMessage updates the AI message with final content and marks it as completed.
func (h *Handler) finalizeAIMessage(aiMessageID int64, content string) {
	h.db.Model(&model.Message{}).Where("id = ?", aiMessageID).Updates(map[string]interface{}{
		"content": content,
		"status":  model.MessageStatusCompleted,
	})
}

// triggerSummaryIfNeeded checks if summary generation should be triggered
// based on the current message count and the configured summary window size.
// Summary is triggered when message count >= summary_window_size.
func (h *Handler) triggerSummaryIfNeeded(sessionID int64) {
	settings := config.Get()

	var messageCount int64
	h.db.Model(&model.Message{}).Where("session_id = ?", sessionID).Count(&messageCount)

	if messageCount >= int64(settings.SummaryWindowSize) {
		applogger.L.Info("Triggering summary generation", "session_id", sessionID, "V", messageCount)
		go h.generateSummary(sessionID, int(messageCount), settings.SummaryWindowSize)
	}
}

// generateSummary runs summary generation in a background goroutine.
func (h *Handler) generateSummary(sessionID int64, version int, windowSize int) {
	chatcontext.GenerateSummaryForSession(h.db, h.dataService, sessionID, version, windowSize)
}

// getPathIDFromParam extracts an int64 path parameter from the URL.
// Returns 0 if the parameter is not a valid integer.
func getPathIDFromParam(c *gin.Context, param string) int64 {
	idStr := c.Param(param)
	id, _ := strconv.ParseInt(idStr, 10, 64)
	return id
}
