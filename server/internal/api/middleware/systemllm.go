package middleware

import (
	"private-buddy-server/internal/api/response"
	"private-buddy-server/internal/service"

	"github.com/gin-gonic/gin"
)

// RequireSystemLLM blocks requests when the system-level LLM config is not set up.
func RequireSystemLLM(c *gin.Context) {
	if !service.IsSystemLLMConfigured() {
		response.BadRequest(c, "System LLM config is required but not configured")
		c.Abort()
		return
	}
	c.Next()
}
