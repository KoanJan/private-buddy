package dops

import (
	"private-buddy-server/internal/config"
	"private-buddy-server/internal/database"
	applogger "private-buddy-server/internal/logger"
	"private-buddy-server/internal/model"
)

// GetVersion returns the version record in db of app
func GetVersion() string {
	var versionRecord model.DBVersion
	err := database.DB.Order("id DESC").First(&versionRecord).Error
	version := config.AppVersion
	if err == nil {
		version = versionRecord.Version
	} else {
		applogger.Error("failed to query version", "error", err)
	}
	return version
}
