package oauth

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

const oauthPrincipalContextKey = "oauth_principal"

type OAuthPrincipal struct {
	UserID    uint
	RawUserID string
	ClientID  string
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

func RequireOAuthToken() gin.HandlerFunc {
	return func(c *gin.Context) {
		if Srv == nil {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "oauth server not initialized"})
			return
		}

		token, ok := bearerTokenFromHeader(c.GetHeader("Authorization"))
		if !ok {
			c.Header("WWW-Authenticate", `Bearer realm="google-fulfillment"`)
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid access token"})
			return
		}

		tokenInfo, err := Srv.Manager.LoadAccessToken(c.Request.Context(), token)
		if err != nil {
			c.Header("WWW-Authenticate", `Bearer realm="google-fulfillment"`)
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid access token"})
			return
		}

		rawUserID := strings.TrimSpace(tokenInfo.GetUserID())
		if rawUserID == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid access token"})
			return
		}

		parsedUserID, err := strconv.ParseUint(rawUserID, 10, 64)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid access token"})
			return
		}

		c.Set(oauthPrincipalContextKey, OAuthPrincipal{
			UserID:    uint(parsedUserID),
			RawUserID: rawUserID,
			ClientID:  tokenInfo.GetClientID(),
		})
		c.Next()
	}
}

func CurrentOAuthPrincipal(c *gin.Context) (OAuthPrincipal, bool) {
	value, ok := c.Get(oauthPrincipalContextKey)
	if !ok {
		return OAuthPrincipal{}, false
	}

	principal, ok := value.(OAuthPrincipal)
	return principal, ok
}
