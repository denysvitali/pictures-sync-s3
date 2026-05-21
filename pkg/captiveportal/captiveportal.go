// Package captiveportal handles automatic captive portal authentication for WiFi networks.
// It monitors WiFi connection state and performs authentication when connected to known networks.
//
// The authenticator polls the WiFi manager every 10 seconds to check for network changes.
// When connected to a recognized captive portal network (like JinJiangRewards), it automatically
// sends an authentication request to the portal's URL using the device's current IP and MAC address.
//
// ADDING SUPPORT FOR NEW NETWORKS:
//
// To add support for a new captive portal network:
//  1. Add constants for the SSID and authentication URL
//  2. Create an authentication function with signature: func(ip, mac string) error
//  3. Register the authenticator in NewAuthenticator() by adding to the authenticators map
//
// Example:
//
//	const myHotelSSID = "MyHotelWiFi"
//	const myHotelAuthURL = "http://captive.myhotel.com/auth"
//
//	func authenticateMyHotel(ip, mac string) error {
//	    url := fmt.Sprintf("%s?ip=%s&mac=%s", myHotelAuthURL, ip, mac)
//	    // ... perform HTTP request
//	}
//
//	// In NewAuthenticator():
//	authenticators: map[string]AuthFunc{
//	    jinjiangSSID: authenticateJinJiang,
//	    myHotelSSID:  authenticateMyHotel,  // Add new entry
//	}
//
// LOGGING:
//
// All authentication attempts are logged with the [CaptivePortal] prefix for easy filtering.
// Logs include:
//   - Network connection/disconnection events
//   - Authentication attempts with IP/MAC addresses
//   - HTTP request details and responses
//   - Error conditions with troubleshooting hints
package captiveportal

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	// pollInterval is how often to check for network changes
	pollInterval = 10 * time.Second

	// authTimeout is the timeout for authentication HTTP requests
	authTimeout = 10 * time.Second

	// jinjiangSSID is the SSID of the JinJiang Rewards WiFi network
	jinjiangSSID = "JinJiangRewards"

	// jinjiangAuthURL is the base URL for JinJiang authentication
	jinjiangAuthURL = "http://swrz-ruijie.jinjianghotels.com.cn:8888/auth/wifidogAuth/portal/"
)

// Authenticator handles captive portal authentication
type Authenticator struct {
	mu             sync.Mutex
	lastSSID       string
	lastAuthTime   time.Time
	getCurrentSSID func() (string, error)
	getLocalIPMAC  func() (string, string, error)
	retryBackoff   func(attempt int) time.Duration
	stopChan       chan struct{}
	stopOnce       sync.Once
	authenticators map[string]AuthFunc
}

// AuthFunc is a function that performs authentication for a specific network
type AuthFunc func(ip, mac string) error

// NewAuthenticator creates a new captive portal authenticator
func NewAuthenticator(getCurrentSSID func() (string, error)) *Authenticator {
	return &Authenticator{
		getCurrentSSID: getCurrentSSID,
		getLocalIPMAC:  getLocalIPAndMAC,
		retryBackoff: func(attempt int) time.Duration {
			return time.Duration(attempt-1) * 2 * time.Second
		},
		stopChan: make(chan struct{}),
		authenticators: map[string]AuthFunc{
			jinjiangSSID: authenticateJinJiang,
		},
	}
}

// Start begins monitoring for captive portal networks
func (a *Authenticator) Start() {
	log.Println("Captive portal authenticator starting...")
	go a.run()
}

// Stop stops the authenticator
func (a *Authenticator) Stop() {
	log.Println("Captive portal authenticator stopping...")
	a.stopOnce.Do(func() {
		close(a.stopChan)
	})
}

// run is the main loop that monitors network state and authenticates
func (a *Authenticator) run() {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	// Perform initial check
	a.checkAndAuthenticate()

	for {
		select {
		case <-ticker.C:
			a.checkAndAuthenticate()
		case <-a.stopChan:
			log.Println("Captive portal authenticator stopped")
			return
		}
	}
}

// checkAndAuthenticate checks current network and authenticates if needed
func (a *Authenticator) checkAndAuthenticate() {
	a.mu.Lock()
	defer a.mu.Unlock()
	ssid, err := a.getCurrentSSID()
	if err != nil {
		// Not connected to any network, just return silently
		// Only log if we were previously connected (to avoid spam)
		if a.lastSSID != "" {
			log.Printf("[CaptivePortal] Disconnected from network '%s': %v", a.lastSSID, err)
		}
		a.lastSSID = ""
		return
	}

	// Log network change
	if ssid != a.lastSSID {
		if a.lastSSID == "" {
			log.Printf("[CaptivePortal] Connected to network: '%s'", ssid)
		} else {
			log.Printf("[CaptivePortal] Network changed from '%s' to '%s'", a.lastSSID, ssid)
		}
		a.lastSSID = ssid
		// Reset auth time when network changes
		a.lastAuthTime = time.Time{}
	}

	// Check if this network needs authentication
	authFunc, needsAuth := a.authenticators[ssid]
	if !needsAuth {
		// Only log once when first connecting to non-portal network
		if a.lastSSID == ssid && a.lastAuthTime.IsZero() {
			log.Printf("[CaptivePortal] Network '%s' does not require captive portal authentication", ssid)
			a.lastAuthTime = time.Now() // Prevent repeated logging
		}
		return
	}

	// Check if we recently authenticated (avoid spamming)
	timeSinceLastAuth := time.Since(a.lastAuthTime)
	if timeSinceLastAuth < 5*time.Minute && !a.lastAuthTime.IsZero() {
		log.Printf("[CaptivePortal] Skipping re-authentication for '%s' (last auth was %v ago)",
			ssid, timeSinceLastAuth.Round(time.Second))
		return
	}

	log.Printf("[CaptivePortal] === Starting authentication process for '%s' ===", ssid)

	// Get local IP and MAC with detailed error handling
	ip, mac, err := a.getLocalIPMAC()
	if err != nil {
		log.Printf("[CaptivePortal] ERROR: Failed to get local IP/MAC for '%s': %v", ssid, err)
		log.Printf("[CaptivePortal] EDGE CASE: Cannot authenticate without network identifiers")
		log.Printf("[CaptivePortal] Possible causes:")
		log.Printf("[CaptivePortal]   - Interface not fully initialized")
		log.Printf("[CaptivePortal]   - No IPv4 address assigned yet")
		log.Printf("[CaptivePortal]   - Wireless interface not detected")
		log.Printf("[CaptivePortal] Will retry on next poll interval (%v)", pollInterval)
		return
	}

	log.Printf("[CaptivePortal] Network identifiers retrieved - IP: %s, MAC: %s", ip, mac)
	log.Printf("[CaptivePortal] Sending authentication request to portal...")

	// Perform authentication with retry logic
	maxRetries := 3
	var authErr error
	for attempt := 1; attempt <= maxRetries; attempt++ {
		if attempt > 1 {
			backoff := a.retryBackoff(attempt)
			log.Printf("[CaptivePortal] Retry attempt %d/%d after %v backoff...", attempt, maxRetries, backoff)
			if backoff > 0 {
				time.Sleep(backoff)
			}
		}

		authErr = authFunc(ip, mac)
		if authErr == nil {
			// Success!
			log.Printf("[CaptivePortal] ✓ Authentication successful on attempt %d", attempt)
			log.Printf("[CaptivePortal] Network '%s' is now authenticated", ssid)
			a.lastAuthTime = time.Now()
			log.Printf("[CaptivePortal] === Authentication process completed successfully ===")
			return
		}

		log.Printf("[CaptivePortal] ✗ Attempt %d/%d failed: %v", attempt, maxRetries, authErr)
	}

	// All retries exhausted
	log.Printf("[CaptivePortal] ERROR: Authentication failed for '%s' after %d attempts", ssid, maxRetries)
	log.Printf("[CaptivePortal] Last error: %v", authErr)
	log.Printf("[CaptivePortal] TROUBLESHOOTING:")
	log.Printf("[CaptivePortal]   - Check if portal URL is accessible")
	log.Printf("[CaptivePortal]   - Verify IP/MAC parameters are correct")
	log.Printf("[CaptivePortal]   - Check network connectivity")
	log.Printf("[CaptivePortal]   - Portal may require additional steps")
	log.Printf("[CaptivePortal] === Authentication process failed ===")
}

// authenticateJinJiang performs authentication for JinJiang Rewards WiFi
//
// Authentication Flow:
// 1. Build authentication URL with IP and MAC parameters
// 2. Send GET request to portal with browser-like User-Agent
// 3. Follow redirects (up to 10) to handle portal workflow
// 4. Check response status (2xx/3xx = success)
//
// Edge Cases Handled:
// - Request timeout (10 seconds)
// - Too many redirects (>10)
// - Network errors during request
// - Non-success status codes
func authenticateJinJiang(ip, mac string) error {
	log.Printf("[CaptivePortal:JinJiang] Starting authentication with IP=%s, MAC=%s", ip, mac)

	// Build the authentication URL with parameters
	// Note: gw_id and gw_sn are specific to JinJiang portal infrastructure
	// Example: http://swrz-ruijie.jinjianghotels.com.cn:8888/auth/wifidogAuth/portal/?gw_id=00749cb295f8&gw_sn=H1M10N0000783&ip=172.31.30.153&mac=ce:3f:16:a4:ae:93
	url := fmt.Sprintf("%s?gw_id=00749cb295f8&gw_sn=H1M10N0000783&ip=%s&mac=%s",
		jinjiangAuthURL, ip, mac)

	log.Printf("[CaptivePortal:JinJiang] Portal URL: %s", url)
	log.Printf("[CaptivePortal:JinJiang] Request timeout: %v", authTimeout)

	// Create HTTP client with timeout
	ctx, cancel := context.WithTimeout(context.Background(), authTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		log.Printf("[CaptivePortal:JinJiang] ERROR: Failed to create HTTP request: %v", err)
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Set user agent to mimic a browser (some portals check this)
	userAgent := "Mozilla/5.0 (X11; Linux armv7l) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
	req.Header.Set("User-Agent", userAgent)
	log.Printf("[CaptivePortal:JinJiang] User-Agent: %s", userAgent)

	// Track redirect count for logging
	redirectCount := 0

	client := &http.Client{
		Timeout: authTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			redirectCount++
			// Allow up to 10 redirects
			if len(via) >= 10 {
				log.Printf("[CaptivePortal:JinJiang] ERROR: Too many redirects (%d)", len(via))
				return fmt.Errorf("too many redirects")
			}
			log.Printf("[CaptivePortal:JinJiang] Redirect #%d: %s", redirectCount, req.URL.String())
			return nil
		},
	}

	log.Printf("[CaptivePortal:JinJiang] Sending HTTP GET request...")
	startTime := time.Now()

	resp, err := client.Do(req)
	if err != nil {
		duration := time.Since(startTime)
		log.Printf("[CaptivePortal:JinJiang] ERROR: HTTP request failed after %v: %v", duration, err)

		// Provide specific error context
		if ctx.Err() == context.DeadlineExceeded {
			log.Printf("[CaptivePortal:JinJiang] EDGE CASE: Request timed out (exceeded %v)", authTimeout)
		}
		return fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	duration := time.Since(startTime)
	log.Printf("[CaptivePortal:JinJiang] Response received after %v", duration)
	log.Printf("[CaptivePortal:JinJiang] Status: %d %s", resp.StatusCode, resp.Status)

	// Log response headers for debugging (helps diagnose portal issues)
	log.Printf("[CaptivePortal:JinJiang] Response headers:")
	for key, values := range resp.Header {
		for _, value := range values {
			log.Printf("[CaptivePortal:JinJiang]   %s: %s", key, value)
		}
	}

	// Accept any 2xx or 3xx status as success
	if resp.StatusCode >= 200 && resp.StatusCode < 400 {
		log.Printf("[CaptivePortal:JinJiang] SUCCESS: Authentication accepted by portal (status %d)", resp.StatusCode)
		if redirectCount > 0 {
			log.Printf("[CaptivePortal:JinJiang] Completed with %d redirect(s)", redirectCount)
		}
		return nil
	}

	// Authentication failed - provide detailed error
	log.Printf("[CaptivePortal:JinJiang] ERROR: Portal rejected authentication")
	log.Printf("[CaptivePortal:JinJiang] HTTP Status: %d %s", resp.StatusCode, resp.Status)

	// Provide troubleshooting hints based on status code
	switch resp.StatusCode {
	case 400:
		log.Printf("[CaptivePortal:JinJiang] Hint: Bad request - check IP/MAC format")
	case 401, 403:
		log.Printf("[CaptivePortal:JinJiang] Hint: Unauthorized - portal may require additional credentials")
	case 404:
		log.Printf("[CaptivePortal:JinJiang] Hint: Portal URL not found - portal configuration may have changed")
	case 500, 502, 503:
		log.Printf("[CaptivePortal:JinJiang] Hint: Portal server error - temporary issue, retry may help")
	case 504:
		log.Printf("[CaptivePortal:JinJiang] Hint: Portal gateway timeout - network connectivity issue")
	}

	return fmt.Errorf("authentication failed with status: %d %s", resp.StatusCode, resp.Status)
}

// getLocalIPAndMAC returns the local IP address and MAC address for the active network interface
//
// Edge Cases Handled:
// - No network interfaces available (system error)
// - Multiple interfaces (selects first wireless interface)
// - Loopback and down interfaces (skipped)
// - Interface without IP address (skipped)
// - Interface with only IPv6 (skipped, needs IPv4)
// - Non-wireless interfaces (skipped for captive portal use case)
func getLocalIPAndMAC() (string, string, error) {
	log.Printf("[CaptivePortal] Retrieving local IP and MAC address...")

	interfaces, err := net.Interfaces()
	if err != nil {
		log.Printf("[CaptivePortal] ERROR: Failed to enumerate network interfaces: %v", err)
		return "", "", fmt.Errorf("failed to get interfaces: %w", err)
	}

	log.Printf("[CaptivePortal] Found %d network interface(s)", len(interfaces))

	// Track what we're finding for debugging
	checkedCount := 0
	skippedLoopback := 0
	skippedDown := 0
	skippedNonWireless := 0
	skippedNoIPv4 := 0

	// Look for a wireless interface with an IP address
	for _, iface := range interfaces {
		checkedCount++
		log.Printf("[CaptivePortal] Checking interface #%d: %s", checkedCount, iface.Name)

		// Skip loopback interfaces
		if iface.Flags&net.FlagLoopback != 0 {
			log.Printf("[CaptivePortal]   -> Skipped: loopback interface")
			skippedLoopback++
			continue
		}

		// Skip interfaces that are down
		if iface.Flags&net.FlagUp == 0 {
			log.Printf("[CaptivePortal]   -> Skipped: interface is down")
			skippedDown++
			continue
		}

		// Look for wireless interfaces (wlan0, wlp*, etc.)
		name := iface.Name
		if !strings.HasPrefix(name, "wlan") && !strings.HasPrefix(name, "wlp") {
			log.Printf("[CaptivePortal]   -> Skipped: not a wireless interface (name: %s)", name)
			skippedNonWireless++
			continue
		}

		log.Printf("[CaptivePortal]   -> Found wireless interface: %s (flags: %v)", name, iface.Flags)

		// Get addresses for this interface
		addrs, err := iface.Addrs()
		if err != nil {
			log.Printf("[CaptivePortal]   -> WARNING: Failed to get addresses: %v", err)
			continue
		}

		log.Printf("[CaptivePortal]   -> Interface has %d address(es)", len(addrs))

		// Find IPv4 address
		for addrIdx, addr := range addrs {
			log.Printf("[CaptivePortal]   -> Checking address #%d: %s", addrIdx+1, addr.String())

			ipNet, ok := addr.(*net.IPNet)
			if !ok {
				log.Printf("[CaptivePortal]     -> Skipped: not an IP network address")
				continue
			}

			ipv4 := ipNet.IP.To4()
			if ipv4 == nil {
				log.Printf("[CaptivePortal]     -> Skipped: IPv6 address (need IPv4)")
				skippedNoIPv4++
				continue
			}

			// Found a wireless interface with IPv4 address!
			mac := iface.HardwareAddr.String()
			ip := ipv4.String()

			log.Printf("[CaptivePortal] SUCCESS: Selected interface %s", name)
			log.Printf("[CaptivePortal]   IP:  %s", ip)
			log.Printf("[CaptivePortal]   MAC: %s", mac)
			return ip, mac, nil
		}
	}

	// No suitable interface found - provide detailed diagnostics
	log.Printf("[CaptivePortal] ERROR: No suitable wireless interface found")
	log.Printf("[CaptivePortal] Interface search summary:")
	log.Printf("[CaptivePortal]   Total interfaces: %d", len(interfaces))
	log.Printf("[CaptivePortal]   Skipped (loopback): %d", skippedLoopback)
	log.Printf("[CaptivePortal]   Skipped (down): %d", skippedDown)
	log.Printf("[CaptivePortal]   Skipped (non-wireless): %d", skippedNonWireless)
	log.Printf("[CaptivePortal]   Skipped (no IPv4): %d", skippedNoIPv4)
	log.Printf("[CaptivePortal] TROUBLESHOOTING:")
	log.Printf("[CaptivePortal]   - Ensure WiFi interface is up and connected")
	log.Printf("[CaptivePortal]   - Check if DHCP has assigned an IPv4 address")
	log.Printf("[CaptivePortal]   - Verify wireless interface naming (wlan* or wlp*)")

	return "", "", fmt.Errorf("no active wireless interface found")
}

// ClearAuthenticationState clears the authentication state, forcing re-authentication on next check
// This is useful for testing or when manual re-authentication is needed
func (a *Authenticator) ClearAuthenticationState() {
	log.Printf("[CaptivePortal] Clearing authentication state (manual trigger)")
	a.mu.Lock()
	a.lastAuthTime = time.Time{}
	a.mu.Unlock()
	log.Printf("[CaptivePortal] Authentication state cleared, next check will trigger authentication")
}

// GetLocalIPMAC exposes the getLocalIPAndMAC function for API/testing purposes
// Returns the local IP address and MAC address for the active wireless interface
func (a *Authenticator) GetLocalIPMAC() (string, string, error) {
	return a.getLocalIPMAC()
}

// Authenticate manually triggers the authentication check
// This is a public wrapper around checkAndAuthenticate for API/testing use
func (a *Authenticator) Authenticate() {
	a.checkAndAuthenticate()
}

// GetAuthenticationStatus returns the current authentication status
// This provides visibility into the authenticator's state for monitoring and debugging
func (a *Authenticator) GetAuthenticationStatus() map[string]interface{} {
	a.mu.Lock()
	defer a.mu.Unlock()
	authenticated := !a.lastAuthTime.IsZero()

	status := map[string]interface{}{
		"authenticated":        authenticated,
		"health_check_running": a.stopChan != nil,
	}

	if authenticated {
		status["network"] = a.lastSSID
		status["authenticated_at"] = a.lastAuthTime.Format(time.RFC3339)
		status["age_seconds"] = int(time.Since(a.lastAuthTime).Seconds())
	}

	return status
}
