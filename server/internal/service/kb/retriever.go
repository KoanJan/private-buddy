package kb

import (
	"context"
	"fmt"
	"sync"

	"private-buddy-server/internal/database"
	"private-buddy-server/internal/model"
	"private-buddy-server/internal/schema"
	"private-buddy-server/internal/service/llm"
)

// getEmbeddingService creates an EmbeddingService for the given knowledge base.
func getEmbeddingService(kbID int64) (*llm.EmbeddingService, error) {
	var kb model.KnowledgeBase
	if err := database.DB.First(&kb, kbID).Error; err != nil {
		return nil, fmt.Errorf("knowledge base not found: %w", err)
	}

	var embConfig model.EmbeddingConfig
	if err := database.DB.First(&embConfig, kb.EmbeddingConfigID).Error; err != nil {
		return nil, fmt.Errorf("embedding config not found: %w", err)
	}

	return llm.NewEmbeddingService(embConfig.BaseURL, embConfig.APIKey, embConfig.ModelID, embeddingDim), nil
}

// searchKB searches a single knowledge base for relevant chunks.
func searchKB(ctx context.Context, kbID int64, query string, topK int) ([]schema.SearchResult, error) {
	if topK <= 0 {
		topK = 5
	}

	embService, err := getEmbeddingService(kbID)
	if err != nil {
		return nil, err
	}

	queryVec, err := embService.EmbedSingle(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to embed query: %w", err)
	}

	mgr, err := getOrCreateIndexManager(kbID)
	if err != nil {
		return nil, fmt.Errorf("failed to get index manager: %w", err)
	}

	candidates, err := mgr.Search(queryVec, topK)
	if err != nil {
		return nil, fmt.Errorf("search failed: %w", err)
	}

	var deletedChunkIDs []int64
	database.DB.Model(&model.DocumentChunk{}).
		Where("knowledge_base_id = ? AND deleted = 1", kbID).
		Pluck("id", &deletedChunkIDs)

	tracker := newDeletedVectorTracker()
	tracker.LoadDeletedChunkIDs(deletedChunkIDs)
	candidates = tracker.FilterCandidates(candidates)

	return candidatesToResults(candidates, kbID), nil
}

// searchMultiKB searches multiple knowledge bases concurrently.
// Generates one query embedding per KB (since KBs may use different models),
// then searches each KB in parallel.
func searchMultiKB(ctx context.Context, kbIDs []int64, query string, topK int) ([]schema.SearchResult, error) {
	if topK <= 0 {
		topK = 5
	}

	type kbResult struct {
		results []schema.SearchResult
		err     error
		kbID    int64
	}

	ch := make(chan kbResult, len(kbIDs))
	var wg sync.WaitGroup

	for _, kbID := range kbIDs {
		wg.Add(1)
		go func(id int64) {
			defer wg.Done()

			embService, err := getEmbeddingService(id)
			if err != nil {
				ch <- kbResult{err: err, kbID: id}
				return
			}

			queryVec, err := embService.EmbedSingle(ctx, query)
			if err != nil {
				ch <- kbResult{err: err, kbID: id}
				return
			}

			mgr, err := getOrCreateIndexManager(id)
			if err != nil {
				ch <- kbResult{err: err, kbID: id}
				return
			}

			candidates, err := mgr.Search(queryVec, topK)
			if err != nil {
				ch <- kbResult{err: err, kbID: id}
				return
			}

			var deletedChunkIDs []int64
			database.DB.Model(&model.DocumentChunk{}).
				Where("knowledge_base_id = ? AND deleted = 1", id).
				Pluck("id", &deletedChunkIDs)

			tracker := newDeletedVectorTracker()
			tracker.LoadDeletedChunkIDs(deletedChunkIDs)
			candidates = tracker.FilterCandidates(candidates)

			ch <- kbResult{results: candidatesToResults(candidates, id), kbID: id}
		}(kbID)
	}

	go func() {
		wg.Wait()
		close(ch)
	}()

	allResults := make([]schema.SearchResult, 0)
	for res := range ch {
		if res.err != nil {
			continue
		}
		allResults = append(allResults, res.results...)
	}

	return allResults, nil
}

func candidatesToResults(candidates []searchCandidate, kbID int64) []schema.SearchResult {
	if len(candidates) == 0 {
		return make([]schema.SearchResult, 0)
	}

	results := make([]schema.SearchResult, 0, len(candidates))
	for _, c := range candidates {
		var chunk model.DocumentChunk
		if err := database.DB.First(&chunk, int64(c.ChunkID)).Error; err != nil {
			continue
		}
		if chunk.Deleted == 1 {
			continue
		}

		var doc model.Document
		if err := database.DB.Select("id, title").First(&doc, chunk.DocumentID).Error; err != nil {
			continue
		}

		results = append(results, schema.SearchResult{
			ChunkID:         int64(c.ChunkID),
			DocumentID:      chunk.DocumentID,
			DocumentTitle:   doc.Title,
			Content:         chunk.Content,
			Score:           c.Score,
			KnowledgeBaseID: kbID,
		})
	}

	return results
}
