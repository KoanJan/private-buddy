package handler

import (
	"fmt"
	"os"
	"strconv"

	"private-buddy-server/internal/api/response"
	"private-buddy-server/internal/config"

	"github.com/gin-gonic/gin"
)

func getPathID(c *gin.Context) int64 {
	return getPathIDByParam(c, "id")
}

// getPathIDByParam extracts an int64 ID from the URL path by parameter name.
// Returns 0 if the parameter is not a valid integer.
func getPathIDByParam(c *gin.Context, param string) int64 {
	idStr := c.Param(param)
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

// handleNotFound returns a not-found response via business code.
func handleNotFound(c *gin.Context, entityName string, id int64) {
	response.NotFound(c, fmt.Sprintf("%s %d not found", entityName, id))
}
