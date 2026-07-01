package model

import "time"

// SystemLLMConfig stores a reference to the LLM config used for system-level
// operations (e.g., skill ingestion) that are not bound to any agent.
// A single row (id=1) stores the llm_config_id reference.
//
// Design mirrors the embedding config pattern: a global singleton row
// that the admin configures via the UI.
type SystemLLMConfig struct {
	ID          int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	LLMConfigID int64     `gorm:"not null;column:llm_config_id" json:"llm_config_id"`
	CreatedAt   time.Time `gorm:"not null;autoCreateTime" json:"created_at"`
	UpdatedAt   time.Time `gorm:"not null;autoUpdateTime" json:"updated_at"`
}

func (SystemLLMConfig) TableName() string { return "system_llm_configs" }
