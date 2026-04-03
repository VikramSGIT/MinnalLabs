package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const deterministicPassword = "StressTestPass123!"

type Runner struct {
	cfg              Config
	state            *PhaseStateStore
	metrics          *MetricCollector
	client           *http.Client
	noRedirectClient *http.Client
	scenario         string
	logMu            sync.Mutex
}

type SessionContext struct {
	User         UserSnapshot
	Username     string
	Password     string
	SessionToken string
}

type requestResult struct {
	StatusCode int
	Header     http.Header
	Body       []byte
	DurationMs float64
	Expected   bool
	Err        error
}

type userEnrollResponse struct {
	UserID int
}

type homeEnrollResponse struct {
	HomeID int
}

type homeListEntry struct {
	HomeID              int    `json:"home_id"`
	Name                string `json:"name"`
	MQTTProvisionState  string `json:"mqtt_provision_state"`
	MQTTProvisionError  string `json:"mqtt_provision_error"`
}

type deviceEnrollResponse struct {
	DeviceID int
}

type deleteHomeResponse struct {
	MQTTProvisionState string `json:"mqtt_provision_state"`
}

type oauthTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

type fulfillmentStep struct {
	Intent  string
	Payload map[string]any
}

func newRunner(cfg Config, state *PhaseStateStore, metrics *MetricCollector) *Runner {
	baseTransport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
	}
	return &Runner{
		cfg:     cfg,
		state:   state,
		metrics: metrics,
		client: &http.Client{
			Timeout:   cfg.HTTPTimeout,
			Transport: baseTransport.Clone(),
		},
		noRedirectClient: &http.Client{
			Timeout:   cfg.HTTPTimeout,
			Transport: baseTransport.Clone(),
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		scenario: cfg.Phase.ScenarioLabel,
	}
}

func (r *Runner) logf(format string, args ...any) {
	r.logMu.Lock()
	defer r.logMu.Unlock()
	fmt.Printf(format+"\n", args...)
}

func (r *Runner) doRequest(ctx context.Context, method, path, name string, expectedStatuses []int, headers map[string]string, body []byte, noRedirect bool) requestResult {
	startedAt := time.Now()
	targetURL := strings.TrimRight(r.cfg.BaseURL, "/") + path
	req, err := http.NewRequestWithContext(ctx, method, targetURL, bytes.NewReader(body))
	if err != nil {
		r.metrics.RecordHTTP(r.scenario, name, startedAt, 0, false, err)
		r.metrics.RecordCheck(r.scenario, fmt.Sprintf("%s returned expected status", name), false)
		return requestResult{Err: err}
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	client := r.client
	if noRedirect {
		client = r.noRedirectClient
	}
	resp, err := client.Do(req)
	if err != nil {
		durationMs := time.Since(startedAt).Seconds() * 1000
		r.metrics.RecordHTTP(r.scenario, name, startedAt, 0, false, err)
		r.metrics.RecordCheck(r.scenario, fmt.Sprintf("%s returned expected status", name), false)
		return requestResult{Err: err, DurationMs: durationMs}
	}
	defer resp.Body.Close()

	responseBody, readErr := io.ReadAll(resp.Body)
	durationMs := time.Since(startedAt).Seconds() * 1000
	result := requestResult{
		StatusCode: resp.StatusCode,
		Header:     resp.Header.Clone(),
		Body:       responseBody,
		DurationMs: durationMs,
	}
	if readErr != nil {
		result.Err = readErr
	}
	result.Expected = statusAllowed(resp.StatusCode, expectedStatuses) && result.Err == nil
	r.metrics.RecordHTTP(r.scenario, name, startedAt, resp.StatusCode, result.Expected, result.Err)
	r.metrics.RecordCheck(r.scenario, fmt.Sprintf("%s returned expected status", name), result.Expected)
	return result
}

func (r *Runner) parseJSON(result requestResult, target any) bool {
	if result.Err != nil {
		return false
	}
	if err := json.Unmarshal(result.Body, target); err != nil {
		r.logf("invalid JSON for phase %s: %v", r.cfg.Phase.Name, err)
		return false
	}
	return true
}

func (r *Runner) registerUser(ctx context.Context, credentials Credentials) (userEnrollResponse, bool) {
	payload := map[string]string{
		"username": credentials.Username,
		"password": credentials.Password,
	}
	body, _ := json.Marshal(payload)
	result := r.doRequest(ctx, http.MethodPost, "/api/enroll/user", "POST /api/enroll/user", []int{201}, jsonHeaders(nil), body, false)
	r.metrics.RecordTrend("api_enroll_user_duration", result.DurationMs, r.scenario)
	if !result.Expected {
		return userEnrollResponse{}, false
	}
	var parsed map[string]any
	if !r.parseJSON(result, &parsed) {
		return userEnrollResponse{}, false
	}
	userID, ok := intFromAny(parsed["user_id"])
	return userEnrollResponse{UserID: userID}, ok
}

func (r *Runner) loginSession(ctx context.Context, username, password string, expectedStatuses ...int) (requestResult, string) {
	if len(expectedStatuses) == 0 {
		expectedStatuses = []int{200}
	}
	payload := map[string]string{
		"username": username,
		"password": password,
	}
	body, _ := json.Marshal(payload)
	result := r.doRequest(ctx, http.MethodPost, "/api/session/login", "POST /api/session/login", expectedStatuses, jsonHeaders(nil), body, false)
	r.metrics.RecordTrend("api_session_login_duration", result.DurationMs, r.scenario)
	if !result.Expected {
		return result, ""
	}
	if result.StatusCode != http.StatusOK {
		return result, ""
	}
	token := sessionTokenFromHeaders(result.Header, r.cfg.SessionCookieName)
	if token == "" {
		r.metrics.RecordCheck(r.scenario, "POST /api/session/login returned a session cookie", false)
		return result, ""
	}
	r.metrics.RecordCheck(r.scenario, "POST /api/session/login returned a session cookie", true)
	return result, token
}

func (r *Runner) logoutSession(ctx context.Context, sessionToken string, allowUnauthorized bool) bool {
	expected := []int{200}
	if allowUnauthorized {
		expected = []int{200, 401}
	}
	result := r.doRequest(ctx, http.MethodPost, "/api/session/logout", "POST /api/session/logout", expected, cookieHeaders(r.cfg.SessionCookieName, sessionToken, nil), nil, false)
	r.metrics.RecordTrend("api_session_logout_duration", result.DurationMs, r.scenario)
	return result.Expected
}

func (r *Runner) deleteCurrentUser(ctx context.Context, sessionToken string) bool {
	result := r.doRequest(ctx, http.MethodDelete, "/api/session/me", "DELETE /api/session/me", []int{200}, cookieHeaders(r.cfg.SessionCookieName, sessionToken, nil), nil, false)
	r.metrics.RecordTrend("api_session_delete_me_duration", result.DurationMs, r.scenario)
	return result.Expected
}

func (r *Runner) deleteUserAsAdmin(ctx context.Context, sessionToken string, userID int) bool {
	result := r.doRequest(ctx, http.MethodDelete, fmt.Sprintf("/api/admin/users/%d", userID), "DELETE /api/admin/users/:userID", []int{200}, cookieHeaders(r.cfg.SessionCookieName, sessionToken, nil), nil, false)
	r.metrics.RecordTrend("api_admin_delete_user_duration", result.DurationMs, r.scenario)
	return result.Expected
}

func (r *Runner) enrollHome(ctx context.Context, sessionToken string, userIndex, homeSlot int) (homeEnrollResponse, time.Time, bool) {
	payload := map[string]any{
		"name":          homeName(r.cfg.RunID, userIndex, homeSlot),
		"wifi_ssid":     fmt.Sprintf("vegeta-ssid-%d", userIndex),
		"wifi_password": "vegeta-password",
	}
	body, _ := json.Marshal(payload)
	startedAt := time.Now()
	result := r.doRequest(ctx, http.MethodPost, "/api/enroll/home", "POST /api/enroll/home", []int{201}, cookieAndJSONHeaders(r.cfg.SessionCookieName, sessionToken, nil), body, false)
	r.metrics.RecordTrend("api_enroll_home_duration", result.DurationMs, r.scenario)
	if !result.Expected {
		return homeEnrollResponse{}, startedAt, false
	}
	var parsed map[string]any
	if !r.parseJSON(result, &parsed) {
		return homeEnrollResponse{}, startedAt, false
	}
	homeID, ok := intFromAny(parsed["home_id"])
	return homeEnrollResponse{HomeID: homeID}, startedAt, ok
}

func (r *Runner) listHomes(ctx context.Context, sessionToken string) ([]homeListEntry, bool) {
	result := r.doRequest(ctx, http.MethodGet, "/api/enroll/homes", "GET /api/enroll/homes", []int{200}, cookieHeaders(r.cfg.SessionCookieName, sessionToken, nil), nil, false)
	r.metrics.RecordTrend("api_enroll_homes_duration", result.DurationMs, r.scenario)
	if !result.Expected {
		return nil, false
	}
	var parsed []homeListEntry
	if !r.parseJSON(result, &parsed) {
		return nil, false
	}
	return parsed, true
}

func (r *Runner) enrollDevice(ctx context.Context, sessionToken string, homeID int, product ProductInfo, userIndex, homeSlot, deviceSlot int) (deviceEnrollResponse, bool) {
	payload := map[string]any{
		"home_id":           homeID,
		"name":              deviceName(r.cfg.RunID, userIndex, homeSlot, deviceSlot),
		"product_id":        product.ProductID,
		"product_name":      product.Name,
		"device_public_key": r.cfg.DevicePublicKey,
	}
	body, _ := json.Marshal(payload)
	result := r.doRequest(ctx, http.MethodPost, "/api/enroll/device", "POST /api/enroll/device", []int{201}, cookieAndJSONHeaders(r.cfg.SessionCookieName, sessionToken, nil), body, false)
	r.metrics.RecordTrend("api_enroll_device_duration", result.DurationMs, r.scenario)
	if !result.Expected {
		return deviceEnrollResponse{}, false
	}
	var parsed map[string]any
	if !r.parseJSON(result, &parsed) {
		return deviceEnrollResponse{}, false
	}
	deviceID, ok := intFromAny(parsed["device_id"])
	return deviceEnrollResponse{DeviceID: deviceID}, ok
}

func (r *Runner) deleteDevice(ctx context.Context, sessionToken string, deviceID int) bool {
	result := r.doRequest(ctx, http.MethodDelete, fmt.Sprintf("/api/enroll/device/%d", deviceID), "DELETE /api/enroll/device/:deviceID", []int{200}, cookieHeaders(r.cfg.SessionCookieName, sessionToken, nil), nil, false)
	r.metrics.RecordTrend("api_delete_device_duration", result.DurationMs, r.scenario)
	return result.Expected
}

func (r *Runner) deleteHome(ctx context.Context, sessionToken string, homeID int) (deleteHomeResponse, bool) {
	result := r.doRequest(ctx, http.MethodDelete, fmt.Sprintf("/api/enroll/home/%d", homeID), "DELETE /api/enroll/home/:homeID", []int{200}, cookieHeaders(r.cfg.SessionCookieName, sessionToken, nil), nil, false)
	r.metrics.RecordTrend("api_delete_home_duration", result.DurationMs, r.scenario)
	if !result.Expected {
		return deleteHomeResponse{}, false
	}
	var parsed deleteHomeResponse
	if !r.parseJSON(result, &parsed) {
		return deleteHomeResponse{}, false
	}
	return parsed, true
}

func (r *Runner) authorizeCode(ctx context.Context, sessionToken, stateValue string) (string, bool) {
	query := url.Values{}
	query.Set("response_type", "code")
	query.Set("client_id", r.cfg.OAuthClientID)
	query.Set("redirect_uri", r.cfg.OAuthRedirectURI)
	query.Set("state", stateValue)
	path := "/oauth/authorize?" + query.Encode()
	result := r.doRequest(ctx, http.MethodGet, path, "GET /oauth/authorize", []int{302, 303}, cookieHeaders(r.cfg.SessionCookieName, sessionToken, nil), nil, true)
	r.metrics.RecordTrend("api_oauth_authorize_duration", result.DurationMs, r.scenario)
	if !result.Expected {
		return "", false
	}
	location := result.Header.Get("Location")
	if location == "" {
		location = result.Header.Get("location")
	}
	if location == "" {
		r.metrics.RecordCheck(r.scenario, "GET /oauth/authorize returned an authorization code", false)
		return "", false
	}
	code := queryParam(location, "code")
	r.metrics.RecordCheck(r.scenario, "GET /oauth/authorize returned an authorization code", code != "")
	return code, code != ""
}

func (r *Runner) exchangeOAuthToken(ctx context.Context, code string) (oauthTokenResponse, bool) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", r.cfg.OAuthRedirectURI)
	form.Set("client_id", r.cfg.OAuthClientID)
	form.Set("client_secret", r.cfg.OAuthClientSecret)
	headers := map[string]string{
		"Authorization": "Basic " + base64.StdEncoding.EncodeToString([]byte(r.cfg.OAuthClientID+":"+r.cfg.OAuthClientSecret)),
		"Content-Type":  "application/x-www-form-urlencoded",
	}
	result := r.doRequest(ctx, http.MethodPost, "/oauth/token", "POST /oauth/token", []int{200}, headers, []byte(form.Encode()), false)
	r.metrics.RecordTrend("api_oauth_token_duration", result.DurationMs, r.scenario)
	if !result.Expected {
		return oauthTokenResponse{}, false
	}
	var parsed oauthTokenResponse
	if !r.parseJSON(result, &parsed) {
		return oauthTokenResponse{}, false
	}
	ok := parsed.AccessToken != ""
	r.metrics.RecordCheck(r.scenario, "POST /oauth/token returned an access token", ok)
	return parsed, ok
}

func (r *Runner) googleFulfillment(ctx context.Context, accessToken, intent string, payload map[string]any) (map[string]any, bool) {
	labelByIntent := map[string]string{
		"action.devices.SYNC":       "POST /api/google/fulfillment SYNC",
		"action.devices.EXECUTE":    "POST /api/google/fulfillment EXECUTE",
		"action.devices.QUERY":      "POST /api/google/fulfillment QUERY",
		"action.devices.DISCONNECT": "POST /api/google/fulfillment DISCONNECT",
	}
	trendByIntent := map[string]string{
		"action.devices.SYNC":       "api_google_sync_duration",
		"action.devices.EXECUTE":    "api_google_execute_duration",
		"action.devices.QUERY":      "api_google_query_duration",
		"action.devices.DISCONNECT": "api_google_disconnect_duration",
	}
	requestPayload := map[string]any{
		"requestId": fmt.Sprintf("%s-%d", r.cfg.RunID, time.Now().UnixNano()),
		"inputs": []map[string]any{
			{
				"intent":  intent,
				"payload": payload,
			},
		},
	}
	body, _ := json.Marshal(requestPayload)
	startedAt := time.Now()
	result := r.doRequest(ctx, http.MethodPost, "/api/google/fulfillment", labelByIntent[intent], []int{200}, jsonHeaders(map[string]string{
		"Authorization": "Bearer " + accessToken,
	}), body, false)
	r.metrics.RecordTrend(trendByIntent[intent], time.Since(startedAt).Seconds()*1000, r.scenario)
	r.metrics.RecordCounter("phase_fulfillment_requests", 1, r.scenario)
	if !result.Expected {
		return nil, false
	}
	var parsed map[string]any
	if !r.parseJSON(result, &parsed) {
		return nil, false
	}
	return parsed, true
}

func (r *Runner) ensureSessionForSlot(ctx context.Context, slot int) (SessionContext, bool) {
	user, ok := r.state.User(slot)
	if !ok {
		r.metrics.RecordCounter("phase_state_missing", 1, r.scenario)
		r.logf("missing phase-state user slot %d", slot)
		return SessionContext{}, false
	}
	credentials := credentialsForUser(r.cfg.RunID, slot, user)
	if user.SessionToken != "" {
		r.metrics.RecordCounter("phase_session_reuse", 1, r.scenario)
		return SessionContext{
			User:         user,
			Username:     credentials.Username,
			Password:     credentials.Password,
			SessionToken: user.SessionToken,
		}, true
	}

	_, sessionToken := r.loginSession(ctx, credentials.Username, credentials.Password)
	if sessionToken == "" {
		return SessionContext{}, false
	}
	r.metrics.RecordCounter("phase_session_refresh", 1, r.scenario)
	r.state.UpdateSession(slot, sessionToken)
	return SessionContext{
		User:         user,
		Username:     credentials.Username,
		Password:     credentials.Password,
		SessionToken: sessionToken,
	}, true
}

func (r *Runner) ensureOAuthForSlot(ctx context.Context, slot int, sessionToken string) (string, bool) {
	user, ok := r.state.User(slot)
	if !ok {
		r.metrics.RecordCounter("phase_state_missing", 1, r.scenario)
		r.logf("missing phase-state user slot %d for oauth", slot)
		return "", false
	}
	if user.OAuthAccessToken != "" {
		r.metrics.RecordCounter("phase_oauth_reuse", 1, r.scenario)
		return user.OAuthAccessToken, true
	}

	code, ok := r.authorizeCode(ctx, sessionToken, fmt.Sprintf("%d-oauth-refresh", slot))
	if !ok {
		return "", false
	}
	token, ok := r.exchangeOAuthToken(ctx, code)
	if !ok {
		return "", false
	}
	r.metrics.RecordCounter("phase_oauth_refresh", 1, r.scenario)
	r.state.UpdateOAuth(slot, token.AccessToken, token.RefreshToken)
	return token.AccessToken, true
}

func userCredentials(runID string, userIndex int) Credentials {
	username := fmt.Sprintf("k6_phase_%s_%d", runID, userIndex)
	username = sanitizeIdentifier(username)
	return Credentials{
		Username: username,
		Password: deterministicPassword,
	}
}

type Credentials struct {
	Username string
	Password string
}

func credentialsForUser(runID string, slot int, snapshot UserSnapshot) Credentials {
	if snapshot.Username != "" && snapshot.Password != "" {
		return Credentials{
			Username: snapshot.Username,
			Password: snapshot.Password,
		}
	}
	return userCredentials(runID, slot)
}

func homeName(runID string, userIndex, homeSlot int) string {
	return fmt.Sprintf("k6-home-%s-%d-%d", runID, userIndex, homeSlot)
}

func deviceName(runID string, userIndex, homeSlot, deviceSlot int) string {
	return fmt.Sprintf("k6-device-%s-%d-%d-%d", runID, userIndex, homeSlot, deviceSlot)
}

func sanitizeIdentifier(value string) string {
	builder := strings.Builder{}
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			builder.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			builder.WriteRune(r)
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
		default:
			builder.WriteByte('_')
		}
	}
	result := builder.String()
	if len(result) > 64 {
		return result[:64]
	}
	return result
}

func sessionTokenFromHeaders(header http.Header, cookieName string) string {
	for _, value := range header.Values("Set-Cookie") {
		parts := strings.Split(value, ";")
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if strings.HasPrefix(part, cookieName+"=") {
				return strings.TrimPrefix(part, cookieName+"=")
			}
		}
	}
	return ""
}

func statusAllowed(status int, expected []int) bool {
	for _, code := range expected {
		if status == code {
			return true
		}
	}
	return false
}

func jsonHeaders(extra map[string]string) map[string]string {
	headers := map[string]string{
		"Content-Type": "application/json",
	}
	for key, value := range extra {
		headers[key] = value
	}
	return headers
}

func cookieHeaders(cookieName, cookieValue string, extra map[string]string) map[string]string {
	headers := map[string]string{
		"Cookie": cookieName + "=" + cookieValue,
	}
	for key, value := range extra {
		headers[key] = value
	}
	return headers
}

func cookieAndJSONHeaders(cookieName, cookieValue string, extra map[string]string) map[string]string {
	return cookieHeaders(cookieName, cookieValue, jsonHeaders(extra))
}

func queryParam(location, name string) string {
	parsed, err := url.Parse(location)
	if err != nil {
		return ""
	}
	return parsed.Query().Get(name)
}

func syncResponseIncludesDevice(payload map[string]any, compoundID string) bool {
	inner, ok := payload["payload"].(map[string]any)
	if !ok {
		return false
	}
	devices, ok := inner["devices"].([]any)
	if !ok {
		return false
	}
	for _, entry := range devices {
		item, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		if fmt.Sprint(item["id"]) == compoundID {
			return true
		}
	}
	return false
}

func queryResponseSucceeded(payload map[string]any, compoundID string) bool {
	inner, ok := payload["payload"].(map[string]any)
	if !ok {
		return false
	}
	devices, ok := inner["devices"].(map[string]any)
	if !ok {
		return false
	}
	deviceState, ok := devices[compoundID].(map[string]any)
	if !ok {
		return false
	}
	return strings.EqualFold(fmt.Sprint(deviceState["status"]), "SUCCESS")
}

func executeResponseSucceeded(payload map[string]any) bool {
	inner, ok := payload["payload"].(map[string]any)
	if !ok {
		return false
	}
	commands, ok := inner["commands"].([]any)
	if !ok {
		return false
	}
	for _, entry := range commands {
		item, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		if strings.EqualFold(fmt.Sprint(item["status"]), "SUCCESS") {
			return true
		}
	}
	return false
}

func fulfillmentSteps(compoundID string, iterationSeed, requestCount int) []fulfillmentStep {
	steps := []fulfillmentStep{
		{
			Intent:  "action.devices.SYNC",
			Payload: map[string]any{},
		},
		{
			Intent: "action.devices.QUERY",
			Payload: map[string]any{
				"devices": []map[string]any{{"id": compoundID}},
			},
		},
		{
			Intent:  "action.devices.EXECUTE",
			Payload: buildExecutePayload(compoundID, iterationSeed%2 == 0),
		},
		{
			Intent: "action.devices.QUERY",
			Payload: map[string]any{
				"devices": []map[string]any{{"id": compoundID}},
			},
		},
		{
			Intent:  "action.devices.EXECUTE",
			Payload: buildExecutePayload(compoundID, iterationSeed%2 != 0),
		},
		{
			Intent:  "action.devices.DISCONNECT",
			Payload: map[string]any{},
		},
	}
	if requestCount < len(steps) {
		return steps[:requestCount]
	}
	return steps
}

func buildExecutePayload(compoundID string, on bool) map[string]any {
	return map[string]any{
		"commands": []map[string]any{
			{
				"devices": []map[string]any{{"id": compoundID}},
				"execution": []map[string]any{
					{
						"command": "action.devices.commands.OnOff",
						"params": map[string]any{
							"on": on,
						},
					},
				},
			},
		},
	}
}

func provisionState(entry homeListEntry) string {
	return strings.ToLower(strings.TrimSpace(entry.MQTTProvisionState))
}

func findHomeByID(homes []homeListEntry, homeID int) *homeListEntry {
	for idx := range homes {
		if homes[idx].HomeID == homeID {
			return &homes[idx]
		}
	}
	return nil
}

func findHomeByName(homes []homeListEntry, name string) *homeListEntry {
	for idx := range homes {
		if homes[idx].Name == name {
			return &homes[idx]
		}
	}
	return nil
}

func boolString(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func indexString(value int) string {
	return strconv.Itoa(value)
}

func intFromAny(value any) (int, bool) {
	switch typed := value.(type) {
	case float64:
		return int(typed), true
	case float32:
		return int(typed), true
	case int:
		return typed, true
	case int64:
		return int(typed), true
	case json.Number:
		parsed, err := typed.Int64()
		if err != nil {
			return 0, false
		}
		return int(parsed), true
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(typed))
		if err != nil {
			return 0, false
		}
		return parsed, true
	default:
		return 0, false
	}
}
