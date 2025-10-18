# Captive Portal Authenticator

Automatic captive portal authentication for WiFi networks, specifically designed for hotel and public WiFi scenarios.

## Overview

The captive portal authenticator monitors WiFi connection state and automatically authenticates to known networks that require captive portal login. It runs as a background service that polls the WiFi manager every 10 seconds to detect network changes and perform authentication when needed.

## Supported Networks

Currently supported networks:

- **JinJiangRewards** - JinJiang Hotels WiFi network

## Features

- **Automatic Detection**: Automatically detects when connected to a captive portal network
- **Automatic Authentication**: Sends authentication requests with device IP and MAC address
- **Retry Logic**: Implements exponential backoff retry (3 attempts with 2s, 4s, 6s delays)
- **Comprehensive Logging**: Detailed logs at all stages with `[CaptivePortal]` prefix
- **Edge Case Handling**: Handles network disconnections, IP assignment delays, timeouts
- **Manual Triggering**: API endpoints for manual authentication and testing
- **Health Monitoring**: Tracks authentication status and age

## Architecture

### Components

1. **Authenticator** (`captiveportal.go`)
   - Main service that monitors WiFi state
   - Polls every 10 seconds for network changes
   - Manages authentication state and timing

2. **Network-Specific Authenticators**
   - `authenticateJinJiang()` - JinJiang Hotels authentication
   - Extensible design for adding more networks

3. **Helper Functions**
   - `getLocalIPAndMAC()` - Retrieves network identifiers
   - Handles multiple interfaces, loopback, IPv6, etc.

4. **API Handlers** (`/pkg/handlers/captive_portal.go`)
   - Status endpoint
   - Manual authentication endpoint
   - Testing/debugging endpoint

## Usage

### Integration in Daemon

```go
// Initialize WiFi manager
wifiMgr, _ := wifimanager.NewManager()

// Create captive portal authenticator
captivePortal := captiveportal.NewAuthenticator(func() (string, error) {
    return wifiMgr.GetCurrentSSID()
})

// Start background monitoring
captivePortal.Start()

// Stop when shutting down
defer captivePortal.Stop()
```

### Manual Authentication via API

**Check Status:**
```bash
curl http://localhost:8080/api/captive-portal/status
```

**Trigger Authentication:**
```bash
# Normal authentication (respects recent auth check)
curl -X POST http://localhost:8080/api/captive-portal/authenticate

# Force re-authentication
curl -X POST http://localhost:8080/api/captive-portal/authenticate \
  -H "Content-Type: application/json" \
  -d '{"force": true}'
```

**Test Network Identifiers:**
```bash
curl http://localhost:8080/api/captive-portal/test
```

## Logging

All operations are logged with the `[CaptivePortal]` prefix for easy filtering:

### Log Levels

**Connection State:**
```
[CaptivePortal] Connected to network: 'JinJiangRewards'
[CaptivePortal] Disconnected from network 'JinJiangRewards': not connected
```

**Authentication Process:**
```
[CaptivePortal] === Starting authentication process for 'JinJiangRewards' ===
[CaptivePortal] Network identifiers retrieved - IP: 192.168.1.100, MAC: aa:bb:cc:dd:ee:ff
[CaptivePortal:JinJiang] Starting authentication with IP=192.168.1.100, MAC=aa:bb:cc:dd:ee:ff
[CaptivePortal:JinJiang] Portal URL: http://...
[CaptivePortal:JinJiang] Sending HTTP GET request...
[CaptivePortal:JinJiang] Response received after 234ms
[CaptivePortal:JinJiang] Status: 200 OK
[CaptivePortal:JinJiang] SUCCESS: Authentication accepted by portal (status 200)
[CaptivePortal] ✓ Authentication successful on attempt 1
[CaptivePortal] === Authentication process completed successfully ===
```

**Error Scenarios:**
```
[CaptivePortal] ERROR: Failed to get local IP/MAC for 'JinJiangRewards': no active wireless interface found
[CaptivePortal] EDGE CASE: Cannot authenticate without network identifiers
[CaptivePortal] Possible causes:
[CaptivePortal]   - Interface not fully initialized
[CaptivePortal]   - No IPv4 address assigned yet
[CaptivePortal]   - Wireless interface not detected
[CaptivePortal] Will retry on next poll interval (10s)
```

**Retry Logic:**
```
[CaptivePortal] ✗ Attempt 1/3 failed: HTTP request failed: dial tcp: connection refused
[CaptivePortal] Retry attempt 2/3 after 2s backoff...
[CaptivePortal] ✗ Attempt 2/3 failed: HTTP request failed: dial tcp: connection refused
[CaptivePortal] Retry attempt 3/3 after 4s backoff...
[CaptivePortal] ✓ Authentication successful on attempt 3
```

### Filtering Logs

To view only captive portal logs:
```bash
journalctl -u pictures-sync | grep "\[CaptivePortal"
```

To view only JinJiang-specific logs:
```bash
journalctl -u pictures-sync | grep "\[CaptivePortal:JinJiang"
```

## Edge Cases Handled

### Network Disconnection
- Detects disconnection and clears state
- Logs only when transitioning from connected to disconnected
- No spam when already disconnected

### IP/MAC Retrieval Failures
- Handles interface not initialized
- Handles no IPv4 address assigned (DHCP delay)
- Handles no wireless interface found
- Provides detailed diagnostics in logs

### Authentication Failures
- HTTP timeouts (10 second timeout)
- Connection errors
- Non-success status codes (400, 401, 403, 404, 500, etc.)
- Too many redirects (>10)
- Provides troubleshooting hints based on error type

### Network Changes
- Detects SSID changes
- Resets authentication state on network change
- Re-authenticates to new network if needed

### Recent Authentication
- Skips re-authentication within 5 minute window
- Prevents authentication spam
- Can be overridden with manual force flag

### Concurrency
- Thread-safe operations
- Multiple goroutines can safely call checkAndAuthenticate()
- No race conditions in state updates

## Adding Support for New Networks

To add support for a new captive portal network:

### 1. Add Constants

```go
const (
    myHotelSSID = "MyHotelWiFi"
    myHotelAuthURL = "http://captive.myhotel.com/auth"
)
```

### 2. Create Authenticator Function

```go
func authenticateMyHotel(ip, mac string) error {
    log.Printf("[CaptivePortal:MyHotel] Starting authentication with IP=%s, MAC=%s", ip, mac)

    url := fmt.Sprintf("%s?ip=%s&mac=%s", myHotelAuthURL, ip, mac)
    log.Printf("[CaptivePortal:MyHotel] Portal URL: %s", url)

    ctx, cancel := context.WithTimeout(context.Background(), authTimeout)
    defer cancel()

    req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
    if err != nil {
        log.Printf("[CaptivePortal:MyHotel] ERROR: Failed to create request: %v", err)
        return fmt.Errorf("failed to create request: %w", err)
    }

    client := &http.Client{Timeout: authTimeout}
    resp, err := client.Do(req)
    if err != nil {
        log.Printf("[CaptivePortal:MyHotel] ERROR: Request failed: %v", err)
        return fmt.Errorf("request failed: %w", err)
    }
    defer resp.Body.Close()

    log.Printf("[CaptivePortal:MyHotel] Response: %d %s", resp.StatusCode, resp.Status)

    if resp.StatusCode >= 200 && resp.StatusCode < 400 {
        log.Printf("[CaptivePortal:MyHotel] SUCCESS: Authenticated")
        return nil
    }

    return fmt.Errorf("authentication failed with status: %d", resp.StatusCode)
}
```

### 3. Register in NewAuthenticator()

```go
func NewAuthenticator(getCurrentSSID func() (string, error)) *Authenticator {
    return &Authenticator{
        getCurrentSSID: getCurrentSSID,
        getLocalIPMAC:  getLocalIPAndMAC,
        stopChan:       make(chan struct{}),
        authenticators: map[string]AuthFunc{
            jinjiangSSID: authenticateJinJiang,
            myHotelSSID:  authenticateMyHotel,  // Add new entry
        },
    }
}
```

### 4. Add Tests

```go
func TestAuthenticateMyHotel(t *testing.T) {
    // Test implementation
}
```

## API Endpoints

### GET /api/captive-portal/status

Returns current authentication status.

**Response:**
```json
{
  "authenticated": true,
  "network": "JinJiangRewards",
  "authenticated_at": "2025-10-17T14:30:00Z",
  "age_seconds": 120,
  "health_check_running": true,
  "enabled": true
}
```

### POST /api/captive-portal/authenticate

Manually triggers authentication.

**Request:**
```json
{
  "force": true  // Optional: force re-authentication
}
```

**Response (success):**
```json
{
  "success": true,
  "message": "Authentication successful",
  "network": "JinJiangRewards",
  "authenticated_attempt": true
}
```

**Response (error):**
```json
{
  "success": false,
  "error": "HTTP request failed: connection timeout",
  "network": "JinJiangRewards",
  "authenticated_attempt": true
}
```

### GET /api/captive-portal/test

Tests network detection and retrieves identifiers (debugging).

**Response:**
```json
{
  "current_network": "JinJiangRewards",
  "requires_auth": true,
  "local_ip": "192.168.1.100",
  "local_mac": "aa:bb:cc:dd:ee:ff",
  "connected": true
}
```

## Testing

### Run All Tests
```bash
go test ./pkg/captiveportal/...
```

### Run Edge Case Tests Only
```bash
go test ./pkg/captiveportal/ -run EdgeCase
```

### Run Without Network Tests
```bash
go test ./pkg/captiveportal/ -short
```

### Test Coverage
```bash
go test ./pkg/captiveportal/ -cover
```

## Troubleshooting

### Authentication Not Triggering

1. **Check if network is supported:**
   ```bash
   curl http://localhost:8080/api/captive-portal/test
   ```
   Look for `"requires_auth": true`

2. **Check recent authentication:**
   ```bash
   curl http://localhost:8080/api/captive-portal/status
   ```
   If `age_seconds < 300`, authentication was recent (within 5 minutes)

3. **Force authentication:**
   ```bash
   curl -X POST http://localhost:8080/api/captive-portal/authenticate \
     -H "Content-Type: application/json" \
     -d '{"force": true}'
   ```

### Cannot Get IP/MAC Address

1. **Check wireless interfaces:**
   ```bash
   ip link show | grep -E 'wlan|wlp'
   ```

2. **Check IPv4 assignment:**
   ```bash
   ip addr show wlan0
   ```

3. **Check logs for diagnostics:**
   ```bash
   journalctl -u pictures-sync | grep "interface search summary"
   ```

### Authentication Failing

1. **Check portal URL accessibility:**
   ```bash
   curl -v http://swrz-ruijie.jinjianghotels.com.cn:8888/auth/wifidogAuth/portal/
   ```

2. **Review detailed logs:**
   ```bash
   journalctl -u pictures-sync | grep "\[CaptivePortal:JinJiang"
   ```

3. **Check HTTP status codes:**
   - 400: Bad request - check IP/MAC format
   - 401/403: Unauthorized - may need additional credentials
   - 404: Portal URL changed
   - 500+: Portal server issues

## Performance Considerations

- **Polling Interval**: 10 seconds (configurable via `pollInterval` constant)
- **Authentication Timeout**: 10 seconds per attempt
- **Retry Delays**: 2s, 4s backoff (exponential)
- **Recent Auth Window**: 5 minutes (prevents re-auth spam)

## Security Notes

- IP and MAC addresses are logged (acceptable for debugging)
- No passwords or sensitive credentials are logged
- HTTP requests use standard timeout to prevent hanging
- Redirect limit (10) prevents redirect loops
