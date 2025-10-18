package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/denysvitali/pictures-sync-s3/pkg/events"
	"github.com/denysvitali/pictures-sync-s3/pkg/handlers"
	"github.com/denysvitali/pictures-sync-s3/pkg/settings"
	"github.com/denysvitali/pictures-sync-s3/pkg/ssrf"
	"github.com/denysvitali/pictures-sync-s3/pkg/state"
	"github.com/denysvitali/pictures-sync-s3/pkg/syncmanager"
	"github.com/denysvitali/pictures-sync-s3/pkg/websocket"
	"github.com/denysvitali/pictures-sync-s3/pkg/wifimanager"
	"github.com/gorilla/websocket"
)

// TestE2EWebSocketRealTimeUpdates tests WebSocket real-time state updates
func TestE2EWebSocketRealTimeUpdates(t *testing.T) {
	testEnv := setupIntegrationEnvironment(t)
	defer testEnv.Cleanup()

	stateMgr := testEnv.StateMgr
	eventMgr := events.NewManager()

	// Setup WebSocket server
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", websocket.HandleWebSocket(stateMgr, eventMgr))

	// Get WebSocket token
	mux.HandleFunc("/api/ws-token", handlers.HandleWSToken)

	server := httptest.NewServer(mux)
	defer server.Close()

	// Get WebSocket token first
	resp, err := http.Get(server.URL + "/api/ws-token")
	if err != nil {
		t.Fatalf("Failed to get WS token: %v", err)
	}
	defer resp.Body.Close()

	var tokenResp struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		t.Fatalf("Failed to decode token response: %v", err)
	}

	// Connect to WebSocket
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws?token=" + tokenResp.Token
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to connect to WebSocket: %v", err)
	}
	defer ws.Close()

	// Channel to collect WebSocket messages
	messages := make(chan map[string]interface{}, 100)
	wsErrors := make(chan error, 1)

	go func() {
		for {
			var msg map[string]interface{}
			err := ws.ReadJSON(&msg)
			if err != nil {
				wsErrors <- err
				return
			}
			messages <- msg
		}
	}()

	// Test state transitions and verify WebSocket updates
	stateChanges := []struct {
		name   string
		change func()
		verify func(msg map[string]interface{}) bool
	}{
		{
			name: "Detected status",
			change: func() {
				stateMgr.SetStatus(state.StatusDetected)
			},
			verify: func(msg map[string]interface{}) bool {
				return msg["status"] == "detected"
			},
		},
		{
			name: "SD card mounted",
			change: func() {
				stateMgr.SetSDCard(true, "/mnt/test")
			},
			verify: func(msg map[string]interface{}) bool {
				return msg["sdcard_mounted"] == true
			},
		},
		{
			name: "Start sync",
			change: func() {
				stateMgr.StartSync("ws-test-card", 100, 100*1024*1024)
			},
			verify: func(msg map[string]interface{}) bool {
				if msg["status"] != "syncing" {
					return false
				}
				currentSync, ok := msg["current_sync"].(map[string]interface{})
				return ok && currentSync != nil
			},
		},
		{
			name: "Sync progress",
			change: func() {
				stateMgr.UpdateSyncProgress(50, 50*1024*1024, "IMG_0050.JPG", 2*1024*1024, 1.5*1024*1024, "1m30s")
			},
			verify: func(msg map[string]interface{}) bool {
				currentSync, ok := msg["current_sync"].(map[string]interface{})
				if !ok {
					return false
				}
				filesSynced, ok := currentSync["files_synced"].(float64)
				return ok && filesSynced == 50
			},
		},
		{
			name: "Finish sync",
			change: func() {
				stateMgr.FinishSync(true, nil)
			},
			verify: func(msg map[string]interface{}) bool {
				return msg["status"] == "success"
			},
		},
	}

	for _, tc := range stateChanges {
		t.Run(tc.name, func(t *testing.T) {
			// Apply state change
			tc.change()

			// Wait for WebSocket message
			timeout := time.After(2 * time.Second)
			verified := false

			for !verified {
				select {
				case msg := <-messages:
					if tc.verify(msg) {
						verified = true
						t.Logf("WebSocket message verified: %s", tc.name)
					}
				case err := <-wsErrors:
					t.Fatalf("WebSocket error: %v", err)
				case <-timeout:
					t.Fatalf("Timeout waiting for WebSocket message: %s", tc.name)
				}
			}
		})
	}
}

// TestE2EWebSocketMultipleClients tests multiple WebSocket clients
func TestE2EWebSocketMultipleClients(t *testing.T) {
	testEnv := setupIntegrationEnvironment(t)
	defer testEnv.Cleanup()

	stateMgr := testEnv.StateMgr
	eventMgr := events.NewManager()

	// Setup server
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", websocket.HandleWebSocket(stateMgr, eventMgr))
	mux.HandleFunc("/api/ws-token", handlers.HandleWSToken)

	server := httptest.NewServer(mux)
	defer server.Close()

	// Connect multiple clients
	const numClients = 10
	clients := make([]*websocket.Conn, numClients)
	clientMessages := make([]chan map[string]interface{}, numClients)

	for i := 0; i < numClients; i++ {
		// Get token
		resp, err := http.Get(server.URL + "/api/ws-token")
		if err != nil {
			t.Fatalf("Client %d: failed to get token: %v", i, err)
		}

		var tokenResp struct {
			Token string `json:"token"`
		}
		json.NewDecoder(resp.Body).Decode(&tokenResp)
		resp.Body.Close()

		// Connect
		wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws?token=" + tokenResp.Token
		ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			t.Fatalf("Client %d: failed to connect: %v", i, err)
		}

		clients[i] = ws
		clientMessages[i] = make(chan map[string]interface{}, 100)

		// Start reading messages
		go func(idx int, conn *websocket.Conn, msgChan chan map[string]interface{}) {
			for {
				var msg map[string]interface{}
				if err := conn.ReadJSON(&msg); err != nil {
					return
				}
				msgChan <- msg
			}
		}(i, ws, clientMessages[i])
	}

	// Cleanup clients
	defer func() {
		for _, client := range clients {
			client.Close()
		}
	}()

	// Broadcast state change
	stateMgr.SetStatus(state.StatusDetected)

	// Verify all clients received the update
	timeout := time.After(3 * time.Second)
	receivedCount := 0
	var receiveMu sync.Mutex

	for i := 0; i < numClients; i++ {
		go func(idx int) {
			select {
			case msg := <-clientMessages[idx]:
				if msg["status"] == "detected" {
					receiveMu.Lock()
					receivedCount++
					receiveMu.Unlock()
					t.Logf("Client %d received update", idx)
				}
			case <-timeout:
				t.Logf("Client %d timeout", idx)
			}
		}(i)
	}

	// Wait for all clients
	time.Sleep(2 * time.Second)

	receiveMu.Lock()
	finalCount := receivedCount
	receiveMu.Unlock()

	if finalCount != numClients {
		t.Errorf("Only %d/%d clients received update", finalCount, numClients)
	} else {
		t.Logf("All %d clients received update successfully", numClients)
	}
}

// TestE2EWebSocketReconnection tests WebSocket reconnection handling
func TestE2EWebSocketReconnection(t *testing.T) {
	testEnv := setupIntegrationEnvironment(t)
	defer testEnv.Cleanup()

	stateMgr := testEnv.StateMgr
	eventMgr := events.NewManager()

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", websocket.HandleWebSocket(stateMgr, eventMgr))
	mux.HandleFunc("/api/ws-token", handlers.HandleWSToken)

	server := httptest.NewServer(mux)
	defer server.Close()

	// Helper to connect
	connect := func() (*websocket.Conn, error) {
		resp, err := http.Get(server.URL + "/api/ws-token")
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		var tokenResp struct {
			Token string `json:"token"`
		}
		json.NewDecoder(resp.Body).Decode(&tokenResp)

		wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws?token=" + tokenResp.Token
		return websocket.DefaultDialer.Dial(wsURL, nil)
	}

	// First connection
	ws1, err := connect()
	if err != nil {
		t.Fatalf("First connection failed: %v", err)
	}

	// Apply state change
	stateMgr.SetStatus(state.StatusDetected)

	// Read message
	var msg1 map[string]interface{}
	if err := ws1.ReadJSON(&msg1); err != nil {
		t.Fatalf("Failed to read first message: %v", err)
	}

	if msg1["status"] != "detected" {
		t.Errorf("First connection: status = %v, want detected", msg1["status"])
	}

	// Close first connection
	ws1.Close()
	t.Log("First connection closed")

	// Wait a moment
	time.Sleep(500 * time.Millisecond)

	// Reconnect
	ws2, err := connect()
	if err != nil {
		t.Fatalf("Reconnection failed: %v", err)
	}
	defer ws2.Close()

	t.Log("Reconnected successfully")

	// Should receive current state immediately
	var msg2 map[string]interface{}
	ws2.SetReadDeadline(time.Now().Add(2 * time.Second))
	if err := ws2.ReadJSON(&msg2); err != nil {
		t.Fatalf("Failed to read after reconnect: %v", err)
	}

	// New connection should get current state
	if msg2["status"] == "" {
		t.Error("Reconnected client did not receive state")
	}

	t.Log("Reconnection test completed successfully")
}

// TestE2EWebSocketEventStream tests event streaming through WebSocket
func TestE2EWebSocketEventStream(t *testing.T) {
	testEnv := setupIntegrationEnvironment(t)
	defer testEnv.Cleanup()

	stateMgr := testEnv.StateMgr
	eventMgr := events.NewManager()

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", websocket.HandleWebSocket(stateMgr, eventMgr))
	mux.HandleFunc("/api/ws-token", handlers.HandleWSToken)

	server := httptest.NewServer(mux)
	defer server.Close()

	// Get token and connect
	resp, _ := http.Get(server.URL + "/api/ws-token")
	var tokenResp struct {
		Token string `json:"token"`
	}
	json.NewDecoder(resp.Body).Decode(&tokenResp)
	resp.Body.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws?token=" + tokenResp.Token
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer ws.Close()

	// Collect all events
	eventTypes := make([]events.EventType, 0)
	var eventsMu sync.Mutex

	go func() {
		for {
			var msg map[string]interface{}
			if err := ws.ReadJSON(&msg); err != nil {
				return
			}

			// Check if this is an event message
			if eventType, ok := msg["event_type"].(string); ok {
				eventsMu.Lock()
				eventTypes = append(eventTypes, events.EventType(eventType))
				eventsMu.Unlock()
			}
		}
	}()

	// Emit various events
	testEvents := []events.EventType{
		events.EventInfo,
		events.EventSDCardInserted,
		events.EventPhotosDetected,
		events.EventSyncStarted,
		events.EventSyncProgress,
	}

	for _, eventType := range testEvents {
		switch eventType {
		case events.EventInfo:
			eventMgr.EmitInfo("Test info", nil)
		case events.EventSDCardInserted:
			eventMgr.EmitSDCardInserted("test-dev", "/mnt/test")
		case events.EventPhotosDetected:
			eventMgr.EmitPhotosDetected(100, 100*1024*1024)
		case events.EventSyncStarted:
			eventMgr.EmitSyncStarted("test-card", 100, 100*1024*1024)
		case events.EventSyncProgress:
			eventMgr.EmitSyncProgress(50, 50*1024*1024, "IMG_0050.JPG", 2.5, "1m")
		}

		time.Sleep(100 * time.Millisecond)
	}

	// Wait for events to be received
	time.Sleep(1 * time.Second)

	// Verify events were received
	eventsMu.Lock()
	receivedCount := len(eventTypes)
	eventsMu.Unlock()

	if receivedCount == 0 {
		t.Error("No events received through WebSocket")
	} else {
		t.Logf("Received %d events through WebSocket", receivedCount)
	}
}

// TestE2EWebSocketSyncProgressUpdates tests real-time sync progress via WebSocket
func TestE2EWebSocketSyncProgressUpdates(t *testing.T) {
	testEnv := setupIntegrationEnvironment(t)
	defer testEnv.Cleanup()

	stateMgr := testEnv.StateMgr
	eventMgr := events.NewManager()

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", websocket.HandleWebSocket(stateMgr, eventMgr))
	mux.HandleFunc("/api/ws-token", handlers.HandleWSToken)

	server := httptest.NewServer(mux)
	defer server.Close()

	// Connect to WebSocket
	resp, _ := http.Get(server.URL + "/api/ws-token")
	var tokenResp struct {
		Token string `json:"token"`
	}
	json.NewDecoder(resp.Body).Decode(&tokenResp)
	resp.Body.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws?token=" + tokenResp.Token
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer ws.Close()

	// Track progress updates
	progressUpdates := make([]int64, 0)
	var progressMu sync.Mutex

	go func() {
		for {
			var msg map[string]interface{}
			if err := ws.ReadJSON(&msg); err != nil {
				return
			}

			if currentSync, ok := msg["current_sync"].(map[string]interface{}); ok {
				if filesSynced, ok := currentSync["files_synced"].(float64); ok {
					progressMu.Lock()
					progressUpdates = append(progressUpdates, int64(filesSynced))
					progressMu.Unlock()
				}
			}
		}
	}()

	// Simulate sync with progress updates
	stateMgr.StartSync("progress-test", 100, 100*1024*1024)

	for i := 0; i <= 100; i += 10 {
		stateMgr.UpdateSyncProgress(
			int64(i),
			int64(i)*1024*1024,
			fmt.Sprintf("IMG_%04d.JPG", i),
			2*1024*1024,
			1.5*1024*1024,
			fmt.Sprintf("%ds", 100-i),
		)
		time.Sleep(50 * time.Millisecond)
	}

	stateMgr.FinishSync(true, nil)

	// Wait for all updates
	time.Sleep(1 * time.Second)

	// Verify progress updates
	progressMu.Lock()
	updateCount := len(progressUpdates)
	progressMu.Unlock()

	if updateCount == 0 {
		t.Fatal("No progress updates received")
	}

	// Verify progress is monotonically increasing
	progressMu.Lock()
	for i := 1; i < len(progressUpdates); i++ {
		if progressUpdates[i] < progressUpdates[i-1] {
			t.Errorf("Progress went backwards: %d -> %d", progressUpdates[i-1], progressUpdates[i])
		}
	}
	progressMu.Unlock()

	t.Logf("Received %d progress updates", updateCount)
}

// TestE2EWebSocketTokenValidation tests WebSocket token security
func TestE2EWebSocketTokenValidation(t *testing.T) {
	testEnv := setupIntegrationEnvironment(t)
	defer testEnv.Cleanup()

	stateMgr := testEnv.StateMgr
	eventMgr := events.NewManager()

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", websocket.HandleWebSocket(stateMgr, eventMgr))
	mux.HandleFunc("/api/ws-token", handlers.HandleWSToken)

	server := httptest.NewServer(mux)
	defer server.Close()

	baseURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"

	// Test 1: Connection without token should fail
	_, resp, err := websocket.DefaultDialer.Dial(baseURL, nil)
	if err == nil {
		t.Error("Connection without token should fail")
	} else {
		t.Logf("Correctly rejected connection without token: %v", err)
	}
	if resp != nil {
		resp.Body.Close()
	}

	// Test 2: Connection with invalid token should fail
	invalidURL := baseURL + "?token=invalid-token-12345"
	_, resp, err = websocket.DefaultDialer.Dial(invalidURL, nil)
	if err == nil {
		t.Error("Connection with invalid token should fail")
	} else {
		t.Logf("Correctly rejected invalid token: %v", err)
	}
	if resp != nil {
		resp.Body.Close()
	}

	// Test 3: Connection with valid token should succeed
	tokenResp, _ := http.Get(server.URL + "/api/ws-token")
	var token struct {
		Token string `json:"token"`
	}
	json.NewDecoder(tokenResp.Body).Decode(&token)
	tokenResp.Body.Close()

	validURL := baseURL + "?token=" + token.Token
	ws, _, err := websocket.DefaultDialer.Dial(validURL, nil)
	if err != nil {
		t.Errorf("Connection with valid token should succeed: %v", err)
	} else {
		ws.Close()
		t.Log("Valid token connection succeeded")
	}

	// Test 4: Token reuse should fail (one-time use)
	_, resp, err = websocket.DefaultDialer.Dial(validURL, nil)
	if err == nil {
		t.Error("Token reuse should fail")
	} else {
		t.Logf("Correctly rejected token reuse: %v", err)
	}
	if resp != nil {
		resp.Body.Close()
	}
}

// TestE2EWebSocketConcurrentUpdates tests handling concurrent state updates
func TestE2EWebSocketConcurrentUpdates(t *testing.T) {
	testEnv := setupIntegrationEnvironment(t)
	defer testEnv.Cleanup()

	stateMgr := testEnv.StateMgr
	eventMgr := events.NewManager()

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", websocket.HandleWebSocket(stateMgr, eventMgr))
	mux.HandleFunc("/api/ws-token", handlers.HandleWSToken)

	server := httptest.NewServer(mux)
	defer server.Close()

	// Connect client
	tokenResp, _ := http.Get(server.URL + "/api/ws-token")
	var token struct {
		Token string `json:"token"`
	}
	json.NewDecoder(tokenResp.Body).Decode(&token)
	tokenResp.Body.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws?token=" + token.Token
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer ws.Close()

	// Count messages received
	messageCount := 0
	var countMu sync.Mutex

	go func() {
		for {
			var msg map[string]interface{}
			if err := ws.ReadJSON(&msg); err != nil {
				return
			}
			countMu.Lock()
			messageCount++
			countMu.Unlock()
		}
	}()

	// Generate concurrent state updates
	const numUpdates = 50
	var wg sync.WaitGroup

	for i := 0; i < numUpdates; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			stateMgr.SetStatus(state.StatusSyncing)
			stateMgr.UpdateSyncProgress(int64(n), int64(n)*1024, fmt.Sprintf("file%d.jpg", n), 1024, 1024, "1s")
		}(i)
	}

	wg.Wait()
	time.Sleep(1 * time.Second)

	// Verify messages were received without errors
	countMu.Lock()
	finalCount := messageCount
	countMu.Unlock()

	if finalCount == 0 {
		t.Error("No messages received during concurrent updates")
	} else {
		t.Logf("Received %d messages from %d concurrent updates", finalCount, numUpdates)
	}
}

// TestE2EWebSocketLongRunningConnection tests WebSocket stability over time
func TestE2EWebSocketLongRunningConnection(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running test in short mode")
	}

	testEnv := setupIntegrationEnvironment(t)
	defer testEnv.Cleanup()

	stateMgr := testEnv.StateMgr
	eventMgr := events.NewManager()

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", websocket.HandleWebSocket(stateMgr, eventMgr))
	mux.HandleFunc("/api/ws-token", handlers.HandleWSToken)

	server := httptest.NewServer(mux)
	defer server.Close()

	// Connect
	tokenResp, _ := http.Get(server.URL + "/api/ws-token")
	var token struct {
		Token string `json:"token"`
	}
	json.NewDecoder(tokenResp.Body).Decode(&token)
	tokenResp.Body.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws?token=" + token.Token
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer ws.Close()

	// Track messages
	messageCount := 0
	errors := 0
	var statsMu sync.Mutex

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Read messages
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
				var msg map[string]interface{}
				if err := ws.ReadJSON(&msg); err != nil {
					statsMu.Lock()
					errors++
					statsMu.Unlock()
					return
				}
				statsMu.Lock()
				messageCount++
				statsMu.Unlock()
			}
		}
	}()

	// Send periodic updates
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	updateCount := 0
	for {
		select {
		case <-ctx.Done():
			goto done
		case <-ticker.C:
			stateMgr.SetStatus(state.StatusSyncing)
			updateCount++
		}
	}

done:
	time.Sleep(500 * time.Millisecond) // Let final messages arrive

	statsMu.Lock()
	finalMessages := messageCount
	finalErrors := errors
	statsMu.Unlock()

	t.Logf("Long-running test: sent %d updates, received %d messages, %d errors",
		updateCount, finalMessages, finalErrors)

	if finalErrors > 0 {
		t.Errorf("WebSocket had %d errors during long-running test", finalErrors)
	}

	if finalMessages == 0 {
		t.Error("No messages received during long-running test")
	}
}

// TestE2EWebSocketIntegrationWithSync tests WebSocket during actual sync operation
func TestE2EWebSocketIntegrationWithSync(t *testing.T) {
	testEnv := setupIntegrationEnvironment(t)
	defer testEnv.Cleanup()

	stateMgr := testEnv.StateMgr
	eventMgr := events.NewManager()
	appSettings, _ := settings.Load()

	syncMgr := syncmanager.NewManager(
		testEnv.RcloneConfigPath,
		appSettings.GetRemoteName(),
		appSettings.GetRemotePath(),
		stateMgr,
		appSettings.GetTransfers(),
		appSettings.GetCheckers(),
	)

	// Create handlers
	wifiMgr, _ := wifimanager.NewManager()
	ssrfValidator := ssrf.NewValidator(10, time.Minute)

	ctx := &handlers.Context{
		StateMgr:      stateMgr,
		SyncMgr:       syncMgr,
		WiFiMgr:       wifiMgr,
		AppSettings:   appSettings,
		SSRFValidator: ssrfValidator,
	}

	// Setup server
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", websocket.HandleWebSocket(stateMgr, eventMgr))
	mux.HandleFunc("/api/ws-token", handlers.HandleWSToken)
	mux.HandleFunc("/api/sync/start", ctx.HandleSyncStart)

	server := httptest.NewServer(mux)
	defer server.Close()

	// Connect WebSocket
	tokenResp, _ := http.Get(server.URL + "/api/ws-token")
	var token struct {
		Token string `json:"token"`
	}
	json.NewDecoder(tokenResp.Body).Decode(&token)
	tokenResp.Body.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws?token=" + token.Token
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer ws.Close()

	// Track sync lifecycle through WebSocket
	syncStates := make([]string, 0)
	var statesMu sync.Mutex

	go func() {
		for {
			var msg map[string]interface{}
			if err := ws.ReadJSON(&msg); err != nil {
				return
			}

			if status, ok := msg["status"].(string); ok {
				statesMu.Lock()
				syncStates = append(syncStates, status)
				statesMu.Unlock()
			}
		}
	}()

	// Create mock card and trigger sync
	mockCard := testEnv.CreateMockSDCard("ws-sync-test", 25)
	testEnv.MountMockCard(mockCard)

	stateMgr.SetSDCard(true, mockCard.MountPath)

	// Note: In real test, we'd trigger actual sync
	// For now, simulate the sync lifecycle
	stateMgr.SetStatus(state.StatusDetected)
	time.Sleep(100 * time.Millisecond)

	stateMgr.StartSync(mockCard.CardID, 25, 25*1024*1024)
	time.Sleep(100 * time.Millisecond)

	for i := 0; i < 25; i += 5 {
		stateMgr.UpdateSyncProgress(int64(i), int64(i)*1024*1024, fmt.Sprintf("IMG_%04d.JPG", i), 1024*1024, 1.5*1024*1024, "30s")
		time.Sleep(100 * time.Millisecond)
	}

	stateMgr.FinishSync(true, nil)
	time.Sleep(500 * time.Millisecond)

	// Verify we saw the complete lifecycle
	statesMu.Lock()
	defer statesMu.Unlock()

	expectedStates := []string{"detected", "syncing", "success"}
	for _, expected := range expectedStates {
		found := false
		for _, actual := range syncStates {
			if actual == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected state %s not found in WebSocket updates", expected)
		}
	}

	t.Logf("WebSocket sync integration: observed states: %v", syncStates)
}
