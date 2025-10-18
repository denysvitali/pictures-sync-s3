package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/denysvitali/pictures-sync-s3/pkg/daemon"
	"github.com/denysvitali/pictures-sync-s3/pkg/events"
	"github.com/denysvitali/pictures-sync-s3/pkg/handlers"
	"github.com/denysvitali/pictures-sync-s3/pkg/sdmonitor"
	"github.com/denysvitali/pictures-sync-s3/pkg/settings"
	"github.com/denysvitali/pictures-sync-s3/pkg/ssrf"
	"github.com/denysvitali/pictures-sync-s3/pkg/state"
	"github.com/denysvitali/pictures-sync-s3/pkg/syncmanager"
	"github.com/denysvitali/pictures-sync-s3/pkg/websocket"
	"github.com/denysvitali/pictures-sync-s3/pkg/wifimanager"
)

// TestE2EServiceIntegration tests integration between webui and pictures-sync services
func TestE2EServiceIntegration(t *testing.T) {
	// Skip if not in integration test mode
	if os.Getenv("E2E_INTEGRATION") == "" {
		t.Skip("Skipping integration test. Set E2E_INTEGRATION=1 to run")
	}

	testEnv := setupIntegrationEnvironment(t)
	defer testEnv.Cleanup()

	// Start the daemon service in background
	daemonCtx, daemonCancel := context.WithCancel(context.Background())
	defer daemonCancel()

	daemonReady := make(chan bool)
	daemonErrors := make(chan error, 1)

	go func() {
		cfg := daemon.DefaultConfig()
		cfg.NTPSyncDelay = 1 * time.Second // Speed up for testing

		svc, err := daemon.New(cfg)
		if err != nil {
			daemonErrors <- err
			return
		}
		defer svc.Shutdown()

		daemonReady <- true

		// Run until context cancelled
		go func() {
			<-daemonCtx.Done()
			svc.Shutdown()
		}()

		if err := svc.Run(); err != nil {
			daemonErrors <- err
		}
	}()

	// Wait for daemon to be ready or error
	select {
	case <-daemonReady:
		t.Log("Daemon service started")
	case err := <-daemonErrors:
		t.Fatalf("Daemon failed to start: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("Daemon did not start within 10 seconds")
	}

	// Start the webui service
	webuiServer := testEnv.StartWebUIServer()
	defer webuiServer.Close()

	t.Logf("WebUI server started at %s", webuiServer.URL)

	// Test 1: Verify status endpoint returns correct initial state
	resp, err := http.Get(webuiServer.URL + "/api/status")
	if err != nil {
		t.Fatalf("Failed to call status endpoint: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Status endpoint returned %d, want 200", resp.StatusCode)
	}

	var statusData map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&statusData); err != nil {
		t.Fatalf("Failed to decode status response: %v", err)
	}

	if statusData["status"] != "idle" {
		t.Errorf("Initial status = %v, want idle", statusData["status"])
	}

	// Test 2: Simulate SD card insertion through daemon
	mockCard := testEnv.CreateMockSDCard("integration-card", 25)
	if err := testEnv.MountMockCard(mockCard); err != nil {
		t.Fatalf("Failed to mount mock card: %v", err)
	}

	// Trigger SD card detection by creating device in monitored location
	// The daemon's SD monitor should detect it automatically
	time.Sleep(3 * time.Second) // Give monitor time to detect

	// Test 3: Verify WebUI reflects the state change
	resp, err = http.Get(webuiServer.URL + "/api/status")
	if err != nil {
		t.Fatalf("Failed to call status endpoint: %v", err)
	}
	defer resp.Body.Close()

	if err := json.NewDecoder(resp.Body).Decode(&statusData); err != nil {
		t.Fatalf("Failed to decode status response: %v", err)
	}

	// Status should have changed from idle
	status := statusData["status"].(string)
	if status != "detected" && status != "syncing" && status != "success" {
		t.Errorf("Status after card insertion = %v, want detected/syncing/success", status)
	}

	// Test 4: Wait for sync to complete and verify history
	waitForSyncCompletionViaAPI(t, webuiServer.URL, 30*time.Second)

	resp, err = http.Get(webuiServer.URL + "/api/history")
	if err != nil {
		t.Fatalf("Failed to call history endpoint: %v", err)
	}
	defer resp.Body.Close()

	var history []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&history); err != nil {
		t.Fatalf("Failed to decode history response: %v", err)
	}

	if len(history) == 0 {
		t.Fatal("No sync history after card insertion")
	}

	lastSync := history[len(history)-1]
	if lastSync["card_id"] != mockCard.CardID {
		t.Errorf("Sync card_id = %v, want %s", lastSync["card_id"], mockCard.CardID)
	}

	t.Log("Service integration test completed successfully")
}

// TestE2EWebUIStateReflection tests that WebUI accurately reflects daemon state
func TestE2EWebUIStateReflection(t *testing.T) {
	testEnv := setupIntegrationEnvironment(t)
	defer testEnv.Cleanup()

	// Initialize shared state manager
	stateMgr, err := state.NewManager()
	if err != nil {
		t.Fatalf("Failed to create state manager: %v", err)
	}

	// Start WebUI with shared state
	webuiServer := testEnv.StartWebUIServerWithState(stateMgr)
	defer webuiServer.Close()

	// Test various state transitions
	stateTests := []struct {
		name          string
		setState      func()
		expectedField string
		expectedValue interface{}
	}{
		{
			name: "Detected status",
			setState: func() {
				stateMgr.SetStatus(state.StatusDetected)
			},
			expectedField: "status",
			expectedValue: "detected",
		},
		{
			name: "SD card mounted",
			setState: func() {
				stateMgr.SetSDCard(true, "/mnt/test")
			},
			expectedField: "sdcard_mounted",
			expectedValue: true,
		},
		{
			name: "Syncing status",
			setState: func() {
				stateMgr.StartSync("test-card", 50, 50*1024*1024)
			},
			expectedField: "status",
			expectedValue: "syncing",
		},
	}

	for _, tt := range stateTests {
		t.Run(tt.name, func(t *testing.T) {
			// Apply state change
			tt.setState()

			// Give state time to propagate
			time.Sleep(100 * time.Millisecond)

			// Query WebUI status
			resp, err := http.Get(webuiServer.URL + "/api/status")
			if err != nil {
				t.Fatalf("Failed to call status endpoint: %v", err)
			}
			defer resp.Body.Close()

			var statusData map[string]interface{}
			if err := json.NewDecoder(resp.Body).Decode(&statusData); err != nil {
				t.Fatalf("Failed to decode status: %v", err)
			}

			// Verify expected field value
			actualValue := statusData[tt.expectedField]
			if actualValue != tt.expectedValue {
				t.Errorf("%s: %s = %v, want %v",
					tt.name, tt.expectedField, actualValue, tt.expectedValue)
			}
		})
	}
}

// TestE2EConcurrentAPIRequests tests concurrent requests to WebUI don't cause issues
func TestE2EConcurrentAPIRequests(t *testing.T) {
	testEnv := setupIntegrationEnvironment(t)
	defer testEnv.Cleanup()

	stateMgr, _ := state.NewManager()
	webuiServer := testEnv.StartWebUIServerWithState(stateMgr)
	defer webuiServer.Close()

	// Simulate concurrent clients polling status
	const numClients = 20
	const requestsPerClient = 10

	errors := make(chan error, numClients*requestsPerClient)
	done := make(chan bool, numClients)

	for i := 0; i < numClients; i++ {
		go func(clientID int) {
			for j := 0; j < requestsPerClient; j++ {
				resp, err := http.Get(webuiServer.URL + "/api/status")
				if err != nil {
					errors <- fmt.Errorf("client %d request %d: %v", clientID, j, err)
					continue
				}

				if resp.StatusCode != http.StatusOK {
					errors <- fmt.Errorf("client %d request %d: status %d", clientID, j, resp.StatusCode)
				}
				resp.Body.Close()

				// Small delay between requests
				time.Sleep(10 * time.Millisecond)
			}
			done <- true
		}(i)
	}

	// Wait for all clients to complete
	for i := 0; i < numClients; i++ {
		<-done
	}

	close(errors)

	// Check for any errors
	errorCount := 0
	for err := range errors {
		t.Errorf("Concurrent request error: %v", err)
		errorCount++
	}

	if errorCount > 0 {
		t.Errorf("Total errors from concurrent requests: %d", errorCount)
	} else {
		t.Log("All concurrent requests succeeded")
	}
}

// TestE2ESettingsPersistence tests settings changes persist across service restarts
func TestE2ESettingsPersistence(t *testing.T) {
	testEnv := setupIntegrationEnvironment(t)
	defer testEnv.Cleanup()

	// First instance - set settings
	stateMgr1, _ := state.NewManager()
	server1 := testEnv.StartWebUIServerWithState(stateMgr1)

	// Get initial settings
	resp, err := http.Get(server1.URL + "/api/settings")
	if err != nil {
		t.Fatalf("Failed to get settings: %v", err)
	}
	defer resp.Body.Close()

	var initialSettings map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&initialSettings)

	// Modify a setting
	appSettings, _ := settings.Load()
	appSettings.SetRemoteName("test-remote-persist")
	appSettings.SetRemotePath("/test/persist/path")
	appSettings.SetReformatThreshold(0.25)

	// Stop first server
	server1.Close()

	// Start second instance - should load persisted settings
	stateMgr2, _ := state.NewManager()
	server2 := testEnv.StartWebUIServerWithState(stateMgr2)
	defer server2.Close()

	// Get settings from new instance
	resp, err = http.Get(server2.URL + "/api/settings")
	if err != nil {
		t.Fatalf("Failed to get settings from new instance: %v", err)
	}
	defer resp.Body.Close()

	var persistedSettings map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&persistedSettings)

	// Verify settings persisted
	if persistedSettings["remote_name"] != "test-remote-persist" {
		t.Errorf("remote_name not persisted: got %v", persistedSettings["remote_name"])
	}
	if persistedSettings["remote_path"] != "/test/persist/path" {
		t.Errorf("remote_path not persisted: got %v", persistedSettings["remote_path"])
	}

	t.Log("Settings persistence verified")
}

// IntegrationEnvironment holds integration test resources
type IntegrationEnvironment struct {
	*TestEnvironment
	StateMgr *state.Manager
	SyncMgr  *syncmanager.Manager
	Settings *settings.Settings
}

// setupIntegrationEnvironment creates an integration test environment
func setupIntegrationEnvironment(t *testing.T) *IntegrationEnvironment {
	baseEnv := setupTestEnvironment(t)

	stateMgr, err := state.NewManager()
	if err != nil {
		t.Fatalf("Failed to create state manager: %v", err)
	}

	appSettings, err := settings.Load()
	if err != nil {
		t.Fatalf("Failed to load settings: %v", err)
	}

	appSettings.SetRemoteName("test-remote")
	appSettings.SetRemotePath(filepath.Join(baseEnv.BaseDir, "remote"))

	syncMgr := syncmanager.NewManager(
		baseEnv.RcloneConfigPath,
		appSettings.GetRemoteName(),
		appSettings.GetRemotePath(),
		stateMgr,
		appSettings.GetTransfers(),
		appSettings.GetCheckers(),
	)

	return &IntegrationEnvironment{
		TestEnvironment: baseEnv,
		StateMgr:        stateMgr,
		SyncMgr:         syncMgr,
		Settings:        appSettings,
	}
}

// StartWebUIServer starts a test WebUI server
func (env *IntegrationEnvironment) StartWebUIServer() *httptest.Server {
	return env.StartWebUIServerWithState(env.StateMgr)
}

// StartWebUIServerWithState starts a WebUI server with custom state manager
func (env *IntegrationEnvironment) StartWebUIServerWithState(stateMgr *state.Manager) *httptest.Server {
	eventMgr := events.NewManager()

	syncMgr := syncmanager.NewManager(
		env.RcloneConfigPath,
		env.Settings.GetRemoteName(),
		env.Settings.GetRemotePath(),
		stateMgr,
		env.Settings.GetTransfers(),
		env.Settings.GetCheckers(),
	)

	wifiMgr, _ := wifimanager.NewManager()
	ssrfValidator := ssrf.NewValidator(10, time.Minute)

	ctx := &handlers.Context{
		StateMgr:      stateMgr,
		SyncMgr:       syncMgr,
		WiFiMgr:       wifiMgr,
		AppSettings:   env.Settings,
		SSRFValidator: ssrfValidator,
	}

	// Setup routes
	mux := http.NewServeMux()
	mux.HandleFunc("/api/status", ctx.HandleStatus)
	mux.HandleFunc("/api/history", ctx.HandleHistory)
	mux.HandleFunc("/api/settings", ctx.HandleSettings)
	mux.HandleFunc("/api/config", ctx.HandleConfig)
	mux.HandleFunc("/api/devices", ctx.HandleDevices)
	mux.HandleFunc("/api/sync/start", ctx.HandleSyncStart)
	mux.HandleFunc("/api/sync/cancel", ctx.HandleSyncCancel)
	mux.HandleFunc("/ws", websocket.HandleWebSocket(stateMgr, eventMgr))

	return httptest.NewServer(mux)
}

// waitForSyncCompletionViaAPI waits for sync to complete by polling API
func waitForSyncCompletionViaAPI(t *testing.T, baseURL string, timeout time.Duration) {
	deadline := time.After(timeout)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-deadline:
			t.Fatal("Sync did not complete within timeout (via API)")
		case <-ticker.C:
			resp, err := http.Get(baseURL + "/api/status")
			if err != nil {
				continue
			}

			var statusData map[string]interface{}
			if err := json.NewDecoder(resp.Body).Decode(&statusData); err != nil {
				resp.Body.Close()
				continue
			}
			resp.Body.Close()

			status := statusData["status"].(string)
			if status == "success" || status == "error" || status == "idle" {
				return
			}
		}
	}
}

// TestE2EDaemonStartupSequence tests the daemon initialization sequence
func TestE2EDaemonStartupSequence(t *testing.T) {
	if os.Getenv("E2E_INTEGRATION") == "" {
		t.Skip("Skipping integration test. Set E2E_INTEGRATION=1 to run")
	}

	testEnv := setupIntegrationEnvironment(t)
	defer testEnv.Cleanup()

	// Track initialization events
	initSteps := make([]string, 0)
	stepsMutex := make(chan struct{}, 1)
	stepsMutex <- struct{}{} // Initialize

	eventMgr := events.NewManager()
	eventChan := eventMgr.Subscribe()
	defer eventMgr.Unsubscribe(eventChan)

	go func() {
		for event := range eventChan {
			<-stepsMutex
			initSteps = append(initSteps, event.Type.String())
			stepsMutex <- struct{}{}
		}
	}()

	// Create daemon with fast config for testing
	cfg := daemon.DefaultConfig()
	cfg.NTPSyncDelay = 500 * time.Millisecond

	svc, err := daemon.New(cfg)
	if err != nil {
		t.Fatalf("Failed to create daemon: %v", err)
	}
	defer svc.Shutdown()

	// Daemon should initialize without error
	t.Log("Daemon initialized successfully")

	// Start daemon in background
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- svc.Run()
	}()

	// Let it run briefly
	time.Sleep(2 * time.Second)
	cancel()

	// Wait for shutdown
	select {
	case err := <-done:
		if err != nil && err != context.Canceled {
			t.Errorf("Daemon error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Error("Daemon did not shut down cleanly")
	}

	t.Log("Daemon startup and shutdown completed successfully")
}

// TestE2ECardDetectionLatency measures SD card detection latency
func TestE2ECardDetectionLatency(t *testing.T) {
	testEnv := setupIntegrationEnvironment(t)
	defer testEnv.Cleanup()

	monitor := sdmonitor.NewMonitor(testEnv.MountDir)
	eventChan := monitor.Events()

	if err := monitor.Start(); err != nil {
		t.Fatalf("Failed to start monitor: %v", err)
	}
	defer monitor.Stop()

	mockCard := testEnv.CreateMockSDCard("latency-test", 10)

	// Measure detection time
	startTime := time.Now()

	if err := testEnv.MountMockCard(mockCard); err != nil {
		t.Fatalf("Failed to mount mock card: %v", err)
	}

	// Wait for detection event
	timeout := time.After(10 * time.Second)
	detected := false

	for !detected {
		select {
		case event := <-eventChan:
			if event.Type == sdmonitor.EventInserted {
				detected = true
			}
		case <-timeout:
			t.Fatal("Card not detected within 10 seconds")
		}
	}

	detectionLatency := time.Since(startTime)
	t.Logf("Card detection latency: %v", detectionLatency)

	// Detection should be reasonably fast (under 5 seconds)
	if detectionLatency > 5*time.Second {
		t.Errorf("Detection latency %v exceeds 5 seconds", detectionLatency)
	}
}
