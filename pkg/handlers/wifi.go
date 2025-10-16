package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/denysvitali/pictures-sync-s3/pkg/websocket"
)

// HandleWSToken generates and returns a WebSocket authentication token
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

	networks, err := ctx.WiFiMgr.ScanNetworks()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	JSONResponse(w, networks)
}

// HandleWiFiNetworks returns saved networks
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
	JSONResponse(w, networks)
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
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	JSONResponse(w, map[string]string{"status": "ok"})
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
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	JSONResponse(w, map[string]string{"status": "ok"})
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
