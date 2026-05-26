package websocket

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/denysvitali/pictures-sync-s3/pkg/events"
	"github.com/denysvitali/pictures-sync-s3/pkg/ota"
	"github.com/denysvitali/pictures-sync-s3/pkg/state"
	"github.com/gorilla/websocket"
	"golang.org/x/time/rate"
)

// rateLimiterEntry pairs a rate limiter with its last-seen timestamp,
// enabling per-IP idle-time eviction without dropping active limiters.
type rateLimiterEntry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// rateLimiterIdleTimeout is how long an IP entry can be idle before eviction.
const rateLimiterIdleTimeout = 15 * time.Minute

// ConnectionRateLimiter tracks rate limits per IP address
type ConnectionRateLimiter struct {
	limiters map[string]*rateLimiterEntry
	mu       sync.RWMutex
}

// NewConnectionRateLimiter creates a new rate limiter for WebSocket connections
func NewConnectionRateLimiter() *ConnectionRateLimiter {
	return &ConnectionRateLimiter{
		limiters: make(map[string]*rateLimiterEntry),
	}
}

// GetLimiter returns or creates a rate limiter for the given IP
// Allows 10 connections per minute with burst of 3
func (c *ConnectionRateLimiter) GetLimiter(ip string) *rate.Limiter {
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, exists := c.limiters[ip]
	if !exists {
		limit, burst := getConnectionRateConfig()
		entry = &rateLimiterEntry{
			limiter:  rate.NewLimiter(limit, burst),
			lastSeen: now,
		}
		c.limiters[ip] = entry
	} else {
		entry.lastSeen = now
	}
	return entry.limiter
}

// CleanupOldLimiters evicts per-IP limiter entries that have been idle past
// rateLimiterIdleTimeout. Active IPs keep their limiter state, so attackers
// can't reset their rate window simply by waiting for the periodic clear.
func (c *ConnectionRateLimiter) CleanupOldLimiters(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			c.mu.Lock()
			for ip, entry := range c.limiters {
				if now.Sub(entry.lastSeen) > rateLimiterIdleTimeout {
					delete(c.limiters, ip)
				}
			}
			c.mu.Unlock()
		}
	}
}

// StartRateLimiterCleanup starts the rate limiter cleanup goroutine
func StartRateLimiterCleanup(ctx context.Context) {
	getConnectionRateLimiter().CleanupOldLimiters(ctx)
}

// maxWSTokens caps the number of outstanding WebSocket auth tokens to bound
// memory consumption against issuance abuse. When at capacity, oldest tokens
// are evicted before new tokens are accepted.
const maxWSTokens = 1000

// maxIncomingMessageBytes caps the size of any single WebSocket frame the
// server is willing to read from a client. Bounds memory usage against a
// hostile client sending oversized messages (especially before auth, when
// the connection is still anonymous).
const maxIncomingMessageBytes = 64 * 1024

var (
	wsTokens            = make(map[string]time.Time) // WebSocket auth tokens with expiry
	wsTokenMutex        sync.RWMutex
	connRateLimiter     *ConnectionRateLimiter
	connRateLimiterOnce sync.Once
	connectionRateLimit = rate.Every(6 * time.Second)
	connectionRateBurst = 3
	// wsConfigMutex guards the mutable package-level config knobs above
	// (connRateLimiter pointer, rate/burst, and authReadTimeout) which can be
	// re-set in tests while connection handlers concurrently read them.
	wsConfigMutex sync.RWMutex
	allowedOrigins      = []string{} // Configurable whitelist (empty = same-host only by default)
	allowedOriginsMutex sync.RWMutex
	// trustLANOrigins controls whether origins on private/RFC1918 networks (and
	// localhost / *.local) are auto-trusted for WebSocket upgrades. SECURITY:
	// defaults to false. Operators must opt-in explicitly via SetTrustLANOrigins.
	trustLANOrigins      bool
	trustLANOriginsMutex sync.RWMutex
	authReadTimeout      = 5 * time.Second

	// wsTokenIssuanceLimiters rate-limits WS-token issuance per client IP to
	// prevent token-map flooding. Entries are evicted alongside other idle state.
	wsTokenIssuanceLimiters = make(map[string]*rateLimiterEntry)
	wsTokenIssuanceMutex    sync.Mutex

	upgrader = websocket.Upgrader{
		CheckOrigin: checkOriginStrict,
	}
)

// SetTrustLANOrigins enables or disables LAN/private-IP origin auto-trust for
// WebSocket connections. Default is disabled; explicit allowlists via
// SetAllowedOrigins are preferred.
func SetTrustLANOrigins(trust bool) {
	trustLANOriginsMutex.Lock()
	defer trustLANOriginsMutex.Unlock()
	trustLANOrigins = trust
}

func lanOriginsTrusted() bool {
	trustLANOriginsMutex.RLock()
	defer trustLANOriginsMutex.RUnlock()
	return trustLANOrigins
}

// AllowWSTokenIssuance returns true if an additional WS-token may be issued
// for the given client IP. Allows up to ~10 tokens/minute with a burst of 5.
func AllowWSTokenIssuance(ip string) bool {
	now := time.Now()
	wsTokenIssuanceMutex.Lock()
	defer wsTokenIssuanceMutex.Unlock()

	// Opportunistically evict idle entries.
	for k, entry := range wsTokenIssuanceLimiters {
		if now.Sub(entry.lastSeen) > rateLimiterIdleTimeout {
			delete(wsTokenIssuanceLimiters, k)
		}
	}

	entry, ok := wsTokenIssuanceLimiters[ip]
	if !ok {
		entry = &rateLimiterEntry{
			limiter:  rate.NewLimiter(rate.Every(6*time.Second), 5),
			lastSeen: now,
		}
		wsTokenIssuanceLimiters[ip] = entry
	} else {
		entry.lastSeen = now
	}
	return entry.limiter.Allow()
}

// getConnectionRateLimiter returns the singleton rate limiter instance
func getConnectionRateLimiter() *ConnectionRateLimiter {
	wsConfigMutex.RLock()
	if connRateLimiter != nil {
		l := connRateLimiter
		wsConfigMutex.RUnlock()
		return l
	}
	wsConfigMutex.RUnlock()
	wsConfigMutex.Lock()
	defer wsConfigMutex.Unlock()
	connRateLimiterOnce.Do(func() {
		connRateLimiter = NewConnectionRateLimiter()
	})
	if connRateLimiter == nil {
		connRateLimiter = NewConnectionRateLimiter()
	}
	return connRateLimiter
}

// getAuthReadTimeout returns the configured auth read timeout in a
// concurrency-safe way (tests mutate this value).
func getAuthReadTimeout() time.Duration {
	wsConfigMutex.RLock()
	defer wsConfigMutex.RUnlock()
	return authReadTimeout
}

// getConnectionRateConfig returns the current rate limit and burst.
func getConnectionRateConfig() (rate.Limit, int) {
	wsConfigMutex.RLock()
	defer wsConfigMutex.RUnlock()
	return connectionRateLimit, connectionRateBurst
}

// isPrivateIP checks if an IP address is in a private RFC 1918 range or Tailscale range
func isPrivateIP(ip string) bool {
	// Parse IP address
	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		return false
	}

	// Check loopback
	if parsedIP.IsLoopback() {
		return true
	}

	// Check private IP ranges (RFC 1918) and Tailscale CGNAT range
	privateRanges := []string{
		"10.0.0.0/8",          // 10.0.0.0 - 10.255.255.255
		"172.16.0.0/12",       // 172.16.0.0 - 172.31.255.255 (FIXED: was accepting all 172.x)
		"192.168.0.0/16",      // 192.168.0.0 - 192.168.255.255
		"169.254.0.0/16",      // Link-local
		"100.64.0.0/10",       // Tailscale/CGNAT range (100.64.0.0 - 100.127.255.255)
		"fc00::/7",            // IPv6 Unique Local Addresses
		"fe80::/10",           // IPv6 Link-local
		"fd7a:115c:a1e0::/48", // Tailscale IPv6 range
	}

	for _, cidr := range privateRanges {
		_, subnet, _ := net.ParseCIDR(cidr)
		if subnet != nil && subnet.Contains(parsedIP) {
			return true
		}
	}

	return false
}

// checkOriginStrict performs strict origin validation
func checkOriginStrict(r *http.Request) bool {
	origin := r.Header.Get("Origin")

	// SECURITY: Reject empty Origin headers
	// Empty origins could be from non-browser clients attempting to bypass CORS
	if origin == "" {
		log.Printf("WebSocket: Rejected connection with empty Origin from %s", r.RemoteAddr)
		return false
	}

	// Parse the origin URL
	u, err := url.Parse(origin)
	if err != nil {
		log.Printf("WebSocket: Rejected connection with invalid Origin '%s' from %s: %v", origin, r.RemoteAddr, err)
		return false
	}

	// Check configurable whitelist first
	allowedOriginsMutex.RLock()
	if len(allowedOrigins) > 0 {
		allowed := false
		for _, allowedOrigin := range allowedOrigins {
			if strings.EqualFold(u.Host, allowedOrigin) {
				allowed = true
				break
			}
		}
		allowedOriginsMutex.RUnlock()

		if !allowed {
			log.Printf("WebSocket: Rejected connection from non-whitelisted origin '%s' (from %s)", origin, r.RemoteAddr)
		}
		return allowed
	}
	allowedOriginsMutex.RUnlock()

	// Allow same host (exact match) — this is always permitted because the
	// browser is talking to the same origin that's serving the API.
	if u.Host == r.Host {
		return true
	}

	// Beyond same-host, LAN/private-IP origins are only honored when the
	// operator has explicitly opted in. Default is closed.
	if !lanOriginsTrusted() {
		log.Printf("WebSocket: Rejected cross-origin '%s' from %s (LAN auto-trust disabled; use allowlist)", origin, r.RemoteAddr)
		return false
	}

	// Extract hostname and port from origin
	hostname := u.Hostname()
	port := u.Port()

	// For local network access, validate against private IP ranges
	hostnameLower := strings.ToLower(hostname)

	// Allow .local mDNS domains (common in local networks)
	if strings.HasSuffix(hostnameLower, ".local") {
		return true
	}

	// Allow localhost variants
	if hostnameLower == "localhost" {
		return true
	}

	// Validate IP addresses against RFC 1918 private ranges
	if isPrivateIP(hostname) {
		// Additional check: if origin has explicit port, ensure it matches request port
		if port != "" {
			requestPort := "80"
			if r.TLS != nil {
				requestPort = "443"
			}
			// Extract port from r.Host if present
			if h, p, err := net.SplitHostPort(r.Host); err == nil {
				requestPort = p
				_ = h // Use h to avoid unused variable
			}

			if port != requestPort {
				log.Printf("WebSocket: Rejected private IP origin with mismatched port: %s (expected %s) from %s",
					origin, requestPort, r.RemoteAddr)
				return false
			}
		}
		return true
	}

	// All other origins are rejected
	log.Printf("WebSocket: Rejected non-private origin '%s' from %s", origin, r.RemoteAddr)
	return false
}

// SetAllowedOrigins configures the whitelist of allowed origin hosts.
// If empty, falls back to same-host validation (and LAN auto-trust if enabled).
// SECURITY: wildcard "*" entries are dropped — WebSocket connections carry
// credentials (Basic Auth + ws-token) so a wildcard origin is unsafe and
// inconsistent with the CORS middleware which also rejects wildcards under
// allowCredentials=true.
func SetAllowedOrigins(origins []string) {
	allowedOriginsMutex.Lock()
	defer allowedOriginsMutex.Unlock()
	filtered := make([]string, 0, len(origins))
	wildcardSeen := false
	for _, origin := range origins {
		if origin == "*" {
			wildcardSeen = true
			continue
		}
		filtered = append(filtered, origin)
	}
	if wildcardSeen {
		log.Printf("WebSocket: ignoring wildcard '*' origin entry; configure explicit hosts instead")
	}
	allowedOrigins = filtered
	log.Printf("WebSocket: Updated allowed origins whitelist: %v", allowedOrigins)
}

// GetAllowedOrigins returns the current whitelist
func GetAllowedOrigins() []string {
	allowedOriginsMutex.RLock()
	defer allowedOriginsMutex.RUnlock()
	result := make([]string, len(allowedOrigins))
	copy(result, allowedOrigins)
	return result
}

// GenerateWSToken creates a new WebSocket auth token
func GenerateWSToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		log.Fatal("Failed to generate WebSocket token:", err)
	}
	return base64.URLEncoding.EncodeToString(b)
}

// CreateWSToken generates and stores a new WebSocket token. The map is bounded
// to maxWSTokens entries; if at capacity, expired tokens are pruned first and,
// failing that, the oldest entry is evicted to make room.
func CreateWSToken() string {
	token := GenerateWSToken()
	now := time.Now()
	wsTokenMutex.Lock()
	defer wsTokenMutex.Unlock()

	// Prune expired tokens before checking capacity.
	if len(wsTokens) >= maxWSTokens {
		for t, exp := range wsTokens {
			if now.After(exp) {
				delete(wsTokens, t)
			}
		}
	}
	// Still at/over capacity: evict the entry with the soonest expiry.
	if len(wsTokens) >= maxWSTokens {
		var oldestToken string
		var oldestExpiry time.Time
		first := true
		for t, exp := range wsTokens {
			if first || exp.Before(oldestExpiry) {
				oldestToken = t
				oldestExpiry = exp
				first = false
			}
		}
		if oldestToken != "" {
			delete(wsTokens, oldestToken)
		}
	}

	wsTokens[token] = now.Add(5 * time.Minute) // Token valid for 5 minutes
	return token
}

// ValidateWSToken checks if a WebSocket token is valid and not expired
func ValidateWSToken(token string) bool {
	if token == "" {
		return false
	}
	wsTokenMutex.RLock()
	expiry, exists := wsTokens[token]
	wsTokenMutex.RUnlock()

	if !exists {
		return false
	}

	if time.Now().After(expiry) {
		// Token expired, remove it
		wsTokenMutex.Lock()
		delete(wsTokens, token)
		wsTokenMutex.Unlock()
		return false
	}

	return true
}

// ValidateAndDeleteWSToken atomically validates and deletes a token to prevent reuse
// This prevents race conditions where two connections could use the same token
func ValidateAndDeleteWSToken(token string) bool {
	if token == "" {
		return false
	}

	wsTokenMutex.Lock()
	defer wsTokenMutex.Unlock()

	expiry, exists := wsTokens[token]
	if !exists {
		return false
	}

	if time.Now().After(expiry) {
		// Token expired
		delete(wsTokens, token)
		return false
	}

	// Token is valid - delete it immediately to prevent reuse
	delete(wsTokens, token)
	return true
}

// DeleteWSToken removes a token after use
func DeleteWSToken(token string) {
	wsTokenMutex.Lock()
	delete(wsTokens, token)
	wsTokenMutex.Unlock()
}

// CleanupExpiredWSTokens removes expired tokens periodically
// It runs until the provided context is cancelled, ensuring proper cleanup
func CleanupExpiredWSTokens(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			// Context cancelled, exit goroutine
			log.Println("WebSocket token cleanup goroutine shutting down")
			return
		case <-ticker.C:
			// Cleanup expired tokens
			now := time.Now()
			wsTokenMutex.Lock()
			for token, expiry := range wsTokens {
				if now.After(expiry) {
					delete(wsTokens, token)
				}
			}
			wsTokenMutex.Unlock()
		}
	}
}

// HandleWebSocket provides real-time status updates
func HandleWebSocket(stateMgr *state.Manager, eventMgr *events.Manager, otaManagers ...*ota.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Extract client IP for rate limiting
		clientIP := r.RemoteAddr
		if host, _, err := net.SplitHostPort(clientIP); err == nil {
			clientIP = host
		}

		// Apply rate limiting per IP
		limiter := getConnectionRateLimiter().GetLimiter(clientIP)
		if !limiter.Allow() {
			log.Printf("WebSocket: Rate limit exceeded for IP %s", clientIP)
			http.Error(w, "Too many connection attempts. Please try again later.", http.StatusTooManyRequests)
			return
		}

		// Upgrade to WebSocket without auth - we'll validate token in first message
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("WebSocket upgrade error from %s: %v", clientIP, err)
			return
		}
		defer conn.Close()

		// Bound the maximum incoming WebSocket message size. Without this,
		// gorilla/websocket imposes no limit and an unauthenticated client
		// can stream arbitrarily large JSON into ReadJSON below, exhausting
		// memory. 64 KiB is comfortably above the ~80-byte auth handshake
		// and any plausible client ping payload.
		conn.SetReadLimit(maxIncomingMessageBytes)

		// Set a deadline for receiving the auth token.
		conn.SetReadDeadline(time.Now().Add(getAuthReadTimeout()))

		// First message MUST be the auth token
		var authMsg struct {
			Type  string `json:"type"`
			Token string `json:"token"`
		}
		if err := conn.ReadJSON(&authMsg); err != nil {
			log.Printf("WebSocket auth failed: failed to read auth message from %s: %v", r.RemoteAddr, err)
			_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			conn.WriteJSON(map[string]any{
				"type":  "error",
				"error": "Authentication required",
			})
			return
		}

		// Validate token type and atomically delete to prevent concurrent reuse
		if authMsg.Type != "auth" || !ValidateAndDeleteWSToken(authMsg.Token) {
			log.Printf("WebSocket auth failed: invalid token from %s", r.RemoteAddr)
			_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			conn.WriteJSON(map[string]any{
				"type":  "error",
				"error": "Invalid or expired token",
			})
			return
		}

		// Clear read deadline after successful auth
		conn.SetReadDeadline(time.Time{})

		log.Printf("WebSocket authenticated successfully from %s", r.RemoteAddr)

		// Send auth success message
		_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		if err := conn.WriteJSON(map[string]any{
			"type": "auth_success",
		}); err != nil {
			return
		}

		// Subscribe to state updates and events
		updates := stateMgr.Subscribe()
		events := eventMgr.Subscribe()
		var otaUpdates chan ota.Status
		var otaMgr *ota.Manager
		if len(otaManagers) > 0 {
			otaMgr = otaManagers[0]
		}
		if otaMgr != nil {
			otaUpdates = otaMgr.Subscribe()
		}
		// IMPORTANT: Unsubscribe when WebSocket closes to prevent memory leak
		defer stateMgr.Unsubscribe(updates)
		defer eventMgr.Unsubscribe(events)
		if otaMgr != nil {
			defer otaMgr.Unsubscribe(otaUpdates)
		}

		// Send initial state. The stateMgr cache is kept up to date by the
		// daemon subscribe stream (see cmd/webui/main.go), so we no longer
		// need to reload from disk here.
		status := stateMgr.GetState()
		initialMessage := map[string]any{
			"type": "state",
			"data": status,
		}
		_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		if err := conn.WriteJSON(initialMessage); err != nil {
			return
		}

		// Set up ping/pong handlers for connection health monitoring
		conn.SetPongHandler(func(string) error {
			// Extend read deadline when we receive a pong
			conn.SetReadDeadline(time.Now().Add(60 * time.Second))
			return nil
		})

		// Start a goroutine to read client messages (for ping/pong).
		// done signals the reader to exit even when it is blocked on a
		// channel send to clientMessages — closing conn alone is not enough
		// because a goroutine parked on `clientMessages <- msg` is not woken
		// by the underlying socket closing.
		clientMessages := make(chan map[string]any, 10)
		done := make(chan struct{})
		var readerWG sync.WaitGroup
		readerWG.Add(1)
		go func() {
			defer readerWG.Done()
			defer close(clientMessages)
			for {
				var msg map[string]any
				if err := conn.ReadJSON(&msg); err != nil {
					// Connection closed or error reading
					return
				}
				select {
				case clientMessages <- msg:
				case <-done:
					return
				}
			}
		}()
		// Ensure the reader goroutine has a chance to observe both the
		// closed connection and the done signal, and that we don't leak
		// it past HandleWebSocket's return. Defers run LIFO, so close(done)
		// runs first (unblocking a reader parked on the channel send), then
		// we close the connection to unblock a reader parked in ReadJSON,
		// and finally we wait for the goroutine to actually exit.
		defer readerWG.Wait()
		defer conn.Close()
		defer close(done)

		// State and event updates are pushed in real time from the local
		// caches (populated by the daemon subscribe stream in the webui
		// process). No more 2-second disk-polling ticker — that path read
		// state.json every 2s on every connected browser and racked up
		// pointless I/O against the SD card.

		// Ping ticker for connection health
		pingTicker := time.NewTicker(30 * time.Second)
		defer pingTicker.Stop()

		// Set initial read deadline
		conn.SetReadDeadline(time.Now().Add(60 * time.Second))

		// writeDeadline is applied to every WriteJSON below. A 10s ceiling
		// matches the existing ping deadline and prevents a slow / dead
		// browser from wedging this goroutine (and pinning the per-IP rate
		// limiter slot) forever.
		const writeDeadline = 10 * time.Second

		for {
			select {
			case state, ok := <-updates:
				if !ok {
					// Channel was closed by Unsubscribe
					return
				}
				// Send state update
				message := map[string]any{
					"type": "state",
					"data": state,
				}
				_ = conn.SetWriteDeadline(time.Now().Add(writeDeadline))
				if err := conn.WriteJSON(message); err != nil {
					return
				}
			case event, ok := <-events:
				if !ok {
					// Channel was closed by Unsubscribe
					return
				}
				// Send event
				message := map[string]any{
					"type": "event",
					"data": event,
				}
				_ = conn.SetWriteDeadline(time.Now().Add(writeDeadline))
				if err := conn.WriteJSON(message); err != nil {
					return
				}
			case otaStatus, ok := <-otaUpdates:
				if otaUpdates == nil {
					continue
				}
				if !ok {
					return
				}
				message := map[string]any{
					"type": "ota_status",
					"data": otaStatus,
				}
				_ = conn.SetWriteDeadline(time.Now().Add(writeDeadline))
				if err := conn.WriteJSON(message); err != nil {
					return
				}
			case msg, ok := <-clientMessages:
				if !ok {
					// Client disconnected
					return
				}
				// Handle client messages (ping/pong)
				if msgType, exists := msg["type"].(string); exists && msgType == "ping" {
					// Respond to ping with pong
					pongMsg := map[string]any{
						"type": "pong",
					}
					_ = conn.SetWriteDeadline(time.Now().Add(writeDeadline))
					if err := conn.WriteJSON(pongMsg); err != nil {
						return
					}
				}
			case <-pingTicker.C:
				// Send WebSocket-level ping frame
				if err := conn.WriteControl(websocket.PingMessage, []byte{}, time.Now().Add(10*time.Second)); err != nil {
					log.Printf("Failed to send ping: %v", err)
					return
				}
			}
		}
	}
}
