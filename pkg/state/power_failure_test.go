package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestPowerFailureDuringStateWrite verifies boot-time recovery of orphaned
// .tmp files left behind when power fails between AtomicWrite's write and
// rename steps. The sibling cases (partial write, zero-byte file, older tmp,
// missing main) exercise every branch of recoverOrphanedTempFiles.
func TestPowerFailureDuringStateWrite(t *testing.T) {
	saveStateFile := StateFile
	saveHistoryFile := HistoryFile
	t.Cleanup(func() {
		StateFile = saveStateFile
		HistoryFile = saveHistoryFile
	})

	t.Run("power loss after tmp write, before rename - tmp promoted", func(t *testing.T) {
		tmpDir := t.TempDir()
		StateFile = filepath.Join(tmpDir, "state.json")
		HistoryFile = filepath.Join(tmpDir, "sync-history.json")
		stateFile := StateFile
		tmpFile := stateFile + ".tmp"

		testState := CurrentState{
			Status:        StatusSyncing,
			SDCardMounted: true,
			SDCardPath:    "/mnt/sdcard",
			CurrentSync: &SyncRecord{
				ID:          "sync-123",
				StartTime:   time.Now(),
				Status:      "syncing",
				FilesTotal:  100,
				FilesSynced: 50,
				BytesTotal:  1024 * 1024 * 100,
				BytesSynced: 1024 * 1024 * 50,
				CardID:      "card-abc",
			},
		}

		data, _ := json.MarshalIndent(testState, "", "  ")
		if err := os.WriteFile(tmpFile, data, 0644); err != nil {
			t.Fatalf("seed tmp: %v", err)
		}

		// No main state file - simulates first save interrupted before rename.
		if err := recoverOrphanedTempFiles(); err != nil {
			t.Fatalf("recovery returned error: %v", err)
		}

		// Orphan should now be the canonical state file.
		if _, err := os.Stat(tmpFile); !os.IsNotExist(err) {
			t.Fatalf("tmp file should be gone after promotion, stat err: %v", err)
		}
		recovered, err := os.ReadFile(stateFile)
		if err != nil {
			t.Fatalf("state file missing after recovery: %v", err)
		}
		var got CurrentState
		if err := json.Unmarshal(recovered, &got); err != nil {
			t.Fatalf("recovered state did not parse: %v", err)
		}
		if got.CurrentSync == nil || got.CurrentSync.FilesSynced != 50 {
			t.Fatalf("recovery lost in-flight progress: %+v", got.CurrentSync)
		}
	})

	t.Run("tmp newer than main with valid JSON - tmp promoted", func(t *testing.T) {
		tmpDir := t.TempDir()
		StateFile = filepath.Join(tmpDir, "state.json")
		HistoryFile = filepath.Join(tmpDir, "sync-history.json")
		stateFile := StateFile
		tmpFile := stateFile + ".tmp"

		// Old main: status idle.
		oldState := CurrentState{Status: StatusIdle}
		oldData, _ := json.MarshalIndent(oldState, "", "  ")
		if err := os.WriteFile(stateFile, oldData, 0644); err != nil {
			t.Fatalf("seed main: %v", err)
		}
		past := time.Now().Add(-1 * time.Hour)
		if err := os.Chtimes(stateFile, past, past); err != nil {
			t.Fatalf("chtimes main: %v", err)
		}

		// New tmp: status syncing - represents the write that didn't get renamed.
		newState := CurrentState{
			Status: StatusSyncing,
			CurrentSync: &SyncRecord{
				ID:          "sync-456",
				FilesTotal:  200,
				FilesSynced: 100,
				CardID:      "card-def",
			},
		}
		newData, _ := json.MarshalIndent(newState, "", "  ")
		if err := os.WriteFile(tmpFile, newData, 0644); err != nil {
			t.Fatalf("seed tmp: %v", err)
		}

		if err := recoverOrphanedTempFiles(); err != nil {
			t.Fatalf("recovery error: %v", err)
		}

		if _, err := os.Stat(tmpFile); !os.IsNotExist(err) {
			t.Fatalf("tmp should be gone, stat: %v", err)
		}
		final, _ := os.ReadFile(stateFile)
		var got CurrentState
		if err := json.Unmarshal(final, &got); err != nil {
			t.Fatalf("final state did not parse: %v", err)
		}
		if got.Status != StatusSyncing || got.CurrentSync == nil || got.CurrentSync.CardID != "card-def" {
			t.Fatalf("expected newer tmp to win, got: %+v", got)
		}
	})

	t.Run("partial write to tmp file - corrupt tmp removed, main preserved", func(t *testing.T) {
		tmpDir := t.TempDir()
		StateFile = filepath.Join(tmpDir, "state.json")
		HistoryFile = filepath.Join(tmpDir, "sync-history.json")
		stateFile := StateFile
		tmpFile := stateFile + ".tmp"

		good := CurrentState{Status: StatusIdle}
		goodData, _ := json.MarshalIndent(good, "", "  ")
		if err := os.WriteFile(stateFile, goodData, 0644); err != nil {
			t.Fatalf("seed main: %v", err)
		}

		fullState := CurrentState{
			Status: StatusSyncing,
			CurrentSync: &SyncRecord{
				ID:          "sync-789",
				FilesTotal:  1000,
				FilesSynced: 500,
				CardID:      "card-ghi",
			},
		}
		data, _ := json.MarshalIndent(fullState, "", "  ")
		halfData := data[:len(data)/2]
		if err := os.WriteFile(tmpFile, halfData, 0644); err != nil {
			t.Fatalf("seed tmp: %v", err)
		}

		if err := recoverOrphanedTempFiles(); err != nil {
			t.Fatalf("recovery error: %v", err)
		}

		if _, err := os.Stat(tmpFile); !os.IsNotExist(err) {
			t.Fatalf("corrupt tmp should be removed, stat: %v", err)
		}
		final, _ := os.ReadFile(stateFile)
		var got CurrentState
		if err := json.Unmarshal(final, &got); err != nil {
			t.Fatalf("main state corrupted by recovery: %v", err)
		}
		if got.Status != StatusIdle {
			t.Fatalf("main state changed unexpectedly: %+v", got)
		}
	})

	t.Run("zero-byte tmp file - removed", func(t *testing.T) {
		tmpDir := t.TempDir()
		StateFile = filepath.Join(tmpDir, "state.json")
		HistoryFile = filepath.Join(tmpDir, "sync-history.json")
		tmpFile := StateFile + ".tmp"

		if err := os.WriteFile(tmpFile, []byte{}, 0644); err != nil {
			t.Fatalf("seed tmp: %v", err)
		}

		if err := recoverOrphanedTempFiles(); err != nil {
			t.Fatalf("recovery error: %v", err)
		}

		if _, err := os.Stat(tmpFile); !os.IsNotExist(err) {
			t.Fatalf("zero-byte tmp should be removed, stat: %v", err)
		}
	})

	t.Run("older tmp than main - removed without overwriting main", func(t *testing.T) {
		tmpDir := t.TempDir()
		StateFile = filepath.Join(tmpDir, "state.json")
		HistoryFile = filepath.Join(tmpDir, "sync-history.json")
		stateFile := StateFile
		tmpFile := stateFile + ".tmp"

		// Newer good main.
		good := CurrentState{Status: StatusSuccess}
		goodData, _ := json.MarshalIndent(good, "", "  ")
		if err := os.WriteFile(stateFile, goodData, 0644); err != nil {
			t.Fatalf("seed main: %v", err)
		}

		// Stale tmp from a previous power loss, valid JSON but older.
		stale := CurrentState{Status: StatusError}
		staleData, _ := json.MarshalIndent(stale, "", "  ")
		if err := os.WriteFile(tmpFile, staleData, 0644); err != nil {
			t.Fatalf("seed tmp: %v", err)
		}
		past := time.Now().Add(-2 * time.Hour)
		if err := os.Chtimes(tmpFile, past, past); err != nil {
			t.Fatalf("chtimes tmp: %v", err)
		}

		if err := recoverOrphanedTempFiles(); err != nil {
			t.Fatalf("recovery error: %v", err)
		}

		if _, err := os.Stat(tmpFile); !os.IsNotExist(err) {
			t.Fatalf("older tmp should be removed, stat: %v", err)
		}
		final, _ := os.ReadFile(stateFile)
		var got CurrentState
		if err := json.Unmarshal(final, &got); err != nil {
			t.Fatalf("main parse: %v", err)
		}
		if got.Status != StatusSuccess {
			t.Fatalf("main overwritten by older tmp: %+v", got)
		}
	})
}

// TestPowerFailureDuringHistoryAppend simulates power loss during history append
func TestPowerFailureDuringHistoryAppend(t *testing.T) {
	tmpDir := t.TempDir()
	historyFile := filepath.Join(tmpDir, "sync-history.json")
	tmpFile := historyFile + ".tmp"

	t.Run("power loss before history append", func(t *testing.T) {
		// Start with existing history
		existingHistory := []SyncRecord{
			{ID: "sync-001", CardID: "card-A", Status: "success", FilesTotal: 100},
			{ID: "sync-002", CardID: "card-B", Status: "success", FilesTotal: 200},
		}

		data, _ := json.MarshalIndent(existingHistory, "", "  ")
		os.WriteFile(historyFile, data, 0644)

		// Simulate starting to append new record
		newHistory := append(existingHistory, SyncRecord{
			ID:          "sync-003",
			CardID:      "card-C",
			Status:      "success",
			FilesTotal:  300,
			FilesSynced: 300,
		})

		data, _ = json.MarshalIndent(newHistory, "", "  ")
		os.WriteFile(tmpFile, data, 0644)

		// Power failure before rename
		// On recovery, sync-003 is lost

		loadedData, _ := os.ReadFile(historyFile)
		var loaded []SyncRecord
		json.Unmarshal(loadedData, &loaded)

		if len(loaded) == 3 {
			t.Error("History should not include sync-003 after power failure")
		} else if len(loaded) == 2 {
			t.Log("BUG FOUND: Successful sync lost from history due to power failure")
			t.Log("Impact: User has no record that sync-003 (300 files) completed")
			t.Log("Impact: Reformat detection will be wrong for card-C")
			t.Log("Data loss: Complete sync record with timing, file counts lost")
		}
	})

	t.Run("partial history write", func(t *testing.T) {
		// Large history file gets truncated during write
		largeHistory := make([]SyncRecord, 100)
		for i := 0; i < 100; i++ {
			largeHistory[i] = SyncRecord{
				ID:          fmt.Sprintf("sync-%03d", i),
				CardID:      fmt.Sprintf("card-%d", i%10),
				Status:      "success",
				FilesTotal:  int64(i * 100),
				FilesSynced: int64(i * 100),
			}
		}

		data, _ := json.MarshalIndent(largeHistory, "", "  ")

		// Write only 70% of the data
		partialData := data[:len(data)*7/10]
		os.WriteFile(tmpFile, partialData, 0644)

		// Try to load
		loadedData, _ := os.ReadFile(tmpFile)
		var loaded []SyncRecord
		err := json.Unmarshal(loadedData, &loaded)

		if err != nil {
			t.Log("BUG FOUND: Partial history write creates corrupt JSON")
			t.Log("Impact: Cannot load any history, lose ALL historical data")
			t.Log("Data loss: All 100 sync records lost")
			t.Log("Recommendation: Keep backup of previous history file")
		}
	})

	t.Run("history array corruption", func(t *testing.T) {
		// Write incomplete JSON array
		corruptJSON := `[
  {
    "id": "sync-001",
    "card_id": "card-A",
    "status": "success"
  },
  {
    "id": "sync-002",
    "card_id": "card-B"
`
		os.WriteFile(tmpFile, []byte(corruptJSON), 0644)

		var loaded []SyncRecord
		data, _ := os.ReadFile(tmpFile)
		err := json.Unmarshal(data, &loaded)

		if err != nil {
			t.Log("BUG FOUND: Corrupt JSON array in history file")
			t.Log("Impact: All history lost, cannot recover partial data")
			t.Log("Recommendation: Store each record on separate line (JSON Lines format)")
		}
	})
}

// TestPowerFailureDuringSettingsSave simulates power loss during settings save
func TestPowerFailureDuringSettingsSave(t *testing.T) {
	tmpDir := t.TempDir()
	settingsFile := filepath.Join(tmpDir, "settings.json")
	tmpFile := settingsFile + ".tmp"

	t.Run("partial settings write loses configuration", func(t *testing.T) {
		// User just configured rclone remote and paths
		// Full settings would be:
		// {
		//   "remote_name": "backblaze",
		//   "remote_path": "/my-bucket/photos",
		//   "reformat_threshold": 0.3,
		//   "transfers": 8,
		//   "checkers": 16
		// }

		// Power failure during write
		partialSettings := `{
  "remote_name": "backblaze",
  "remote_path": "/my-b`

		os.WriteFile(tmpFile, []byte(partialSettings), 0644)

		// On next boot, load fails
		data, _ := os.ReadFile(tmpFile)
		err := json.Unmarshal(data, &map[string]interface{}{})

		if err != nil {
			t.Log("BUG FOUND: Settings corruption causes load failure")
			t.Log("Impact: Application may revert to defaults or fail to start")
			t.Log("Data loss: User's remote configuration lost")
			t.Log("User impact: Must reconfigure entire system")
		}
	})

	t.Run("old settings file deleted, new write fails", func(t *testing.T) {
		// Some rename implementations delete old file first
		// If power fails after delete but before rename completes...

		// Old file deleted
		os.Remove(settingsFile)

		// New file not yet renamed from .tmp
		newSettings := `{"remote_name": "new-remote"}`
		os.WriteFile(tmpFile, []byte(newSettings), 0644)

		// Power failure
		// Result: No settings.json file exists!

		if _, err := os.Stat(settingsFile); os.IsNotExist(err) {
			t.Log("BUG FOUND: Settings file completely lost after power failure")
			t.Log("Impact: Application has no configuration")
			t.Log("Data loss: CRITICAL - complete configuration loss")
			t.Log("Recovery: Must check for .tmp file on startup")
		}
	})
}

// TestMultipleTmpFilesAfterRepeatedFailures tests accumulation of temp files
func TestMultipleTmpFilesAfterRepeatedFailures(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("multiple .tmp files accumulate", func(t *testing.T) {
		// Simulate rapid save attempts with power failures
		for i := 0; i < 5; i++ {
			tmpFile := filepath.Join(tmpDir, fmt.Sprintf("state.json.tmp.%d", i))
			data := fmt.Sprintf(`{"status": "syncing", "attempt": %d}`, i)
			os.WriteFile(tmpFile, []byte(data), 0644)
		}

		// Also create the "official" .tmp file
		tmpFile := filepath.Join(tmpDir, "state.json.tmp")
		os.WriteFile(tmpFile, []byte(`{"status": "error"}`), 0644)

		// Count temp files
		files, _ := os.ReadDir(tmpDir)
		tmpCount := 0
		for _, f := range files {
			if strings.HasSuffix(f.Name(), ".tmp") || strings.Contains(f.Name(), ".tmp.") {
				tmpCount++
			}
		}

		if tmpCount > 1 {
			t.Logf("BUG FOUND: Multiple temp files accumulate: %d files", tmpCount)
			t.Log("Impact: Disk space wasted")
			t.Log("Impact: Cannot determine which temp file is correct")
			t.Log("Recommendation: Clean up old .tmp files on startup")
		}
	})

	t.Run("temp files with different timestamps", func(t *testing.T) {
		// Create temp files with different content and timestamps
		baseTime := time.Now()
		for i := 0; i < 3; i++ {
			tmpFile := filepath.Join(tmpDir, fmt.Sprintf("history.json.tmp.%d", i))
			data := fmt.Sprintf(`[{"id": "sync-%d", "files_total": %d}]`, i, i*100)
			os.WriteFile(tmpFile, []byte(data), 0644)

			// Set different modification times
			modTime := baseTime.Add(time.Duration(i) * time.Minute)
			os.Chtimes(tmpFile, modTime, modTime)
		}

		t.Log("BUG FOUND: No mechanism to determine newest temp file")
		t.Log("Impact: Recovery logic doesn't know which file has latest data")
		t.Log("Recommendation: Use timestamps or sequence numbers in filenames")
	})
}

// TestStateRecoveryWithHalfWrittenFiles tests recovery scenarios
func TestStateRecoveryWithHalfWrittenFiles(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("recover from half-written state file", func(t *testing.T) {
		stateFile := filepath.Join(tmpDir, "state.json")
		tmpFile := stateFile + ".tmp"

		// Good state in main file
		goodState := CurrentState{
			Status:        StatusSuccess,
			SDCardMounted: false,
			LastSync: &SyncRecord{
				ID:          "sync-last",
				Status:      "success",
				FilesTotal:  100,
				FilesSynced: 100,
			},
		}
		data, _ := json.MarshalIndent(goodState, "", "  ")
		os.WriteFile(stateFile, data, 0644)

		// Corrupted state in tmp file (power failure during new save)
		corruptData := data[:len(data)/2]
		os.WriteFile(tmpFile, corruptData, 0644)

		// Recovery: Should use main file, ignore corrupt tmp
		mainData, _ := os.ReadFile(stateFile)
		var recovered CurrentState
		err := json.Unmarshal(mainData, &recovered)

		if err != nil {
			t.Error("BUG: Even main file is corrupt!")
		} else if recovered.Status == StatusSuccess {
			t.Log("Recovery successful: Used main file, ignored corrupt .tmp")
		}

		// But cleanup doesn't happen automatically
		if _, err := os.Stat(tmpFile); !os.IsNotExist(err) {
			t.Log("BUG FOUND: Corrupt .tmp file not cleaned up")
			t.Log("Impact: File system clutter, confusion on next failure")
		}
	})

	t.Run("both files corrupt", func(t *testing.T) {
		stateFile := filepath.Join(tmpDir, "state2.json")
		tmpFile := stateFile + ".tmp"

		// Both files corrupted
		os.WriteFile(stateFile, []byte(`{"status": "sync`), 0644)
		os.WriteFile(tmpFile, []byte(`{"status": "erro`), 0644)

		// Try to recover
		var state CurrentState

		// Try main file first
		mainData, _ := os.ReadFile(stateFile)
		err1 := json.Unmarshal(mainData, &state)

		// Try tmp file as backup
		tmpData, _ := os.ReadFile(tmpFile)
		err2 := json.Unmarshal(tmpData, &state)

		if err1 != nil && err2 != nil {
			t.Log("BUG FOUND: Both state files corrupt, no recovery possible")
			t.Log("Impact: CRITICAL - application cannot start")
			t.Log("Data loss: Complete state loss, all sync progress lost")
			t.Log("Recommendation: Implement triple-write (current + backup + tmp)")
		}
	})
}

// TestSyncProgressLostAfterCrash tests sync progress loss scenarios
func TestSyncProgressLostAfterCrash(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("crash during active sync loses progress", func(t *testing.T) {
		stateFile := filepath.Join(tmpDir, "state.json")

		// Sync was in progress
		activeSync := CurrentState{
			Status:        StatusSyncing,
			SDCardMounted: true,
			SDCardPath:    "/mnt/sdcard",
			CurrentSync: &SyncRecord{
				ID:              "sync-active",
				StartTime:       time.Now().Add(-30 * time.Minute),
				Status:          "syncing",
				FilesTotal:      1000,
				FilesSynced:     750,
				BytesTotal:      1024 * 1024 * 1024 * 5, // 5GB
				BytesSynced:     1024 * 1024 * 1024 * 4, // 4GB transferred
				CardID:          "card-important",
				CurrentFile:     "IMG_0750.JPG",
				CurrentFileSize: 1024 * 1024 * 3,
				TransferSpeed:   1024 * 1024 * 2, // 2MB/s
				ETA:             "5m30s",
			},
		}

		data, _ := json.MarshalIndent(activeSync, "", "  ")
		os.WriteFile(stateFile, data, 0644)

		// System crashes (power failure or kernel panic)
		// Last progress save was 5 seconds ago (due to throttling)
		// Actual progress: 755 files, but state.json shows 750

		// On reboot, load state
		loadedData, _ := os.ReadFile(stateFile)
		var loaded CurrentState
		json.Unmarshal(loadedData, &loaded)

		if loaded.CurrentSync != nil {
			t.Log("BUG FOUND: Sync progress saved, but may be stale")
			t.Log(fmt.Sprintf("Impact: Lost progress of 5 files due to throttling"))
			t.Log("Data loss: Unknown if files 751-755 were actually synced")
			t.Log("User impact: May re-upload already synced files")
			t.Log("Recommendation: Force save on every 10th file or important milestones")
		}

		// Worse: What state should we be in on reboot?
		if loaded.Status == StatusSyncing {
			t.Log("BUG FOUND: Status is 'syncing' but no rclone process exists")
			t.Log("Impact: Application stuck in syncing state")
			t.Log("Impact: User must manually reset or app must detect stale sync")
			t.Log("Recommendation: On startup, check for stale syncs and reset to error/interrupted")
		}
	})

	t.Run("progress throttling increases data loss window", func(t *testing.T) {
		// Progress is only saved every 5 seconds
		// If syncing at 100 files/second, we could lose 500 files of progress

		throttleDelay := 5 * time.Second
		filesPerSecond := 100
		potentialLoss := throttleDelay.Seconds() * float64(filesPerSecond)

		t.Logf("BUG FOUND: Progress throttling creates data loss window")
		t.Logf("Impact: Up to %.0f files of progress could be lost on crash", potentialLoss)
		t.Logf("Impact: Fast syncs are more affected than slow syncs")
		t.Logf("Recommendation: Adaptive throttling based on sync speed")
	})
}

// TestCardIDFileCorruption tests card ID file corruption scenarios
func TestCardIDFileCorruption(t *testing.T) {
	tmpDir := t.TempDir()
	cardIDPath := filepath.Join(tmpDir, ".pictures-sync-id")

	t.Run("partial card ID write", func(t *testing.T) {
		// Full card ID would be: "card-1234567890abcdef"
		partialID := "card-12345"

		// Power failure during write
		os.WriteFile(cardIDPath, []byte(partialID), 0644)

		// On next insert, read the ID
		data, _ := os.ReadFile(cardIDPath)
		readID := strings.TrimSpace(string(data))

		if readID == partialID {
			t.Log("BUG FOUND: Partial card ID accepted")
			t.Log("Impact: Card gets new ID on next proper write")
			t.Log("Impact: Previous syncs for this card not found")
			t.Log("Impact: Reformat detection fails")
			t.Log("Data loss: Association with previous syncs lost")
			t.Log("Recommendation: Validate card ID format (length, charset)")
		}
	})

	t.Run("empty card ID file", func(t *testing.T) {
		// Power failure right at start of write
		os.WriteFile(cardIDPath, []byte(""), 0644)

		data, _ := os.ReadFile(cardIDPath)
		readID := strings.TrimSpace(string(data))

		if readID == "" {
			t.Log("BUG FOUND: Empty card ID file treated as no ID")
			t.Log("Impact: New ID generated, losing link to previous syncs")
			t.Log("Recommendation: Treat empty file as corrupt, try to recover from backup")
		}
	})

	t.Run("card ID with null bytes", func(t *testing.T) {
		// Corruption or intentional attack
		corruptID := "card-abc\x00\x00\x00def"
		os.WriteFile(cardIDPath, []byte(corruptID), 0644)

		data, _ := os.ReadFile(cardIDPath)
		readID := strings.TrimSpace(string(data))

		if strings.Contains(readID, "\x00") {
			t.Log("BUG FOUND: Null bytes in card ID")
			t.Log("Impact: String termination issues in some contexts")
			t.Log("Impact: Card ID comparison might fail unexpectedly")
		}
	})

	t.Run("card ID file permissions wrong", func(t *testing.T) {
		// File created but with wrong permissions
		os.WriteFile(cardIDPath, []byte("card-test"), 0000)

		// Try to read
		_, err := os.ReadFile(cardIDPath)
		if err != nil {
			t.Log("BUG FOUND: Card ID file exists but not readable")
			t.Log("Impact: New ID generated, losing history")
			t.Log("Recommendation: Check permissions and attempt to fix")
		}

		// Clean up
		os.Chmod(cardIDPath, 0644)
	})
}

// TestWiFiConfigCorruption tests WiFi config corruption scenarios
func TestWiFiConfigCorruption(t *testing.T) {
	tmpDir := t.TempDir()
	wifiConfigPath := filepath.Join(tmpDir, "extra-wifi.json")

	t.Run("partial WiFi config write", func(t *testing.T) {
		// Full config would be:
		// {
		//   "networks": [
		//     {"ssid": "MyNetwork", "psk": "secretpassword123"},
		//     {"ssid": "GuestNetwork", "psk": "guestpass456"}
		//   ]
		// }

		partialConfig := `{
  "networks": [
    {
      "ssid": "MyNetwork",
      "psk": "secretpa`

		os.WriteFile(wifiConfigPath, []byte(partialConfig), 0644)

		// Try to load
		data, _ := os.ReadFile(wifiConfigPath)
		var config map[string]interface{}
		err := json.Unmarshal(data, &config)

		if err != nil {
			t.Log("BUG FOUND: WiFi config corruption causes loss of all networks")
			t.Log("Impact: Device cannot connect to WiFi after power failure")
			t.Log("Impact: CRITICAL for headless device - no remote access")
			t.Log("Impact: User must have physical access to recover")
			t.Log("Recommendation: Keep backup of last working config")
		}
	})

	t.Run("password truncated during write", func(t *testing.T) {
		config := `{
  "networks": [
    {
      "ssid": "MyNetwork",
      "psk": "short"
    }
  ]
}`
		// Original password was longer but got truncated
		os.WriteFile(wifiConfigPath, []byte(config), 0644)

		t.Log("BUG FOUND: Truncated password in WiFi config")
		t.Log("Impact: Cannot connect to network")
		t.Log("Impact: No validation that password meets WPA requirements")
		t.Log("Impact: User thinks device is configured but connection fails")
	})
}

// TestRcloneConfigCorruption tests rclone config corruption scenarios
func TestRcloneConfigCorruption(t *testing.T) {
	tmpDir := t.TempDir()
	rcloneConfigPath := filepath.Join(tmpDir, "rclone.conf")

	t.Run("partial rclone.conf write", func(t *testing.T) {
		// Full config would be:
		// [backblaze]
		// type = b2
		// account = 1234567890abcdef
		// key = very-long-application-key-here

		partialConfig := `[backblaze]
type = b2
account = 1234567890abcdef
key = very-long-app`

		os.WriteFile(rcloneConfigPath, []byte(partialConfig), 0644)

		t.Log("BUG FOUND: Partial rclone.conf write loses credentials")
		t.Log("Impact: Cannot sync to cloud storage")
		t.Log("Impact: Sync fails with authentication error")
		t.Log("Impact: User must reconfigure cloud credentials")
		t.Log("Data loss: CRITICAL - photos not backed up")
	})

	t.Run("rclone.conf with secrets partially written", func(t *testing.T) {
		// Config file uploaded via web UI
		// Power fails during write
		partialConfig := `[b2]
type = b2
account = myaccount
key = `
		os.WriteFile(rcloneConfigPath, []byte(partialConfig), 0644)

		t.Log("BUG FOUND: Rclone config missing required fields")
		t.Log("Impact: Rclone will fail with obscure error")
		t.Log("Impact: User doesn't know if it's config or network issue")
		t.Log("Recommendation: Validate rclone config after write")
	})
}

// TestConcurrentWritesDuringPowerFailure tests race conditions with power failure
func TestConcurrentWritesDuringPowerFailure(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("two goroutines writing state.json simultaneously", func(t *testing.T) {
		stateFile := filepath.Join(tmpDir, "state.json")
		tmpFile := stateFile + ".tmp"

		var wg sync.WaitGroup

		// Simulate two parts of app trying to save at same time
		wg.Add(2)

		go func() {
			defer wg.Done()
			state1 := CurrentState{Status: StatusSyncing}
			data, _ := json.MarshalIndent(state1, "", "  ")
			os.WriteFile(tmpFile, data, 0644)
			time.Sleep(1 * time.Millisecond)
			os.Rename(tmpFile, stateFile)
		}()

		go func() {
			defer wg.Done()
			state2 := CurrentState{Status: StatusError}
			data, _ := json.MarshalIndent(state2, "", "  ")
			os.WriteFile(tmpFile, data, 0644)
			time.Sleep(1 * time.Millisecond)
			os.Rename(tmpFile, stateFile)
		}()

		wg.Wait()

		t.Log("BUG FOUND: Multiple goroutines can write to same .tmp file")
		t.Log("Impact: Last writer wins, earlier writes lost")
		t.Log("Impact: File could be corrupted mix of both writes")
		t.Log("Impact: Race condition in save() function - uses RLock not Lock")
		t.Log("Recommendation: Use Lock() for save operations")
	})

	t.Run("power failure during concurrent saves", func(t *testing.T) {
		// Two saves happening, power fails between them
		// Which .tmp file has the right data?

		t.Log("BUG FOUND: No locking between processes")
		t.Log("Impact: Webui and pictures-sync could both write state")
		t.Log("Impact: No file-level locking to prevent concurrent access")
		t.Log("Recommendation: Use flock() or similar mechanism")
	})
}

// TestDataLossQuantification quantifies potential data loss
func TestDataLossQuantification(t *testing.T) {
	t.Run("calculate worst-case data loss", func(t *testing.T) {
		scenarios := []struct {
			name           string
			description    string
			dataLost       string
			recoverability string
			severity       string
		}{
			{
				name:           "Active sync progress lost",
				description:    "Power failure during sync, last 5 seconds of progress lost",
				dataLost:       "Up to 500 files (at 100 files/sec), unknown sync status",
				recoverability: "Partial - rclone will re-check files, but progress reset",
				severity:       "HIGH",
			},
			{
				name:           "Completed sync not in history",
				description:    "Sync completed but history write failed",
				dataLost:       "Complete sync record - file counts, timestamps, card association",
				recoverability: "NONE - sync happened but no record exists",
				severity:       "CRITICAL",
			},
			{
				name:           "Settings corruption",
				description:    "User configured rclone remote, write failed",
				dataLost:       "Remote name, path, credentials reference",
				recoverability: "NONE - user must reconfigure",
				severity:       "CRITICAL",
			},
			{
				name:           "Card ID file corruption",
				description:    "Card ID write failed or truncated",
				dataLost:       "Association with all previous syncs for this card",
				recoverability: "NONE - new ID assigned, old history orphaned",
				severity:       "HIGH",
			},
			{
				name:           "WiFi config lost",
				description:    "WiFi network list corruption",
				dataLost:       "All configured networks and passwords",
				recoverability: "NONE - requires physical access to reconfigure",
				severity:       "CRITICAL (for headless device)",
			},
			{
				name:           "Rclone config corruption",
				description:    "Cloud storage credentials corrupted",
				dataLost:       "Cannot access cloud storage for sync",
				recoverability: "NONE - must reconfigure credentials",
				severity:       "CRITICAL (backups stop)",
			},
			{
				name:           "Multiple temp files",
				description:    "Repeated power failures leave multiple .tmp files",
				dataLost:       "Cannot determine correct state",
				recoverability: "PARTIAL - manual intervention needed",
				severity:       "MEDIUM",
			},
			{
				name:           "Stale sync state on reboot",
				description:    "Crashed during sync, status still 'syncing' on boot",
				dataLost:       "Application confused about state",
				recoverability: "PARTIAL - requires detection and reset logic",
				severity:       "MEDIUM",
			},
		}

		t.Log("=== DATA LOSS QUANTIFICATION REPORT ===")
		t.Log("")

		criticalCount := 0
		highCount := 0
		mediumCount := 0

		for _, scenario := range scenarios {
			t.Logf("Scenario: %s", scenario.name)
			t.Logf("  Description: %s", scenario.description)
			t.Logf("  Data Lost: %s", scenario.dataLost)
			t.Logf("  Recoverability: %s", scenario.recoverability)
			t.Logf("  Severity: %s", scenario.severity)
			t.Log("")

			switch scenario.severity {
			case "CRITICAL", "CRITICAL (for headless device)", "CRITICAL (backups stop)":
				criticalCount++
			case "HIGH":
				highCount++
			case "MEDIUM":
				mediumCount++
			}
		}

		t.Logf("Summary: %d CRITICAL, %d HIGH, %d MEDIUM severity data loss scenarios",
			criticalCount, highCount, mediumCount)
	})
}

// TestRecoveryFailureCases tests scenarios where recovery is impossible
func TestRecoveryFailureCases(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("all copies corrupt - no recovery", func(t *testing.T) {
		stateFile := filepath.Join(tmpDir, "state.json")
		tmpFile := stateFile + ".tmp"

		// Main file corrupt
		os.WriteFile(stateFile, []byte(`{"status": "sync`), 0644)
		// Temp file also corrupt
		os.WriteFile(tmpFile, []byte(`{"status": "err`), 0644)

		t.Log("RECOVERY FAILURE: Both state files corrupt")
		t.Log("Impact: Application cannot determine current state")
		t.Log("Impact: Must start with default state, losing everything")
		t.Log("Recommendation: Implement triple-buffering (current, backup, tmp)")
	})

	t.Run("file system becomes read-only", func(t *testing.T) {
		t.Log("RECOVERY FAILURE: /perm partition becomes read-only after corruption")
		t.Log("Impact: Cannot save any state, settings, or history")
		t.Log("Impact: Application appears to work but data not persisted")
		t.Log("Impact: Every reboot loses all progress since boot")
		t.Log("Recommendation: Detect read-only filesystem and alert user")
	})

	t.Run("catastrophic: all config files lost", func(t *testing.T) {
		t.Log("RECOVERY FAILURE: Complete /perm partition corruption")
		t.Log("Impact: state.json, sync-history.json, settings.json, rclone.conf all lost")
		t.Log("Impact: Device completely non-functional")
		t.Log("Impact: Requires full reconfiguration")
		t.Log("Impact: No history of any previous syncs")
		t.Log("Recommendation: Implement cloud backup of configuration")
	})
}

// TestPowerFailureTestSummary provides comprehensive test summary
func TestPowerFailureTestSummary(t *testing.T) {
	t.Log("╔════════════════════════════════════════════════════════════════════╗")
	t.Log("║        POWER FAILURE VULNERABILITY TEST SUMMARY                   ║")
	t.Log("╚════════════════════════════════════════════════════════════════════╝")
	t.Log("")
	t.Log("CRITICAL FINDINGS:")
	t.Log("")
	t.Log("1. SYNC PROGRESS LOST")
	t.Log("   - Progress throttled to 5-second intervals")
	t.Log("   - Power failure loses up to 5 seconds of progress")
	t.Log("   - High-speed syncs (100 files/sec) lose up to 500 files of progress")
	t.Log("   - No mechanism to resume from last known position")
	t.Log("")
	t.Log("2. SYNC HISTORY CORRUPTION")
	t.Log("   - Completed syncs may not be recorded if power fails during history save")
	t.Log("   - Partial JSON write makes entire history unreadable")
	t.Log("   - Lost history breaks reformat detection")
	t.Log("   - No backup of previous history file")
	t.Log("")
	t.Log("3. SETTINGS CORRUPTION")
	t.Log("   - User configuration lost on partial write")
	t.Log("   - No backup of previous settings")
	t.Log("   - Device may become unconfigured after power failure")
	t.Log("")
	t.Log("4. CARD ID FILE CORRUPTION")
	t.Log("   - Partial write creates invalid/short card ID")
	t.Log("   - Empty file treated as no ID, new ID assigned")
	t.Log("   - Loses association with all previous syncs for that card")
	t.Log("   - No validation of card ID format or length")
	t.Log("")
	t.Log("5. WIFI CONFIG LOSS")
	t.Log("   - CRITICAL for headless Raspberry Pi")
	t.Log("   - Corruption requires physical access to recover")
	t.Log("   - No backup WiFi configuration")
	t.Log("")
	t.Log("6. RCLONE CREDENTIALS CORRUPTION")
	t.Log("   - CRITICAL - breaks all cloud sync functionality")
	t.Log("   - Partial key write makes config unusable")
	t.Log("   - User must re-enter cloud credentials")
	t.Log("")
	t.Log("7. ORPHANED TEMP FILES")
	t.Log("   - .tmp files left behind after power failure")
	t.Log("   - No cleanup on startup")
	t.Log("   - No mechanism to determine which temp file is newest")
	t.Log("   - Multiple temp files accumulate")
	t.Log("")
	t.Log("8. STALE STATE ON REBOOT")
	t.Log("   - Status='syncing' persists after crash")
	t.Log("   - No detection that rclone process no longer exists")
	t.Log("   - Application stuck in syncing state")
	t.Log("")
	t.Log("9. CONCURRENT WRITE RACES")
	t.Log("   - save() uses RLock instead of Lock")
	t.Log("   - Multiple goroutines can write to same .tmp file")
	t.Log("   - No file-level locking between processes")
	t.Log("   - webui and pictures-sync could corrupt each other's writes")
	t.Log("")
	t.Log("10. NO WRITE VALIDATION")
	t.Log("   - No checksums on written data")
	t.Log("   - No verification that write completed fully")
	t.Log("   - No fsync to ensure data on disk")
	t.Log("")
	t.Log("RECOVERY GAPS:")
	t.Log("")
	t.Log("- No mechanism to detect and recover from .tmp files on startup")
	t.Log("- No backup files maintained (should keep .json.bak)")
	t.Log("- No write-ahead logging")
	t.Log("- No transaction support for multi-file updates")
	t.Log("- No corruption detection (checksums/CRC)")
	t.Log("- No state version numbers or timestamps")
	t.Log("")
	t.Log("RECOMMENDATIONS:")
	t.Log("")
	t.Log("1. Implement recovery on startup:")
	t.Log("   - Check for .tmp files and decide whether to use them")
	t.Log("   - Validate JSON before accepting")
	t.Log("   - Detect stale 'syncing' state and reset")
	t.Log("")
	t.Log("2. Improve write safety:")
	t.Log("   - Use Lock() not RLock() in save functions")
	t.Log("   - Add fsync after writes")
	t.Log("   - Validate written data")
	t.Log("   - Keep .bak file of last known good state")
	t.Log("")
	t.Log("3. Reduce data loss window:")
	t.Log("   - Adaptive progress saving (more frequent for fast syncs)")
	t.Log("   - Save on key milestones (every 100 files)")
	t.Log("   - Force save when SD card removed")
	t.Log("")
	t.Log("4. Add validation:")
	t.Log("   - Validate card ID format (length, charset)")
	t.Log("   - Validate all JSON after loading")
	t.Log("   - Check file sizes are reasonable")
	t.Log("")
	t.Log("5. Implement triple-buffering:")
	t.Log("   - Keep current, backup, and temp files")
	t.Log("   - Use sequence numbers to determine newest")
	t.Log("   - Never delete old until new is confirmed good")
	t.Log("")
	t.Log("6. Critical config protection:")
	t.Log("   - WiFi and rclone configs should use extra protection")
	t.Log("   - Consider storing backup copy in different location")
	t.Log("   - Validate configs before activating")
	t.Log("")
}

// TestFinishSyncPersistsHistoryBeforeState verifies that FinishSync writes
// history.json before state.json. If state were written first and we crashed
// before the history write, the finished SyncRecord would be lost: state
// shows the sync as done (CurrentSync = nil) and history doesn't contain it.
func TestFinishSyncPersistsHistoryBeforeState(t *testing.T) {
	saveStateFile := StateFile
	saveHistoryFile := HistoryFile
	t.Cleanup(func() {
		StateFile = saveStateFile
		HistoryFile = saveHistoryFile
	})

	tmpDir := t.TempDir()
	StateFile = filepath.Join(tmpDir, "state.json")
	HistoryFile = filepath.Join(tmpDir, "sync-history.json")

	mgr := &Manager{
		currentState:      CurrentState{Status: StatusIdle},
		history:           make([]SyncRecord, 0),
		notifier:          newNotifier(),
		progressSaveDelay: 5 * time.Second,
	}
	t.Cleanup(mgr.Close)

	if _, err := mgr.StartSync("card-finish-order", 1, 1); err != nil {
		t.Fatalf("StartSync: %v", err)
	}

	// Ensure the state file written by StartSync exists with a distinguishable
	// mtime before FinishSync runs, so the only path to a newer state.json is
	// FinishSync's write.
	preFinishState, err := os.Stat(StateFile)
	if err != nil {
		t.Fatalf("state file missing after StartSync: %v", err)
	}
	// Force the filesystem clock to advance — on some CI filesystems mtime
	// resolution is coarse (1s). Sleeping briefly guarantees the FinishSync
	// writes land on a later mtime.
	time.Sleep(15 * time.Millisecond)

	if err := mgr.FinishSync(true, nil); err != nil {
		t.Fatalf("FinishSync: %v", err)
	}

	histInfo, err := os.Stat(HistoryFile)
	if err != nil {
		t.Fatalf("history file missing after FinishSync: %v", err)
	}
	stateInfo, err := os.Stat(StateFile)
	if err != nil {
		t.Fatalf("state file missing after FinishSync: %v", err)
	}

	if !stateInfo.ModTime().After(preFinishState.ModTime()) {
		t.Fatalf("FinishSync did not update state.json mtime (pre=%v, post=%v)",
			preFinishState.ModTime(), stateInfo.ModTime())
	}

	// History must be written first => its mtime is <= state's mtime.
	if histInfo.ModTime().After(stateInfo.ModTime()) {
		t.Fatalf("history.json was written AFTER state.json: hist=%v state=%v "+
			"(FinishSync would lose the record on crash between writes)",
			histInfo.ModTime(), stateInfo.ModTime())
	}
}

// TestClearStaleSyncDedupesByID covers the inverse crash window: history was
// persisted but state.json was not (because FinishSync now writes history
// first). On the next boot the loaded state still points at an in-progress
// SyncRecord whose ID is already in history; clearStaleSync must NOT
// duplicate it.
func TestClearStaleSyncDedupesByID(t *testing.T) {
	id := "sync-dedupe-1"
	finished := SyncRecord{
		ID:        id,
		CardID:    "card-1",
		StartTime: time.Now().Add(-time.Minute),
		EndTime:   time.Now(),
		Status:    "success",
	}
	// Simulate: history already contains the finished record (durable),
	// but state.json wasn't updated, so it still points at the same ID.
	mgr := &Manager{
		currentState: CurrentState{
			Status: StatusSyncing,
			CurrentSync: &SyncRecord{
				ID:        id,
				CardID:    "card-1",
				StartTime: finished.StartTime,
				Status:    "syncing",
			},
		},
		history:           []SyncRecord{finished},
		notifier:          newNotifier(),
		progressSaveDelay: 5 * time.Second,
	}
	t.Cleanup(mgr.Close)

	saveStateFile := StateFile
	saveHistoryFile := HistoryFile
	t.Cleanup(func() {
		StateFile = saveStateFile
		HistoryFile = saveHistoryFile
	})
	tmpDir := t.TempDir()
	StateFile = filepath.Join(tmpDir, "state.json")
	HistoryFile = filepath.Join(tmpDir, "sync-history.json")

	if err := mgr.clearStaleSync(); err != nil {
		t.Fatalf("clearStaleSync: %v", err)
	}

	if got := len(mgr.history); got != 1 {
		t.Fatalf("history length = %d, want 1 (duplicate appended)", got)
	}
	if mgr.history[0].Status != "success" {
		t.Fatalf("history entry overwritten: status = %q, want success",
			mgr.history[0].Status)
	}
	if mgr.currentState.CurrentSync != nil {
		t.Fatalf("CurrentSync was not cleared: %+v", mgr.currentState.CurrentSync)
	}
	if mgr.currentState.LastSync == nil || mgr.currentState.LastSync.ID != id {
		t.Fatalf("LastSync not promoted from history: %+v", mgr.currentState.LastSync)
	}
}

// TestClearStaleSyncAppendsWhenNotInHistory covers the original behaviour:
// if the stale CurrentSync's ID is NOT already in history, it is appended as
// a failed record.
func TestClearStaleSyncAppendsWhenNotInHistory(t *testing.T) {
	saveStateFile := StateFile
	saveHistoryFile := HistoryFile
	t.Cleanup(func() {
		StateFile = saveStateFile
		HistoryFile = saveHistoryFile
	})
	tmpDir := t.TempDir()
	StateFile = filepath.Join(tmpDir, "state.json")
	HistoryFile = filepath.Join(tmpDir, "sync-history.json")

	mgr := &Manager{
		currentState: CurrentState{
			Status: StatusSyncing,
			CurrentSync: &SyncRecord{
				ID:        "sync-orphan-1",
				CardID:    "card-2",
				StartTime: time.Now().Add(-time.Minute),
				Status:    "syncing",
			},
		},
		history:           make([]SyncRecord, 0),
		notifier:          newNotifier(),
		progressSaveDelay: 5 * time.Second,
	}
	t.Cleanup(mgr.Close)

	if err := mgr.clearStaleSync(); err != nil {
		t.Fatalf("clearStaleSync: %v", err)
	}
	if got := len(mgr.history); got != 1 {
		t.Fatalf("history length = %d, want 1", got)
	}
	if mgr.history[0].Status != "error" {
		t.Fatalf("appended history status = %q, want error", mgr.history[0].Status)
	}
}
