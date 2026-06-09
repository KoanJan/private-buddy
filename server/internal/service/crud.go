package service

import (
	"private-buddy-server/internal/database"
)

// CRUDBase provides generic CRUD operations for any GORM model.
// Used to avoid duplicating basic database operations across handlers.
// The database connection is obtained from the database package directly.
type CRUDBase[T any] struct {
	entityName string
}

// NewCRUDBase creates a CRUDBase instance for the given model type.
func NewCRUDBase[T any](entityName string) *CRUDBase[T] {
	return &CRUDBase[T]{entityName: entityName}
}

// Get retrieves a single entity by ID.
func (c *CRUDBase[T]) Get(id int64) (*T, error) {
	var entity T
	if err := database.DB.First(&entity, id).Error; err != nil {
		return nil, err
	}
	return &entity, nil
}

// GetMulti retrieves multiple entities with pagination.
func (c *CRUDBase[T]) GetMulti(skip, limit int) ([]T, error) {
	var entities []T
	if err := database.DB.Offset(skip).Limit(limit).Find(&entities).Error; err != nil {
		return nil, err
	}
	return entities, nil
}

// Create inserts a new entity.
func (c *CRUDBase[T]) Create(entity *T) error {
	return database.DB.Create(entity).Error
}

// Update applies partial updates to an entity.
func (c *CRUDBase[T]) Update(entity *T, updates map[string]interface{}) error {
	return database.DB.Model(entity).Updates(updates).Error
}

// Delete removes an entity by ID.
func (c *CRUDBase[T]) Delete(id int64) error {
	var entity T
	return database.DB.Delete(&entity, id).Error
}
