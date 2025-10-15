package state

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"sync"
	"testing"
	"time"
)

// TestIntegrationFullSyncLifecycle tests a complete sync lifecycle
func TestIntegrationFullSyncLifecycle(t *testing.T) {
	// Skip this test if running in an environment without write access to /perm
	if _, err := os.Stat("/perm"); os.IsNotExist(err) {
		t.Skip("Skipping integration test: /perm directory does not exist")
	}

	// Create manager
	manager, err := NewManager()
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	// Subscribe to state changes
	stateChannel := manager.Subscribe()
	defer manager.Unsubscribe(stateChannel)

	// Collect all state transitions
	var stateTransitions []SyncStatus
	var transitionsMutex sync.Mutex

	go func() {
		for state := range stateChannel {
			transitionsMutex.Lock()
			stateTransitions = append(stateTransitions, state.Status)
			transitionsMutex.Unlock()
		}
	}()

	// Simulate SD card detection
	err = manager.SetStatus(StatusDetected)
	if err != nil {
		t.Errorf("Failed to set detected status: %v", err)
	}

	// Simulate SD card mount
	err = manager.SetSDCard(true, "/mnt/test-card")
	if err != nil {
		t.Errorf("Failed to set SD card: %v", err)
	}

	// Start sync
	cardID := "integration-card-001"
	totalFiles := int64(250)
	totalBytes := int64(500 * 1024 * 1024) // 500MB

	record, err := manager.StartSync(cardID, totalFiles, totalBytes)
	if err != nil {
		t.Fatalf("Failed to start sync: %v", err)
	}

	// Simulate progressive sync updates
	progressSteps := []struct {
		files    int64
		bytes    int64
		file     string
		fileSize int64
		speed    float64
		eta      string
	}{
		{25, 50 * 1024 * 1024, "IMG_0001.jpg", 2 * 1024 * 1024, 1 * 1024 * 1024, "8m 20s"},
		{50, 100 * 1024 * 1024, "IMG_0050.jpg", 2 * 1024 * 1024, 1.5 * 1024 * 1024, "5m 33s"},
		{100, 200 * 1024 * 1024, "IMG_0100.jpg", 2 * 1024 * 1024, 2 * 1024 * 1024, "2m 30s"},
		{150, 300 * 1024 * 1024, "IMG_0150.jpg", 2 * 1024 * 1024, 2 * 1024 * 1024, "1m 40s"},
		{200, 400 * 1024 * 1024, "IMG_0200.jpg", 2 * 1024 * 1024, 2.5 * 1024 * 1024, "40s"},
		{250, 500 * 1024 * 1024, "IMG_0250.jpg", 2 * 1024 * 1024, 3 * 1024 * 1024, "0s"},
	}

	for _, step := range progressSteps {
		err = manager.UpdateSyncProgress(step.files, step.bytes, step.file, step.fileSize, step.speed, step.eta)
		if err != nil {
			t.Errorf("Failed to update progress: %v", err)
		}
		time.Sleep(10 * time.Millisecond) // Small delay to simulate real sync
	}

	// Verify sync record has correct data
	if record.CardID != cardID {
		t.Errorf("Record CardID = %v, want %v", record.CardID, cardID)
	}

	// Finish sync successfully
	err = manager.FinishSync(true, nil)
	if err != nil {
		t.Errorf("Failed to finish sync: %v", err)
	}

	// Wait for state transitions to propagate
	time.Sleep(100 * time.Millisecond)

	// Verify final state
	finalState := manager.GetState()
	if finalState.Status != StatusSuccess {
		t.Errorf("Final status = %v, want %v", finalState.Status, StatusSuccess)
	}

	// Verify history was saved
	history := manager.GetHistory()
	if len(history) != 1 {
		t.Fatalf("Expected 1 sync in history, got %d", len(history))
	}

	// Verify history file exists and is valid JSON
	historyData, err := ioutil.ReadFile(HistoryFile)
	if err != nil {
		t.Errorf("Failed to read history file: %v", err)
	}

	var savedHistory []SyncRecord
	err = json.Unmarshal(historyData, &savedHistory)
	if err != nil {
		t.Errorf("Failed to parse history JSON: %v", err)
	}

	if len(savedHistory) != 1 {
		t.Errorf("Saved history length = %d, want 1", len(savedHistory))
	}

	// Verify state file exists and is valid
	stateData, err := ioutil.ReadFile(StateFile)
	if err != nil {
		t.Errorf("Failed to read state file: %v", err)
	}

	var savedState CurrentState
	err = json.Unmarshal(stateData, &savedState)
	if err != nil {
		t.Errorf("Failed to parse state JSON: %v", err)
	}

	// Verify state transitions occurred in expected order
	transitionsMutex.Lock()
	expectedTransitions := []SyncStatus{StatusDetected, StatusSyncing, StatusSuccess}
	foundTransitions := make(map[SyncStatus]bool)
	for _, status := range stateTransitions {
		foundTransitions[status] = true
	}
	transitionsMutex.Unlock()

	for _, expected := range expectedTransitions {
		if !foundTransitions[expected] {
			t.Errorf("Missing expected state transition: %v", expected)
		}
	}
}

// TestIntegrationMultipleCards tests managing multiple card syncs
func TestIntegrationMultipleCards(t *testing.T) {
	// Skip this test if running in an environment without write access to /perm
	if _, err := os.Stat("/perm"); os.IsNotExist(err) {
		t.Skip("Skipping integration test: /perm directory does not exist")
	}

	manager, err := NewManager()
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	// Simulate multiple cards being synced
	cards := []struct {
		id         string
		files      int64
		bytes      int64
		shouldFail bool
		error      error
	}{
		{"card-alpha", 100, 100 * 1024 * 1024, false, nil},
		{"card-beta", 200, 200 * 1024 * 1024, false, nil},
		{"card-gamma", 150, 150 * 1024 * 1024, true, fmt.Errorf("network timeout")},
		{"card-alpha", 120, 120 * 1024 * 1024, false, nil}, // Same card, more files
		{"card-delta", 80, 80 * 1024 * 1024, false, nil},
	}

	for i, card := range cards {
		// Simulate card insertion
		manager.SetStatus(StatusDetected)
		manager.SetSDCard(true, fmt.Sprintf("/mnt/card-%d", i))

		// Start sync
		_, err := manager.StartSync(card.id, card.files, card.bytes)
		if err != nil {
			t.Errorf("Card %s: Failed to start sync: %v", card.id, err)
			continue
		}

		// Simulate progress
		for j := int64(0); j <= 10; j++ {
			progress := (j * card.files) / 10
			bytes := (j * card.bytes) / 10
			manager.UpdateSyncProgress(progress, bytes, fmt.Sprintf("file_%d.jpg", progress), 1024*1024, 1024*1024, "1m")
			time.Sleep(5 * time.Millisecond)
		}

		// Finish sync
		if card.shouldFail {
			manager.FinishSync(false, card.error)
		} else {
			manager.FinishSync(true, nil)
		}

		// Reset for next card
		manager.SetSDCard(false, "")
		manager.SetStatus(StatusIdle)
		time.Sleep(10 * time.Millisecond)
	}

	// Verify history
	history := manager.GetHistory()
	if len(history) != len(cards) {
		t.Errorf("History length = %d, want %d", len(history), len(cards))
	}

	// Count syncs per card
	cardSyncCount := make(map[string]int)
	for _, record := range history {
		cardSyncCount[record.CardID]++
	}

	// Verify card-alpha has 2 syncs
	if cardSyncCount["card-alpha"] != 2 {
		t.Errorf("card-alpha sync count = %d, want 2", cardSyncCount["card-alpha"])
	}

	// Find last sync for card-alpha
	lastAlphaSync := manager.FindLastSyncByCardID("card-alpha")
	if lastAlphaSync == nil {
		t.Error("Should find last sync for card-alpha")
	} else if lastAlphaSync.FilesTotal != 120 {
		t.Errorf("Last card-alpha sync files = %d, want 120", lastAlphaSync.FilesTotal)
	}

	// Verify failed sync
	gammaSync := manager.FindLastSyncByCardID("card-gamma")
	if gammaSync == nil {
		t.Error("Should find sync for card-gamma")
	} else {
		if gammaSync.Status != "error" {
			t.Errorf("card-gamma status = %s, want error", gammaSync.Status)
		}
		if gammaSync.Error != "network timeout" {
			t.Errorf("card-gamma error = %s, want 'network timeout'", gammaSync.Error)
		}
	}
}

// TestIntegrationPersistenceAcrossRestarts tests state persistence
func TestIntegrationPersistenceAcrossRestarts(t *testing.T) {
	// Skip this test if running in an environment without write access to /perm
	if _, err := os.Stat("/perm"); os.IsNotExist(err) {
		t.Skip("Skipping integration test: /perm directory does not exist")
	}

	// === First "session" ===
	manager1, err := NewManager()
	if err != nil {
		t.Fatalf("Failed to create manager1: %v", err)
	}

	// Perform some operations
	manager1.SetStatus(StatusDetected)
	manager1.SetSDCard(true, "/mnt/persistent-card")

	// Add devices
	devices := []DeviceInfo{
		{DevicePath: "/dev/sda1", DeviceName: "USB1", Size: 32 * 1024 * 1024 * 1024, IsUSB: true},
		{DevicePath: "/dev/sdb1", DeviceName: "USB2", Size: 64 * 1024 * 1024 * 1024, IsUSB: true},
	}
	manager1.SetAvailableDevices(devices)

	// Do a sync
	manager1.StartSync("persist-card-1", 50, 50*1024*1024)
	manager1.UpdateSyncProgress(25, 25*1024*1024, "halfway.jpg", 1024*1024, 1024*1024, "25s")
	manager1.FinishSync(true, nil)

	// Do another sync
	manager1.StartSync("persist-card-2", 75, 75*1024*1024)
	manager1.UpdateSyncProgress(75, 75*1024*1024, "complete.jpg", 1024*1024, 2*1024*1024, "0s")
	manager1.FinishSync(true, nil)

	// Capture state before "restart"
	state1 := manager1.GetState()
	history1 := manager1.GetHistory()

	// === Simulate restart - create new manager ===
	manager2, err := NewManager()
	if err != nil {
		t.Fatalf("Failed to create manager2: %v", err)
	}

	// Get restored state
	state2 := manager2.GetState()
	history2 := manager2.GetHistory()

	// Verify state was restored
	if state2.Status != state1.Status {
		t.Errorf("Status not restored: got %v, want %v", state2.Status, state1.Status)
	}

	if state2.SDCardMounted != state1.SDCardMounted {
		t.Errorf("SDCardMounted not restored: got %v, want %v", state2.SDCardMounted, state1.SDCardMounted)
	}

	if state2.SDCardPath != state1.SDCardPath {
		t.Errorf("SDCardPath not restored: got %v, want %v", state2.SDCardPath, state1.SDCardPath)
	}

	// Note: Available devices are not persisted by default, so we don't check them

	// Verify history was restored
	if len(history2) != len(history1) {
		t.Errorf("History length mismatch: got %d, want %d", len(history2), len(history1))
	}

	if len(history2) >= 2 {
		// Check both syncs are present
		if history2[0].CardID != "persist-card-1" {
			t.Errorf("First history entry CardID = %v, want persist-card-1", history2[0].CardID)
		}
		if history2[1].CardID != "persist-card-2" {
			t.Errorf("Second history entry CardID = %v, want persist-card-2", history2[1].CardID)
		}
	}

	// === Third session - test Reload functionality ===
	// Note: This test would need Reload() functionality to be implemented
}

// TestIntegrationConcurrentSubscribers tests multiple concurrent subscribers
func TestIntegrationConcurrentSubscribers(t *testing.T) {
	// Skip this test if running in an environment without write access to /perm
	if _, err := os.Stat("/perm"); os.IsNotExist(err) {
		t.Skip("Skipping integration test: /perm directory does not exist")
	}

	manager, err := NewManager()
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	// Create multiple subscribers
	numSubscribers := 10
	subscribers := make([]chan CurrentState, numSubscribers)
	receivedCounts := make([]int, numSubscribers)
	var countMutex sync.Mutex
	var wg sync.WaitGroup

	for i := 0; i < numSubscribers; i++ {
		subscribers[i] = manager.Subscribe()
		wg.Add(1)

		go func(index int, ch chan CurrentState) {
			defer wg.Done()
			timeout := time.After(5 * time.Second)

			for {
				select {
				case state, ok := <-ch:
					if !ok {
						return
					}
					countMutex.Lock()
					receivedCounts[index]++
					countMutex.Unlock()

					// Log interesting transitions
					if state.Status == StatusSyncing && state.CurrentSync != nil {
						t.Logf("Subscriber %d: Sync progress %d/%d files",
							index, state.CurrentSync.FilesSynced, state.CurrentSync.FilesTotal)
					}
				case <-timeout:
					return
				}
			}
		}(i, subscribers[i])
	}

	// Perform a complete sync cycle
	manager.SetStatus(StatusDetected)
	time.Sleep(10 * time.Millisecond)

	manager.SetSDCard(true, "/mnt/multi-subscriber-test")
	time.Sleep(10 * time.Millisecond)

	manager.StartSync("subscriber-test", 100, 100*1024*1024)

	// Multiple progress updates
	for i := 0; i <= 10; i++ {
		manager.UpdateSyncProgress(int64(i*10), int64(i*10*1024*1024),
			fmt.Sprintf("file_%d.jpg", i*10), 1024*1024, 1024*1024, fmt.Sprintf("%ds", 100-i*10))
		time.Sleep(20 * time.Millisecond)
	}

	manager.FinishSync(true, nil)
	time.Sleep(50 * time.Millisecond)

	// Unsubscribe all
	for _, ch := range subscribers {
		manager.Unsubscribe(ch)
	}

	wg.Wait()

	// Verify all subscribers received updates
	countMutex.Lock()
	defer countMutex.Unlock()

	for i, count := range receivedCounts {
		if count < 5 { // Should have received at least 5 updates
			t.Errorf("Subscriber %d only received %d updates", i, count)
		}
		t.Logf("Subscriber %d received %d updates", i, count)
	}
}

// TestIntegrationRcloneConfig tests rclone config management
func TestIntegrationRcloneConfig(t *testing.T) {
	// Skip this test since ConfigFile is a constant and can't be overridden
	t.Skip("Skipping test that requires modifying constants")

	// Test when config doesn't exist
	exists, err := EnsureRcloneConfig()
	if err != nil {
		t.Errorf("Error checking non-existent config: %v", err)
	}
	if exists {
		t.Error("Config should not exist initially")
	}

	// Create a mock rclone config
	configContent := `[myremote]
type = s3
provider = AWS
env_auth = false
access_key_id = test_key
secret_access_key = test_secret
region = us-east-1
endpoint = https://s3.amazonaws.com
`
	err = ioutil.WriteFile(ConfigFile, []byte(configContent), 0600)
	if err != nil {
		t.Fatalf("Failed to create test config: %v", err)
	}

	// Test when config exists
	exists, err = EnsureRcloneConfig()
	if err != nil {
		t.Errorf("Error checking existing config: %v", err)
	}
	if !exists {
		t.Error("Config should exist after creation")
	}

	// Verify config path
	path := GetRcloneConfigPath()
	if path != ConfigFile {
		t.Errorf("Config path = %s, want %s", path, ConfigFile)
	}

	// Verify config permissions (should be readable by owner only)
	info, err := os.Stat(ConfigFile)
	if err != nil {
		t.Errorf("Failed to stat config: %v", err)
	}

	mode := info.Mode().Perm()
	if mode != 0600 {
		t.Logf("Warning: Config permissions are %o, expected 0600 for security", mode)
	}
}

// TestEmptyTotalsScenario tests what happens if totals are 0
func TestEmptyTotalsScenario(t *testing.T) {
	// Test what happens if totals are 0 (this might be the bug!)
	record := &SyncRecord{
		FilesTotal:  0, // This could happen if CountPhotos returns 0
		FilesSynced: 0,
		BytesTotal:  0,
		BytesSynced: 0,
		Status:      "syncing",
	}

	state := CurrentState{
		Status:      StatusSyncing,
		CurrentSync: record,
	}

	jsonData, _ := json.Marshal(state)
	t.Logf("State with zero totals: %s", string(jsonData))

	// Check if this breaks the JavaScript progress calculation
	var wsData map[string]interface{}
	json.Unmarshal(jsonData, &wsData)

	if cs, ok := wsData["current_sync"].(map[string]interface{}); ok {
		filesTotal := cs["files_total"].(float64)
		bytesTotal := cs["bytes_total"].(float64)

		if filesTotal == 0 {
			t.Log("WARNING: files_total is 0 - this will cause progress bar to not show!")
			t.Log("This is likely THE BUG causing sync progress not to display")
		}
		if bytesTotal == 0 {
			t.Log("WARNING: bytes_total is 0 - this will cause progress bar to not show!")
		}
	}
}

// TestJavaScriptConditions tests conditions that JavaScript checks
func TestJavaScriptConditions(t *testing.T) {
	// Test various conditions that the JavaScript checks
	testCases := []struct {
		name         string
		status       string
		hasSync      bool
		filesTotal   int64
		shouldShow   bool
		reason       string
	}{
		{
			name:       "Normal syncing",
			status:     "syncing",
			hasSync:    true,
			filesTotal: 100,
			shouldShow: true,
			reason:     "All conditions met",
		},
		{
			name:       "Status not syncing",
			status:     "idle",
			hasSync:    true,
			filesTotal: 100,
			shouldShow: false,
			reason:     "status !== 'syncing'",
		},
		{
			name:       "No current_sync",
			status:     "syncing",
			hasSync:    false,
			filesTotal: 0,
			shouldShow: false,
			reason:     "current_sync is null",
		},
		{
			name:       "Zero files total",
			status:     "syncing",
			hasSync:    true,
			filesTotal: 0,
			shouldShow: true, // It would pass the JS check but show 0% progress
			reason:     "Would show but with 0% progress",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			state := CurrentState{
				Status: SyncStatus(tc.status),
			}

			if tc.hasSync {
				state.CurrentSync = &SyncRecord{
					FilesTotal:  tc.filesTotal,
					FilesSynced: 10,
					BytesTotal:  1024 * 1024 * 50,
					BytesSynced: 1024 * 1024 * 5,
					Status:      "syncing",
				}
			}

			jsonData, _ := json.Marshal(state)
			var wsData map[string]interface{}
			json.Unmarshal(jsonData, &wsData)

			// Simulate JavaScript: if (data.current_sync && data.status === 'syncing')
			status, _ := wsData["status"].(string)
			currentSync, hasCurrentSync := wsData["current_sync"].(map[string]interface{})

			wouldShow := hasCurrentSync && currentSync != nil && status == "syncing"

			if wouldShow != tc.shouldShow {
				t.Errorf("Progress display = %v, want %v (reason: %s)", wouldShow, tc.shouldShow, tc.reason)
			}

			t.Logf("Test case '%s': %s", tc.name, tc.reason)
			if tc.filesTotal == 0 && wouldShow {
				t.Logf("⚠️  This would show progress section but with 0 total files!")
			}
		})
	}
}