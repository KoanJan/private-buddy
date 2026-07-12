package model

import "time"

// AgentDelivery records a file delivery from one person to another.
// Each delivery creates a delivery_N subdirectory under the recipient's received/
// directory to avoid conflicts between multiple deliveries in the same session.
type AgentDelivery struct {
	ID           int64     `gorm:"primaryKey;autoIncrement;type:INTEGER PRIMARY KEY AUTOINCREMENT" json:"id"`
	FromPersonID int64     `gorm:"not null;index:idx_from_person;column:from_person_id" json:"from_person_id"`
	ToPersonID   int64     `gorm:"not null;index:idx_to_person;column:to_person_id" json:"to_person_id"`
	SessionID    int64     `gorm:"not null;index:idx_session;column:session_id" json:"session_id"`
	Paths        string    `gorm:"type:text;not null" json:"paths"`             // JSON array of relative paths delivered
	Remark       string    `gorm:"type:text;not null;default:''" json:"remark"` // Optional note from the sender
	CreatedAt    time.Time `gorm:"not null;autoCreateTime;column:created_at" json:"created_at"`
}

// TableName returns the database table name for AgentDelivery.
func (AgentDelivery) TableName() string { return "agent_deliveries" }
