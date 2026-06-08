package kb

import (
	"context"
	"fmt"
	"path/filepath"

	"private-buddy-server/internal/config"
	"private-buddy-server/internal/database"
	applogger "private-buddy-server/internal/logger"
	"private-buddy-server/internal/model"
	"private-buddy-server/internal/service/llm"

	"gorm.io/gorm"
)

const (
	defaultChunkSize    = 500
	defaultChunkOverlap = 50
	defaultMinChunkSize = 100
	embedBatchSize      = 10
)

// DocumentProcessor handles the document processing pipeline:
// extract text → split into chunks → generate embeddings → store vectors.
//
// The database connection is obtained from the database package directly.
type DocumentProcessor struct {
	splitter   *TextSplitter
	embService *llm.EmbeddingService
}

// NewDocumentProcessor creates a DocumentProcessor with the given embedding service.
func NewDocumentProcessor(embService *llm.EmbeddingService) *DocumentProcessor {
	return &DocumentProcessor{
		splitter:   NewTextSplitter(defaultChunkSize, defaultChunkOverlap, defaultMinChunkSize),
		embService: embService,
	}
}

// Process executes the full document processing pipeline.
// Steps: extract → split → embed → store chunks → store vectors → update index.
func (dp *DocumentProcessor) Process(ctx context.Context, kbID int64, doc *model.Document) error {
	dp.updateStatus(doc.ID, model.DocumentStatusProcessing, "")

	text, err := Extract(doc.FilePath)
	if err != nil {
		dp.cleanupChunks(doc.ID)
		dp.updateStatus(doc.ID, model.DocumentStatusFailed, fmt.Sprintf("Extraction failed: %v", err))
		return err
	}

	chunks := dp.splitter.Split(text)
	if len(chunks) == 0 {
		dp.updateStatus(doc.ID, model.DocumentStatusFailed, "No text content extracted")
		return fmt.Errorf("no text content extracted from document %d", doc.ID)
	}

	chunkModels := dp.storeChunks(kbID, doc.ID, chunks)

	embeddings, err := dp.generateEmbeddings(ctx, chunkModels)
	if err != nil {
		dp.cleanupChunks(doc.ID)
		dp.updateStatus(doc.ID, model.DocumentStatusFailed, fmt.Sprintf("Embedding failed: %v", err))
		return err
	}

	vectorsDBPath := filepath.Join(config.Get().GetKBDir(), fmt.Sprintf("%d", kbID), "vectors.db")
	vs, err := NewVectorStore(vectorsDBPath)
	if err != nil {
		dp.cleanupChunks(doc.ID)
		dp.updateStatus(doc.ID, model.DocumentStatusFailed, fmt.Sprintf("Vector store error: %v", err))
		return err
	}
	defer vs.Close()

	entries := make([]VectorEntry, len(chunkModels))
	for i, cm := range chunkModels {
		entries[i] = VectorEntry{
			ChunkID:   cm.ID,
			Embedding: embeddings[i],
		}
	}
	if err := vs.InsertBatch(entries); err != nil {
		dp.cleanupChunks(doc.ID)
		dp.updateStatus(doc.ID, model.DocumentStatusFailed, fmt.Sprintf("Vector insert failed: %v", err))
		return err
	}

	for i := range chunkModels {
		database.DB.Model(&model.DocumentChunk{}).Where("id = ?", chunkModels[i].ID).
			Update("vector_id", 1)
	}

	database.DB.Model(&model.KnowledgeBase{}).Where("id = ?", kbID).
		Update("vector_count", gorm.Expr("vector_count + ?", len(chunkModels)))

	dp.updateStatus(doc.ID, model.DocumentStatusReady, "")
	database.DB.Model(&model.Document{}).Where("id = ?", doc.ID).Update("chunk_count", len(chunkModels))
	database.DB.Model(&model.KnowledgeBase{}).Where("id = ?", kbID).
		Update("document_count", gorm.Expr("document_count + 1"))

	applogger.L.Info("Document processed successfully",
		"doc_id", doc.ID, "chunks", len(chunkModels))
	return nil
}

func (dp *DocumentProcessor) storeChunks(kbID, docID int64, chunks []Chunk) []model.DocumentChunk {
	models := make([]model.DocumentChunk, len(chunks))
	for i, c := range chunks {
		models[i] = model.DocumentChunk{
			KnowledgeBaseID: kbID,
			DocumentID:      docID,
			ChunkIndex:      c.ChunkIndex,
			Content:         c.Content,
			StartOffset:     c.StartOffset,
			EndOffset:       c.EndOffset,
		}
	}
	database.DB.Create(&models)
	return models
}

func (dp *DocumentProcessor) generateEmbeddings(ctx context.Context, chunks []model.DocumentChunk) ([][]float32, error) {
	var allEmbeddings [][]float32

	for i := 0; i < len(chunks); i += embedBatchSize {
		end := i + embedBatchSize
		if end > len(chunks) {
			end = len(chunks)
		}

		texts := make([]string, end-i)
		for j := i; j < end; j++ {
			texts[j-i] = chunks[j].Content
		}

		embeddings, err := dp.embService.Embed(ctx, texts)
		if err != nil {
			return nil, fmt.Errorf("embedding batch %d failed: %w", i/embedBatchSize, err)
		}
		allEmbeddings = append(allEmbeddings, embeddings...)
	}

	return allEmbeddings, nil
}

func (dp *DocumentProcessor) updateStatus(docID int64, status int, errMsg string) {
	updates := map[string]interface{}{
		"status": status,
	}
	if errMsg != "" {
		updates["error_message"] = errMsg
	}
	database.DB.Model(&model.Document{}).Where("id = ?", docID).Updates(updates)
}

func (dp *DocumentProcessor) cleanupChunks(docID int64) {
	var chunkIDs []int64
	database.DB.Model(&model.DocumentChunk{}).Where("document_id = ?", docID).Pluck("id", &chunkIDs)
	if len(chunkIDs) > 0 {
		database.DB.Delete(&model.DocumentChunk{}, chunkIDs)
		applogger.L.Info("Cleaned up orphan chunks", "doc_id", docID, "count", len(chunkIDs))
	}
}
