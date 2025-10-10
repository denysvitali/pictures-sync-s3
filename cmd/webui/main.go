package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/denysvitali/pictures-sync-s3/pkg/settings"
	"github.com/denysvitali/pictures-sync-s3/pkg/state"
	"github.com/denysvitali/pictures-sync-s3/pkg/syncmanager"
	"github.com/denysvitali/pictures-sync-s3/pkg/wifimanager"
	"github.com/gorilla/websocket"
)

var (
	stateMgr   *state.Manager
	syncMgr    *syncmanager.Manager
	wifiMgr    *wifimanager.Manager
	appSettings *settings.Settings
	upgrader   = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return true // Allow all origins for now
		},
	}
)

func main() {
	log.Println("Photo Backup Station WebUI - Starting...")

	// Get port from environment or default to 8080
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	// Initialize state manager
	var err error
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
	)

	// Initialize WiFi manager
	wifiMgr, err = wifimanager.NewManager()
	if err != nil {
		log.Printf("Warning: Failed to initialize WiFi manager: %v", err)
	}

	// Setup HTTP handlers
	http.HandleFunc("/api/status", handleStatus)
	http.HandleFunc("/api/history", handleHistory)
	http.HandleFunc("/api/config", handleConfig)
	http.HandleFunc("/api/config/test", handleConfigTest)
	http.HandleFunc("/api/settings", handleSettings)
	http.HandleFunc("/api/wifi/scan", handleWiFiScan)
	http.HandleFunc("/api/wifi/networks", handleWiFiNetworks)
	http.HandleFunc("/api/wifi/connect", handleWiFiConnect)
	http.HandleFunc("/api/wifi/disconnect", handleWiFiDisconnect)
	http.HandleFunc("/api/wifi/status", handleWiFiStatus)
	http.HandleFunc("/ws", handleWebSocket)
	http.HandleFunc("/", handleIndex)

	// Start server
	addr := ":" + port
	log.Printf("WebUI server listening on %s", addr)
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
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
            <button class="tab" onclick="switchTab('history')">📚 History</button>
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
                    <button type="submit" class="btn btn-primary">💾 Save Settings</button>
                </form>
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
            if (tabName === 'history') loadHistory();
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

            let html = '<div class="progress">';
            html += '<div class="progress-bar" style="width: ' + percent.toFixed(1) + '%">' + percent.toFixed(1) + '%</div>';
            html += '</div>';

            html += '<div class="info-grid">';
            html += '<div class="info-item"><label>Files</label><value>' + sync.files_synced + ' / ' + sync.files_total + '</value></div>';
            html += '<div class="info-item"><label>Data Transferred</label><value>' + formatBytes(sync.bytes_transferred) + '</value></div>';

            if (sync.transfer_speed) {
                html += '<div class="info-item"><label>Speed</label><value>' + formatBytes(sync.transfer_speed) + '/s</value></div>';
            }

            if (sync.eta) {
                html += '<div class="info-item"><label>ETA</label><value>' + sync.eta + '</value></div>';
            }

            html += '</div>';

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

                        html += '<div class="history-item ' + statusClass + '">';
                        html += '<div style="display: flex; justify-content: space-between; margin-bottom: 0.5rem;">';
                        html += '<strong>' + statusIcon + ' ' + new Date(item.timestamp).toLocaleString() + '</strong>';
                        html += '<span class="code">' + (item.card_id || 'N/A') + '</span>';
                        html += '</div>';

                        if (item.files_synced) {
                            html += '<p>Files: ' + item.files_synced + ' (' + formatBytes(item.bytes_transferred || 0) + ')';
                            if (item.duration) {
                                html += ' in ' + formatDuration(item.duration);
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
                .then(r => r.json())
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
                        if (network.security) html += ' 🔒';
                        html += '</div>';
                        html += '<div style="display: flex; gap: 1rem; align-items: center;">';
                        html += '<div class="signal-strength signal-' + signalStrength + '">';
                        html += '<div class="signal-bar"></div><div class="signal-bar"></div><div class="signal-bar"></div><div class="signal-bar"></div>';
                        html += '</div>';
                        html += '<button class="btn btn-primary" onclick="connectWiFi(\'' + network.ssid + '\')">Connect</button>';
                        html += '</div></div>';
                    });

                    networksDiv.innerHTML = html;
                })
                .catch(err => {
                    networksDiv.innerHTML = '<p class="alert alert-error">Scan failed: ' + err.message + '</p>';
                });
        }

        function connectWiFi(ssid) {
            const password = prompt('Enter password for ' + ssid + ':');
            if (!password) return;

            fetch('/api/wifi/connect', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ ssid, password })
            })
            .then(r => r.json())
            .then(data => {
                alert('WiFi network added. Connection may take a few moments.');
                loadWiFiStatus();
            })
            .catch(err => alert('Failed to connect: ' + err.message));
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
                reformat_threshold: parseFloat(document.getElementById('reformat-threshold').value) / 100
            };

            fetch('/api/settings', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(settings)
            })
            .then(r => r.json())
            .then(data => {
                showConfigAlert('Settings saved successfully!', 'success');
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
			RemoteName        string  `json:"remote_name"`
			RemotePath        string  `json:"remote_path"`
			ReformatThreshold float64 `json:"reformat_threshold"`
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

	// Send initial state
	status := stateMgr.GetState()
	if err := conn.WriteJSON(status); err != nil {
		return
	}

	// Send updates
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case state := <-updates:
			if err := conn.WriteJSON(state); err != nil {
				return
			}
		case <-ticker.C:
			// Ping to keep connection alive
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// jsonResponse writes a JSON response
func jsonResponse(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}
