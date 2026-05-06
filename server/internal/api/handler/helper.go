package handler

import (
	"os"
	"strconv"

	"private-buddy-server/internal/config"

	"github.com/gin-gonic/gin"
)

func getPathID(c *gin.Context) int64 {
	idStr := c.Param("id")
	id, _ := strconv.ParseInt(idStr, 10, 64)
	return id
}

func getPagination(c *gin.Context) (skip, limit int) {
	skip = 0
	limit = 100
	if s := c.Query("skip"); s != "" {
		if n, err := strconv.Atoi(s); err == nil {
			skip = n
		}
	}
	if l := c.Query("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 {
			limit = n
		}
	}
	return
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func getAvatarsDir() string {
	return config.Get().GetAvatarsDir()
}

func osRemoveIfExists(path string) {
	os.Remove(path)
}

func removeSessionWorkspace(sessionID int64) {
	settings := config.Get()
	workspaceDir := settings.GetWorkspaceRoot() + "/" + strconv.FormatInt(sessionID, 10)
	os.RemoveAll(workspaceDir)
}
