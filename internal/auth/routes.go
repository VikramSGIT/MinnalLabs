package auth

import (
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/iot-backend/internal/db"
	"github.com/iot-backend/internal/models"
)

// SetupRoutes registers internal auth-related endpoints.
func SetupRoutes(r *gin.Engine) {
	internal := r.Group("/internal")
	{
		internal.POST("/hooks/registration", handleRegistrationHook)
	}

	// Session info endpoint.
	api := r.Group("/api/session")
	{
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
	}

	// OAuth login/consent/logout handlers (called by Hydra).
	oauth := r.Group("/oauth")
	{
		oauth.GET("/login", handleOAuthLogin)
		oauth.GET("/consent", handleOAuthConsentPage)
		oauth.POST("/consent", handleOAuthConsentDecision)
		oauth.GET("/logout", handleOAuthLogout)
	}
}

// handleOAuthLogin handles the Hydra login challenge.
// If the user has a valid Kratos session, it accepts the login.
// Otherwise it redirects to the frontend login page.
func handleOAuthLogin(c *gin.Context) {
	challenge := c.Query("login_challenge")
	if challenge == "" {
		c.String(http.StatusBadRequest, "missing login_challenge")
		return
	}

	ctx := c.Request.Context()

	loginReq, err := getLoginRequest(ctx, challenge)
	if err != nil {
		log.Printf("error getting login request: %v", err)
		c.String(http.StatusInternalServerError, "failed to get login request")
		return
	}

	// Already authenticated — skip.
	if loginReq.Skip {
		redirectTo, err := acceptLogin(ctx, challenge, loginReq.Subject)
		if err != nil {
			log.Printf("error accepting login: %v", err)
			c.String(http.StatusInternalServerError, "failed to accept login")
			return
		}
		c.Redirect(http.StatusFound, redirectTo)
		return
	}

	// Check for a valid Kratos session.
	cookie := extractKratosCookie(c.Request)
	if cookie == "" {
		redirectToLogin(c, challenge)
		return
	}

	sess, err := lookupKratosSession(ctx, cookie)
	if err != nil {
		redirectToLogin(c, challenge)
		return
	}

	redirectTo, err := acceptLogin(ctx, challenge, sess.Identity.ID)
	if err != nil {
		log.Printf("error accepting login: %v", err)
		c.String(http.StatusInternalServerError, "failed to accept login")
		return
	}
	c.Redirect(http.StatusFound, redirectTo)
}

func redirectToLogin(c *gin.Context, challenge string) {
	selfURL := fmt.Sprintf("%s/oauth/login?login_challenge=%s", backendPublicURL(c.Request), challenge)
	loginURL := fmt.Sprintf("%s/login?return_to=%s", frontendURL, selfURL)
	c.Redirect(http.StatusFound, loginURL)
}

func backendPublicURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

// handleOAuthConsentPage renders a consent page showing what the client is requesting.
func handleOAuthConsentPage(c *gin.Context) {
	challenge := c.Query("consent_challenge")
	if challenge == "" {
		c.String(http.StatusBadRequest, "missing consent_challenge")
		return
	}

	ctx := c.Request.Context()

	consentReq, err := getConsentRequest(ctx, challenge)
	if err != nil {
		log.Printf("error getting consent request: %v", err)
		c.String(http.StatusInternalServerError, "failed to get consent request")
		return
	}

	// Look up the app user.
	var user models.User
	if err := db.DB.Where("kratos_identity_id = ?", consentReq.Subject).First(&user).Error; err != nil {
		log.Printf("user not found for kratos_identity_id=%s: %v", consentReq.Subject, err)
		c.String(http.StatusForbidden, "user not found")
		return
	}

	// Render the consent page.
	data := consentPageData{
		Challenge:  challenge,
		ClientID:   consentReq.Client.ClientID,
		Scopes:     consentReq.RequestedScope,
		Username:   user.Username,
	}

	c.Header("Content-Type", "text/html; charset=utf-8")
	if err := consentTmpl.Execute(c.Writer, data); err != nil {
		log.Printf("error rendering consent page: %v", err)
	}
}

// handleOAuthConsentDecision processes the user's Allow/Deny decision.
func handleOAuthConsentDecision(c *gin.Context) {
	challenge := c.PostForm("consent_challenge")
	if challenge == "" {
		c.String(http.StatusBadRequest, "missing consent_challenge")
		return
	}

	ctx := c.Request.Context()
	decision := c.PostForm("decision")

	if decision == "deny" {
		redirectTo, err := rejectConsent(ctx, challenge, "The user denied the request")
		if err != nil {
			log.Printf("error rejecting consent: %v", err)
			c.String(http.StatusInternalServerError, "failed to reject consent")
			return
		}
		c.Redirect(http.StatusFound, redirectTo)
		return
	}

	// Accept — look up consent request and user.
	consentReq, err := getConsentRequest(ctx, challenge)
	if err != nil {
		log.Printf("error getting consent request: %v", err)
		c.String(http.StatusInternalServerError, "failed to get consent request")
		return
	}

	var user models.User
	if err := db.DB.Where("kratos_identity_id = ?", consentReq.Subject).First(&user).Error; err != nil {
		log.Printf("user not found for kratos_identity_id=%s: %v", consentReq.Subject, err)
		c.String(http.StatusForbidden, "user not found")
		return
	}

	redirectTo, err := acceptConsent(ctx, challenge,
		consentReq.RequestedScope,
		consentReq.RequestedAccessTokenAudience,
		strconv.FormatUint(uint64(user.ID), 10),
		consentReq.Subject,
	)
	if err != nil {
		log.Printf("error accepting consent: %v", err)
		c.String(http.StatusInternalServerError, "failed to accept consent")
		return
	}
	c.Redirect(http.StatusFound, redirectTo)
}

// handleOAuthLogout handles the Hydra logout challenge.
func handleOAuthLogout(c *gin.Context) {
	challenge := c.Query("logout_challenge")
	if challenge == "" {
		c.String(http.StatusBadRequest, "missing logout_challenge")
		return
	}

	redirectTo, err := acceptLogout(c.Request.Context(), challenge)
	if err != nil {
		log.Printf("error accepting logout: %v", err)
		c.String(http.StatusInternalServerError, "failed to accept logout")
		return
	}
	c.Redirect(http.StatusFound, redirectTo)
}

// --- Consent page template ---

type consentPageData struct {
	Challenge string
	ClientID  string
	Scopes    []string
	Username  string
}

var consentTmpl = template.Must(template.New("consent").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Authorize Application</title>
  <style>
    *, *::before, *::after { box-sizing: border-box; }
    body {
      font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
      background: #f5f7fa;
      color: #1a1a2e;
      margin: 0;
      display: flex;
      justify-content: center;
      align-items: center;
      min-height: 100vh;
    }
    .card {
      background: #fff;
      border-radius: 12px;
      box-shadow: 0 4px 24px rgba(0,0,0,.08);
      padding: 40px;
      max-width: 440px;
      width: 100%;
    }
    h1 { font-size: 1.4rem; margin: 0 0 8px; }
    .subtitle { color: #666; margin: 0 0 24px; font-size: .95rem; }
    .app-name {
      display: inline-block;
      background: #e8f0fe;
      color: #1a73e8;
      padding: 2px 10px;
      border-radius: 4px;
      font-weight: 600;
    }
    .section-label { font-weight: 600; font-size: .85rem; color: #555; margin: 0 0 8px; text-transform: uppercase; letter-spacing: .5px; }
    .scopes {
      list-style: none;
      padding: 0;
      margin: 0 0 28px;
    }
    .scopes li {
      padding: 10px 14px;
      background: #f8f9fa;
      border-radius: 8px;
      margin-bottom: 6px;
      font-size: .95rem;
    }
    .scopes li::before { content: "✓ "; color: #34a853; font-weight: bold; }
    .user-info {
      background: #f8f9fa;
      border-radius: 8px;
      padding: 12px 14px;
      margin-bottom: 28px;
      font-size: .95rem;
    }
    .user-info span { font-weight: 600; }
    .actions { display: flex; gap: 12px; }
    button {
      flex: 1;
      padding: 12px;
      border: none;
      border-radius: 8px;
      font-size: 1rem;
      font-weight: 600;
      cursor: pointer;
      transition: opacity .15s;
    }
    button:hover { opacity: .85; }
    .btn-allow { background: #1a73e8; color: #fff; }
    .btn-deny { background: #e8eaed; color: #555; }
  </style>
</head>
<body>
  <div class="card">
    <h1>Authorize Application</h1>
    <p class="subtitle">
      <span class="app-name">{{.ClientID}}</span> is requesting access to your account.
    </p>

    <div class="user-info">
      Signed in as <span>{{.Username}}</span>
    </div>

    <p class="section-label">This application will be able to:</p>
    <ul class="scopes">
      {{range .Scopes}}<li>{{.}}</li>{{end}}
      {{if not .Scopes}}<li>Access your basic account information</li>{{end}}
    </ul>

    <form method="POST" action="/oauth/consent">
      <input type="hidden" name="consent_challenge" value="{{.Challenge}}">
      <div class="actions">
        <button type="submit" name="decision" value="deny" class="btn-deny">Deny</button>
        <button type="submit" name="decision" value="allow" class="btn-allow">Allow</button>
      </div>
    </form>
  </div>
</body>
</html>`))
