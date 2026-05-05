package service

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// CRUDBase provides generic CRUD operations for any GORM model.
// Used to avoid duplicating basic database operations across handlers.
type CRUDBase[T any] struct {
	db         *gorm.DB
	entityName string
}

// NewCRUDBase creates a CRUDBase instance for the given model type.
func NewCRUDBase[T any](db *gorm.DB, entityName string) *CRUDBase[T] {
	return &CRUDBase[T]{db: db, entityName: entityName}
}

// Get retrieves a single entity by ID.
func (c *CRUDBase[T]) Get(id int64) (*T, error) {
	var entity T
	if err := c.db.First(&entity, id).Error; err != nil {
		return nil, err
	}
	return &entity, nil
}

// GetMulti retrieves multiple entities with pagination.
func (c *CRUDBase[T]) GetMulti(skip, limit int) ([]T, error) {
	var entities []T
	if err := c.db.Offset(skip).Limit(limit).Find(&entities).Error; err != nil {
		return nil, err
	}
	return entities, nil
}

// Create inserts a new entity.
func (c *CRUDBase[T]) Create(entity *T) error {
	return c.db.Create(entity).Error
}

// Update applies partial updates to an entity.
func (c *CRUDBase[T]) Update(entity *T, updates map[string]interface{}) error {
	return c.db.Model(entity).Updates(updates).Error
}

// Delete removes an entity by ID.
func (c *CRUDBase[T]) Delete(id int64) error {
	var entity T
	return c.db.Delete(&entity, id).Error
}

// HandleNotFound returns a 404 JSON response for a missing entity.
func HandleNotFound(c *gin.Context, entityName string, id int64) {
	c.JSON(http.StatusNotFound, gin.H{
		"detail": entityName + " not found",
	})
}
