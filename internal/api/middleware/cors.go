package middleware

import (
	"os"
	"strings"

	"github.com/gin-gonic/gin"
)

func CORS() gin.HandlerFunc {
	raw := os.Getenv("ALLOWED_ORIGINS")
	var allowed []string
	if raw != "" {
		for _, o := range strings.Split(raw, ",") {
			if s := strings.TrimSpace(o); s != "" {
				allowed = append(allowed, s)
			}
		}
	}

	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")

		if len(allowed) == 0 {
			c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		} else {
			for _, o := range allowed {
				if o == origin {
					c.Writer.Header().Set("Access-Control-Allow-Origin", origin)
					c.Writer.Header().Set("Vary", "Origin")
					break
				}
			}
		}

		c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		c.Next()
	}
}
