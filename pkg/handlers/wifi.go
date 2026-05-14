package handlers

import (
	"crypto/subtle"
	"encoding/json"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/denysvitali/pictures-sync-s3/pkg/auth"
	"github.com/denysvitali/pictures-sync-s3/pkg/ratelimit"
	"github.com/denysvitali/pictures-sync-s3/pkg/websocket"
	"github.com/denysvitali/pictures-sync-s3/pkg/wifimanager"
)

var wsTokenRateLimitConfig = ratelimit.Config{
	RequestsPerSecond: 5.0 / 60.0,
	Burst:             5,
	MaxAuthAttempts:   0,
	AuthWindow:        0,
	LockoutDuration:   0,
	CleanupInterval:   5 * time.Minute,
	ClientExpiry:      30 * time.Minute,
}

// WSTokenRateLimitConfig returns the per-IP rate-limit configuration used by
// the ws-token endpoint (5 requests/minute per IP).
func WSTokenRateLimitConfig() ratelimit.Config {
	return wsTokenRateLimitConfig
}

// HandleWSToken generates and returns a WebSocket authentication token.
func HandleWSToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	token := websocket.CreateWSToken()
	JSONResponse(w, map[string]string{
		"ws_token": token,
	})
}

// WSTokenHandler returns an http.HandlerFunc for /api/ws-token that enforces
// Basic Auth (gokrazy:<password>) and a tight per-IP rate limit before
// minting a WebSocket auth token.
func WSTokenHandler(passwordProvider auth.PasswordProvider, limiter *ratelimit.Limiter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		ip := getClientIP(r)

		if limiter != nil && !limiter.Allow(ip, wsTokenRateLimitConfig) {
			log.Printf("SECURITY: ws-token rate limit exceeded for IP %s", ip)
			http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
			return
		}

		username, password, ok := r.BasicAuth()
		expected := ""
		if passwordProvider != nil {
			expected = passwordProvider.CurrentPassword()
		}
		usernameMatch := subtle.ConstantTimeCompare([]byte(username), []byte("gokrazy")) == 1
		passwordMatch := subtle.ConstantTimeCompare([]byte(password), []byte(expected)) == 1
		if !ok || !usernameMatch || !passwordMatch {
			w.Header().Set("WWW-Authenticate", `Basic realm="Photo Backup Station"`)
			log.Printf("SECURITY: ws-token unauthenticated request from IP %s", ip)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		token := websocket.CreateWSToken()
		JSONResponse(w, map[string]string{
			"ws_token": token,
		})
	}
}

// HandleWiFiScan scans for available networks
func (ctx *Context) HandleWiFiScan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if ctx.WiFiMgr == nil {
		http.Error(w, "WiFi manager not initialized", http.StatusServiceUnavailable)
		return
	}

	// Parse request body for sorting options
	var req struct {
		SortBy string `json:"sort_by,omitempty"` // "signal", "name", "security"
	}

	// Try to decode request body, ignore errors for backward compatibility
	if r.Body != nil {
		json.NewDecoder(r.Body).Decode(&req)
	}

	networks, err := ctx.WiFiMgr.ScanNetworks()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Apply sorting
	sortedNetworks := sortWiFiNetworks(networks, req.SortBy)

	JSONResponse(w, map[string]interface{}{
		"networks": sortedNetworks,
	})
}

// SafeNetworkInfo represents network information without sensitive credentials.
// SECURITY: This struct is used to prevent password exposure via API responses.
// Even with authentication, credentials should never be returned in API calls.
type SafeNetworkInfo struct {
	SSID        string `json:"ssid"`
	HasPassword bool   `json:"has_password"`
}

// HandleWiFiNetworks returns saved networks without exposing passwords
// SECURITY FIX: Filters out PSK field to prevent credential exposure
func (ctx *Context) HandleWiFiNetworks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if ctx.WiFiMgr == nil {
		http.Error(w, "WiFi manager not initialized", http.StatusServiceUnavailable)
		return
	}

	networks := ctx.WiFiMgr.GetNetworks()

	// Convert to safe response that excludes passwords
	safeNetworks := make([]SafeNetworkInfo, len(networks))
	for i, network := range networks {
		safeNetworks[i] = SafeNetworkInfo{
			SSID:        network.SSID,
			HasPassword: network.PSK != "",
		}
	}

	JSONResponse(w, map[string]interface{}{
		"networks": safeNetworks,
	})
}

// HandleWiFiConnect connects to a network
func (ctx *Context) HandleWiFiConnect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if ctx.WiFiMgr == nil {
		http.Error(w, "WiFi manager not initialized", http.StatusServiceUnavailable)
		return
	}

	var req struct {
		SSID     string `json:"ssid"`
		Password string `json:"password"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if err := ctx.WiFiMgr.AddNetwork(req.SSID, req.Password); err != nil {
		JSONResponse(w, map[string]interface{}{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	JSONResponse(w, map[string]interface{}{
		"success": true,
	})
}

// HandleWiFiDisconnect removes a network
func (ctx *Context) HandleWiFiDisconnect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if ctx.WiFiMgr == nil {
		http.Error(w, "WiFi manager not initialized", http.StatusServiceUnavailable)
		return
	}

	var req struct {
		SSID string `json:"ssid"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if err := ctx.WiFiMgr.RemoveNetwork(req.SSID); err != nil {
		JSONResponse(w, map[string]interface{}{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	JSONResponse(w, map[string]interface{}{
		"success": true,
	})
}

// HandleWiFiStatus returns current WiFi status with signal strength
func (ctx *Context) HandleWiFiStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if ctx.WiFiMgr == nil {
		http.Error(w, "WiFi manager not initialized", http.StatusServiceUnavailable)
		return
	}

	conn, err := ctx.WiFiMgr.GetCurrentConnection()
	if err != nil {
		JSONResponse(w, map[string]interface{}{
			"connected": false,
			"error":     err.Error(),
		})
		return
	}

	JSONResponse(w, map[string]interface{}{
		"connected": true,
		"ssid":      conn.SSID,
		"signal":    conn.Signal,
	})
}

// HandleWiFiReorder reorders the WiFi networks
func (ctx *Context) HandleWiFiReorder(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if ctx.WiFiMgr == nil {
		http.Error(w, "WiFi manager not initialized", http.StatusServiceUnavailable)
		return
	}

	var req struct {
		SSIDs []string `json:"ssids"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if err := ctx.WiFiMgr.ReorderNetworks(req.SSIDs); err != nil {
		JSONResponse(w, map[string]interface{}{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	JSONResponse(w, map[string]interface{}{
		"success": true,
	})
}

// sortWiFiNetworks sorts a slice of WiFi networks based on the specified criteria
func sortWiFiNetworks(networks []wifimanager.ScanResult, sortBy string) []wifimanager.ScanResult {
	if len(networks) <= 1 {
		return networks
	}

	// Make a copy to avoid modifying the original slice
	sorted := make([]wifimanager.ScanResult, len(networks))
	copy(sorted, networks)

	switch strings.ToLower(sortBy) {
	case "signal", "signal_strength":
		// Sort by signal strength (strongest first)
		sort.Slice(sorted, func(i, j int) bool {
			return sorted[i].Signal > sorted[j].Signal
		})
	case "name", "ssid":
		// Sort by SSID alphabetically (case-insensitive)
		sort.Slice(sorted, func(i, j int) bool {
			return strings.ToLower(sorted[i].SSID) < strings.ToLower(sorted[j].SSID)
		})
	case "security":
		// Sort by security status (encrypted first, then by signal strength)
		sort.Slice(sorted, func(i, j int) bool {
			if sorted[i].Encrypted != sorted[j].Encrypted {
				return sorted[i].Encrypted // Encrypted networks first
			}
			// If same security status, sort by signal strength
			return sorted[i].Signal > sorted[j].Signal
		})
	default:
		// Default sorting: by signal strength (strongest first)
		sort.Slice(sorted, func(i, j int) bool {
			return sorted[i].Signal > sorted[j].Signal
		})
	}

	return sorted
}
