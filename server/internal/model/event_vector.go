package model

import "time"

// EventVector stores the embedding vector for an event.
//
// A single embedding is shared across all agent observations of the same event,
// avoiding redundant embedding computation. Retrieval uses the chain:
// observation → event_id → event_vectors.embedding.
//
// event_id is used directly as the primary key since the relationship is
// strictly 1:1 — one event has exactly one embedding.
type EventVector struct {
	EventID   int64     `gorm:"primaryKey;column:event_id" json:"event_id"`
	Embedding []byte    `gorm:"not null;type:blob" json:"embedding"`
	CreatedAt time.Time `gorm:"not null;autoCreateTime" json:"created_at"`
}

// TableName returns the database table name for EventVector.
func (EventVector) TableName() string { return "event_vectors" }
