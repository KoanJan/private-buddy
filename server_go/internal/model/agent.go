package model

import "time"

type Agent struct {
	ID               int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	Name             string    `gorm:"type:varchar(255);not null" json:"name"`
	CharacterSettings string   `gorm:"type:text;not null;default:'';column:character_settings" json:"character_settings"`
	LLMConfigID      int64     `gorm:"not null;index;column:llm_config_id" json:"llm_config_id"`
	EmbeddingConfigID int64    `gorm:"not null;default:0;index;column:embedding_config_id" json:"embedding_config_id"`
	Description      string    `gorm:"type:text;not null;default:''" json:"description"`
	Avatar           string    `gorm:"type:varchar(500);not null;default:''" json:"avatar"`
	CreatedAt        time.Time `gorm:"not null;autoCreateTime" json:"created_at"`
	UpdatedAt        time.Time `gorm:"not null;autoUpdateTime" json:"updated_at"`
}

func (Agent) TableName() string { return "agents" }
