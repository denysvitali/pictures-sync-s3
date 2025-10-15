package sdmonitor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestMultipleSDCardsSimultaneous tests behavior when multiple SD cards are detected
// BUG FOUND: Monitor only tracks lastDevice (single card), will ignore additional cards
func TestMultipleSDCardsSimultaneous(t *testing.T) {
	t.Skip("Requires mock device setup - demonstrates bug: only tracks one device")

	// This test reveals that Monitor.lastDevice is a string, not a slice
	// If multiple USB card readers are connected, only one will be tracked
	// The findUSBStorageDevice() returns first available, ignoring others

	// Expected behavior: Should handle multiple cards or return error
	// Actual behavior: Silently ignores second card
}

// TestCardRemovedDuringMount tests SD card removal during mount operation
// BUG FOUND: Race condition - no synchronization between mount() and checkDevices()
func TestCardRemovedDuringMount(t *testing.T) {
	t.Skip("Requires mock device - demonstrates race condition")

	// Scenario:
	// 1. checkDevices() detects device at line 128
	// 2. User removes card
	// 3. mount() called at line 135 on non-existent device
	// 4. mount() will fail but error is only logged, not propagated to event
	// 5. EventInserted is still sent at line 142 even though mount failed

	// BUG: Line 136-137 logs error and returns, but event is sent anyway
	// Should check mount success before updating lastDevice and sending event
}

// TestCorruptedCardIDFile tests handling of corrupted .pictures-sync-id file
func TestCorruptedCardIDFile(t *testing.T) {
	tmpDir := t.TempDir()
	idPath := filepath.Join(tmpDir, CardIDFile)

	// Test 1: File with null bytes
	t.Run("NullBytes", func(t *testing.T) {
		err := os.WriteFile(idPath, []byte("card-\x00\x00\x00corrupted"), 0644)
		if err != nil {
			t.Fatal(err)
		}

		cardID, isNew, err := GetOrCreateCardID(tmpDir, nil)
		if err != nil {
			t.Errorf("Should handle null bytes gracefully: %v", err)
		}

		// BUG: TrimSpace doesn't remove null bytes, returns corrupted ID
		if strings.Contains(cardID, "\x00") {
			t.Errorf("Card ID contains null bytes: %q", cardID)
		}

		if isNew {
			t.Log("Generated new ID (expected behavior)")
		}
	})

	// Test 2: File with only whitespace
	t.Run("OnlyWhitespace", func(t *testing.T) {
		err := os.WriteFile(idPath, []byte("   \n\t\n   "), 0644)
		if err != nil {
			t.Fatal(err)
		}

		cardID, isNew, err := GetOrCreateCardID(tmpDir, nil)
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}

		// This correctly generates new ID when trimmed string is empty (line 355-366)
		if !isNew {
			t.Error("Should detect empty ID and generate new one")
		}

		if cardID == "" {
			t.Error("Card ID should not be empty")
		}
	})

	// Test 3: File with extremely long content
	t.Run("ExtremelyLong", func(t *testing.T) {
		longID := strings.Repeat("a", 10*1024*1024) // 10MB ID
		err := os.WriteFile(idPath, []byte(longID), 0644)
		if err != nil {
			t.Fatal(err)
		}

		// BUG: ReadFile at line 353 will read entire 10MB into memory
		// No size limit check before reading
		cardID, _, err := GetOrCreateCardID(tmpDir, nil)
		if err != nil {
			t.Errorf("Should handle large files: %v", err)
		}

		if len(cardID) > 1024 {
			t.Errorf("Card ID is too long (%d bytes), should be truncated", len(cardID))
		}
	})

	// Test 4: File with multiple lines
	t.Run("MultipleLines", func(t *testing.T) {
		multiLine := "card-valid-id\nextra-line\nmore-data"
		err := os.WriteFile(idPath, []byte(multiLine), 0644)
		if err != nil {
			t.Fatal(err)
		}

		cardID, isNew, err := GetOrCreateCardID(tmpDir, nil)
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}

		// TrimSpace at line 354 will trim but include newlines in middle
		if strings.Contains(cardID, "\n") {
			t.Errorf("Card ID contains newlines: %q", cardID)
		}

		if isNew {
			t.Error("Should use existing ID from first line")
		}
	})

	// Test 5: Symlink pointing to invalid location
	t.Run("SymlinkToInvalid", func(t *testing.T) {
		os.Remove(idPath) // Clean up
		err := os.Symlink("/nonexistent/file", idPath)
		if err != nil {
			t.Skip("Cannot create symlink")
		}

		// ReadFile follows symlinks and will fail
		_, _, err = GetOrCreateCardID(tmpDir, nil)
		if err == nil {
			t.Error("Should return error for invalid symlink")
		}
	})
}

// TestNoDCIMFolder tests cards without DCIM directory
func TestNoDCIMFolder(t *testing.T) {
	tmpDir := t.TempDir()

	// HasDCIM should return false
	if HasDCIM(tmpDir) {
		t.Error("Should return false when DCIM doesn't exist")
	}

	// Create DCIM as a file, not directory
	dcimPath := filepath.Join(tmpDir, "DCIM")
	err := os.WriteFile(dcimPath, []byte("not a directory"), 0644)
	if err != nil {
		t.Fatal(err)
	}

	// BUG: HasDCIM checks IsDir() but doesn't handle case where DCIM is a file
	// Line 294-298: Stat succeeds, but IsDir() returns false
	// This is actually correct behavior, but CountPhotos will fail
	if HasDCIM(tmpDir) {
		t.Error("Should return false when DCIM is a file")
	}

	// CountPhotos should fail gracefully
	count, size, err := CountPhotos(tmpDir)
	if err == nil {
		t.Error("Should return error when DCIM is not a directory")
	}
	if count != 0 || size != 0 {
		t.Errorf("Expected zero count/size for invalid DCIM, got count=%d size=%d", count, size)
	}
}

// TestPermissionIssues tests various permission problems
func TestPermissionIssues(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("Running as root, permission tests won't work")
	}

	tmpDir := t.TempDir()

	// Test 1: DCIM directory without read permission
	t.Run("DCIMNoRead", func(t *testing.T) {
		dcimPath := filepath.Join(tmpDir, "DCIM")
		err := os.Mkdir(dcimPath, 0000) // No permissions
		if err != nil {
			t.Fatal(err)
		}
		defer os.Chmod(dcimPath, 0755) // Cleanup

		// BUG: HasDCIM at line 294 uses Stat which succeeds even without read permission
		// But CountPhotos will fail when trying to walk
		if !HasDCIM(tmpDir) {
			t.Error("HasDCIM should succeed even without read permission")
		}

		_, _, err = CountPhotos(tmpDir)
		if err == nil {
			t.Error("CountPhotos should fail without read permission")
		}
	})

	// Test 2: Cannot write card ID file
	t.Run("CannotWriteID", func(t *testing.T) {
		readOnlyDir := t.TempDir()
		os.Chmod(readOnlyDir, 0555) // Read-only
		defer os.Chmod(readOnlyDir, 0755)

		// BUG: GetOrCreateCardID at line 374 fails to write but returns the new ID anyway
		// The error is logged but returned as error at line 377
		cardID, isNew, err := GetOrCreateCardID(readOnlyDir, nil)
		if err == nil {
			t.Error("Should return error when cannot write ID file")
		}

		if !isNew {
			t.Error("Should indicate new card even if write fails")
		}

		if cardID == "" {
			t.Error("Should still return generated ID even if write fails")
		}
	})
}

// TestUSBVsBuiltinDetection tests USB device detection logic
func TestUSBVsBuiltinDetection(t *testing.T) {
	t.Skip("Requires real hardware or complex mocking")

	// BUG ANALYSIS: isUSBDeviceHelper at line 565-598
	// Issues:
	// 1. Relies on sysfs paths that may not exist in all environments
	// 2. String matching "usb" is fragile (line 577, 586-588)
	// 3. Limited to 10 iterations (line 568) - arbitrary limit
	// 4. No caching - repeatedly reads filesystem for same device
	// 5. uevent parsing with Contains is unreliable - could match substring
}

// TestCardIDFileCorruptionRace tests concurrent access to card ID file
func TestCardIDFileCorruptionRace(t *testing.T) {
	tmpDir := t.TempDir()

	// Simulate multiple processes trying to read/write card ID simultaneously
	// BUG: No file locking in GetOrCreateCardID or CreateNewCardID

	var wg sync.WaitGroup
	results := make(chan string, 10)

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cardID, _, _ := GetOrCreateCardID(tmpDir, nil)
			results <- cardID
		}()
	}

	wg.Wait()
	close(results)

	// Collect all IDs
	ids := make(map[string]int)
	for id := range results {
		ids[id]++
	}

	if len(ids) > 1 {
		t.Errorf("Race condition: Got %d different card IDs: %v", len(ids), ids)
		t.Log("BUG: No file locking allows multiple writes")
	}
}

// TestExtremeCardSizes tests handling of very large and very small cards
func TestExtremeCardSizes(t *testing.T) {
	t.Skip("Would require creating large file structures")

	// Test scenarios:
	// 1. 1TB card with millions of photos - CountPhotos could take hours
	//    BUG: No timeout or cancellation context in CountPhotos (line 302-331)
	//    BUG: Could exhaust memory counting millions of files
	//
	// 2. Nearly full card - no space to write card ID file
	//    BUG: WriteFile at line 374 doesn't check disk space first
	//
	// 3. Empty card (0 bytes) - edge case for size calculation
	//    getDeviceInfo at line 524: sectors * 512 could be 0
	//
	// 4. Card reporting negative size (corrupted sysfs)
	//    BUG: No validation that sectors is positive at line 523
}

// TestSpecialCharactersInVolumeLabel tests volume labels with special characters
func TestSpecialCharactersInVolumeLabel(t *testing.T) {
	// BUG ANALYSIS: getDeviceInfo at line 556-559
	// Uses blkid output directly without sanitization
	// Volume labels could contain:
	// - Newlines, tabs, control characters
	// - Shell metacharacters if used in commands
	// - Unicode that could break JSON encoding
	// - Empty string vs no label distinction is lost

	testLabels := []string{
		"Normal Label",
		"Label\nWith\nNewlines",
		"Label\x00With\x00Nulls",
		"Label'With\"Quotes",
		"Label;With;Semicolons",
		"Label\tWith\tTabs",
		"Label🚀With😀Emoji",
		strings.Repeat("A", 1000), // Very long label
		"",                         // Empty label
	}

	for _, label := range testLabels {
		t.Run(fmt.Sprintf("Label_%q", label), func(t *testing.T) {
			// Can't test directly without mock device
			// But this documents the potential issue
			t.Skipf("Would test label: %q", label)
		})
	}
}

// TestRapidInsertRemoveCycles tests rapid SD card insertion/removal
func TestRapidInsertRemoveCycles(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping slow test")
	}

	tmpDir := t.TempDir()
	monitor := NewMonitor(filepath.Join(tmpDir, "mount"))

	// Start monitoring
	err := monitor.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer monitor.Stop()

	// BUG ANALYSIS: Rapid state changes could cause issues
	// 1. eventChan has buffer of 10 (line 55) - could overflow with rapid events
	// 2. No event deduplication - same event could be sent multiple times
	// 3. pollDevices runs every 2 seconds (line 92) - could miss rapid changes
	// 4. No debouncing - unstable connection could flood events

	// Simulate rapid state changes by calling checkDevices repeatedly
	done := make(chan bool)
	go func() {
		for i := 0; i < 100; i++ {
			monitor.checkDevices()
			time.Sleep(10 * time.Millisecond)
		}
		done <- true
	}()

	eventCount := 0
	timeout := time.After(5 * time.Second)

loop:
	for {
		select {
		case <-monitor.Events():
			eventCount++
		case <-timeout:
			break loop
		case <-done:
			// Drain remaining events
			time.Sleep(100 * time.Millisecond)
			break loop
		}
	}

	t.Logf("Received %d events from rapid polling", eventCount)
	// In real scenario with actual device changes, this could reveal bugs
}

// TestIsMountedElsewhereEdgeCases tests mount detection logic
func TestIsMountedElsewhereEdgeCases(t *testing.T) {
	tmpDir := t.TempDir()
	monitor := NewMonitor(filepath.Join(tmpDir, "mount"))

	// Test with crafted /proc/mounts data
	t.Run("DeviceNameSubstring", func(t *testing.T) {
		// BUG: Line 205 uses device + " " prefix matching
		// Could match "/dev/sda1" when searching for "/dev/sda"
		// Actually this is correct because it requires space after device

		// But what if mount path contains device name?
		monitor.cachedMounts = "/dev/sda1 /mnt/sda1/backup ext4 rw 0 0\n"
		monitor.mountsCacheTime = time.Now()

		result := monitor.isMountedElsewhere("/dev/sda1")
		if !result {
			t.Error("Should detect mount at different path")
		}
	})

	t.Run("NoSpaceAfterDevice", func(t *testing.T) {
		// Malformed mount line without space
		monitor.cachedMounts = "/dev/sda1/something"
		monitor.mountsCacheTime = time.Now()

		result := monitor.isMountedElsewhere("/dev/sda1")
		// This should return false due to line 206-208 Index returning -1
		if result {
			t.Error("Should not match malformed mount line")
		}
	})

	t.Run("DeviceAtEndOfFile", func(t *testing.T) {
		// Device at end without trailing newline
		monitor.cachedMounts = "/dev/sda1 /mnt/other ext4 rw 0 0"
		monitor.mountsCacheTime = time.Now()

		result := monitor.isMountedElsewhere("/dev/sda1")
		if !result {
			t.Error("Should handle device at EOF")
		}
	})

	t.Run("EmptyMountPath", func(t *testing.T) {
		// Mount line with insufficient fields
		monitor.cachedMounts = "/dev/sda1\n"
		monitor.mountsCacheTime = time.Now()

		// BUG: Line 221-224 checks len(fields) >= 2
		// But Fields() with single field returns 1 element
		result := monitor.isMountedElsewhere("/dev/sda1")
		if result {
			t.Error("Should return false for malformed line with no mount path")
		}
	})
}

// TestCountPhotosSymlinks tests photo counting with symlinks
func TestCountPhotosSymlinks(t *testing.T) {
	tmpDir := t.TempDir()
	dcimPath := filepath.Join(tmpDir, "DCIM")
	err := os.Mkdir(dcimPath, 0755)
	if err != nil {
		t.Fatal(err)
	}

	// Create a photo file
	photoPath := filepath.Join(dcimPath, "IMG_001.jpg")
	err = os.WriteFile(photoPath, []byte("fake photo"), 0644)
	if err != nil {
		t.Fatal(err)
	}

	// Create symlink to the photo
	linkPath := filepath.Join(dcimPath, "IMG_002.jpg")
	err = os.Symlink(photoPath, linkPath)
	if err != nil {
		t.Skip("Cannot create symlink")
	}

	// BUG: CountPhotos uses WalkDir which follows symlinks by default in some cases
	// Could count same file twice or follow symlink loops
	count, _, err := CountPhotos(tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	if count != 1 {
		t.Errorf("Expected 1 photo (symlink should not be counted separately), got %d", count)
	}

	// Create circular symlink
	circularPath := filepath.Join(dcimPath, "circular")
	err = os.Symlink(dcimPath, circularPath)
	if err != nil {
		t.Skip("Cannot create circular symlink")
	}

	// This could cause infinite loop or error
	_, _, err = CountPhotos(tmpDir)
	if err != nil {
		t.Logf("Circular symlink handled: %v", err)
	}
}

// TestRemountReadOnlyFailure tests handling of remount failures
func TestRemountReadOnlyFailure(t *testing.T) {
	tmpDir := t.TempDir()
	monitor := NewMonitor(filepath.Join(tmpDir, "mount"))

	// RemountReadOnly requires actual mount
	// BUG ANALYSIS: Lines 271-278
	// 1. RemountReadOnly returns error but callers may ignore it
	// 2. Critical failure at line 361-362 and 385-387 returns error
	//    but card is already identified - inconsistent state
	// 3. No rollback mechanism if remount fails
	// 4. SD card remains read-write, risking corruption
	// 5. Error message doesn't suggest recovery actions

	err := monitor.RemountReadOnly()
	if err == nil {
		t.Error("Should fail when nothing is mounted")
	}
}

// TestMountCacheExpiry tests mount cache expiration logic
func TestMountCacheExpiry(t *testing.T) {
	tmpDir := t.TempDir()
	monitor := NewMonitor(filepath.Join(tmpDir, "mount"))

	// Set cached data
	monitor.cachedMounts = "test data"
	monitor.mountsCacheTime = time.Now().Add(-5 * time.Second) // Expired

	// BUG: Line 110 checks time.Since < TTL
	// If system clock jumps backward, cache never expires
	// Should use monotonic clock

	_, err := monitor.getCachedMounts()
	if err != nil {
		t.Fatal(err)
	}

	// Should have refreshed
	if monitor.cachedMounts == "test data" {
		t.Error("Cache should have been refreshed")
	}
}

// TestConcurrentStop tests concurrent Stop() calls
func TestConcurrentStop(t *testing.T) {
	tmpDir := t.TempDir()
	monitor := NewMonitor(filepath.Join(tmpDir, "mount"))

	err := monitor.Start()
	if err != nil {
		t.Fatal(err)
	}

	// Call Stop concurrently
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			monitor.Stop()
		}()
	}

	wg.Wait()

	// BUG: Line 78 closes stopChan
	// Multiple Stop() calls will panic on closed channel
	// Should use sync.Once or check if already closed
}

// TestEventChannelBlocking tests event channel blocking behavior
func TestEventChannelBlocking(t *testing.T) {
	tmpDir := t.TempDir()
	monitor := NewMonitor(filepath.Join(tmpDir, "mount"))

	// eventChan buffer is 10 (line 55)
	// If consumer is slow, channel fills up

	// Fill the buffer without consuming
	for i := 0; i < 10; i++ {
		monitor.eventChan <- Event{Type: EventInserted, DevPath: fmt.Sprintf("/dev/test%d", i)}
	}

	// Try to send one more event
	done := make(chan bool)
	go func() {
		// This should block because buffer is full
		monitor.eventChan <- Event{Type: EventInserted, DevPath: "/dev/test11"}
		done <- true
	}()

	select {
	case <-done:
		t.Error("Should block when event buffer is full")
	case <-time.After(100 * time.Millisecond):
		t.Log("Correctly blocking on full buffer")
	}

	// BUG: If checkDevices tries to send event when buffer is full,
	// it will block the polling goroutine (line 102)
	// This could prevent device removal detection
}

// TestGenerateCardIDFailure tests card ID generation edge cases
func TestGenerateCardIDFailure(t *testing.T) {
	// generateCardID at line 395-403
	// Uses crypto/rand which could fail on some systems

	// The fallback at line 400 uses Unix timestamp
	// BUG: Two cards inserted in same second get same ID
	// Should include process ID or additional randomness

	// Also, timestamp-based ID format differs from random format:
	// Random: "card-hexstring" (16 chars)
	// Fallback: "card-1234567890" (10-digit number)
	// Could cause confusion or conflicts in remote storage
}

// TestDeviceInfoMemoryLeak tests for potential memory leaks
func TestDeviceInfoMemoryLeak(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping memory test")
	}

	// BUG ANALYSIS: ListAllStorageDevices at line 445-477
	// 1. Calls getDeviceInfo for each device
	// 2. getDeviceInfo spawns external commands (blkid at line 336, 556)
	// 3. No limit on number of devices scanned
	// 4. If called repeatedly in loop, could leak file descriptors or memory
	// 5. No cleanup of failed command processes

	for i := 0; i < 100; i++ {
		_, err := ListAllStorageDevices()
		if err != nil {
			t.Fatal(err)
		}
	}

	// In production, this would be called from API endpoint
	// Malicious user could spam requests to exhaust resources
}

// TestFormatBytesEdgeCases tests byte formatting
func TestFormatBytesEdgeCases(t *testing.T) {
	tests := []struct {
		input    int64
		expected string
	}{
		{0, "0 B"},
		{1, "1 B"},
		{1023, "1023 B"},
		{1024, "1.0 KB"},
		{-1, "-1 B"}, // Negative size
		{9223372036854775807, "8.0 EB"}, // Max int64
	}

	for _, tt := range tests {
		result := formatBytes(tt.input)
		t.Logf("formatBytes(%d) = %s", tt.input, result)

		// BUG: Line 602 checks bytes < unit
		// But doesn't handle negative bytes
		// Would return "-1 B" instead of error
	}
}

// TestGlobPatternErrors tests error handling in findUSBStorageDevice
func TestGlobPatternErrors(t *testing.T) {
	// BUG ANALYSIS: Line 168-172
	// filepath.Glob can return error for invalid patterns
	// But the pattern "/dev/sd[a-z]1" is hardcoded, so error unlikely
	// However, if /dev is not accessible (permissions), Glob returns error
	// Error is logged but empty string returned - device won't be detected

	// Similar issue at line 184-191 for mmcblk devices
	// If first Glob succeeds but second fails, inconsistent behavior
}
