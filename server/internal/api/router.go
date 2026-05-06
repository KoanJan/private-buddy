// Package router sets up the Gin HTTP router with all API endpoints.
package api

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"private-buddy-server/internal/api/handler"
	"private-buddy-server/internal/api/middleware"
	"private-buddy-server/internal/config"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// SetupRouter creates and configures the Gin engine with all routes.
// Includes CORS middleware, static file serving for avatars, and all API endpoints.
func SetupRouter(db *gorm.DB) *gin.Engine {
	r := gin.Default()

	r.Use(middleware.CORS())

	h := handler.NewHandler(db)

	r.GET("/", h.Root)
	r.GET("/api/version", h.GetVersion)

	avatarsDir := config.Get().GetAvatarsDir()
	os.MkdirAll(avatarsDir, 0755)
	r.GET("/avatars/:filename", func(c *gin.Context) {
		filename := c.Param("filename")
		if strings.Contains(filename, "..") {
			c.Status(http.StatusForbidden)
			return
		}
		filePath := filepath.Join(avatarsDir, filename)
		if _, err := os.Stat(filePath); os.IsNotExist(err) {
			c.Status(http.StatusNotFound)
			return
		}
		c.Header("Cache-Control", "public, max-age=86400")
		c.File(filePath)
	})

	api := r.Group("/api")
	{
		llmConfigs := api.Group("/llm-configs")
		{
			llmConfigs.POST("", h.CreateLLMConfig)
			llmConfigs.GET("", h.ListLLMConfigs)
			llmConfigs.GET("/:id", h.GetLLMConfig)
			llmConfigs.PUT("/:id", h.UpdateLLMConfig)
			llmConfigs.DELETE("/:id", h.DeleteLLMConfig)
		}

		embeddingConfigs := api.Group("/embedding-configs")
		{
			embeddingConfigs.POST("", h.CreateEmbeddingConfig)
			embeddingConfigs.GET("", h.ListEmbeddingConfigs)
			embeddingConfigs.GET("/:id", h.GetEmbeddingConfig)
			embeddingConfigs.PUT("/:id", h.UpdateEmbeddingConfig)
			embeddingConfigs.DELETE("/:id", h.DeleteEmbeddingConfig)
		}

		agents := api.Group("/agents")
		{
			agents.POST("", h.CreateAgent)
			agents.GET("", h.ListAgents)
			agents.GET("/with-sessions", h.ListAgentsWithSessions)
			agents.GET("/:id", h.GetAgent)
			agents.PUT("/:id", h.UpdateAgent)
			agents.DELETE("/:id", h.DeleteAgent)
		}

		sessions := api.Group("/sessions")
		{
			sessions.POST("", h.CreateSession)
			sessions.GET("", h.ListSessions)
			sessions.GET("/:id", h.GetSession)
			sessions.PUT("/:id", h.UpdateSession)
			sessions.DELETE("/:id", h.DeleteSession)
		}

		messages := api.Group("/messages")
		{
			messages.POST("/:id", h.CreateMessage)
			messages.GET("/:id", h.ListMessages)
		}

		chat := api.Group("/chat")
		{
			chat.POST("/new", h.CreateAndSend)
			chat.POST("/send/:session_id", h.SendMessage)
			chat.GET("/stream/:session_id", h.StreamMessages)
		}

		api.GET("/interactions", h.GetInteractions)
		api.GET("/messages/:id/interaction-status", h.GetInteractionStatus)

		searchConfig := api.Group("/search-config")
		{
			searchConfig.GET("", h.GetSearchConfig)
			searchConfig.PUT("", h.UpdateSearchConfig)
		}

		uploads := api.Group("/uploads")
		{
			uploads.POST("/avatar", h.UploadAvatar)
		}
	}

	return r
}
