// WebSocket connection
let ws = null;
let reconnectInterval = null;

// Status icons
const statusIcons = {
    idle: '⏸️',
    detected: '💿',
    syncing: '⏳',
    success: '✅',
    error: '❌'
};

// Initialize app
document.addEventListener('DOMContentLoaded', () => {
    initTabs();
    initForms();
    connectWebSocket();
    loadStatus();
    loadHistory();
    loadWiFiNetworks();
    loadCurrentSSID();
});

// Tab navigation
function initTabs() {
    const tabs = document.querySelectorAll('.tab');
    const tabContents = document.querySelectorAll('.tab-content');

    tabs.forEach(tab => {
        tab.addEventListener('click', () => {
            const tabName = tab.dataset.tab;

            tabs.forEach(t => t.classList.remove('active'));
            tabContents.forEach(tc => tc.classList.remove('active'));

            tab.classList.add('active');
            document.getElementById(`${tabName}-tab`).classList.add('active');
        });
    });
}

// Initialize forms
function initForms() {
    // WiFi form
    document.getElementById('wifi-form').addEventListener('submit', handleWiFiSubmit);

    // Config form
    document.getElementById('config-form').addEventListener('submit', handleConfigSubmit);
    document.getElementById('test-config-btn').addEventListener('click', testConnection);

    // Scan button
    document.getElementById('scan-btn').addEventListener('click', scanNetworks);
}

// WebSocket connection
function connectWebSocket() {
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const wsUrl = `${protocol}//${window.location.host}/ws`;

    ws = new WebSocket(wsUrl);

    ws.onopen = () => {
        console.log('WebSocket connected');
        updateConnectionStatus(true);
        if (reconnectInterval) {
            clearInterval(reconnectInterval);
            reconnectInterval = null;
        }
    };

    ws.onmessage = (event) => {
        const data = JSON.parse(event.data);
        handleWebSocketMessage(data);
    };

    ws.onclose = () => {
        console.log('WebSocket disconnected');
        updateConnectionStatus(false);

        // Reconnect after 3 seconds
        if (!reconnectInterval) {
            reconnectInterval = setInterval(() => {
                console.log('Attempting to reconnect...');
                connectWebSocket();
            }, 3000);
        }
    };

    ws.onerror = (error) => {
        console.error('WebSocket error:', error);
        updateConnectionStatus(false, true);
    };
}

// Handle WebSocket messages
function handleWebSocketMessage(data) {
    if (data.type === 'state') {
        updateStatus(data.state);
    } else if (data.type === 'progress') {
        updateProgress(data.progress);
    }
}

// Update connection status indicator
function updateConnectionStatus(connected, error = false) {
    const indicator = document.getElementById('connection-status');
    const text = indicator.querySelector('.text');

    indicator.classList.remove('connected', 'error');

    if (connected) {
        indicator.classList.add('connected');
        text.textContent = 'Connected';
    } else if (error) {
        indicator.classList.add('error');
        text.textContent = 'Connection Error';
    } else {
        text.textContent = 'Disconnected';
    }
}

// Load current status
async function loadStatus() {
    try {
        const response = await fetch('/api/status');
        const data = await response.json();
        updateStatus(data);
    } catch (error) {
        console.error('Failed to load status:', error);
    }
}

// Update status display
function updateStatus(state) {
    const statusIcon = document.getElementById('status-icon');
    const statusText = document.getElementById('status-text');
    const sdCardInfo = document.getElementById('sd-card-info');
    const progressSection = document.getElementById('progress-section');

    // Update icon and text
    statusIcon.textContent = statusIcons[state.status] || '⏸️';

    switch (state.status) {
        case 'idle':
            statusText.textContent = 'Idle - Waiting for SD card';
            sdCardInfo.classList.add('hidden');
            progressSection.classList.add('hidden');
            break;
        case 'detected':
            statusText.textContent = 'SD Card Detected';
            if (state.sdcard_mounted) {
                sdCardInfo.classList.remove('hidden');
                document.getElementById('sdcard-id').textContent = state.sdcard_path || '-';
                document.getElementById('mount-path').textContent = state.sdcard_path || '-';
            }
            progressSection.classList.add('hidden');
            break;
        case 'syncing':
            statusText.textContent = 'Syncing Photos...';
            sdCardInfo.classList.remove('hidden');
            progressSection.classList.remove('hidden');
            if (state.current_sync) {
                updateSyncProgress(state.current_sync);
            }
            break;
        case 'success':
            statusText.textContent = 'Sync Complete!';
            progressSection.classList.remove('hidden');
            if (state.last_sync) {
                updateSyncProgress(state.last_sync);
            }
            break;
        case 'error':
            statusText.textContent = 'Sync Failed';
            progressSection.classList.remove('hidden');
            break;
    }
}

// Update sync progress
function updateSyncProgress(sync) {
    const percentage = sync.bytes_total > 0
        ? Math.round((sync.bytes_synced / sync.bytes_total) * 100)
        : 0;

    document.getElementById('progress-fill').style.width = `${percentage}%`;
    document.getElementById('progress-text').textContent = `${percentage}%`;

    document.getElementById('files-progress').textContent =
        `${sync.files_synced || 0} / ${sync.files_total || 0}`;

    const bytesSynced = formatBytes(sync.bytes_synced || 0);
    const bytesTotal = formatBytes(sync.bytes_total || 0);
    document.getElementById('bytes-progress').textContent = `${bytesSynced} / ${bytesTotal}`;
}

// Update progress from WebSocket
function updateProgress(progress) {
    document.getElementById('progress-fill').style.width = `${progress.percentage}%`;
    document.getElementById('progress-text').textContent = `${progress.percentage}%`;

    document.getElementById('files-progress').textContent =
        `${progress.transferred_files} / ${progress.total_files}`;

    const bytesSynced = formatBytes(progress.bytes);
    document.getElementById('bytes-progress').textContent = bytesSynced;

    const speed = formatBytes(progress.speed);
    document.getElementById('speed').textContent = `${speed}/s`;

    const eta = progress.eta > 0 ? formatDuration(progress.eta) : '-';
    document.getElementById('eta').textContent = eta;
}

// Load sync history
async function loadHistory() {
    try {
        const response = await fetch('/api/history');
        const history = await response.json();
        displayHistory(history);
    } catch (error) {
        console.error('Failed to load history:', error);
    }
}

// Display sync history
function displayHistory(history) {
    const historyList = document.getElementById('history-list');

    if (!history || history.length === 0) {
        historyList.innerHTML = '<p class="empty-state">No sync history yet</p>';
        return;
    }

    historyList.innerHTML = history.map(item => {
        const startTime = new Date(item.start_time).toLocaleString();
        const duration = item.end_time
            ? Math.round((new Date(item.end_time) - new Date(item.start_time)) / 1000)
            : 0;

        return `
            <div class="history-item ${item.status}">
                <div class="history-item-header">
                    <span class="history-item-status">${item.status.toUpperCase()}</span>
                    <span class="history-item-time">${startTime}</span>
                </div>
                <div class="history-item-stats">
                    ${item.files_synced} files • ${formatBytes(item.bytes_synced)} • ${formatDuration(duration)}
                    ${item.error ? `<br><span style="color: #ef4444;">Error: ${item.error}</span>` : ''}
                </div>
            </div>
        `;
    }).join('');
}

// WiFi form submission
async function handleWiFiSubmit(e) {
    e.preventDefault();

    const ssid = document.getElementById('ssid').value;
    const password = document.getElementById('password').value;

    try {
        const response = await fetch('/api/wifi/configure', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ ssid, password })
        });

        if (response.ok) {
            alert('WiFi network added successfully! The device will attempt to connect.');
            document.getElementById('wifi-form').reset();
            loadWiFiNetworks();
        } else {
            const error = await response.text();
            alert(`Failed to add network: ${error}`);
        }
    } catch (error) {
        alert(`Error: ${error.message}`);
    }
}

// Scan for WiFi networks
async function scanNetworks() {
    const btn = document.getElementById('scan-btn');
    btn.disabled = true;
    btn.textContent = 'Scanning...';

    try {
        const response = await fetch('/api/wifi/scan');
        const networks = await response.json();
        displayAvailableNetworks(networks);
    } catch (error) {
        alert(`Scan failed: ${error.message}`);
    } finally {
        btn.disabled = false;
        btn.textContent = 'Scan for Networks';
    }
}

// Display available networks
function displayAvailableNetworks(networks) {
    const container = document.getElementById('available-networks');
    const list = document.getElementById('networks');

    if (!networks || networks.length === 0) {
        list.innerHTML = '<p class="empty-state">No networks found</p>';
        return;
    }

    container.classList.remove('hidden');

    list.innerHTML = networks.map(network => `
        <div class="network-item">
            <div class="network-info">
                <div class="network-name">${escapeHtml(network.ssid)}</div>
                <div class="network-details">
                    Signal: ${network.signal} dBm • ${network.encrypted ? '🔒 Secured' : '🔓 Open'}
                </div>
            </div>
            <div class="network-actions">
                <button class="button" onclick="selectNetwork('${escapeHtml(network.ssid)}')">Select</button>
            </div>
        </div>
    `).join('');
}

// Select a network from scan results
function selectNetwork(ssid) {
    document.getElementById('ssid').value = ssid;
    document.querySelector('[data-tab="wifi"]').click();
    document.getElementById('ssid').focus();
}

// Load saved WiFi networks
async function loadWiFiNetworks() {
    try {
        const response = await fetch('/api/wifi/networks');
        const networks = await response.json();
        displaySavedNetworks(networks);
    } catch (error) {
        console.error('Failed to load WiFi networks:', error);
    }
}

// Display saved networks
function displaySavedNetworks(networks) {
    const list = document.getElementById('saved-networks');

    if (!networks || networks.length === 0) {
        list.innerHTML = '<p class="empty-state">No saved networks</p>';
        return;
    }

    list.innerHTML = networks.map(network => `
        <div class="network-item">
            <div class="network-info">
                <div class="network-name">${escapeHtml(network.ssid)}</div>
            </div>
            <div class="network-actions">
                <button class="button" onclick="removeNetwork('${escapeHtml(network.ssid)}')">Remove</button>
            </div>
        </div>
    `).join('');
}

// Remove a saved network
async function removeNetwork(ssid) {
    if (!confirm(`Remove network "${ssid}"?`)) {
        return;
    }

    try {
        const response = await fetch(`/api/wifi/remove`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ ssid })
        });

        if (response.ok) {
            loadWiFiNetworks();
        } else {
            alert('Failed to remove network');
        }
    } catch (error) {
        alert(`Error: ${error.message}`);
    }
}

// Load current SSID
async function loadCurrentSSID() {
    try {
        const response = await fetch('/api/wifi/current');
        const data = await response.json();
        document.getElementById('current-ssid').textContent = data.ssid || 'Not connected';
    } catch (error) {
        console.error('Failed to load current SSID:', error);
    }
}

// Config form submission
async function handleConfigSubmit(e) {
    e.preventDefault();

    const remoteName = document.getElementById('remote-name').value;
    const remotePath = document.getElementById('remote-path').value;
    const configText = document.getElementById('config-text').value;

    try {
        const response = await fetch('/api/config', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                remote_name: remoteName,
                remote_path: remotePath,
                config: configText
            })
        });

        const statusDiv = document.getElementById('config-status');
        statusDiv.classList.remove('hidden');

        if (response.ok) {
            statusDiv.className = 'success-message';
            statusDiv.textContent = 'Configuration saved successfully!';
        } else {
            const error = await response.text();
            statusDiv.className = 'error-message';
            statusDiv.textContent = `Failed to save configuration: ${error}`;
        }
    } catch (error) {
        const statusDiv = document.getElementById('config-status');
        statusDiv.className = 'error-message';
        statusDiv.classList.remove('hidden');
        statusDiv.textContent = `Error: ${error.message}`;
    }
}

// Test rclone connection
async function testConnection() {
    const btn = document.getElementById('test-config-btn');
    btn.disabled = true;
    btn.textContent = 'Testing...';

    try {
        const response = await fetch('/api/config/test');
        const statusDiv = document.getElementById('config-status');
        statusDiv.classList.remove('hidden');

        if (response.ok) {
            statusDiv.className = 'success-message';
            statusDiv.textContent = 'Connection test successful!';
        } else {
            const error = await response.text();
            statusDiv.className = 'error-message';
            statusDiv.textContent = `Connection test failed: ${error}`;
        }
    } catch (error) {
        const statusDiv = document.getElementById('config-status');
        statusDiv.className = 'error-message';
        statusDiv.classList.remove('hidden');
        statusDiv.textContent = `Error: ${error.message}`;
    } finally {
        btn.disabled = false;
        btn.textContent = 'Test Connection';
    }
}

// Utility functions
function formatBytes(bytes) {
    if (bytes === 0) return '0 B';
    const k = 1024;
    const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
}

function formatDuration(seconds) {
    if (seconds < 60) return `${seconds}s`;
    const minutes = Math.floor(seconds / 60);
    const secs = seconds % 60;
    return `${minutes}m ${secs}s`;
}

function escapeHtml(text) {
    const div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
}
