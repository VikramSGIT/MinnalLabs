package googleauth

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/iot-backend/internal/config"
	"github.com/iot-backend/internal/oauth"
)

// SetupTestGoogleLoginRoute registers a stress-only endpoint that simulates
// Google Sign-In without going through the real Google consent screen.
// It must only be called when SERVER_PROFILE=stress.
func SetupTestGoogleLoginRoute(r *gin.Engine, cfg *config.Config) {
	r.POST("/api/test/google-login", func(c *gin.Context) {
		// Double-gate: also check profile inside the handler
		if !strings.EqualFold(strings.TrimSpace(cfg.Server.Profile), "stress") {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}

		var req struct {
			GoogleSub string `json:"google_sub" binding:"required"`
			Email     string `json:"email" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		user, err := FindOrCreateGoogleUser(req.GoogleSub, req.Email)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create user: " + err.Error()})
			return
		}

		if err := oauth.IssueSessionForUser(c, user); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to issue session: " + err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"user_id":  user.ID,
			"username": user.Username,
		})
	})
}
