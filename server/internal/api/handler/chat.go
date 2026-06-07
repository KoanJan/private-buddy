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
	"context"
	"encoding/json"
	"io"
	"net/http"
	"runtime/debug"
	"strconv"
	"sync"
	"time"

	"private-buddy-server/internal/config"
	"private-buddy-server/internal/database"
	"private-buddy-server/internal/model"
	"private-buddy-server/internal/service"
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

// PushToSession sends a message to all SSE channels of a session.
// Drops the message if a channel is full (non-blocking send).
func (cm *ConnectionManager) PushToSession(sessionID int64, data string) {
	conns := cm.connections[sessionID]
	for _, ch := range conns {
		select {
		case ch <- data:
		default:
			// TODO: Notify the client to refresh and reset the SSE connection when messages
			// are dropped. Without this, the client will have an incomplete message list,
			// causing a cognitive gap for the user who sees stale/partial data.
			applogger.L.Warn("SSE channel full, dropping message", "session_id", sessionID)
		}
	}
}

// TaskCancelManager tracks running processChatTask goroutines per session.
// When a session is deleted, CancelSession is called to abort all associated tasks.
type TaskCancelManager struct {
	mu      sync.Mutex
	cancels map[int64][]*taskCancel // sessionID -> list of cancel handles
	nextID  int64
}

// taskCancel wraps a cancel function with a unique ID for identification.
type taskCancel struct {
	id     int64
	cancel context.CancelFunc
}

// taskCancelMgr is the global singleton for managing task cancellation.
var taskCancelMgr = &TaskCancelManager{
	cancels: make(map[int64][]*taskCancel),
}

// Register adds a cancel function for a session's running task.
// Returns a handle ID for later unregistration.
func (tcm *TaskCancelManager) Register(sessionID int64, cancel context.CancelFunc) int64 {
	tcm.mu.Lock()
	defer tcm.mu.Unlock()
	tcm.nextID++
	handle := &taskCancel{id: tcm.nextID, cancel: cancel}
	tcm.cancels[sessionID] = append(tcm.cancels[sessionID], handle)
	return handle.id
}

// CancelSession cancels all running tasks for a session and cleans up.
func (tcm *TaskCancelManager) CancelSession(sessionID int64) {
	tcm.mu.Lock()
	defer tcm.mu.Unlock()
	for _, tc := range tcm.cancels[sessionID] {
		tc.cancel()
	}
	delete(tcm.cancels, sessionID)
}

// Unregister removes a cancel handle after the task completes normally.
func (tcm *TaskCancelManager) Unregister(sessionID int64, handleID int64) {
	tcm.mu.Lock()
	defer tcm.mu.Unlock()
	cancels := tcm.cancels[sessionID]
	for i, tc := range cancels {
		if tc.id == handleID {
			tcm.cancels[sessionID] = append(cancels[:i], cancels[i+1:]...)
			break
		}
	}
	if len(tcm.cancels[sessionID]) == 0 {
		delete(tcm.cancels, sessionID)
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
		if err := database.DB.First(&defaultAgent).Error; err != nil {
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
	if err := database.DB.Create(&session).Error; err != nil {
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
	if err := database.DB.Select("SessionID", "Role", "Content", "Status", "HasInteractions").Create(&userMsg).Error; err != nil {
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
	if err := database.DB.Select("SessionID", "Role", "Content", "Status", "HasInteractions").Create(&aiMsg).Error; err != nil {
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
	sessionID := getPathIDByParam(c, "session_id")

	var session model.Session
	if err := database.DB.First(&session, sessionID).Error; err != nil {
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
	if err := database.DB.Select("SessionID", "Role", "Content", "Status", "HasInteractions").Create(&userMsg).Error; err != nil {
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
	if err := database.DB.Select("SessionID", "Role", "Content", "Status", "HasInteractions").Create(&aiMsg).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}

	database.DB.Model(&session).Update("status", model.SessionStatusStreaming)

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
//  1. Registers a cancel handle so the task can be aborted on session deletion
//  2. Retrieves the trigger message, session, agent, and LLM config
//  3. Creates a ChatService instance with SSE callbacks
//  4. Runs the full chat processing pipeline
//  5. Finalizes the AI message with the result
//  6. Broadcasts the "message" event to all SSE clients
//  7. Recovers from panics with graceful error handling
//
// Always sets session status back to idle on completion.
func (h *Handler) processChatTask(triggerMessageID, aiMessageID, sessionID int64) {
	ctx, cancel := context.WithCancel(context.Background())
	handleID := taskCancelMgr.Register(sessionID, cancel)

	applogger.L.Info("processChatTask started",
		"trigger_message_id", triggerMessageID,
		"ai_message_id", aiMessageID,
		"session_id", sessionID,
	)

	defer func() {
		cancel()
		taskCancelMgr.Unregister(sessionID, handleID)

		if r := recover(); r != nil {
			applogger.L.Error("processChatTask panic recovered",
				"session_id", sessionID,
				"panic", r,
				"stack", string(debug.Stack()),
			)
			h.finalizeAIMessage(aiMessageID, userFriendlyErrorMessage)
			h.pushMessage(sessionID, aiMessageID, userFriendlyErrorMessage)
		}
		database.DB.Model(&model.Session{}).Where("id = ?", sessionID).Update("status", model.SessionStatusIdle)
		applogger.L.Info("processChatTask completed", "session_id", sessionID)
	}()

	// Check if the task was cancelled before starting work
	if ctx.Err() != nil {
		applogger.L.Info("processChatTask cancelled before execution", "session_id", sessionID)
		return
	}

	var triggerMsg model.Message
	if err := database.DB.First(&triggerMsg, triggerMessageID).Error; err != nil {
		h.finalizeAIMessage(aiMessageID, userFriendlyErrorMessage)
		h.pushMessage(sessionID, aiMessageID, userFriendlyErrorMessage)
		return
	}

	if triggerMsg.Role != "user" {
		applogger.L.Error("Trigger message is not from user",
			"session_id", sessionID,
			"trigger_message_id", triggerMessageID,
			"role", triggerMsg.Role,
		)
		h.finalizeAIMessage(aiMessageID, userFriendlyErrorMessage)
		h.pushMessage(sessionID, aiMessageID, userFriendlyErrorMessage)
		return
	}

	session := service.GetSession(sessionID)
	if session == nil {
		h.finalizeAIMessage(aiMessageID, userFriendlyErrorMessage)
		h.pushMessage(sessionID, aiMessageID, userFriendlyErrorMessage)
		return
	}

	agent := service.GetAgent(session.AgentID)
	if agent == nil {
		h.finalizeAIMessage(aiMessageID, userFriendlyErrorMessage)
		h.pushMessage(sessionID, aiMessageID, userFriendlyErrorMessage)
		return
	}

	llmConfig := service.GetLLMConfig(agent.LLMConfigID)
	if llmConfig == nil {
		h.finalizeAIMessage(aiMessageID, userFriendlyErrorMessage)
		h.pushMessage(sessionID, aiMessageID, userFriendlyErrorMessage)
		return
	}

	callbacks := &chat.ChatCallbacks{
		OnNotify: func(data string) {
			connManager.PushToSession(sessionID, data)
		},
	}

	result, err := chat.Process(ctx, session, agent, llmConfig, triggerMessageID, aiMessageID, callbacks)
	if err != nil {
		// If cancelled, skip finalization to avoid overwriting a new session's data
		if ctx.Err() != nil {
			applogger.L.Info("processChatTask cancelled, skipping finalization",
				"session_id", sessionID,
			)
			return
		}
		applogger.L.Error("Chat processing failed", "error", err)
		h.finalizeAIMessage(aiMessageID, userFriendlyErrorMessage)
		h.pushMessage(sessionID, aiMessageID, userFriendlyErrorMessage)
		return
	}

	// If cancelled after processing, skip finalization to avoid overwriting
	if ctx.Err() != nil {
		applogger.L.Info("processChatTask cancelled after processing, skipping finalization",
			"session_id", sessionID,
		)
		return
	}

	h.finalizeAIMessage(aiMessageID, result)
	h.pushMessage(sessionID, aiMessageID, result)
}

// pushMessage pushes a complete message event to all SSE clients of a session.
func (h *Handler) pushMessage(sessionID, messageID int64, content string) {
	msgData, _ := json.Marshal(map[string]interface{}{
		"type":       "message",
		"message_id": messageID,
		"content":    content,
	})
	connManager.PushToSession(sessionID, string(msgData))
}

// finalizeAIMessage updates the AI message with final content and marks it as completed.
func (h *Handler) finalizeAIMessage(aiMessageID int64, content string) {
	database.DB.Model(&model.Message{}).Where("id = ?", aiMessageID).Updates(map[string]interface{}{
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
	database.DB.Model(&model.Message{}).Where("session_id = ?", sessionID).Count(&messageCount)

	if messageCount >= int64(settings.SummaryWindowSize) {
		applogger.L.Info("Triggering summary generation", "session_id", sessionID, "V", messageCount)
		go h.generateSummary(sessionID, int(messageCount), settings.SummaryWindowSize)
	}
}

// generateSummary runs summary generation in a background goroutine.
func (h *Handler) generateSummary(sessionID int64, version int, windowSize int) {
	chatcontext.GenerateSummaryForSession(sessionID, version, windowSize)
}
