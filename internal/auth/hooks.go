package auth

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/iot-backend/internal/db"
	"github.com/iot-backend/internal/models"
)

// registrationPayload matches the JSON body sent by the Kratos registration webhook
// (shaped by ory/kratos/registration-hook.jsonnet).
type registrationPayload struct {
	IdentityID string `json:"identity_id" binding:"required"`
	Username   string `json:"username" binding:"required"`
	Email      string `json:"email"`
}

// handleRegistrationHook creates an app-level user row when Kratos registers
// a new identity. This is called by the Kratos after-registration webhook.
func handleRegistrationHook(c *gin.Context) {
	var payload registrationPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})
		return
	}

	payload.Username = strings.TrimSpace(payload.Username)
	if payload.Username == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "username is required"})
		return
	}

	user := models.User{
		Username:         payload.Username,
		KratosIdentityID: payload.IdentityID,
	}
	if err := db.DB.Create(&user).Error; err != nil {
		// If the user already exists (e.g. duplicate webhook), treat as success.
		var existing models.User
		if db.DB.Where("kratos_identity_id = ?", payload.IdentityID).First(&existing).Error == nil {
			c.JSON(http.StatusOK, gin.H{"user_id": existing.ID})
			return
		}
		c.JSON(http.StatusConflict, gin.H{"error": "failed to create user"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"user_id": user.ID})
}
