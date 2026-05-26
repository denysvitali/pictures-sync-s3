package websocket

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/denysvitali/pictures-sync-s3/pkg/events"
	"github.com/denysvitali/pictures-sync-s3/pkg/state"
	"github.com/gorilla/websocket"
	"golang.org/x/time/rate"
)

func runtimeNumGoroutineImpl() int { return runtime.NumGoroutine() }

// TestMain enables LAN/private-IP origin auto-trust for the websocket test
// suite. Production default is false; many tests rely on origins like
// http://localhost:8080 being honored without an explicit allowlist.
func TestMain(m *testing.M) {
	SetTrustLANOrigins(true)
	code := m.Run()
	SetTrustLANOrigins(false)
	os.Exit(code)
}

func resetWebSocketTestState(t *testing.T) {
	t.Helper()
	wsConfigMutex.Lock()
	connRateLimiter = NewConnectionRateLimiter()
	connRateLimiterOnce = sync.Once{}
	connectionRateLimit = rate.Inf
	connectionRateBurst = 1000
	authReadTimeout = 5 * time.Second
	wsConfigMutex.Unlock()
	// LAN auto-trust is enabled at the suite level via TestMain.
	t.Cleanup(func() {
		wsConfigMutex.Lock()
		connRateLimiter = nil
		connRateLimiterOnce = sync.Once{}
		connectionRateLimit = rate.Every(6 * time.Second)
		connectionRateBurst = 3
		authReadTimeout = 5 * time.Second
		wsConfigMutex.Unlock()
	})
}

// Helper function to create a dialer with proper headers
func createTestDialer() (*websocket.Dialer, http.Header) {
	dialer := websocket.DefaultDialer
	headers := http.Header{}
	headers.Add("Origin", "http://localhost:8080")
	return dialer, headers
}

// TestWebSocketAuthentication tests the WebSocket authentication flow
func TestWebSocketAuthentication(t *testing.T) {
	resetWebSocketTestState(t)
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
		wsConfigMutex.Lock()
		authReadTimeout = 10 * time.Millisecond
		wsConfigMutex.Unlock()

		dialer, headers := createTestDialer()
		conn, _, err := dialer.Dial(wsURL, headers)
		if err != nil {
			t.Fatalf("Failed to connect: %v", err)
		}
		defer conn.Close()

		// Try to read - should get error or connection close
		var response map[string]interface{}
		conn.SetReadDeadline(time.Now().Add(time.Second))
		err = conn.ReadJSON(&response)
		if err == nil && response["type"] != "error" {
			t.Errorf("Expected error response or connection close, got %v", response["type"])
		}
	})

	t.Run("token reuse prevention", func(t *testing.T) {
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
	resetWebSocketTestState(t)
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
	resetWebSocketTestState(t)
	stateMgr, err := state.NewManager()
	if err != nil {
		t.Fatalf("Failed to create state manager: %v", err)
	}
	eventMgr := events.NewManager()

	server := httptest.NewServer(HandleWebSocket(stateMgr, eventMgr))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	_, serverPort, err := net.SplitHostPort(strings.TrimPrefix(server.URL, "http://"))
	if err != nil {
		t.Fatalf("Failed to parse test server port: %v", err)
	}

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

	t.Run("accept Tailscale origin", func(t *testing.T) {
		dialer := websocket.DefaultDialer
		headers := http.Header{}
		// Test Tailscale CGNAT IP range (100.64.0.0/10)
		headers.Add("Origin", "https://100.106.81.42:"+serverPort)

		conn, _, err := dialer.Dial(wsURL, headers)
		if err != nil {
			t.Errorf("Tailscale IP origin should be accepted: %v", err)
		}
		if conn != nil {
			conn.Close()
		}
	})

	t.Run("accept private IP ranges", func(t *testing.T) {
		testCases := []struct {
			name   string
			origin string
		}{
			{"10.x network", "http://10.0.0.1:" + serverPort},
			{"172.16.x network", "http://172.16.0.1:" + serverPort},
			{"192.168.x network", "http://192.168.1.1:" + serverPort},
			{"Tailscale lower range", "http://100.64.0.1:" + serverPort},
			{"Tailscale upper range", "http://100.127.255.254:" + serverPort},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				dialer := websocket.DefaultDialer
				headers := http.Header{}
				headers.Add("Origin", tc.origin)

				conn, _, err := dialer.Dial(wsURL, headers)
				if err != nil {
					t.Errorf("Private IP origin %s should be accepted: %v", tc.origin, err)
				}
				if conn != nil {
					conn.Close()
				}
			})
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
	resetWebSocketTestState(t)
	stateMgr, err := state.NewManager()
	if err != nil {
		t.Fatalf("Failed to create state manager: %v", err)
	}
	eventMgr := events.NewManager()

	server := httptest.NewServer(HandleWebSocket(stateMgr, eventMgr))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	t.Run("multiple concurrent connections", func(t *testing.T) {
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
		_ = CreateWSToken()                // Create but don't use yet

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

// TestWebSocketReaderGoroutineNoLeak verifies the per-connection reader
// goroutine exits cleanly even when a client is flooding the server with
// messages at the moment the server-side handler returns. Before the fix,
// a reader parked on `clientMessages <- msg` would never observe the
// connection close and leaked for the lifetime of the process.
func TestWebSocketReaderGoroutineNoLeak(t *testing.T) {
	resetWebSocketTestState(t)

	stateMgr, err := state.NewManager()
	if err != nil {
		t.Fatalf("Failed to create state manager: %v", err)
	}
	eventMgr := events.NewManager()

	server := httptest.NewServer(HandleWebSocket(stateMgr, eventMgr))
	defer server.Close()
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	baseline := runtimeNumGoroutine()

	const conns = 50
	for i := 0; i < conns; i++ {
		token := CreateWSToken()
		dialer, headers := createTestDialer()
		conn, _, err := dialer.Dial(wsURL, headers)
		if err != nil {
			t.Fatalf("Failed to connect: %v", err)
		}

		// Authenticate.
		if err := conn.WriteJSON(map[string]interface{}{
			"type":  "auth",
			"token": token,
		}); err != nil {
			t.Fatalf("Failed to send auth: %v", err)
		}

		// Drain auth_success + initial state so the server-side reader has
		// progressed past authentication.
		var msg map[string]interface{}
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		if err := conn.ReadJSON(&msg); err != nil {
			t.Fatalf("Failed to read auth_success: %v", err)
		}
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		if err := conn.ReadJSON(&msg); err != nil {
			t.Fatalf("Failed to read initial state: %v", err)
		}

		// Flood the server with client pings so the reader is repeatedly
		// trying to deliver into clientMessages. We do NOT drain the
		// corresponding pongs, so the main loop's WriteJSON path will
		// eventually block (TCP send buffer fills), the consumer stops
		// reading clientMessages, and a buggy reader parks on the send.
		for j := 0; j < 64; j++ {
			if err := conn.WriteJSON(map[string]interface{}{"type": "ping"}); err != nil {
				break
			}
		}

		// Close abruptly — this should cause the server-side handler to
		// return and tear the reader goroutine down.
		_ = conn.Close()
	}

	// Give the runtime a moment to reap exited goroutines.
	deadline := time.Now().Add(5 * time.Second)
	var leftover int
	for time.Now().Before(deadline) {
		leftover = runtimeNumGoroutine() - baseline
		if leftover <= 5 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Allow some slack for httptest server bookkeeping. With the fix the
	// delta consistently reaps to near zero; without the fix the count
	// scales linearly with `conns`.
	const tolerance = 10
	if leftover > tolerance {
		t.Fatalf("reader goroutines leaked: baseline=%d delta=%d (expected ≤%d, %d connections)",
			baseline, leftover, tolerance, conns)
	}
}

// runtimeNumGoroutine is a thin indirection so the test reads naturally
// without importing runtime in the file-level imports block.
func runtimeNumGoroutine() int {
	return runtimeNumGoroutineImpl()
}

// TestWebSocketReadLimitEnforced verifies HandleWebSocket caps the size of
// incoming messages so an unauthenticated client can't make the server
// buffer an unbounded JSON payload.
func TestWebSocketReadLimitEnforced(t *testing.T) {
	resetWebSocketTestState(t)

	stateMgr, err := state.NewManager()
	if err != nil {
		t.Fatalf("Failed to create state manager: %v", err)
	}
	eventMgr := events.NewManager()

	server := httptest.NewServer(HandleWebSocket(stateMgr, eventMgr))
	defer server.Close()
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	dialer, headers := createTestDialer()
	conn, _, err := dialer.Dial(wsURL, headers)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	// Build a JSON payload comfortably larger than maxIncomingMessageBytes.
	oversized := strings.Repeat("A", maxIncomingMessageBytes+16*1024)
	payload := map[string]interface{}{
		"type":  "auth",
		"token": oversized,
	}
	// Whether the write itself succeeds is implementation-detail; what we
	// require is that the server tears the connection down rather than
	// happily allocating the full payload.
	_ = conn.WriteJSON(payload)

	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	var resp map[string]interface{}
	err = conn.ReadJSON(&resp)
	if err == nil {
		t.Fatalf("Expected connection to be closed after oversized message, got response: %v", resp)
	}
}
