package middleware

import (
	"private-buddy-server/internal/api/response"
	"private-buddy-server/internal/dops"

	"github.com/gin-gonic/gin"
)

// RequireEmbedding blocks requests when the embedding config is not set up.
func RequireEmbedding(c *gin.Context) {
	if !dops.IsEmbeddingConfigured() {
		response.BadRequest(c, "Embedding config is required but not configured")
		c.Abort()
		return
	}
	c.Next()
}
