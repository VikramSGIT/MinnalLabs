package oauth

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-oauth2/oauth2/v4/errors"
	"github.com/go-oauth2/oauth2/v4/manage"
	"github.com/go-oauth2/oauth2/v4/models"
	"github.com/go-oauth2/oauth2/v4/server"
	"github.com/go-oauth2/oauth2/v4/store"
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
	
	Srv.SetInternalErrorHandler(func(err error) (re *errors.Response) {
		log.Println("Internal Error:", err.Error())
		return
	})
	
	Srv.SetResponseErrorHandler(func(re *errors.Response) {
		log.Println("Response Error:", re.Error.Error())
	})
}

// userAuthorizeHandler handles the user authorization logic
func userAuthorizeHandler(w http.ResponseWriter, r *http.Request) (userID string, err error) {
	// Simple session check (using cookies)
	cookie, err := r.Cookie("user_session")
	if err != nil {
		// User not logged in, redirect to login page
		
		// Encode the original request URL
		originalURL := r.URL.String()
		loginURL := "/login?redirect=" + url.QueryEscape(originalURL)
		
		http.Redirect(w, r, loginURL, http.StatusFound)
		return "", nil
	}
	
	// User is logged in
	return cookie.Value, nil
}

// SetupOAuthRoutes adds OAuth endpoints to the Gin router
func SetupOAuthRoutes(r *gin.Engine) {
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

		var user localmodels.User
		if err := db.DB.Where("username = ?", username).First(&user).Error; err == nil {
			if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(password)); err == nil {
				cookie := &http.Cookie{
					Name:     "user_session",
					Value:    fmt.Sprintf("%d", user.ID),
					Path:     "/",
					Expires:  time.Now().Add(24 * time.Hour),
					HttpOnly: true,
				}
				http.SetCookie(c.Writer, cookie)
				c.Redirect(http.StatusFound, redirect)
				return
			}
		}

		c.String(http.StatusUnauthorized, "Invalid credentials")
	})

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
