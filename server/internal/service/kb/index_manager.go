package kb

import (
	"bufio"
	"database/sql"
	"fmt"
	"math"
	"os"
	"sort"
	"sync"

	"private-buddy-server/internal/database"
	applogger "private-buddy-server/internal/logger"
	"private-buddy-server/internal/model"
	"private-buddy-server/internal/service/vectorstore"
	"private-buddy-server/pkg/hnsw"

	_ "github.com/glebarez/go-sqlite/compat"
)

// IndexType represents the current indexing mode for a knowledge base.
type IndexType string

const (
	IndexTypeFlat      IndexType = "flat"
	IndexTypeSwitching IndexType = "switching"
	IndexTypeHNSW      IndexType = "hnsw"
)

// PendingVector holds a vector awaiting insertion into the HNSW graph
// during a flat→HNSW switch.
type PendingVector struct {
	ChunkID   uint64
	Embedding []float32
}

// IndexManager manages the vector index for a single knowledge base.
// It supports both Flat (brute-force) and HNSW indexing modes,
// with automatic switching based on vector count threshold.
//
// The database connection is obtained from the database package directly.
type IndexManager struct {
	mu            sync.RWMutex
	graph         *hnsw.SavedGraph[uint64]
	indexType     IndexType
	indexPath     string
	vectorsDBPath string
	vectorsDB     *sql.DB
	kbID          int64
	threshold     int
	pendingAdds   []PendingVector
	loaded        bool
}

// NewIndexManager creates an IndexManager for a knowledge base.
func NewIndexManager(indexType, indexFilePath, vectorsDBPath string, kbID int64, threshold int) *IndexManager {
	return &IndexManager{
		indexType:     IndexType(indexType),
		indexPath:     indexFilePath,
		vectorsDBPath: vectorsDBPath,
		kbID:          kbID,
		threshold:     threshold,
	}
}

// Load initializes the index manager by opening the vectors database
// and loading the HNSW graph if index type is hnsw.
func (m *IndexManager) Load() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.loaded {
		return nil
	}

	var err error
	m.vectorsDB, err = sql.Open("sqlite3", m.vectorsDBPath)
	if err != nil {
		return fmt.Errorf("failed to open vectors db: %w", err)
	}

	if m.indexType == IndexTypeHNSW {
		if err := m.loadHNSWGraph(); err != nil {
			applogger.L.Warn("Failed to load HNSW graph, falling back to flat", "kb_id", m.kbID, "error", err)
			m.indexType = IndexTypeFlat
		}
	}

	m.loaded = true
	return nil
}

// Close releases all resources held by the index manager.
func (m *IndexManager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.vectorsDB != nil {
		m.vectorsDB.Close()
		m.vectorsDB = nil
	}
	m.graph = nil
	m.loaded = false
}

// GetVector retrieves the embedding for a chunk from the vectors database.
func (m *IndexManager) GetVector(chunkID int64) ([]float32, error) {
	var blob []byte
	err := m.vectorsDB.QueryRow("SELECT embedding FROM vectors WHERE chunk_id = ?", chunkID).Scan(&blob)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return vectorstore.BlobToFloat32Slice(blob), nil
}

// Add inserts a vector into the index. It writes to SQLite for persistence
// and adds to the HNSW graph if in HNSW mode. Triggers flat→HNSW switch
// when vector count exceeds threshold.
func (m *IndexManager) Add(chunkID uint64, embedding []float32) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.loaded {
		return fmt.Errorf("index manager not loaded")
	}

	blob := vectorstore.Float32SliceToBlob(embedding)
	_, err := m.vectorsDB.Exec(
		"INSERT OR REPLACE INTO vectors (chunk_id, embedding) VALUES (?, ?)",
		chunkID, blob,
	)
	if err != nil {
		return fmt.Errorf("failed to insert vector: %w", err)
	}

	m.addToMemoryIndex(chunkID, embedding)
	return nil
}

// AddToIndex adds a vector to the in-memory index only (skip SQLite write).
// Used when the vector has already been persisted by VectorStore.
func (m *IndexManager) AddToIndex(chunkID uint64, embedding []float32) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.loaded {
		return fmt.Errorf("index manager not loaded")
	}

	m.addToMemoryIndex(chunkID, embedding)
	return nil
}

// addToMemoryIndex updates the in-memory index state (HNSW graph or pending queue).
// Must be called with m.mu held.
func (m *IndexManager) addToMemoryIndex(chunkID uint64, embedding []float32) {
	switch m.indexType {
	case IndexTypeFlat:
		m.checkFlatToHNSW()
	case IndexTypeSwitching:
		m.pendingAdds = append(m.pendingAdds, PendingVector{ChunkID: chunkID, Embedding: embedding})
	case IndexTypeHNSW:
		if m.graph != nil {
			if err := safeAddToGraph(m.graph, chunkID, embedding); err != nil {
				applogger.L.Error("Failed to add node to HNSW graph",
					"kb_id", m.kbID, "chunk_id", chunkID, "error", err)
			}
		}
	}
}

// checkFlatToHNSW checks if vector count exceeds threshold and triggers HNSW switch.
func (m *IndexManager) checkFlatToHNSW() {
	var count int
	m.vectorsDB.QueryRow("SELECT COUNT(*) FROM vectors").Scan(&count)

	if count >= m.threshold {
		result := database.DB.Model(&model.KnowledgeBase{}).
			Where("id = ? AND index_type = ?", m.kbID, string(IndexTypeFlat)).
			Update("index_type", string(IndexTypeSwitching))
		if result.RowsAffected == 1 {
			m.indexType = IndexTypeSwitching
			go m.buildHNSWIndex()
		}
	}
}

// buildHNSWIndex builds the HNSW graph from all vectors in SQLite.
// Runs asynchronously. During build, new vectors go to pendingAdds queue.
// The graph is not assigned to m.graph until fully built (including pending
// vectors), because hnsw.Graph is not concurrency-safe.
func (m *IndexManager) buildHNSWIndex() {
	defer func() {
		if r := recover(); r != nil {
			applogger.L.Error("HNSW build panic", "kb_id", m.kbID, "panic", r)
			m.casIndexType(IndexTypeSwitching, IndexTypeFlat)
		}
	}()

	applogger.L.Info("Building HNSW index", "kb_id", m.kbID)

	if m.vectorsDB == nil {
		applogger.L.Error("HNSW build: vectorsDB not initialized", "kb_id", m.kbID)
		m.casIndexType(IndexTypeSwitching, IndexTypeFlat)
		return
	}

	entries, err := m.loadAllVectors()
	if err != nil {
		applogger.L.Error("Failed to load vectors for HNSW build", "kb_id", m.kbID, "error", err)
		m.casIndexType(IndexTypeSwitching, IndexTypeFlat)
		return
	}
	applogger.L.Info("HNSW build: vectors loaded", "kb_id", m.kbID, "count", len(entries))

	dimStats := m.analyzeEmbeddings(entries)
	applogger.L.Info("HNSW build: embedding stats",
		"kb_id", m.kbID, "expected_dim", m.threshold,
		"min_dim", dimStats.minDim, "max_dim", dimStats.maxDim,
		"zero_count", dimStats.zeroCount, "nan_count", dimStats.nanCount)

	if dimStats.nanCount > 0 {
		applogger.L.Error("HNSW build: found NaN/Inf in embeddings, aborting", "kb_id", m.kbID, "count", dimStats.nanCount)
		m.casIndexType(IndexTypeSwitching, IndexTypeFlat)
		return
	}
	if dimStats.minDim != dimStats.maxDim {
		applogger.L.Error("HNSW build: inconsistent embedding dimensions",
			"kb_id", m.kbID, "min_dim", dimStats.minDim, "max_dim", dimStats.maxDim)
		m.casIndexType(IndexTypeSwitching, IndexTypeFlat)
		return
	}

	sg, err := hnsw.LoadSavedGraph[uint64](m.indexPath)
	if err != nil || sg == nil {
		sg = &hnsw.SavedGraph[uint64]{
			Graph: hnsw.NewGraph[uint64](),
			Path:  m.indexPath,
		}
	}
	if sg.Graph == nil {
		sg.Graph = hnsw.NewGraph[uint64]()
	}

	addedChunkIDs := make(map[uint64]bool)

	for i, e := range entries {
		if !isValidEmbedding(e.Embedding) {
			applogger.L.Error("HNSW build: invalid embedding",
				"kb_id", m.kbID, "index", i, "total", len(entries),
				"chunk_id", e.ChunkID, "embedding_len", len(e.Embedding))
			m.casIndexType(IndexTypeSwitching, IndexTypeFlat)
			return
		}
		if err := safeAddToGraph(sg, uint64(e.ChunkID), e.Embedding); err != nil {
			sample := sampleEmbedding(e.Embedding)
			applogger.L.Error("HNSW build: failed to add node",
				"kb_id", m.kbID, "index", i, "total", len(entries),
				"chunk_id", e.ChunkID, "embedding_len", len(e.Embedding),
				"sample", sample, "error", err)
			m.casIndexType(IndexTypeSwitching, IndexTypeFlat)
			return
		}
		addedChunkIDs[uint64(e.ChunkID)] = true
		if (i+1)%100 == 0 {
			applogger.L.Info("HNSW build: progress", "kb_id", m.kbID, "added", i+1, "total", len(entries))
		}
	}

	m.mu.Lock()
	pending := m.pendingAdds
	m.pendingAdds = nil
	m.mu.Unlock()

	applogger.L.Info("HNSW build: processing pending vectors", "kb_id", m.kbID, "pending_count", len(pending))

	addedFromPending := 0
	for i, pv := range pending {
		if addedChunkIDs[pv.ChunkID] {
			continue
		}
		if !isValidEmbedding(pv.Embedding) {
			applogger.L.Error("HNSW build: invalid pending embedding",
				"kb_id", m.kbID, "index", i, "total_pending", len(pending),
				"chunk_id", pv.ChunkID, "embedding_len", len(pv.Embedding))
			m.casIndexType(IndexTypeSwitching, IndexTypeFlat)
			return
		}
		if err := safeAddToGraph(sg, pv.ChunkID, pv.Embedding); err != nil {
			sample := sampleEmbedding(pv.Embedding)
			applogger.L.Error("HNSW build: failed to add pending node",
				"kb_id", m.kbID, "index", i, "total_pending", len(pending),
				"chunk_id", pv.ChunkID, "embedding_len", len(pv.Embedding),
				"sample", sample, "error", err)
			m.casIndexType(IndexTypeSwitching, IndexTypeFlat)
			return
		}
		addedChunkIDs[pv.ChunkID] = true
		addedFromPending++
	}
	applogger.L.Info("HNSW build: graph built, saving", "kb_id", m.kbID,
		"entries", len(entries), "pending_total", len(pending), "pending_added", addedFromPending)

	if err := saveGraph(sg); err != nil {
		applogger.L.Error("Failed to save HNSW graph", "kb_id", m.kbID, "error", err)
		m.casIndexType(IndexTypeSwitching, IndexTypeFlat)
		return
	}

	m.mu.Lock()
	m.graph = sg
	m.indexType = IndexTypeHNSW
	m.mu.Unlock()

	database.DB.Model(&model.KnowledgeBase{}).
		Where("id = ? AND index_type = ?", m.kbID, string(IndexTypeSwitching)).
		Update("index_type", string(IndexTypeHNSW))

	applogger.L.Info("HNSW index built successfully", "kb_id", m.kbID,
		"total_vectors", len(addedChunkIDs))
}

// Search performs a vector similarity search.
func (m *IndexManager) Search(query []float32, topK int) ([]SearchCandidate, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if !m.loaded {
		return nil, fmt.Errorf("index manager not loaded")
	}

	switch m.indexType {
	case IndexTypeHNSW:
		return m.searchHNSW(query, topK)
	case IndexTypeFlat, IndexTypeSwitching:
		return m.searchFlat(query, topK)
	default:
		return nil, fmt.Errorf("unknown index type: %s", m.indexType)
	}
}

func (m *IndexManager) searchHNSW(query []float32, topK int) ([]SearchCandidate, error) {
	if m.graph == nil {
		return m.searchFlat(query, topK)
	}

	neighbors := m.graph.Search(query, topK)
	if len(neighbors) == 0 {
		return nil, nil
	}

	results := make([]SearchCandidate, 0, len(neighbors))
	for _, n := range neighbors {
		var blob []byte
		err := m.vectorsDB.QueryRow(
			"SELECT embedding FROM vectors WHERE chunk_id = ?", n.Key,
		).Scan(&blob)
		if err != nil {
			continue
		}
		vec := vectorstore.BlobToFloat32Slice(blob)
		results = append(results, SearchCandidate{
			ChunkID: n.Key,
			Score:   cosineSimilarity(query, vec),
		})
	}

	return results, nil
}

func (m *IndexManager) searchFlat(query []float32, topK int) ([]SearchCandidate, error) {
	entries, err := m.loadAllVectors()
	if err != nil {
		return nil, err
	}

	type scored struct {
		chunkID uint64
		score   float64
	}

	scores := make([]scored, 0, len(entries))
	for _, e := range entries {
		sim := cosineSimilarity(query, e.Embedding)
		scores = append(scores, scored{chunkID: uint64(e.ChunkID), score: sim})
	}

	sort.Slice(scores, func(i, j int) bool {
		return scores[i].score > scores[j].score
	})

	if topK > len(scores) {
		topK = len(scores)
	}

	results := make([]SearchCandidate, topK)
	for i := 0; i < topK; i++ {
		results[i] = SearchCandidate{
			ChunkID: scores[i].chunkID,
			Score:   scores[i].score,
		}
	}
	return results, nil
}

func (m *IndexManager) loadAllVectors() ([]VectorEntry, error) {
	rows, err := m.vectorsDB.Query("SELECT chunk_id, embedding FROM vectors")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []VectorEntry
	for rows.Next() {
		var chunkID int64
		var blob []byte
		if err := rows.Scan(&chunkID, &blob); err != nil {
			return nil, err
		}
		entries = append(entries, VectorEntry{
			ChunkID:   chunkID,
			Embedding: vectorstore.BlobToFloat32Slice(blob),
		})
	}
	return entries, rows.Err()
}

func (m *IndexManager) loadHNSWGraph() error {
	sg, err := hnsw.LoadSavedGraph[uint64](m.indexPath)
	if err != nil {
		return err
	}
	m.graph = sg
	return nil
}

func (m *IndexManager) casIndexType(from, to IndexType) bool {
	result := database.DB.Model(&model.KnowledgeBase{}).
		Where("id = ? AND index_type = ?", m.kbID, string(from)).
		Update("index_type", string(to))
	if result.RowsAffected == 1 {
		m.mu.Lock()
		m.indexType = to
		m.mu.Unlock()
		return true
	}
	return false
}

// isValidEmbedding checks if an embedding vector is valid for HNSW insertion.
// Returns false if the embedding is nil, empty, or contains NaN/Inf values.
func isValidEmbedding(emb []float32) bool {
	if len(emb) == 0 {
		return false
	}
	for _, v := range emb {
		if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
			return false
		}
	}
	return true
}

// embeddingStats holds statistics about a batch of embeddings.
type embeddingStats struct {
	minDim    int
	maxDim    int
	zeroCount int
	nanCount  int
}

// analyzeEmbeddings analyzes a batch of embeddings for diagnostic purposes.
func (m *IndexManager) analyzeEmbeddings(entries []VectorEntry) embeddingStats {
	stats := embeddingStats{
		minDim:    -1,
		maxDim:    0,
		zeroCount: 0,
		nanCount:  0,
	}
	for _, e := range entries {
		dim := len(e.Embedding)
		if stats.minDim < 0 || dim < stats.minDim {
			stats.minDim = dim
		}
		if dim > stats.maxDim {
			stats.maxDim = dim
		}

		isZero := true
		for _, v := range e.Embedding {
			if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
				stats.nanCount++
				break
			}
			if v != 0 {
				isZero = false
			}
		}
		if isZero && dim > 0 {
			stats.zeroCount++
		}
	}
	return stats
}

// sampleEmbedding returns a sample of the embedding for logging (first 5 values).
func sampleEmbedding(emb []float32) []float32 {
	if len(emb) <= 5 {
		return emb
	}
	return emb[:5]
}

// safeAddToGraph safely adds a node to the HNSW graph, recovering from panics.
func safeAddToGraph(sg *hnsw.SavedGraph[uint64], chunkID uint64, embedding []float32) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic adding chunk %d: %v", chunkID, r)
		}
	}()
	sg.Add(hnsw.MakeNode(chunkID, embedding))
	return nil
}

// SearchCandidate represents a candidate result from index search.
type SearchCandidate struct {
	ChunkID uint64
	Score   float64
}

// cosineSimilarity computes cosine similarity between two float32 vectors.
func cosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}

// saveGraph persists the HNSW graph to disk atomically.
// Writes to a temp file first, then renames to the target path.
// os.Rename replaces the target atomically on POSIX and has worked
// on Windows since Go 1.15.
func saveGraph(sg *hnsw.SavedGraph[uint64]) error {
	tmpPath := sg.Path + ".tmp"
	f, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}

	wr := bufio.NewWriter(f)
	if err := sg.Export(wr); err != nil {
		f.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("export: %w", err)
	}
	if err := wr.Flush(); err != nil {
		f.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("flush: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("close: %w", err)
	}
	if err := os.Rename(tmpPath, sg.Path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}
