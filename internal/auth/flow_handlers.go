package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
)

// kratosFlowUI is the subset of a Kratos flow response needed for proxying submissions.
type kratosFlowUI struct {
	Action string `json:"action"`
	Method string `json:"method"`
}

type kratosFlowResponse struct {
	ID       string       `json:"id"`
	UI       kratosFlowUI `json:"ui"`
	ReturnTo string       `json:"return_to"`
}

// handleFlowInit returns a handler that redirects the browser to Kratos to create a new flow.
// Kratos will then redirect back to the backend's /api/auth/ui/{type} endpoint.
func handleFlowInit(flowType string) gin.HandlerFunc {
	return func(c *gin.Context) {
		target := kratosPublicURL + "/self-service/" + flowType + "/browser"
		if returnTo := c.Query("return_to"); returnTo != "" {
			target += "?return_to=" + url.QueryEscape(returnTo)
		}
		c.Redirect(http.StatusFound, target)
	}
}

// handleFlowUI returns a handler that receives Kratos UI URL callbacks and redirects
// the browser to the frontend page with the flow ID.
func handleFlowUI(flowType string) gin.HandlerFunc {
	return func(c *gin.Context) {
		flowID := c.Query("flow")
		if flowID == "" {
			c.String(http.StatusBadRequest, "missing flow parameter")
			return
		}
		target := "/" + flowType + "?flow=" + url.QueryEscape(flowID)
		c.Redirect(http.StatusFound, target)
	}
}

// handleLogout creates a Kratos logout flow and redirects the browser to the logout URL.
func handleLogout(c *gin.Context) {
	cookie := extractKratosCookie(c.Request)
	if cookie == "" {
		c.Redirect(http.StatusFound, "/")
		return
	}

	req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodGet,
		kratosPublicURL+"/self-service/logout/browser", nil)
	if err != nil {
		log.Printf("error creating logout request: %v", err)
		c.Redirect(http.StatusFound, "/")
		return
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Cookie", "ory_kratos_session="+cookie)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("error fetching logout flow: %v", err)
		c.Redirect(http.StatusFound, "/")
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("kratos logout/browser returned %d", resp.StatusCode)
		c.Redirect(http.StatusFound, "/")
		return
	}

	var data struct {
		LogoutURL string `json:"logout_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil || data.LogoutURL == "" {
		log.Printf("error parsing logout flow: %v", err)
		c.Redirect(http.StatusFound, "/")
		return
	}

	c.Redirect(http.StatusFound, data.LogoutURL)
}

// handleFlowSubmit proxies a form submission to Kratos and interprets the response.
// It returns JSON to the frontend: either {"redirect_to": "..."} or the Kratos flow
// (with ui/errors) for re-rendering.
func handleFlowSubmit(c *gin.Context) {
	flowType := c.Param("type")
	if flowType == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing flow type"})
		return
	}

	flowID := c.Query("flow")
	if flowID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing flow parameter"})
		return
	}

	// Fetch the flow to get the Kratos action URL.
	flow, err := fetchKratosFlow(c.Request.Context(), flowType, flowID, c.Request)
	if err != nil {
		log.Printf("error fetching flow for submit: %v", err)
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to fetch flow"})
		return
	}

	// Rewrite the action URL from the public domain to the internal Kratos URL.
	actionURL := rewriteToInternalKratos(flow.UI.Action)

	// Read the form body from the frontend.
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read request body"})
		return
	}

	// Proxy the submission to Kratos.
	kratosReq, err := http.NewRequestWithContext(c.Request.Context(),
		flow.UI.Method, actionURL, strings.NewReader(string(body)))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create proxy request"})
		return
	}
	kratosReq.Header.Set("Content-Type", c.GetHeader("Content-Type"))
	kratosReq.Header.Set("Accept", "application/json")

	// Forward all cookies from the browser to Kratos.
	for _, ck := range c.Request.Cookies() {
		kratosReq.AddCookie(ck)
	}

	// Use a client that does NOT follow redirects — we want the raw response.
	noRedirectClient := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	kratosResp, err := noRedirectClient.Do(kratosReq)
	if err != nil {
		log.Printf("error proxying to kratos: %v", err)
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to contact identity service"})
		return
	}
	defer kratosResp.Body.Close()

	// Forward Set-Cookie headers from Kratos to the browser.
	for _, setCookie := range kratosResp.Header.Values("Set-Cookie") {
		c.Header("Set-Cookie", setCookie)
	}

	// If Kratos returned a redirect (3xx), forward it.
	if kratosResp.StatusCode >= 300 && kratosResp.StatusCode < 400 {
		loc := kratosResp.Header.Get("Location")
		if loc != "" {
			c.JSON(http.StatusOK, gin.H{"redirect_to": loc})
			return
		}
	}

	// Read and parse the response body.
	respBody, err := io.ReadAll(kratosResp.Body)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to read identity service response"})
		return
	}

	var respData map[string]interface{}
	if err := json.Unmarshal(respBody, &respData); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to parse identity service response"})
		return
	}

	// 1. redirect_browser_to — follow it (OIDC, verification UI, etc.)
	if redirectTo, ok := respData["redirect_browser_to"].(string); ok && redirectTo != "" {
		c.JSON(http.StatusOK, gin.H{"redirect_to": redirectTo})
		return
	}

	// 2. continue_with — check for show_verification_ui
	if continueWith, ok := respData["continue_with"].([]interface{}); ok {
		for _, item := range continueWith {
			action, _ := item.(map[string]interface{})
			if action["action"] == "show_verification_ui" {
				if flowObj, ok := action["flow"].(map[string]interface{}); ok {
					if flowURL, ok := flowObj["url"].(string); ok && flowURL != "" {
						c.JSON(http.StatusOK, gin.H{"redirect_to": flowURL})
						return
					}
				}
			}
		}
	}

	// 3. ui present — validation errors, return as-is for re-rendering
	if _, hasUI := respData["ui"]; hasUI {
		c.Data(kratosResp.StatusCode, "application/json", respBody)
		return
	}

	// 4. Success — determine return URL
	returnTo := "/"
	if rt, ok := respData["return_to"].(string); ok && rt != "" {
		returnTo = rt
	} else if flow.ReturnTo != "" {
		returnTo = flow.ReturnTo
	}

	c.JSON(http.StatusOK, gin.H{"redirect_to": returnTo})
}

// fetchKratosFlow fetches a Kratos flow by type and ID, forwarding the browser's cookies.
func fetchKratosFlow(ctx context.Context, flowType, flowID string, originalReq *http.Request) (*kratosFlowResponse, error) {
	reqURL := fmt.Sprintf("%s/self-service/%s/flows?id=%s", kratosPublicURL, flowType, url.QueryEscape(flowID))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	// Forward cookies from the browser.
	for _, ck := range originalReq.Cookies() {
		req.AddCookie(ck)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("kratos returned %d for %s flow %s", resp.StatusCode, flowType, flowID)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var flow kratosFlowResponse
	if err := json.Unmarshal(body, &flow); err != nil {
		return nil, err
	}
	return &flow, nil
}

// rewriteToInternalKratos rewrites a Kratos public URL to use the internal address.
// e.g. "https://example.com/self-service/login?flow=abc" -> "http://kratos:4433/self-service/login?flow=abc"
func rewriteToInternalKratos(publicURL string) string {
	parsed, err := url.Parse(publicURL)
	if err != nil {
		return publicURL
	}
	internal, err := url.Parse(kratosPublicURL)
	if err != nil {
		return publicURL
	}
	parsed.Scheme = internal.Scheme
	parsed.Host = internal.Host
	return parsed.String()
}
