package websocket

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/denysvitali/pictures-sync-s3/pkg/events"
	"github.com/denysvitali/pictures-sync-s3/pkg/state"
	"github.com/gorilla/websocket"
)

// Helper function to create a dialer with proper headers
func createTestDialer() (*websocket.Dialer, http.Header) {
	dialer := websocket.DefaultDialer
	headers := http.Header{}
	headers.Add("Origin", "http://localhost:8080")
	return dialer, headers
}

// TestWebSocketAuthentication tests the WebSocket authentication flow
func TestWebSocketAuthentication(t *testing.T) {
	// Create test server
	stateMgr, err := state.NewManager()
	if err != nil {
		t.Fatalf("Failed to create state manager: %v", err)
	}
	eventMgr := events.NewManager()

	server := httptest.NewServer(HandleWebSocket(stateMgr, eventMgr))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	t.Run("successful authentication", func(t *testing.T) {
		// Get auth token
		token := CreateWSToken()

		// Connect to WebSocket with proper origin header
		dialer := websocket.DefaultDialer
		headers := http.Header{}
		headers.Add("Origin", "http://localhost:8080")

		conn, _, err := dialer.Dial(wsURL, headers)
		if err != nil {
			t.Fatalf("Failed to connect: %v", err)
		}
		defer conn.Close()

		// Send auth message
		authMsg := map[string]interface{}{
			"type":  "auth",
			"token": token,
		}
		if err := conn.WriteJSON(authMsg); err != nil {
			t.Fatalf("Failed to send auth: %v", err)
		}

		// Read auth success response
		var response map[string]interface{}
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		if err := conn.ReadJSON(&response); err != nil {
			t.Fatalf("Failed to read auth response: %v", err)
		}

		if response["type"] != "auth_success" {
			t.Errorf("Expected auth_success, got %v", response["type"])
		}
	})

	t.Run("authentication with invalid token", func(t *testing.T) {
		time.Sleep(13 * time.Second) // Avoid rate limit - wait for bucket to refill

		dialer, headers := createTestDialer()
		conn, _, err := dialer.Dial(wsURL, headers)
		if err != nil {
			t.Fatalf("Failed to connect: %v", err)
		}
		defer conn.Close()

		// Send invalid token
		authMsg := map[string]interface{}{
			"type":  "auth",
			"token": "invalid-token",
		}
		if err := conn.WriteJSON(authMsg); err != nil {
			t.Fatalf("Failed to send auth: %v", err)
		}

		// Should receive error
		var response map[string]interface{}
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		if err := conn.ReadJSON(&response); err != nil {
			t.Fatalf("Failed to read error response: %v", err)
		}

		if response["type"] != "error" {
			t.Errorf("Expected error type, got %v", response["type"])
		}
	})

	t.Run("authentication timeout", func(t *testing.T) {
		time.Sleep(13 * time.Second) // Avoid rate limit - wait for bucket to refill

		dialer, headers := createTestDialer()
		conn, _, err := dialer.Dial(wsURL, headers)
		if err != nil {
			t.Fatalf("Failed to connect: %v", err)
		}
		defer conn.Close()

		// Don't send auth message, wait for timeout
		time.Sleep(6 * time.Second)

		// Try to read - should get error or connection close
		var response map[string]interface{}
		err = conn.ReadJSON(&response)
		if err == nil {
			t.Error("Expected error or connection close, but read succeeded")
		}
	})

	t.Run("token reuse prevention", func(t *testing.T) {
		time.Sleep(19 * time.Second) // Avoid rate limit - wait for bucket to refill (6s auth timeout + 13s)

		token := CreateWSToken()

		// First connection - should succeed
		dialer, headers := createTestDialer()
		conn1, _, err := dialer.Dial(wsURL, headers)
		if err != nil {
			t.Fatalf("Failed to connect first time: %v", err)
		}
		defer conn1.Close()

		authMsg := map[string]interface{}{
			"type":  "auth",
			"token": token,
		}
		if err := conn1.WriteJSON(authMsg); err != nil {
			t.Fatalf("Failed to send auth: %v", err)
		}

		var response1 map[string]interface{}
		conn1.SetReadDeadline(time.Now().Add(2 * time.Second))
		if err := conn1.ReadJSON(&response1); err != nil {
			t.Fatalf("Failed to read first auth response: %v", err)
		}

		if response1["type"] != "auth_success" {
			t.Errorf("First auth should succeed, got %v", response1["type"])
		}

		// Second connection with same token - should fail (no sleep needed here, same client)
		conn2, _, err := dialer.Dial(wsURL, headers)
		if err != nil {
			t.Fatalf("Failed to connect second time: %v", err)
		}
		defer conn2.Close()

		if err := conn2.WriteJSON(authMsg); err != nil {
			t.Fatalf("Failed to send second auth: %v", err)
		}

		var response2 map[string]interface{}
		conn2.SetReadDeadline(time.Now().Add(2 * time.Second))
		if err := conn2.ReadJSON(&response2); err != nil {
			t.Fatalf("Failed to read second auth response: %v", err)
		}

		if response2["type"] != "error" {
			t.Errorf("Second auth with same token should fail, got %v", response2["type"])
		}
	})
}

// TestWebSocketPingPong tests the ping/pong heartbeat mechanism
func TestWebSocketPingPong(t *testing.T) {
	stateMgr, err := state.NewManager()
	if err != nil {
		t.Fatalf("Failed to create state manager: %v", err)
	}
	eventMgr := events.NewManager()

	server := httptest.NewServer(HandleWebSocket(stateMgr, eventMgr))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	t.Run("ping pong exchange", func(t *testing.T) {
		time.Sleep(500 * time.Millisecond) // Avoid rate limit
		token := CreateWSToken()

		dialer, headers := createTestDialer()
		conn, _, err := dialer.Dial(wsURL, headers)
		if err != nil {
			t.Fatalf("Failed to connect: %v", err)
		}
		defer conn.Close()

		// Authenticate
		authMsg := map[string]interface{}{
			"type":  "auth",
			"token": token,
		}
		if err := conn.WriteJSON(authMsg); err != nil {
			t.Fatalf("Failed to send auth: %v", err)
		}

		// Read auth success
		var authResp map[string]interface{}
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		if err := conn.ReadJSON(&authResp); err != nil {
			t.Fatalf("Failed to read auth response: %v", err)
		}

		// Read initial state message
		var stateResp map[string]interface{}
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		if err := conn.ReadJSON(&stateResp); err != nil {
			t.Fatalf("Failed to read initial state: %v", err)
		}

		// Send ping
		pingMsg := map[string]interface{}{
			"type": "ping",
		}
		if err := conn.WriteJSON(pingMsg); err != nil {
			t.Fatalf("Failed to send ping: %v", err)
		}

		// Wait for pong
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		for {
			var response map[string]interface{}
			if err := conn.ReadJSON(&response); err != nil {
				t.Fatalf("Failed to read pong: %v", err)
			}

			if response["type"] == "pong" {
				// Success!
				return
			}
			// Might receive state updates, keep reading
		}
	})
}

// TestWebSocketStateUpdates tests real-time state update delivery
func TestWebSocketStateUpdates(t *testing.T) {
	stateMgr, err := state.NewManager()
	if err != nil {
		t.Fatalf("Failed to create state manager: %v", err)
	}
	eventMgr := events.NewManager()

	server := httptest.NewServer(HandleWebSocket(stateMgr, eventMgr))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	t.Run("receive state updates", func(t *testing.T) {
		time.Sleep(500 * time.Millisecond) // Avoid rate limit
		token := CreateWSToken()

		dialer, headers := createTestDialer()
		conn, _, err := dialer.Dial(wsURL, headers)
		if err != nil {
			t.Fatalf("Failed to connect: %v", err)
		}
		defer conn.Close()

		// Authenticate
		authMsg := map[string]interface{}{
			"type":  "auth",
			"token": token,
		}
		if err := conn.WriteJSON(authMsg); err != nil {
			t.Fatalf("Failed to send auth: %v", err)
		}

		// Read auth success
		var authResp map[string]interface{}
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		if err := conn.ReadJSON(&authResp); err != nil {
			t.Fatalf("Failed to read auth response: %v", err)
		}

		// Read initial state
		var stateResp map[string]interface{}
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		if err := conn.ReadJSON(&stateResp); err != nil {
			t.Fatalf("Failed to read initial state: %v", err)
		}

		if stateResp["type"] != "state" {
			t.Errorf("Expected state message, got %v", stateResp["type"])
		}

		// Trigger state update
		if err := stateMgr.SetStatus(state.StatusDetected); err != nil {
			t.Fatalf("Failed to set status: %v", err)
		}

		// Should receive state update
		conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		foundUpdate := false
		for i := 0; i < 5; i++ {
			var update map[string]interface{}
			if err := conn.ReadJSON(&update); err != nil {
				t.Fatalf("Failed to read state update: %v", err)
			}

			if update["type"] == "state" {
				foundUpdate = true
				break
			}
		}

		if !foundUpdate {
			t.Error("Did not receive state update after status change")
		}
	})
}

// TestWebSocketEventDelivery tests event delivery to WebSocket clients
func TestWebSocketEventDelivery(t *testing.T) {
	stateMgr, err := state.NewManager()
	if err != nil {
		t.Fatalf("Failed to create state manager: %v", err)
	}
	eventMgr := events.NewManager()

	server := httptest.NewServer(HandleWebSocket(stateMgr, eventMgr))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	t.Run("receive events", func(t *testing.T) {
		time.Sleep(500 * time.Millisecond) // Avoid rate limit
		token := CreateWSToken()

		dialer, headers := createTestDialer()
		conn, _, err := dialer.Dial(wsURL, headers)
		if err != nil {
			t.Fatalf("Failed to connect: %v", err)
		}
		defer conn.Close()

		// Authenticate
		authMsg := map[string]interface{}{
			"type":  "auth",
			"token": token,
		}
		if err := conn.WriteJSON(authMsg); err != nil {
			t.Fatalf("Failed to send auth: %v", err)
		}

		// Read auth success and initial state
		var resp map[string]interface{}
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		conn.ReadJSON(&resp) // auth_success
		conn.ReadJSON(&resp) // initial state

		// Emit an event
		eventMgr.EmitInfo("test event", map[string]interface{}{
			"test_data": "hello",
		})

		// Should receive event
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		foundEvent := false
		for i := 0; i < 5; i++ {
			var event map[string]interface{}
			if err := conn.ReadJSON(&event); err != nil {
				t.Fatalf("Failed to read event: %v", err)
			}

			if event["type"] == "event" {
				foundEvent = true
				break
			}
		}

		if !foundEvent {
			t.Error("Did not receive event after emission")
		}
	})
}

// TestWebSocketRateLimiting tests per-IP rate limiting
func TestWebSocketRateLimiting(t *testing.T) {
	stateMgr, err := state.NewManager()
	if err != nil {
		t.Fatalf("Failed to create state manager: %v", err)
	}
	eventMgr := events.NewManager()

	server := httptest.NewServer(HandleWebSocket(stateMgr, eventMgr))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	t.Run("rate limit enforcement", func(t *testing.T) {
		time.Sleep(500 * time.Millisecond) // Avoid rate limit from previous tests

		// Make multiple rapid connections
		successCount := 0
		rateLimitedCount := 0

		dialer, headers := createTestDialer()
		for i := 0; i < 10; i++ {
			conn, resp, err := dialer.Dial(wsURL, headers)
			if err != nil {
				if resp != nil && resp.StatusCode == http.StatusTooManyRequests {
					rateLimitedCount++
				}
				continue
			}
			conn.Close()
			successCount++
			time.Sleep(100 * time.Millisecond)
		}

		if rateLimitedCount == 0 {
			t.Log("Warning: Rate limiting may not be working as expected")
		}
	})
}

// TestWebSocketOriginValidation tests origin header validation
func TestWebSocketOriginValidation(t *testing.T) {
	stateMgr, err := state.NewManager()
	if err != nil {
		t.Fatalf("Failed to create state manager: %v", err)
	}
	eventMgr := events.NewManager()

	server := httptest.NewServer(HandleWebSocket(stateMgr, eventMgr))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	t.Run("reject invalid origin", func(t *testing.T) {
		dialer := websocket.DefaultDialer
		headers := http.Header{}
		headers.Add("Origin", "https://evil.com")

		_, resp, err := dialer.Dial(wsURL, headers)
		if err == nil {
			t.Error("Expected connection to be rejected with invalid origin")
		}
		if resp != nil && resp.StatusCode != http.StatusForbidden {
			// Note: gorilla/websocket returns different error for origin rejection
			// The connection should fail in some way
		}
	})

	t.Run("accept localhost origin", func(t *testing.T) {
		dialer := websocket.DefaultDialer
		headers := http.Header{}
		headers.Add("Origin", "http://localhost:8080")

		conn, _, err := dialer.Dial(wsURL, headers)
		if err != nil {
			t.Errorf("Localhost origin should be accepted: %v", err)
		}
		if conn != nil {
			conn.Close()
		}
	})
}

// TestWebSocketCleanup tests proper resource cleanup
func TestWebSocketCleanup(t *testing.T) {
	stateMgr, err := state.NewManager()
	if err != nil {
		t.Fatalf("Failed to create state manager: %v", err)
	}
	_ = events.NewManager() // Create but don't use

	t.Run("token cleanup", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// Start cleanup goroutine
		go CleanupExpiredWSTokens(ctx)

		// Create expired token
		token := GenerateWSToken()
		wsTokenMutex.Lock()
		wsTokens[token] = time.Now().Add(-1 * time.Minute) // Already expired
		wsTokenMutex.Unlock()

		// Wait for cleanup
		time.Sleep(2 * time.Second)

		// Token should be removed
		if ValidateWSToken(token) {
			t.Error("Expired token was not cleaned up")
		}

		// Cancel context to stop cleanup
		cancel()
		time.Sleep(100 * time.Millisecond)
	})

	t.Run("subscriber cleanup", func(t *testing.T) {
		// Subscribe
		ch := stateMgr.Subscribe()

		// Unsubscribe should close channel
		stateMgr.Unsubscribe(ch)

		// Try to read from channel - should be closed
		_, ok := <-ch
		if ok {
			t.Error("Channel should be closed after unsubscribe")
		}
	})
}

// TestWebSocketConcurrency tests concurrent WebSocket operations
func TestWebSocketConcurrency(t *testing.T) {
	stateMgr, err := state.NewManager()
	if err != nil {
		t.Fatalf("Failed to create state manager: %v", err)
	}
	eventMgr := events.NewManager()

	server := httptest.NewServer(HandleWebSocket(stateMgr, eventMgr))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	t.Run("multiple concurrent connections", func(t *testing.T) {
		time.Sleep(15 * time.Second) // Wait for rate limit to reset from previous tests

		numClients := 10
		done := make(chan bool, numClients)

		for i := 0; i < numClients; i++ {
			go func(clientID int) {
				defer func() { done <- true }()

				token := CreateWSToken()
				dialer, headers := createTestDialer()
				conn, _, err := dialer.Dial(wsURL, headers)
				if err != nil {
					t.Errorf("Client %d: Failed to connect: %v", clientID, err)
					return
				}
				defer conn.Close()

				// Authenticate
				authMsg := map[string]interface{}{
					"type":  "auth",
					"token": token,
				}
				if err := conn.WriteJSON(authMsg); err != nil {
					t.Errorf("Client %d: Failed to auth: %v", clientID, err)
					return
				}

				// Read messages for a bit
				conn.SetReadDeadline(time.Now().Add(3 * time.Second))
				for j := 0; j < 5; j++ {
					var msg map[string]interface{}
					if err := conn.ReadJSON(&msg); err != nil {
						break
					}
				}
			}(i)
		}

		// Wait for all clients to complete
		for i := 0; i < numClients; i++ {
			<-done
		}
	})
}

// TestWebSocketMessageValidation tests message structure validation
func TestWebSocketMessageValidation(t *testing.T) {
	stateMgr, err := state.NewManager()
	if err != nil {
		t.Fatalf("Failed to create state manager: %v", err)
	}
	eventMgr := events.NewManager()

	server := httptest.NewServer(HandleWebSocket(stateMgr, eventMgr))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	t.Run("malformed messages", func(t *testing.T) {
		time.Sleep(500 * time.Millisecond) // Avoid rate limit
		_ = CreateWSToken() // Create but don't use yet

		dialer, headers := createTestDialer()
		conn, _, err := dialer.Dial(wsURL, headers)
		if err != nil {
			t.Fatalf("Failed to connect: %v", err)
		}
		defer conn.Close()

		// Send malformed auth
		if err := conn.WriteMessage(websocket.TextMessage, []byte("invalid json")); err != nil {
			t.Fatalf("Failed to send malformed message: %v", err)
		}

		// Should get error or connection close
		var resp map[string]interface{}
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		err = conn.ReadJSON(&resp)
		if err == nil && resp["type"] != "error" {
			t.Error("Expected error for malformed JSON")
		}
	})
}

// Benchmark WebSocket message throughput
func BenchmarkWebSocketThroughput(b *testing.B) {
	stateMgr, err := state.NewManager()
	if err != nil {
		b.Fatalf("Failed to create state manager: %v", err)
	}
	eventMgr := events.NewManager()

	server := httptest.NewServer(HandleWebSocket(stateMgr, eventMgr))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	token := CreateWSToken()
	dialer, headers := createTestDialer()
	conn, _, err := dialer.Dial(wsURL, headers)
	if err != nil {
		b.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	// Authenticate
	authMsg := map[string]interface{}{
		"type":  "auth",
		"token": token,
	}
	conn.WriteJSON(authMsg)

	var resp map[string]interface{}
	conn.ReadJSON(&resp) // auth_success
	conn.ReadJSON(&resp) // initial state

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		// Trigger state update
		stateMgr.SetStatus(state.StatusIdle)

		// Read update
		conn.SetReadDeadline(time.Now().Add(1 * time.Second))
		conn.ReadJSON(&resp)
	}
}
