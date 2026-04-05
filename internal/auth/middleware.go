package auth

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/iot-backend/internal/db"
	"github.com/iot-backend/internal/models"
)

const (
	sessionUserContextKey    = "session_user"
	oauthPrincipalContextKey = "oauth_principal"
)

// SessionUser carries the authenticated user information through Gin context.
type SessionUser struct {
	UserID   uint   `json:"user_id"`
	Username string `json:"username"`
	IsAdmin  bool   `json:"is_admin"`
}

// OAuthPrincipal carries the OAuth2 token subject through Gin context.
type OAuthPrincipal struct {
	UserID    uint
	RawUserID string
	ClientID  string
}

// RequireSession validates the Kratos session cookie and populates SessionUser in context.
func RequireSession() gin.HandlerFunc {
	return func(c *gin.Context) {
		cookie := extractKratosCookie(c.Request)
		if cookie == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}

		// Try Valkey cache first.
		if cached, ok := getCachedSession(cookie); ok {
			c.Set(sessionUserContextKey, SessionUser{
				UserID:   cached.UserID,
				Username: cached.Username,
				IsAdmin:  cached.IsAdmin,
			})
			c.Next()
			return
		}

		// Call Kratos to validate the session.
		sess, err := lookupKratosSession(c.Request.Context(), cookie)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}

		// Reject unverified email addresses.
		if !isEmailVerified(sess) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "email_not_verified"})
			return
		}

		// Look up the app user by Kratos identity ID — create on first verified login.
		var user models.User
		if err := db.DB.Where("kratos_identity_id = ?", sess.Identity.ID).First(&user).Error; err != nil {
			user = models.User{KratosIdentityID: sess.Identity.ID}
			if err := db.DB.Create(&user).Error; err != nil {
				// Race condition: another request created it first.
				if db.DB.Where("kratos_identity_id = ?", sess.Identity.ID).First(&user).Error != nil {
					c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "failed to create user"})
					return
				}
			}
		}

		isAdmin := isAdminUser(user.ID)

		// Username comes from Kratos identity traits, not the app DB.
		username := sess.Identity.Traits.Username

		// Cache the resolved session.
		cacheSession(cookie, cachedSessionInfo{
			UserID:   user.ID,
			Username: username,
			IsAdmin:  isAdmin,
		})

		c.Set(sessionUserContextKey, SessionUser{
			UserID:   user.ID,
			Username: username,
			IsAdmin:  isAdmin,
		})
		c.Next()
	}
}

// RequireAdmin checks that the session user has admin privileges.
// Must be called after RequireSession.
func RequireAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		sessionUser, ok := CurrentSessionUser(c)
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		if !sessionUser.IsAdmin {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "admin access required"})
			return
		}
		c.Next()
	}
}

// RequireOAuthToken validates a Bearer token via Hydra introspection.
func RequireOAuthToken() gin.HandlerFunc {
	return func(c *gin.Context) {
		token, ok := bearerTokenFromHeader(c.GetHeader("Authorization"))
		if !ok {
			c.Header("WWW-Authenticate", `Bearer realm="google-fulfillment"`)
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid access token"})
			return
		}

		result, err := introspectToken(c.Request.Context(), token)
		if err != nil || !result.Active {
			c.Header("WWW-Authenticate", `Bearer realm="google-fulfillment"`)
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid access token"})
			return
		}

		// Extract app_user_id from the token session (set during Hydra consent).
		appUserIDStr, _ := result.Ext["app_user_id"].(string)
		if appUserIDStr == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid access token"})
			return
		}
		parsedUserID, err := strconv.ParseUint(appUserIDStr, 10, 64)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid access token"})
			return
		}

		c.Set(oauthPrincipalContextKey, OAuthPrincipal{
			UserID:    uint(parsedUserID),
			RawUserID: appUserIDStr,
			ClientID:  result.ClientID,
		})
		c.Next()
	}
}

// CurrentSessionUser retrieves the SessionUser from Gin context.
func CurrentSessionUser(c *gin.Context) (SessionUser, bool) {
	value, ok := c.Get(sessionUserContextKey)
	if !ok {
		return SessionUser{}, false
	}
	user, ok := value.(SessionUser)
	return user, ok
}

// CurrentOAuthPrincipal retrieves the OAuthPrincipal from Gin context.
func CurrentOAuthPrincipal(c *gin.Context) (OAuthPrincipal, bool) {
	value, ok := c.Get(oauthPrincipalContextKey)
	if !ok {
		return OAuthPrincipal{}, false
	}
	principal, ok := value.(OAuthPrincipal)
	return principal, ok
}

func bearerTokenFromHeader(header string) (string, bool) {
	header = strings.TrimSpace(header)
	if header == "" {
		return "", false
	}
	parts := strings.SplitN(header, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return "", false
	}
	token := strings.TrimSpace(parts[1])
	return token, token != ""
}

func extractKratosCookie(r *http.Request) string {
	for _, c := range r.Cookies() {
		if c.Name == "ory_kratos_session" {
			return strings.TrimSpace(c.Value)
		}
	}
	return ""
}

func isAdminUser(userID uint) bool {
	if userID == 0 {
		return false
	}
	var count int64
	if err := db.DB.Model(&models.AdminUser{}).Where("user_id = ?", userID).Count(&count).Error; err != nil {
		return false
	}
	return count > 0
}
