package oauth

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"html"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	oautherrors "github.com/go-oauth2/oauth2/v4/errors"
	"github.com/go-oauth2/oauth2/v4/manage"
	"github.com/go-oauth2/oauth2/v4/models"
	"github.com/go-oauth2/oauth2/v4/server"
	"github.com/go-oauth2/oauth2/v4/store"
	"github.com/iot-backend/internal/config"
	"github.com/iot-backend/internal/deletion"
	"github.com/iot-backend/internal/db"
	localmodels "github.com/iot-backend/internal/models"
	"github.com/iot-backend/internal/state"
	"github.com/iot-backend/internal/validation"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

var Srv *server.Server

const (
	defaultOAuthClientDomain = "https://oauth-redirect.googleusercontent.com/"
	legacyOAuthClientSecret  = "my-secret-key"
)

func InitOAuth(cfg *config.Config) {
	manager := manage.NewDefaultManager()
	manager.SetAuthorizeCodeTokenCfg(manage.DefaultAuthorizeCodeTokenCfg)
	manager.MustTokenStorage(NewPostgresTokenStore(state.RedisClient(), db.DB))

	clientStore := store.NewClientStore()

	if err := ensureOAuthClient(cfg); err != nil {
		log.Fatalf("Failed to initialize OAuth clients: %v", err)
	}

	var dbClients []localmodels.OAuthClient
	if err := db.DB.Find(&dbClients).Error; err != nil {
		log.Fatalf("Failed to load OAuth clients: %v", err)
	}

	for _, c := range dbClients {
		clientStore.Set(c.ID, &models.Client{
			ID:     c.ID,
			Secret: c.Secret,
			Domain: c.Domain,
			UserID: c.UserID,
		})
	}

	manager.MapClientStorage(clientStore)

	Srv = server.NewServer(server.NewConfig(), manager)
	Srv.SetUserAuthorizationHandler(userAuthorizeHandler)
	Srv.SetInternalErrorHandler(func(err error) (re *oautherrors.Response) {
		log.Println("Internal Error:", err.Error())
		return
	})
	Srv.SetResponseErrorHandler(func(re *oautherrors.Response) {
		log.Println("Response Error:", re.Error.Error())
	})
}

// userAuthorizeHandler handles the user authorization logic
func userAuthorizeHandler(w http.ResponseWriter, r *http.Request) (userID string, err error) {
	sessionUser, ok := restoreSessionFromRequest(r)
	if !ok {
		http.SetCookie(w, expiredSessionCookie())
		loginURL := "/login?redirect=" + url.QueryEscape(r.URL.String())
		http.Redirect(w, r, loginURL, http.StatusFound)
		return "", nil
	}

	http.SetCookie(w, sessionCookie(sessionUser.Token))
	return fmt.Sprintf("%d", sessionUser.UserID), nil
}

func sanitizeRedirect(target string) string {
	target = strings.TrimSpace(target)
	if target == "" || !strings.HasPrefix(target, "/") || strings.HasPrefix(target, "//") {
		return "/"
	}
	return target
}

func randomOAuthSecret(bytes int) (string, error) {
	buf := make([]byte, bytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func defaultOAuthClientID(cfg *config.Config) string {
	if cfg == nil {
		return "google-client"
	}

	clientID := strings.TrimSpace(cfg.OAuth.ClientID)
	if clientID == "" {
		return "google-client"
	}
	return clientID
}

func ensureOAuthClient(cfg *config.Config) error {
	clientID := defaultOAuthClientID(cfg)
	configuredSecret := ""
	if cfg != nil {
		configuredSecret = strings.TrimSpace(cfg.OAuth.ClientSecret)
	}

	var client localmodels.OAuthClient
	err := db.DB.First(&client, "id = ?", clientID).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}

	if errors.Is(err, gorm.ErrRecordNotFound) {
		secret := configuredSecret
		generated := false
		if secret == "" {
			var secretErr error
			secret, secretErr = randomOAuthSecret(32)
			if secretErr != nil {
				return secretErr
			}
			generated = true
		}

		client = localmodels.OAuthClient{
			ID:     clientID,
			Secret: secret,
			Domain: defaultOAuthClientDomain,
			UserID: "1",
		}
		if err := db.DB.Create(&client).Error; err != nil {
			return err
		}

		if generated {
			log.Printf("Created default OAuth client %q with generated secret", clientID)
		} else {
			log.Printf("Created default OAuth client %q from configuration", clientID)
		}
		return nil
	}

	currentSecret := strings.TrimSpace(client.Secret)
	if currentSecret != "" && currentSecret != legacyOAuthClientSecret {
		return nil
	}

	secret := configuredSecret
	rotatedFromConfig := secret != ""
	if secret == "" {
		var secretErr error
		secret, secretErr = randomOAuthSecret(32)
		if secretErr != nil {
			return secretErr
		}
	}

	if err := db.DB.Model(&localmodels.OAuthClient{}).
		Where("id = ?", client.ID).
		Update("secret", secret).Error; err != nil {
		return err
	}

	if rotatedFromConfig {
		log.Printf("Rotated legacy OAuth client secret for %q from configuration", client.ID)
	} else {
		log.Printf("Rotated legacy OAuth client secret for %q with a generated secret", client.ID)
	}
	return nil
}

func validateLoginInput(username, password string) (string, error) {
	username = validation.NormalizeUsername(username)
	if err := validation.ValidateUsername(username); err != nil {
		return "", err
	}
	if err := validation.ValidatePassword(password); err != nil {
		return "", err
	}
	return username, nil
}

func authenticateUser(username, password string) (*localmodels.User, error) {
	var user localmodels.User
	if err := db.DB.Where("username = ?", username).First(&user).Error; err != nil {
		return nil, fmt.Errorf("invalid credentials")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(password)); err != nil {
		return nil, fmt.Errorf("invalid credentials")
	}
	return &user, nil
}

func authResponse(user *localmodels.User) gin.H {
	return gin.H{
		"user_id":  user.ID,
		"username": user.Username,
		"is_admin": IsAdminUser(user.ID),
	}
}

// SetupOAuthRoutes adds OAuth endpoints to the Gin router
func SetupOAuthRoutes(r *gin.Engine, cfg *config.Config) {
	initSessionConfig(cfg)

	r.GET("/login", func(c *gin.Context) {
		redirect := c.Query("redirect")
		html := `
		<html>
			<head><title>Login to IoT Backend</title></head>
			<body>
				<h2>Login</h2>
				<form action="/login" method="POST">
					<input type="hidden" name="redirect" value="` + html.EscapeString(redirect) + `">
					Username: <input type="text" name="username"><br>
					Password: <input type="password" name="password"><br>
					<input type="submit" value="Login">
				</form>
			</body>
		</html>
		`
		c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(html))
	})

	r.POST("/login", func(c *gin.Context) {
		username := c.PostForm("username")
		password := c.PostForm("password")
		redirect := c.PostForm("redirect")

		username, err := validateLoginInput(username, password)
		if err != nil {
			c.String(http.StatusBadRequest, err.Error())
			return
		}

		user, err := authenticateUser(username, password)
		if err == nil {
			if err := issueSession(c, user); err != nil {
				c.String(http.StatusInternalServerError, "Failed to create session")
				return
			}
			c.Redirect(http.StatusFound, sanitizeRedirect(redirect))
			return
		}

		c.String(http.StatusUnauthorized, "Invalid credentials")
	})

	api := r.Group("/api/session")
	{
		api.POST("/login", func(c *gin.Context) {
			var req struct {
				Username string `json:"username" binding:"required"`
				Password string `json:"password" binding:"required"`
			}
			if err := c.ShouldBindJSON(&req); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
				return
			}

			username, err := validateLoginInput(req.Username, req.Password)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}

			user, err := authenticateUser(username, req.Password)
			if err != nil {
				c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
				return
			}

			if err := issueSession(c, user); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create session"})
				return
			}

			c.JSON(http.StatusOK, authResponse(user))
		})

		protected := api.Group("")
		protected.Use(RequireSession())
		protected.GET("/me", func(c *gin.Context) {
			sessionUser, ok := CurrentSessionUser(c)
			if !ok {
				c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
				return
			}

			c.JSON(http.StatusOK, gin.H{
				"user_id":  sessionUser.UserID,
				"username": sessionUser.Username,
				"is_admin": sessionUser.IsAdmin,
			})
		})
		protected.DELETE("/me", func(c *gin.Context) {
			sessionUser, ok := CurrentSessionUser(c)
			if !ok {
				c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
				return
			}

			result, err := deletion.ScheduleUserDeletion(db.DB, sessionUser.UserID, time.Now().UTC())
			if err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					DestroyCurrentSession(c)
					c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
					return
				}
				log.Printf("Failed to delete user %d: %v", sessionUser.UserID, err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete user"})
				return
			}

			state.DeleteSessionsForUser(sessionUser.UserID)
			DestroyCurrentSession(c)
			if err := PurgeTokensForUser(fmt.Sprintf("%d", sessionUser.UserID)); err != nil {
				log.Printf("Failed to purge oauth tokens for deleted user %d: %v", sessionUser.UserID, err)
			}

			c.JSON(http.StatusOK, gin.H{
				"deleted":         true,
				"user_id":         result.UserID,
				"queued_home_ids": result.HomeIDs,
			})
		})
		protected.POST("/logout", func(c *gin.Context) {
			destroySession(c)
			c.JSON(http.StatusOK, gin.H{"ok": true})
		})
	}

	r.Any("/oauth/authorize", func(c *gin.Context) {
		err := Srv.HandleAuthorizeRequest(c.Writer, c.Request)
		if err != nil {
			c.AbortWithError(http.StatusBadRequest, err)
		}
	})

	r.Any("/oauth/token", func(c *gin.Context) {
		err := Srv.HandleTokenRequest(c.Writer, c.Request)
		if err != nil {
			c.AbortWithError(http.StatusInternalServerError, err)
		}
	})
}
