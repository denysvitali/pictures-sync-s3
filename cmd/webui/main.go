package main

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/denysvitali/pictures-sync-s3/pkg/sdmonitor"
	"github.com/denysvitali/pictures-sync-s3/pkg/settings"
	"github.com/denysvitali/pictures-sync-s3/pkg/state"
	"github.com/denysvitali/pictures-sync-s3/pkg/syncmanager"
	"github.com/denysvitali/pictures-sync-s3/pkg/wifimanager"
	"github.com/gorilla/websocket"
	"github.com/nfnt/resize"
)

var (
	stateMgr   *state.Manager
	syncMgr    *syncmanager.Manager
	wifiMgr    *wifimanager.Manager
	appSettings *settings.Settings
	authPassword string
	upgrader   = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return true // Allow all origins for now
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

func main() {
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

	// Setup HTTP handlers
	http.HandleFunc("/api/status", handleStatus)
	http.HandleFunc("/api/history", handleHistory)
	http.HandleFunc("/api/config", handleConfig)
	http.HandleFunc("/api/config/test", handleConfigTest)
	http.HandleFunc("/api/settings", handleSettings)
	http.HandleFunc("/api/devices", handleDevices)
	http.HandleFunc("/api/devices/select", handleDeviceSelect)
	http.HandleFunc("/api/wifi/scan", handleWiFiScan)
	http.HandleFunc("/api/wifi/networks", handleWiFiNetworks)
	http.HandleFunc("/api/wifi/connect", handleWiFiConnect)
	http.HandleFunc("/api/wifi/disconnect", handleWiFiDisconnect)
	http.HandleFunc("/api/wifi/status", handleWiFiStatus)
	http.HandleFunc("/api/files", handleFiles)
	http.HandleFunc("/api/files/view", handleFileView)
	http.HandleFunc("/api/thumbnail", handleThumbnail)
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
            --success: #16a34a;
            --warning: #ea580c;
            --error: #dc2626;
            --bg: #f8fafc;
            --card: #ffffff;
            --text: #1e293b;
            --text-secondary: #64748b;
            --border: #e2e8f0;
            --shadow: 0 1px 3px rgba(0,0,0,0.1);
            --shadow-lg: 0 10px 25px rgba(0,0,0,0.1);
        }

        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', 'Roboto', 'Oxygen', 'Ubuntu', sans-serif;
            background: var(--bg);
            color: var(--text);
            line-height: 1.6;
        }

        .header {
            background: var(--card);
            border-bottom: 1px solid var(--border);
            padding: 1.5rem 2rem;
            box-shadow: var(--shadow);
        }

        .header h1 {
            font-size: 1.5rem;
            font-weight: 700;
            color: var(--primary);
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
            border-radius: 0.5rem;
            padding: 1.5rem;
            margin-bottom: 1.5rem;
            box-shadow: var(--shadow);
        }

        .card h2 {
            font-size: 1.25rem;
            font-weight: 600;
            margin-bottom: 1rem;
        }

        .status-badge {
            display: inline-block;
            padding: 0.25rem 0.75rem;
            border-radius: 9999px;
            font-size: 0.875rem;
            font-weight: 600;
            text-transform: uppercase;
        }

        .badge-idle { background: #dbeafe; color: #1e40af; }
        .badge-detected { background: #fed7aa; color: #c2410c; }
        .badge-syncing { background: #fef3c7; color: #a16207; }
        .badge-success { background: #dcfce7; color: #15803d; }
        .badge-error { background: #fee2e2; color: #991b1b; }

        .progress {
            width: 100%;
            height: 2rem;
            background: var(--border);
            border-radius: 0.5rem;
            overflow: hidden;
            margin: 1rem 0;
        }

        .progress-bar {
            height: 100%;
            background: linear-gradient(90deg, var(--primary), var(--primary-dark));
            transition: width 0.3s ease;
            display: flex;
            align-items: center;
            justify-content: center;
            color: white;
            font-weight: 600;
            font-size: 0.875rem;
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
            border: 1px solid var(--border);
            border-radius: 0.375rem;
            font-size: 1rem;
            font-family: inherit;
            transition: border-color 0.2s;
        }

        .form-input:focus, .form-textarea:focus {
            outline: none;
            border-color: var(--primary);
            box-shadow: 0 0 0 3px rgba(37, 99, 235, 0.1);
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
            border-radius: 0.375rem;
            font-size: 1rem;
            font-weight: 500;
            cursor: pointer;
            transition: all 0.2s;
        }

        .btn-primary {
            background: var(--primary);
            color: white;
        }

        .btn-primary:hover {
            background: var(--primary-dark);
        }

        .btn-secondary {
            background: var(--border);
            color: var(--text);
        }

        .btn-secondary:hover {
            background: #cbd5e1;
        }

        .btn:disabled {
            opacity: 0.5;
            cursor: not-allowed;
        }

        .alert {
            padding: 1rem;
            border-radius: 0.375rem;
            margin-bottom: 1rem;
        }

        .alert-success {
            background: #dcfce7;
            color: #15803d;
            border: 1px solid #bbf7d0;
        }

        .alert-error {
            background: #fee2e2;
            color: #991b1b;
            border: 1px solid #fecaca;
        }

        .alert-info {
            background: #dbeafe;
            color: #1e40af;
            border: 1px solid #bfdbfe;
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
            padding: 2rem;
            color: var(--text-secondary);
        }

        .spinner {
            border: 3px solid var(--border);
            border-top-color: var(--primary);
            border-radius: 50%;
            width: 40px;
            height: 40px;
            animation: spin 1s linear infinite;
            margin: 0 auto 1rem;
        }

        @keyframes spin {
            to { transform: rotate(360deg); }
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
    </style>
</head>
<body>
    <div class="header">
        <h1>📸 Photo Backup Station</h1>
    </div>

    <div class="container">
        <div class="tabs">
            <button class="tab active" onclick="switchTab('status')">📊 Status</button>
            <button class="tab" onclick="switchTab('devices')">💾 Devices</button>
            <button class="tab" onclick="switchTab('history')">📚 History</button>
            <button class="tab" onclick="switchTab('files')">📁 Files</button>
            <button class="tab" onclick="switchTab('wifi')">📡 WiFi</button>
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
                <h2>Remote Files</h2>
                <div id="files-alert"></div>

                <div style="margin-bottom: 1rem;">
                    <div id="current-path" style="font-size: 0.875rem; color: var(--text-secondary); margin-bottom: 0.5rem;">
                        Path: <span class="code" id="path-display">/</span>
                    </div>
                    <button class="btn btn-secondary" onclick="loadFiles('')">🏠 Root</button>
                    <button class="btn btn-secondary" onclick="refreshFiles()">🔄 Refresh</button>
                </div>

                <div id="files-display">
                    <div class="loading">
                        <div class="spinner"></div>
                        <p>Loading files...</p>
                    </div>
                </div>
            </div>
        </div>
    </div>

    <script>
        let ws;
        let reconnectInterval;

        // Tab switching
        function switchTab(tabName) {
            document.querySelectorAll('.tab').forEach(tab => tab.classList.remove('active'));
            document.querySelectorAll('.tab-content').forEach(content => content.classList.remove('active'));

            event.target.classList.add('active');
            document.getElementById(tabName + '-tab').classList.add('active');

            // Load data for the selected tab
            if (tabName === 'devices') refreshDevices();
            if (tabName === 'history') loadHistory();
            if (tabName === 'files') loadFiles('');
            if (tabName === 'wifi') loadWiFiStatus();
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

            let badgeClass = 'badge-' + data.status;
            let statusText = data.status.toUpperCase();

            let html = '<div class="info-grid">';
            html += '<div class="info-item"><label>Status</label><value><span class="status-badge ' + badgeClass + '">' + statusText + '</span></value></div>';
            html += '<div class="info-item"><label>SD Card</label><value>' + (data.sdcard_mounted ? '✓ Mounted' : '✗ Not mounted') + '</value></div>';

            if (data.sdcard_path) {
                html += '<div class="info-item"><label>Mount Path</label><value class="code">' + data.sdcard_path + '</value></div>';
            }

            if (data.card_id) {
                html += '<div class="info-item"><label>Card ID</label><value class="code">' + data.card_id + '</value></div>';
            }

            html += '</div>';

            statusDiv.innerHTML = html;

            // Show sync progress if syncing
            if (data.current_sync && data.status === 'syncing') {
                syncDetails.style.display = 'block';
                updateSyncProgress(data.current_sync);
            } else {
                syncDetails.style.display = 'none';
            }
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
                        html += '<div><strong>' + network.ssid + '</strong>';
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
                        html += '<strong>' + network.ssid + '</strong>';
                        html += '<button class="btn btn-secondary" onclick="removeWiFi(\'' + network.ssid + '\')">Remove</button>';
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
        let currentFilesPath = '';

        function loadFiles(path) {
            currentFilesPath = path;
            const pathDisplay = document.getElementById('path-display');
            pathDisplay.textContent = path || '/';

            const filesDisplay = document.getElementById('files-display');
            filesDisplay.innerHTML = '<div class="loading"><div class="spinner"></div><p>Loading files...</p></div>';

            fetch('/api/files?path=' + encodeURIComponent(path))
                .then(r => r.json())
                .then(data => {
                    if (data.error) {
                        showFilesAlert(data.error, 'error');
                        filesDisplay.innerHTML = '<p style="color: var(--text-secondary); text-align: center; padding: 2rem;">Failed to load files</p>';
                        return;
                    }

                    displayFiles(data.files, path);
                })
                .catch(err => {
                    showFilesAlert('Failed to load files: ' + err.message, 'error');
                    filesDisplay.innerHTML = '<p style="color: var(--text-secondary); text-align: center; padding: 2rem;">Failed to load files</p>';
                });
        }

        function refreshFiles() {
            loadFiles(currentFilesPath);
        }

        function displayFiles(files, path) {
            const filesDisplay = document.getElementById('files-display');

            if (!files || files.length === 0) {
                filesDisplay.innerHTML = '<p style="color: var(--text-secondary); text-align: center; padding: 2rem;">No files found</p>';
                return;
            }

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

                html += '<tr style="border-bottom: 1px solid var(--border);">';

                if (file.is_dir) {
                    html += '<td style="padding: 0.75rem;"><a href="#" onclick="loadFiles(\'' + filePath + '\'); return false;" style="color: var(--primary); text-decoration: none; font-weight: 500;">' + icon + ' ' + fileName + '</a></td>';
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
		// Return current config status
		hasConfig, err := state.EnsureRcloneConfig()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		remotes, _ := syncMgr.ListRemotes()
		jsonResponse(w, map[string]interface{}{
			"configured": hasConfig,
			"remotes":    remotes,
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
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}
	defer conn.Close()

	// Subscribe to state updates
	updates := stateMgr.Subscribe()

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
		case state := <-updates:
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

// handleThumbnail serves thumbnail images for files being synced
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
	filePath := r.URL.Query().Get("path")
	if filePath == "" {
		http.Error(w, "path parameter required", http.StatusBadRequest)
		return
	}

	// Security: Ensure the file is within the SD card mount path
	mountPath := state.MountDir
	if !strings.HasPrefix(filepath.Clean(filePath), filepath.Clean(mountPath)) {
		http.Error(w, "access denied", http.StatusForbidden)
		return
	}

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

// jsonResponse writes a JSON response
func jsonResponse(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}
