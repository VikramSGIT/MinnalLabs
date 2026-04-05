package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/iot-backend/internal/config"
	"github.com/iot-backend/internal/state"
)

var (
	kratosPublicURL string
	kratosAdminURL  string
	hydraAdminURL   string
	frontendURL     string
)

// Init initialises the Ory client URLs from application config.
func Init(cfg *config.Config) {
	kratosPublicURL = cfg.Ory.KratosPublicURL
	kratosAdminURL = cfg.Ory.KratosAdminURL
	hydraAdminURL = cfg.Ory.HydraAdminURL
	frontendURL = cfg.Ory.FrontendURL
}

// kratosSession is the subset of the Kratos /sessions/whoami response we need.
type kratosSession struct {
	Active   bool `json:"active"`
	Identity struct {
		ID     string `json:"id"`
		Traits struct {
			Username string `json:"username"`
			Email    string `json:"email"`
		} `json:"traits"`
		VerifiableAddresses []struct {
			Value    string `json:"value"`
			Via      string `json:"via"`
			Verified bool   `json:"verified"`
		} `json:"verifiable_addresses"`
	} `json:"identity"`
}

// isEmailVerified checks whether any email address on the identity has been verified.
func isEmailVerified(sess *kratosSession) bool {
	for _, addr := range sess.Identity.VerifiableAddresses {
		if addr.Via == "email" && addr.Verified {
			return true
		}
	}
	return len(sess.Identity.VerifiableAddresses) == 0
}

// checkIdentifierAvailable queries Kratos admin API to see if a credential identifier is already taken.
func checkIdentifierAvailable(ctx context.Context, identifier string) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		kratosAdminURL+"/admin/identities?credentials_identifier="+url.QueryEscape(identifier), nil)
	if err != nil {
		return false, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("kratos admin returned %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, err
	}

	var identities []json.RawMessage
	if err := json.Unmarshal(body, &identities); err != nil {
		return false, err
	}
	return len(identities) == 0, nil
}

// sessionCacheKey returns a Valkey key for caching validated Kratos sessions.
func sessionCacheKey(cookie string) string {
	sum := sha256.Sum256([]byte(cookie))
	return "kratos_session:" + hex.EncodeToString(sum[:])
}

// cachedSessionInfo stores the resolved app-level user information in Valkey.
type cachedSessionInfo struct {
	UserID   uint   `json:"user_id"`
	Username string `json:"username"`
	IsAdmin  bool   `json:"is_admin"`
}

const sessionCacheTTL = 60 * time.Second

// lookupKratosSession calls Kratos /sessions/whoami to validate the session cookie.
func lookupKratosSession(ctx context.Context, cookie string) (*kratosSession, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, kratosPublicURL+"/sessions/whoami", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Cookie", "ory_kratos_session="+cookie)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("kratos returned %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var sess kratosSession
	if err := json.Unmarshal(body, &sess); err != nil {
		return nil, err
	}
	if !sess.Active || sess.Identity.ID == "" {
		return nil, fmt.Errorf("session not active")
	}
	return &sess, nil
}

// kratosIdentity is the subset of the Kratos admin GET /identities/{id} response we need.
type kratosIdentity struct {
	ID     string `json:"id"`
	Traits struct {
		Username string `json:"username"`
		Email    string `json:"email"`
		Name     string `json:"name"`
	} `json:"traits"`
}

// getKratosIdentity calls the Kratos admin API to fetch an identity by ID.
func getKratosIdentity(ctx context.Context, kratosID string) (*kratosIdentity, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, kratosAdminURL+"/admin/identities/"+url.PathEscape(kratosID), nil)
	if err != nil {
		return nil, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("kratos admin returned %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var identity kratosIdentity
	if err := json.Unmarshal(body, &identity); err != nil {
		return nil, err
	}
	return &identity, nil
}

// introspectResult is the subset of the Hydra introspection response we need.
type introspectResult struct {
	Active   bool                   `json:"active"`
	ClientID string                 `json:"client_id"`
	Sub      string                 `json:"sub"`
	Ext      map[string]interface{} `json:"ext"`
}

// introspectToken calls Hydra admin to introspect an opaque access token.
func introspectToken(ctx context.Context, token string) (*introspectResult, error) {
	form := url.Values{"token": {token}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		hydraAdminURL+"/admin/oauth2/introspect",
		strings.NewReader(form.Encode()),
	)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("hydra introspect returned %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result introspectResult
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// --- Hydra Admin API helpers for login/consent/logout flows ---

type hydraLoginRequest struct {
	Skip      bool     `json:"skip"`
	Subject   string   `json:"subject"`
	Challenge string   `json:"challenge"`
	Client    struct {
		ClientID string `json:"client_id"`
	} `json:"client"`
	RequestedScope []string `json:"requested_scope"`
}

type hydraConsentRequest struct {
	Challenge              string   `json:"challenge"`
	Subject                string   `json:"subject"`
	RequestedScope         []string `json:"requested_scope"`
	RequestedAccessTokenAudience []string `json:"requested_access_token_audience"`
	Client                 struct {
		ClientID string `json:"client_id"`
	} `json:"client"`
}

type hydraRedirectResponse struct {
	RedirectTo string `json:"redirect_to"`
}

func hydraGet(ctx context.Context, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, hydraAdminURL+path, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("hydra %s returned %d", path, resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

func hydraPut(ctx context.Context, path string, body interface{}) ([]byte, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, hydraAdminURL+path,
		strings.NewReader(string(data)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("hydra PUT %s returned %d", path, resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

func getLoginRequest(ctx context.Context, challenge string) (*hydraLoginRequest, error) {
	body, err := hydraGet(ctx, "/admin/oauth2/auth/requests/login?login_challenge="+url.QueryEscape(challenge))
	if err != nil {
		return nil, err
	}
	var result hydraLoginRequest
	return &result, json.Unmarshal(body, &result)
}

func acceptLogin(ctx context.Context, challenge, subject string) (string, error) {
	body, err := hydraPut(ctx, "/admin/oauth2/auth/requests/login/accept?login_challenge="+url.QueryEscape(challenge),
		map[string]interface{}{
			"subject":      subject,
			"remember":     true,
			"remember_for": 3600,
		})
	if err != nil {
		return "", err
	}
	var resp hydraRedirectResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", err
	}
	return resp.RedirectTo, nil
}

func getConsentRequest(ctx context.Context, challenge string) (*hydraConsentRequest, error) {
	body, err := hydraGet(ctx, "/admin/oauth2/auth/requests/consent?consent_challenge="+url.QueryEscape(challenge))
	if err != nil {
		return nil, err
	}
	var result hydraConsentRequest
	return &result, json.Unmarshal(body, &result)
}

func acceptConsent(ctx context.Context, challenge string, grantScope []string, audience []string, appUserID string, kratosID string) (string, error) {
	body, err := hydraPut(ctx, "/admin/oauth2/auth/requests/consent/accept?consent_challenge="+url.QueryEscape(challenge),
		map[string]interface{}{
			"grant_scope":                grantScope,
			"grant_access_token_audience": audience,
			"remember":                   true,
			"remember_for":               3600,
			"session": map[string]interface{}{
				"access_token": map[string]interface{}{
					"app_user_id":        appUserID,
					"kratos_identity_id": kratosID,
				},
			},
		})
	if err != nil {
		return "", err
	}
	var resp hydraRedirectResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", err
	}
	return resp.RedirectTo, nil
}

func rejectConsent(ctx context.Context, challenge, reason string) (string, error) {
	body, err := hydraPut(ctx, "/admin/oauth2/auth/requests/consent/reject?consent_challenge="+url.QueryEscape(challenge),
		map[string]interface{}{
			"error":             "access_denied",
			"error_description": reason,
		})
	if err != nil {
		return "", err
	}
	var resp hydraRedirectResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", err
	}
	return resp.RedirectTo, nil
}

func acceptLogout(ctx context.Context, challenge string) (string, error) {
	body, err := hydraPut(ctx, "/admin/oauth2/auth/requests/logout/accept?logout_challenge="+url.QueryEscape(challenge),
		map[string]interface{}{})
	if err != nil {
		return "", err
	}
	var resp hydraRedirectResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", err
	}
	return resp.RedirectTo, nil
}

// cacheSession stores a resolved session in Valkey.
func cacheSession(cookie string, info cachedSessionInfo) {
	data, err := json.Marshal(info)
	if err != nil {
		return
	}
	rdb := state.RedisClient()
	rdb.Set(context.Background(), sessionCacheKey(cookie), string(data), sessionCacheTTL)
}

// getCachedSession retrieves a cached session from Valkey. Returns false on miss.
func getCachedSession(cookie string) (cachedSessionInfo, bool) {
	rdb := state.RedisClient()
	val, err := rdb.Get(context.Background(), sessionCacheKey(cookie)).Result()
	if err != nil {
		return cachedSessionInfo{}, false
	}
	var info cachedSessionInfo
	if err := json.Unmarshal([]byte(val), &info); err != nil {
		return cachedSessionInfo{}, false
	}
	return info, true
}
