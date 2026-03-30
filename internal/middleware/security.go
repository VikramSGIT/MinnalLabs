package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

func SecurityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		headers := c.Writer.Header()
		headers.Set("X-Content-Type-Options", "nosniff")
		headers.Set("X-Frame-Options", "DENY")
		headers.Set("X-XSS-Protection", "0")
		headers.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		headers.Set("Cache-Control", "no-store")
		headers.Set("Pragma", "no-cache")
		c.Next()
	}
}

func LimitRequestBody(maxBytes int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if shouldLimitRequestBody(c.Request) {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes)
		}
		c.Next()
	}
}

func shouldLimitRequestBody(r *http.Request) bool {
	if r == nil || r.Body == nil {
		return false
	}

	switch r.Method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
	default:
		return false
	}

	contentType := strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Type")))
	return !strings.HasPrefix(contentType, "multipart/form-data")
}
