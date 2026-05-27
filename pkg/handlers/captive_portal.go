package handlers

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/denysvitali/pictures-sync-s3/pkg/httputil"
)

// HandleCaptivePortalStatus returns the current captive portal authentication status
//
// GET /api/captive-portal/status
//
// Response:
//
//	{
//	  "authenticated": true,
//	  "network": "JinJiangRewards",
//	  "authenticated_at": "2025-10-17T14:30:00Z",
//	  "age_seconds": 120,
//	  "health_check_running": true
//	}
func (ctx *Context) HandleCaptivePortalStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if ctx.CaptivePortal == nil {
		httputil.JSON(w, http.StatusOK, map[string]any{
			"error":   "Captive portal authenticator not initialized",
			"enabled": false,
		})
		return
	}

	status := ctx.CaptivePortal.GetAuthenticationStatus()
	status["enabled"] = true

	httputil.JSON(w, http.StatusOK, status)
}

// HandleCaptivePortalAuthenticate manually triggers captive portal authentication
//
// POST /api/captive-portal/authenticate
//
// This endpoint allows manual triggering of captive portal authentication,
// useful for testing or when automatic authentication fails.
//
// Request body (optional):
//
//	{
//	  "force": true  // Force re-authentication even if recently authenticated
//	}
//
// Response on success:
//
//	{
//	  "success": true,
//	  "message": "Authentication successful",
//	  "network": "JinJiangRewards"
//	}
//
// Response on error:
//
//	{
//	  "success": false,
//	  "error": "Authentication failed: ...",
//	  "network": "JinJiangRewards"
//	}
func (ctx *Context) HandleCaptivePortalAuthenticate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if ctx.CaptivePortal == nil {
		http.Error(w, "Captive portal authenticator not initialized", http.StatusServiceUnavailable)
		return
	}

	if ctx.WiFiMgr == nil {
		http.Error(w, "WiFi manager not initialized", http.StatusServiceUnavailable)
		return
	}

	// Parse optional request body
	var req struct {
		Force bool `json:"force"`
	}
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}

	log.Printf("[API] Manual captive portal authentication requested (force=%v)", req.Force)

	// Get current network
	conn, err := ctx.WiFiMgr.GetCurrentConnection()
	if err != nil {
		log.Printf("[API] Not connected to WiFi: %v", err)
		httputil.JSON(w, http.StatusOK, map[string]any{
			"success": false,
			"error":   "Not connected to WiFi network",
		})
		return
	}

	// If force=true, reset authentication time to trigger re-auth
	if req.Force {
		log.Printf("[API] Force flag set, clearing authentication state")
		ctx.CaptivePortal.ClearAuthenticationState()
	}

	// Trigger authentication
	ctx.CaptivePortal.Authenticate()

	// Check if it was successful by examining the status
	status := ctx.CaptivePortal.GetAuthenticationStatus()
	authenticated := status["authenticated"].(bool)

	response := map[string]any{
		"network": conn.SSID,
		"success": true,
	}

	if authenticated {
		// Successfully authenticated
		log.Printf("[API] Successfully authenticated to '%s'", conn.SSID)
		response["message"] = "Authentication successful"
		response["authenticated"] = true
	} else {
		// Network doesn't require authentication or auth failed
		log.Printf("[API] Network '%s' authentication result: authenticated=%v", conn.SSID, authenticated)
		response["message"] = "Authentication attempted"
		response["authenticated"] = false
	}

	httputil.JSON(w, http.StatusOK, response)
}

// HandleCaptivePortalTest tests the captive portal detection and retrieves network identifiers
//
// GET /api/captive-portal/test
//
// This endpoint is useful for debugging and testing captive portal functionality.
// It retrieves the local IP and MAC address without actually performing authentication.
//
// Response:
//
//	{
//	  "current_network": "JinJiangRewards",
//	  "requires_auth": true,
//	  "local_ip": "192.168.1.100",
//	  "local_mac": "aa:bb:cc:dd:ee:ff",
//	  "error": null
//	}
func (ctx *Context) HandleCaptivePortalTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if ctx.WiFiMgr == nil {
		http.Error(w, "WiFi manager not initialized", http.StatusServiceUnavailable)
		return
	}

	response := map[string]any{}

	// Get current network
	conn, err := ctx.WiFiMgr.GetCurrentConnection()
	if err != nil {
		response["error"] = err.Error()
		response["connected"] = false
		httputil.JSON(w, http.StatusOK, response)
		return
	}

	response["current_network"] = conn.SSID
	response["connected"] = true

	// Check if network requires authentication
	// This is a bit of a hack - we check if it's JinJiangRewards
	requiresAuth := (conn.SSID == "JinJiangRewards")
	response["requires_auth"] = requiresAuth

	// Try to get local IP and MAC
	ip, mac, err := ctx.CaptivePortal.GetLocalIPMAC()
	if err != nil {
		response["ip_mac_error"] = err.Error()
	} else {
		response["local_ip"] = ip
		response["local_mac"] = mac
	}

	httputil.JSON(w, http.StatusOK, response)
}
