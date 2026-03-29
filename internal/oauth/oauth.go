package oauth

import (
	"fmt"
	"log"
	"net/http"
	"net/url"

	"github.com/gin-gonic/gin"
	oautherrors "github.com/go-oauth2/oauth2/v4/errors"
	"github.com/go-oauth2/oauth2/v4/manage"
	"github.com/go-oauth2/oauth2/v4/models"
	"github.com/go-oauth2/oauth2/v4/server"
	"github.com/go-oauth2/oauth2/v4/store"
	"github.com/iot-backend/internal/config"
	"github.com/iot-backend/internal/db"
	localmodels "github.com/iot-backend/internal/models"
	"golang.org/x/crypto/bcrypt"
)

var Srv *server.Server

func InitOAuth() {
	manager := manage.NewDefaultManager()
	manager.SetAuthorizeCodeTokenCfg(manage.DefaultAuthorizeCodeTokenCfg)

	// In memory token store for simplicity.
	// For production, consider using a database store like go-oauth2-gorm
	manager.MustTokenStorage(store.NewMemoryTokenStore())

	// Client store
	clientStore := store.NewClientStore()

	// Load clients from our DB into the in-memory client store
	var dbClients []localmodels.OAuthClient
	db.DB.Find(&dbClients)

	for _, c := range dbClients {
		clientStore.Set(c.ID, &models.Client{
			ID:     c.ID,
			Secret: c.Secret,
			Domain: c.Domain,
			UserID: c.UserID,
		})
	}

	// Create a default client for testing if none exist
	if len(dbClients) == 0 {
		defaultClient := localmodels.OAuthClient{
			ID:     "google-alexa-client",
			Secret: "my-secret-key",
			Domain: "https://oauth-redirect.googleusercontent.com/",
			UserID: "1",
		}
		db.DB.Create(&defaultClient)

		clientStore.Set(defaultClient.ID, &models.Client{
			ID:     defaultClient.ID,
			Secret: defaultClient.Secret,
			Domain: defaultClient.Domain,
			UserID: defaultClient.UserID,
		})
		log.Println("Created default OAuth client: google-alexa-client")
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
		// User not logged in, redirect to login page
		http.SetCookie(w, expiredSessionCookie())

		// Encode the original request URL
		originalURL := r.URL.String()
		loginURL := "/login?redirect=" + url.QueryEscape(originalURL)

		http.Redirect(w, r, loginURL, http.StatusFound)
		return "", nil
	}

	http.SetCookie(w, sessionCookie(sessionUser.Token))
	return fmt.Sprintf("%d", sessionUser.UserID), nil
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

	// Show Login Page
	r.GET("/login", func(c *gin.Context) {
		redirect := c.Query("redirect")

		html := `
		<html>
			<head><title>Login to IoT Backend</title></head>
			<body>
				<h2>Login</h2>
				<form action="/login" method="POST">
					<input type="hidden" name="redirect" value="` + redirect + `">
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

		user, err := authenticateUser(username, password)
		if err == nil {
			if err := issueSession(c, user); err != nil {
				c.String(http.StatusInternalServerError, "Failed to create session")
				return
			}
			c.Redirect(http.StatusFound, redirect)
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
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}

			user, err := authenticateUser(req.Username, req.Password)
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
		protected.POST("/logout", func(c *gin.Context) {
			destroySession(c)
			c.JSON(http.StatusOK, gin.H{"ok": true})
		})
	}

	// OAuth2 Endpoints
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
