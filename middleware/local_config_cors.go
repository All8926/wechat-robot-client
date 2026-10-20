package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// LocalConfigCORS 允许本机配置页跨域读写运行时配置。
func LocalConfigCORS(c *gin.Context) {
	origin := c.GetHeader("Origin")
	if origin == "http://127.0.0.1:18473" || origin == "http://localhost:18473" {
		c.Header("Access-Control-Allow-Origin", origin)
		c.Header("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type")
		c.Header("Vary", "Origin")
	}
	if c.Request.Method == http.MethodOptions {
		c.AbortWithStatus(http.StatusNoContent)
		return
	}
	c.Next()
}
