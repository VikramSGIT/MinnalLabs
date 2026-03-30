package oauth

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/iot-backend/internal/config"
	"github.com/iot-backend/internal/db"
	localmodels "github.com/iot-backend/internal/models"
	"github.com/iot-backend/internal/state"
)

const sessionUserContextKey = "session_user"

var appCfg *config.Config

type SessionUser struct {
	Token    string
	UserID   uint   `json:"user_id"`
	Username string `json:"username"`
	IsAdmin  bool   `json:"is_admin"`
}

func IsAdminUser(userID uint) bool {
	if userID == 0 {
		return false
	}

	var count int64
	if err := db.DB.Model(&localmodels.AdminUser{}).
		Where("user_id = ?", userID).
		Count(&count).Error; err != nil {
		return false
	}

	return count > 0
}

func initSessionConfig(cfg *config.Config) {
	appCfg = cfg
}

func issueSession(c *gin.Context, user *localmodels.User) error {
	if existingToken := sessionTokenFromRequest(c.Request); existingToken != "" {
		state.DeleteSession(existingToken)
	}

	token, _, err := state.CreateSession(user.ID, user.Username)
	if err != nil {
		return err
	}

	http.SetCookie(c.Writer, sessionCookie(token))
	return nil
}

func destroySession(c *gin.Context) {
	if token := sessionTokenFromRequest(c.Request); token != "" {
		state.DeleteSession(token)
	}
	http.SetCookie(c.Writer, expiredSessionCookie())
}

func restoreSessionFromRequest(r *http.Request) (SessionUser, bool) {
	token := sessionTokenFromRequest(r)
	if token == "" {
		return SessionUser{}, false
	}

	info, ok := state.TouchSession(token)
	if !ok {
		return SessionUser{}, false
	}

	return SessionUser{
		Token:    token,
		UserID:   info.UserID,
		Username: info.Username,
		IsAdmin:  IsAdminUser(info.UserID),
	}, true
}

func CurrentSessionUser(c *gin.Context) (SessionUser, bool) {
	value, ok := c.Get(sessionUserContextKey)
	if !ok {
		return SessionUser{}, false
	}

	user, ok := value.(SessionUser)
	return user, ok
}

func RequireSession() gin.HandlerFunc {
	return func(c *gin.Context) {
		sessionUser, ok := restoreSessionFromRequest(c.Request)
		if !ok {
			http.SetCookie(c.Writer, expiredSessionCookie())
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}

		http.SetCookie(c.Writer, sessionCookie(sessionUser.Token))
		c.Set(sessionUserContextKey, sessionUser)
		c.Next()
	}
}

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

func sessionTokenFromRequest(r *http.Request) string {
	cookie, err := r.Cookie(sessionCookieName())
	if err != nil {
		return ""
	}

	return strings.TrimSpace(cookie.Value)
}

func sessionCookieName() string {
	if appCfg == nil {
		return "user_session"
	}

	name := strings.TrimSpace(appCfg.Session.CookieName)
	if name == "" {
		return "user_session"
	}

	return name
}

func sessionCookie(token string) *http.Cookie {
	ttl := 7 * 24 * time.Hour
	secure := true
	sameSite := http.SameSiteLaxMode
	domain := ""

	if appCfg != nil {
		ttl = appCfg.SessionTTL()
		secure = appCfg.Session.Secure
		sameSite = appCfg.SessionSameSite()
		domain = strings.TrimSpace(appCfg.Session.Domain)
	}

	if sameSite == http.SameSiteNoneMode {
		secure = true
	}

	return &http.Cookie{
		Name:     sessionCookieName(),
		Value:    token,
		Path:     "/",
		Domain:   domain,
		Expires:  time.Now().Add(ttl),
		MaxAge:   int(ttl.Seconds()),
		HttpOnly: true,
		Secure:   secure,
		SameSite: sameSite,
	}
}

func expiredSessionCookie() *http.Cookie {
	cookie := sessionCookie("")
	cookie.Value = ""
	cookie.Expires = time.Unix(0, 0)
	cookie.MaxAge = -1
	return cookie
}
