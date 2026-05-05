package handler

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"private-buddy-server/internal/config"

	"github.com/gin-gonic/gin"
)

// maxAvatarFileSize is the maximum allowed avatar file size (2MB).
const maxAvatarFileSize = 2 * 1024 * 1024 // 2MB

// allowedAvatarExtensions defines the allowed image file extensions for avatars.
var allowedAvatarExtensions = map[string]bool{
	".jpg":  true,
	".jpeg": true,
	".png":  true,
	".webp": true,
}

// UploadAvatar handles avatar image upload for agents.
// Validates file type, size, and saves to the avatars directory.
func (h *Handler) UploadAvatar(c *gin.Context) {
	agentIDStr := c.Query("agent_id")
	if agentIDStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "agent_id is required"})
		return
	}
	agentID, err := strconv.ParseInt(agentIDStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "Invalid agent_id"})
		return
	}

	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "No file uploaded"})
		return
	}

	if file.Filename == "" {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "No filename provided"})
		return
	}

	ext := strings.ToLower(filepath.Ext(file.Filename))
	if !allowedAvatarExtensions[ext] {
		c.JSON(http.StatusBadRequest, gin.H{
			"detail": "Invalid file type. Allowed: .jpg, .jpeg, .png, .webp",
		})
		return
	}

	if file.Size > maxAvatarFileSize {
		c.JSON(http.StatusBadRequest, gin.H{
			"detail": fmt.Sprintf("File too large. Max size: %dMB", maxAvatarFileSize/(1024*1024)),
		})
		return
	}

	avatarsDir := config.Get().GetAvatarsDir()
	if err := os.MkdirAll(avatarsDir, 0755); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "Failed to create avatars directory"})
		return
	}

	filename := fmt.Sprintf("%d_%d%s", agentID, time.Now().Unix(), ext)
	savePath := filepath.Join(avatarsDir, filename)

	if err := c.SaveUploadedFile(file, savePath); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "Failed to save file"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"filename": filename})
}
