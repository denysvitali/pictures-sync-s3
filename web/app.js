// WebSocket connection state
let ws = null;
let reconnectAttempts = 0;
let reconnectTimeout = null;
let heartbeatInterval = null;
let lastHeartbeat = null;
let isIntentionalClose = false;

// WebSocket configuration
const WS_CONFIG = {
    maxReconnectDelay: 30000,      // Maximum 30 seconds between reconnection attempts
    initialReconnectDelay: 1000,   // Start with 1 second
    heartbeatInterval: 30000,      // Send ping every 30 seconds
    heartbeatTimeout: 10000,       // Expect pong within 10 seconds
    maxReconnectAttempts: Infinity // Keep trying forever
};

// WebSocket message types (must match server)
const WS_MESSAGE_TYPES = {
    AUTH: 'auth',
    AUTH_SUCCESS: 'auth_success',
    STATE: 'state',
    EVENT: 'event',
    ERROR: 'error',
    PING: 'ping',
    PONG: 'pong'
};

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

    // Event delegation for network action buttons
    document.addEventListener('click', (e) => {
        if (e.target.matches('button[data-action="select-network"]')) {
            e.preventDefault();
            const ssid = e.target.getAttribute('data-ssid');
            if (ssid) selectNetwork(ssid);
        } else if (e.target.matches('button[data-action="remove-network"]')) {
            e.preventDefault();
            const ssid = e.target.getAttribute('data-ssid');
            if (ssid) removeNetwork(ssid);
        }
    });

    // Cleanup on page unload
    window.addEventListener('beforeunload', () => {
        isIntentionalClose = true;
        if (ws) {
            ws.close();
        }
    });
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

// WebSocket connection with authentication and reconnection
async function connectWebSocket() {
    try {
        // Clear any existing reconnection timeout
        if (reconnectTimeout) {
            clearTimeout(reconnectTimeout);
            reconnectTimeout = null;
        }

        // Get authentication token from server
        updateConnectionStatus(false, false, 'Authenticating...');
        const tokenResponse = await fetch('/api/ws-token');
        if (!tokenResponse.ok) {
            throw new Error('Failed to get WebSocket token');
        }
        const tokenData = await tokenResponse.json();
        const token = tokenData.token;

        // Connect to WebSocket
        const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
        const wsUrl = `${protocol}//${window.location.host}/ws`;

        updateConnectionStatus(false, false, 'Connecting...');
        ws = new WebSocket(wsUrl);

        // Set up event handlers
        ws.onopen = () => {
            console.log('WebSocket connected, sending auth token');
            // Send authentication token as first message
            ws.send(JSON.stringify({
                type: WS_MESSAGE_TYPES.AUTH,
                token: token
            }));
        };

        ws.onmessage = (event) => {
            try {
                const data = JSON.parse(event.data);
                handleWebSocketMessage(data);
            } catch (error) {
                console.error('Failed to parse WebSocket message:', error);
            }
        };

        ws.onclose = (event) => {
            console.log('WebSocket disconnected', event.code, event.reason);
            stopHeartbeat();
            updateConnectionStatus(false, false);

            // Don't reconnect if this was intentional
            if (isIntentionalClose) {
                return;
            }

            // Schedule reconnection with exponential backoff
            scheduleReconnection();
        };

        ws.onerror = (error) => {
            console.error('WebSocket error:', error);
            updateConnectionStatus(false, true, 'Connection error');
        };

    } catch (error) {
        console.error('Failed to connect WebSocket:', error);
        updateConnectionStatus(false, true, error.message);
        scheduleReconnection();
    }
}

// Schedule WebSocket reconnection with exponential backoff
function scheduleReconnection() {
    if (reconnectTimeout) {
        return; // Already scheduled
    }

    reconnectAttempts++;
    const delay = Math.min(
        WS_CONFIG.initialReconnectDelay * Math.pow(2, reconnectAttempts - 1),
        WS_CONFIG.maxReconnectDelay
    );

    console.log(`Scheduling reconnection attempt ${reconnectAttempts} in ${delay}ms`);
    updateConnectionStatus(false, false, `Reconnecting in ${Math.ceil(delay / 1000)}s...`);

    reconnectTimeout = setTimeout(() => {
        reconnectTimeout = null;
        console.log(`Reconnection attempt ${reconnectAttempts}`);
        connectWebSocket();
    }, delay);
}

// Start heartbeat monitoring
function startHeartbeat() {
    stopHeartbeat(); // Clear any existing heartbeat

    heartbeatInterval = setInterval(() => {
        if (ws && ws.readyState === WebSocket.OPEN) {
            // Check if we received a recent pong
            if (lastHeartbeat && Date.now() - lastHeartbeat > WS_CONFIG.heartbeatInterval + WS_CONFIG.heartbeatTimeout) {
                console.warn('Heartbeat timeout - connection appears stale');
                ws.close();
                return;
            }

            // Send ping
            try {
                ws.send(JSON.stringify({ type: WS_MESSAGE_TYPES.PING }));
            } catch (error) {
                console.error('Failed to send heartbeat:', error);
            }
        }
    }, WS_CONFIG.heartbeatInterval);
}

// Stop heartbeat monitoring
function stopHeartbeat() {
    if (heartbeatInterval) {
        clearInterval(heartbeatInterval);
        heartbeatInterval = null;
    }
    lastHeartbeat = null;
}

// Handle WebSocket messages with type validation
function handleWebSocketMessage(data) {
    // Validate message structure
    if (!data || typeof data !== 'object' || !data.type) {
        console.warn('Invalid WebSocket message format:', data);
        return;
    }

    // Handle different message types
    switch (data.type) {
        case WS_MESSAGE_TYPES.AUTH_SUCCESS:
            console.log('WebSocket authenticated successfully');
            updateConnectionStatus(true);
            reconnectAttempts = 0; // Reset reconnection counter on success
            startHeartbeat(); // Start monitoring connection health
            break;

        case WS_MESSAGE_TYPES.STATE:
            if (data.data) {
                updateStatus(data.data);
            } else {
                console.warn('State message missing data:', data);
            }
            break;

        case WS_MESSAGE_TYPES.EVENT:
            if (data.data) {
                handleRealtimeEvent(data.data);
            } else {
                console.warn('Event message missing data:', data);
            }
            break;

        case WS_MESSAGE_TYPES.ERROR:
            console.error('WebSocket error from server:', data.error);
            showNotification('Connection error: ' + (data.error || 'Unknown error'), 'error');
            break;

        case WS_MESSAGE_TYPES.PONG:
            // Update last heartbeat time
            lastHeartbeat = Date.now();
            break;

        default:
            console.warn('Unknown WebSocket message type:', data.type);
    }
}

// Handle real-time events from the server
function handleRealtimeEvent(event) {
    if (!event || !event.type) {
        return;
    }

    console.log('Real-time event:', event.type, event.message);

    // Show notification for important events
    const notificationEvents = [
        'sd_card_inserted',
        'sd_card_removed',
        'sync_started',
        'sync_completed',
        'sync_failed',
        'error'
    ];

    if (notificationEvents.includes(event.type)) {
        const isError = event.type === 'error' || event.type === 'sync_failed';
        showNotification(event.message, isError ? 'error' : 'info');
    }

    // Refresh history when sync completes
    if (event.type === 'sync_completed' || event.type === 'sync_failed') {
        loadHistory();
    }
}

// Show user notification
function showNotification(message, type = 'info') {
    // You can enhance this with a toast/notification UI component
    console.log(`[${type.toUpperCase()}] ${message}`);

    // For now, use browser notifications if available
    if ('Notification' in window && Notification.permission === 'granted') {
        new Notification('Photo Backup Station', {
            body: message,
            icon: type === 'error' ? '/static/icons/error.png' : '/static/icons/info.png'
        });
    }
}

// Update connection status indicator with detailed state
function updateConnectionStatus(connected, error = false, customMessage = null) {
    const indicator = document.getElementById('connection-status');
    if (!indicator) return; // Element may not exist on all pages

    const text = indicator.querySelector('.text');
    if (!text) return;

    indicator.classList.remove('connected', 'error', 'connecting');

    if (connected) {
        indicator.classList.add('connected');
        text.textContent = 'Connected';
    } else if (error) {
        indicator.classList.add('error');
        text.textContent = customMessage || 'Connection Error';
    } else if (customMessage) {
        indicator.classList.add('connecting');
        text.textContent = customMessage;
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
                    <span class="history-item-status">${escapeHtml(item.status).toUpperCase()}</span>
                    <span class="history-item-time">${startTime}</span>
                </div>
                <div class="history-item-stats">
                    ${item.files_synced} files • ${formatBytes(item.bytes_synced)} • ${formatDuration(duration)}
                    ${item.error ? `<br><span style="color: #ef4444;">Error: ${escapeHtml(item.error)}</span>` : ''}
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
                <button class="button" data-action="select-network" data-ssid="${escapeHtml(network.ssid)}">Select</button>
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
                <button class="button" data-action="remove-network" data-ssid="${escapeHtml(network.ssid)}">Remove</button>
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
