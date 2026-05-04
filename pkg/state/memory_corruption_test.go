package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// overrideStateDir sets the state directory for testing and returns a cleanup function
// that restores all global path variables.
func overrideStateDir(t *testing.T, dir string) {
	t.Helper()
	savedPermDir := PermDir
	savedStateFile := StateFile
	savedHistoryFile := HistoryFile
	savedMountDir := MountDir
	savedConfigFile := ConfigFile
	savedStateDir := stateDir
	t.Cleanup(func() {
		PermDir = savedPermDir
		StateFile = savedStateFile
		HistoryFile = savedHistoryFile
		MountDir = savedMountDir
		ConfigFile = savedConfigFile
		stateDir = savedStateDir
	})
	SetStateDir(dir)
}

// TestConcurrentMapAccessWithoutLocks tests for concurrent map access bugs
func TestConcurrentMapAccessWithoutLocks(t *testing.T) {
	tmpDir := t.TempDir()
	overrideStateDir(t, tmpDir)

	mgr, err := NewManager()
	if err != nil {
		t.Fatal(err)
	}

	t.Run("ConcurrentStateAccess", func(t *testing.T) {
		var wg sync.WaitGroup

		// Concurrent readers
		for i := 0; i < 50; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := 0; j < 100; j++ {
					_ = mgr.GetState()
				}
			}()
		}

		// Concurrent writers
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				for j := 0; j < 50; j++ {
					_ = mgr.SetStatus(SyncStatus([]string{"idle", "syncing", "success"}[j%3]))
				}
			}(i)
		}

		wg.Wait()
	})
}

// TestIntegerOverflowInSyncProgress tests for integer overflow in progress tracking
func TestIntegerOverflowInSyncProgress(t *testing.T) {
	tmpDir := t.TempDir()
	overrideStateDir(t, tmpDir)

	mgr, err := NewManager()
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name          string
		totalFiles    int64
		totalBytes    int64
		filesSynced   int64
		bytesSynced   int64
		shouldOverflow bool
	}{
		{
			name:        "MaxInt64Files",
			totalFiles:  9223372036854775807,
			totalBytes:  9223372036854775807,
			filesSynced: 9223372036854775807,
			bytesSynced: 9223372036854775807,
		},
		{
			name:        "NegativeValues",
			totalFiles:  -1,
			totalBytes:  -1,
			filesSynced: -1,
			bytesSynced: -1,
		},
		{
			name:        "SyncedExceedsTotal",
			totalFiles:  100,
			totalBytes:  1024,
			filesSynced: 200,
			bytesSynced: 2048,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := mgr.StartSync("card-test", tt.totalFiles, tt.totalBytes)
			if err != nil {
				t.Logf("StartSync failed: %v", err)
				return
			}

			err = mgr.UpdateSyncProgress(tt.filesSynced, tt.bytesSynced, "test.jpg", 1024, 1000.0, "1m")
			if err != nil {
				t.Logf("UpdateSyncProgress failed: %v", err)
			}

			state := mgr.GetState()
			if state.CurrentSync != nil {
				if state.CurrentSync.FilesSynced < 0 {
					t.Errorf("CRITICAL: FilesSynced overflowed to negative: %d", state.CurrentSync.FilesSynced)
				}
				if state.CurrentSync.BytesSynced < 0 {
					t.Errorf("CRITICAL: BytesSynced overflowed to negative: %d", state.CurrentSync.BytesSynced)
				}
			}
		})
	}
}

// TestMemoryLeakInListeners tests for memory leaks in listener management
func TestMemoryLeakInListeners(t *testing.T) {
	tmpDir := t.TempDir()
	overrideStateDir(t, tmpDir)

	mgr, err := NewManager()
	if err != nil {
		t.Fatal(err)
	}

	t.Run("UnsubscribedListenersAccumulate", func(t *testing.T) {
		// Subscribe many listeners without unsubscribing
		for i := 0; i < 1000; i++ {
			_ = mgr.Subscribe()
		}

		listenerCount := mgr.notifier.getListenerCount()

		if listenerCount != 1000 {
			t.Errorf("Expected 1000 listeners, got %d", listenerCount)
		}

		// This is a memory leak if listeners are not cleaned up
		t.Logf("WARNING: %d listeners registered without cleanup - potential memory leak", listenerCount)
	})

	t.Run("ChannelBufferGrowth", func(t *testing.T) {
		mgr2, _ := NewManager()

		ch := mgr2.Subscribe()

		// Generate many events without reading
		for i := 0; i < 1000; i++ {
			mgr2.SetStatus(StatusIdle)
		}

		// Check if channel is full
		if len(ch) >= 10 {
			t.Logf("WARNING: Channel buffer at capacity (%d events), may drop updates", len(ch))
		}
	})
}

// TestSliceBoundsInHistoryAccess tests for slice access violations
func TestSliceBoundsInHistoryAccess(t *testing.T) {
	tmpDir := t.TempDir()
	overrideStateDir(t, tmpDir)

	mgr, err := NewManager()
	if err != nil {
		t.Fatal(err)
	}

	t.Run("EmptyHistory", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("CRITICAL: Panic accessing empty history: %v", r)
			}
		}()

		result := mgr.FindLastSyncByCardID("nonexistent")
		if result != nil {
			t.Errorf("Expected nil for nonexistent card, got %+v", result)
		}
	})

	t.Run("ReverseIterationBounds", func(t *testing.T) {
		// Add some history
		mgr.history = []SyncRecord{
			{ID: "1", CardID: "card-1"},
			{ID: "2", CardID: "card-2"},
		}

		defer func() {
			if r := recover(); r != nil {
				t.Errorf("CRITICAL: Panic in reverse iteration: %v", r)
			}
		}()

		result := mgr.FindLastSyncByCardID("card-2")
		if result == nil || result.ID != "2" {
			t.Errorf("Expected ID '2', got %+v", result)
		}
	})
}

// TestJSONUnmarshalUnsafeTypeAssertions tests for unsafe type assertions
func TestJSONUnmarshalUnsafeTypeAssertions(t *testing.T) {
	tmpDir := t.TempDir()

	tests := []struct {
		name        string
		jsonData    string
		shouldError bool
	}{
		{
			name:        "InvalidJSON",
			jsonData:    `{invalid json}`,
			shouldError: true,
		},
		{
			name:        "TypeMismatch",
			jsonData:    `{"status": 123, "sdcard_mounted": "yes"}`,
			shouldError: true, // JSON decoder returns error for type mismatch
		},
		{
			name: "NullValues",
			jsonData: `{
				"status": null,
				"current_sync": null,
				"last_sync": null,
				"sdcard_mounted": null,
				"sdcard_path": null
			}`,
			shouldError: false,
		},
		{
			name: "ExtraFields",
			jsonData: `{
				"status": "idle",
				"unknown_field": "value",
				"malicious_field": {"nested": "data"}
			}`,
			shouldError: false, // Should ignore unknown fields
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stateFile := filepath.Join(tmpDir, tt.name+"-state.json")
			if err := os.WriteFile(stateFile, []byte(tt.jsonData), 0644); err != nil {
				t.Fatal(err)
			}

			var state CurrentState
			data, err := os.ReadFile(stateFile)
			if err != nil {
				t.Fatal(err)
			}

			err = json.Unmarshal(data, &state)
			if tt.shouldError && err == nil {
				t.Errorf("Expected error for invalid JSON, got nil")
			}
			if !tt.shouldError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}

// TestUnsafeStringOperations tests for unsafe string operations
func TestUnsafeStringOperations(t *testing.T) {
	t.Run("ErrorStringConversion", func(t *testing.T) {
		// Test error.Error() conversions
		testErr := &testError{msg: "test error with null\x00byte"}

		record := SyncRecord{
			Status: "error",
			Error:  testErr.Error(),
		}

		// Verify null bytes don't cause issues
		if strings.Contains(record.Error, "\x00") {
			t.Logf("WARNING: Error string contains null byte: %q", record.Error)
		}

		// Marshal to JSON
		data, err := json.Marshal(record)
		if err != nil {
			t.Errorf("Failed to marshal record with error: %v", err)
		}

		// Verify JSON is valid
		var decoded SyncRecord
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Errorf("Failed to unmarshal: %v", err)
		}
	})
}

type testError struct {
	msg string
}

func (e *testError) Error() string {
	return e.msg
}

// TestConcurrentNotifyListeners tests for race conditions in notification
func TestConcurrentNotifyListeners(t *testing.T) {
	tmpDir := t.TempDir()
	overrideStateDir(t, tmpDir)

	mgr, err := NewManager()
	if err != nil {
		t.Fatal(err)
	}

	t.Run("ConcurrentSubscribeAndNotify", func(t *testing.T) {
		var wg sync.WaitGroup

		// Concurrent subscribe/unsubscribe
		for i := 0; i < 20; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				ch := mgr.Subscribe()
				time.Sleep(10 * time.Millisecond)
				mgr.Unsubscribe(ch)
			}()
		}

		// Concurrent notifications
		for i := 0; i < 50; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := 0; j < 10; j++ {
					mgr.notifyListeners()
				}
			}()
		}

		wg.Wait()
	})
}

// TestAtomicWriteRaceCondition tests for race conditions in file writes
func TestAtomicWriteRaceCondition(t *testing.T) {
	tmpDir := t.TempDir()
	overrideStateDir(t, tmpDir)

	mgr, err := NewManager()
	if err != nil {
		t.Fatal(err)
	}

	t.Run("ConcurrentSaves", func(t *testing.T) {
		var wg sync.WaitGroup

		// Many concurrent saves
		for i := 0; i < 100; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				mgr.mu.Lock()
				mgr.currentState.Status = SyncStatus("test")
				mgr.mu.Unlock()
				_ = mgr.save()
			}(i)
		}

		wg.Wait()

		// Verify file is not corrupted
		data, err := os.ReadFile(StateFile)
		if err != nil {
			t.Errorf("Failed to read state file: %v", err)
		}

		var state CurrentState
		if err := json.Unmarshal(data, &state); err != nil {
			t.Errorf("CRITICAL: State file corrupted after concurrent writes: %v", err)
		}
	})
}

// TestDeepCopyVsShallowCopy tests for data race in GetHistory copy
func TestDeepCopyVsShallowCopy(t *testing.T) {
	tmpDir := t.TempDir()
	overrideStateDir(t, tmpDir)

	mgr, err := NewManager()
	if err != nil {
		t.Fatal(err)
	}

	// Add some history
	mgr.history = []SyncRecord{
		{ID: "1", CardID: "card-1", Error: "original error"},
	}

	// Get copy
	historyCopy := mgr.GetHistory()

	// Modify copy
	historyCopy[0].Error = "modified error"

	// Check if original is affected
	mgr.mu.RLock()
	originalError := mgr.history[0].Error
	mgr.mu.RUnlock()

	if originalError == "modified error" {
		t.Errorf("CRITICAL: GetHistory returns shallow copy, data race possible")
	}
}

// TestHistorySliceAppendRace tests for race in history append
func TestHistorySliceAppendRace(t *testing.T) {
	tmpDir := t.TempDir()
	overrideStateDir(t, tmpDir)

	mgr, err := NewManager()
	if err != nil {
		t.Fatal(err)
	}

	t.Run("ConcurrentFinishSync", func(t *testing.T) {
		var wg sync.WaitGroup

		// Start multiple syncs and finish them concurrently
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()

				cardID := "card-" + string(rune(id))
				_, err := mgr.StartSync(cardID, 100, 1024)
				if err != nil {
					return
				}

				time.Sleep(10 * time.Millisecond)

				err = mgr.FinishSync(true, nil)
				if err != nil {
					t.Logf("FinishSync error: %v", err)
				}
			}(i)
		}

		wg.Wait()

		// Verify history integrity
		history := mgr.GetHistory()
		if len(history) == 0 {
			t.Error("Expected some history entries")
		}
	})
}

// TestNilPointerDereference tests for nil pointer dereferences
func TestNilPointerDereference(t *testing.T) {
	tmpDir := t.TempDir()
	overrideStateDir(t, tmpDir)

	mgr, err := NewManager()
	if err != nil {
		t.Fatal(err)
	}

	t.Run("UpdateProgressWithoutStartSync", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("CRITICAL: Panic when updating progress without active sync: %v", r)
			}
		}()

		// This should handle gracefully (check for nil CurrentSync)
		err := mgr.UpdateSyncProgress(10, 1024, "test.jpg", 100, 1000.0, "1m")
		if err != nil {
			t.Logf("UpdateSyncProgress correctly returned error: %v", err)
		}
	})

	t.Run("FinishSyncWithoutStart", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("CRITICAL: Panic when finishing sync without start: %v", r)
			}
		}()

		err := mgr.FinishSync(true, nil)
		if err == nil {
			t.Error("Expected error when finishing sync without starting")
		}
	})
}

// TestProgressSaveThrottling tests the throttling mechanism for memory corruption
func TestProgressSaveThrottling(t *testing.T) {
	tmpDir := t.TempDir()
	overrideStateDir(t, tmpDir)

	mgr, err := NewManager()
	if err != nil {
		t.Fatal(err)
	}

	// Set very short throttle for testing
	mgr.progressSaveDelay = 10 * time.Millisecond

	_, err = mgr.StartSync("card-test", 100, 1024)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("RapidProgressUpdates", func(t *testing.T) {
		var wg sync.WaitGroup

		// Rapid concurrent progress updates
		for i := 0; i < 100; i++ {
			wg.Add(1)
			go func(val int64) {
				defer wg.Done()
				_ = mgr.UpdateSyncProgress(val, val*1024, "file.jpg", 1024, 1000.0, "1m")
			}(int64(i))
		}

		wg.Wait()

		// Verify state file is not corrupted
		data, err := os.ReadFile(StateFile)
		if err != nil {
			t.Errorf("Failed to read state file: %v", err)
		}

		var state CurrentState
		if err := json.Unmarshal(data, &state); err != nil {
			t.Errorf("CRITICAL: State file corrupted: %v", err)
		}
	})
}

// TestUnsubscribeRaceCondition tests race in Unsubscribe
func TestUnsubscribeRaceCondition(t *testing.T) {
	tmpDir := t.TempDir()
	overrideStateDir(t, tmpDir)

	mgr, err := NewManager()
	if err != nil {
		t.Fatal(err)
	}

	t.Run("ConcurrentUnsubscribe", func(t *testing.T) {
		ch := mgr.Subscribe()

		var wg sync.WaitGroup

		// Multiple goroutines trying to unsubscribe the same channel
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer func() {
					if r := recover(); r != nil {
						t.Errorf("CRITICAL: Panic in concurrent unsubscribe: %v", r)
					}
				}()
				mgr.Unsubscribe(ch)
			}()
		}

		wg.Wait()
	})
}

// TestChannelCloseAfterUnsubscribe tests for double-close panics
func TestChannelCloseAfterUnsubscribe(t *testing.T) {
	tmpDir := t.TempDir()
	oldPermDir := PermDir
	oldStateFile := StateFile
	oldHistoryFile := HistoryFile
	oldMountDir := MountDir
	oldConfigFile := ConfigFile
	oldStateDir := stateDir
	defer func() {
		PermDir = oldPermDir
		StateFile = oldStateFile
		HistoryFile = oldHistoryFile
		MountDir = oldMountDir
		ConfigFile = oldConfigFile
		stateDir = oldStateDir
	}()

	SetStateDir(tmpDir)

	mgr, err := NewManager()
	if err != nil {
		t.Fatal(err)
	}

	ch := mgr.Subscribe()
	mgr.Unsubscribe(ch)

	// Try to read from closed channel
	_, ok := <-ch
	if ok {
		t.Error("Expected closed channel")
	}

	// Try to close again (should panic if not protected)
	defer func() {
		if r := recover(); r == nil {
			// Good - either it's protected or Go handles it
		} else {
			t.Logf("Closing already-closed channel panics: %v", r)
		}
	}()
	close(ch)
}
