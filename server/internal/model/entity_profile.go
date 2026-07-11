package model

import "time"

// Entity type constants for entity_profiles.entity_type.
const (
	EntityTypePerson  = 1 // Profile about a person (AI or human)
	EntityTypeSession = 2 // Profile about a session
)

// EntityProfile represents a directional narrative that one agent has formed
// about a specific entity (person or session).
//
// Unlike observations (which are mechanical event recordings), EntityProfile
// is an LLM-generated reflective narrative: "What do I think about X?"
//
// Key design:
//   - Each (agent_id, entity_type, entity_id) has exactly one profile row.
//     New reflections replace the old narrative (update, not append).
//   - Evidence selection: top K observations sorted by importance DESC (id DESC
//     as tiebreaker), with no survival_count gate.
//   - input_md5 is the MD5 hash of the evidence text at generation time. It is
//     compared before re-generation to skip when input is unchanged.
//   - Each generation is fresh — no prior narrative is fed to the LLM.
//   - Rate limit: same profile at most once per 24 hours.
type EntityProfile struct {
	ID            int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	AgentID       int64     `gorm:"not null;uniqueIndex:idx_entity_profile;column:agent_id" json:"agent_id"`
	EntityType    int       `gorm:"not null;uniqueIndex:idx_entity_profile;column:entity_type" json:"entity_type"`
	EntityID      int64     `gorm:"not null;uniqueIndex:idx_entity_profile;column:entity_id" json:"entity_id"`
	Narrative     string    `gorm:"type:text;not null" json:"narrative"`
	EvidenceCount int       `gorm:"not null;default:0;column:evidence_count" json:"evidence_count"`
	InputMD5      string    `gorm:"not null;default:'';column:input_md5" json:"input_md5"`
	LastUpdatedAt time.Time `gorm:"not null;autoUpdateTime;column:last_updated_at" json:"last_updated_at"`
	CreatedAt     time.Time `gorm:"not null;autoCreateTime" json:"created_at"`
}

// TableName returns the database table name for EntityProfile.
func (EntityProfile) TableName() string { return "entity_profiles" }
