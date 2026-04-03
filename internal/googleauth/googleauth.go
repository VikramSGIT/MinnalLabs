package googleauth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
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
)

var sanitizeRe = regexp.MustCompile(`[^A-Za-z0-9_]`)

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

// SetupGoogleAuthRoutes registers the production Google Sign-In routes.
func SetupGoogleAuthRoutes(r *gin.Engine, cfg *config.Config) {
	oc := oauthConfig(cfg)

	r.GET("/auth/google/login", func(c *gin.Context) {
		if cfg.GoogleAuth.ClientID == "" || cfg.GoogleAuth.ClientSecret == "" {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Google Sign-In is not configured"})
			return
		}

		stateToken, err := generateRandomToken(32)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate state"})
			return
		}

		rdb := state.RedisClient()
		rdb.Set(context.Background(), csrfStateKey(stateToken), "1", 10*time.Minute)

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

		rdb := state.RedisClient()
		val, err := rdb.GetDel(context.Background(), csrfStateKey(stateToken)).Result()
		if err != nil || val == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid or expired state"})
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
	})
}

type googleUserInfo struct {
	Sub   string `json:"sub"`
	Email string `json:"email"`
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
	// 1. Look up by google_sub
	var existing localmodels.User
	if err := db.DB.Where("google_sub = ?", googleSub).First(&existing).Error; err == nil {
		return &existing, nil
	}

	// 2. Derive username from email
	username := deriveUsername(email)

	// 3. Check if username exists with empty google_sub — link it
	var byUsername localmodels.User
	if err := db.DB.Where("username = ?", username).First(&byUsername).Error; err == nil {
		if byUsername.GoogleSub == "" {
			byUsername.GoogleSub = googleSub
			db.DB.Save(&byUsername)
			return &byUsername, nil
		}
		// Username taken with different google_sub — append suffix
		username = findAvailableUsername(username)
	}

	// 4. Create new user
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
	// Pad if too short
	for len(username) < 3 {
		username += "_"
	}
	// Truncate if too long
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
	// Fallback: use a random suffix
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
