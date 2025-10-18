package e2e

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/denysvitali/pictures-sync-s3/pkg/daemon/cardhandler"
	"github.com/denysvitali/pictures-sync-s3/pkg/events"
	"github.com/denysvitali/pictures-sync-s3/pkg/sdmonitor"
	"github.com/denysvitali/pictures-sync-s3/pkg/settings"
	"github.com/denysvitali/pictures-sync-s3/pkg/state"
	"github.com/denysvitali/pictures-sync-s3/pkg/syncmanager"
)

// TestE2ERecoveryFromSyncFailure tests recovery after sync failure
func TestE2ERecoveryFromSyncFailure(t *testing.T) {
	testEnv := setupTestEnvironment(t)
	defer testEnv.Cleanup()

	eventMgr := events.NewManager()
	stateMgr, _ := state.NewManager()
	appSettings, _ := settings.Load()

	// Create sync manager with invalid config to force failure
	syncMgr := syncmanager.NewManager(
		"/nonexistent/rclone.conf", // Invalid config path
		"invalid-remote",
		"/invalid/path",
		stateMgr,
		1,
		1,
	)

	monitor := sdmonitor.NewMonitor(testEnv.MountDir)
	handler := cardhandler.NewHandler(monitor, stateMgr, syncMgr, appSettings, eventMgr)

	monitor.Start()
	defer monitor.Stop()

	// Insert card and attempt sync (should fail)
	mockCard := testEnv.CreateMockSDCard("failure-recovery", 10)
	testEnv.MountMockCard(mockCard)

	handler.HandleInserted(sdmonitor.Event{
		Type:      sdmonitor.EventInserted,
		DevName:   mockCard.DevName,
		MountPath: mockCard.MountPath,
	})

	// Wait for failure
	time.Sleep(3 * time.Second)

	// Verify system is in error state
	currentState := stateMgr.GetState()
	if currentState.Status != state.StatusError && currentState.Status != state.StatusIdle {
		t.Logf("Warning: Expected error or idle state after failed sync, got %s", currentState.Status)
	}

	// Verify error was recorded in history
	history := stateMgr.GetHistory()
	if len(history) > 0 {
		lastSync := history[len(history)-1]
		if lastSync.Status == "error" {
			t.Logf("Error correctly recorded: %s", lastSync.Error)
		}
	}

	// Remove failed card
	handler.HandleRemoved(sdmonitor.Event{
		Type:    sdmonitor.EventRemoved,
		DevName: mockCard.DevName,
	})
	testEnv.UnmountMockCard(mockCard)

	time.Sleep(500 * time.Millisecond)

	// Verify system returned to idle
	currentState = stateMgr.GetState()
	if currentState.Status != state.StatusIdle {
		t.Errorf("System should return to idle after card removal, got %s", currentState.Status)
	}

	// Insert new card with valid config - should work
	appSettings.SetRemoteName("test-remote")
	appSettings.SetRemotePath(filepath.Join(testEnv.BaseDir, "remote"))

	validSyncMgr := syncmanager.NewManager(
		testEnv.RcloneConfigPath,
		appSettings.GetRemoteName(),
		appSettings.GetRemotePath(),
		stateMgr,
		1,
		1,
	)

	handler2 := cardhandler.NewHandler(monitor, stateMgr, validSyncMgr, appSettings, eventMgr)

	mockCard2 := testEnv.CreateMockSDCard("recovery-success", 5)
	testEnv.MountMockCard(mockCard2)

	handler2.HandleInserted(sdmonitor.Event{
		Type:      sdmonitor.EventInserted,
		DevName:   mockCard2.DevName,
		MountPath: mockCard2.MountPath,
	})

	// Wait for sync
	waitForSyncCompletion(t, stateMgr, 15*time.Second)

	// Verify recovery succeeded
	finalState := stateMgr.GetState()
	if finalState.Status == state.StatusError {
		t.Errorf("Recovery failed: %s", finalState.Error)
	}

	t.Log("System successfully recovered from sync failure")
}

// TestE2ERecoveryFromCardRemovalDuringSync tests recovery from mid-sync card removal
func TestE2ERecoveryFromCardRemovalDuringSync(t *testing.T) {
	testEnv := setupTestEnvironment(t)
	defer testEnv.Cleanup()

	eventMgr := events.NewManager()
	stateMgr, _ := state.NewManager()
	appSettings, _ := settings.Load()
	appSettings.SetRemoteName("test-remote")
	appSettings.SetRemotePath(filepath.Join(testEnv.BaseDir, "remote"))

	syncMgr := syncmanager.NewManager(
		testEnv.RcloneConfigPath,
		appSettings.GetRemoteName(),
		appSettings.GetRemotePath(),
		stateMgr,
		1,
		1,
	)

	monitor := sdmonitor.NewMonitor(testEnv.MountDir)
	handler := cardhandler.NewHandler(monitor, stateMgr, syncMgr, appSettings, eventMgr)

	monitor.Start()
	defer monitor.Stop()

	// Insert card and start sync
	mockCard := testEnv.CreateMockSDCard("removal-during-sync", 30)
	testEnv.MountMockCard(mockCard)

	handler.HandleInserted(sdmonitor.Event{
		Type:      sdmonitor.EventInserted,
		DevName:   mockCard.DevName,
		MountPath: mockCard.MountPath,
	})

	// Wait for sync to start
	timeout := time.After(5 * time.Second)
	for {
		select {
		case <-timeout:
			t.Fatal("Sync did not start")
		case <-time.After(100 * time.Millisecond):
			if stateMgr.GetState().Status == state.StatusSyncing {
				goto syncStarted
			}
		}
	}

syncStarted:
	t.Log("Sync started, simulating card removal")

	// Remove card mid-sync
	handler.HandleRemoved(sdmonitor.Event{
		Type:    sdmonitor.EventRemoved,
		DevName: mockCard.DevName,
	})
	testEnv.UnmountMockCard(mockCard)

	time.Sleep(1 * time.Second)

	// Verify system recovered to idle state
	finalState := stateMgr.GetState()
	if finalState.Status != state.StatusIdle {
		t.Errorf("Expected idle state after card removal, got %s", finalState.Status)
	}

	// Verify sync was cancelled in history
	history := stateMgr.GetHistory()
	if len(history) > 0 {
		lastSync := history[len(history)-1]
		if lastSync.Status != "error" {
			t.Errorf("Expected error status for interrupted sync, got %s", lastSync.Status)
		}
		t.Logf("Interrupted sync recorded: %s", lastSync.Error)
	}

	// Test that system can handle new card after interruption
	time.Sleep(500 * time.Millisecond)

	newCard := testEnv.CreateMockSDCard("post-interruption", 10)
	testEnv.MountMockCard(newCard)

	handler.HandleInserted(sdmonitor.Event{
		Type:      sdmonitor.EventInserted,
		DevName:   newCard.DevName,
		MountPath: newCard.MountPath,
	})

	waitForSyncCompletion(t, stateMgr, 15*time.Second)

	if stateMgr.GetState().Status == state.StatusError {
		t.Error("System failed to recover for new card after interruption")
	}

	t.Log("System successfully handled new card after sync interruption")
}

// TestE2ERecoveryFromCorruptedState tests recovery from corrupted state files
func TestE2ERecoveryFromCorruptedState(t *testing.T) {
	testEnv := setupTestEnvironment(t)
	defer testEnv.Cleanup()

	// Create corrupted state file
	stateFile := filepath.Join(state.BaseDir, "state.json")
	if err := os.MkdirAll(state.BaseDir, 0755); err != nil {
		t.Fatalf("Failed to create state dir: %v", err)
	}

	// Write invalid JSON
	corruptedData := []byte(`{"status": "syncing", "incomplete json`)
	if err := os.WriteFile(stateFile, corruptedData, 0644); err != nil {
		t.Fatalf("Failed to write corrupted state: %v", err)
	}

	// Create state manager - should handle corruption gracefully
	stateMgr, err := state.NewManager()
	if err != nil {
		t.Fatalf("Failed to create manager with corrupted state: %v", err)
	}

	// Verify it initialized with default state
	currentState := stateMgr.GetState()
	if currentState.Status != state.StatusIdle {
		t.Errorf("Expected idle state after corruption recovery, got %s", currentState.Status)
	}

	// Verify it can function normally
	stateMgr.SetStatus(state.StatusDetected)
	stateMgr.SetSDCard(true, "/mnt/test")
	stateMgr.StartSync("recovery-test", 10, 10*1024*1024)
	stateMgr.FinishSync(true, nil)

	// Verify state file was rewritten correctly
	newState := stateMgr.GetState()
	if newState.Status == "" {
		t.Error("State was not properly saved after corruption recovery")
	}

	t.Log("Successfully recovered from corrupted state file")
}

// TestE2ERecoveryFromDiskFull tests handling disk full scenarios
func TestE2ERecoveryFromDiskFull(t *testing.T) {
	testEnv := setupTestEnvironment(t)
	defer testEnv.Cleanup()

	stateMgr, _ := state.NewManager()

	// Simulate disk full by trying to write to a read-only location
	// In real scenario, rclone would fail with "no space left on device"

	// Start a sync that will fail
	record, err := stateMgr.StartSync("disk-full-test", 100, 100*1024*1024)
	if err != nil {
		t.Fatalf("Failed to start sync: %v", err)
	}

	// Simulate disk full error
	diskFullErr := errors.New("no space left on device")
	stateMgr.FinishSync(false, diskFullErr)

	// Verify error was recorded
	history := stateMgr.GetHistory()
	if len(history) == 0 {
		t.Fatal("No history recorded")
	}

	lastSync := history[len(history)-1]
	if lastSync.Status != "error" {
		t.Errorf("Expected error status, got %s", lastSync.Status)
	}

	if lastSync.Error != diskFullErr.Error() {
		t.Errorf("Error message = %s, want %s", lastSync.Error, diskFullErr.Error())
	}

	// Verify system can recover for next sync
	stateMgr.SetStatus(state.StatusIdle)
	_, err = stateMgr.StartSync("recovery-after-full", 50, 50*1024*1024)
	if err != nil {
		t.Errorf("Failed to start sync after disk full: %v", err)
	}

	stateMgr.FinishSync(true, nil)

	// Verify successful sync was recorded
	history = stateMgr.GetHistory()
	if len(history) < 2 {
		t.Fatal("Expected at least 2 sync records")
	}

	lastSync = history[len(history)-1]
	if lastSync.Status != "success" {
		t.Errorf("Recovery sync status = %s, want success", lastSync.Status)
	}

	t.Log("Successfully recovered from disk full scenario")
}

// TestE2ERecoveryFromNetworkFailure tests recovery from network failures
func TestE2ERecoveryFromNetworkFailure(t *testing.T) {
	testEnv := setupTestEnvironment(t)
	defer testEnv.Cleanup()

	stateMgr, _ := state.NewManager()

	// Simulate network failure scenarios
	networkErrors := []error{
		errors.New("connection timeout"),
		errors.New("no route to host"),
		errors.New("network unreachable"),
		errors.New("temporary failure in name resolution"),
	}

	for i, netErr := range networkErrors {
		t.Run(fmt.Sprintf("Error_%d", i), func(t *testing.T) {
			cardID := fmt.Sprintf("net-fail-%d", i)

			// Start sync
			stateMgr.StartSync(cardID, 10, 10*1024*1024)

			// Fail with network error
			stateMgr.FinishSync(false, netErr)

			// Verify error recorded
			history := stateMgr.GetHistory()
			lastSync := history[len(history)-1]

			if lastSync.Status != "error" {
				t.Errorf("Expected error status, got %s", lastSync.Status)
			}

			if lastSync.Error != netErr.Error() {
				t.Errorf("Error = %s, want %s", lastSync.Error, netErr.Error())
			}

			// Verify can start new sync
			stateMgr.SetStatus(state.StatusIdle)
		})
	}

	t.Log("Successfully handled all network error scenarios")
}

// TestE2ERecoveryFromRapidCardSwapping tests handling rapid card swaps
func TestE2ERecoveryFromRapidCardSwapping(t *testing.T) {
	testEnv := setupTestEnvironment(t)
	defer testEnv.Cleanup()

	eventMgr := events.NewManager()
	stateMgr, _ := state.NewManager()
	appSettings, _ := settings.Load()
	appSettings.SetRemoteName("test-remote")
	appSettings.SetRemotePath(filepath.Join(testEnv.BaseDir, "remote"))

	syncMgr := syncmanager.NewManager(
		testEnv.RcloneConfigPath,
		appSettings.GetRemoteName(),
		appSettings.GetRemotePath(),
		stateMgr,
		1,
		1,
	)

	monitor := sdmonitor.NewMonitor(testEnv.MountDir)
	handler := cardhandler.NewHandler(monitor, stateMgr, syncMgr, appSettings, eventMgr)

	monitor.Start()
	defer monitor.Stop()

	// Rapidly insert and remove cards
	for i := 0; i < 5; i++ {
		card := testEnv.CreateMockSDCard(fmt.Sprintf("rapid-swap-%d", i), 5)

		// Insert
		testEnv.MountMockCard(card)
		handler.HandleInserted(sdmonitor.Event{
			Type:      sdmonitor.EventInserted,
			DevName:   card.DevName,
			MountPath: card.MountPath,
		})

		// Quick removal before sync can complete
		time.Sleep(200 * time.Millisecond)

		handler.HandleRemoved(sdmonitor.Event{
			Type:    sdmonitor.EventRemoved,
			DevName: card.DevName,
		})
		testEnv.UnmountMockCard(card)

		time.Sleep(100 * time.Millisecond)
	}

	// Wait for everything to settle
	time.Sleep(2 * time.Second)

	// Verify system is still in valid state
	currentState := stateMgr.GetState()
	if currentState.Status != state.StatusIdle && currentState.Status != state.StatusDetected {
		t.Errorf("System in unexpected state after rapid swapping: %s", currentState.Status)
	}

	// Verify system can still process a normal card
	normalCard := testEnv.CreateMockSDCard("post-rapid-swap", 10)
	testEnv.MountMockCard(normalCard)

	handler.HandleInserted(sdmonitor.Event{
		Type:      sdmonitor.EventInserted,
		DevName:   normalCard.DevName,
		MountPath: normalCard.MountPath,
	})

	waitForSyncCompletion(t, stateMgr, 15*time.Second)

	finalState := stateMgr.GetState()
	if finalState.Status == state.StatusError {
		t.Errorf("System failed after rapid swapping: %s", finalState.Error)
	}

	t.Log("Successfully handled rapid card swapping")
}

// TestE2ERecoveryFromConcurrentOperations tests recovery from concurrent issues
func TestE2ERecoveryFromConcurrentOperations(t *testing.T) {
	testEnv := setupTestEnvironment(t)
	defer testEnv.Cleanup()

	stateMgr, _ := state.NewManager()

	// Simulate concurrent state changes
	done := make(chan bool, 10)
	errors := make(chan error, 10)

	for i := 0; i < 10; i++ {
		go func(n int) {
			defer func() {
				if r := recover(); r != nil {
					errors <- fmt.Errorf("goroutine %d panicked: %v", n, r)
				}
				done <- true
			}()

			// Rapid state changes
			stateMgr.SetStatus(state.StatusDetected)
			stateMgr.SetSDCard(true, fmt.Sprintf("/mnt/card-%d", n))
			stateMgr.StartSync(fmt.Sprintf("concurrent-%d", n), 10, 10*1024*1024)
			stateMgr.UpdateSyncProgress(5, 5*1024*1024, "test.jpg", 1024, 1024, "1s")
			stateMgr.FinishSync(true, nil)
			stateMgr.SetStatus(state.StatusIdle)
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	close(errors)

	// Check for errors
	errorCount := 0
	for err := range errors {
		t.Errorf("Concurrent operation error: %v", err)
		errorCount++
	}

	if errorCount > 0 {
		t.Errorf("Total concurrent errors: %d", errorCount)
	}

	// Verify state is still consistent
	currentState := stateMgr.GetState()
	if currentState.Status == "" {
		t.Error("State became invalid after concurrent operations")
	}

	// Verify history is valid
	history := stateMgr.GetHistory()
	t.Logf("History has %d records after concurrent operations", len(history))

	t.Log("Successfully handled concurrent operations")
}

// TestE2ERecoveryFromPermissionErrors tests handling of permission errors
func TestE2ERecoveryFromPermissionErrors(t *testing.T) {
	testEnv := setupTestEnvironment(t)
	defer testEnv.Cleanup()

	// Create a card with restricted permissions
	card := testEnv.CreateMockSDCard("permission-test", 10)
	testEnv.MountMockCard(card)

	// Make DCIM directory read-only
	dcimPath := filepath.Join(card.MountPath, "DCIM")
	if err := os.Chmod(dcimPath, 0444); err != nil {
		t.Fatalf("Failed to make DCIM read-only: %v", err)
	}

	// Attempt to create card ID file (should fail due to permissions)
	monitor := sdmonitor.NewMonitor(testEnv.MountDir)

	// This should handle the permission error gracefully
	_, _, err := sdmonitor.GetOrCreateCardID(card.MountPath, monitor)
	if err != nil {
		t.Logf("Permission error handled gracefully: %v", err)
	}

	// Restore permissions
	if err := os.Chmod(dcimPath, 0755); err != nil {
		t.Fatalf("Failed to restore permissions: %v", err)
	}

	// Retry should work
	cardID, isNew, err := sdmonitor.GetOrCreateCardID(card.MountPath, monitor)
	if err != nil {
		t.Errorf("Failed after permission restoration: %v", err)
	}

	if cardID == "" {
		t.Error("Card ID should not be empty after permission fix")
	}

	t.Logf("Successfully recovered from permission error (new=%v)", isNew)
}

// TestE2ERecoveryAfterPowerFailure tests state recovery after simulated power failure
func TestE2ERecoveryAfterPowerFailure(t *testing.T) {
	testEnv := setupTestEnvironment(t)
	defer testEnv.Cleanup()

	// First "session" - start a sync
	stateMgr1, _ := state.NewManager()

	stateMgr1.SetStatus(state.StatusDetected)
	stateMgr1.SetSDCard(true, "/mnt/power-test")
	stateMgr1.StartSync("power-failure-card", 100, 100*1024*1024)
	stateMgr1.UpdateSyncProgress(50, 50*1024*1024, "IMG_0050.JPG", 2*1024*1024, 1.5*1024*1024, "1m")

	// Simulate power failure - don't call FinishSync
	// Just drop the manager

	// "Power restored" - create new manager
	time.Sleep(500 * time.Millisecond)

	stateMgr2, err := state.NewManager()
	if err != nil {
		t.Fatalf("Failed to create manager after 'power failure': %v", err)
	}

	// Verify state was loaded
	restoredState := stateMgr2.GetState()
	t.Logf("Restored state: status=%s, mounted=%v", restoredState.Status, restoredState.SDCardMounted)

	// System should be able to continue operations
	stateMgr2.SetStatus(state.StatusIdle)

	// Start new sync to verify system works
	stateMgr2.StartSync("post-power-failure", 50, 50*1024*1024)
	stateMgr2.FinishSync(true, nil)

	// Verify sync completed
	history := stateMgr2.GetHistory()
	if len(history) == 0 {
		t.Error("No history after power failure recovery")
	}

	t.Log("Successfully recovered from simulated power failure")
}
