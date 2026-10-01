package router

import (
	"fmt"

	"github.com/gin-gonic/gin"
	"github.com/v03413/bepusdt/app/log"
)

// Inline scripts/styles remain compatible with the shipped Vue and checkout templates.
const deploymentCSP = "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; img-src 'self' data: https:; font-src 'self' data:; connect-src 'self'; object-src 'none'; base-uri 'self'; frame-ancestors 'self'; form-action 'self'"

func securityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "SAMEORIGIN")
		c.Header("Content-Security-Policy", deploymentCSP)
		c.Header("Referrer-Policy", "no-referrer")
		c.Next()
	}
}

// Log route templates only: no raw path, secret entrance, query, headers or bodies.
func accessLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
		path := c.FullPath()
		if path == "" {
			path = "[unmatched]"
		}
		log.Info(fmt.Sprintf("http method=%s route=%s status=%d", c.Request.Method, path, c.Writer.Status()))
	}
}

func safeRecovery() gin.HandlerFunc {
	// Gin's default recovery dumps raw requests. Suppress it entirely rather than
	// attempting to identify every secret carried in arbitrary header/query names.
	return gin.CustomRecoveryWithWriter(nil, func(c *gin.Context, _ any) {
		log.Error("http panic recovered")
		c.AbortWithStatus(500)
	})
}
