package experience

import (
	"context"
	"fmt"
	"sort"

	"private-buddy-server/internal/database"
	applogger "private-buddy-server/internal/logger"
	"private-buddy-server/internal/model"
	"private-buddy-server/internal/service/vectorstore"
)

// SearchResult wraps an AgentExperience with its cosine similarity score.
type SearchResult struct {
	Experience model.AgentExperience
	Score      float64 // Cosine similarity, 0..1
}

// SearchExperiences performs semantic retrieval against an agent's private
// experiences. Returns results sorted by descending cosine similarity,
// filtered by minScore.
// Returns nil, nil when the embedding service is not configured.
func SearchExperiences(ctx context.Context, agentID int64, taskDescription string, topN int, minScore float64) ([]SearchResult, error) {
	if embeddingSvc == nil {
		return nil, nil
	}
	panicIfNotReady()

	queryVec, err := embeddingSvc.EmbedSingle(ctx, taskDescription)
	if err != nil {
		return nil, fmt.Errorf("embed task description: %w", err)
	}
	if len(queryVec) == 0 {
		return nil, nil
	}

	type expWithVec struct {
		exp model.AgentExperience
		vec []float32
	}
	var candidates []expWithVec

	var allVectors []model.AgentExperienceVector
	if err := database.DB.Find(&allVectors).Error; err != nil {
		return nil, fmt.Errorf("load experience vectors: %w", err)
	}

	for _, v := range allVectors {
		var exp model.AgentExperience
		if err := database.DB.Where("id = ? AND agent_id = ?", v.ExperienceID, agentID).First(&exp).Error; err != nil {
			continue
		}
		candidates = append(candidates, expWithVec{
			exp: exp,
			vec: vectorstore.BlobToFloat32Slice(v.Embedding),
		})
	}

	if len(candidates) == 0 {
		return nil, nil
	}

	type scoreEntry struct {
		result SearchResult
		score  float64
	}
	var entries []scoreEntry

	for _, c := range candidates {
		sim := vectorstore.CosineSimilarity(queryVec, c.vec)
		if sim >= minScore {
			entries = append(entries, scoreEntry{
				result: SearchResult{Experience: c.exp, Score: sim},
				score:  sim,
			})
		}
	}

	sort.Slice(entries, func(i, j int) bool { return entries[i].score > entries[j].score })

	if len(entries) > topN {
		entries = entries[:topN]
	}

	results := make([]SearchResult, len(entries))
	for i, s := range entries {
		results[i] = s.result
	}

	applogger.Info("Experience retrieval completed",
		"agent_id", agentID,
		"candidates", len(candidates),
		"results", len(results),
	)
	return results, nil
}
