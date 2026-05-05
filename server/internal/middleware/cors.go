// Package middleware provides HTTP middleware for the Gin router.
package middleware

import "github.com/gin-gonic/gin"

// CORS returns a middleware that sets Cross-Origin Resource Sharing headers.
// Allows all origins, methods, and headers for development convenience.
func CORS() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Credentials", "true")
		c.Header("Access-Control-Allow-Methods", "*")
		c.Header("Access-Control-Allow-Headers", "*")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	}
}
