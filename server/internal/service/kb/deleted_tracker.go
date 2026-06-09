package kb

import (
	"sync"
)

// deletedVectorTracker tracks soft-deleted chunk IDs to exclude them from search results.
// It maintains an in-memory set of deleted chunk IDs for fast filtering during search.
type deletedVectorTracker struct {
	mu      sync.RWMutex
	deleted map[uint64]bool
}

// newDeletedVectorTracker creates a tracker with an empty deleted set.
func newDeletedVectorTracker() *deletedVectorTracker {
	return &deletedVectorTracker{
		deleted: make(map[uint64]bool),
	}
}

// MarkDeleted adds chunk IDs to the deleted set.
func (t *deletedVectorTracker) MarkDeleted(chunkIDs ...uint64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, id := range chunkIDs {
		t.deleted[id] = true
	}
}

// IsDeleted checks if a chunk ID is in the deleted set.
func (t *deletedVectorTracker) IsDeleted(chunkID uint64) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.deleted[chunkID]
}

// FilterCandidates removes deleted chunks from search candidates.
func (t *deletedVectorTracker) FilterCandidates(candidates []searchCandidate) []searchCandidate {
	t.mu.RLock()
	defer t.mu.RUnlock()

	result := make([]searchCandidate, 0, len(candidates))
	for _, c := range candidates {
		if !t.deleted[c.ChunkID] {
			result = append(result, c)
		}
	}
	return result
}

// LoadDeletedChunkIDs loads deleted chunk IDs into the tracker.
func (t *deletedVectorTracker) LoadDeletedChunkIDs(chunkIDs []int64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, id := range chunkIDs {
		t.deleted[uint64(id)] = true
	}
}

// Count returns the number of tracked deleted chunks.
func (t *deletedVectorTracker) Count() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.deleted)
}
