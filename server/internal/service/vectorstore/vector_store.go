// Package vectorstore provides a lightweight vector storage service for semantic search.
// It uses SQLite as the backend to store vector embeddings and supports cosine similarity
// based similarity search for retrieval-augmented generation (RAG) use cases.
package vectorstore

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"os"
	"path/filepath"

	"private-buddy-server/internal/config"
	"private-buddy-server/internal/service/llm"

	applogger "private-buddy-server/internal/logger"

	_ "github.com/glebarez/go-sqlite/compat"
	"github.com/viterin/vek/vek32"
)

// VectorStoreService manages vector storage operations for chat sessions.
// Each session has its own table in SQLite to store message embeddings,
// enabling semantic search across conversation history.
type VectorStoreService struct {
	// embeddingSvc generates vector embeddings from text content
	embeddingSvc *llm.EmbeddingService
	// db is the SQLite connection for persistent vector storage
	db *sql.DB
}

// NewVectorStoreService creates a new VectorStoreService instance.
// The embeddingSvc parameter can be nil if embedding functionality is not needed.
func NewVectorStoreService(embeddingSvc *llm.EmbeddingService) *VectorStoreService {
	return &VectorStoreService{
		embeddingSvc: embeddingSvc,
	}
}

// Init initializes the vector store by creating the database directory
// and opening a SQLite connection. This must be called before any other operations.
func (vss *VectorStoreService) Init() error {
	settings := config.Get()
	dbDir := filepath.Dir(settings.VectorDBFile())
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		return fmt.Errorf("failed to create vector db directory: %w", err)
	}

	db, err := sql.Open("sqlite3", settings.VectorDBFile())
	if err != nil {
		return fmt.Errorf("failed to open vector db: %w", err)
	}

	vss.db = db
	applogger.Info("Vector store initialized", "path", settings.VectorDBFile())
	return nil
}

// Close closes the SQLite database connection.
// This should be called when the service is no longer needed.
func (vss *VectorStoreService) Close() error {
	if vss.db != nil {
		return vss.db.Close()
	}
	return nil
}

// ensureSessionTable creates the vector table for a session if it doesn't exist.
// Each session uses a separate table named "session_vec_{sessionID}" to isolate
// vector data between different conversations.
func (vss *VectorStoreService) ensureSessionTable(sessionID int64) error {
	tableName := fmt.Sprintf("session_vec_%d", sessionID)

	// Check if table already exists to avoid unnecessary CREATE operations
	var tableNameCheck string
	err := vss.db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name=?", tableName).Scan(&tableNameCheck)
	if err == nil {
		return nil
	}

	// Create table with message metadata and vector embedding
	// embedding is stored as BLOB (binary representation of float32 array)
	createSQL := fmt.Sprintf(`
		CREATE TABLE %s (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			message_id INTEGER NOT NULL,
			role TEXT NOT NULL,
			content TEXT NOT NULL,
			embedding BLOB NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)
	`, tableName)
	_, err = vss.db.Exec(createSQL)
	return err
}

// VectorMetadata contains the metadata associated with a vector entry.
// This information is stored alongside the embedding for retrieval.
type VectorMetadata struct {
	// MessageID is the unique identifier of the original message
	MessageID int64 `json:"message_id"`
	// Role indicates whether the message is from "user" or "assistant"
	Role string `json:"role"`
	// Content is the original text content of the message
	Content string `json:"content"`
}

// AddMessages generates embeddings for the given contents and stores them
// in the vector database. Each message is associated with its metadata
// for later retrieval during search operations.
//
// Parameters:
//   - sessionID: the session identifier to group related messages
//   - messageIDs: the unique identifiers of the messages (currently unused but kept for future use)
//   - contents: the text contents to be embedded
//   - metadatas: the metadata for each message
func (vss *VectorStoreService) AddMessages(ctx context.Context, sessionID int64, messageIDs []int64, contents []string, metadatas []VectorMetadata) error {
	// Skip if embedding service is not configured
	if vss.embeddingSvc == nil {
		applogger.Error("Embedding service not available, skipping vector store add")
		return nil
	}

	if err := vss.ensureSessionTable(sessionID); err != nil {
		return fmt.Errorf("failed to ensure session table: %w", err)
	}

	// Generate embeddings for all contents in a single batch request
	embeddings, err := vss.embeddingSvc.Embed(ctx, contents)
	if err != nil {
		return fmt.Errorf("failed to generate embeddings: %w", err)
	}

	// Insert each embedding with its metadata into the database
	tableName := fmt.Sprintf("session_vec_%d", sessionID)
	for i, embedding := range embeddings {
		blob := Float32SliceToBlob(embedding)
		meta := metadatas[i]
		_, err := vss.db.Exec(
			fmt.Sprintf("INSERT INTO %s (message_id, role, content, embedding) VALUES (?, ?, ?, ?)", tableName),
			meta.MessageID, meta.Role, meta.Content, blob,
		)
		if err != nil {
			applogger.Error("Failed to insert vector", "error", err)
		}
	}

	applogger.Info("Added messages to vector store",
		"session_id", sessionID,
		"count", len(contents),
	)
	return nil
}

// SearchResult represents a single search result from the vector store.
type SearchResult struct {
	// MessageID is the identifier of the matching message
	MessageID int64 `json:"message_id"`
	// Role indicates whether the message is from "user" or "assistant"
	Role string `json:"role"`
	// Content is the original text content of the message
	Content string `json:"content"`
	// Score is the cosine similarity score (higher means more similar)
	Score float64 `json:"score"`
}

// Search performs a semantic search across all messages in a session.
// It finds the k most similar messages to the query based on cosine similarity
// of their vector embeddings.
//
// Parameters:
//   - sessionID: the session to search within
//   - query: the search query text
//   - k: the maximum number of results to return
//
// Returns the top-k most similar messages sorted by descending similarity score.
func (vss *VectorStoreService) Search(ctx context.Context, sessionID int64, query string, k int) ([]SearchResult, error) {
	if vss.embeddingSvc == nil {
		return nil, fmt.Errorf("embedding service not available")
	}

	if err := vss.ensureSessionTable(sessionID); err != nil {
		return nil, fmt.Errorf("failed to ensure session table: %w", err)
	}

	// Generate embedding for the search query
	queryEmbedding, err := vss.embeddingSvc.EmbedSingle(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to generate query embedding: %w", err)
	}

	// Retrieve all vectors from the session table
	// Note: This loads all vectors into memory for similarity computation.
	// For large datasets, consider using a dedicated vector database with indexing.
	tableName := fmt.Sprintf("session_vec_%d", sessionID)
	rows, err := vss.db.Query(fmt.Sprintf("SELECT message_id, role, content, embedding FROM %s", tableName))
	if err != nil {
		return nil, fmt.Errorf("failed to query vectors: %w", err)
	}
	defer rows.Close()

	// Internal struct to hold candidates during sorting
	type candidate struct {
		MessageID int64
		Role      string
		Content   string
		Score     float64
	}

	// Compute similarity scores for all stored vectors
	var candidates []candidate
	for rows.Next() {
		var msgID int64
		var role, content string
		var blob []byte
		if err := rows.Scan(&msgID, &role, &content, &blob); err != nil {
			applogger.Error("failed to scan message vector row during search", "error", err)
			continue
		}

		storedEmbedding := BlobToFloat32Slice(blob)
		score := CosineSimilarity(queryEmbedding, storedEmbedding)
		candidates = append(candidates, candidate{
			MessageID: msgID,
			Role:      role,
			Content:   content,
			Score:     score,
		})
	}

	// Sort candidates by score in descending order using simple bubble sort
	// Note: For better performance with large result sets, use sort.Slice or a heap
	for i := 0; i < len(candidates); i++ {
		for j := i + 1; j < len(candidates); j++ {
			if candidates[j].Score > candidates[i].Score {
				candidates[i], candidates[j] = candidates[j], candidates[i]
			}
		}
	}

	// Limit results to k
	if k > len(candidates) {
		k = len(candidates)
	}

	// Convert top candidates to SearchResult
	results := make([]SearchResult, 0, k)
	for i := 0; i < k; i++ {
		results = append(results, SearchResult{
			MessageID: candidates[i].MessageID,
			Role:      candidates[i].Role,
			Content:   candidates[i].Content,
			Score:     candidates[i].Score,
		})
	}

	return results, nil
}

// DeleteSession removes all vector data for a session by dropping its table.
// This should be called when a session is deleted to free up storage space.
func (vss *VectorStoreService) DeleteSession(sessionID int64) error {
	tableName := fmt.Sprintf("session_vec_%d", sessionID)
	_, err := vss.db.Exec(fmt.Sprintf("DROP TABLE IF EXISTS %s", tableName))
	return err
}

// CosineSimilarity computes cosine similarity between two float32 vectors.
func CosineSimilarity(a, b []float32) float64 {
	return float64(vek32.CosineSimilarity(a, b))
}

// Float32SliceToBlob converts a float32 slice to a binary blob for SQLite storage.
// Each float32 is encoded as 4 bytes using IEEE 754 binary representation
// in little-endian byte order.
//
// This encoding is efficient (no serialization overhead) and portable
// (IEEE 754 is a universal standard for floating-point numbers).
func Float32SliceToBlob(slice []float32) []byte {
	buf := make([]byte, len(slice)*4)

	for i, v := range slice {
		bits := math.Float32bits(v)
		buf[i*4] = byte(bits)
		buf[i*4+1] = byte(bits >> 8)
		buf[i*4+2] = byte(bits >> 16)
		buf[i*4+3] = byte(bits >> 24)
	}

	return buf
}

// BlobToFloat32Slice converts a binary blob back to a float32 slice.
// This is the inverse operation of Float32SliceToBlob.
// Returns nil if the blob length is not a multiple of 4.
func BlobToFloat32Slice(blob []byte) []float32 {
	if len(blob)%4 != 0 {
		return nil
	}

	slice := make([]float32, len(blob)/4)

	for i := range slice {
		bits := uint32(blob[i*4]) |
			uint32(blob[i*4+1])<<8 |
			uint32(blob[i*4+2])<<16 |
			uint32(blob[i*4+3])<<24
		slice[i] = math.Float32frombits(bits)
	}

	return slice
}
