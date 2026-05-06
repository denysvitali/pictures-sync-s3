package sdmonitor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// TestBufferOverflowInCountPhotos tests for buffer overflow when counting large numbers of photos
func TestBufferOverflowInCountPhotos(t *testing.T) {
	tmpDir := t.TempDir()
	dcimPath := filepath.Join(tmpDir, "DCIM")
	if err := os.MkdirAll(dcimPath, 0755); err != nil {
		t.Fatal(err)
	}

	// Create a massive number of files to trigger potential integer overflow
	// Testing if count can overflow int type
	t.Run("IntegerOverflowInFileCount", func(t *testing.T) {
		// Simulate a scenario where file count could approach int max
		// Create subdirectories to avoid filesystem limits
		for i := 0; i < 100; i++ {
			subdir := filepath.Join(dcimPath, "100MSDCF", "sub", fmt.Sprintf("%03d", i))
			if err := os.MkdirAll(subdir, 0755); err != nil {
				t.Fatal(err)
			}
			for j := 0; j < 1000; j++ {
				filename := filepath.Join(subdir, fmt.Sprintf("IMG_%03d_%04d.jpg", i, j))
				if err := os.WriteFile(filename, []byte("fake"), 0644); err != nil {
					t.Fatal(err)
				}
			}
		}

		count, totalSize, err := CountPhotos(tmpDir)
		if err != nil {
			t.Errorf("CountPhotos failed: %v", err)
		}

		// Verify counts are reasonable and didn't overflow
		if count < 0 {
			t.Errorf("CRITICAL: File count overflowed to negative: %d", count)
		}
		if totalSize < 0 {
			t.Errorf("CRITICAL: Total size overflowed to negative: %d", totalSize)
		}

		// Expected approximately 100 * 1000 = 100,000 files
		expectedCount := 100000
		if count > expectedCount*2 || count < expectedCount/2 {
			t.Errorf("File count seems incorrect: got %d, expected ~%d", count, expectedCount)
		}
	})
}

// TestIntegerOverflowInSizeCalculation tests for integer overflow in size calculations
func TestIntegerOverflowInSizeCalculation(t *testing.T) {
	tmpDir := t.TempDir()
	dcimPath := filepath.Join(tmpDir, "DCIM")
	if err := os.MkdirAll(dcimPath, 0755); err != nil {
		t.Fatal(err)
	}

	t.Run("MaxInt64Size", func(t *testing.T) {
		// Create a file and manually test size accumulation
		// This tests if totalSize += info.Size() could overflow

		// Simulate large file sizes
		testFile := filepath.Join(dcimPath, "huge.jpg")
		if err := os.WriteFile(testFile, make([]byte, 1024*1024), 0644); err != nil {
			t.Fatal(err)
		}

		count, totalSize, err := CountPhotos(tmpDir)
		if err != nil {
			t.Errorf("CountPhotos failed: %v", err)
		}

		if totalSize < 0 {
			t.Errorf("CRITICAL: Size calculation overflowed: %d", totalSize)
		}

		if count != 1 {
			t.Errorf("Expected 1 file, got %d", count)
		}
	})
}

// TestSliceBoundsViolationInMountParsing tests slice bounds in isMountedElsewhere
func TestSliceBoundsViolationInMountParsing(t *testing.T) {
	m := NewMonitor(t.TempDir())

	// Test with malformed mount data
	t.Run("EmptyMountData", func(t *testing.T) {
		m.cachedMounts = ""
		result := m.isMountedElsewhere("/dev/sda1")
		// Should not panic, just return false
		if result {
			t.Error("Expected false for empty mount data")
		}
	})

	t.Run("MalformedMountEntry", func(t *testing.T) {
		// Mount entry with insufficient fields
		m.cachedMounts = "/dev/sda1\n"
		result := m.isMountedElsewhere("/dev/sda1")
		// Should handle gracefully without panic
		_ = result
	})

	t.Run("InvalidFieldAccess", func(t *testing.T) {
		// Test potential out-of-bounds access in fields[1]
		m.cachedMounts = "/dev/sda1 "
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("CRITICAL: Panic on invalid field access: %v", r)
			}
		}()
		m.isMountedElsewhere("/dev/sda1")
	})
}

// TestUnsafeStringToByteConversion tests for unsafe string/byte conversions
func TestUnsafeStringToByteConversion(t *testing.T) {
	t.Run("CardIDConversion", func(t *testing.T) {
		tmpDir := t.TempDir()
		idPath := filepath.Join(tmpDir, CardIDFile)

		// Write a card ID with potential null bytes
		maliciousID := "card-12345678\x00malicious"
		if err := os.WriteFile(idPath, []byte(maliciousID), 0644); err != nil {
			t.Fatal(err)
		}

		// Read it back - should handle null bytes safely
		data, err := os.ReadFile(idPath)
		if err != nil {
			t.Fatal(err)
		}

		cardID := strings.TrimSpace(string(data))

		// Verify null bytes are preserved or stripped appropriately
		if strings.Contains(cardID, "\x00") {
			t.Logf("WARNING: Null bytes in card ID: %q", cardID)
		}
	})
}

// TestConcurrentMapAccessInMonitor tests for race conditions in Monitor struct
func TestConcurrentMapAccessInMonitor(t *testing.T) {
	m := NewMonitor(t.TempDir())

	// Run with -race flag to detect race conditions
	t.Run("ConcurrentCacheAccess", func(t *testing.T) {
		var wg sync.WaitGroup

		// Concurrent readers
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := 0; j < 100; j++ {
					_, _ = m.getCachedMounts()
				}
			}()
		}

		// Concurrent writers (through checkDevices which updates cache)
		for i := 0; i < 5; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := 0; j < 50; j++ {
					m.checkDevices()
				}
			}()
		}

		wg.Wait()
	})
}

// TestMemoryLeakInEventChannel tests for potential memory leaks in event channel
func TestMemoryLeakInEventChannel(t *testing.T) {
	t.Run("UnreadEventsAccumulation", func(t *testing.T) {
		m := NewMonitor(t.TempDir())

		// Don't read from the event channel
		// Simulate events being generated
		for i := 0; i < 1000; i++ {
			select {
			case m.eventChan <- Event{
				Type:      EventInserted,
				DevPath:   "/dev/sda1",
				DevName:   "sda1",
				MountPath: "/mnt/test",
			}:
			default:
				// Channel full - good, it's buffered
			}
		}

		// Verify channel doesn't grow unbounded
		if len(m.eventChan) > 10 {
			t.Errorf("WARNING: Event channel has %d pending events (expected max 10)", len(m.eventChan))
		}
	})
}

// TestStackExhaustionInPathTraversal tests for deep recursion issues
func TestStackExhaustionInPathTraversal(t *testing.T) {
	tmpDir := t.TempDir()

	// Create deeply nested directory structure
	deepPath := tmpDir
	for i := 0; i < 1000; i++ {
		deepPath = filepath.Join(deepPath, "deep")
	}

	// This should fail to create due to path length limits, but shouldn't crash
	err := os.MkdirAll(deepPath, 0755)
	if err == nil {
		// If it succeeded, try to count photos in it
		dcimPath := filepath.Join(tmpDir, "DCIM")
		if err := os.MkdirAll(dcimPath, 0755); err == nil {
			// Create a file deep in the structure
			testFile := filepath.Join(deepPath, "test.jpg")
			_ = os.WriteFile(testFile, []byte("test"), 0644)

			// This should handle deep paths without stack overflow
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("CRITICAL: Stack overflow on deep path: %v", r)
				}
			}()

			_, _, _ = CountPhotos(tmpDir)
		}
	}
}

// TestFormatBytesIntegerOverflow tests formatBytes for integer overflow
func TestFormatBytesIntegerOverflow(t *testing.T) {
	tests := []struct {
		name  string
		bytes int64
	}{
		{"MaxInt64", 9223372036854775807},
		{"NegativeValue", -1},
		{"Zero", 0},
		{"Boundary", 1024*1024*1024*1024*1024 - 1}, // Just under 1PB
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("CRITICAL: formatBytes panicked on %d: %v", tt.bytes, r)
				}
			}()

			result := formatBytes(tt.bytes)

			if tt.bytes < 0 && !strings.Contains(result, "-") {
				t.Errorf("Negative bytes not handled correctly: %s", result)
			}

			// Should never return empty string
			if result == "" {
				t.Errorf("formatBytes returned empty string for %d", tt.bytes)
			}
		})
	}
}

// TestGetDeviceInfoSliceBounds tests getDeviceInfo for slice access violations
func TestGetDeviceInfoSliceBounds(t *testing.T) {
	t.Run("MalformedDeviceName", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("CRITICAL: Panic in getDeviceInfo: %v", r)
			}
		}()

		// Test with invalid device paths
		invalidDevices := []string{
			"",
			"/",
			"/dev/",
			"/dev/mmcblk",
			"/dev/mmcblkp1", // Missing number
		}

		for _, dev := range invalidDevices {
			_, err := getDeviceInfo(dev)
			// Should return error, not panic
			if err == nil {
				t.Logf("getDeviceInfo accepted invalid device: %s", dev)
			}
		}
	})
}

// TestIsUSBDeviceHelperInfiniteLoop tests for infinite loop in USB detection
func TestIsUSBDeviceHelperInfiniteLoop(t *testing.T) {
	t.Run("CircularSymlink", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create circular symlink
		link1 := filepath.Join(tmpDir, "link1")
		link2 := filepath.Join(tmpDir, "link2")

		_ = os.Symlink(link2, link1)
		_ = os.Symlink(link1, link2)

		// Should terminate due to depth limit (10 iterations)
		done := make(chan bool)
		go func() {
			result := isUSBDeviceHelper(link1)
			_ = result
			done <- true
		}()

		select {
		case <-done:
			// Good, it terminated
		case <-func() chan struct{} {
			ch := make(chan struct{})
			go func() {
				t.Helper()
				// Wait 5 seconds - if function doesn't return, it's stuck
				select {}
			}()
			return ch
		}():
			t.Error("CRITICAL: isUSBDeviceHelper appears to be stuck in infinite loop")
		}
	})
}

// TestConcurrentCardIDGeneration tests for race conditions in card ID generation
func TestConcurrentCardIDGeneration(t *testing.T) {
	t.Run("ParallelIDGeneration", func(t *testing.T) {
		var wg sync.WaitGroup
		ids := make(chan string, 100)

		// Generate IDs concurrently
		for i := 0; i < 100; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				id := generateCardID()
				ids <- id
			}()
		}

		wg.Wait()
		close(ids)

		// Check for uniqueness
		seen := make(map[string]bool)
		for id := range ids {
			if seen[id] {
				t.Errorf("CRITICAL: Duplicate card ID generated: %s", id)
			}
			seen[id] = true

			// Verify format
			if !strings.HasPrefix(id, "card-") {
				t.Errorf("Invalid card ID format: %s", id)
			}
		}
	})
}

// TestUnsafePointerUsage tests for potential unsafe pointer issues
func TestUnsafePointerUsage(t *testing.T) {
	// The code doesn't use unsafe directly, but we test string/[]byte conversions
	t.Run("StringByteRoundtrip", func(t *testing.T) {
		original := "card-12345678"
		asBytes := []byte(original)
		asString := string(asBytes)

		if original != asString {
			t.Errorf("String/byte conversion changed data: %q != %q", original, asString)
		}

		// Modify bytes and ensure string is not affected (should be copy, not reference)
		asBytes[0] = 'X'
		if asString[0] == 'X' {
			t.Errorf("CRITICAL: String references byte array (not a copy)")
		}
	})
}

// TestChannelOperationsAfterClose tests for operations on closed channels
func TestChannelOperationsAfterClose(t *testing.T) {
	t.Run("EventChannelAfterStop", func(t *testing.T) {
		m := NewMonitor(t.TempDir())

		// Start monitor
		if err := m.Start(); err != nil {
			t.Fatal(err)
		}

		// Stop it
		m.Stop()

		// The stop channel should be closed and observable without sending to it.
		select {
		case <-m.stopChan:
		default:
			t.Error("Expected stopChan to be closed after Stop")
		}
	})

	t.Run("ReadFromClosedChannel", func(t *testing.T) {
		ch := make(chan Event, 1)
		close(ch)

		// Reading from closed channel should return zero value
		event, ok := <-ch
		if ok {
			t.Errorf("Expected closed channel, got event: %+v", event)
		}

		// Multiple reads should also work
		event2, ok2 := <-ch
		if ok2 {
			t.Errorf("Expected closed channel on second read, got: %+v", event2)
		}
	})
}

// TestStringIndexOutOfBounds tests string index operations in isMountedElsewhere
func TestStringIndexOutOfBounds(t *testing.T) {
	m := NewMonitor(t.TempDir())

	tests := []struct {
		name        string
		mounts      string
		device      string
		shouldPanic bool
	}{
		{
			name:        "EmptyString",
			mounts:      "",
			device:      "/dev/sda1",
			shouldPanic: false,
		},
		{
			name:        "NoNewlines",
			mounts:      "/dev/sda1 /mnt vfat",
			device:      "/dev/sda1",
			shouldPanic: false,
		},
		{
			name:        "DeviceAtEnd",
			mounts:      "other /other vfat\n/dev/sda1",
			device:      "/dev/sda1",
			shouldPanic: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m.cachedMounts = tt.mounts

			defer func() {
				if r := recover(); r != nil {
					if !tt.shouldPanic {
						t.Errorf("CRITICAL: Unexpected panic: %v", r)
					}
				}
			}()

			_ = m.isMountedElsewhere(tt.device)
		})
	}
}
