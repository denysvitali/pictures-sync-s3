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
	"github.com/denysvitali/pictures-sync-s3/pkg/state"
	"github.com/gorilla/websocket"
	"golang.org/x/time/rate"
)

// ConnectionRateLimiter tracks rate limits per IP address
type ConnectionRateLimiter struct {
	limiters map[string]*rate.Limiter
	mu       sync.RWMutex
}

// NewConnectionRateLimiter creates a new rate limiter for WebSocket connections
func NewConnectionRateLimiter() *ConnectionRateLimiter {
	return &ConnectionRateLimiter{
		limiters: make(map[string]*rate.Limiter),
	}
}

// GetLimiter returns or creates a rate limiter for the given IP
// Allows 5 connections per minute with burst of 2
func (c *ConnectionRateLimiter) GetLimiter(ip string) *rate.Limiter {
	c.mu.RLock()
	limiter, exists := c.limiters[ip]
	c.mu.RUnlock()

	if !exists {
		c.mu.Lock()
		// Double-check after acquiring write lock
		limiter, exists = c.limiters[ip]
		if !exists {
			limiter = rate.NewLimiter(rate.Every(12*time.Second), 2) // 5 per minute, burst 2
			c.limiters[ip] = limiter
		}
		c.mu.Unlock()
	}

	return limiter
}

// CleanupOldLimiters removes rate limiters for IPs that haven't connected recently
func (c *ConnectionRateLimiter) CleanupOldLimiters(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.mu.Lock()
			// Remove all limiters (they'll be recreated on next connection)
			// This prevents memory leaks from storing limiters for IPs that no longer connect
			clear(c.limiters)
			c.mu.Unlock()
		}
	}
}

// StartRateLimiterCleanup starts the rate limiter cleanup goroutine
func StartRateLimiterCleanup(ctx context.Context) {
	getConnectionRateLimiter().CleanupOldLimiters(ctx)
}

var (
	wsTokens            = make(map[string]time.Time) // WebSocket auth tokens with expiry
	wsTokenMutex        sync.RWMutex
	connRateLimiter     *ConnectionRateLimiter
	connRateLimiterOnce sync.Once
	allowedOrigins      = []string{} // Configurable whitelist (empty = use private IP validation)
	allowedOriginsMutex sync.RWMutex
	upgrader            = websocket.Upgrader{
		CheckOrigin: checkOriginStrict,
	}
)

// getConnectionRateLimiter returns the singleton rate limiter instance
func getConnectionRateLimiter() *ConnectionRateLimiter {
	connRateLimiterOnce.Do(func() {
		connRateLimiter = NewConnectionRateLimiter()
	})
	return connRateLimiter
}

// isPrivateIP checks if an IP address is in a private RFC 1918 range
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

	// Check private IP ranges (RFC 1918)
	privateRanges := []string{
		"10.0.0.0/8",       // 10.0.0.0 - 10.255.255.255
		"172.16.0.0/12",    // 172.16.0.0 - 172.31.255.255 (FIXED: was accepting all 172.x)
		"192.168.0.0/16",   // 192.168.0.0 - 192.168.255.255
		"169.254.0.0/16",   // Link-local
		"fc00::/7",         // IPv6 Unique Local Addresses
		"fe80::/10",        // IPv6 Link-local
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

	// Allow same host (exact match)
	if u.Host == r.Host {
		return true
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

// SetAllowedOrigins configures the whitelist of allowed origin hosts
// If empty, falls back to private IP validation
func SetAllowedOrigins(origins []string) {
	allowedOriginsMutex.Lock()
	defer allowedOriginsMutex.Unlock()
	allowedOrigins = make([]string, len(origins))
	copy(allowedOrigins, origins)
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

// CreateWSToken generates and stores a new WebSocket token
func CreateWSToken() string {
	token := GenerateWSToken()
	wsTokenMutex.Lock()
	defer wsTokenMutex.Unlock()
	wsTokens[token] = time.Now().Add(5 * time.Minute) // Token valid for 5 minutes
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
func HandleWebSocket(stateMgr *state.Manager, eventMgr *events.Manager) http.HandlerFunc {
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

		// Set a deadline for receiving the auth token (5 seconds)
		conn.SetReadDeadline(time.Now().Add(5 * time.Second))

		// First message MUST be the auth token
		var authMsg struct {
			Type  string `json:"type"`
			Token string `json:"token"`
		}
		if err := conn.ReadJSON(&authMsg); err != nil {
			log.Printf("WebSocket auth failed: failed to read auth message from %s: %v", r.RemoteAddr, err)
			conn.WriteJSON(map[string]any{
				"type":  "error",
				"error": "Authentication required",
			})
			return
		}

		// Validate token type and atomically delete to prevent concurrent reuse
		if authMsg.Type != "auth" || !ValidateAndDeleteWSToken(authMsg.Token) {
			log.Printf("WebSocket auth failed: invalid token from %s", r.RemoteAddr)
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
		if err := conn.WriteJSON(map[string]any{
			"type": "auth_success",
		}); err != nil {
			return
		}

		// Subscribe to state updates and events
		updates := stateMgr.Subscribe()
		events := eventMgr.Subscribe()
		// IMPORTANT: Unsubscribe when WebSocket closes to prevent memory leak
		defer stateMgr.Unsubscribe(updates)
		defer eventMgr.Unsubscribe(events)

		// Send initial state (reload from disk first to get latest from pictures-sync service)
		stateMgr.Reload()
		status := stateMgr.GetState()
		initialMessage := map[string]any{
			"type": "state",
			"data": status,
		}
		if err := conn.WriteJSON(initialMessage); err != nil {
			return
		}

		// Set up ping/pong handlers for connection health monitoring
		conn.SetPongHandler(func(string) error {
			// Extend read deadline when we receive a pong
			conn.SetReadDeadline(time.Now().Add(60 * time.Second))
			return nil
		})

		// Start a goroutine to read client messages (for ping/pong)
		clientMessages := make(chan map[string]any, 10)
		go func() {
			defer close(clientMessages)
			for {
				var msg map[string]any
				if err := conn.ReadJSON(&msg); err != nil {
					// Connection closed or error reading
					return
				}
				clientMessages <- msg
			}
		}()

		// Send updates and periodically reload state from disk
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()

		// Ping ticker for connection health
		pingTicker := time.NewTicker(30 * time.Second)
		defer pingTicker.Stop()

		// Set initial read deadline
		conn.SetReadDeadline(time.Now().Add(60 * time.Second))

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
					if err := conn.WriteJSON(pongMsg); err != nil {
						return
					}
				}
			case <-ticker.C:
				// Reload state from disk (in case pictures-sync service updated it)
				if err := stateMgr.Reload(); err != nil {
					log.Printf("Failed to reload state: %v", err)
				}

				// Send updated state
				status := stateMgr.GetState()
				message := map[string]any{
					"type": "state",
					"data": status,
				}
				if err := conn.WriteJSON(message); err != nil {
					return
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
