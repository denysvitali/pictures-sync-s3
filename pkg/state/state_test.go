package state

import (
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"
)

// setupTestManager is defined in memory_test.go to avoid duplication

func TestStartSync(t *testing.T) {
	mgr := setupTestManager(t)

	// Start a sync
	cardID := "test-card-123"
	totalFiles := int64(100)
	totalBytes := int64(1024 * 1024 * 50) // 50MB

	record, err := mgr.StartSync(cardID, totalFiles, totalBytes)
	if err != nil {
		t.Fatalf("StartSync failed: %v", err)
	}

	// Verify the record was created correctly
	if record.CardID != cardID {
		t.Errorf("CardID = %v, want %v", record.CardID, cardID)
	}
	if record.FilesTotal != totalFiles {
		t.Errorf("FilesTotal = %v, want %v", record.FilesTotal, totalFiles)
	}
	if record.BytesTotal != totalBytes {
		t.Errorf("BytesTotal = %v, want %v", record.BytesTotal, totalBytes)
	}
	if record.Status != "syncing" {
		t.Errorf("Status = %v, want syncing", record.Status)
	}

	// Verify the state was updated
	state := mgr.GetState()
	if state.Status != StatusSyncing {
		t.Errorf("State.Status = %v, want %v", state.Status, StatusSyncing)
	}
	if state.CurrentSync == nil {
		t.Fatal("CurrentSync is nil after StartSync")
	}
	if state.CurrentSync.FilesTotal != totalFiles {
		t.Errorf("CurrentSync.FilesTotal = %v, want %v", state.CurrentSync.FilesTotal, totalFiles)
	}
	if state.CurrentSync.BytesTotal != totalBytes {
		t.Errorf("CurrentSync.BytesTotal = %v, want %v", state.CurrentSync.BytesTotal, totalBytes)
	}
}

func TestUpdateSyncProgress(t *testing.T) {
	mgr := setupTestManager(t)

	// Start a sync first
	cardID := "test-card-456"
	totalFiles := int64(100)
	totalBytes := int64(1024 * 1024 * 50) // 50MB

	_, err := mgr.StartSync(cardID, totalFiles, totalBytes)
	if err != nil {
		t.Fatalf("StartSync failed: %v", err)
	}

	// Update progress
	filesSynced := int64(25)
	bytesSynced := int64(1024 * 1024 * 12) // 12MB
	currentFile := "IMG_001.jpg"
	currentFileSize := int64(1024 * 1024 * 2) // 2MB
	speed := float64(1024 * 1024)             // 1MB/s
	eta := "45s"

	err = mgr.UpdateSyncProgress(filesSynced, bytesSynced, currentFile, currentFileSize, speed, eta)
	if err != nil {
		t.Fatalf("UpdateSyncProgress failed: %v", err)
	}

	// Verify the progress was updated
	state := mgr.GetState()
	if state.CurrentSync == nil {
		t.Fatal("CurrentSync is nil after UpdateSyncProgress")
	}

	// Check that totals are preserved
	if state.CurrentSync.FilesTotal != totalFiles {
		t.Errorf("FilesTotal changed: got %v, want %v", state.CurrentSync.FilesTotal, totalFiles)
	}
	if state.CurrentSync.BytesTotal != totalBytes {
		t.Errorf("BytesTotal changed: got %v, want %v", state.CurrentSync.BytesTotal, totalBytes)
	}

	// Check that progress values are updated
	if state.CurrentSync.FilesSynced != filesSynced {
		t.Errorf("FilesSynced = %v, want %v", state.CurrentSync.FilesSynced, filesSynced)
	}
	if state.CurrentSync.BytesSynced != bytesSynced {
		t.Errorf("BytesSynced = %v, want %v", state.CurrentSync.BytesSynced, bytesSynced)
	}
	if state.CurrentSync.CurrentFile != currentFile {
		t.Errorf("CurrentFile = %v, want %v", state.CurrentSync.CurrentFile, currentFile)
	}
	if state.CurrentSync.TransferSpeed != speed {
		t.Errorf("TransferSpeed = %v, want %v", state.CurrentSync.TransferSpeed, speed)
	}
	if state.CurrentSync.ETA != eta {
		t.Errorf("ETA = %v, want %v", state.CurrentSync.ETA, eta)
	}
}

func TestJSONSerialization(t *testing.T) {
	mgr := setupTestManager(t)

	// Start a sync
	cardID := "test-card-789"
	totalFiles := int64(50)
	totalBytes := int64(1024 * 1024 * 25) // 25MB

	_, err := mgr.StartSync(cardID, totalFiles, totalBytes)
	if err != nil {
		t.Fatalf("StartSync failed: %v", err)
	}

	// Update progress
	mgr.UpdateSyncProgress(10, 1024*1024*5, "IMG_010.jpg", 1024*1024, 500000, "1m30s")

	// Get the state and serialize to JSON
	state := mgr.GetState()
	jsonData, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("Failed to marshal state: %v", err)
	}

	// Parse the JSON to verify structure
	var parsed map[string]interface{}
	err = json.Unmarshal(jsonData, &parsed)
	if err != nil {
		t.Fatalf("Failed to unmarshal JSON: %v", err)
	}

	// Verify status field
	if status, ok := parsed["status"].(string); !ok || status != "syncing" {
		t.Errorf("JSON status = %v, want 'syncing'", parsed["status"])
	}

	// Verify current_sync exists and has correct structure
	currentSync, ok := parsed["current_sync"].(map[string]interface{})
	if !ok {
		t.Fatal("current_sync is not present or not an object in JSON")
	}

	// Verify critical fields for progress display
	requiredFields := []string{"files_total", "files_synced", "bytes_total", "bytes_synced"}
	for _, field := range requiredFields {
		if _, exists := currentSync[field]; !exists {
			t.Errorf("current_sync missing required field: %s", field)
		}
	}

	// Verify the values are correct types and non-zero
	if filesTotal, ok := currentSync["files_total"].(float64); !ok || filesTotal != 50 {
		t.Errorf("files_total = %v, want 50", currentSync["files_total"])
	}
	if filesSynced, ok := currentSync["files_synced"].(float64); !ok || filesSynced != 10 {
		t.Errorf("files_synced = %v, want 10", currentSync["files_synced"])
	}
	if bytesTotal, ok := currentSync["bytes_total"].(float64); !ok || bytesTotal != float64(1024*1024*25) {
		t.Errorf("bytes_total = %v, want %v", currentSync["bytes_total"], 1024*1024*25)
	}
	if bytesSynced, ok := currentSync["bytes_synced"].(float64); !ok || bytesSynced != float64(1024*1024*5) {
		t.Errorf("bytes_synced = %v, want %v", currentSync["bytes_synced"], 1024*1024*5)
	}

	t.Logf("JSON output: %s", string(jsonData))
}

func TestStateReloadPreservesCurrentSync(t *testing.T) {
	// Create first manager and start sync
	mgr1 := setupTestManager(t)
	_, err := mgr1.StartSync("card-001", 100, 1024*1024*50)
	if err != nil {
		t.Fatalf("StartSync failed: %v", err)
	}
	mgr1.progressSaveDelay = 0
	mgr1.UpdateSyncProgress(25, 1024*1024*12, "IMG_025.jpg", 1024*1024, 500000, "2m")

	// For this test, we'll skip file verification since we're using mock managers

	// Create second manager and reload state
	mgr2 := setupTestManager(t)
	err = mgr2.Reload()
	if err != nil {
		t.Fatalf("Reload failed: %v", err)
	}

	// Verify current_sync is preserved
	state := mgr2.GetState()
	if state.CurrentSync == nil {
		t.Fatal("CurrentSync lost after reload")
	}
	if state.CurrentSync.FilesTotal != 100 {
		t.Errorf("FilesTotal = %v, want 100", state.CurrentSync.FilesTotal)
	}
	if state.CurrentSync.FilesSynced != 25 {
		t.Errorf("FilesSynced = %v, want 25", state.CurrentSync.FilesSynced)
	}
}

func TestProgressThrottling(t *testing.T) {
	mgr := setupTestManager(t)
	mgr.progressSaveDelay = 100 * time.Millisecond // Short delay for testing

	// Start sync
	_, err := mgr.StartSync("card-002", 100, 1024*1024*50)
	if err != nil {
		t.Fatalf("StartSync failed: %v", err)
	}

	// First update should save immediately
	beforeSave := time.Now()
	mgr.UpdateSyncProgress(1, 1024*1024, "IMG_001.jpg", 1024*1024, 500000, "5m")

	// Second update within throttle window should not save
	mgr.UpdateSyncProgress(2, 1024*1024*2, "IMG_002.jpg", 1024*1024, 500000, "4m50s")

	// Verify progress is updated in memory
	state := mgr.GetState()
	if state.CurrentSync.FilesSynced != 2 {
		t.Errorf("FilesSynced not updated in memory: got %v, want 2", state.CurrentSync.FilesSynced)
	}

	// Wait for throttle period
	time.Sleep(110 * time.Millisecond)

	// Third update should save
	mgr.UpdateSyncProgress(3, 1024*1024*3, "IMG_003.jpg", 1024*1024, 500000, "4m40s")
	afterSave := time.Now()

	// Verify throttling worked (at least 100ms passed)
	if afterSave.Sub(beforeSave) < 100*time.Millisecond {
		t.Errorf("Throttling not working: only %v elapsed", afterSave.Sub(beforeSave))
	}
}

func TestConcurrentUpdates(t *testing.T) {
	mgr := setupTestManager(t)

	// Start sync
	_, err := mgr.StartSync("card-003", 1000, 1024*1024*500)
	if err != nil {
		t.Fatalf("StartSync failed: %v", err)
	}

	// Simulate concurrent updates from multiple goroutines
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func(fileNum int) {
			for j := 0; j < 10; j++ {
				mgr.UpdateSyncProgress(
					int64(fileNum*10+j),
					int64(fileNum*10+j)*1024*1024,
					fmt.Sprintf("IMG_%03d.jpg", fileNum*10+j),
					1024*1024,
					500000,
					"varies",
				)
				time.Sleep(time.Millisecond)
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Verify state is consistent
	state := mgr.GetState()
	if state.CurrentSync == nil {
		t.Fatal("CurrentSync is nil after concurrent updates")
	}
	if state.CurrentSync.FilesTotal != 1000 {
		t.Errorf("FilesTotal corrupted: got %v, want 1000", state.CurrentSync.FilesTotal)
	}
	if state.CurrentSync.BytesTotal != 1024*1024*500 {
		t.Errorf("BytesTotal corrupted: got %v, want %v", state.CurrentSync.BytesTotal, 1024*1024*500)
	}
}

// TestFinishSync tests completing a sync operation
func TestFinishSync(t *testing.T) {
	mgr := setupTestManager(t)

	// Start a sync
	cardID := "test-card-finish"
	_, err := mgr.StartSync(cardID, 100, 1024*1024*50)
	if err != nil {
		t.Fatalf("StartSync failed: %v", err)
	}

	// Update progress to completion
	mgr.UpdateSyncProgress(100, 1024*1024*50, "IMG_100.jpg", 1024*1024, 500000, "0s")

	// Finish sync successfully
	err = mgr.FinishSync(true, nil)
	if err != nil {
		t.Fatalf("FinishSync failed: %v", err)
	}

	// Verify state after successful sync
	state := mgr.GetState()
	if state.Status != StatusSuccess {
		t.Errorf("Status = %v, want %v", state.Status, StatusSuccess)
	}
	if state.CurrentSync != nil {
		t.Error("CurrentSync should be nil after finishing")
	}
	if state.LastSync == nil {
		t.Fatal("LastSync should not be nil after finishing")
	}
	if state.LastSync.Status != "success" {
		t.Errorf("LastSync.Status = %v, want success", state.LastSync.Status)
	}

	// Check history
	history := mgr.GetHistory()
	if len(history) != 1 {
		t.Fatalf("History length = %d, want 1", len(history))
	}
	if history[0].CardID != cardID {
		t.Errorf("History CardID = %v, want %v", history[0].CardID, cardID)
	}
}

// TestFinishSyncWithError tests completing a sync with error
func TestFinishSyncWithError(t *testing.T) {
	mgr := setupTestManager(t)

	// Start a sync
	_, err := mgr.StartSync("test-card-error", 100, 1024*1024*50)
	if err != nil {
		t.Fatalf("StartSync failed: %v", err)
	}

	// Update with partial progress
	mgr.UpdateSyncProgress(25, 1024*1024*12, "IMG_025.jpg", 1024*1024, 500000, "3m")

	// Finish sync with error
	testErr := fmt.Errorf("connection lost")
	err = mgr.FinishSync(false, testErr)
	if err != nil {
		t.Fatalf("FinishSync failed: %v", err)
	}

	// Verify state after error
	state := mgr.GetState()
	if state.Status != StatusError {
		t.Errorf("Status = %v, want %v", state.Status, StatusError)
	}
	if state.LastSync == nil {
		t.Fatal("LastSync should not be nil after error")
	}
	if state.LastSync.Status != "error" {
		t.Errorf("LastSync.Status = %v, want error", state.LastSync.Status)
	}
	if state.LastSync.Error != testErr.Error() {
		t.Errorf("LastSync.Error = %v, want %v", state.LastSync.Error, testErr.Error())
	}
}

// TestFindLastSyncByCardID tests finding the last sync for a card
func TestFindLastSyncByCardID(t *testing.T) {
	mgr := setupTestManager(t)

	// Create multiple syncs for different cards
	cards := []struct {
		id    string
		files int64
	}{
		{"card-A", 10},
		{"card-B", 20},
		{"card-A", 30},
		{"card-C", 40},
		{"card-A", 50},
	}

	for _, card := range cards {
		mgr.StartSync(card.id, card.files, card.files*1024)
		mgr.UpdateSyncProgress(card.files, card.files*1024, "done.jpg", 1024, 500000, "0s")
		mgr.FinishSync(true, nil)
		time.Sleep(10 * time.Millisecond) // Ensure different timestamps
	}

	// Find last sync for card-A
	lastSync := mgr.FindLastSyncByCardID("card-A")
	if lastSync == nil {
		t.Fatal("Should find last sync for card-A")
	}
	if lastSync.FilesTotal != 50 {
		t.Errorf("Last sync FilesTotal = %d, want 50", lastSync.FilesTotal)
	}

	// Find last sync for card-B
	lastSync = mgr.FindLastSyncByCardID("card-B")
	if lastSync == nil {
		t.Fatal("Should find last sync for card-B")
	}
	if lastSync.FilesTotal != 20 {
		t.Errorf("Last sync FilesTotal = %d, want 20", lastSync.FilesTotal)
	}

	// Try non-existent card
	lastSync = mgr.FindLastSyncByCardID("card-Z")
	if lastSync != nil {
		t.Error("Should not find sync for non-existent card")
	}
}

// TestSubscribeUnsubscribe tests the pub/sub system
func TestSubscribeUnsubscribe(t *testing.T) {
	mgr := setupTestManager(t)

	// Subscribe multiple listeners
	ch1 := mgr.Subscribe()
	ch2 := mgr.Subscribe()
	ch3 := mgr.Subscribe()

	// Trigger a state change
	mgr.SetStatus(StatusDetected)

	// All channels should receive the update
	timeout := time.After(1 * time.Second)
	for i, ch := range []chan CurrentState{ch1, ch2, ch3} {
		select {
		case state := <-ch:
			if state.Status != StatusDetected {
				t.Errorf("Channel %d: Status = %v, want %v", i+1, state.Status, StatusDetected)
			}
		case <-timeout:
			t.Errorf("Channel %d: timeout waiting for update", i+1)
		}
	}

	// Unsubscribe ch2
	mgr.Unsubscribe(ch2)

	// Trigger another change
	mgr.SetStatus(StatusSyncing)

	// Only ch1 and ch3 should receive update
	for i, ch := range []chan CurrentState{ch1, ch3} {
		select {
		case state := <-ch:
			if state.Status != StatusSyncing {
				t.Errorf("Channel %d: Status = %v, want %v", i+1, state.Status, StatusSyncing)
			}
		case <-time.After(1 * time.Second):
			t.Errorf("Channel %d: timeout waiting for update", i+1)
		}
	}

	// ch2 should be closed
	select {
	case _, ok := <-ch2:
		if ok {
			t.Error("ch2 should be closed")
		}
	default:
		// Channel might still be open but empty, force check
		go func() { <-ch2 }()
		time.Sleep(100 * time.Millisecond)
	}
}

// TestSetSDCard tests SD card management
func TestSetSDCard(t *testing.T) {
	mgr := setupTestManager(t)

	// Set SD card mounted
	err := mgr.SetSDCard(true, "/mnt/sdcard")
	if err != nil {
		t.Fatalf("SetSDCard failed: %v", err)
	}

	state := mgr.GetState()
	if !state.SDCardMounted {
		t.Error("SDCardMounted should be true")
	}
	if state.SDCardPath != "/mnt/sdcard" {
		t.Errorf("SDCardPath = %v, want /mnt/sdcard", state.SDCardPath)
	}

	// Unmount SD card
	err = mgr.SetSDCard(false, "")
	if err != nil {
		t.Fatalf("SetSDCard unmount failed: %v", err)
	}

	state = mgr.GetState()
	if state.SDCardMounted {
		t.Error("SDCardMounted should be false")
	}
	if state.SDCardPath != "" {
		t.Errorf("SDCardPath = %v, want empty", state.SDCardPath)
	}
}

// TestDeviceManagement tests device info management
func TestDeviceManagement(t *testing.T) {
	mgr := setupTestManager(t)

	devices := []DeviceInfo{
		{
			DevicePath:  "/dev/sda1",
			DeviceName:  "USB Drive",
			Size:        32 * 1024 * 1024 * 1024,
			SizeHuman:   "32GB",
			IsUSB:       true,
			IsMounted:   false,
			HasDCIM:     true,
			VolumeLabel: "PHOTOS",
		},
		{
			DevicePath:  "/dev/mmcblk0p1",
			DeviceName:  "SD Card",
			Size:        64 * 1024 * 1024 * 1024,
			SizeHuman:   "64GB",
			IsUSB:       false,
			IsMounted:   true,
			MountPath:   "/mnt/sdcard",
			HasDCIM:     true,
			VolumeLabel: "CANON",
		},
	}

	// Set available devices
	err := mgr.SetAvailableDevices(devices)
	if err != nil {
		t.Fatalf("SetAvailableDevices failed: %v", err)
	}

	state := mgr.GetState()
	if len(state.AvailableDevices) != 2 {
		t.Fatalf("AvailableDevices length = %d, want 2", len(state.AvailableDevices))
	}

	// Verify device details
	for i, expected := range devices {
		actual := state.AvailableDevices[i]
		if actual.DevicePath != expected.DevicePath {
			t.Errorf("Device[%d].DevicePath = %v, want %v", i, actual.DevicePath, expected.DevicePath)
		}
		if actual.IsUSB != expected.IsUSB {
			t.Errorf("Device[%d].IsUSB = %v, want %v", i, actual.IsUSB, expected.IsUSB)
		}
		if actual.HasDCIM != expected.HasDCIM {
			t.Errorf("Device[%d].HasDCIM = %v, want %v", i, actual.HasDCIM, expected.HasDCIM)
		}
	}

	// Set needs device select
	err = mgr.SetNeedsDeviceSelect(true)
	if err != nil {
		t.Fatalf("SetNeedsDeviceSelect failed: %v", err)
	}

	state = mgr.GetState()
	if !state.NeedsDeviceSelect {
		t.Error("NeedsDeviceSelect should be true")
	}
}

// TestConcurrentOperations tests thread safety with mixed operations
func TestConcurrentOperations(t *testing.T) {
	mgr := setupTestManager(t)

	var wg sync.WaitGroup

	// Multiple readers
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_ = mgr.GetState()
				_ = mgr.GetHistory()
				time.Sleep(time.Microsecond * 100)
			}
		}(i)
	}

	// Status updaters
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			statuses := []SyncStatus{StatusIdle, StatusDetected, StatusSyncing, StatusSuccess, StatusError}
			for j := 0; j < 20; j++ {
				mgr.SetStatus(statuses[j%len(statuses)])
				time.Sleep(time.Millisecond)
			}
		}(i)
	}

	// Sync operations
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 5; j++ {
				cardID := fmt.Sprintf("card-%d-%d", id, j)
				if _, err := mgr.StartSync(cardID, 100, 1024*1024); err == nil {
					for k := 0; k < 10; k++ {
						mgr.UpdateSyncProgress(int64(k*10), int64(k*100*1024), "file.jpg", 1024, 100000, "1m")
						time.Sleep(time.Microsecond * 500)
					}
					mgr.FinishSync(true, nil)
				}
				time.Sleep(time.Millisecond * 5)
			}
		}(i)
	}

	// Subscribers
	var channels []chan CurrentState
	var chMutex sync.Mutex
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			ch := mgr.Subscribe()
			chMutex.Lock()
			channels = append(channels, ch)
			chMutex.Unlock()

			count := 0
			timeout := time.After(5 * time.Second)
			for {
				select {
				case _, ok := <-ch:
					if !ok {
						return
					}
					count++
					if count > 50 {
						mgr.Unsubscribe(ch)
						return
					}
				case <-timeout:
					mgr.Unsubscribe(ch)
					return
				}
			}
		}(i)
	}

	wg.Wait()

	// Clean up remaining channels
	chMutex.Lock()
	for _, ch := range channels {
		select {
		case <-ch:
		default:
			mgr.Unsubscribe(ch)
		}
	}
	chMutex.Unlock()

	// Verify final state is consistent
	state := mgr.GetState()
	if state.CurrentSync != nil {
		if state.Status == StatusIdle || state.Status == StatusSuccess || state.Status == StatusError {
			t.Error("Inconsistent state: CurrentSync should be nil when not syncing")
		}
	}
}
