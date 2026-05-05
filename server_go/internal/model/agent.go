package model

import "time"

// Agent represents an AI assistant configuration.
//
// An agent defines the behavior and capabilities of an AI assistant,
// including its character settings (personality, style, identity)
// and the LLM/embedding configurations to use.
type Agent struct {
	ID               int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	Name             string    `gorm:"type:varchar(255);not null" json:"name"`
	CharacterSettings string   `gorm:"type:text;not null;default:'';column:character_settings" json:"character_settings"` // Agent's personality, style, identity
	LLMConfigID      int64     `gorm:"not null;index;column:llm_config_id" json:"llm_config_id"`
	EmbeddingConfigID int64    `gorm:"not null;default:0;index;column:embedding_config_id" json:"embedding_config_id"`
	Description      string    `gorm:"type:text;not null;default:''" json:"description"`
	Avatar           string    `gorm:"type:varchar(500);not null;default:''" json:"avatar"` // Relative path under PrivateBuddyData/avatars/
	CreatedAt        time.Time `gorm:"not null;autoCreateTime" json:"created_at"`
	UpdatedAt        time.Time `gorm:"not null;autoUpdateTime" json:"updated_at"`
}

func (Agent) TableName() string { return "agents" }
