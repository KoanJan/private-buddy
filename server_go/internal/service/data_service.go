package service

import (
	"private-buddy-server/internal/model"

	applogger "private-buddy-server/internal/logger"
	"gorm.io/gorm"
)

type DataService struct{}

func NewDataService() *DataService {
	return &DataService{}
}

func (ds *DataService) GetSession(db *gorm.DB, sessionID int64) *model.Session {
	var session model.Session
	if err := db.First(&session, sessionID).Error; err != nil {
		applogger.L.Error("Session not found", "session_id", sessionID, "error", err)
		return nil
	}
	return &session
}

func (ds *DataService) GetAgent(db *gorm.DB, agentID int64) *model.Agent {
	var agent model.Agent
	if err := db.First(&agent, agentID).Error; err != nil {
		applogger.L.Error("Agent not found", "agent_id", agentID, "error", err)
		return nil
	}
	return &agent
}

func (ds *DataService) GetLLMConfig(db *gorm.DB, llmConfigID int64) *model.LLMConfig {
	var config model.LLMConfig
	if err := db.First(&config, llmConfigID).Error; err != nil {
		applogger.L.Error("LLM config not found", "llm_config_id", llmConfigID, "error", err)
		return nil
	}
	return &config
}

type MessageHistoryItem struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func (ds *DataService) GetMessageHistory(db *gorm.DB, sessionID int64, beforeMessageID *int64, limit *int) []MessageHistoryItem {
	query := db.Model(&model.Message{}).Where("session_id = ?", sessionID)

	if beforeMessageID != nil {
		query = query.Where("id < ?", *beforeMessageID)
	}

	var messages []model.Message
	if limit != nil {
		query = query.Order("id DESC").Limit(*limit).Find(&messages)
		for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
			messages[i], messages[j] = messages[j], messages[i]
		}
	} else {
		query = query.Order("created_at ASC").Find(&messages)
	}

	history := make([]MessageHistoryItem, 0, len(messages))
	for _, msg := range messages {
		history = append(history, MessageHistoryItem{
			Role:    msg.Role,
			Content: msg.Content,
		})
	}

	applogger.L.Info("Loaded message history", "session_id", sessionID, "count", len(history))
	return history
}
