package handler

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"private-buddy-server/internal/config"
	"private-buddy-server/internal/constants"
	"private-buddy-server/internal/database"
	applogger "private-buddy-server/internal/logger"
	"private-buddy-server/internal/model"
	"private-buddy-server/internal/schema"
	"private-buddy-server/internal/service"
	"private-buddy-server/internal/service/kb"

	"private-buddy-server/internal/api/response"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type KBHandler struct {
	crudKB  *service.CRUDBase[model.KnowledgeBase]
	crudDoc *service.CRUDBase[model.Document]
}

func NewKBHandler() *KBHandler {
	return &KBHandler{
		crudKB:  service.NewCRUDBase[model.KnowledgeBase]("Knowledge base"),
		crudDoc: service.NewCRUDBase[model.Document]("Document"),
	}
}

func (h *KBHandler) CreateKnowledgeBase(c *gin.Context) {
	var req schema.KnowledgeBaseCreate
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	entity := model.KnowledgeBase{
		Name:        req.Name,
		Description: req.Description,
	}

	if err := kb.CreateKnowledgeBase(&entity); err != nil {
		response.InternalError(c, err.Error())
		return
	}

	response.Success(c, entity)
}

func (h *KBHandler) ListKnowledgeBases(c *gin.Context) {
	skip, limit := getPagination(c)
	entities, err := h.crudKB.GetMulti(skip, limit)
	if err != nil {
		response.InternalError(c, err.Error())
		return
	}

	type kbWithStats struct {
		model.KnowledgeBase
		DocumentCount int64 `json:"document_count"`
	}

	results := make([]kbWithStats, 0)
	for _, entity := range entities {
		var count int64
		if err := database.DB.Model(&model.Document{}).Where("knowledge_base_id = ?", entity.ID).Count(&count).Error; err != nil {
			applogger.Warn("failed to count documents for KB list", "kb_id", entity.ID, "error", err)
		}
		results = append(results, kbWithStats{
			KnowledgeBase: entity,
			DocumentCount: count,
		})
	}

	response.Success(c, results)
}

func (h *KBHandler) GetKnowledgeBase(c *gin.Context) {
	entity, err := h.crudKB.Get(getPathID(c))
	if err != nil {
		response.NotFound(c, err.Error())
		return
	}
	response.Success(c, entity)
}

func (h *KBHandler) UpdateKnowledgeBase(c *gin.Context) {
	var req schema.KnowledgeBaseUpdate
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	id := getPathID(c)
	entity, err := h.crudKB.Get(id)
	if err != nil {
		response.NotFound(c, err.Error())
		return
	}

	updates := req.BuildUpdates()
	if len(updates) > 0 {
		if err := h.crudKB.Update(entity, updates); err != nil {
			response.InternalError(c, err.Error())
			return
		}
	}

	response.Success(c, entity)
}

func (h *KBHandler) DeleteKnowledgeBase(c *gin.Context) {
	id := getPathID(c)
	if err := kb.DeleteKnowledgeBase(id); err != nil {
		response.InternalError(c, err.Error())
		return
	}
	response.Success(c, nil)
}

func (h *KBHandler) ListDocuments(c *gin.Context) {
	kbID := getPathID(c)
	var documents []model.Document
	if err := database.DB.Where("knowledge_base_id = ?", kbID).Order("created_at DESC").Find(&documents).Error; err != nil {
		response.InternalError(c, err.Error())
		return
	}
	response.Success(c, documents)
}

func (h *KBHandler) UploadDocument(c *gin.Context) {
	kbID := getPathID(c)

	file, header, err := c.Request.FormFile("file")
	if err != nil {
		response.BadRequest(c, "No file provided")
		return
	}
	defer file.Close()

	// Validate file extension
	ext := strings.ToLower(filepath.Ext(header.Filename))
	if !constants.IsAllowedFileExtension(ext) {
		response.BadRequest(c, fmt.Sprintf("Unsupported file type: %s. Allowed types: .txt, .md, .pdf", ext))
		return
	}

	kbDir := config.Get().GetKBDir()
	docDir := filepath.Join(kbDir, fmt.Sprintf("%d", kbID), "files")
	if err := os.MkdirAll(docDir, 0755); err != nil {
		response.InternalError(c, err.Error())
		return
	}

	filePath := filepath.Join(docDir, header.Filename)
	dst, err := os.Create(filePath)
	if err != nil {
		response.InternalError(c, err.Error())
		return
	}
	defer dst.Close()

	if _, err := dst.ReadFrom(file); err != nil {
		response.InternalError(c, err.Error())
		return
	}

	doc := model.Document{
		KnowledgeBaseID: kbID,
		Title:           header.Filename,
		FilePath:        filePath,
		Status:          model.DocumentStatusPending,
	}

	if err := database.DB.Create(&doc).Error; err != nil {
		response.InternalError(c, err.Error())
		return
	}

	kb.SubmitDocument(doc.ID)

	response.Success(c, doc)
}

func (h *KBHandler) GetDocument(c *gin.Context) {
	docID := getPathIDByParam(c, "doc_id")
	entity, err := h.crudDoc.Get(docID)
	if err != nil {
		response.NotFound(c, err.Error())
		return
	}
	response.Success(c, entity)
}

func (h *KBHandler) DeleteDocument(c *gin.Context) {
	docID := getPathIDByParam(c, "doc_id")
	var doc model.Document
	if err := database.DB.First(&doc, docID).Error; err != nil {
		response.NotFound(c, err.Error())
		return
	}

	if doc.FilePath != "" {
		os.Remove(doc.FilePath)
	}

	var chunkCount int64
	if err := database.DB.Model(&model.DocumentChunk{}).Where("document_id = ? AND deleted = 0", docID).Count(&chunkCount).Error; err != nil {
		applogger.Error("failed to count chunks for document soft-delete", "doc_id", docID, "error", err)
		response.InternalError(c, "Failed to delete document")
		return
	}
	if chunkCount > 0 {
		if err := database.DB.Model(&model.DocumentChunk{}).Where("document_id = ? AND deleted = 0", docID).Update("deleted", 1).Error; err != nil {
			applogger.Error("failed to soft-delete document chunks", "doc_id", docID, "error", err)
		}
		if err := database.DB.Model(&model.KnowledgeBase{}).Where("id = ?", doc.KnowledgeBaseID).
			Update("deleted_count", gorm.Expr("deleted_count + ?", chunkCount)).Error; err != nil {
			applogger.Warn("failed to update KB deleted_count after document delete", "kb_id", doc.KnowledgeBaseID, "error", err)
		}
	}

	if err := database.DB.Delete(&doc).Error; err != nil {
		applogger.Error("failed to delete document", "doc_id", docID, "error", err)
		response.InternalError(c, "Failed to delete document")
		return
	}

	response.Success(c, nil)
}

func (h *KBHandler) SearchKB(c *gin.Context) {
	kbID := getPathID(c)
	var req schema.SearchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	results, err := kb.SearchKB(c.Request.Context(), kbID, req.Query, req.TopK)
	if err != nil {
		response.InternalError(c, err.Error())
		return
	}
	response.Success(c, results)
}

func (h *KBHandler) SearchMultiKB(c *gin.Context) {
	var req schema.MultiKBSearchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	results, err := kb.SearchMultiKB(c.Request.Context(), req.KBIDs, req.Query, req.TopK)
	if err != nil {
		response.InternalError(c, err.Error())
		return
	}
	response.Success(c, results)
}

// isImageFile checks if the file extension indicates an image.
func isImageFile(filename string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	imageExts := []string{".jpg", ".jpeg", ".png", ".gif", ".webp", ".bmp"}
	for _, e := range imageExts {
		if ext == e {
			return true
		}
	}
	return false
}
