package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/denysvitali/pictures-sync-s3/pkg/auth"
	"github.com/denysvitali/pictures-sync-s3/pkg/events"
	"github.com/denysvitali/pictures-sync-s3/pkg/handlers"
	"github.com/denysvitali/pictures-sync-s3/pkg/settings"
	"github.com/denysvitali/pictures-sync-s3/pkg/state"
	"github.com/denysvitali/pictures-sync-s3/pkg/syncmanager"
	"github.com/denysvitali/pictures-sync-s3/pkg/webui"
	"github.com/denysvitali/pictures-sync-s3/pkg/websocket"
	"github.com/denysvitali/pictures-sync-s3/pkg/wifimanager"
)

// logConfiguredWiFiNetworks logs WiFi networks from both gokrazy and app config files
func logConfiguredWiFiNetworks(wifiMgr *wifimanager.Manager) {
	// Log networks from our app's config (/perm/extra-wifi.json)
	appNetworks := wifiMgr.GetNetworks()
	log.Printf("WiFi networks in /perm/extra-wifi.json: %d", len(appNetworks))
	for i, net := range appNetworks {
		log.Printf("  [%d] SSID: %s (has password: %v)", i+1, net.SSID, net.PSK != "")
	}

	// Log networks from gokrazy's WiFi config (/perm/wifi.json)
	type GokrazyNetwork struct {
		SSID string `json:"ssid"`
		PSK  string `json:"psk"`
	}
	type GokrazyWiFiConfig struct {
		Networks []GokrazyNetwork `json:"networks"`
	}

	gokrazyConfigPath := "/perm/wifi.json"
	if data, err := os.ReadFile(gokrazyConfigPath); err == nil {
		var config GokrazyWiFiConfig
		if err := json.Unmarshal(data, &config); err == nil {
			log.Printf("WiFi networks in %s: %d", gokrazyConfigPath, len(config.Networks))
			for i, net := range config.Networks {
				log.Printf("  [%d] SSID: %s (has password: %v)", i+1, net.SSID, net.PSK != "")
			}
		} else {
			log.Printf("WiFi networks in %s: failed to parse (%v)", gokrazyConfigPath, err)
		}
	} else if !os.IsNotExist(err) {
		log.Printf("WiFi networks in %s: failed to read (%v)", gokrazyConfigPath, err)
	} else {
		log.Printf("WiFi networks in %s: 0 (file does not exist)", gokrazyConfigPath)
	}
}

func main() {
	// Enable caller reporting in logs (file:line)
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	log.Println("Photo Backup Station WebUI - Starting...")

	// Load password from gokrazy password file or use default for development
	var authPassword string
	passwordBytes, err := os.ReadFile("/etc/gokr-pw.txt")
	if err != nil {
		log.Printf("Warning: Failed to read password file: %v", err)
		log.Println("Using default development password")
		authPassword = "dev"
	} else {
		authPassword = strings.TrimSpace(string(passwordBytes))
		if authPassword == "" {
			log.Println("Password file is empty, using default development password")
			authPassword = "dev"
		}
	}

	// Get port from environment or default to 8080
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	// Start WebSocket token cleanup goroutine
	go websocket.CleanupExpiredWSTokens()

	// Initialize event manager
	eventMgr := events.NewManager()

	// Initialize state manager
	stateMgr, err := state.NewManager()
	if err != nil {
		log.Fatalf("Failed to create state manager: %v", err)
	}

	// Load settings
	appSettings, err := settings.Load()
	if err != nil {
		log.Fatalf("Failed to load settings: %v", err)
	}

	// Initialize sync manager
	syncMgr := syncmanager.NewManager(
		state.GetRcloneConfigPath(),
		appSettings.GetRemoteName(),
		appSettings.GetRemotePath(),
		stateMgr,
		appSettings.GetTransfers(),
		appSettings.GetCheckers(),
	)
	// Update Google Photos settings
	syncMgr.SetGooglePhotos(appSettings.GetGooglePhotosEnabled(), appSettings.GetGooglePhotosRemoteName())

	// Initialize WiFi manager
	wifiMgr, err := wifimanager.NewManager()
	if err != nil {
		log.Printf("Warning: Failed to initialize WiFi manager: %v", err)
	} else {
		// Log configured WiFi networks
		logConfiguredWiFiNetworks(wifiMgr)
	}

	// Initialize CSRF token
	auth.InitCSRFToken()
	log.Println("CSRF protection enabled")

	// Create handler context
	ctx := &handlers.Context{
		StateMgr:    stateMgr,
		SyncMgr:     syncMgr,
		WiFiMgr:     wifiMgr,
		AppSettings: appSettings,
	}

	// Setup HTTP handlers
	http.HandleFunc("/api/csrf-token", handlers.HandleCSRFToken) // GET endpoint for CSRF token
	http.HandleFunc("/api/ws-token", handlers.HandleWSToken)     // GET endpoint for WebSocket token
	http.HandleFunc("/api/status", ctx.HandleStatus)
	http.HandleFunc("/api/history", ctx.HandleHistory)
	http.HandleFunc("/api/config", auth.CSRFProtection(ctx.HandleConfig))                  // CSRF protected
	http.HandleFunc("/api/config/test", auth.CSRFProtection(ctx.HandleConfigTest))         // CSRF protected
	http.HandleFunc("/api/settings", auth.CSRFProtection(ctx.HandleSettings))              // CSRF protected
	http.HandleFunc("/api/devices", ctx.HandleDevices)
	http.HandleFunc("/api/devices/select", auth.CSRFProtection(ctx.HandleDeviceSelect))    // CSRF protected
	http.HandleFunc("/api/sync/start", auth.CSRFProtection(ctx.HandleSyncStart))           // CSRF protected
	http.HandleFunc("/api/sync/cancel", auth.CSRFProtection(ctx.HandleSyncCancel))         // CSRF protected
	http.HandleFunc("/api/wifi/scan", ctx.HandleWiFiScan)
	http.HandleFunc("/api/wifi/networks", ctx.HandleWiFiNetworks)
	http.HandleFunc("/api/wifi/connect", auth.CSRFProtection(ctx.HandleWiFiConnect))       // CSRF protected
	http.HandleFunc("/api/wifi/disconnect", auth.CSRFProtection(ctx.HandleWiFiDisconnect)) // CSRF protected
	http.HandleFunc("/api/wifi/status", ctx.HandleWiFiStatus)
	http.HandleFunc("/api/files/cards", ctx.HandleFileCards)
	http.HandleFunc("/api/files", ctx.HandleFiles)
	http.HandleFunc("/api/files/paginated", ctx.HandleFilesPaginated)
	http.HandleFunc("/api/files/view", ctx.HandleFileView)
	http.HandleFunc("/api/thumbnail", ctx.HandleThumbnail)
	http.HandleFunc("/api/sdcard/files", ctx.HandleSDCardFiles)
	http.HandleFunc("/api/sdcard/preview", ctx.HandleSDCardPreview)
	http.HandleFunc("/api/network/dns", ctx.HandleNetworkDNS)
	http.HandleFunc("/api/network/interfaces", ctx.HandleNetworkInterfaces)
	http.HandleFunc("/api/network/dns-lookup", ctx.HandleDNSLookup)
	http.HandleFunc("/api/network/ping", ctx.HandlePing)
	http.HandleFunc("/api/network/diagnostics", ctx.HandleNetworkDiagnostics)
	http.HandleFunc("/ws", websocket.HandleWebSocket(stateMgr, eventMgr))
	http.HandleFunc("/", webui.HandleIndex)

	// Wrap default mux with basic auth middleware
	handler := auth.BasicAuthMiddleware(authPassword)(http.DefaultServeMux)

	// Start HTTPS server
	addr := ":" + port
	certFile := "/etc/ssl/gokrazy-web.pem"
	keyFile := "/etc/ssl/gokrazy-web.key.pem"

	log.Printf("WebUI HTTPS server listening on %s", addr)
	if err := http.ListenAndServeTLS(addr, certFile, keyFile, handler); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
