package vectorstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"private-buddy-server/internal/config"
	"private-buddy-server/internal/service/llm"

	applogger "private-buddy-server/internal/logger"

	_ "github.com/glebarez/go-sqlite/compat"
)

type VectorStoreService struct {
	embeddingSvc *llm.EmbeddingService
	db           *sql.DB
}

func NewVectorStoreService(embeddingSvc *llm.EmbeddingService) *VectorStoreService {
	return &VectorStoreService{
		embeddingSvc: embeddingSvc,
	}
}

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
	applogger.L.Info("Vector store initialized", "path", settings.VectorDBFile())
	return nil
}

func (vss *VectorStoreService) Close() error {
	if vss.db != nil {
		return vss.db.Close()
	}
	return nil
}

func (vss *VectorStoreService) ensureSessionTable(sessionID int64) error {
	tableName := fmt.Sprintf("session_vec_%d", sessionID)

	var tableNameCheck string
	err := vss.db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name=?", tableName).Scan(&tableNameCheck)
	if err == nil {
		return nil
	}

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

type VectorMetadata struct {
	MessageID int64  `json:"message_id"`
	Role      string `json:"role"`
	Content   string `json:"content"`
}

func (vss *VectorStoreService) AddMessages(sessionID int64, messageIDs []int64, contents []string, metadatas []VectorMetadata) error {
	if vss.embeddingSvc == nil {
		applogger.L.Warn("Embedding service not available, skipping vector store add")
		return nil
	}

	if err := vss.ensureSessionTable(sessionID); err != nil {
		return fmt.Errorf("failed to ensure session table: %w", err)
	}

	embeddings, err := vss.embeddingSvc.Embed(context.Background(), contents)
	if err != nil {
		return fmt.Errorf("failed to generate embeddings: %w", err)
	}

	tableName := fmt.Sprintf("session_vec_%d", sessionID)
	for i, embedding := range embeddings {
		blob := float32SliceToBlob(embedding)
		meta := metadatas[i]
		_, err := vss.db.Exec(
			fmt.Sprintf("INSERT INTO %s (message_id, role, content, embedding) VALUES (?, ?, ?, ?)", tableName),
			meta.MessageID, meta.Role, meta.Content, blob,
		)
		if err != nil {
			applogger.L.Error("Failed to insert vector", "error", err)
		}
	}

	applogger.L.Info("Added messages to vector store",
		"session_id", sessionID,
		"count", len(contents),
	)
	return nil
}

type SearchResult struct {
	MessageID int64   `json:"message_id"`
	Role      string  `json:"role"`
	Content   string  `json:"content"`
	Score     float64 `json:"score"`
}

func (vss *VectorStoreService) Search(sessionID int64, query string, k int) ([]SearchResult, error) {
	if vss.embeddingSvc == nil {
		return nil, fmt.Errorf("embedding service not available")
	}

	if err := vss.ensureSessionTable(sessionID); err != nil {
		return nil, fmt.Errorf("failed to ensure session table: %w", err)
	}

	queryEmbedding, err := vss.embeddingSvc.EmbedSingle(context.Background(), query)
	if err != nil {
		return nil, fmt.Errorf("failed to generate query embedding: %w", err)
	}

	tableName := fmt.Sprintf("session_vec_%d", sessionID)
	rows, err := vss.db.Query(fmt.Sprintf("SELECT message_id, role, content, embedding FROM %s", tableName))
	if err != nil {
		return nil, fmt.Errorf("failed to query vectors: %w", err)
	}
	defer rows.Close()

	type candidate struct {
		MessageID int64
		Role      string
		Content   string
		Score     float64
	}

	var candidates []candidate
	for rows.Next() {
		var msgID int64
		var role, content string
		var blob []byte
		if err := rows.Scan(&msgID, &role, &content, &blob); err != nil {
			continue
		}

		storedEmbedding := blobToFloat32Slice(blob)
		score := cosineSimilarity(queryEmbedding, storedEmbedding)
		candidates = append(candidates, candidate{
			MessageID: msgID,
			Role:      role,
			Content:   content,
			Score:     score,
		})
	}

	for i := 0; i < len(candidates); i++ {
		for j := i + 1; j < len(candidates); j++ {
			if candidates[j].Score > candidates[i].Score {
				candidates[i], candidates[j] = candidates[j], candidates[i]
			}
		}
	}

	if k > len(candidates) {
		k = len(candidates)
	}

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

func (vss *VectorStoreService) DeleteSession(sessionID int64) error {
	tableName := fmt.Sprintf("session_vec_%d", sessionID)
	_, err := vss.db.Exec(fmt.Sprintf("DROP TABLE IF EXISTS %s", tableName))
	return err
}

func cosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) {
		return 0
	}
	var dotProduct, normA, normB float64
	for i := range a {
		dotProduct += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dotProduct / (math.Sqrt(normA) * math.Sqrt(normB))
}

func float32SliceToBlob(slice []float32) []byte {
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

func blobToFloat32Slice(blob []byte) []float32 {
	if len(blob)%4 != 0 {
		return nil
	}
	slice := make([]float32, len(blob)/4)
	for i := range slice {
		bits := uint32(blob[i*4]) | uint32(blob[i*4+1])<<8 | uint32(blob[i*4+2])<<16 | uint32(blob[i*4+3])<<24
		slice[i] = math.Float32frombits(bits)
	}
	return slice
}

func init() {
	_ = json.Marshal
	_ = strconv.Atoi
	_ = strings.TrimSpace
}
