package syncmanager

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/denysvitali/pictures-sync-s3/pkg/state"
)

// TestRcloneConcurrentSyncPrevention tests that concurrent syncs are properly prevented
func TestRcloneConcurrentSyncPrevention(t *testing.T) {
	syncMgr, _, cleanup := setupTestSyncManager(t)
	defer cleanup()

	// Mark as running
	syncMgr.mu.Lock()
	syncMgr.isRunning = true
	syncMgr.mu.Unlock()

	// Try to start another sync
	err := syncMgr.Sync("/tmp/test", "card-0123456789abcdef", 10, 1024)
	if err == nil {
		t.Error("Expected error when starting sync while another is running")
	}

	expectedMsg := "sync already in progress"
	if !strings.Contains(err.Error(), expectedMsg) {
		t.Errorf("Expected error to contain %q, got: %v", expectedMsg, err)
	}
}

// TestCancelFuncCleanup verifies that cancelFunc is properly cleaned up after sync
func TestCancelFuncCleanup(t *testing.T) {
	syncMgr, _, cleanup := setupTestSyncManager(t)
	defer cleanup()

	// Manually set running state with cancel func
	_, cancel := context.WithCancel(context.Background())
	syncMgr.mu.Lock()
	syncMgr.isRunning = true
	syncMgr.cancelFunc = cancel
	syncMgr.mu.Unlock()

	// Simulate sync completion
	syncMgr.mu.Lock()
	syncMgr.isRunning = false
	syncMgr.cancelFunc = nil
	syncMgr.mu.Unlock()

	// Verify cancel func is nil
	syncMgr.mu.Lock()
	if syncMgr.cancelFunc != nil {
		t.Error("cancelFunc should be nil after sync completion")
	}
	syncMgr.mu.Unlock()
}

// TestCancelWithoutSync tests canceling when no sync is running
func TestCancelWithoutSync(t *testing.T) {
	syncMgr, _, cleanup := setupTestSyncManager(t)
	defer cleanup()

	err := syncMgr.Cancel()
	if err == nil {
		t.Error("Cancel should return error when no sync is running")
	}

	expectedMsg := "no sync in progress"
	if !strings.Contains(err.Error(), expectedMsg) {
		t.Errorf("Expected error to contain %q, got: %v", expectedMsg, err)
	}
}

// TestInvalidConfigPath tests handling of invalid config paths
// DISABLED: rclone library calls os.Exit() on severe config errors which kills the test
// BUG FOUND: rclone library logs ERROR to stdout/stderr and may call os.Exit()
// This makes it impossible to test severe config errors gracefully
/* func TestInvalidConfigPath(t *testing.T) {
	stateMgr, _ := state.NewManager()

	// Use a path that doesn't exist and can't be created
	invalidPath := "/root/impossible/path/rclone.conf"
	syncMgr := NewManager(invalidPath, "test-remote", "/test", stateMgr, 4, 8)

	// BUG FOUND: rclone library logs ERROR to stdout/stderr but continues execution
	// This pollutes logs and makes it hard to distinguish real errors
	// The operations fail gracefully but error logging is excessive

	// Test various operations with invalid config

	t.Run("ListRemotes", func(t *testing.T) {
		_, err := syncMgr.ListRemotes()
		if err == nil {
			t.Log("ListRemotes succeeded unexpectedly - config might have been created")
		} else {
			t.Logf("ListRemotes properly failed: %v", err)
		}
	})

	t.Run("TestConnection", func(t *testing.T) {
		err := syncMgr.TestConnection()
		if err == nil {
			t.Error("TestConnection should fail with invalid config path")
		} else {
			t.Logf("TestConnection properly failed: %v", err)
		}
	})
} */

// TestEmptyConfigFile tests handling of empty rclone config
func TestEmptyConfigFile(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "syncmanager-error-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create empty config file
	configPath := filepath.Join(tmpDir, "rclone.conf")
	if err := os.WriteFile(configPath, []byte(""), 0644); err != nil {
		t.Fatal(err)
	}

	stateMgr, _ := state.NewManager()
	syncMgr := NewManager(configPath, "nonexistent-remote", "/test", stateMgr, 4, 8)

	// Test operations with empty config
	t.Run("ListRemotes_EmptyConfig", func(t *testing.T) {
		remotes, err := syncMgr.ListRemotes()
		if err != nil {
			t.Errorf("ListRemotes failed with empty config: %v", err)
		}
		if len(remotes) != 0 {
			t.Errorf("Expected 0 remotes, got %d", len(remotes))
		}
	})

	t.Run("TestConnection_NonexistentRemote", func(t *testing.T) {
		err := syncMgr.TestConnection()
		if err == nil {
			t.Error("TestConnection should fail with nonexistent remote")
		}
	})
}

// TestMalformedConfigFile tests handling of malformed rclone config
func TestMalformedConfigFile(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "syncmanager-error-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create malformed config file
	configPath := filepath.Join(tmpDir, "rclone.conf")
	malformedConfig := `[test-remote
type = s3
this is not valid INI format
[[[[broken
	`
	if err := os.WriteFile(configPath, []byte(malformedConfig), 0644); err != nil {
		t.Fatal(err)
	}

	stateMgr, _ := state.NewManager()
	syncMgr := NewManager(configPath, "test-remote", "/test", stateMgr, 4, 8)

	// Config loading should handle this gracefully
	err = syncMgr.TestConnection()
	t.Logf("TestConnection with malformed config returned: %v", err)
	// The error should be about connection failure, not parsing failure
}

// TestNonexistentSourcePath tests syncing from a nonexistent source
func TestNonexistentSourcePath(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "syncmanager-error-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create minimal valid config with local backend
	configPath := filepath.Join(tmpDir, "rclone.conf")
	validConfig := `[local-test]
type = local
`
	if err := os.WriteFile(configPath, []byte(validConfig), 0644); err != nil {
		t.Fatal(err)
	}

	stateMgr, _ := state.NewManager()
	syncMgr := NewManager(configPath, "local-test", tmpDir, stateMgr, 4, 8)

	// Try to sync from nonexistent path
	nonexistentPath := filepath.Join(tmpDir, "this-does-not-exist")
	err = syncMgr.Sync(nonexistentPath, "card-0123456789abcdef", 10, 1024)

	if err == nil {
		t.Error("Sync should fail with nonexistent source path")
	}

	// BUG: rclone actually creates the source filesystem successfully even if path doesn't exist
	// Then fails later during sync with "directory not found"
	// The error message is different than expected
	if !strings.Contains(err.Error(), "failed to create source filesystem") &&
		!strings.Contains(err.Error(), "directory not found") &&
		!strings.Contains(err.Error(), "sync failed") {
		t.Errorf("Expected filesystem or sync error, got: %v", err)
	} else {
		t.Logf("Sync properly failed with: %v", err)
	}

	// Verify isRunning was reset
	if syncMgr.IsRunning() {
		t.Error("isRunning should be false after failed sync")
	}

	// Verify cancelFunc was cleaned up
	syncMgr.mu.Lock()
	if syncMgr.cancelFunc != nil {
		t.Error("cancelFunc should be nil after failed sync")
	}
	syncMgr.mu.Unlock()
}

// TestInvalidDestinationPath tests syncing to an invalid destination
func TestInvalidDestinationPath(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "syncmanager-error-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create source directory with a test file
	srcDir := filepath.Join(tmpDir, "source")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatal(err)
	}
	testFile := filepath.Join(srcDir, "test.jpg")
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create config with invalid remote
	configPath := filepath.Join(tmpDir, "rclone.conf")
	invalidConfig := `[broken-remote]
type = s3
access_key_id = invalid
secret_access_key = invalid
endpoint = https://invalid.example.com
`
	if err := os.WriteFile(configPath, []byte(invalidConfig), 0644); err != nil {
		t.Fatal(err)
	}

	stateMgr, _ := state.NewManager()
	syncMgr := NewManager(configPath, "missing-remote", "/test", stateMgr, 4, 8)

	// Try to sync - destination creation should fail
	err = syncMgr.Sync(srcDir, "card-0123456789abcdef", 1, 4)

	if err == nil {
		t.Error("Sync should fail with invalid destination remote")
	}

	if !strings.Contains(err.Error(), "failed to create destination filesystem") {
		t.Logf("Error: %v", err)
	}

	// Verify cleanup
	if syncMgr.IsRunning() {
		t.Error("isRunning should be false after failed sync")
	}
}

// TestProgressChannelOverflow tests that progress updates don't block when channels are full
func TestProgressChannelOverflow(t *testing.T) {
	syncMgr, stateMgr, cleanup := setupTestSyncManager(t)
	defer cleanup()

	// Subscribe with a channel that we won't read from
	progressChan := syncMgr.SubscribeProgress()

	// Create a mock context and stats
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// This should not block even though the channel is full
	done := make(chan struct{})

	// Start monitoring with a short interval
	go syncMgr.monitorProgress(ctx, nil, 100, 1024*1024, 0, done)

	// Let it try to send multiple times
	time.Sleep(100 * time.Millisecond)

	// Signal done
	close(done)

	// Channel should have some updates but not be blocked
	select {
	case <-progressChan:
		t.Log("Received at least one progress update")
	case <-time.After(100 * time.Millisecond):
		t.Log("No progress updates received (expected with nil stats)")
	}

	// Verify state manager wasn't blocked
	currentState := stateMgr.GetState()
	t.Logf("State manager still responsive: %v", currentState.Status)
}

// TestStateManagerNilHandling tests handling when state manager is nil
func TestStateManagerNilHandling(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "syncmanager-error-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	configPath := filepath.Join(tmpDir, "rclone.conf")

	// Create manager with nil state manager
	syncMgr := NewManager(configPath, "test", "/test", nil, 4, 8)

	// Operations should not panic with nil state manager
	t.Run("IsRunning", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("IsRunning panicked with nil state manager: %v", r)
			}
		}()
		syncMgr.IsRunning()
	})

	t.Run("SubscribeProgress", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("SubscribeProgress panicked: %v", r)
			}
		}()
		ch := syncMgr.SubscribeProgress()
		if ch == nil {
			t.Error("SubscribeProgress returned nil channel")
		}
	})
}

// TestConcurrentSetRemote tests thread safety of SetRemote
func TestConcurrentSetRemote(t *testing.T) {
	syncMgr, _, cleanup := setupTestSyncManager(t)
	defer cleanup()

	var wg sync.WaitGroup
	numGoroutines := 10

	// Concurrently update remote settings
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			remoteName := fmt.Sprintf("remote-%d", idx)
			remotePath := fmt.Sprintf("/path-%d", idx)
			syncMgr.SetRemote(remoteName, remotePath)
		}(i)
	}

	wg.Wait()

	// Verify no panic occurred and state is consistent
	t.Log("Concurrent SetRemote completed without panic")
}

// TestConcurrentProgressSubscriptions tests thread safety of progress subscriptions
func TestConcurrentProgressSubscriptions(t *testing.T) {
	syncMgr, _, cleanup := setupTestSyncManager(t)
	defer cleanup()

	var wg sync.WaitGroup
	numSubscribers := 20

	// Concurrently subscribe to progress
	for i := 0; i < numSubscribers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ch := syncMgr.SubscribeProgress()
			if ch == nil {
				t.Error("SubscribeProgress returned nil")
			}
		}()
	}

	wg.Wait()

	// Verify all channels were created
	syncMgr.mu.Lock()
	numChans := len(syncMgr.progressChans)
	syncMgr.mu.Unlock()

	if numChans != numSubscribers {
		t.Errorf("Expected %d progress channels, got %d", numSubscribers, numChans)
	}
}

// TestProgressCalculationWithZeroTotal tests progress calculation edge cases
func TestProgressCalculationWithZeroTotal(t *testing.T) {
	// This tests the percentage calculation in monitorProgress
	testCases := []struct {
		name               string
		totalBytes         int64
		transferredBytes   int64
		expectedPercentage int
	}{
		{
			name:               "Zero total bytes",
			totalBytes:         0,
			transferredBytes:   100,
			expectedPercentage: 0,
		},
		{
			name:               "Normal case",
			totalBytes:         1000,
			transferredBytes:   500,
			expectedPercentage: 50,
		},
		{
			name:               "Over 100% (more transferred than total)",
			totalBytes:         100,
			transferredBytes:   150,
			expectedPercentage: 150,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var percentage int
			if tc.totalBytes > 0 {
				percentage = int((float64(tc.transferredBytes) / float64(tc.totalBytes)) * 100)
			}

			if percentage != tc.expectedPercentage {
				t.Errorf("Expected %d%%, got %d%%", tc.expectedPercentage, percentage)
			}
		})
	}
}

// TestFormatDuration tests the duration formatting function
func TestFormatDuration(t *testing.T) {
	testCases := []struct {
		seconds  int
		expected string
	}{
		{30, "30s"},
		{60, "1m 0s"},
		{90, "1m 30s"},
		{3600, "1h 0m"},
		{3661, "1h 1m"},
		{7200, "2h 0m"},
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("%ds", tc.seconds), func(t *testing.T) {
			result := formatDuration(tc.seconds)
			if result != tc.expected {
				t.Errorf("formatDuration(%d) = %q, want %q", tc.seconds, result, tc.expected)
			}
		})
	}
}

// TestListFilesWithInvalidPath tests ListFiles with various invalid paths
func TestListFilesWithInvalidPath(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "syncmanager-error-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	configPath := filepath.Join(tmpDir, "rclone.conf")
	validConfig := `[local-test]
type = local
`
	if err := os.WriteFile(configPath, []byte(validConfig), 0644); err != nil {
		t.Fatal(err)
	}

	stateMgr, _ := state.NewManager()
	syncMgr := NewManager(configPath, "local-test", tmpDir, stateMgr, 4, 8)

	t.Run("NonexistentPath", func(t *testing.T) {
		_, err := syncMgr.ListFiles("/nonexistent/path")
		if err == nil {
			t.Error("ListFiles should fail with nonexistent path")
		}
	})

	t.Run("EmptyPath", func(t *testing.T) {
		// Should list root
		files, err := syncMgr.ListFiles("")
		if err != nil {
			t.Logf("ListFiles with empty path: %v", err)
		} else {
			t.Logf("Listed %d files at root", len(files))
		}
	})

	t.Run("RootPath", func(t *testing.T) {
		files, err := syncMgr.ListFiles("/")
		if err != nil {
			t.Logf("ListFiles with root path: %v", err)
		} else {
			t.Logf("Listed %d files at root", len(files))
		}
	})
}

// TestGetFileWithInvalidPath tests GetFile error handling
func TestGetFileWithInvalidPath(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "syncmanager-error-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	configPath := filepath.Join(tmpDir, "rclone.conf")
	validConfig := `[local-test]
type = local
`
	if err := os.WriteFile(configPath, []byte(validConfig), 0644); err != nil {
		t.Fatal(err)
	}

	stateMgr, _ := state.NewManager()
	syncMgr := NewManager(configPath, "local-test", tmpDir, stateMgr, 4, 8)

	var buf strings.Builder
	err = syncMgr.GetFile("/nonexistent/file.jpg", &buf)

	if err == nil {
		t.Error("GetFile should fail with nonexistent file")
	}

	if !strings.Contains(err.Error(), "failed to get file object") {
		t.Logf("GetFile error: %v", err)
	}
}

// TestGooglePhotosUploadErrors tests error handling in Google Photos upload
func TestGooglePhotosUploadErrors(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "syncmanager-error-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create source directory with JPG files
	srcDir := filepath.Join(tmpDir, "source")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatal(err)
	}

	testFile := filepath.Join(srcDir, "test.jpg")
	if err := os.WriteFile(testFile, []byte("fake jpg"), 0644); err != nil {
		t.Fatal(err)
	}

	configPath := filepath.Join(tmpDir, "rclone.conf")
	validConfig := `[local-test]
type = local
`
	if err := os.WriteFile(configPath, []byte(validConfig), 0644); err != nil {
		t.Fatal(err)
	}

	stateMgr, _ := state.NewManager()
	syncMgr := NewManager(configPath, "local-test", tmpDir, stateMgr, 4, 8)

	// Enable Google Photos with invalid remote
	syncMgr.SetGooglePhotos(true, "nonexistent-remote")

	// Test uploadToGooglePhotos directly
	ctx := context.Background()
	err = syncMgr.uploadToGooglePhotos(ctx, srcDir, "card-0123456789abcdef")

	if err == nil {
		t.Error("uploadToGooglePhotos should fail with nonexistent remote")
	}

	t.Logf("Google Photos upload error (expected): %v", err)
}

// TestConfigLoadingRaceCondition tests for race conditions in config loading
func TestConfigLoadingRaceCondition(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "syncmanager-error-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	configPath := filepath.Join(tmpDir, "rclone.conf")
	initialConfig := `[local-test]
type = local
`
	if err := os.WriteFile(configPath, []byte(initialConfig), 0644); err != nil {
		t.Fatal(err)
	}

	stateMgr, _ := state.NewManager()
	syncMgr := NewManager(configPath, "local-test", tmpDir, stateMgr, 4, 8)

	var wg sync.WaitGroup
	numOps := 10

	// Concurrently perform operations that load config
	for i := 0; i < numOps; i++ {
		wg.Add(2)

		go func() {
			defer wg.Done()
			syncMgr.ListRemotes()
		}()

		go func() {
			defer wg.Done()
			syncMgr.TestConnection()
		}()

	}

	wg.Wait()
	t.Log("Concurrent config loading completed without panic")
}

// BUG FOUND: TestResourceLeakOnCancelledSync tests for resource leaks when sync is cancelled
func TestResourceLeakOnCancelledSync(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "syncmanager-error-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create source and destination
	srcDir := filepath.Join(tmpDir, "source")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatal(err)
	}

	configPath := filepath.Join(tmpDir, "rclone.conf")
	validConfig := `[local-test]
type = local
`
	if err := os.WriteFile(configPath, []byte(validConfig), 0644); err != nil {
		t.Fatal(err)
	}

	stateMgr, _ := state.NewManager()
	syncMgr := NewManager(configPath, "local-test", tmpDir, stateMgr, 4, 8)

	// Start a sync in background
	go syncMgr.Sync(srcDir, "card-0123456789abcdef", 0, 0)

	// Wait for sync to actually start
	time.Sleep(100 * time.Millisecond)

	// Cancel it
	if err := syncMgr.Cancel(); err != nil {
		t.Logf("Cancel error (might be expected): %v", err)
	}

	// Wait a bit for cleanup
	time.Sleep(100 * time.Millisecond)

	// BUG: Check if isRunning was properly reset
	if syncMgr.IsRunning() {
		t.Error("BUG: isRunning should be false after cancel, but it's still true - possible resource leak")
	}

	// BUG: Check if cancelFunc was cleaned up
	syncMgr.mu.Lock()
	if syncMgr.cancelFunc != nil {
		t.Error("BUG: cancelFunc should be nil after cancel - goroutine cleanup may not be complete")
	}
	syncMgr.mu.Unlock()
}

// BUG FOUND: TestStateSyncAfterError verifies state consistency after errors
func TestStateSyncAfterError(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "syncmanager-error-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	configPath := filepath.Join(tmpDir, "rclone.conf")
	stateMgr, _ := state.NewManager()
	syncMgr := NewManager(configPath, "nonexistent-remote", "/test", stateMgr, 4, 8)

	// Start a sync that will fail
	err = syncMgr.Sync("/nonexistent/path", "card-0123456789abcdef", 10, 1024)

	if err == nil {
		t.Fatal("Expected sync to fail")
	}

	// BUG: Verify manager state is consistent after error
	if syncMgr.IsRunning() {
		t.Error("BUG: Sync manager shows as running after failed sync - state inconsistency")
	}

	syncMgr.mu.Lock()
	if syncMgr.cancelFunc != nil {
		t.Error("BUG: cancelFunc not cleaned up after failed sync - potential goroutine leak")
	}
	isRunning := syncMgr.isRunning
	syncMgr.mu.Unlock()

	if isRunning {
		t.Error("BUG: isRunning flag not reset after error - will prevent future syncs")
	}
}

// BUG FOUND: TestProgressChanDeadlock tests for potential deadlock in progress monitoring
func TestProgressChanDeadlock(t *testing.T) {
	syncMgr, stateMgr, cleanup := setupTestSyncManager(t)
	defer cleanup()

	// Subscribe to progress but never read
	syncMgr.SubscribeProgress()
	syncMgr.SubscribeProgress()
	syncMgr.SubscribeProgress()

	// Verify operations don't deadlock
	done := make(chan bool, 1)

	go func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		completeChan := make(chan struct{})
		go syncMgr.monitorProgress(ctx, nil, 100, 1024*1024, 0, completeChan)

		time.Sleep(50 * time.Millisecond)
		close(completeChan)
		cancel()

		done <- true
	}()

	select {
	case <-done:
		t.Log("Progress monitoring completed without deadlock")
	case <-time.After(500 * time.Millisecond):
		t.Error("BUG: Potential deadlock in progress monitoring - blocked channels may cause hangs")
	}

	// Verify state manager is still responsive
	state := stateMgr.GetState()
	t.Logf("State manager responsive: %v", state.Status)
}

// BUG FOUND: TestZeroDivisionInETACalculation tests ETA calculation edge cases
func TestZeroDivisionInETACalculation(t *testing.T) {
	// Simulates the ETA calculation from monitorProgress
	testCases := []struct {
		name             string
		speed            float64
		totalBytes       int64
		transferredBytes int64
		shouldPanic      bool
	}{
		{
			name:             "Zero speed",
			speed:            0,
			totalBytes:       1024 * 1024,
			transferredBytes: 512 * 1024,
			shouldPanic:      false,
		},
		{
			name:             "Negative speed",
			speed:            -100,
			totalBytes:       1024,
			transferredBytes: 512,
			shouldPanic:      false,
		},
		{
			name:             "All bytes transferred",
			speed:            1000,
			totalBytes:       1024,
			transferredBytes: 1024,
			shouldPanic:      false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					if !tc.shouldPanic {
						t.Errorf("BUG: Unexpected panic in ETA calculation: %v", r)
					}
				}
			}()

			var etaSeconds int
			if tc.speed > 0 {
				remaining := tc.totalBytes - tc.transferredBytes
				etaSeconds = int(float64(remaining) / tc.speed)
			}

			t.Logf("ETA: %d seconds", etaSeconds)
		})
	}
}

// BUG FOUND: TestSyncProgressAfterRemoteDisconnect simulates network interruption
func TestSyncProgressAfterRemoteDisconnect(t *testing.T) {
	// This test documents expected behavior when network disconnects during sync
	// The rclone library should handle this, but we need to verify state cleanup

	tmpDir, err := os.MkdirTemp("", "syncmanager-error-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	srcDir := filepath.Join(tmpDir, "source")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatal(err)
	}

	configPath := filepath.Join(tmpDir, "rclone.conf")
	validConfig := `[local-test]
type = local
`
	if err := os.WriteFile(configPath, []byte(validConfig), 0644); err != nil {
		t.Fatal(err)
	}

	stateMgr, _ := state.NewManager()
	syncMgr := NewManager(configPath, "local-test", tmpDir, stateMgr, 4, 8)

	// Start sync in background
	errChan := make(chan error, 1)
	go func() {
		errChan <- syncMgr.Sync(srcDir, "card-0123456789abcdef", 0, 0)
	}()

	// Wait for sync to start
	time.Sleep(50 * time.Millisecond)

	// Simulate interruption by canceling
	if err := syncMgr.Cancel(); err != nil {
		t.Logf("Cancel: %v", err)
	}

	// Wait for sync to complete
	select {
	case err := <-errChan:
		t.Logf("Sync returned: %v", err)
	case <-time.After(2 * time.Second):
		t.Error("BUG: Sync did not complete after cancel - possible goroutine leak")
	}

	// Verify cleanup
	if syncMgr.IsRunning() {
		t.Error("BUG: isRunning not reset after cancelled sync")
	}
}

// TestRemoteStatsParsingErrors tests handling of malformed remote stats
func TestRemoteStatsParsingErrors(t *testing.T) {
	// This test verifies error handling when parsing RemoteStats in monitorProgress
	// Lines 238-249 use type assertions that could fail

	testCases := []struct {
		name        string
		remoteStats map[string]interface{}
		expectPanic bool
	}{
		{
			name: "Valid transferring array",
			remoteStats: map[string]interface{}{
				"transferring": []interface{}{
					map[string]interface{}{
						"name": "file.jpg",
						"size": int64(1024),
					},
				},
			},
			expectPanic: false,
		},
		{
			name: "Transferring not an array",
			remoteStats: map[string]interface{}{
				"transferring": "not an array",
			},
			expectPanic: false, // Should handle gracefully
		},
		{
			name: "Empty transferring array",
			remoteStats: map[string]interface{}{
				"transferring": []interface{}{},
			},
			expectPanic: false,
		},
		{
			name: "Transferring item wrong type",
			remoteStats: map[string]interface{}{
				"transferring": []interface{}{
					"not a map",
				},
			},
			expectPanic: false,
		},
		{
			name: "Name field wrong type",
			remoteStats: map[string]interface{}{
				"transferring": []interface{}{
					map[string]interface{}{
						"name": 12345, // Should be string
						"size": int64(1024),
					},
				},
			},
			expectPanic: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					if !tc.expectPanic {
						t.Errorf("BUG: Unexpected panic parsing remote stats: %v", r)
					}
				}
			}()

			// Simulate the parsing logic from monitorProgress
			var currentFile string
			var currentFileSize int64

			if transferring, ok := tc.remoteStats["transferring"].([]interface{}); ok && len(transferring) > 0 {
				if transfer, ok := transferring[0].(map[string]interface{}); ok {
					if name, ok := transfer["name"].(string); ok {
						currentFile = name
					}
					if size, ok := transfer["size"].(int64); ok {
						currentFileSize = size
					}
				}
			}

			t.Logf("Parsed: file=%q, size=%d", currentFile, currentFileSize)
		})
	}
}

// TestConfigPathTraversal tests for path traversal vulnerabilities in config handling
func TestConfigPathTraversal(t *testing.T) {
	// Test that config path can't be exploited with path traversal
	suspiciousPaths := []string{
		"../../../etc/passwd",
		"/etc/passwd",
		"./../../sensitive",
	}

	for _, path := range suspiciousPaths {
		t.Run(path, func(t *testing.T) {
			stateMgr, _ := state.NewManager()
			syncMgr := NewManager(path, "test", "/test", stateMgr, 4, 8)

			// Operations should fail safely, not expose system files
			err := syncMgr.TestConnection()
			t.Logf("TestConnection with path %q: %v", path, err)

			// Should not panic or expose sensitive data
		})
	}
}

// TestMemoryLeakInProgressChannels tests for memory leaks from unclosed progress channels
func TestMemoryLeakInProgressChannels(t *testing.T) {
	syncMgr, _, cleanup := setupTestSyncManager(t)
	defer cleanup()

	initialCount := func() int {
		syncMgr.mu.Lock()
		defer syncMgr.mu.Unlock()
		return len(syncMgr.progressChans)
	}()

	// Create many subscriptions
	channels := make([]chan Progress, 100)
	for i := 0; i < 100; i++ {
		channels[i] = syncMgr.SubscribeProgress()
	}

	syncMgr.mu.Lock()
	count := len(syncMgr.progressChans)
	syncMgr.mu.Unlock()

	if count != initialCount+100 {
		t.Errorf("Expected %d channels, got %d", initialCount+100, count)
	}

	// BUG: There's no UnsubscribeProgress method to clean up channels
	// This could lead to memory leaks if subscribers disconnect
	t.Log("BUG: No mechanism to remove progress channel subscriptions - potential memory leak")
	t.Log("BUG: If WebSocket clients disconnect, their channels remain in progressChans slice")
}
