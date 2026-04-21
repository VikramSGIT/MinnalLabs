package googleauth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/iot-backend/internal/config"
	"github.com/iot-backend/internal/db"
	localmodels "github.com/iot-backend/internal/models"
	"github.com/iot-backend/internal/oauth"
	"github.com/iot-backend/internal/state"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"gorm.io/gorm"
)

var sanitizeRe = regexp.MustCompile(`[^A-Za-z0-9_]`)

const (
	oauthIntentLogin = "login"
	oauthIntentLink  = "link"
)

type oauthStatePayload struct {
	Intent string `json:"intent"`
	UserID uint   `json:"user_id,omitempty"`
}

func oauthConfig(cfg *config.Config) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     cfg.GoogleAuth.ClientID,
		ClientSecret: cfg.GoogleAuth.ClientSecret,
		RedirectURL:  cfg.GoogleAuth.RedirectURI,
		Scopes:       []string{"openid", "email", "profile"},
		Endpoint:     google.Endpoint,
	}
}

func csrfStateKey(token string) string {
	return "google_csrf:" + token
}

func storeOAuthState(payload oauthStatePayload) (string, error) {
	stateToken, err := generateRandomToken(32)
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	rdb := state.RedisClient()
	if err := rdb.Set(context.Background(), csrfStateKey(stateToken), string(data), 10*time.Minute).Err(); err != nil {
		return "", err
	}
	return stateToken, nil
}

func consumeOAuthState(stateToken string) (oauthStatePayload, error) {
	rdb := state.RedisClient()
	val, err := rdb.GetDel(context.Background(), csrfStateKey(stateToken)).Result()
	if err != nil || val == "" {
		return oauthStatePayload{}, errors.New("invalid or expired state")
	}
	var payload oauthStatePayload
	if err := json.Unmarshal([]byte(val), &payload); err != nil {
		return oauthStatePayload{}, errors.New("malformed state")
	}
	return payload, nil
}

// SetupGoogleAuthRoutes registers the production Google Sign-In routes.
func SetupGoogleAuthRoutes(r *gin.Engine, cfg *config.Config) {
	oc := oauthConfig(cfg)

	r.GET("/auth/google/login", func(c *gin.Context) {
		if cfg.GoogleAuth.ClientID == "" || cfg.GoogleAuth.ClientSecret == "" {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Google Sign-In is not configured"})
			return
		}

		stateToken, err := storeOAuthState(oauthStatePayload{Intent: oauthIntentLogin})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate state"})
			return
		}

		url := oc.AuthCodeURL(stateToken)
		c.Redirect(http.StatusTemporaryRedirect, url)
	})

	r.GET("/auth/google/callback", func(c *gin.Context) {
		if cfg.GoogleAuth.ClientID == "" || cfg.GoogleAuth.ClientSecret == "" {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Google Sign-In is not configured"})
			return
		}

		stateToken := c.Query("state")
		if stateToken == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "missing state parameter"})
			return
		}

		payload, err := consumeOAuthState(stateToken)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		code := c.Query("code")
		if code == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "missing code parameter"})
			return
		}

		token, err := oc.Exchange(context.Background(), code)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "failed to exchange code"})
			return
		}

		userInfo, err := fetchGoogleUserInfo(token.AccessToken)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch user info"})
			return
		}

		if !userInfo.EmailVerified {
			c.JSON(http.StatusBadRequest, gin.H{"error": "email not verified by Google"})
			return
		}

		switch payload.Intent {
		case oauthIntentLink:
			handleGoogleLinkCallback(c, payload, userInfo)
		case oauthIntentLogin:
			handleGoogleLoginCallback(c, userInfo)
		default:
			c.JSON(http.StatusBadRequest, gin.H{"error": "unknown state intent"})
		}
	})

	r.POST("/api/session/link-google", oauth.RequireSession(), func(c *gin.Context) {
		if cfg.GoogleAuth.ClientID == "" || cfg.GoogleAuth.ClientSecret == "" {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Google Sign-In is not configured"})
			return
		}

		sessionUser, ok := oauth.CurrentSessionUser(c)
		if !ok {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}

		var user localmodels.User
		if err := db.DB.First(&user, sessionUser.UserID).Error; err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		if user.GoogleSub != "" {
			c.JSON(http.StatusConflict, gin.H{"error": "already linked"})
			return
		}

		stateToken, err := storeOAuthState(oauthStatePayload{
			Intent: oauthIntentLink,
			UserID: sessionUser.UserID,
		})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate state"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"redirect_url": oc.AuthCodeURL(stateToken)})
	})
}

func handleGoogleLoginCallback(c *gin.Context, userInfo *googleUserInfo) {
	user, err := findOrCreateGoogleUser(userInfo.Sub, userInfo.Email)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create user"})
		return
	}

	if err := oauth.IssueSessionForUser(c, user); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to issue session"})
		return
	}

	c.Redirect(http.StatusTemporaryRedirect, "/")
}

func handleGoogleLinkCallback(c *gin.Context, payload oauthStatePayload, userInfo *googleUserInfo) {
	sessionUser, ok := oauth.SessionFromRequest(c)
	if !ok || sessionUser.UserID == 0 || sessionUser.UserID != payload.UserID {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "link session mismatch"})
		return
	}

	var existingBySub localmodels.User
	err := db.DB.Where("google_sub = ?", userInfo.Sub).First(&existingBySub).Error
	if err == nil {
		c.JSON(http.StatusConflict, gin.H{"error": "google account already linked"})
		return
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to check google identity"})
		return
	}

	var user localmodels.User
	if err := db.DB.First(&user, payload.UserID).Error; err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "link session mismatch"})
		return
	}
	if user.GoogleSub != "" {
		c.JSON(http.StatusConflict, gin.H{"error": "account already has a linked google identity"})
		return
	}

	user.GoogleSub = userInfo.Sub
	if err := db.DB.Save(&user).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to link google identity"})
		return
	}

	c.Redirect(http.StatusTemporaryRedirect, "/?linked=1")
}

type googleUserInfo struct {
	Sub           string `json:"sub"`
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
}

func fetchGoogleUserInfo(accessToken string) (*googleUserInfo, error) {
	req, err := http.NewRequest(http.MethodGet, "https://www.googleapis.com/oauth2/v3/userinfo", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("google userinfo returned %d: %s", resp.StatusCode, string(body))
	}

	var info googleUserInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, err
	}
	if info.Sub == "" {
		return nil, fmt.Errorf("google userinfo missing sub")
	}
	return &info, nil
}

// FindOrCreateGoogleUser finds or creates a user by Google subject ID.
// Exported so that the stress test endpoint can reuse it.
func FindOrCreateGoogleUser(googleSub, email string) (*localmodels.User, error) {
	return findOrCreateGoogleUser(googleSub, email)
}

func findOrCreateGoogleUser(googleSub, email string) (*localmodels.User, error) {
	var existing localmodels.User
	if err := db.DB.Where("google_sub = ?", googleSub).First(&existing).Error; err == nil {
		return &existing, nil
	}

	username := deriveUsername(email)

	var byUsername localmodels.User
	if err := db.DB.Where("username = ?", username).First(&byUsername).Error; err == nil {
		username = findAvailableUsername(username)
	}

	newUser := localmodels.User{
		Username:  username,
		GoogleSub: googleSub,
	}
	if err := db.DB.Create(&newUser).Error; err != nil {
		return nil, fmt.Errorf("create google user: %w", err)
	}
	return &newUser, nil
}

func deriveUsername(email string) string {
	local := email
	if idx := strings.Index(email, "@"); idx > 0 {
		local = email[:idx]
	}

	username := sanitizeRe.ReplaceAllString(local, "_")
	for len(username) < 3 {
		username += "_"
	}
	if len(username) > 64 {
		username = username[:64]
	}
	return username
}

func findAvailableUsername(base string) string {
	for i := 2; i < 10000; i++ {
		candidate := fmt.Sprintf("%s_%d", base, i)
		if len(candidate) > 64 {
			candidate = candidate[:64]
		}
		var count int64
		db.DB.Model(&localmodels.User{}).Where("username = ?", candidate).Count(&count)
		if count == 0 {
			return candidate
		}
	}
	token, _ := generateRandomToken(8)
	return (base + "_" + token)[:64]
}

func generateRandomToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
