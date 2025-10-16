package websocket

import (
	"crypto/rand"
	"encoding/base64"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/denysvitali/pictures-sync-s3/pkg/events"
	"github.com/denysvitali/pictures-sync-s3/pkg/state"
	"github.com/gorilla/websocket"
)

var (
	wsTokens     = make(map[string]time.Time) // WebSocket auth tokens with expiry
	wsTokenMutex sync.RWMutex
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

// DeleteWSToken removes a token after use
func DeleteWSToken(token string) {
	wsTokenMutex.Lock()
	delete(wsTokens, token)
	wsTokenMutex.Unlock()
}

// CleanupExpiredWSTokens removes expired tokens periodically
func CleanupExpiredWSTokens() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
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

// HandleWebSocket provides real-time status updates
func HandleWebSocket(stateMgr *state.Manager, eventMgr *events.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Upgrade to WebSocket without auth - we'll validate token in first message
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("WebSocket upgrade error: %v", err)
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
			conn.WriteJSON(map[string]interface{}{
				"type":  "error",
				"error": "Authentication required",
			})
			return
		}

		// Validate token type and value
		if authMsg.Type != "auth" || !ValidateWSToken(authMsg.Token) {
			log.Printf("WebSocket auth failed: invalid token from %s", r.RemoteAddr)
			conn.WriteJSON(map[string]interface{}{
				"type":  "error",
				"error": "Invalid or expired token",
			})
			return
		}

		// Remove token after successful validation (one-time use)
		DeleteWSToken(authMsg.Token)

		// Clear read deadline after successful auth
		conn.SetReadDeadline(time.Time{})

		log.Printf("WebSocket authenticated successfully from %s", r.RemoteAddr)

		// Send auth success message
		if err := conn.WriteJSON(map[string]interface{}{
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
		initialMessage := map[string]interface{}{
			"type": "state",
			"data": status,
		}
		if err := conn.WriteJSON(initialMessage); err != nil {
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
				// Send state update
				message := map[string]interface{}{
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
				message := map[string]interface{}{
					"type": "event",
					"data": event,
				}
				if err := conn.WriteJSON(message); err != nil {
					return
				}
			case <-ticker.C:
				// Reload state from disk (in case pictures-sync service updated it)
				if err := stateMgr.Reload(); err != nil {
					log.Printf("Failed to reload state: %v", err)
				}

				// Send updated state
				status := stateMgr.GetState()
				message := map[string]interface{}{
					"type": "state",
					"data": status,
				}
				if err := conn.WriteJSON(message); err != nil {
					return
				}
			}
		}
	}
}
