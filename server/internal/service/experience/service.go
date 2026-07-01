package experience

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"private-buddy-server/internal/database"
	applogger "private-buddy-server/internal/logger"
	"private-buddy-server/internal/model"
	"private-buddy-server/internal/service/llm"
	"private-buddy-server/internal/service/vectorstore"
)

// Package-level state for the experience system singleton.
var (
	embeddingSvc *llm.EmbeddingService
	initOnce     sync.Once
	ready        atomic.Bool
)

// Init sets the embedding service reference for the experience system.
// Must be called once during application startup, before any experience operations.
// Idempotent: only the first call has effect.
// embeddingSvc may be nil if no embedding service is configured.
func Init(es *llm.EmbeddingService) {
	initOnce.Do(func() {
		embeddingSvc = es
		ready.Store(true)
		applogger.Info("Experience system initialized")
	})
}

func panicIfNotReady() {
	if !ready.Load() {
		panic("Experience system not initialized")
	}
}

// createExperience inserts a private experience and computes its embedding
// from the description. Called internally by CheckReflection.
func createExperience(ctx context.Context, agentID int64, source int, sourceFingerprint, title, description, whenToUse, guidelines, pitfalls, procedure string) (*model.AgentExperience, error) {
	emb, err := embeddingSvc.EmbedSingle(ctx, description)
	if err != nil {
		return nil, fmt.Errorf("embed experience description: %w", err)
	}

	exp := &model.AgentExperience{
		AgentID:           agentID,
		Title:             title,
		Description:       description,
		WhenToUse:         whenToUse,
		Guidelines:        guidelines,
		Pitfalls:          pitfalls,
		Procedure:         procedure,
		Source:            source,
		SourceFingerprint: sourceFingerprint,
	}

	if err := database.DB.Create(exp).Error; err != nil {
		return nil, fmt.Errorf("insert agent_experience: %w", err)
	}

	vec := &model.AgentExperienceVector{
		ExperienceID: exp.ID,
		Embedding:    vectorstore.Float32SliceToBlob(emb),
	}
	if err := database.DB.Create(vec).Error; err != nil {
		applogger.Error("Failed to insert agent_experience_vector, orphaned experience row",
			"experience_id", exp.ID,
			"error", err,
		)
		return nil, fmt.Errorf("insert agent_experience_vector: %w", err)
	}

	applogger.Info("AgentExperience created",
		"id", exp.ID,
		"agent_id", agentID,
		"source", source,
	)
	return exp, nil
}

// existsBySourceFingerprint checks whether an experience with the given agent,
// source, and source_fingerprint already exists. Used for dedup before reflection.
func existsBySourceFingerprint(ctx context.Context, agentID int64, source int, sourceFingerprint string) (bool, error) {
	var count int64
	err := database.DB.Model(&model.AgentExperience{}).
		Where("agent_id = ? AND source = ? AND source_fingerprint = ?", agentID, source, sourceFingerprint).
		Count(&count).Error
	return count > 0, err
}
