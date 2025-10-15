package main

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/denysvitali/pictures-sync-s3/pkg/sdmonitor"
	"github.com/denysvitali/pictures-sync-s3/pkg/settings"
	"github.com/denysvitali/pictures-sync-s3/pkg/state"
	"github.com/denysvitali/pictures-sync-s3/pkg/syncmanager"
	"github.com/denysvitali/pictures-sync-s3/pkg/wifimanager"
	"github.com/gorilla/websocket"
	"github.com/nfnt/resize"
	"github.com/rwcarlsen/goexif/exif"
	"github.com/tatsushid/go-fastping"
)

var (
	stateMgr     *state.Manager
	syncMgr      *syncmanager.Manager
	wifiMgr      *wifimanager.Manager
	appSettings  *settings.Settings
	authPassword string
	csrfToken    string
	csrfMutex    sync.RWMutex
	upgrader     = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			// Allow same-origin requests and local network addresses
			origin := r.Header.Get("Origin")
			if origin == "" {
				return true // Allow requests without Origin header (same-origin)
			}

			// Parse the origin URL
			u, err := url.Parse(origin)
			if err != nil {
				return false
			}

			// Allow same host
			if u.Host == r.Host {
				return true
			}

			// Allow local network addresses (for Gokrazy appliance access)
			// This includes private IP ranges and .local domains
			host := strings.ToLower(u.Hostname())
			if strings.HasSuffix(host, ".local") ||
				strings.HasPrefix(host, "192.168.") ||
				strings.HasPrefix(host, "10.") ||
				strings.HasPrefix(host, "172.") ||
				host == "localhost" ||
				host == "127.0.0.1" {
				return true
			}

			return false
		},
	}
)

// logConfiguredWiFiNetworks logs WiFi networks from both gokrazy and app config files
func logConfiguredWiFiNetworks() {
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

// generateCSRFToken creates a new CSRF token
func generateCSRFToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		log.Fatal("Failed to generate CSRF token:", err)
	}
	return base64.StdEncoding.EncodeToString(b)
}

// getCSRFToken returns the current CSRF token
func getCSRFToken() string {
	csrfMutex.RLock()
	defer csrfMutex.RUnlock()
	return csrfToken
}

// validateCSRFToken checks if the provided token matches the current token
func validateCSRFToken(token string) bool {
	if token == "" {
		return false
	}
	csrfMutex.RLock()
	defer csrfMutex.RUnlock()
	return subtle.ConstantTimeCompare([]byte(token), []byte(csrfToken)) == 1
}

// csrfProtection is middleware that validates CSRF tokens for state-changing requests
func csrfProtection(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Only check CSRF for state-changing methods
		if r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodDelete {
			token := r.Header.Get("X-CSRF-Token")
			if !validateCSRFToken(token) {
				http.Error(w, "Invalid CSRF token", http.StatusForbidden)
				log.Printf("CSRF validation failed for %s %s from %s", r.Method, r.URL.Path, r.RemoteAddr)
				return
			}
		}
		next(w, r)
	}
}

func main() {
	// Enable caller reporting in logs (file:line)
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	log.Println("Photo Backup Station WebUI - Starting...")

	// Load password from gokrazy password file
	passwordBytes, err := os.ReadFile("/etc/gokr-pw.txt")
	if err != nil {
		log.Fatalf("Failed to read password file: %v", err)
	}
	authPassword = strings.TrimSpace(string(passwordBytes))
	if authPassword == "" {
		log.Fatalf("Password file is empty")
	}

	// Get port from environment or default to 8080
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	// Initialize state manager
	stateMgr, err = state.NewManager()
	if err != nil {
		log.Fatalf("Failed to create state manager: %v", err)
	}

	// Load settings
	appSettings, err = settings.Load()
	if err != nil {
		log.Fatalf("Failed to load settings: %v", err)
	}

	// Initialize sync manager
	syncMgr = syncmanager.NewManager(
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
	wifiMgr, err = wifimanager.NewManager()
	if err != nil {
		log.Printf("Warning: Failed to initialize WiFi manager: %v", err)
	} else {
		// Log configured WiFi networks
		logConfiguredWiFiNetworks()
	}

	// Initialize CSRF token
	csrfMutex.Lock()
	csrfToken = generateCSRFToken()
	csrfMutex.Unlock()
	log.Println("CSRF protection enabled")

	// Setup HTTP handlers
	http.HandleFunc("/api/csrf-token", handleCSRFToken) // GET endpoint for CSRF token
	http.HandleFunc("/api/status", handleStatus)
	http.HandleFunc("/api/history", handleHistory)
	http.HandleFunc("/api/config", csrfProtection(handleConfig))                  // CSRF protected
	http.HandleFunc("/api/config/test", csrfProtection(handleConfigTest))          // CSRF protected
	http.HandleFunc("/api/settings", csrfProtection(handleSettings))               // CSRF protected
	http.HandleFunc("/api/devices", handleDevices)
	http.HandleFunc("/api/devices/select", csrfProtection(handleDeviceSelect))     // CSRF protected
	http.HandleFunc("/api/sync/start", csrfProtection(handleSyncStart))            // CSRF protected
	http.HandleFunc("/api/sync/cancel", csrfProtection(handleSyncCancel))          // CSRF protected
	http.HandleFunc("/api/wifi/scan", handleWiFiScan)
	http.HandleFunc("/api/wifi/networks", handleWiFiNetworks)
	http.HandleFunc("/api/wifi/connect", csrfProtection(handleWiFiConnect))        // CSRF protected
	http.HandleFunc("/api/wifi/disconnect", csrfProtection(handleWiFiDisconnect))  // CSRF protected
	http.HandleFunc("/api/wifi/status", handleWiFiStatus)
	http.HandleFunc("/api/files/cards", handleFileCards)
	http.HandleFunc("/api/files", handleFiles)
	http.HandleFunc("/api/files/paginated", handleFilesPaginated)
	http.HandleFunc("/api/files/view", handleFileView)
	http.HandleFunc("/api/thumbnail", handleThumbnail)
	http.HandleFunc("/api/sdcard/files", handleSDCardFiles)
	http.HandleFunc("/api/sdcard/preview", handleSDCardPreview)
	http.HandleFunc("/api/network/dns", handleNetworkDNS)
	http.HandleFunc("/api/network/interfaces", handleNetworkInterfaces)
	http.HandleFunc("/api/network/dns-lookup", handleDNSLookup)
	http.HandleFunc("/api/network/ping", handlePing)
	http.HandleFunc("/api/network/diagnostics", handleNetworkDiagnostics)
	http.HandleFunc("/ws", handleWebSocket)
	http.HandleFunc("/", handleIndex)

	// Wrap default mux with basic auth middleware
	handler := basicAuthMiddleware(http.DefaultServeMux)

	// Start HTTPS server
	addr := ":" + port
	certFile := "/etc/ssl/gokrazy-web.pem"
	keyFile := "/etc/ssl/gokrazy-web.key.pem"

	log.Printf("WebUI HTTPS server listening on %s", addr)
	if err := http.ListenAndServeTLS(addr, certFile, keyFile, handler); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}

// basicAuthMiddleware provides HTTP Basic Authentication
func basicAuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, password, ok := r.BasicAuth()

		// Use constant-time comparison to prevent timing attacks
		usernameMatch := subtle.ConstantTimeCompare([]byte(username), []byte("gokrazy")) == 1
		passwordMatch := subtle.ConstantTimeCompare([]byte(password), []byte(authPassword)) == 1

		if !ok || !usernameMatch || !passwordMatch {
			w.Header().Set("WWW-Authenticate", `Basic realm="Photo Backup Station"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// handleIndex serves the main web UI
func handleIndex(w http.ResponseWriter, r *http.Request) {
	html := getWebUIHTML()
	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(html))
}

// getWebUIHTML returns the complete HTML for the web UI
func getWebUIHTML() string {
	return `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Photo Backup Station</title>
    <style>
        * {
            margin: 0;
            padding: 0;
            box-sizing: border-box;
        }

        :root {
            --primary: #2563eb;
            --primary-dark: #1e40af;
            --primary-light: #dbeafe;
            --success: #16a34a;
            --success-light: #dcfce7;
            --warning: #ea580c;
            --warning-light: #fed7aa;
            --error: #dc2626;
            --error-light: #fee2e2;
            --bg: #f8fafc;
            --bg-secondary: #f1f5f9;
            --card: #ffffff;
            --text: #1e293b;
            --text-secondary: #64748b;
            --border: #e2e8f0;
            --shadow: 0 1px 3px rgba(0,0,0,0.1);
            --shadow-md: 0 4px 6px -1px rgba(0,0,0,0.1);
            --shadow-lg: 0 10px 25px rgba(0,0,0,0.1);
            --radius: 0.5rem;
            --radius-lg: 0.75rem;
        }

        [data-theme="dark"] {
            --primary: #3b82f6;
            --primary-dark: #2563eb;
            --primary-light: #1e3a8a;
            --success: #34d399;
            --success-light: #064e3b;
            --warning: #fbbf24;
            --warning-light: #78350f;
            --error: #f87171;
            --error-light: #7f1d1d;
            --bg: #0f172a;
            --bg-secondary: #1e293b;
            --card: #1e293b;
            --text: #f1f5f9;
            --text-secondary: #94a3b8;
            --border: #334155;
            --shadow: 0 1px 3px rgba(0,0,0,0.3);
            --shadow-md: 0 4px 6px -1px rgba(0,0,0,0.3);
            --shadow-lg: 0 10px 25px rgba(0,0,0,0.3);
        }

        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', 'Roboto', 'Oxygen', 'Ubuntu', sans-serif;
            background: linear-gradient(135deg, var(--bg) 0%, var(--bg-secondary) 100%);
            color: var(--text);
            line-height: 1.6;
            min-height: 100vh;
            transition: all 0.3s ease;
        }

        .header {
            background: linear-gradient(135deg, var(--card), var(--bg-secondary));
            border-bottom: 1px solid var(--border);
            padding: 1.5rem 2rem;
            box-shadow: var(--shadow-md);
            position: relative;
            overflow: hidden;
        }

        .header h1 {
            font-size: 1.5rem;
            font-weight: 700;
            color: var(--primary);
        }

        .header-content {
            display: flex;
            justify-content: space-between;
            align-items: center;
            position: relative;
            z-index: 1;
        }

        .theme-toggle {
            background: var(--card);
            border: 1px solid var(--border);
            border-radius: var(--radius);
            padding: 0.5rem 0.75rem;
            cursor: pointer;
            transition: all 0.2s ease;
            box-shadow: var(--shadow);
            color: var(--text);
            font-size: 1.25rem;
        }

        .theme-toggle #theme-icon {
            display: inline-block;
            transition: transform 0.3s ease;
        }

        .theme-toggle:hover {
            transform: scale(1.05);
            box-shadow: var(--shadow-md);
            background: var(--bg-secondary);
        }

        .container {
            max-width: 1200px;
            margin: 0 auto;
            padding: 2rem;
        }

        .tabs {
            display: flex;
            gap: 0.5rem;
            margin-bottom: 2rem;
            border-bottom: 2px solid var(--border);
            overflow-x: auto;
        }

        .tab {
            padding: 0.75rem 1.5rem;
            background: none;
            border: none;
            border-bottom: 3px solid transparent;
            cursor: pointer;
            font-size: 1rem;
            font-weight: 500;
            color: var(--text-secondary);
            transition: all 0.2s;
            white-space: nowrap;
        }

        .tab:hover {
            color: var(--primary);
            background: rgba(37, 99, 235, 0.05);
        }

        .tab.active {
            color: var(--primary);
            border-bottom-color: var(--primary);
        }

        .tab-content {
            display: none;
        }

        .tab-content.active {
            display: block;
            animation: fadeIn 0.3s;
        }

        @keyframes fadeIn {
            from { opacity: 0; transform: translateY(10px); }
            to { opacity: 1; transform: translateY(0); }
        }

        .card {
            background: var(--card);
            border-radius: var(--radius-lg);
            padding: 1.5rem;
            margin-bottom: 1.5rem;
            box-shadow: var(--shadow);
            border: 1px solid var(--border);
            transition: all 0.3s ease;
        }

        .card:hover {
            box-shadow: var(--shadow-md);
            transform: translateY(-2px);
        }

        .card h2 {
            font-size: 1.25rem;
            font-weight: 600;
            margin-bottom: 1rem;
            display: flex;
            align-items: center;
            gap: 0.5rem;
        }

        .card h2::before {
            content: '';
            width: 4px;
            height: 1.5rem;
            background: linear-gradient(180deg, var(--primary), var(--primary-dark));
            border-radius: 2px;
        }

        .status-badge {
            display: inline-block;
            padding: 0.35rem 0.85rem;
            border-radius: 9999px;
            font-size: 0.75rem;
            font-weight: 700;
            text-transform: uppercase;
            letter-spacing: 0.5px;
            position: relative;
            overflow: hidden;
        }

        .status-badge::before {
            content: '';
            position: absolute;
            top: 0;
            left: -100%;
            width: 100%;
            height: 100%;
            background: linear-gradient(90deg, transparent, rgba(255,255,255,0.3), transparent);
            animation: badgeShine 3s infinite;
        }

        @keyframes badgeShine {
            0% { left: -100%; }
            50%, 100% { left: 100%; }
        }

        .badge-idle {
            background: linear-gradient(135deg, #dbeafe, #bfdbfe);
            color: #1e40af;
            box-shadow: 0 2px 8px rgba(59, 130, 246, 0.15);
        }
        .badge-detected {
            background: linear-gradient(135deg, #fed7aa, #fdba74);
            color: #c2410c;
            box-shadow: 0 2px 8px rgba(251, 146, 60, 0.15);
        }
        .badge-syncing {
            background: linear-gradient(135deg, #fef3c7, #fde68a);
            color: #a16207;
            box-shadow: 0 2px 8px rgba(251, 191, 36, 0.15);
            animation: pulse 2s cubic-bezier(0.4, 0, 0.6, 1) infinite;
        }
        .badge-success {
            background: linear-gradient(135deg, #dcfce7, #bbf7d0);
            color: #15803d;
            box-shadow: 0 2px 8px rgba(34, 197, 94, 0.15);
        }
        .badge-error {
            background: linear-gradient(135deg, #fee2e2, #fecaca);
            color: #991b1b;
            box-shadow: 0 2px 8px rgba(239, 68, 68, 0.15);
        }

        @keyframes pulse {
            0%, 100% { opacity: 1; }
            50% { opacity: .8; }
        }

        .progress {
            width: 100%;
            height: 2.5rem;
            background: var(--bg-secondary);
            border-radius: var(--radius-lg);
            overflow: hidden;
            margin: 1rem 0;
            position: relative;
            box-shadow: inset 0 2px 4px rgba(0,0,0,0.1);
        }

        .progress-bar {
            height: 100%;
            background: linear-gradient(90deg, var(--primary), var(--primary-dark));
            transition: width 0.5s cubic-bezier(0.25, 0.46, 0.45, 0.94);
            display: flex;
            align-items: center;
            justify-content: center;
            color: white;
            font-weight: 600;
            font-size: 0.875rem;
            position: relative;
            overflow: hidden;
        }

        .progress-bar::after {
            content: '';
            position: absolute;
            top: 0;
            left: 0;
            bottom: 0;
            right: 0;
            background: linear-gradient(
                90deg,
                transparent,
                rgba(255, 255, 255, 0.2),
                transparent
            );
            animation: shimmer 2s infinite;
        }

        @keyframes shimmer {
            0% { transform: translateX(-100%); }
            100% { transform: translateX(100%); }
        }

        .info-grid {
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
            gap: 1rem;
            margin: 1rem 0;
        }

        .info-item {
            padding: 1rem;
            background: var(--bg);
            border-radius: 0.375rem;
        }

        .info-item label {
            display: block;
            font-size: 0.75rem;
            font-weight: 600;
            text-transform: uppercase;
            color: var(--text-secondary);
            margin-bottom: 0.25rem;
        }

        .info-item value {
            display: block;
            font-size: 1.125rem;
            font-weight: 600;
            color: var(--text);
        }

        .form-group {
            margin-bottom: 1.5rem;
        }

        .form-group label {
            display: block;
            font-weight: 500;
            margin-bottom: 0.5rem;
            color: var(--text);
        }

        .form-input, .form-textarea {
            width: 100%;
            padding: 0.75rem;
            border: 2px solid var(--border);
            border-radius: var(--radius);
            font-size: 1rem;
            font-family: inherit;
            background: var(--card);
            color: var(--text);
            transition: all 0.3s ease;
        }

        .form-input:focus, .form-textarea:focus {
            outline: none;
            border-color: var(--primary);
            box-shadow: 0 0 0 4px rgba(37, 99, 235, 0.1);
            transform: translateY(-1px);
        }

        .form-input:invalid {
            border-color: var(--error);
        }

        .form-input:valid:not(:placeholder-shown) {
            border-color: var(--success);
        }

        .form-input::placeholder, .form-textarea::placeholder {
            color: var(--text-secondary);
            opacity: 0.6;
        }

        .form-textarea {
            resize: vertical;
            min-height: 150px;
            font-family: 'Monaco', 'Courier New', monospace;
            font-size: 0.875rem;
        }

        .btn {
            padding: 0.75rem 1.5rem;
            border: none;
            border-radius: var(--radius);
            font-size: 1rem;
            font-weight: 500;
            cursor: pointer;
            transition: all 0.2s ease;
            position: relative;
            overflow: hidden;
            display: inline-flex;
            align-items: center;
            gap: 0.5rem;
        }

        .btn::before {
            content: '';
            position: absolute;
            top: 50%;
            left: 50%;
            width: 0;
            height: 0;
            border-radius: 50%;
            background: rgba(255, 255, 255, 0.3);
            transform: translate(-50%, -50%);
            transition: width 0.6s, height 0.6s;
        }

        .btn:active::before {
            width: 300px;
            height: 300px;
        }

        .btn-primary {
            background: linear-gradient(135deg, var(--primary), var(--primary-dark));
            color: white;
            box-shadow: 0 4px 15px rgba(37, 99, 235, 0.2);
        }

        .btn-primary:hover {
            transform: translateY(-2px);
            box-shadow: 0 6px 20px rgba(37, 99, 235, 0.3);
        }

        .btn-secondary {
            background: var(--card);
            color: var(--text);
            border: 1px solid var(--border);
        }

        .btn-secondary:hover {
            background: var(--bg-secondary);
            transform: translateY(-1px);
        }

        .btn-danger {
            background: linear-gradient(135deg, var(--error), #b91c1c);
            color: white;
        }

        .btn-danger:hover {
            transform: translateY(-2px);
            box-shadow: 0 6px 20px rgba(239, 68, 68, 0.3);
        }

        .btn:disabled {
            opacity: 0.5;
            cursor: not-allowed;
            transform: none !important;
            box-shadow: none !important;
        }

        .alert {
            padding: 1rem 1.25rem;
            border-radius: var(--radius);
            margin-bottom: 1rem;
            border-left: 4px solid;
            position: relative;
            animation: slideInRight 0.3s ease;
        }

        @keyframes slideInRight {
            from {
                opacity: 0;
                transform: translateX(-20px);
            }
            to {
                opacity: 1;
                transform: translateX(0);
            }
        }

        .alert-success {
            background: var(--success-light);
            color: var(--success);
            border-left-color: var(--success);
            box-shadow: 0 4px 12px rgba(34, 197, 94, 0.1);
        }

        .alert-error {
            background: var(--error-light);
            color: var(--error);
            border-left-color: var(--error);
            box-shadow: 0 4px 12px rgba(239, 68, 68, 0.1);
        }

        .alert-info {
            background: var(--primary-light);
            color: var(--primary);
            border-left-color: var(--primary);
            box-shadow: 0 4px 12px rgba(59, 130, 246, 0.1);
        }

        .alert-warning {
            background: var(--warning-light);
            color: var(--warning);
            border-left-color: var(--warning);
            box-shadow: 0 4px 12px rgba(245, 158, 11, 0.1);
        }

        .history-item {
            padding: 1rem;
            border-left: 4px solid var(--border);
            background: var(--bg);
            border-radius: 0.375rem;
            margin-bottom: 1rem;
        }

        .history-item.success { border-left-color: var(--success); }
        .history-item.error { border-left-color: var(--error); }

        .wifi-network {
            display: flex;
            justify-content: space-between;
            align-items: center;
            padding: 1rem;
            border: 1px solid var(--border);
            border-radius: 0.375rem;
            margin-bottom: 0.5rem;
        }

        .wifi-network:hover {
            background: var(--bg);
        }

        .signal-strength {
            display: flex;
            gap: 2px;
            align-items: flex-end;
        }

        .signal-bar {
            width: 4px;
            background: var(--text-secondary);
            border-radius: 2px;
        }

        .signal-bar:nth-child(1) { height: 6px; }
        .signal-bar:nth-child(2) { height: 10px; }
        .signal-bar:nth-child(3) { height: 14px; }
        .signal-bar:nth-child(4) { height: 18px; }

        .signal-strong .signal-bar { background: var(--success); }
        .signal-medium .signal-bar:nth-child(-n+3) { background: var(--warning); }
        .signal-weak .signal-bar:nth-child(-n+2) { background: var(--error); }

        .loading {
            text-align: center;
            padding: 3rem;
            color: var(--text-secondary);
        }

        .spinner {
            position: relative;
            width: 50px;
            height: 50px;
            margin: 0 auto 1.5rem;
        }

        .spinner::before,
        .spinner::after {
            content: '';
            position: absolute;
            border: 3px solid transparent;
            border-top-color: var(--primary);
            border-right-color: var(--primary);
            border-radius: 50%;
            animation: spin 1.5s cubic-bezier(0.68, -0.55, 0.265, 1.55) infinite;
        }

        .spinner::before {
            width: 100%;
            height: 100%;
            animation-duration: 1.5s;
        }

        .spinner::after {
            width: 70%;
            height: 70%;
            top: 15%;
            left: 15%;
            animation-duration: 2s;
            animation-direction: reverse;
            border-top-color: var(--primary-dark);
            border-right-color: var(--primary-dark);
        }

        @keyframes spin {
            0% { transform: rotate(0deg); }
            100% { transform: rotate(360deg); }
        }

        @media (max-width: 768px) {
            .container {
                padding: 1rem;
            }

            .header {
                padding: 1rem;
            }

            .info-grid {
                grid-template-columns: 1fr;
            }
        }

        .code {
            font-family: 'Monaco', 'Courier New', monospace;
            background: var(--bg);
            padding: 0.125rem 0.375rem;
            border-radius: 0.25rem;
            font-size: 0.875rem;
        }

        /* Gallery Styles */
        .gallery-grid {
            display: grid;
            grid-template-columns: repeat(auto-fill, minmax(200px, 1fr));
            gap: 1rem;
            margin-top: 1rem;
        }

        .gallery-item {
            position: relative;
            aspect-ratio: 1;
            overflow: hidden;
            border-radius: 0.5rem;
            cursor: pointer;
            background: var(--bg);
            transition: transform 0.2s, box-shadow 0.2s;
        }

        .gallery-item:hover {
            transform: translateY(-4px);
            box-shadow: 0 8px 16px rgba(0, 0, 0, 0.1);
        }

        .gallery-item img {
            width: 100%;
            height: 100%;
            object-fit: cover;
        }

        .gallery-item-info {
            position: absolute;
            bottom: 0;
            left: 0;
            right: 0;
            background: linear-gradient(to top, rgba(0, 0, 0, 0.8), transparent);
            color: white;
            padding: 0.75rem;
            font-size: 0.75rem;
            opacity: 0;
            transition: opacity 0.2s;
        }

        .gallery-item:hover .gallery-item-info {
            opacity: 1;
        }

        /* Lightbox Styles */
        .lightbox {
            display: none;
            position: fixed;
            top: 0;
            left: 0;
            right: 0;
            bottom: 0;
            background: rgba(0, 0, 0, 0.95);
            z-index: 9999;
            animation: fadeIn 0.2s;
        }

        .lightbox.active {
            display: flex;
            flex-direction: column;
            align-items: center;
            justify-content: center;
        }

        .lightbox-content {
            position: relative;
            max-width: 95vw;
            max-height: 85vh;
            display: flex;
            align-items: center;
            justify-content: center;
        }

        .lightbox-image {
            max-width: 100%;
            max-height: 85vh;
            object-fit: contain;
        }

        .lightbox-controls {
            position: absolute;
            top: 1rem;
            right: 1rem;
            display: flex;
            gap: 0.5rem;
        }

        .lightbox-btn {
            background: rgba(255, 255, 255, 0.9);
            border: none;
            width: 40px;
            height: 40px;
            border-radius: 50%;
            cursor: pointer;
            font-size: 1.25rem;
            display: flex;
            align-items: center;
            justify-content: center;
            transition: background 0.2s;
        }

        .lightbox-btn:hover {
            background: white;
        }

        .lightbox-nav {
            position: absolute;
            top: 50%;
            transform: translateY(-50%);
            background: rgba(255, 255, 255, 0.9);
            border: none;
            width: 50px;
            height: 50px;
            border-radius: 50%;
            cursor: pointer;
            font-size: 1.5rem;
            display: flex;
            align-items: center;
            justify-content: center;
            transition: background 0.2s;
        }

        .lightbox-nav:hover {
            background: white;
        }

        .lightbox-nav.prev {
            left: 2rem;
        }

        .lightbox-nav.next {
            right: 2rem;
        }

        .lightbox-info {
            background: rgba(0, 0, 0, 0.8);
            color: white;
            padding: 1rem 2rem;
            border-radius: 0.5rem;
            margin-top: 1rem;
            max-width: 800px;
            width: 90vw;
        }

        .lightbox-exif {
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(150px, 1fr));
            gap: 0.75rem;
            margin-top: 0.75rem;
        }

        .lightbox-exif-item {
            font-size: 0.875rem;
        }

        .lightbox-exif-label {
            color: #aaa;
            font-size: 0.75rem;
        }

        .gallery-path-nav {
            display: flex;
            align-items: center;
            gap: 0.5rem;
            margin-bottom: 1rem;
            flex-wrap: wrap;
        }

        .path-segment {
            cursor: pointer;
            color: var(--primary);
            text-decoration: none;
        }

        .path-segment:hover {
            text-decoration: underline;
        }

        @keyframes fadeIn {
            from { opacity: 0; }
            to { opacity: 1; }
        }

        @media (max-width: 768px) {
            .gallery-grid {
                grid-template-columns: repeat(auto-fill, minmax(120px, 1fr));
                gap: 0.5rem;
            }

            .lightbox-nav {
                width: 40px;
                height: 40px;
            }

            .lightbox-nav.prev {
                left: 0.5rem;
            }

            .lightbox-nav.next {
                right: 0.5rem;
            }

            .lightbox-info {
                padding: 0.75rem;
                font-size: 0.875rem;
            }

            .lightbox-exif {
                grid-template-columns: repeat(auto-fit, minmax(120px, 1fr));
                gap: 0.5rem;
            }
        }
    </style>
</head>
<body>
    <div class="header">
        <div class="header-content">
            <div>
                <h1>📸 Photo Backup Station</h1>
                <p>Automated photo backup system for your Raspberry Pi</p>
            </div>
            <button class="theme-toggle" onclick="toggleTheme()" title="Toggle dark mode">
                <span id="theme-icon">🌙</span>
            </button>
        </div>
    </div>

    <div class="container">
        <div class="tabs">
            <button class="tab active" onclick="switchTab('status')">📊 Status</button>
            <button class="tab" onclick="switchTab('devices')">💾 Devices</button>
            <button class="tab" onclick="switchTab('gallery')">🖼️ Gallery</button>
            <button class="tab" onclick="switchTab('history')">📚 History</button>
            <button class="tab" onclick="switchTab('files')">📁 Files</button>
            <button class="tab" onclick="switchTab('wifi')">📡 WiFi</button>
            <button class="tab" onclick="switchTab('network')">🔧 Network Debug</button>
            <button class="tab" onclick="switchTab('config')">⚙️ Configuration</button>
        </div>

        <!-- Status Tab -->
        <div id="status-tab" class="tab-content active">
            <div class="card">
                <h2>Current Status</h2>
                <div id="status-display">
                    <div class="loading">
                        <div class="spinner"></div>
                        <p>Loading status...</p>
                    </div>
                </div>
                <div id="sync-controls" style="margin-top: 1rem; display: none;">
                    <button id="start-sync-btn" class="btn btn-primary" onclick="startSync()">▶️ Start Sync</button>
                    <button id="cancel-sync-btn" class="btn btn-secondary" onclick="cancelSync()" style="display: none;">⏹️ Cancel Sync</button>
                </div>
            </div>

            <div class="card" id="sync-details" style="display: none;">
                <h2>Sync Progress</h2>
                <div id="sync-progress"></div>
            </div>
        </div>

        <!-- Devices Tab -->
        <div id="devices-tab" class="tab-content">
            <div class="card">
                <h2>Available Storage Devices</h2>
                <p style="color: var(--text-secondary); margin-bottom: 1rem;">
                    These are all detected storage devices on the system. Select one to sync manually if needed.
                </p>
                <button class="btn btn-primary" onclick="refreshDevices()">🔄 Refresh Devices</button>
                <div id="devices-list" style="margin-top: 1rem;">
                    <div class="loading">
                        <div class="spinner"></div>
                        <p>Loading devices...</p>
                    </div>
                </div>
            </div>
        </div>

        <!-- Gallery Tab -->
        <div id="gallery-tab" class="tab-content">
            <div class="card">
                <h2>📸 SD Card Gallery</h2>
                <p style="color: var(--text-secondary); margin-bottom: 1rem;">
                    Browse and preview photos from your SD card with EXIF metadata.
                </p>

                <div class="gallery-path-nav" id="gallery-path-nav">
                    <button class="btn btn-secondary" onclick="loadGallery('')">🏠 Root</button>
                    <button class="btn btn-secondary" onclick="loadGallery('DCIM')">📁 DCIM</button>
                    <button class="btn btn-secondary" onclick="refreshGallery()">🔄 Refresh</button>
                    <span id="gallery-current-path" class="code" style="margin-left: auto;"></span>
                </div>

                <div id="gallery-alert" style="margin-bottom: 1rem;"></div>

                <div id="gallery-display">
                    <div class="loading">
                        <div class="spinner"></div>
                        <p>Loading gallery...</p>
                    </div>
                </div>
            </div>
        </div>

        <!-- Lightbox -->
        <div id="lightbox" class="lightbox">
            <div class="lightbox-controls">
                <button class="lightbox-btn" onclick="closeLightbox()" title="Close">✕</button>
            </div>
            <button class="lightbox-nav prev" onclick="navigateLightbox(-1)">‹</button>
            <button class="lightbox-nav next" onclick="navigateLightbox(1)">›</button>
            <div class="lightbox-content">
                <img id="lightbox-img" class="lightbox-image" src="" alt="">
            </div>
            <div class="lightbox-info" id="lightbox-info"></div>
        </div>

        <!-- History Tab -->
        <div id="history-tab" class="tab-content">
            <div class="card">
                <h2>Sync History</h2>
                <div id="history-display">
                    <div class="loading">
                        <div class="spinner"></div>
                        <p>Loading history...</p>
                    </div>
                </div>
            </div>
        </div>

        <!-- WiFi Tab -->
        <div id="wifi-tab" class="tab-content">
            <div class="card">
                <h2>WiFi Status</h2>
                <div id="wifi-status-display"></div>
            </div>

            <div class="card">
                <h2>Available Networks</h2>
                <button class="btn btn-primary" onclick="scanWiFi()">🔍 Scan Networks</button>
                <div id="wifi-networks" style="margin-top: 1rem;"></div>
            </div>

            <div class="card">
                <h2>Add Network Manually</h2>
                <form onsubmit="addNetworkManually(event)">
                    <div class="form-group">
                        <label for="manual-ssid">SSID (Network Name)</label>
                        <input type="text" id="manual-ssid" class="form-input" placeholder="Enter network name" required>
                    </div>
                    <div class="form-group">
                        <label for="manual-password">Password</label>
                        <input type="password" id="manual-password" class="form-input" placeholder="Enter password (leave empty for open networks)">
                    </div>
                    <button type="submit" class="btn btn-primary">➕ Add Network</button>
                </form>
            </div>

            <div class="card">
                <h2>Saved Networks</h2>
                <div id="saved-networks"></div>
            </div>
        </div>

        <!-- Network Debug Tab -->
        <div id="network-tab" class="tab-content">
            <div class="card">
                <h2>Network Configuration</h2>
                <div id="network-config">
                    <div class="loading">
                        <div class="spinner"></div>
                        <p>Loading network configuration...</p>
                    </div>
                </div>
            </div>

            <div class="card">
                <h2>Network Interfaces</h2>
                <button class="btn btn-primary" onclick="refreshNetworkInfo()">🔄 Refresh</button>
                <div id="network-interfaces" style="margin-top: 1rem;">
                    <div class="loading">
                        <div class="spinner"></div>
                        <p>Loading interfaces...</p>
                    </div>
                </div>
            </div>

            <div class="card">
                <h2>DNS Resolution Test</h2>
                <form onsubmit="testDNS(event)">
                    <div class="form-group">
                        <label for="dns-hostname">Hostname to resolve</label>
                        <input type="text" id="dns-hostname" class="form-input" placeholder="example.com" value="google.com" required>
                    </div>
                    <button type="submit" class="btn btn-primary">🔍 Resolve DNS</button>
                </form>
                <div id="dns-result" style="margin-top: 1rem;"></div>
            </div>

            <div class="card">
                <h2>Ping Test</h2>
                <p style="color: var(--text-secondary); margin-bottom: 1rem; font-size: 0.875rem;">
                    Sends ICMP echo requests to test network reachability and latency
                </p>
                <form onsubmit="testPing(event)">
                    <div class="form-group">
                        <label for="ping-hostname">Host to ping</label>
                        <input type="text" id="ping-hostname" class="form-input" placeholder="8.8.8.8" value="8.8.8.8" required>
                    </div>
                    <div class="form-group">
                        <label for="ping-count">Number of pings</label>
                        <input type="number" id="ping-count" class="form-input" value="4" min="1" max="10" required>
                    </div>
                    <button type="submit" class="btn btn-primary">📡 Ping</button>
                </form>
                <div id="ping-result" style="margin-top: 1rem;"></div>
            </div>

            <div class="card">
                <h2>Connectivity Tests</h2>
                <button class="btn btn-primary" onclick="runFullDiagnostics()">🔍 Run Full Diagnostics</button>
                <div id="diagnostics-result" style="margin-top: 1rem;"></div>
            </div>
        </div>

        <!-- Configuration Tab -->
        <div id="config-tab" class="tab-content">
            <div class="card">
                <h2>Rclone Configuration</h2>
                <div id="config-alert"></div>
                <form onsubmit="saveConfig(event)">
                    <div class="form-group">
                        <label for="rclone-config">Rclone Config (INI format)</label>
                        <textarea id="rclone-config" class="form-textarea" placeholder="[remote-name]
type = b2
account = ...
key = ..."></textarea>
                    </div>
                    <button type="submit" class="btn btn-primary">💾 Save Configuration</button>
                    <button type="button" class="btn btn-secondary" onclick="testConfig()">🔌 Test Connection</button>
                </form>
            </div>

            <div class="card">
                <h2>Settings</h2>
                <form onsubmit="saveSettings(event)">
                    <div class="form-group">
                        <label for="remote-name">Remote Name</label>
                        <input type="text" id="remote-name" class="form-input" placeholder="e.g., b2backup">
                    </div>
                    <div class="form-group">
                        <label for="remote-path">Remote Path</label>
                        <input type="text" id="remote-path" class="form-input" placeholder="e.g., /photos">
                    </div>
                    <div class="form-group">
                        <label for="reformat-threshold">Reformat Detection Threshold (%)</label>
                        <input type="number" id="reformat-threshold" class="form-input" placeholder="30" min="1" max="100" step="1">
                    </div>
                    <div class="form-group">
                        <label for="transfers">Parallel Transfers</label>
                        <input type="number" id="transfers" class="form-input" placeholder="4" min="1" max="16" step="1" title="Number of files to upload simultaneously (default: 4)">
                        <p style="font-size: 0.875rem; color: var(--text-secondary); margin-top: 0.25rem;">Higher values = faster uploads but more bandwidth/memory usage</p>
                    </div>
                    <div class="form-group">
                        <label for="checkers">Parallel File Checkers</label>
                        <input type="number" id="checkers" class="form-input" placeholder="8" min="1" max="32" step="1" title="Number of file integrity checkers (default: 8)">
                        <p style="font-size: 0.875rem; color: var(--text-secondary); margin-top: 0.25rem;">Determines how many files are compared in parallel</p>
                    </div>
                    <button type="submit" class="btn btn-primary">💾 Save Settings</button>
                </form>
            </div>

            <div class="card">
                <h2>Google Photos Upload (Optional)</h2>
                <p style="color: var(--text-secondary); margin-bottom: 1rem;">
                    Optionally upload JPG files to Google Photos after syncing to your primary remote.
                    You must configure a Google Photos remote in rclone (using Google Drive backend) before enabling this feature.
                </p>
                <form onsubmit="saveSettings(event)">
                    <div class="form-group">
                        <label style="display: flex; align-items: center; gap: 0.5rem; cursor: pointer;">
                            <input type="checkbox" id="google-photos-enabled" style="width: auto; cursor: pointer;">
                            <span>Enable Google Photos Upload</span>
                        </label>
                        <p style="font-size: 0.875rem; color: var(--text-secondary); margin-top: 0.25rem;">When enabled, JPG files will be uploaded to Google Photos after the main sync completes</p>
                    </div>
                    <div class="form-group">
                        <label for="google-photos-remote">Google Photos Remote Name</label>
                        <input type="text" id="google-photos-remote" class="form-input" placeholder="e.g., googlephotos">
                        <p style="font-size: 0.875rem; color: var(--text-secondary); margin-top: 0.25rem;">The name of your Google Photos rclone remote (configured via rclone config)</p>
                    </div>
                    <button type="submit" class="btn btn-primary">💾 Save Google Photos Settings</button>
                </form>
            </div>
        </div>

        <!-- Files Tab -->
        <div id="files-tab" class="tab-content">
            <div class="card">
                <h2>📁 Remote Files Browser</h2>
                <div id="files-alert"></div>

                <!-- View Selector -->
                <div style="display: flex; gap: 0.5rem; margin-bottom: 1rem; flex-wrap: wrap;">
                    <button class="btn btn-secondary" id="view-cards-btn" onclick="switchFilesView('cards')">
                        🗂️ Browse by Card
                    </button>
                    <button class="btn btn-secondary" id="view-folder-btn" onclick="switchFilesView('folder')">
                        📂 Browse by Folder
                    </button>
                    <button class="btn btn-secondary" onclick="refreshFilesView()">🔄 Refresh</button>
                </div>

                <!-- Cards View -->
                <div id="files-cards-view" style="display: none;">
                    <p style="color: var(--text-secondary); margin-bottom: 1rem;">
                        Browse photos organized by SD card. Each card represents a unique SD card that has been synced.
                    </p>
                    <div id="cards-list">
                        <div class="loading">
                            <div class="spinner"></div>
                            <p>Loading cards...</p>
                        </div>
                    </div>
                </div>

                <!-- Folder View -->
                <div id="files-folder-view" style="display: none;">
                    <!-- Breadcrumb Navigation -->
                    <div style="display: flex; align-items: center; gap: 0.5rem; margin-bottom: 1rem; flex-wrap: wrap;">
                        <span style="color: var(--text-secondary); font-size: 0.875rem;">📍 Path:</span>
                        <div id="breadcrumb-nav" style="display: flex; align-items: center; gap: 0.25rem; flex-wrap: wrap;"></div>
                    </div>

                    <!-- Search Box -->
                    <div style="margin-bottom: 1rem;">
                        <input
                            type="text"
                            id="files-search"
                            placeholder="🔍 Search files..."
                            style="width: 100%; padding: 0.75rem; border: 1px solid var(--border); border-radius: 6px; background: var(--bg); color: var(--text);"
                            oninput="filterFiles()"
                        />
                    </div>

                    <!-- Files Display -->
                    <div id="files-display">
                        <div class="loading">
                            <div class="spinner"></div>
                            <p>Loading files...</p>
                        </div>
                    </div>

                    <!-- Pagination Controls -->
                    <div id="pagination-controls" style="display: none; margin-top: 1rem; text-align: center;">
                        <div style="display: inline-flex; align-items: center; gap: 0.5rem; flex-wrap: wrap;">
                            <button class="btn btn-secondary" id="prev-page-btn" onclick="loadPrevPage()">← Previous</button>
                            <span id="page-info" style="padding: 0 1rem; color: var(--text-secondary);"></span>
                            <button class="btn btn-secondary" id="next-page-btn" onclick="loadNextPage()">Next →</button>
                        </div>
                        <div style="margin-top: 0.5rem; color: var(--text-secondary); font-size: 0.875rem;">
                            <span id="items-info"></span>
                        </div>
                    </div>
                </div>
            </div>
        </div>
    </div>

    <script>
        let ws;
        let reconnectInterval;
        let currentPath = '';

        // Initialize theme on page load
        document.addEventListener('DOMContentLoaded', () => {
            const savedTheme = localStorage.getItem('theme') || 'light';
            if (savedTheme === 'dark') {
                document.documentElement.setAttribute('data-theme', 'dark');
                document.getElementById('theme-icon').textContent = '☀️';
            }
        });

        // Theme toggle function
        function toggleTheme() {
            const currentTheme = document.documentElement.getAttribute('data-theme');
            const newTheme = currentTheme === 'dark' ? 'light' : 'dark';

            document.documentElement.setAttribute('data-theme', newTheme);
            localStorage.setItem('theme', newTheme);

            const icon = document.getElementById('theme-icon');
            icon.textContent = newTheme === 'dark' ? '☀️' : '🌙';

            // Add a subtle animation
            icon.style.transform = 'rotate(360deg)';
            setTimeout(() => {
                icon.style.transform = 'rotate(0deg)';
            }, 300);
        }

        // Tab switching
        function switchTab(tabName) {
            document.querySelectorAll('.tab').forEach(tab => tab.classList.remove('active'));
            document.querySelectorAll('.tab-content').forEach(content => content.classList.remove('active'));

            event.target.classList.add('active');
            document.getElementById(tabName + '-tab').classList.add('active');

            // Load data for the selected tab
            if (tabName === 'devices') refreshDevices();
            if (tabName === 'gallery') loadGallery('DCIM');
            if (tabName === 'history') loadHistory();
            if (tabName === 'files') switchFilesView('cards');
            if (tabName === 'wifi') loadWiFiStatus();
            if (tabName === 'network') loadNetworkInfo();
            if (tabName === 'config') loadConfig();
        }

        // WebSocket connection
        function connectWebSocket() {
            const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
            const wsUrl = protocol + '//' + window.location.host + '/ws';

            ws = new WebSocket(wsUrl);

            ws.onopen = () => {
                console.log('WebSocket connected');
                clearInterval(reconnectInterval);
            };

            ws.onmessage = (event) => {
                const data = JSON.parse(event.data);
                updateStatus(data);
            };

            ws.onerror = (error) => {
                console.error('WebSocket error:', error);
            };

            ws.onclose = () => {
                console.log('WebSocket disconnected, reconnecting...');
                reconnectInterval = setInterval(() => {
                    connectWebSocket();
                }, 5000);
            };
        }

        // Update status display
        function updateStatus(data) {
            const statusDiv = document.getElementById('status-display');
            const syncDetails = document.getElementById('sync-details');
            const syncControls = document.getElementById('sync-controls');
            const startBtn = document.getElementById('start-sync-btn');
            const cancelBtn = document.getElementById('cancel-sync-btn');

            let badgeClass = 'badge-' + data.status;
            let statusText = data.status.toUpperCase();

            let html = '<div class="info-grid">';
            html += '<div class="info-item"><label>Status</label><value><span class="status-badge ' + badgeClass + '">' + statusText + '</span></value></div>';
            html += '<div class="info-item"><label>SD Card</label><value>' + (data.sdcard_mounted ? '✓ Mounted' : '✗ Not mounted') + '</value></div>';

            if (data.sdcard_path) {
                html += '<div class="info-item"><label>Mount Path</label><value class="code">' + escapeHtml(data.sdcard_path) + '</value></div>';
            }

            if (data.card_id) {
                html += '<div class="info-item"><label>Card ID</label><value class="code">' + escapeHtml(data.card_id) + '</value></div>';
            }

            html += '</div>';

            statusDiv.innerHTML = html;

            // Show/hide sync controls based on state
            if (data.sdcard_mounted) {
                syncControls.style.display = 'block';

                if (data.status === 'syncing') {
                    startBtn.style.display = 'none';
                    cancelBtn.style.display = 'inline-block';
                } else {
                    startBtn.style.display = 'inline-block';
                    cancelBtn.style.display = 'none';
                }
            } else {
                syncControls.style.display = 'none';
            }

            // Show sync progress if syncing
            if (data.current_sync && data.status === 'syncing') {
                syncDetails.style.display = 'block';
                updateSyncProgress(data.current_sync);
            } else {
                syncDetails.style.display = 'none';
            }
        }

        // Start manual sync
        function startSync() {
            if (!confirm('Start syncing photos from SD card to cloud?')) return;

            fetch('/api/sync/start', { method: 'POST' })
                .then(r => {
                    if (!r.ok) {
                        return r.text().then(text => {
                            throw new Error(text || 'Failed to start sync');
                        });
                    }
                    return r.json();
                })
                .then(data => {
                    console.log('Sync started:', data);
                })
                .catch(err => alert('Failed to start sync: ' + err.message));
        }

        // Cancel current sync
        function cancelSync() {
            if (!confirm('Cancel the current sync operation?')) return;

            fetch('/api/sync/cancel', { method: 'POST' })
                .then(r => {
                    if (!r.ok) {
                        return r.text().then(text => {
                            throw new Error(text || 'Failed to cancel sync');
                        });
                    }
                    return r.json();
                })
                .then(data => {
                    console.log('Sync cancelled:', data);
                })
                .catch(err => alert('Failed to cancel sync: ' + err.message));
        }

        function updateSyncProgress(sync) {
            const progressDiv = document.getElementById('sync-progress');
            const percent = sync.files_total > 0 ? (sync.files_synced / sync.files_total * 100) : 0;
            const bytesPercent = sync.bytes_total > 0 ? (sync.bytes_synced / sync.bytes_total * 100) : 0;

            // Main progress bar showing file count
            let html = '<div style="margin-bottom: 0.5rem;">';
            html += '<div style="display: flex; justify-content: space-between; align-items: center; margin-bottom: 0.5rem;">';
            html += '<label style="font-size: 0.875rem; font-weight: 600; color: var(--text-secondary);">File Progress</label>';
            html += '<span style="font-size: 0.875rem; font-weight: 600; color: var(--text);">' + sync.files_synced + ' / ' + sync.files_total + ' files</span>';
            html += '</div>';
            html += '<div class="progress">';
            html += '<div class="progress-bar" style="width: ' + percent.toFixed(1) + '%">' + percent.toFixed(1) + '%</div>';
            html += '</div>';
            html += '</div>';

            // Data transfer progress bar
            html += '<div style="margin-bottom: 1.5rem;">';
            html += '<div style="display: flex; justify-content: space-between; align-items: center; margin-bottom: 0.5rem;">';
            html += '<label style="font-size: 0.875rem; font-weight: 600; color: var(--text-secondary);">Data Transfer</label>';
            html += '<span style="font-size: 0.875rem; font-weight: 600; color: var(--text);">' + formatBytes(sync.bytes_synced) + ' / ' + formatBytes(sync.bytes_total) + '</span>';
            html += '</div>';
            html += '<div class="progress">';
            html += '<div class="progress-bar" style="width: ' + bytesPercent.toFixed(1) + '%">' + bytesPercent.toFixed(1) + '%</div>';
            html += '</div>';
            html += '</div>';

            // Stats grid
            html += '<div class="info-grid">';

            if (sync.transfer_speed && sync.transfer_speed > 0) {
                html += '<div class="info-item"><label>Transfer Speed</label><value>' + formatBytes(sync.transfer_speed) + '/s</value></div>';
            }

            if (sync.eta && sync.eta !== '') {
                html += '<div class="info-item"><label>Time Remaining</label><value>' + sync.eta + '</value></div>';
            }

            // Show start time if available
            if (sync.start_time) {
                const startTime = new Date(sync.start_time);
                const elapsed = Math.floor((Date.now() - startTime.getTime()) / 1000);
                html += '<div class="info-item"><label>Elapsed Time</label><value>' + formatDuration(elapsed * 1000) + '</value></div>';
            }

            html += '</div>';

            // Show current file being synced
            if (sync.current_file) {
                html += '<div style="margin-top: 1.5rem; padding: 1.5rem; background: linear-gradient(135deg, #f0f9ff 0%, #e0f2fe 100%); border-radius: 0.5rem; border: 2px solid var(--primary); box-shadow: var(--shadow-lg);">';
                html += '<label style="display: block; font-size: 0.875rem; font-weight: 700; text-transform: uppercase; color: var(--primary); margin-bottom: 1rem; letter-spacing: 0.05em;">⚡ Currently Syncing</label>';

                // Show thumbnail if it's a JPEG
                const fileName = sync.current_file.split('/').pop();
                const isJpeg = fileName.toLowerCase().endsWith('.jpg') || fileName.toLowerCase().endsWith('.jpeg');

                html += '<div style="display: flex; gap: 1.5rem; align-items: center;">';

                if (isJpeg) {
                    const thumbnailUrl = '/api/thumbnail?path=' + encodeURIComponent(sync.current_file);
                    html += '<div style="position: relative; flex-shrink: 0;">';
                    html += '<img src="' + thumbnailUrl + '" alt="Preview" style="width: 120px; height: 120px; object-fit: cover; border-radius: 0.5rem; border: 3px solid white; box-shadow: 0 4px 12px rgba(0,0,0,0.15);" onerror="this.style.display=\'none\'">';
                    html += '<div style="position: absolute; bottom: -8px; right: -8px; width: 32px; height: 32px; background: var(--primary); border-radius: 50%; border: 3px solid white; display: flex; align-items: center; justify-content: center; box-shadow: 0 2px 8px rgba(0,0,0,0.2);">';
                    html += '<span style="color: white; font-size: 1rem;">📷</span>';
                    html += '</div>';
                    html += '</div>';
                } else {
                    // Show generic file icon for non-JPEG files
                    html += '<div style="width: 120px; height: 120px; background: linear-gradient(135deg, #dbeafe 0%, #bfdbfe 100%); border-radius: 0.5rem; border: 3px solid white; box-shadow: 0 4px 12px rgba(0,0,0,0.15); display: flex; align-items: center; justify-content: center; flex-shrink: 0;">';
                    html += '<span style="font-size: 3rem;">📄</span>';
                    html += '</div>';
                }

                html += '<div style="flex: 1; min-width: 0;">';
                html += '<div style="font-weight: 700; font-size: 1.125rem; color: var(--text); margin-bottom: 0.5rem; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;" title="' + escapeHtml(fileName) + '">' + escapeHtml(fileName) + '</div>';
                html += '<div style="font-size: 1rem; color: var(--text-secondary); font-weight: 500;">';
                html += '<span style="display: inline-block; padding: 0.25rem 0.75rem; background: white; border-radius: 0.25rem; box-shadow: 0 1px 3px rgba(0,0,0,0.1);">';
                html += '📦 ' + formatBytes(sync.current_file_size);
                html += '</span>';
                html += '</div>';

                // Show file progress bar if we have current file size info
                if (sync.current_file_size > 0 && sync.bytes_synced > 0) {
                    const filePercent = Math.min(100, (sync.bytes_synced / sync.current_file_size) * 100);
                    if (filePercent > 0) {
                        html += '<div style="margin-top: 1rem;">';
                        html += '<div style="font-size: 0.75rem; font-weight: 600; color: var(--text-secondary); margin-bottom: 0.25rem;">File Transfer Progress</div>';
                        html += '<div class="progress" style="height: 0.5rem;">';
                        html += '<div class="progress-bar" style="width: ' + filePercent.toFixed(1) + '%"></div>';
                        html += '</div>';
                        html += '</div>';
                    }
                }
                html += '</div>';

                html += '</div>';
                html += '</div>';
            }

            progressDiv.innerHTML = html;
        }

        // Load history
        function loadHistory() {
            fetch('/api/history')
                .then(r => r.json())
                .then(data => {
                    const historyDiv = document.getElementById('history-display');

                    if (!data || data.length === 0) {
                        historyDiv.innerHTML = '<p class="loading">No sync history yet</p>';
                        return;
                    }

                    let html = '';
                    data.forEach(item => {
                        const statusClass = item.error ? 'error' : 'success';
                        const statusIcon = item.error ? '❌' : '✓';

                        // Use start_time for the timestamp
                        const timestamp = item.start_time ? new Date(item.start_time).toLocaleString() : 'N/A';

                        html += '<div class="history-item ' + statusClass + '">';
                        html += '<div style="display: flex; justify-content: space-between; margin-bottom: 0.5rem;">';
                        html += '<strong>' + statusIcon + ' ' + timestamp + '</strong>';
                        html += '<span class="code">' + (item.card_id || 'N/A') + '</span>';
                        html += '</div>';

                        if (item.files_synced) {
                            html += '<p>Files: ' + item.files_synced + ' (' + formatBytes(item.bytes_synced || 0) + ')';
                            // Calculate duration from start_time and end_time
                            if (item.start_time && item.end_time) {
                                const durationMs = new Date(item.end_time) - new Date(item.start_time);
                                html += ' in ' + formatDuration(durationMs);
                            }
                            html += '</p>';
                        }

                        if (item.error) {
                            html += '<p style="color: var(--error); margin-top: 0.5rem;">Error: ' + item.error + '</p>';
                        }

                        html += '</div>';
                    });

                    historyDiv.innerHTML = html;
                })
                .catch(err => {
                    document.getElementById('history-display').innerHTML = '<p class="alert alert-error">Failed to load history: ' + err.message + '</p>';
                });
        }

        // WiFi functions
        function loadWiFiStatus() {
            fetch('/api/wifi/status')
                .then(r => r.json())
                .then(data => {
                    const statusDiv = document.getElementById('wifi-status-display');
                    if (data.connected) {
                        statusDiv.innerHTML = '<p class="alert alert-success">✓ Connected to: <strong>' + data.ssid + '</strong></p>';
                    } else {
                        statusDiv.innerHTML = '<p class="alert alert-info">Not connected to any network</p>';
                    }
                })
                .catch(err => {
                    document.getElementById('wifi-status-display').innerHTML = '<p class="alert alert-error">Failed to get WiFi status</p>';
                });

            loadSavedNetworks();
        }

        function scanWiFi() {
            const networksDiv = document.getElementById('wifi-networks');
            networksDiv.innerHTML = '<div class="loading"><div class="spinner"></div><p>Scanning...</p></div>';

            fetch('/api/wifi/scan', { method: 'POST' })
                .then(r => {
                    if (!r.ok) {
                        return r.text().then(text => {
                            throw new Error(text || 'Network scan failed');
                        });
                    }
                    return r.json();
                })
                .then(networks => {
                    if (!networks || networks.length === 0) {
                        networksDiv.innerHTML = '<p class="loading">No networks found</p>';
                        return;
                    }

                    let html = '';
                    networks.forEach(network => {
                        const signalStrength = getSignalStrength(network.signal);
                        html += '<div class="wifi-network">';
                        html += '<div><strong>' + escapeHtml(network.ssid) + '</strong>';
                        if (network.encrypted) html += ' 🔒';
                        html += '</div>';
                        html += '<div style="display: flex; gap: 1rem; align-items: center;">';
                        html += '<div class="signal-strength signal-' + signalStrength + '">';
                        html += '<div class="signal-bar"></div><div class="signal-bar"></div><div class="signal-bar"></div><div class="signal-bar"></div>';
                        html += '</div>';
                        html += '<button class="btn btn-primary" onclick="connectWiFi(\'' + escapeHtml(network.ssid) + '\', ' + network.encrypted + ')">Connect</button>';
                        html += '</div></div>';
                    });

                    networksDiv.innerHTML = html;
                })
                .catch(err => {
                    networksDiv.innerHTML = '<p class="alert alert-error">Scan failed: ' + err.message + '</p>';
                });
        }

        function connectWiFi(ssid, encrypted) {
            let password = '';
            if (encrypted !== false) {
                password = prompt('Enter password for ' + ssid + ':');
                if (password === null) return; // User cancelled
            }

            fetch('/api/wifi/connect', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ ssid: ssid, password: password })
            })
            .then(r => {
                if (!r.ok) {
                    return r.text().then(text => {
                        throw new Error(text || 'Failed to add network');
                    });
                }
                return r.json();
            })
            .then(data => {
                alert('WiFi network added successfully!');
                loadSavedNetworks();
            })
            .catch(err => alert('Failed to connect: ' + err.message));
        }

        function addNetworkManually(event) {
            event.preventDefault();

            const ssid = document.getElementById('manual-ssid').value;
            const password = document.getElementById('manual-password').value;

            fetch('/api/wifi/connect', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ ssid: ssid, password: password })
            })
            .then(r => {
                if (!r.ok) {
                    return r.text().then(text => {
                        throw new Error(text || 'Failed to add network');
                    });
                }
                return r.json();
            })
            .then(data => {
                alert('Network added successfully!');
                document.getElementById('manual-ssid').value = '';
                document.getElementById('manual-password').value = '';
                loadSavedNetworks();
            })
            .catch(err => alert('Failed to add network: ' + err.message));
        }

        function loadSavedNetworks() {
            fetch('/api/wifi/networks')
                .then(r => r.json())
                .then(networks => {
                    const div = document.getElementById('saved-networks');

                    if (!networks || networks.length === 0) {
                        div.innerHTML = '<p class="loading">No saved networks</p>';
                        return;
                    }

                    let html = '';
                    networks.forEach(network => {
                        html += '<div class="wifi-network">';
                        html += '<strong>' + escapeHtml(network.ssid) + '</strong>';
                        html += '<button class="btn btn-secondary" onclick="removeWiFi(\'' + escapeHtml(network.ssid) + '\')">Remove</button>';
                        html += '</div>';
                    });

                    div.innerHTML = html;
                })
                .catch(err => {
                    document.getElementById('saved-networks').innerHTML = '<p class="alert alert-error">Failed to load saved networks</p>';
                });
        }

        function removeWiFi(ssid) {
            if (!confirm('Remove network ' + ssid + '?')) return;

            fetch('/api/wifi/disconnect', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ ssid })
            })
            .then(r => r.json())
            .then(data => {
                loadSavedNetworks();
            })
            .catch(err => alert('Failed to remove: ' + err.message));
        }

        function getSignalStrength(signal) {
            if (signal > -50) return 'strong';
            if (signal > -70) return 'medium';
            return 'weak';
        }

        // Device management functions
        function refreshDevices() {
            const devicesDiv = document.getElementById('devices-list');
            devicesDiv.innerHTML = '<div class="loading"><div class="spinner"></div><p>Loading devices...</p></div>';

            fetch('/api/devices')
                .then(r => {
                    if (!r.ok) {
                        return r.text().then(text => {
                            throw new Error(text || 'Failed to load devices');
                        });
                    }
                    return r.json();
                })
                .then(devices => {
                    if (!devices || devices.length === 0) {
                        devicesDiv.innerHTML = '<p class="alert alert-info">No storage devices detected</p>';
                        return;
                    }

                    let html = '<div style="display: grid; gap: 1rem; margin-top: 1rem;">';
                    devices.forEach(device => {
                        const statusColor = device.has_dcim ? 'var(--success)' : 'var(--text-secondary)';
                        const usbBadge = device.is_usb ? '<span class="status-badge badge-idle" style="font-size: 0.75rem;">USB</span>' : '';
                        const dcimBadge = device.has_dcim ? '<span class="status-badge badge-success" style="font-size: 0.75rem;">HAS DCIM</span>' : '';
                        const mountedBadge = device.is_mounted ? '<span class="status-badge badge-syncing" style="font-size: 0.75rem;">MOUNTED</span>' : '';

                        html += '<div class="card" style="margin: 0; border-left: 4px solid ' + statusColor + ';">';
                        html += '<div style="display: flex; justify-content: space-between; align-items: flex-start; margin-bottom: 1rem;">';
                        html += '<div style="flex: 1;">';
                        html += '<h3 style="margin: 0 0 0.5rem 0; font-size: 1.125rem;">';
                        html += '<strong>' + device.device_name + '</strong> ' + usbBadge + ' ' + mountedBadge + ' ' + dcimBadge;
                        html += '</h3>';
                        html += '<p style="margin: 0; color: var(--text-secondary);" class="code">' + device.device_path + '</p>';
                        html += '</div>';
                        if (device.has_dcim) {
                            html += '<button class="btn btn-primary" onclick="selectDevice(\'' + device.device_path + '\')">▶️ Use This Device</button>';
                        }
                        html += '</div>';

                        html += '<div class="info-grid">';
                        html += '<div class="info-item"><label>Size</label><value>' + device.size_human + '</value></div>';
                        if (device.volume_label) {
                            html += '<div class="info-item"><label>Label</label><value>' + escapeHtml(device.volume_label) + '</value></div>';
                        }
                        if (device.is_mounted && device.mount_path) {
                            html += '<div class="info-item"><label>Mounted At</label><value class="code">' + device.mount_path + '</value></div>';
                        }
                        html += '</div>';

                        html += '</div>';
                    });
                    html += '</div>';

                    devicesDiv.innerHTML = html;
                })
                .catch(err => {
                    devicesDiv.innerHTML = '<p class="alert alert-error">Failed to load devices: ' + err.message + '</p>';
                });
        }

        function selectDevice(devicePath) {
            if (!confirm('Select device ' + devicePath + ' for syncing?')) return;

            fetch('/api/devices/select', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ device_path: devicePath })
            })
            .then(r => {
                if (!r.ok) {
                    return r.text().then(text => {
                        throw new Error(text || 'Failed to select device');
                    });
                }
                return r.json();
            })
            .then(data => {
                alert('Device selected: ' + data.message);
                switchTab('status');
            })
            .catch(err => alert('Failed to select device: ' + err.message));
        }

        // Configuration functions
        function loadConfig() {
            fetch('/api/config')
                .then(r => r.json())
                .then(data => {
                    // Populate the config textarea with actual content
                    if (data.content) {
                        document.getElementById('rclone-config').value = data.content;
                    }

                    if (data.configured) {
                        showConfigAlert('Rclone is configured (' + (data.remotes ? data.remotes.length : 0) + ' remotes)', 'success');
                    } else {
                        showConfigAlert('Rclone not configured yet', 'info');
                    }
                });

            fetch('/api/settings')
                .then(r => r.json())
                .then(data => {
                    if (data.remote_name) document.getElementById('remote-name').value = data.remote_name;
                    if (data.remote_path) document.getElementById('remote-path').value = data.remote_path;
                    if (data.reformat_threshold) document.getElementById('reformat-threshold').value = data.reformat_threshold * 100;
                    if (data.transfers) document.getElementById('transfers').value = data.transfers;
                    if (data.checkers) document.getElementById('checkers').value = data.checkers;

                    // Load Google Photos settings
                    if (data.google_photos_enabled !== undefined) {
                        document.getElementById('google-photos-enabled').checked = data.google_photos_enabled;
                    }
                    if (data.google_photos_remote_name) {
                        document.getElementById('google-photos-remote').value = data.google_photos_remote_name;
                    }
                });
        }

        function saveConfig(event) {
            event.preventDefault();
            const config = document.getElementById('rclone-config').value;

            fetch('/api/config', {
                method: 'POST',
                headers: { 'Content-Type': 'text/plain' },
                body: config
            })
            .then(r => r.json())
            .then(data => {
                showConfigAlert('Configuration saved successfully!', 'success');
            })
            .catch(err => {
                showConfigAlert('Failed to save configuration: ' + err.message, 'error');
            });
        }

        function testConfig() {
            fetch('/api/config/test', { method: 'POST' })
                .then(r => r.json())
                .then(data => {
                    if (data.success) {
                        showConfigAlert('✓ Connection test successful!', 'success');
                    } else {
                        showConfigAlert('✗ Connection test failed: ' + (data.error || 'Unknown error'), 'error');
                    }
                })
                .catch(err => {
                    showConfigAlert('✗ Connection test failed: ' + err.message, 'error');
                });
        }

        function saveSettings(event) {
            event.preventDefault();

            const settings = {
                remote_name: document.getElementById('remote-name').value,
                remote_path: document.getElementById('remote-path').value,
                reformat_threshold: parseFloat(document.getElementById('reformat-threshold').value) / 100,
                transfers: parseInt(document.getElementById('transfers').value) || 4,
                checkers: parseInt(document.getElementById('checkers').value) || 8,
                google_photos_enabled: document.getElementById('google-photos-enabled').checked,
                google_photos_remote_name: document.getElementById('google-photos-remote').value
            };

            fetch('/api/settings', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(settings)
            })
            .then(r => r.json())
            .then(data => {
                showConfigAlert('Settings saved successfully! Restart pictures-sync service for changes to take effect.', 'success');
            })
            .catch(err => {
                showConfigAlert('Failed to save settings: ' + err.message, 'error');
            });
        }

        function showConfigAlert(message, type) {
            const alertDiv = document.getElementById('config-alert');
            alertDiv.innerHTML = '<p class="alert alert-' + type + '">' + message + '</p>';
            setTimeout(() => { alertDiv.innerHTML = ''; }, 5000);
        }

        // Files browser functions
        // Remote Files Browser State
        let currentFilesPath = '';
        let currentFilesView = 'cards'; // 'cards' or 'folder'
        let currentPage = 1;
        let currentPageSize = 100;
        let currentPaginationData = null;
        let allFilesCache = []; // For client-side search

        // Switch between card view and folder view
        function switchFilesView(view) {
            currentFilesView = view;

            const cardsBtn = document.getElementById('view-cards-btn');
            const folderBtn = document.getElementById('view-folder-btn');
            const cardsView = document.getElementById('files-cards-view');
            const folderView = document.getElementById('files-folder-view');

            if (view === 'cards') {
                cardsBtn.classList.add('btn-primary');
                cardsBtn.classList.remove('btn-secondary');
                folderBtn.classList.remove('btn-primary');
                folderBtn.classList.add('btn-secondary');
                cardsView.style.display = 'block';
                folderView.style.display = 'none';
                loadCards();
            } else {
                folderBtn.classList.add('btn-primary');
                folderBtn.classList.remove('btn-secondary');
                cardsBtn.classList.remove('btn-primary');
                cardsBtn.classList.add('btn-secondary');
                folderView.style.display = 'block';
                cardsView.style.display = 'none';
                loadFilesPaginated('', 1);
            }
        }

        function refreshFilesView() {
            if (currentFilesView === 'cards') {
                loadCards();
            } else {
                loadFilesPaginated(currentFilesPath, currentPage);
            }
        }

        // Load cards list
        function loadCards() {
            const cardsList = document.getElementById('cards-list');
            cardsList.innerHTML = '<div class="loading"><div class="spinner"></div><p>Loading cards...</p></div>';

            fetch('/api/files/cards')
                .then(r => r.json())
                .then(data => {
                    if (data.error) {
                        showFilesAlert(data.error, 'error');
                        cardsList.innerHTML = '<p style="color: var(--text-secondary); text-align: center; padding: 2rem;">Failed to load cards</p>';
                        return;
                    }

                    displayCards(data.cards);
                })
                .catch(err => {
                    showFilesAlert('Failed to load cards: ' + err.message, 'error');
                    cardsList.innerHTML = '<p style="color: var(--text-secondary); text-align: center; padding: 2rem;">Failed to load cards</p>';
                });
        }

        function displayCards(cards) {
            const cardsList = document.getElementById('cards-list');

            if (!cards || cards.length === 0) {
                cardsList.innerHTML = '<p style="color: var(--text-secondary); text-align: center; padding: 2rem;">No cards found. Insert an SD card and sync to see it here.</p>';
                return;
            }

            let html = '<div style="display: grid; grid-template-columns: repeat(auto-fill, minmax(300px, 1fr)); gap: 1rem;">';

            cards.forEach(card => {
                const cardName = card.name;
                const modTime = new Date(card.mod_time).toLocaleString();

                html += '<div style="border: 1px solid var(--border); border-radius: 8px; padding: 1.5rem; cursor: pointer; transition: all 0.2s; background: var(--bg);" ';
                html += 'onclick="browseCard(\'' + cardName + '\')" ';
                html += 'onmouseover="this.style.borderColor=\'var(--primary)\'; this.style.transform=\'translateY(-2px)\'; this.style.boxShadow=\'0 4px 12px rgba(0,0,0,0.1)\';" ';
                html += 'onmouseout="this.style.borderColor=\'var(--border)\'; this.style.transform=\'translateY(0)\'; this.style.boxShadow=\'none\';">';
                html += '<div style="display: flex; align-items: center; gap: 1rem; margin-bottom: 0.5rem;">';
                html += '<span style="font-size: 2rem;">💾</span>';
                html += '<div>';
                html += '<div style="font-weight: 600; font-size: 1.1rem; color: var(--text);">' + cardName + '</div>';
                html += '<div style="font-size: 0.875rem; color: var(--text-secondary); margin-top: 0.25rem;">Last synced: ' + modTime + '</div>';
                html += '</div>';
                html += '</div>';
                html += '<div style="margin-top: 1rem; padding-top: 1rem; border-top: 1px solid var(--border);">';
                html += '<span style="color: var(--primary); font-size: 0.875rem; font-weight: 500;">Browse photos →</span>';
                html += '</div>';
                html += '</div>';
            });

            html += '</div>';
            cardsList.innerHTML = html;
        }

        function browseCard(cardName) {
            switchFilesView('folder');
            loadFilesPaginated(cardName + '/DCIM', 1);
        }

        // Load files with pagination
        function loadFilesPaginated(path, page) {
            currentFilesPath = path;
            currentPage = page;

            updateBreadcrumb(path);

            const filesDisplay = document.getElementById('files-display');
            filesDisplay.innerHTML = '<div class="loading"><div class="spinner"></div><p>Loading files...</p></div>';

            const url = '/api/files/paginated?path=' + encodeURIComponent(path) + '&page=' + page + '&page_size=' + currentPageSize;

            fetch(url)
                .then(r => r.json())
                .then(data => {
                    if (data.error) {
                        showFilesAlert(data.error, 'error');
                        filesDisplay.innerHTML = '<p style="color: var(--text-secondary); text-align: center; padding: 2rem;">Failed to load files</p>';
                        return;
                    }

                    currentPaginationData = data;
                    allFilesCache = data.files; // Cache for search
                    displayFilesPaginated(data);
                    updatePaginationControls(data);
                })
                .catch(err => {
                    showFilesAlert('Failed to load files: ' + err.message, 'error');
                    filesDisplay.innerHTML = '<p style="color: var(--text-secondary); text-align: center; padding: 2rem;">Failed to load files</p>';
                });
        }

        function loadPrevPage() {
            if (currentPage > 1) {
                loadFilesPaginated(currentFilesPath, currentPage - 1);
            }
        }

        function loadNextPage() {
            if (currentPaginationData && currentPaginationData.has_more) {
                loadFilesPaginated(currentFilesPath, currentPage + 1);
            }
        }

        function updateBreadcrumb(path) {
            const breadcrumbNav = document.getElementById('breadcrumb-nav');

            const parts = path ? path.split('/').filter(p => p) : [];
            let html = '<a href="#" onclick="loadFilesPaginated(\'\', 1); return false;" style="color: var(--primary); text-decoration: none; padding: 0.25rem 0.5rem; border-radius: 4px; font-size: 0.875rem;">🏠 Root</a>';

            let currentPath = '';
            parts.forEach((part, index) => {
                currentPath += (index > 0 ? '/' : '') + part;
                const pathForLink = currentPath;
                html += '<span style="color: var(--text-secondary);">/</span>';
                html += '<a href="#" onclick="loadFilesPaginated(\'' + pathForLink + '\', 1); return false;" style="color: var(--primary); text-decoration: none; padding: 0.25rem 0.5rem; border-radius: 4px; font-size: 0.875rem;">' + part + '</a>';
            });

            breadcrumbNav.innerHTML = html;
        }

        function updatePaginationControls(data) {
            const controls = document.getElementById('pagination-controls');
            const prevBtn = document.getElementById('prev-page-btn');
            const nextBtn = document.getElementById('next-page-btn');
            const pageInfo = document.getElementById('page-info');
            const itemsInfo = document.getElementById('items-info');

            if (data.total_pages > 1) {
                controls.style.display = 'block';
                prevBtn.disabled = data.page === 1;
                nextBtn.disabled = !data.has_more;
                pageInfo.textContent = 'Page ' + data.page + ' of ' + data.total_pages;

                const start = (data.page - 1) * data.page_size + 1;
                const end = Math.min(data.page * data.page_size, data.total);
                itemsInfo.textContent = 'Showing ' + start + '-' + end + ' of ' + data.total + ' items';
            } else {
                controls.style.display = 'none';
            }
        }

        // Backward compatibility
        function refreshFiles() {
            refreshFilesView();
        }

        function loadFiles(path) {
            loadFilesPaginated(path, 1);
        }

        function displayFilesPaginated(data) {
            const filesDisplay = document.getElementById('files-display');

            if (!data || !data.files || data.files.length === 0) {
                filesDisplay.innerHTML = '<p style="color: var(--text-secondary); text-align: center; padding: 2rem;">No files found</p>';
                return;
            }

            const files = data.files;
            const path = data.path;

            // Sort files: directories first, then by name
            files.sort((a, b) => {
                if (a.is_dir !== b.is_dir) return a.is_dir ? -1 : 1;
                return a.name.localeCompare(b.name);
            });

            let html = '<div style="overflow-x: auto;"><table style="width: 100%; border-collapse: collapse;">';
            html += '<thead style="background: var(--bg); border-bottom: 2px solid var(--border);">';
            html += '<tr>';
            html += '<th style="text-align: left; padding: 0.75rem;">Name</th>';
            html += '<th style="text-align: right; padding: 0.75rem;">Size</th>';
            html += '<th style="text-align: right; padding: 0.75rem;">Modified</th>';
            html += '</tr>';
            html += '</thead>';
            html += '<tbody>';

            files.forEach(file => {
                const fileName = file.name;
                const fileSize = file.is_dir ? '-' : formatBytes(file.size);
                const modTime = new Date(file.mod_time).toLocaleString();
                const filePath = path ? path + '/' + fileName : fileName;

                // Check if file is an image
                const ext = fileName.toLowerCase().split('.').pop();
                const isImage = ['jpg', 'jpeg', 'png', 'gif', 'webp'].includes(ext);
                const icon = file.is_dir ? '📁' : (isImage ? '🖼️' : '📄');

                html += '<tr style="border-bottom: 1px solid var(--border); transition: background 0.2s;" onmouseover="this.style.background=\'var(--bg)\'" onmouseout="this.style.background=\'transparent\'">';

                if (file.is_dir) {
                    html += '<td style="padding: 0.75rem;"><a href="#" onclick="loadFilesPaginated(\'' + filePath + '\', 1); return false;" style="color: var(--primary); text-decoration: none; font-weight: 500;">' + icon + ' ' + fileName + '</a></td>';
                } else if (isImage) {
                    html += '<td style="padding: 0.75rem;"><a href="#" onclick="viewImage(\'' + filePath.replace(/'/g, "\\'") + '\', \'' + fileName.replace(/'/g, "\\'") + '\'); return false;" style="color: var(--primary); text-decoration: none; font-weight: 500;">' + icon + ' ' + fileName + '</a></td>';
                } else {
                    html += '<td style="padding: 0.75rem;">' + icon + ' ' + fileName + '</td>';
                }

                html += '<td style="text-align: right; padding: 0.75rem; color: var(--text-secondary);">' + fileSize + '</td>';
                html += '<td style="text-align: right; padding: 0.75rem; color: var(--text-secondary); font-size: 0.875rem;">' + modTime + '</td>';
                html += '</tr>';
            });

            html += '</tbody></table></div>';
            filesDisplay.innerHTML = html;
        }

        function filterFiles() {
            const searchInput = document.getElementById('files-search');
            const query = searchInput.value.toLowerCase().trim();

            if (!currentPaginationData || !currentPaginationData.files) {
                return;
            }

            const files = currentPaginationData.files;

            if (!query) {
                // Show all files if no search query
                displayFilesPaginated(currentPaginationData);
                return;
            }

            // Filter files by name
            const filtered = files.filter(file =>
                file.name.toLowerCase().includes(query)
            );

            // Create filtered result
            const filteredData = {
                files: filtered,
                path: currentPaginationData.path,
                total: filtered.length,
                page: 1,
                page_size: filtered.length,
                total_pages: 1,
                has_more: false
            };

            displayFilesPaginated(filteredData);
        }

        function showFilesAlert(message, type) {
            const alertDiv = document.getElementById('files-alert');
            alertDiv.innerHTML = '<p class="alert alert-' + type + '">' + message + '</p>';
            setTimeout(() => { alertDiv.innerHTML = ''; }, 5000);
        }

        function viewImage(filePath, fileName) {
            // Create modal overlay
            const modal = document.createElement('div');
            modal.style.cssText = 'position: fixed; top: 0; left: 0; right: 0; bottom: 0; background: rgba(0,0,0,0.9); z-index: 10000; display: flex; flex-direction: column; align-items: center; justify-content: center; padding: 2rem;';

            // Close button
            const closeBtn = document.createElement('button');
            closeBtn.innerHTML = '✕ Close';
            closeBtn.className = 'btn btn-secondary';
            closeBtn.style.cssText = 'position: absolute; top: 1rem; right: 1rem; z-index: 10001;';
            closeBtn.onclick = () => document.body.removeChild(modal);

            // Image title
            const title = document.createElement('div');
            title.textContent = fileName;
            title.style.cssText = 'color: white; font-size: 1.25rem; font-weight: 600; margin-bottom: 1rem; position: absolute; top: 1rem; left: 1rem;';

            // Loading spinner
            const loading = document.createElement('div');
            loading.innerHTML = '<div class="spinner"></div><p style="color: white; margin-top: 1rem;">Loading image...</p>';
            loading.style.cssText = 'display: flex; flex-direction: column; align-items: center;';

            // Image element
            const img = document.createElement('img');
            img.style.cssText = 'max-width: 90%; max-height: 80vh; object-fit: contain; display: none; box-shadow: 0 10px 40px rgba(0,0,0,0.5); border-radius: 0.5rem;';
            img.src = '/api/files/view?path=' + encodeURIComponent(filePath);

            img.onload = () => {
                loading.style.display = 'none';
                img.style.display = 'block';
            };

            img.onerror = () => {
                loading.innerHTML = '<p style="color: #ef4444;">Failed to load image</p>';
            };

            modal.appendChild(closeBtn);
            modal.appendChild(title);
            modal.appendChild(loading);
            modal.appendChild(img);

            // Close on background click
            modal.onclick = (e) => {
                if (e.target === modal) {
                    document.body.removeChild(modal);
                }
            };

            // Close on Escape key
            const escHandler = (e) => {
                if (e.key === 'Escape') {
                    document.body.removeChild(modal);
                    document.removeEventListener('keydown', escHandler);
                }
            };
            document.addEventListener('keydown', escHandler);

            document.body.appendChild(modal);
        }

        // Gallery functions
        let currentGalleryPath = 'DCIM';
        let galleryPhotos = [];
        let currentLightboxIndex = 0;

        function loadGallery(path) {
            currentGalleryPath = path || 'DCIM';
            const galleryDisplay = document.getElementById('gallery-display');
            const pathDisplay = document.getElementById('gallery-current-path');

            pathDisplay.textContent = currentGalleryPath || '/';
            galleryDisplay.innerHTML = '<div class="loading"><div class="spinner"></div><p>Loading gallery...</p></div>';

            fetch('/api/sdcard/files?path=' + encodeURIComponent(currentGalleryPath))
                .then(r => r.json())
                .then(data => {
                    if (data.error) {
                        showGalleryAlert(data.error, 'error');
                        galleryDisplay.innerHTML = '<p style="color: var(--text-secondary); text-align: center; padding: 2rem;">' + data.error + '</p>';
                        return;
                    }

                    displayGallery(data.files);
                })
                .catch(err => {
                    showGalleryAlert('Failed to load gallery: ' + err.message, 'error');
                    galleryDisplay.innerHTML = '<p style="color: var(--text-secondary); text-align: center; padding: 2rem;">Failed to load gallery</p>';
                });
        }

        function refreshGallery() {
            loadGallery(currentGalleryPath);
        }

        function displayGallery(files) {
            const galleryDisplay = document.getElementById('gallery-display');

            if (!files || files.length === 0) {
                galleryDisplay.innerHTML = '<p style="color: var(--text-secondary); text-align: center; padding: 2rem;">No files found</p>';
                return;
            }

            // Separate folders and images
            const folders = files.filter(f => f.is_dir);
            const images = files.filter(f => f.is_image && !f.is_dir);

            // Store images for lightbox navigation
            galleryPhotos = images;

            let html = '';

            // Display folders first
            if (folders.length > 0) {
                html += '<div style="margin-bottom: 1.5rem;"><h3 style="font-size: 1rem; font-weight: 600; margin-bottom: 0.75rem;">📁 Folders</h3>';
                html += '<div style="display: flex; gap: 0.5rem; flex-wrap: wrap;">';
                folders.forEach(folder => {
                    const folderPath = folder.path;
                    html += '<button class="btn btn-secondary" onclick="loadGallery(\'' + folderPath.replace(/'/g, "\\'") + '\')">';
                    html += '📁 ' + folder.name;
                    html += '</button>';
                });
                html += '</div></div>';
            }

            // Display images in grid
            if (images.length > 0) {
                html += '<h3 style="font-size: 1rem; font-weight: 600; margin-bottom: 0.75rem;">🖼️ Photos (' + images.length + ')</h3>';
                html += '<div class="gallery-grid">';

                images.forEach((file, index) => {
                    const thumbnailUrl = '/api/thumbnail?path=' + encodeURIComponent(file.path);

                    html += '<div class="gallery-item" onclick="openLightbox(' + index + ')">';
                    html += '<img src="' + thumbnailUrl + '" alt="' + file.name + '" loading="lazy">';

                    // Show brief info on hover
                    html += '<div class="gallery-item-info">';
                    html += '<div style="font-weight: 600;">' + file.name + '</div>';
                    if (file.exif && file.exif.camera_model) {
                        html += '<div style="font-size: 0.7rem; margin-top: 0.25rem;">' + file.exif.camera_model + '</div>';
                    }
                    html += '</div>';

                    html += '</div>';
                });

                html += '</div>';
            } else {
                html += '<p style="color: var(--text-secondary); text-align: center; padding: 2rem;">No images found in this folder</p>';
            }

            galleryDisplay.innerHTML = html;
        }

        function showGalleryAlert(message, type) {
            const alertDiv = document.getElementById('gallery-alert');
            const bgColor = type === 'error' ? 'var(--error-light)' : 'var(--success-light)';
            const textColor = type === 'error' ? 'var(--error)' : 'var(--success)';
            alertDiv.innerHTML = '<div style="padding: 0.75rem; border-radius: 0.375rem; background: ' + bgColor + '; color: ' + textColor + ';">' + message + '</div>';
            setTimeout(() => { alertDiv.innerHTML = ''; }, 5000);
        }

        function openLightbox(index) {
            if (!galleryPhotos || galleryPhotos.length === 0) return;

            currentLightboxIndex = index;
            const photo = galleryPhotos[currentLightboxIndex];

            const lightbox = document.getElementById('lightbox');
            const img = document.getElementById('lightbox-img');
            const info = document.getElementById('lightbox-info');

            // Show lightbox
            lightbox.classList.add('active');

            // Load full-resolution image
            img.src = '/api/sdcard/preview?path=' + encodeURIComponent(photo.path);
            img.alt = photo.name;

            // Display EXIF info
            let infoHtml = '<h3 style="font-size: 1.125rem; font-weight: 600; margin-bottom: 0.5rem;">' + photo.name + '</h3>';

            if (photo.exif && Object.keys(photo.exif).length > 0) {
                infoHtml += '<div class="lightbox-exif">';

                if (photo.exif.camera_make) {
                    infoHtml += '<div class="lightbox-exif-item"><div class="lightbox-exif-label">Camera</div>' + photo.exif.camera_make;
                    if (photo.exif.camera_model) infoHtml += ' ' + photo.exif.camera_model;
                    infoHtml += '</div>';
                }

                if (photo.exif.date_time) {
                    infoHtml += '<div class="lightbox-exif-item"><div class="lightbox-exif-label">Date</div>' + photo.exif.date_time + '</div>';
                }

                if (photo.exif.f_number) {
                    infoHtml += '<div class="lightbox-exif-item"><div class="lightbox-exif-label">Aperture</div>' + photo.exif.f_number + '</div>';
                }

                if (photo.exif.exposure_time) {
                    infoHtml += '<div class="lightbox-exif-item"><div class="lightbox-exif-label">Shutter</div>' + photo.exif.exposure_time + '</div>';
                }

                if (photo.exif.iso) {
                    infoHtml += '<div class="lightbox-exif-item"><div class="lightbox-exif-label">ISO</div>' + photo.exif.iso + '</div>';
                }

                if (photo.exif.focal_length) {
                    infoHtml += '<div class="lightbox-exif-item"><div class="lightbox-exif-label">Focal Length</div>' + photo.exif.focal_length + '</div>';
                }

                if (photo.exif.gps_latitude && photo.exif.gps_longitude) {
                    const lat = photo.exif.gps_latitude.toFixed(6);
                    const lon = photo.exif.gps_longitude.toFixed(6);
                    infoHtml += '<div class="lightbox-exif-item"><div class="lightbox-exif-label">GPS</div>';
                    infoHtml += '<a href="https://www.google.com/maps?q=' + lat + ',' + lon + '" target="_blank" style="color: white; text-decoration: underline;">';
                    infoHtml += lat + ', ' + lon + '</a></div>';
                }

                infoHtml += '</div>';
            }

            infoHtml += '<div style="margin-top: 0.75rem; font-size: 0.875rem; color: #aaa;">Photo ' + (currentLightboxIndex + 1) + ' of ' + galleryPhotos.length + '</div>';

            info.innerHTML = infoHtml;

            // Prevent body scrolling
            document.body.style.overflow = 'hidden';
        }

        function closeLightbox() {
            const lightbox = document.getElementById('lightbox');
            lightbox.classList.remove('active');
            document.body.style.overflow = '';
        }

        function navigateLightbox(direction) {
            if (!galleryPhotos || galleryPhotos.length === 0) return;

            currentLightboxIndex += direction;

            // Wrap around
            if (currentLightboxIndex < 0) {
                currentLightboxIndex = galleryPhotos.length - 1;
            } else if (currentLightboxIndex >= galleryPhotos.length) {
                currentLightboxIndex = 0;
            }

            openLightbox(currentLightboxIndex);
        }

        // Keyboard navigation for lightbox
        document.addEventListener('keydown', (e) => {
            const lightbox = document.getElementById('lightbox');
            if (!lightbox.classList.contains('active')) return;

            if (e.key === 'Escape') {
                closeLightbox();
            } else if (e.key === 'ArrowLeft') {
                navigateLightbox(-1);
            } else if (e.key === 'ArrowRight') {
                navigateLightbox(1);
            }
        });

        // Close lightbox on background click
        document.getElementById('lightbox').addEventListener('click', (e) => {
            if (e.target.id === 'lightbox') {
                closeLightbox();
            }
        });

        // Network Debug functions
        function loadNetworkInfo() {
            // Load DNS config
            fetch('/api/network/dns')
                .then(r => r.json())
                .then(data => {
                    const configDiv = document.getElementById('network-config');
                    let html = '<div style="background: var(--bg); padding: 1rem; border-radius: 0.375rem; font-family: monospace;">';
                    html += '<h3 style="margin-bottom: 0.5rem; font-size: 1rem;">DNS Configuration (/etc/resolv.conf)</h3>';
                    html += '<pre style="margin: 0; white-space: pre-wrap; word-break: break-all;">' + escapeHtml(data.resolv_conf || 'Unable to read') + '</pre>';
                    html += '</div>';
                    configDiv.innerHTML = html;
                })
                .catch(err => {
                    document.getElementById('network-config').innerHTML = '<p class="alert alert-error">Failed to load DNS config: ' + err.message + '</p>';
                });

            // Load network interfaces
            refreshNetworkInfo();
        }

        function refreshNetworkInfo() {
            const interfacesDiv = document.getElementById('network-interfaces');
            interfacesDiv.innerHTML = '<div class="loading"><div class="spinner"></div><p>Loading interfaces...</p></div>';

            fetch('/api/network/interfaces')
                .then(r => r.json())
                .then(data => {
                    let html = '<div style="display: grid; gap: 1rem;">';

                    if (data.interfaces && data.interfaces.length > 0) {
                        data.interfaces.forEach(iface => {
                            const isUp = iface.flags && iface.flags.includes('up');
                            const statusColor = isUp ? 'var(--success)' : 'var(--text-secondary)';

                            html += '<div style="padding: 1rem; background: var(--bg); border-radius: 0.375rem; border-left: 4px solid ' + statusColor + ';">';
                            html += '<h4 style="margin: 0 0 0.5rem 0;">' + iface.name + ' <span class="status-badge badge-' + (isUp ? 'success' : 'idle') + '" style="font-size: 0.75rem;">' + (isUp ? 'UP' : 'DOWN') + '</span></h4>';

                            if (iface.addresses && iface.addresses.length > 0) {
                                html += '<div class="info-grid" style="margin-top: 0.5rem;">';
                                iface.addresses.forEach(addr => {
                                    html += '<div class="info-item">';
                                    html += '<label>' + addr.family.toUpperCase() + '</label>';
                                    html += '<value class="code">' + addr.address + '</value>';
                                    html += '</div>';
                                });

                                if (iface.mac) {
                                    html += '<div class="info-item"><label>MAC</label><value class="code">' + iface.mac + '</value></div>';
                                }
                                if (iface.mtu) {
                                    html += '<div class="info-item"><label>MTU</label><value>' + iface.mtu + '</value></div>';
                                }
                                html += '</div>';
                            }
                            html += '</div>';
                        });
                    } else {
                        html += '<p style="text-align: center; color: var(--text-secondary);">No network interfaces found</p>';
                    }

                    html += '</div>';
                    interfacesDiv.innerHTML = html;
                })
                .catch(err => {
                    interfacesDiv.innerHTML = '<p class="alert alert-error">Failed to load interfaces: ' + err.message + '</p>';
                });
        }

        function testDNS(event) {
            event.preventDefault();
            const hostname = document.getElementById('dns-hostname').value;
            const resultDiv = document.getElementById('dns-result');

            resultDiv.innerHTML = '<div class="loading"><div class="spinner"></div><p>Resolving ' + escapeHtml(hostname) + '...</p></div>';

            fetch('/api/network/dns-lookup', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ hostname: hostname })
            })
            .then(r => r.json())
            .then(data => {
                if (data.error) {
                    resultDiv.innerHTML = '<div class="alert alert-error">❌ DNS resolution failed: ' + escapeHtml(data.error) + '</div>';
                } else {
                    let html = '<div class="alert alert-success">✅ DNS resolution successful</div>';
                    html += '<div style="margin-top: 1rem; padding: 1rem; background: var(--bg); border-radius: 0.375rem; font-family: monospace;">';

                    if (data.addresses && data.addresses.length > 0) {
                        html += '<strong>Resolved addresses:</strong><br>';
                        data.addresses.forEach(addr => {
                            html += '• ' + escapeHtml(addr) + '<br>';
                        });
                    }

                    if (data.raw_output) {
                        html += '<br><strong>Raw output:</strong><br>';
                        html += '<pre style="margin: 0; white-space: pre-wrap;">' + escapeHtml(data.raw_output) + '</pre>';
                    }

                    html += '</div>';
                    resultDiv.innerHTML = html;
                }
            })
            .catch(err => {
                resultDiv.innerHTML = '<div class="alert alert-error">Failed to perform DNS lookup: ' + err.message + '</div>';
            });
        }

        function testPing(event) {
            event.preventDefault();
            const hostname = document.getElementById('ping-hostname').value;
            const count = document.getElementById('ping-count').value;
            const resultDiv = document.getElementById('ping-result');

            resultDiv.innerHTML = '<div class="loading"><div class="spinner"></div><p>Pinging ' + escapeHtml(hostname) + '...</p></div>';

            fetch('/api/network/ping', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ hostname: hostname, count: parseInt(count) })
            })
            .then(r => r.json())
            .then(data => {
                if (data.error) {
                    resultDiv.innerHTML = '<div class="alert alert-error">❌ Ping failed: ' + escapeHtml(data.error) + '</div>';
                } else {
                    let html = '<div class="alert alert-success">✅ Ping successful</div>';
                    html += '<div style="margin-top: 1rem; padding: 1rem; background: var(--bg); border-radius: 0.375rem; font-family: monospace;">';

                    if (data.summary) {
                        html += '<strong>Summary:</strong><br>';
                        html += escapeHtml(data.summary) + '<br><br>';
                    }

                    if (data.output) {
                        html += '<strong>Full output:</strong><br>';
                        html += '<pre style="margin: 0; white-space: pre-wrap;">' + escapeHtml(data.output) + '</pre>';
                    }

                    html += '</div>';
                    resultDiv.innerHTML = html;
                }
            })
            .catch(err => {
                resultDiv.innerHTML = '<div class="alert alert-error">Failed to ping: ' + err.message + '</div>';
            });
        }

        function runFullDiagnostics() {
            const resultDiv = document.getElementById('diagnostics-result');
            resultDiv.innerHTML = '<div class="loading"><div class="spinner"></div><p>Running full network diagnostics...</p></div>';

            fetch('/api/network/diagnostics', { method: 'POST' })
                .then(r => r.json())
                .then(data => {
                    let html = '<div style="display: grid; gap: 1rem; margin-top: 1rem;">';

                    // DNS Test
                    html += '<div style="padding: 1rem; background: var(--bg); border-radius: 0.375rem;">';
                    html += '<h4 style="margin: 0 0 0.5rem 0;">DNS Servers (ICMP Ping)</h4>';
                    if (data.dns_google) {
                        html += '<p>✅ Google DNS (8.8.8.8): Reachable</p>';
                    } else {
                        html += '<p>❌ Google DNS (8.8.8.8): Unreachable</p>';
                    }
                    if (data.dns_cloudflare) {
                        html += '<p>✅ Cloudflare DNS (1.1.1.1): Reachable</p>';
                    } else {
                        html += '<p>❌ Cloudflare DNS (1.1.1.1): Unreachable</p>';
                    }
                    html += '</div>';

                    // Internet Connectivity
                    html += '<div style="padding: 1rem; background: var(--bg); border-radius: 0.375rem;">';
                    html += '<h4 style="margin: 0 0 0.5rem 0;">Internet Connectivity</h4>';
                    if (data.internet_google) {
                        html += '<p>✅ google.com: Reachable</p>';
                    } else {
                        html += '<p>❌ google.com: Unreachable</p>';
                    }
                    if (data.internet_cloudflare) {
                        html += '<p>✅ cloudflare.com: Reachable</p>';
                    } else {
                        html += '<p>❌ cloudflare.com: Unreachable</p>';
                    }
                    html += '</div>';

                    // Gateway
                    html += '<div style="padding: 1rem; background: var(--bg); border-radius: 0.375rem;">';
                    html += '<h4 style="margin: 0 0 0.5rem 0;">Default Gateway</h4>';
                    if (data.gateway) {
                        html += '<p>Gateway: <span class="code">' + escapeHtml(data.gateway) + '</span></p>';
                        if (data.gateway_reachable) {
                            html += '<p>✅ Gateway is reachable</p>';
                        } else {
                            html += '<p>❌ Gateway is unreachable</p>';
                        }
                    } else {
                        html += '<p>❌ No default gateway found</p>';
                    }
                    html += '</div>';

                    // Routes
                    if (data.routes) {
                        html += '<div style="padding: 1rem; background: var(--bg); border-radius: 0.375rem;">';
                        html += '<h4 style="margin: 0 0 0.5rem 0;">Routing Table</h4>';
                        html += '<pre style="margin: 0; white-space: pre-wrap; font-size: 0.875rem;">' + escapeHtml(data.routes) + '</pre>';
                        html += '</div>';
                    }

                    html += '</div>';
                    resultDiv.innerHTML = html;
                })
                .catch(err => {
                    resultDiv.innerHTML = '<div class="alert alert-error">Failed to run diagnostics: ' + err.message + '</div>';
                });
        }

        // Utility functions
        function formatBytes(bytes) {
            if (!bytes || bytes === 0) return '0 B';
            const k = 1024;
            const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
            const i = Math.floor(Math.log(bytes) / Math.log(k));
            return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
        }

        function formatDuration(ms) {
            const seconds = Math.floor(ms / 1000);
            const minutes = Math.floor(seconds / 60);
            const hours = Math.floor(minutes / 60);

            if (hours > 0) return hours + 'h ' + (minutes % 60) + 'm';
            if (minutes > 0) return minutes + 'm ' + (seconds % 60) + 's';
            return seconds + 's';
        }

        function escapeHtml(text) {
            const div = document.createElement('div');
            div.textContent = text;
            return div.innerHTML;
        }

        // Initialize
        connectWebSocket();
        loadHistory();
    </script>
</body>
</html>`
}

// handleCSRFToken returns the current CSRF token
func handleCSRFToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	jsonResponse(w, map[string]string{
		"csrf_token": getCSRFToken(),
	})
}

// handleStatus returns current system status
func handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Reload state from disk to get latest updates from pictures-sync service
	if err := stateMgr.Reload(); err != nil {
		log.Printf("Failed to reload state: %v", err)
	}

	status := stateMgr.GetState()
	jsonResponse(w, status)
}

// handleHistory returns sync history
func handleHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	history := stateMgr.GetHistory()
	jsonResponse(w, history)
}

// handleConfig handles rclone configuration
func handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		// Return current config status (but NOT the content with credentials)
		hasConfig, err := state.EnsureRcloneConfig()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		remotes, _ := syncMgr.ListRemotes()
		jsonResponse(w, map[string]interface{}{
			"configured": hasConfig,
			"remotes":    remotes,
			// SECURITY: Never return config content - it contains cloud credentials
			// Users can view/edit config via rclone config commands on the device
		})

	case http.MethodPost:
		// Update rclone config
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "Failed to read body", http.StatusBadRequest)
			return
		}

		// Write config file
		if err := os.WriteFile(state.GetRcloneConfigPath(), body, 0600); err != nil {
			http.Error(w, fmt.Sprintf("Failed to write config: %v", err), http.StatusInternalServerError)
			return
		}

		log.Println("Rclone configuration updated")
		jsonResponse(w, map[string]string{"status": "ok"})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleConfigTest tests rclone connection
func handleConfigTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := syncMgr.TestConnection(); err != nil {
		jsonResponse(w, map[string]interface{}{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	jsonResponse(w, map[string]bool{"success": true})
}

// handleSettings manages application settings
func handleSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		jsonResponse(w, appSettings.ToJSON())

	case http.MethodPost:
		var req struct {
			RemoteName             string  `json:"remote_name"`
			RemotePath             string  `json:"remote_path"`
			ReformatThreshold      float64 `json:"reformat_threshold"`
			Transfers              int     `json:"transfers"`
			Checkers               int     `json:"checkers"`
			GooglePhotosEnabled    bool    `json:"google_photos_enabled"`
			GooglePhotosRemoteName string  `json:"google_photos_remote_name"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		// Update settings
		if req.RemoteName != "" || req.RemotePath != "" {
			if err := appSettings.SetRemote(req.RemoteName, req.RemotePath); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			// Update sync manager
			syncMgr.SetRemote(req.RemoteName, req.RemotePath)
		}

		if req.ReformatThreshold > 0 {
			if err := appSettings.SetReformatThreshold(req.ReformatThreshold); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}

		if req.Transfers > 0 {
			if err := appSettings.SetTransfers(req.Transfers); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}

		if req.Checkers > 0 {
			if err := appSettings.SetCheckers(req.Checkers); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}

		// Update Google Photos settings
		if err := appSettings.SetGooglePhotos(req.GooglePhotosEnabled, req.GooglePhotosRemoteName); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		// Update sync manager with Google Photos settings
		syncMgr.SetGooglePhotos(req.GooglePhotosEnabled, req.GooglePhotosRemoteName)

		log.Println("Settings updated")
		jsonResponse(w, map[string]string{"status": "ok"})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleWiFiScan scans for available networks
func handleWiFiScan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if wifiMgr == nil {
		http.Error(w, "WiFi manager not initialized", http.StatusServiceUnavailable)
		return
	}

	networks, err := wifiMgr.ScanNetworks()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	jsonResponse(w, networks)
}

// handleWiFiNetworks returns saved networks
func handleWiFiNetworks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if wifiMgr == nil {
		http.Error(w, "WiFi manager not initialized", http.StatusServiceUnavailable)
		return
	}

	networks := wifiMgr.GetNetworks()
	jsonResponse(w, networks)
}

// handleWiFiConnect connects to a network
func handleWiFiConnect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if wifiMgr == nil {
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

	if err := wifiMgr.AddNetwork(req.SSID, req.Password); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	jsonResponse(w, map[string]string{"status": "ok"})
}

// handleWiFiDisconnect removes a network
func handleWiFiDisconnect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if wifiMgr == nil {
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

	if err := wifiMgr.RemoveNetwork(req.SSID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	jsonResponse(w, map[string]string{"status": "ok"})
}

// handleWiFiStatus returns current WiFi status
func handleWiFiStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if wifiMgr == nil {
		http.Error(w, "WiFi manager not initialized", http.StatusServiceUnavailable)
		return
	}

	ssid, err := wifiMgr.GetCurrentSSID()
	if err != nil {
		jsonResponse(w, map[string]interface{}{
			"connected": false,
			"error":     err.Error(),
		})
		return
	}

	jsonResponse(w, map[string]interface{}{
		"connected": true,
		"ssid":      ssid,
	})
}

// handleWebSocket provides real-time status updates
func handleWebSocket(w http.ResponseWriter, r *http.Request) {
	// Enforce authentication on WebSocket connections
	username, password, ok := r.BasicAuth()
	usernameMatch := subtle.ConstantTimeCompare([]byte(username), []byte("gokrazy")) == 1
	passwordMatch := subtle.ConstantTimeCompare([]byte(password), []byte(authPassword)) == 1

	if !ok || !usernameMatch || !passwordMatch {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}
	defer conn.Close()

	// Subscribe to state updates
	updates := stateMgr.Subscribe()
	// IMPORTANT: Unsubscribe when WebSocket closes to prevent memory leak
	defer stateMgr.Unsubscribe(updates)

	// Send initial state (reload from disk first to get latest from pictures-sync service)
	stateMgr.Reload()
	status := stateMgr.GetState()
	if err := conn.WriteJSON(status); err != nil {
		return
	}

	// Send updates and periodically reload state from disk
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case state, ok := <-updates:
			if !ok {
				// Channel was closed by Unsubscribe
				return
			}
			if err := conn.WriteJSON(state); err != nil {
				return
			}
		case <-ticker.C:
			// Reload state from disk (in case pictures-sync service updated it)
			if err := stateMgr.Reload(); err != nil {
				log.Printf("Failed to reload state: %v", err)
			}

			// Send updated state
			status := stateMgr.GetState()
			if err := conn.WriteJSON(status); err != nil {
				return
			}
		}
	}
}

// handleDevices lists all available storage devices
func handleDevices(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	devices, err := sdmonitor.ListAllStorageDevices()
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to list devices: %v", err), http.StatusInternalServerError)
		return
	}

	// Convert to state.DeviceInfo to match the state manager's type
	stateDevices := make([]state.DeviceInfo, len(devices))
	for i, d := range devices {
		stateDevices[i] = state.DeviceInfo{
			DevicePath:  d.DevicePath,
			DeviceName:  d.DeviceName,
			Size:        d.Size,
			SizeHuman:   d.SizeHuman,
			IsUSB:       d.IsUSB,
			IsMounted:   d.IsMounted,
			MountPath:   d.MountPath,
			HasDCIM:     d.HasDCIM,
			VolumeLabel: d.VolumeLabel,
		}
	}

	// Update state manager with available devices
	stateMgr.SetAvailableDevices(stateDevices)

	jsonResponse(w, devices)
}

// handleDeviceSelect handles manual device selection
func handleDeviceSelect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		DevicePath string `json:"device_path"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if req.DevicePath == "" {
		http.Error(w, "device_path is required", http.StatusBadRequest)
		return
	}

	log.Printf("Manual device selection: %s", req.DevicePath)

	// TODO: Trigger sync for the selected device
	// This needs to be integrated with the main pictures-sync service

	jsonResponse(w, map[string]string{
		"status": "ok",
		"message": "Device selection received. Integration with sync service pending.",
	})
}

// handleSyncStart starts a manual sync operation
func handleSyncStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Check if sync is already running
	if syncMgr.IsRunning() {
		http.Error(w, "Sync already in progress", http.StatusConflict)
		return
	}

	// Get current state to check if SD card is mounted
	currentState := stateMgr.GetState()
	if !currentState.SDCardMounted || currentState.SDCardPath == "" {
		http.Error(w, "No SD card mounted", http.StatusBadRequest)
		return
	}

	// Trigger sync in a goroutine to avoid blocking the HTTP response
	go func() {
		log.Printf("Manual sync triggered for mounted SD card at: %s", currentState.SDCardPath)

		// Check for DCIM directory
		dcimPath := filepath.Join(currentState.SDCardPath, "DCIM")
		if !sdmonitor.HasDCIM(currentState.SDCardPath) {
			log.Println("No DCIM directory found on SD card")
			stateMgr.SetStatus(state.StatusError)
			return
		}

		// Count photos
		totalFiles, totalBytes, err := sdmonitor.CountPhotos(currentState.SDCardPath)
		if err != nil {
			log.Printf("Error counting photos: %v", err)
			stateMgr.SetStatus(state.StatusError)
			return
		}

		if totalFiles == 0 {
			log.Println("No photos found on SD card")
			stateMgr.SetStatus(state.StatusIdle)
			return
		}

		// Get card ID
		cardID, _, err := sdmonitor.GetOrCreateCardID(currentState.SDCardPath, nil)
		if err != nil {
			log.Printf("Error getting card ID: %v", err)
			stateMgr.SetStatus(state.StatusError)
			return
		}

		log.Printf("Starting manual sync of %d files (%.2f MB) for card: %s",
			totalFiles, float64(totalBytes)/(1024*1024), cardID)

		_, err = stateMgr.StartSync(cardID, int64(totalFiles), totalBytes)
		if err != nil {
			log.Printf("Error starting sync: %v", err)
			stateMgr.SetStatus(state.StatusError)
			return
		}

		// Perform sync
		err = syncMgr.Sync(dcimPath, cardID, totalFiles, totalBytes)

		if err != nil {
			log.Printf("Manual sync failed: %v", err)
			stateMgr.FinishSync(false, err)
			stateMgr.SetStatus(state.StatusError)
		} else {
			log.Println("Manual sync completed successfully!")
			stateMgr.FinishSync(true, nil)
			stateMgr.SetStatus(state.StatusSuccess)

			// Keep success status for a few seconds, then go idle
			time.Sleep(5 * time.Second)
			stateMgr.SetStatus(state.StatusIdle)
		}
	}()

	jsonResponse(w, map[string]string{
		"status": "ok",
		"message": "Sync started",
	})
}

// handleSyncCancel cancels the current sync operation
func handleSyncCancel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if !syncMgr.IsRunning() {
		http.Error(w, "No sync in progress", http.StatusBadRequest)
		return
	}

	log.Println("Manual sync cancellation requested")

	if err := syncMgr.Cancel(); err != nil {
		http.Error(w, fmt.Sprintf("Failed to cancel sync: %v", err), http.StatusInternalServerError)
		return
	}

	// Update state
	stateMgr.FinishSync(false, fmt.Errorf("cancelled by user"))
	stateMgr.SetStatus(state.StatusIdle)

	jsonResponse(w, map[string]string{
		"status": "ok",
		"message": "Sync cancelled",
	})
}

// handleThumbnail serves thumbnail images for files being synced
// handleFileCards returns list of card IDs
func handleFileCards(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	cards, err := syncMgr.ListCardIDs()
	if err != nil {
		jsonResponse(w, map[string]interface{}{
			"error": fmt.Sprintf("Failed to list cards: %v", err),
		})
		return
	}

	jsonResponse(w, map[string]interface{}{
		"cards": cards,
	})
}

// handleFilesPaginated returns paginated file listing
func handleFilesPaginated(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	path := r.URL.Query().Get("path")
	page := 1
	pageSize := 100

	if p := r.URL.Query().Get("page"); p != "" {
		if parsed, err := strconv.Atoi(p); err == nil && parsed > 0 {
			page = parsed
		}
	}

	if ps := r.URL.Query().Get("page_size"); ps != "" {
		if parsed, err := strconv.Atoi(ps); err == nil && parsed > 0 && parsed <= 1000 {
			pageSize = parsed
		}
	}

	result, err := syncMgr.ListFilesPaginated(path, page, pageSize)
	if err != nil {
		jsonResponse(w, map[string]interface{}{
			"error": fmt.Sprintf("Failed to list files: %v", err),
		})
		return
	}

	jsonResponse(w, result)
}

func handleFiles(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get path from query param (defaults to root)
	path := r.URL.Query().Get("path")

	// List files on remote
	files, err := syncMgr.ListFiles(path)
	if err != nil {
		jsonResponse(w, map[string]interface{}{
			"error": fmt.Sprintf("Failed to list files: %v", err),
		})
		return
	}

	jsonResponse(w, map[string]interface{}{
		"files": files,
		"path":  path,
	})
}

func handleFileView(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get file path from query param
	filePath := r.URL.Query().Get("path")
	if filePath == "" {
		http.Error(w, "path parameter required", http.StatusBadRequest)
		return
	}

	// Check if file is an image
	ext := strings.ToLower(filepath.Ext(filePath))
	var contentType string
	switch ext {
	case ".jpg", ".jpeg":
		contentType = "image/jpeg"
	case ".png":
		contentType = "image/png"
	case ".gif":
		contentType = "image/gif"
	case ".webp":
		contentType = "image/webp"
	default:
		http.Error(w, "unsupported file type", http.StatusBadRequest)
		return
	}

	// Set content type header
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "public, max-age=3600")

	// Stream the file from remote
	if err := syncMgr.GetFile(filePath, w); err != nil {
		log.Printf("Failed to get file %s: %v", filePath, err)
		http.Error(w, fmt.Sprintf("failed to retrieve file: %v", err), http.StatusInternalServerError)
		return
	}
}

func handleThumbnail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get file path from query param
	requestedPath := r.URL.Query().Get("path")
	if requestedPath == "" {
		http.Error(w, "path parameter required", http.StatusBadRequest)
		return
	}

	// Security: Properly validate path to prevent traversal attacks
	mountPath := state.MountDir

	// Clean the mount path and ensure it ends with separator
	cleanMountPath := filepath.Clean(mountPath)
	if !strings.HasSuffix(cleanMountPath, string(os.PathSeparator)) {
		cleanMountPath += string(os.PathSeparator)
	}

	// Join mount path with requested path and clean the result
	// This resolves any .. or . in the path
	fullPath := filepath.Join(mountPath, filepath.Clean("/"+requestedPath))
	cleanFullPath := filepath.Clean(fullPath)

	// Verify the cleaned path is still within the mount directory
	if !strings.HasPrefix(cleanFullPath, cleanMountPath) {
		http.Error(w, "access denied", http.StatusForbidden)
		return
	}

	// Use the validated path
	filePath := cleanFullPath

	// Check if file is a JPEG
	ext := strings.ToLower(filepath.Ext(filePath))
	if ext != ".jpg" && ext != ".jpeg" {
		http.Error(w, "only JPEG images supported", http.StatusBadRequest)
		return
	}

	// Open and decode the image
	file, err := os.Open(filePath)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to open image: %v", err), http.StatusInternalServerError)
		return
	}
	defer file.Close()

	img, _, err := image.Decode(file)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to decode image: %v", err), http.StatusInternalServerError)
		return
	}

	// Resize to thumbnail (max 200px width, preserve aspect ratio)
	thumbnail := resize.Thumbnail(200, 200, img, resize.Lanczos3)

	// Encode as JPEG and send
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "public, max-age=3600")

	if err := jpeg.Encode(w, thumbnail, &jpeg.Options{Quality: 80}); err != nil {
		log.Printf("Failed to encode thumbnail: %v", err)
	}
}

// SDCardFileInfo contains file metadata including EXIF data
type SDCardFileInfo struct {
	Name     string                 `json:"name"`
	Path     string                 `json:"path"`
	Size     int64                  `json:"size"`
	ModTime  time.Time              `json:"mod_time"`
	IsDir    bool                   `json:"is_dir"`
	IsImage  bool                   `json:"is_image"`
	EXIF     map[string]interface{} `json:"exif,omitempty"`
}

// handleSDCardFiles lists files on the SD card with EXIF metadata
func handleSDCardFiles(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get path from query param (defaults to DCIM)
	requestedPath := r.URL.Query().Get("path")
	if requestedPath == "" {
		requestedPath = "DCIM"
	}

	// Security: Validate path
	mountPath := state.MountDir
	cleanMountPath := filepath.Clean(mountPath)
	if !strings.HasSuffix(cleanMountPath, string(os.PathSeparator)) {
		cleanMountPath += string(os.PathSeparator)
	}

	fullPath := filepath.Join(mountPath, filepath.Clean("/"+requestedPath))
	cleanFullPath := filepath.Clean(fullPath)

	if !strings.HasPrefix(cleanFullPath, cleanMountPath) {
		jsonResponse(w, map[string]interface{}{
			"error": "access denied",
		})
		return
	}

	// Check if SD card is mounted
	currentState := stateMgr.GetState()
	if !currentState.SDCardMounted {
		jsonResponse(w, map[string]interface{}{
			"error": "no SD card mounted",
		})
		return
	}

	// Read directory
	entries, err := os.ReadDir(cleanFullPath)
	if err != nil {
		jsonResponse(w, map[string]interface{}{
			"error": fmt.Sprintf("failed to read directory: %v", err),
		})
		return
	}

	// Build file list with metadata
	var files []SDCardFileInfo
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}

		fileInfo := SDCardFileInfo{
			Name:    entry.Name(),
			Path:    filepath.Join(requestedPath, entry.Name()),
			Size:    info.Size(),
			ModTime: info.ModTime(),
			IsDir:   entry.IsDir(),
		}

		// Check if it's an image and extract EXIF
		if !entry.IsDir() {
			ext := strings.ToLower(filepath.Ext(entry.Name()))
			if ext == ".jpg" || ext == ".jpeg" {
				fileInfo.IsImage = true

				// Extract EXIF data
				filePath := filepath.Join(cleanFullPath, entry.Name())
				if exifData := extractEXIF(filePath); exifData != nil {
					fileInfo.EXIF = exifData
				}
			} else if ext == ".png" || ext == ".gif" || ext == ".webp" {
				fileInfo.IsImage = true
			}
		}

		files = append(files, fileInfo)
	}

	jsonResponse(w, map[string]interface{}{
		"files": files,
		"path":  requestedPath,
	})
}

// extractEXIF extracts EXIF metadata from an image file
func extractEXIF(filePath string) map[string]interface{} {
	file, err := os.Open(filePath)
	if err != nil {
		return nil
	}
	defer file.Close()

	x, err := exif.Decode(file)
	if err != nil {
		return nil
	}

	data := make(map[string]interface{})

	// Extract common EXIF fields
	if cameraMake, err := x.Get(exif.Make); err == nil {
		if val, err := cameraMake.StringVal(); err == nil {
			data["camera_make"] = strings.TrimSpace(val)
		}
	}

	if cameraModel, err := x.Get(exif.Model); err == nil {
		if val, err := cameraModel.StringVal(); err == nil {
			data["camera_model"] = strings.TrimSpace(val)
		}
	}

	if dateTime, err := x.Get(exif.DateTimeOriginal); err == nil {
		if val, err := dateTime.StringVal(); err == nil {
			data["date_time"] = val
		}
	}

	if iso, err := x.Get(exif.ISOSpeedRatings); err == nil {
		if val, err := iso.Int(0); err == nil {
			data["iso"] = val
		}
	}

	if fNumber, err := x.Get(exif.FNumber); err == nil {
		if num, denom, err := fNumber.Rat2(0); err == nil {
			data["f_number"] = fmt.Sprintf("f/%.1f", float64(num)/float64(denom))
		}
	}

	if exposure, err := x.Get(exif.ExposureTime); err == nil {
		if num, denom, err := exposure.Rat2(0); err == nil {
			if num == 1 {
				data["exposure_time"] = fmt.Sprintf("1/%d", denom)
			} else {
				data["exposure_time"] = fmt.Sprintf("%.2fs", float64(num)/float64(denom))
			}
		}
	}

	if focalLength, err := x.Get(exif.FocalLength); err == nil {
		if num, denom, err := focalLength.Rat2(0); err == nil {
			data["focal_length"] = fmt.Sprintf("%.1fmm", float64(num)/float64(denom))
		}
	}

	// GPS coordinates
	lat, lon, err := x.LatLong()
	if err == nil {
		data["gps_latitude"] = lat
		data["gps_longitude"] = lon
	}

	return data
}

// handleSDCardPreview serves full-resolution images from SD card
func handleSDCardPreview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	requestedPath := r.URL.Query().Get("path")
	if requestedPath == "" {
		http.Error(w, "path parameter required", http.StatusBadRequest)
		return
	}

	// Security: Validate path
	mountPath := state.MountDir
	cleanMountPath := filepath.Clean(mountPath)
	if !strings.HasSuffix(cleanMountPath, string(os.PathSeparator)) {
		cleanMountPath += string(os.PathSeparator)
	}

	fullPath := filepath.Join(mountPath, filepath.Clean("/"+requestedPath))
	cleanFullPath := filepath.Clean(fullPath)

	if !strings.HasPrefix(cleanFullPath, cleanMountPath) {
		http.Error(w, "access denied", http.StatusForbidden)
		return
	}

	// Check if file is an image
	ext := strings.ToLower(filepath.Ext(cleanFullPath))
	var contentType string
	switch ext {
	case ".jpg", ".jpeg":
		contentType = "image/jpeg"
	case ".png":
		contentType = "image/png"
	case ".gif":
		contentType = "image/gif"
	case ".webp":
		contentType = "image/webp"
	default:
		http.Error(w, "unsupported file type", http.StatusBadRequest)
		return
	}

	// Serve the file
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "public, max-age=3600")
	http.ServeFile(w, r, cleanFullPath)
}

// handleNetworkDNS returns the DNS configuration
func handleNetworkDNS(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Read /etc/resolv.conf
	resolvConf, err := os.ReadFile("/etc/resolv.conf")
	if err != nil {
		log.Printf("Failed to read /etc/resolv.conf: %v", err)
		resolvConf = []byte("Unable to read /etc/resolv.conf")
	}

	jsonResponse(w, map[string]string{
		"resolv_conf": string(resolvConf),
	})
}

// handleNetworkInterfaces returns network interface information
func handleNetworkInterfaces(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get network interfaces using Go's net package
	interfaces, err := net.Interfaces()
	if err != nil {
		log.Printf("Failed to get network interfaces: %v", err)
		jsonResponse(w, map[string]interface{}{
			"interfaces": []map[string]interface{}{},
			"error":      err.Error(),
		})
		return
	}

	var result []map[string]interface{}
	for _, iface := range interfaces {
		ifaceInfo := make(map[string]interface{})
		ifaceInfo["name"] = iface.Name
		ifaceInfo["mac"] = iface.HardwareAddr.String()
		ifaceInfo["mtu"] = iface.MTU

		// Parse flags
		flags := []string{}
		if iface.Flags&net.FlagUp != 0 {
			flags = append(flags, "up")
		}
		if iface.Flags&net.FlagBroadcast != 0 {
			flags = append(flags, "broadcast")
		}
		if iface.Flags&net.FlagLoopback != 0 {
			flags = append(flags, "loopback")
		}
		if iface.Flags&net.FlagPointToPoint != 0 {
			flags = append(flags, "pointtopoint")
		}
		if iface.Flags&net.FlagMulticast != 0 {
			flags = append(flags, "multicast")
		}
		ifaceInfo["flags"] = flags

		// Get addresses
		addrs, err := iface.Addrs()
		if err == nil {
			var addresses []map[string]string
			for _, addr := range addrs {
				addrInfo := make(map[string]string)
				ipNet, ok := addr.(*net.IPNet)
				if ok {
					if ipNet.IP.To4() != nil {
						addrInfo["family"] = "inet"
						addrInfo["address"] = ipNet.String()
					} else {
						addrInfo["family"] = "inet6"
						addrInfo["address"] = ipNet.String()
					}
					addresses = append(addresses, addrInfo)
				}
			}
			ifaceInfo["addresses"] = addresses
		}

		result = append(result, ifaceInfo)
	}

	jsonResponse(w, map[string]interface{}{
		"interfaces": result,
	})
}

// handleDNSLookup performs DNS lookup
func handleDNSLookup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Hostname string `json:"hostname"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if req.Hostname == "" {
		http.Error(w, "hostname is required", http.StatusBadRequest)
		return
	}

	// Perform DNS lookup using Go's net package
	ips, err := net.LookupIP(req.Hostname)
	if err != nil {
		jsonResponse(w, map[string]interface{}{
			"error": fmt.Sprintf("DNS lookup failed: %v", err),
		})
		return
	}

	// Convert IPs to strings
	var addresses []string
	for _, ip := range ips {
		addresses = append(addresses, ip.String())
	}

	// Build raw output
	var rawOutput strings.Builder
	rawOutput.WriteString(fmt.Sprintf("Name: %s\n", req.Hostname))
	for _, ip := range ips {
		if ip.To4() != nil {
			rawOutput.WriteString(fmt.Sprintf("Address: %s (IPv4)\n", ip.String()))
		} else {
			rawOutput.WriteString(fmt.Sprintf("Address: %s (IPv6)\n", ip.String()))
		}
	}

	jsonResponse(w, map[string]interface{}{
		"addresses":  addresses,
		"raw_output": rawOutput.String(),
	})
}

// handlePing performs ICMP ping test
func handlePing(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Hostname string `json:"hostname"`
		Count    int    `json:"count"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if req.Hostname == "" {
		http.Error(w, "hostname is required", http.StatusBadRequest)
		return
	}

	if req.Count <= 0 {
		req.Count = 4
	}
	if req.Count > 10 {
		req.Count = 10
	}

	// Resolve hostname to IP
	ips, err := net.LookupIP(req.Hostname)
	if err != nil {
		jsonResponse(w, map[string]interface{}{
			"error": fmt.Sprintf("Failed to resolve hostname: %v", err),
		})
		return
	}

	if len(ips) == 0 {
		jsonResponse(w, map[string]interface{}{
			"error": "No IP addresses found for hostname",
		})
		return
	}

	// Use first IPv4 address
	var targetIP net.IP
	for _, ip := range ips {
		if ip.To4() != nil {
			targetIP = ip
			break
		}
	}
	if targetIP == nil {
		targetIP = ips[0] // Fallback to first IP (might be IPv6)
	}

	// Perform ping
	result := performICMPPing(req.Hostname, targetIP.String(), req.Count)
	jsonResponse(w, result)
}

// performICMPPing executes ICMP ping using go-fastping
func performICMPPing(hostname, ipAddr string, count int) map[string]interface{} {
	var output strings.Builder
	var rtts []time.Duration
	var successCount, failCount int

	output.WriteString(fmt.Sprintf("PING %s (%s) 56 bytes of data\n\n", hostname, ipAddr))

	p := fastping.NewPinger()
	ra, err := net.ResolveIPAddr("ip4:icmp", ipAddr)
	if err != nil {
		return map[string]interface{}{
			"error": fmt.Sprintf("Failed to resolve IP: %v", err),
		}
	}

	p.AddIPAddr(ra)
	p.MaxRTT = 3 * time.Second

	// Track responses
	responseChan := make(chan time.Duration, count)

	p.OnRecv = func(addr *net.IPAddr, rtt time.Duration) {
		responseChan <- rtt
	}

	// Run ping multiple times
	for i := 0; i < count; i++ {
		err := p.Run()
		if err != nil {
			failCount++
			output.WriteString(fmt.Sprintf("%d: Request timeout\n", i+1))
		} else {
			// Check if we got a response
			select {
			case rtt := <-responseChan:
				successCount++
				rtts = append(rtts, rtt)
				output.WriteString(fmt.Sprintf("%d: Reply from %s: time=%v\n", i+1, ipAddr, rtt))
			case <-time.After(3 * time.Second):
				failCount++
				output.WriteString(fmt.Sprintf("%d: Request timeout\n", i+1))
			}
		}

		// Sleep between pings (except for last one)
		if i < count-1 {
			time.Sleep(1 * time.Second)
		}
	}

	// Calculate statistics
	output.WriteString(fmt.Sprintf("\n--- %s ping statistics ---\n", hostname))
	output.WriteString(fmt.Sprintf("%d packets transmitted, %d received, %.1f%% packet loss\n",
		count, successCount, float64(failCount)/float64(count)*100))

	if successCount > 0 {
		var minRTT, maxRTT, totalRTT time.Duration
		minRTT = rtts[0]
		maxRTT = rtts[0]

		for _, rtt := range rtts {
			totalRTT += rtt
			if rtt < minRTT {
				minRTT = rtt
			}
			if rtt > maxRTT {
				maxRTT = rtt
			}
		}

		avgRTT := totalRTT / time.Duration(successCount)
		output.WriteString(fmt.Sprintf("rtt min/avg/max = %v/%v/%v\n", minRTT, avgRTT, maxRTT))
	}

	summary := fmt.Sprintf("%d packets transmitted, %d received, %.1f%% packet loss",
		count, successCount, float64(failCount)/float64(count)*100)

	return map[string]interface{}{
		"output":  output.String(),
		"summary": summary,
	}
}

// handleNetworkDiagnostics runs full network diagnostics
func handleNetworkDiagnostics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	result := make(map[string]interface{})

	// Test DNS servers (ICMP ping)
	result["dns_google"] = testICMPPing("8.8.8.8", 2*time.Second)
	result["dns_cloudflare"] = testICMPPing("1.1.1.1", 2*time.Second)

	// Test internet connectivity via DNS resolution and ping
	ips, err := net.LookupIP("google.com")
	if err == nil && len(ips) > 0 {
		// Try to ping the resolved IP
		result["internet_google"] = testICMPPing(ips[0].String(), 3*time.Second)
	} else {
		result["internet_google"] = false
	}

	ips, err = net.LookupIP("cloudflare.com")
	if err == nil && len(ips) > 0 {
		result["internet_cloudflare"] = testICMPPing(ips[0].String(), 3*time.Second)
	} else {
		result["internet_cloudflare"] = false
	}

	// Get default gateway from routes
	gateway := getDefaultGatewayFromRoutes()
	result["gateway"] = gateway

	if gateway != "" {
		// Test gateway reachability with ICMP ping
		result["gateway_reachable"] = testICMPPing(gateway, 2*time.Second)
	}

	// Read routing table from /proc/net/route
	routes := readRoutingTable()
	result["routes"] = routes

	jsonResponse(w, result)
}

// Helper functions for network debugging

// testICMPPing tests if a host responds to ICMP ping
func testICMPPing(ipAddr string, timeout time.Duration) bool {
	p := fastping.NewPinger()
	ra, err := net.ResolveIPAddr("ip4:icmp", ipAddr)
	if err != nil {
		return false
	}

	p.AddIPAddr(ra)
	p.MaxRTT = timeout

	responded := false
	p.OnRecv = func(addr *net.IPAddr, rtt time.Duration) {
		responded = true
	}

	err = p.Run()
	if err != nil {
		return false
	}

	return responded
}

// testTCPConnectivity tests if a TCP connection can be established
func testTCPConnectivity(address string, timeout time.Duration) bool {
	conn, err := net.DialTimeout("tcp", address, timeout)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// getDefaultGatewayFromRoutes reads the default gateway from /proc/net/route
func getDefaultGatewayFromRoutes() string {
	// Read /proc/net/route
	data, err := os.ReadFile("/proc/net/route")
	if err != nil {
		log.Printf("Failed to read /proc/net/route: %v", err)
		return ""
	}

	lines := strings.Split(string(data), "\n")
	for i, line := range lines {
		if i == 0 {
			continue // Skip header
		}
		fields := strings.Fields(line)
		if len(fields) >= 3 {
			// Check if destination is 00000000 (default route)
			if fields[1] == "00000000" {
				// Gateway is in field 2, in hex format (little-endian)
				gatewayHex := fields[2]
				if len(gatewayHex) == 8 {
					// Convert hex to IP (little-endian)
					ip := fmt.Sprintf("%d.%d.%d.%d",
						hexToInt(gatewayHex[6:8]),
						hexToInt(gatewayHex[4:6]),
						hexToInt(gatewayHex[2:4]),
						hexToInt(gatewayHex[0:2]))
					return ip
				}
			}
		}
	}
	return ""
}

// hexToInt converts hex string to int
func hexToInt(hex string) int {
	var val int
	fmt.Sscanf(hex, "%x", &val)
	return val
}

// readRoutingTable reads and formats the routing table
func readRoutingTable() string {
	data, err := os.ReadFile("/proc/net/route")
	if err != nil {
		return fmt.Sprintf("Failed to read routing table: %v", err)
	}

	lines := strings.Split(string(data), "\n")
	var output strings.Builder
	output.WriteString(fmt.Sprintf("%-15s %-15s %-15s %-7s\n", "Destination", "Gateway", "Genmask", "Iface"))
	output.WriteString(strings.Repeat("-", 60) + "\n")

	for i, line := range lines {
		if i == 0 || line == "" {
			continue // Skip header and empty lines
		}
		fields := strings.Fields(line)
		if len(fields) >= 8 {
			iface := fields[0]
			dest := hexToIP(fields[1])
			gateway := hexToIP(fields[2])
			mask := hexToIP(fields[7])

			output.WriteString(fmt.Sprintf("%-15s %-15s %-15s %-7s\n", dest, gateway, mask, iface))
		}
	}

	return output.String()
}

// hexToIP converts hex IP (little-endian) to dotted notation
func hexToIP(hex string) string {
	if len(hex) != 8 {
		return "0.0.0.0"
	}
	return fmt.Sprintf("%d.%d.%d.%d",
		hexToInt(hex[6:8]),
		hexToInt(hex[4:6]),
		hexToInt(hex[2:4]),
		hexToInt(hex[0:2]))
}

// jsonResponse writes a JSON response
func jsonResponse(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}
