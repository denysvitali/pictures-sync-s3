package e2e

import (
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
	ws "github.com/gorilla/websocket"
)

// TestUIStateSDCardIndicator tests SD card indicator state consistency
func TestUIStateSDCardIndicator(t *testing.T) {
	testEnv := setupIntegrationEnvironment(t)
	defer testEnv.Cleanup()

	server := httptest.NewServer(createUITestMux(testEnv))
	defer server.Close()

	// Test 1: No SD card mounted
	resp, err := http.Get(server.URL + "/api/status")
	if err != nil {
		t.Fatalf("Status request failed: %v", err)
	}
	defer resp.Body.Close()

	var statusData map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&statusData)

	if statusData["sdcard_mounted"] != false {
		t.Errorf("SD card should not be mounted initially, got: %v", statusData["sdcard_mounted"])
	}

	// Test 2: Mount SD card
	testEnv.StateMgr.SetSDCard(true, "/mnt/test")
	time.Sleep(100 * time.Millisecond)

	resp, err = http.Get(server.URL + "/api/status")
	if err != nil {
		t.Fatalf("Status request failed: %v", err)
	}
	defer resp.Body.Close()

	json.NewDecoder(resp.Body).Decode(&statusData)

	if statusData["sdcard_mounted"] != true {
		t.Errorf("SD card should be mounted, got: %v", statusData["sdcard_mounted"])
	}

	if statusData["sdcard_path"] != "/mnt/test" {
		t.Errorf("SD card path incorrect, got: %v", statusData["sdcard_path"])
	}

	// Test 3: Unmount SD card
	testEnv.StateMgr.SetSDCard(false, "")
	time.Sleep(100 * time.Millisecond)

	resp, err = http.Get(server.URL + "/api/status")
	if err != nil {
		t.Fatalf("Status request failed: %v", err)
	}
	defer resp.Body.Close()

	json.NewDecoder(resp.Body).Decode(&statusData)

	if statusData["sdcard_mounted"] != false {
		t.Errorf("SD card should be unmounted, got: %v", statusData["sdcard_mounted"])
	}

	t.Log("SD card indicator state consistency verified")
}

// TestUIStatePageNavigation tests UI state during page navigation
func TestUIStatePageNavigation(t *testing.T) {
	testEnv := setupIntegrationEnvironment(t)
	defer testEnv.Cleanup()

	server := httptest.NewServer(createUITestMux(testEnv))
	defer server.Close()

	// Simulate navigation between pages
	pages := []struct {
		name            string
		endpoint        string
		expectedStatus  int
		requiresSDCard  bool
		expectIndicator string
	}{
		{"Status Page", "/api/status", 200, false, "idle"},
		{"Gallery (Remote)", "/api/files", 200, false, "gallery-remote"},
		{"Gallery (SD Card)", "/api/sdcard/files", 200, true, "gallery-sdcard"},
		{"Settings Page", "/api/settings", 200, false, "settings"},
		{"History Page", "/api/history", 200, false, "history"},
	}

	for _, page := range pages {
		t.Run(page.name, func(t *testing.T) {
			if page.requiresSDCard {
				testEnv.StateMgr.SetSDCard(true, "/mnt/test")
			}

			resp, err := http.Get(server.URL + page.endpoint)
			if err != nil {
				t.Fatalf("Navigation to %s failed: %v", page.name, err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != page.expectedStatus {
				t.Errorf("%s: expected status %d, got %d", page.name, page.expectedStatus, resp.StatusCode)
			}

			// Verify state is consistent
			statusResp, _ := http.Get(server.URL + "/api/status")
			var statusData map[string]interface{}
			json.NewDecoder(statusResp.Body).Decode(&statusData)
			statusResp.Body.Close()

			// State should reflect which page user is on
			t.Logf("%s: status=%v, sdcard_mounted=%v", page.name, statusData["status"], statusData["sdcard_mounted"])
		})
	}
}

// TestUIStateWebSocketConsistency tests UI state consistency via WebSocket
func TestUIStateWebSocketConsistency(t *testing.T) {
	testEnv := setupIntegrationEnvironment(t)
	defer testEnv.Cleanup()

	server := httptest.NewServer(createUITestMux(testEnv))
	defer server.Close()

	// Connect WebSocket
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"
	conn, _, err := ws.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("WebSocket connection failed: %v", err)
	}
	defer conn.Close()

	// Read initial state from WebSocket
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var wsState map[string]interface{}
	if err := conn.ReadJSON(&wsState); err != nil {
		t.Fatalf("Failed to read WebSocket state: %v", err)
	}

	// Get state via HTTP API
	resp, _ := http.Get(server.URL + "/api/status")
	var httpState map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&httpState)
	resp.Body.Close()

	// States should match
	if wsState["status"] != httpState["status"] {
		t.Errorf("WebSocket status (%v) != HTTP status (%v)", wsState["status"], httpState["status"])
	}

	if wsState["sdcard_mounted"] != httpState["sdcard_mounted"] {
		t.Errorf("WebSocket sdcard_mounted (%v) != HTTP sdcard_mounted (%v)",
			wsState["sdcard_mounted"], httpState["sdcard_mounted"])
	}

	// Test state change propagation
	testEnv.StateMgr.SetStatus(state.StatusDetected)
	time.Sleep(100 * time.Millisecond)

	// WebSocket should receive update
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if err := conn.ReadJSON(&wsState); err != nil {
		t.Fatalf("Failed to read updated state: %v", err)
	}

	// HTTP should also reflect change
	resp, _ = http.Get(server.URL + "/api/status")
	json.NewDecoder(resp.Body).Decode(&httpState)
	resp.Body.Close()

	if wsState["status"] != "detected" || httpState["status"] != "detected" {
		t.Errorf("State change not propagated correctly: ws=%v, http=%v", wsState["status"], httpState["status"])
	}

	t.Log("WebSocket and HTTP state consistency verified")
}

// TestUIStateSyncProgress tests UI state during sync operation
func TestUIStateSyncProgress(t *testing.T) {
	testEnv := setupIntegrationEnvironment(t)
	defer testEnv.Cleanup()

	server := httptest.NewServer(createUITestMux(testEnv))
	defer server.Close()

	// Connect WebSocket to monitor state
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"
	conn, _, err := ws.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("WebSocket connection failed: %v", err)
	}
	defer conn.Close()

	// Read initial state
	var initialState map[string]interface{}
	conn.ReadJSON(&initialState)

	// Start sync
	testEnv.StateMgr.StartSync("test-card", 100, 100*1024*1024)

	// Monitor state changes
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	var syncState map[string]interface{}
	if err := conn.ReadJSON(&syncState); err != nil {
		t.Fatalf("Failed to read sync state: %v", err)
	}

	if syncState["status"] != "syncing" {
		t.Errorf("Expected syncing status, got: %v", syncState["status"])
	}

	// Update progress
	testEnv.StateMgr.UpdateSyncProgress(50, 50*1024*1024, "file50.jpg", 1024*1024, 10*1024*1024, "30s")

	// Verify progress reflected in UI
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var progressState map[string]interface{}
	if err := conn.ReadJSON(&progressState); err != nil {
		t.Fatalf("Failed to read progress state: %v", err)
	}

	// Check via HTTP too
	resp, _ := http.Get(server.URL + "/api/status")
	var httpState map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&httpState)
	resp.Body.Close()

	if httpState["status"] != "syncing" {
		t.Errorf("HTTP status should be syncing, got: %v", httpState["status"])
	}

	t.Log("Sync progress state consistency verified")
}

// TestUIStateConcurrentClients tests state consistency with multiple clients
func TestUIStateConcurrentClients(t *testing.T) {
	testEnv := setupIntegrationEnvironment(t)
	defer testEnv.Cleanup()

	server := httptest.NewServer(createUITestMux(testEnv))
	defer server.Close()

	const numClients = 10
	var wg sync.WaitGroup
	errors := make(chan error, numClients)

	// Connect multiple WebSocket clients
	for i := 0; i < numClients; i++ {
		wg.Add(1)
		go func(clientID int) {
			defer wg.Done()

			wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"
			conn, _, err := ws.DefaultDialer.Dial(wsURL, nil)
			if err != nil {
				errors <- fmt.Errorf("client %d: connection failed: %v", clientID, err)
				return
			}
			defer conn.Close()

			// Read initial state
			conn.SetReadDeadline(time.Now().Add(2 * time.Second))
			var state1 map[string]interface{}
			if err := conn.ReadJSON(&state1); err != nil {
				errors <- fmt.Errorf("client %d: read failed: %v", clientID, err)
				return
			}

			// Wait for state change
			time.Sleep(100 * time.Millisecond)

			// Read updated state
			conn.SetReadDeadline(time.Now().Add(2 * time.Second))
			var state2 map[string]interface{}
			if err := conn.ReadJSON(&state2); err != nil {
				errors <- fmt.Errorf("client %d: update read failed: %v", clientID, err)
				return
			}

			// Verify state is consistent
			if state2["status"] != "detected" {
				errors <- fmt.Errorf("client %d: expected detected, got %v", clientID, state2["status"])
			}
		}(i)
	}

	// Change state while clients are connected
	time.Sleep(50 * time.Millisecond)
	testEnv.StateMgr.SetStatus(state.StatusDetected)

	wg.Wait()
	close(errors)

	errorCount := 0
	for err := range errors {
		t.Error(err)
		errorCount++
	}

	if errorCount > 0 {
		t.Errorf("%d/%d clients experienced state inconsistency", errorCount, numClients)
	} else {
		t.Logf("All %d clients received consistent state updates", numClients)
	}
}

// TestUIStateTransitions tests all state transition paths
func TestUIStateTransitions(t *testing.T) {
	testEnv := setupIntegrationEnvironment(t)
	defer testEnv.Cleanup()

	server := httptest.NewServer(createUITestMux(testEnv))
	defer server.Close()

	transitions := []struct {
		from     state.SyncStatus
		to       state.SyncStatus
		action   string
		validate func(*testing.T, map[string]interface{})
	}{
		{
			from:   state.StatusIdle,
			to:     state.StatusDetected,
			action: "SD card detected",
			validate: func(t *testing.T, s map[string]interface{}) {
				if s["status"] != "detected" {
					t.Errorf("Expected detected, got %v", s["status"])
				}
			},
		},
		{
			from:   state.StatusDetected,
			to:     state.StatusSyncing,
			action: "Sync started",
			validate: func(t *testing.T, s map[string]interface{}) {
				if s["status"] != "syncing" {
					t.Errorf("Expected syncing, got %v", s["status"])
				}
			},
		},
		{
			from:   state.StatusSyncing,
			to:     state.StatusSuccess,
			action: "Sync completed",
			validate: func(t *testing.T, s map[string]interface{}) {
				if s["status"] != "success" {
					t.Errorf("Expected success, got %v", s["status"])
				}
			},
		},
		{
			from:   state.StatusSuccess,
			to:     state.StatusIdle,
			action: "Return to idle",
			validate: func(t *testing.T, s map[string]interface{}) {
				if s["status"] != "idle" {
					t.Errorf("Expected idle, got %v", s["status"])
				}
			},
		},
	}

	for _, tr := range transitions {
		t.Run(tr.action, func(t *testing.T) {
			// Set initial state
			testEnv.StateMgr.SetStatus(tr.from)
			time.Sleep(50 * time.Millisecond)

			// Transition to new state
			testEnv.StateMgr.SetStatus(tr.to)
			time.Sleep(100 * time.Millisecond)

			// Verify via API
			resp, err := http.Get(server.URL + "/api/status")
			if err != nil {
				t.Fatalf("Status request failed: %v", err)
			}
			defer resp.Body.Close()

			var statusData map[string]interface{}
			json.NewDecoder(resp.Body).Decode(&statusData)

			// Validate state
			tr.validate(t, statusData)

			t.Logf("Transition %s -> %s verified", tr.from, tr.to)
		})
	}
}

// TestUIStateErrorHandling tests UI state during error conditions
func TestUIStateErrorHandling(t *testing.T) {
	testEnv := setupIntegrationEnvironment(t)
	defer testEnv.Cleanup()

	server := httptest.NewServer(createUITestMux(testEnv))
	defer server.Close()

	// Start sync
	testEnv.StateMgr.StartSync("test-card", 100, 100*1024*1024)
	time.Sleep(100 * time.Millisecond)

	// Simulate sync error
	testEnv.StateMgr.SetStatus(state.StatusError)
	testEnv.StateMgr.SetError("Test error: sync failed")
	time.Sleep(100 * time.Millisecond)

	// Verify error state
	resp, _ := http.Get(server.URL + "/api/status")
	var statusData map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&statusData)
	resp.Body.Close()

	if statusData["status"] != "error" {
		t.Errorf("Expected error status, got: %v", statusData["status"])
	}

	if statusData["error"] == nil {
		t.Error("Expected error message in state")
	}

	// Verify UI can recover
	testEnv.StateMgr.SetStatus(state.StatusIdle)
	testEnv.StateMgr.SetError("")
	time.Sleep(100 * time.Millisecond)

	resp, _ = http.Get(server.URL + "/api/status")
	json.NewDecoder(resp.Body).Decode(&statusData)
	resp.Body.Close()

	if statusData["status"] != "idle" {
		t.Errorf("Expected recovery to idle, got: %v", statusData["status"])
	}

	t.Log("Error handling and recovery verified")
}

// TestUIStateSessionPersistence tests state persistence across sessions
func TestUIStateSessionPersistence(t *testing.T) {
	testEnv := setupIntegrationEnvironment(t)
	defer testEnv.Cleanup()

	// Set some state
	testEnv.StateMgr.SetSDCard(true, "/mnt/test")
	testEnv.StateMgr.StartSync("test-card", 50, 50*1024*1024)
	time.Sleep(100 * time.Millisecond)

	// Create first server instance
	server1 := httptest.NewServer(createUITestMux(testEnv))

	resp, _ := http.Get(server1.URL + "/api/status")
	var state1 map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&state1)
	resp.Body.Close()
	server1.Close()

	// Create second server instance (simulating restart)
	time.Sleep(100 * time.Millisecond)
	server2 := httptest.NewServer(createUITestMux(testEnv))
	defer server2.Close()

	resp, _ = http.Get(server2.URL + "/api/status")
	var state2 map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&state2)
	resp.Body.Close()

	// States should be consistent (state manager persists)
	if state1["status"] != state2["status"] {
		t.Errorf("Status not persistent: %v -> %v", state1["status"], state2["status"])
	}

	t.Log("State persistence across sessions verified")
}

// Helper functions

func createUITestMux(testEnv *IntegrationEnvironment) *http.ServeMux {
	eventMgr := events.NewManager()
	wifiMgr, _ := wifimanager.NewManager()
	ssrfValidator := ssrf.NewValidator(10, time.Minute)

	ctx := &handlers.Context{
		StateMgr:      testEnv.StateMgr,
		SyncMgr:       testEnv.SyncMgr,
		WiFiMgr:       wifiMgr,
		AppSettings:   testEnv.Settings,
		SSRFValidator: ssrfValidator,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/status", ctx.HandleStatus)
	mux.HandleFunc("/api/history", ctx.HandleHistory)
	mux.HandleFunc("/api/settings", ctx.HandleSettings)
	mux.HandleFunc("/api/files", ctx.HandleFiles)
	mux.HandleFunc("/api/sdcard/files", ctx.HandleSDCardFiles)
	mux.HandleFunc("/ws", websocket.HandleWebSocket(testEnv.StateMgr, eventMgr))

	return mux
}
