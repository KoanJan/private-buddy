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

// PublicSearchResult wraps a PublicExperience with its cosine similarity score.
type PublicSearchResult struct {
	Experience model.PublicExperience
	Score      float64 // Cosine similarity, 0..1
}

// createPublicExperience inserts a public experience and computes its embedding
// from the description. Called internally by IngestSkill.
func createPublicExperience(ctx context.Context, title, description, whenToUse,
	guidelines, pitfalls, procedure string, sourceID int64, sourceFingerprint string) (*model.PublicExperience, error) {

	emb, err := embeddingSvc.EmbedSingle(ctx, description)
	if err != nil {
		return nil, fmt.Errorf("embed public experience description: %w", err)
	}

	exp := &model.PublicExperience{
		Title:             title,
		Description:       description,
		WhenToUse:         whenToUse,
		Guidelines:        guidelines,
		Pitfalls:          pitfalls,
		Procedure:         procedure,
		SourceType:        model.PublicExperienceSourceIngestion,
		SourceID:          sourceID,
		SourceFingerprint: sourceFingerprint,
	}

	if err := database.DB.Create(exp).Error; err != nil {
		return nil, fmt.Errorf("insert public_experience: %w", err)
	}

	vec := &model.PublicExperienceVector{
		ExperienceID: exp.ID,
		Embedding:    vectorstore.Float32SliceToBlob(emb),
	}
	if err := database.DB.Create(vec).Error; err != nil {
		applogger.Error("Failed to insert public_experience_vector, orphaned experience row",
			"experience_id", exp.ID,
			"error", err,
		)
		return nil, fmt.Errorf("insert public_experience_vector: %w", err)
	}

	applogger.Info("PublicExperience created",
		"id", exp.ID,
	)
	return exp, nil
}

// SearchPublicExperiences performs semantic retrieval against public experiences.
// query is an arbitrary search text (entity profile narratives, domain keywords, etc.).
// Returns results sorted by descending cosine similarity, filtered by minScore.
// Returns nil, nil when the embedding service is not configured.
func SearchPublicExperiences(ctx context.Context, query string, topN int, minScore float64) ([]PublicSearchResult, error) {
	if embeddingSvc == nil {
		return nil, nil
	}
	panicIfNotReady()

	queryVec, err := embeddingSvc.EmbedSingle(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("embed search query: %w", err)
	}
	if len(queryVec) == 0 {
		applogger.Warn("empty queryVec")
		return nil, nil
	}

	type expWithVec struct {
		exp model.PublicExperience
		vec []float32
	}
	var candidates []expWithVec

	var allVectors []model.PublicExperienceVector
	if err := database.DB.Find(&allVectors).Error; err != nil {
		return nil, fmt.Errorf("load public experience vectors: %w", err)
	}

	for _, v := range allVectors {
		var exp model.PublicExperience
		if err := database.DB.Where("id = ?", v.ExperienceID).First(&exp).Error; err != nil {
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
		result PublicSearchResult
		score  float64
	}
	var entries []scoreEntry

	for _, c := range candidates {
		sim := vectorstore.CosineSimilarity(queryVec, c.vec)
		if sim >= minScore {
			entries = append(entries, scoreEntry{
				result: PublicSearchResult{Experience: c.exp, Score: sim},
				score:  sim,
			})
		}
	}

	sort.Slice(entries, func(i, j int) bool { return entries[i].score > entries[j].score })

	if len(entries) > topN {
		entries = entries[:topN]
	}

	results := make([]PublicSearchResult, len(entries))
	for i, s := range entries {
		results[i] = s.result
	}

	applogger.Info("Public experience search completed",
		"candidates", len(candidates),
		"results", len(results),
	)
	return results, nil
}
