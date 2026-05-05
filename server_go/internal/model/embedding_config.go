package model

import "time"

// EmbeddingConfig stores the configuration for the embedding API provider.
// Used for RAG (Retrieval-Augmented Generation) vector operations.
type EmbeddingConfig struct {
	ID          int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	Name        string    `gorm:"type:varchar(100);not null" json:"name"`
	ModelID     string    `gorm:"type:varchar(100);not null;column:model_id" json:"model_id"`
	BaseURL     string    `gorm:"type:varchar(255);not null;column:base_url" json:"base_url"`
	APIKey      string    `gorm:"type:varchar(255);not null;column:api_key" json:"api_key"`
	Description string    `gorm:"type:text;not null;default:''" json:"description"`
	CreatedAt   time.Time `gorm:"not null;autoCreateTime" json:"created_at"`
	UpdatedAt   time.Time `gorm:"not null;autoUpdateTime" json:"updated_at"`
}

func (EmbeddingConfig) TableName() string { return "embedding_configs" }
