package model

import "time"

// ModelCapability caches LLM model feature support status.
// Keyed by (base_url, model_id) to distinguish the same model ID behind different proxies.
// Only records discovery results — absence means "untested", not "supported".
type ModelCapability struct {
	ID                 int64     `gorm:"primaryKey;autoIncrement;type:INTEGER PRIMARY KEY AUTOINCREMENT" json:"id"`
	BaseURL            string    `gorm:"type:varchar(500);not null;column:base_url;uniqueIndex:idx_model_capability_key" json:"base_url"`
	ModelID            string    `gorm:"type:varchar(100);not null;column:model_id;uniqueIndex:idx_model_capability_key" json:"model_id"`
	SupportsJSONSchema int       `gorm:"type:integer;not null;default:0;column:supports_json_schema" json:"supports_json_schema"`
	CreatedAt          time.Time `gorm:"not null;autoCreateTime" json:"created_at"`
	UpdatedAt          time.Time `gorm:"not null;autoUpdateTime" json:"updated_at"`
}

// TableName returns the database table name for ModelCapability.
func (ModelCapability) TableName() string { return "model_capabilities" }