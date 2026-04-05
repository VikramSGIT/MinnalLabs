package auth

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/iot-backend/internal/db"
	"github.com/iot-backend/internal/models"
)

// registrationPayload matches the JSON body sent by the Kratos registration webhook
// (shaped by ory/kratos/registration-hook.jsonnet).
type registrationPayload struct {
	IdentityID string `json:"identity_id" binding:"required"`
}

// handleRegistrationHook creates an app-level user row when Kratos registers
// a new identity. This is called by the Kratos after-registration webhook.
// The row is a pure ID mapping — all identity data (username, email) lives in Kratos.
func handleRegistrationHook(c *gin.Context) {
	var payload registrationPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})
		return
	}

	user := models.User{
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
