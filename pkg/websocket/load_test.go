package websocket

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/denysvitali/pictures-sync-s3/pkg/events"
	"github.com/denysvitali/pictures-sync-s3/pkg/state"
	"github.com/gorilla/websocket"
)

// BenchmarkWebSocketUpgrade benchmarks WebSocket upgrade handshake
func BenchmarkWebSocketUpgrade(b *testing.B) {
	tmpDir := b.TempDir()
	stateMgr, eventMgr := setupManagers(b, tmpDir)

	handler := HandleWebSocket(stateMgr, eventMgr)
	server := httptest.NewServer(handler)
	defer server.Close()

	wsURL := "ws" + server.URL[4:]

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		token := CreateWSToken()
		b.StartTimer()

		conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			b.Fatal(err)
		}

		// Send auth
		authMsg := map[string]string{
			"type":  "auth",
			"token": token,
		}
		if err := conn.WriteJSON(authMsg); err != nil {
			b.Fatal(err)
		}

		conn.Close()
	}
}

// BenchmarkWebSocketAuth benchmarks WebSocket authentication
func BenchmarkWebSocketAuth(b *testing.B) {
	b.Run("valid_token", func(b *testing.B) {
		tmpDir := b.TempDir()
		stateMgr, eventMgr := setupManagers(b, tmpDir)

		handler := HandleWebSocket(stateMgr, eventMgr)
		server := httptest.NewServer(handler)
		defer server.Close()

		wsURL := "ws" + server.URL[4:]

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			b.StopTimer()
			token := CreateWSToken()
			conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
			if err != nil {
				b.Fatal(err)
			}
			b.StartTimer()

			authMsg := map[string]string{
				"type":  "auth",
				"token": token,
			}
			if err := conn.WriteJSON(authMsg); err != nil {
				b.Fatal(err)
			}

			// Read auth success
			var response map[string]interface{}
			if err := conn.ReadJSON(&response); err != nil {
				b.Fatal(err)
			}

			conn.Close()
		}
	})

	b.Run("invalid_token", func(b *testing.B) {
		tmpDir := b.TempDir()
		stateMgr, eventMgr := setupManagers(b, tmpDir)

		handler := HandleWebSocket(stateMgr, eventMgr)
		server := httptest.NewServer(handler)
		defer server.Close()

		wsURL := "ws" + server.URL[4:]

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			b.StopTimer()
			conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
			if err != nil {
				b.Fatal(err)
			}
			b.StartTimer()

			authMsg := map[string]string{
				"type":  "auth",
				"token": "invalid-token",
			}
			if err := conn.WriteJSON(authMsg); err != nil {
				b.Fatal(err)
			}

			// Read error response
			var response map[string]interface{}
			conn.ReadJSON(&response)

			conn.Close()
		}
	})
}

// BenchmarkWebSocketMessageReceive benchmarks message reception
func BenchmarkWebSocketMessageReceive(b *testing.B) {
	tmpDir := b.TempDir()
	stateMgr, eventMgr := setupManagers(b, tmpDir)

	handler := HandleWebSocket(stateMgr, eventMgr)
	server := httptest.NewServer(handler)
	defer server.Close()

	wsURL := "ws" + server.URL[4:]

	// Setup authenticated connection
	token := CreateWSToken()
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		b.Fatal(err)
	}
	defer conn.Close()

	// Authenticate
	authMsg := map[string]string{
		"type":  "auth",
		"token": token,
	}
	if err := conn.WriteJSON(authMsg); err != nil {
		b.Fatal(err)
	}

	// Read auth success
	var authResp map[string]interface{}
	if err := conn.ReadJSON(&authResp); err != nil {
		b.Fatal(err)
	}

	// Read initial state
	var initialState map[string]interface{}
	if err := conn.ReadJSON(&initialState); err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()

	// Trigger state updates
	for i := 0; i < b.N; i++ {
		// Update state in background
		go stateMgr.SetStatus(state.StatusSyncing)

		// Read message
		var msg map[string]interface{}
		if err := conn.ReadJSON(&msg); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkConcurrentConnections benchmarks multiple simultaneous connections
func BenchmarkConcurrentConnections(b *testing.B) {
	tmpDir := b.TempDir()
	stateMgr, eventMgr := setupManagers(b, tmpDir)

	handler := HandleWebSocket(stateMgr, eventMgr)
	server := httptest.NewServer(handler)
	defer server.Close()

	wsURL := "ws" + server.URL[4:]

	connections := []struct {
		name  string
		count int
	}{
		{"10_connections", 10},
		{"50_connections", 50},
		{"100_connections", 100},
	}

	for _, tc := range connections {
		b.Run(tc.name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				var wg sync.WaitGroup
				errors := make(chan error, tc.count)

				b.StartTimer()
				for j := 0; j < tc.count; j++ {
					wg.Add(1)
					go func() {
						defer wg.Done()

						token := CreateWSToken()
						conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
						if err != nil {
							errors <- err
							return
						}
						defer conn.Close()

						// Authenticate
						authMsg := map[string]string{
							"type":  "auth",
							"token": token,
						}
						if err := conn.WriteJSON(authMsg); err != nil {
							errors <- err
							return
						}

						// Read responses
						var resp map[string]interface{}
						conn.ReadJSON(&resp) // auth success
						conn.ReadJSON(&resp) // initial state
					}()
				}

				wg.Wait()
				close(errors)

				if len(errors) > 0 {
					b.Fatalf("got %d errors: %v", len(errors), <-errors)
				}
			}
		})
	}
}

// BenchmarkTokenGeneration benchmarks token generation
func BenchmarkTokenGeneration(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		token := GenerateWSToken()
		if len(token) == 0 {
			b.Fatal("generated empty token")
		}
	}
}

// BenchmarkTokenValidation benchmarks token validation
func BenchmarkTokenValidation(b *testing.B) {
	// Create some valid tokens
	tokens := make([]string, 100)
	for i := 0; i < 100; i++ {
		tokens[i] = CreateWSToken()
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		token := tokens[i%100]
		_ = ValidateWSToken(token)
	}
}

// BenchmarkOriginValidation benchmarks origin checking
func BenchmarkOriginValidation(b *testing.B) {
	testCases := []struct {
		name   string
		origin string
	}{
		{"localhost", "http://localhost:8080"},
		{"private_ip", "http://192.168.1.100:8080"},
		{"same_host", "http://example.com:8080"},
	}

	for _, tc := range testCases {
		b.Run(tc.name, func(b *testing.B) {
			req := &http.Request{
				Header: http.Header{
					"Origin": []string{tc.origin},
				},
				Host:       "example.com:8080",
				RemoteAddr: "192.168.1.100:12345",
			}

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = checkOriginStrict(req)
			}
		})
	}
}

// BenchmarkRateLimiter benchmarks rate limiting
func BenchmarkRateLimiter(b *testing.B) {
	limiter := NewConnectionRateLimiter()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ip := fmt.Sprintf("192.168.1.%d", i%255)
		l := limiter.GetLimiter(ip)
		_ = l.Allow()
	}
}

// LoadTestWebSocket performs load testing on WebSocket connections
func LoadTestWebSocket(t *testing.T, concurrency, messagesPerConn int) {
	tmpDir := t.TempDir()
	stateMgr, eventMgr := setupManagers(t, tmpDir)

	handler := HandleWebSocket(stateMgr, eventMgr)
	server := httptest.NewServer(handler)
	defer server.Close()

	wsURL := "ws" + server.URL[4:]

	start := time.Now()
	var successCount, errorCount atomic.Int64
	var totalLatency atomic.Int64

	var wg sync.WaitGroup

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			token := CreateWSToken()
			conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
			if err != nil {
				errorCount.Add(1)
				return
			}
			defer conn.Close()

			// Authenticate
			authMsg := map[string]string{
				"type":  "auth",
				"token": token,
			}
			reqStart := time.Now()
			if err := conn.WriteJSON(authMsg); err != nil {
				errorCount.Add(1)
				return
			}

			// Read auth response
			var authResp map[string]interface{}
			if err := conn.ReadJSON(&authResp); err != nil {
				errorCount.Add(1)
				return
			}
			authLatency := time.Since(reqStart)
			totalLatency.Add(int64(authLatency))

			// Read initial state
			var initialState map[string]interface{}
			if err := conn.ReadJSON(&initialState); err != nil {
				errorCount.Add(1)
				return
			}

			successCount.Add(1)

			// Receive messages
			for j := 0; j < messagesPerConn; j++ {
				var msg map[string]interface{}
				conn.SetReadDeadline(time.Now().Add(5 * time.Second))
				if err := conn.ReadJSON(&msg); err != nil {
					break // Connection closed or timeout
				}
			}
		}(i)
	}

	// Send state updates while connections are active
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for i := 0; i < messagesPerConn*2; i++ {
			select {
			case <-ticker.C:
				stateMgr.SetStatus(state.StatusSyncing)
			}
		}
	}()

	wg.Wait()
	duration := time.Since(start)

	success := successCount.Load()
	errors := errorCount.Load()
	avgLatency := time.Duration(0)
	if success > 0 {
		avgLatency = time.Duration(totalLatency.Load() / success)
	}

	t.Logf("WebSocket Load Test Results:")
	t.Logf("  Concurrency: %d", concurrency)
	t.Logf("  Messages per connection: %d", messagesPerConn)
	t.Logf("  Duration: %v", duration)
	t.Logf("  Successful connections: %d", success)
	t.Logf("  Failed connections: %d", errors)
	t.Logf("  Success rate: %.2f%%", float64(success)/float64(concurrency)*100)
	t.Logf("  Avg auth latency: %v", avgLatency)

	if errors > int64(float64(concurrency)*0.05) { // Allow up to 5% failure rate
		t.Errorf("Load test error rate too high: %d errors out of %d connections", errors, concurrency)
	}
}

// TestWebSocketLoadTest runs various load test scenarios
func TestWebSocketLoadTest(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping load test in short mode")
	}

	testCases := []struct {
		name        string
		concurrency int
		messages    int
	}{
		{"low_load", 10, 10},
		{"medium_load", 50, 20},
		{"high_load", 100, 30},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			LoadTestWebSocket(t, tc.concurrency, tc.messages)
		})
	}
}

// TestWebSocketStress stress tests WebSocket under extreme load
func TestWebSocketStress(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress test in short mode")
	}

	tmpDir := t.TempDir()
	stateMgr, eventMgr := setupManagers(t, tmpDir)

	handler := HandleWebSocket(stateMgr, eventMgr)
	server := httptest.NewServer(handler)
	defer server.Close()

	wsURL := "ws" + server.URL[4:]

	const (
		concurrency = 200
		duration    = 10 * time.Second
	)

	var connectCount, messageCount, errorCount atomic.Int64
	stopChan := make(chan struct{})

	start := time.Now()
	var wg sync.WaitGroup

	// Spawn workers
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for {
				select {
				case <-stopChan:
					return
				default:
					token := CreateWSToken()
					conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
					if err != nil {
						errorCount.Add(1)
						time.Sleep(10 * time.Millisecond)
						continue
					}

					// Authenticate
					authMsg := map[string]string{
						"type":  "auth",
						"token": token,
					}
					if err := conn.WriteJSON(authMsg); err != nil {
						errorCount.Add(1)
						conn.Close()
						continue
					}

					connectCount.Add(1)

					// Read messages for a short time
					conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
					for j := 0; j < 5; j++ {
						var msg map[string]interface{}
						if err := conn.ReadJSON(&msg); err != nil {
							break
						}
						messageCount.Add(1)
					}

					conn.Close()
				}
			}
		}()
	}

	// Send state updates
	go func() {
		ticker := time.NewTicker(50 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stopChan:
				return
			case <-ticker.C:
				stateMgr.SetStatus(state.StatusSyncing)
			}
		}
	}()

	// Run for specified duration
	time.Sleep(duration)
	close(stopChan)
	wg.Wait()

	elapsed := time.Since(start)
	connects := connectCount.Load()
	messages := messageCount.Load()
	errors := errorCount.Load()

	t.Logf("WebSocket Stress Test Results:")
	t.Logf("  Concurrency: %d", concurrency)
	t.Logf("  Duration: %v", elapsed)
	t.Logf("  Successful connections: %d", connects)
	t.Logf("  Messages received: %d", messages)
	t.Logf("  Errors: %d", errors)
	t.Logf("  Connections/sec: %.2f", float64(connects)/elapsed.Seconds())
	t.Logf("  Messages/sec: %.2f", float64(messages)/elapsed.Seconds())
	t.Logf("  Error rate: %.2f%%", float64(errors)/float64(connects+errors)*100)
}

// BenchmarkNotificationBroadcast benchmarks state notification broadcasting
func BenchmarkNotificationBroadcast(b *testing.B) {
	tmpDir := b.TempDir()
	stateMgr, eventMgr := setupManagers(b, tmpDir)

	handler := HandleWebSocket(stateMgr, eventMgr)
	server := httptest.NewServer(handler)
	defer server.Close()

	wsURL := "ws" + server.URL[4:]

	// Create multiple connections
	const numConnections = 10
	conns := make([]*websocket.Conn, numConnections)

	for i := 0; i < numConnections; i++ {
		token := CreateWSToken()
		conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			b.Fatal(err)
		}
		defer conn.Close()

		// Authenticate
		authMsg := map[string]string{
			"type":  "auth",
			"token": token,
		}
		if err := conn.WriteJSON(authMsg); err != nil {
			b.Fatal(err)
		}

		// Read auth response and initial state
		var resp map[string]interface{}
		conn.ReadJSON(&resp)
		conn.ReadJSON(&resp)

		conns[i] = conn

		// Start reading messages
		go func(c *websocket.Conn) {
			for {
				var msg map[string]interface{}
				if err := c.ReadJSON(&msg); err != nil {
					return
				}
			}
		}(conn)
	}

	b.ResetTimer()

	// Broadcast updates
	for i := 0; i < b.N; i++ {
		stateMgr.SetStatus(state.StatusSyncing)
		time.Sleep(1 * time.Millisecond) // Small delay for message delivery
	}
}

// TestConnectionTimeout tests connection timeout behavior
func TestConnectionTimeout(t *testing.T) {
	tmpDir := t.TempDir()
	stateMgr, eventMgr := setupManagers(t, tmpDir)

	handler := HandleWebSocket(stateMgr, eventMgr)
	server := httptest.NewServer(handler)
	defer server.Close()

	wsURL := "ws" + server.URL[4:]

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	// Don't send auth - should timeout after 5 seconds
	start := time.Now()
	var msg map[string]interface{}
	err = conn.ReadJSON(&msg)

	elapsed := time.Since(start)

	if err == nil {
		t.Error("expected timeout error, got nil")
	}

	if elapsed < 4*time.Second || elapsed > 6*time.Second {
		t.Errorf("expected ~5s timeout, got %v", elapsed)
	}
}

// setupManagers creates test state and event managers
func setupManagers(tb testing.TB, tmpDir string) (*state.Manager, *events.Manager) {
	tb.Helper()

	oldStateDir := state.GetStateDir()
	state.SetStateDir(tmpDir)
	tb.Cleanup(func() {
		state.SetStateDir(oldStateDir)
	})

	stateMgr, err := state.NewManager()
	if err != nil {
		tb.Fatal(err)
	}

	eventMgr := events.NewManager()

	return stateMgr, eventMgr
}

// BenchmarkMemoryUsagePerConnection measures memory per WebSocket connection
func BenchmarkMemoryUsagePerConnection(b *testing.B) {
	tmpDir := b.TempDir()
	stateMgr, eventMgr := setupManagers(b, tmpDir)

	handler := HandleWebSocket(stateMgr, eventMgr)
	server := httptest.NewServer(handler)
	defer server.Close()

	wsURL := "ws" + server.URL[4:]

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		token := CreateWSToken()
		conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			b.Fatal(err)
		}

		// Authenticate
		authMsg := map[string]string{
			"type":  "auth",
			"token": token,
		}
		conn.WriteJSON(authMsg)

		// Read responses
		var resp map[string]interface{}
		conn.ReadJSON(&resp)
		conn.ReadJSON(&resp)

		conn.Close()
	}
}

// BenchmarkTokenCleanup benchmarks expired token cleanup
func BenchmarkTokenCleanup(b *testing.B) {
	// Create many expired tokens
	for i := 0; i < 1000; i++ {
		token := GenerateWSToken()
		wsTokenMutex.Lock()
		wsTokens[token] = time.Now().Add(-10 * time.Minute) // Already expired
		wsTokenMutex.Unlock()
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Manually trigger cleanup
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
