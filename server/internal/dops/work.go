package dops

import (
	"private-buddy-server/internal/database"
	"private-buddy-server/internal/model"
)

// ListTaskWorks list all task work ids in the session
func ListTaskWorks(sessionID int64) ([]int64, error) {
	var workIDs []int64
	if err := database.DB.Model(&model.Work{}).
		Where("session_id = ? AND type = ?", sessionID, model.WorkTypeTask).
		Pluck("id", &workIDs).Error; err != nil {
		return nil, err
	}
	return workIDs, nil
}
