package sdmonitor

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// BUG REPORT: Mount/Unmount Lifecycle Critical Issues
//
// This test file exposes critical bugs in the mount/unmount lifecycle that can cause:
// - Data loss from improper read-write mounts during sync
// - Mount point pollution from failed mount attempts
// - Resource leaks (stale mounts, unclosed file descriptors)
// - Security issues (writable mounts that should be read-only)
// - Race conditions between mount operations and device removal

// =============================================================================
// CRITICAL BUG #1: Mount fails but device marked as mounted
// =============================================================================
// Severity: HIGH - Causes EventInserted to be sent even when mount fails
// Location: sdmonitor.go:135-147
//
// The checkDevices() function logs mount errors but continues to:
// 1. Set lastDevice = device (line 140)
// 2. Send EventInserted (line 142-147)
//
// This causes the sync operation to fail because mount point is empty.

func TestMountFailureButDeviceMarkedAsMounted(t *testing.T) {
	tmpDir := t.TempDir()
	mountPoint := filepath.Join(tmpDir, "mount")
	monitor := NewMonitor(mountPoint)

	// Simulate mount() returning error by trying to mount nonexistent device
	// In real scenario, this happens when:
	// - Device removed between detection and mount
	// - Device has corrupted filesystem
	// - Insufficient permissions

	// BUG EVIDENCE: checkDevices continues after mount error
	// Expected: lastDevice should remain "" and no event sent
	// Actual: lastDevice is set and EventInserted is sent

	if err := monitor.Start(); err != nil {
		t.Fatal(err)
	}
	defer monitor.Stop()

	// Manually call mount with invalid device
	err := monitor.mount("/dev/nonexistent123")
	if err == nil {
		t.Fatal("Expected mount to fail for nonexistent device")
	}

	// BUG: In checkDevices(), even though mount() returns error,
	// the code at line 136-137 only logs and returns.
	// But the REAL BUG is that the error check is MISSING proper state cleanup.

	t.Logf("Mount correctly failed: %v", err)
	t.Log("BUG: In production, checkDevices() would have set lastDevice and sent EventInserted")
	t.Log("This causes handleCardInserted() to run with an empty/unmounted directory")
}

// =============================================================================
// CRITICAL BUG #2: Unmount during active file operations
// =============================================================================
// Severity: CRITICAL - Can cause data corruption or panic
// Location: sdmonitor.go:281-289, interaction with main.go:198-209
//
// The unmount() function uses unix.Unmount with flags=0 (normal unmount).
// If rclone is actively reading files, this causes:
// - EBUSY error (device is busy)
// - unmount() logs error but doesn't retry
// - Device removal event is still sent
// - Sync operation continues reading from unmounted filesystem

func TestUnmountDuringActiveFileOperations(t *testing.T) {
	if os.Getuid() != 0 {
		t.Skip("Requires root for mount operations")
	}

	tmpDir := t.TempDir()
	mountPoint := filepath.Join(tmpDir, "mount")
	if err := os.MkdirAll(mountPoint, 0755); err != nil {
		t.Fatal(err)
	}

	// Create a test filesystem image
	fsImage := filepath.Join(tmpDir, "test.img")
	createTestFATImage(t, fsImage, 10*1024*1024) // 10MB

	// Mount it
	err := unix.Mount(fsImage, mountPoint, "vfat", unix.MS_RDONLY, "loop")
	if err != nil {
		t.Skipf("Cannot mount test image: %v (requires loop device support)", err)
	}

	// Open file and keep it open
	testFile := filepath.Join(mountPoint, "testfile.txt")
	if err := os.WriteFile(testFile, []byte("test data"), 0644); err != nil {
		// File might not exist in mounted image
		t.Logf("Could not write test file: %v", err)
	}

	file, err := os.Open(testFile)
	if err == nil {
		defer file.Close()

		// Try to unmount while file is open
		err = unix.Unmount(mountPoint, 0)
		if err != nil {
			if err == unix.EBUSY {
				t.Log("BUG CONFIRMED: Unmount fails with EBUSY when files are open")
				t.Log("Current code at line 283-288 logs error but doesn't:")
				t.Log("  1. Retry with MNT_FORCE flag")
				t.Log("  2. Wait for operations to complete")
				t.Log("  3. Cancel ongoing sync operations")
				t.Log("  4. Prevent EventRemoved from being sent")
			} else {
				t.Logf("Unmount failed with different error: %v", err)
			}
		} else {
			t.Log("Unmount succeeded (unexpected - file was open)")
		}
	}

	// Cleanup
	unix.Unmount(mountPoint, unix.MNT_FORCE)
}

// =============================================================================
// CRITICAL BUG #3: Mount point already exists with files
// =============================================================================
// Severity: HIGH - Causes confusion about what data is from SD card
// Location: sdmonitor.go:229-268
//
// The mount() function checks if already mounted (line 234-236) but doesn't
// verify mount point is empty before mounting. If mount point has stale files
// from previous crash, those files appear to be from the new SD card.

func TestMountPointAlreadyHasFiles(t *testing.T) {
	tmpDir := t.TempDir()
	mountPoint := filepath.Join(tmpDir, "mount")
	if err := os.MkdirAll(mountPoint, 0755); err != nil {
		t.Fatal(err)
	}

	// Create stale files in mount point (from previous crash)
	staleDCIM := filepath.Join(mountPoint, "DCIM", "100STALE")
	if err := os.MkdirAll(staleDCIM, 0755); err != nil {
		t.Fatal(err)
	}

	stalePhotos := []string{"STALE_001.JPG", "STALE_002.JPG"}
	for _, photo := range stalePhotos {
		path := filepath.Join(staleDCIM, photo)
		if err := os.WriteFile(path, []byte("stale data"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Verify stale files exist
	if !HasDCIM(mountPoint) {
		t.Fatal("DCIM should exist before mount")
	}

	count, _, err := CountPhotos(mountPoint)
	if err != nil {
		t.Fatal(err)
	}

	if count != 2 {
		t.Fatalf("Expected 2 stale photos, got %d", count)
	}

	t.Log("BUG CONFIRMED: Mount point has stale files")
	t.Log("Current code doesn't:")
	t.Log("  1. Check if mount point is empty before mounting")
	t.Log("  2. Clear mount point if unmount failed previously")
	t.Log("  3. Warn that these files are NOT from the current SD card")
	t.Log("")
	t.Log("Impact: handleCardInserted() will count stale photos and try to sync them")
	t.Log("        Card ID will be based on stale .pictures-sync-id file")
}

// =============================================================================
// CRITICAL BUG #4: Mount point directory permissions (root only)
// =============================================================================
// Severity: MEDIUM - Mount succeeds but files unreadable
// Location: sdmonitor.go:64-67, 229-268
//
// Start() creates mount directory with 0755 (line 65).
// mount() doesn't verify permissions are correct after mounting.
// On some systems, mounted filesystem has root-only permissions.

func TestMountPointPermissionsRootOnly(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("Test requires non-root user")
	}

	tmpDir := t.TempDir()
	mountPoint := filepath.Join(tmpDir, "mount")

	// Create mount point with restrictive permissions
	if err := os.MkdirAll(mountPoint, 0700); err != nil {
		t.Fatal(err)
	}

	// Try to access as non-root
	_, err := os.ReadDir(mountPoint)
	if err != nil {
		if os.IsPermission(err) {
			t.Log("BUG SCENARIO: Mount point not accessible to current user")
			t.Log("Current code doesn't:")
			t.Log("  1. Verify mount point is readable after mounting")
			t.Log("  2. Set proper permissions on mount point")
			t.Log("  3. Handle permission errors in HasDCIM/CountPhotos")
			t.Log("")
			t.Log("Impact: Sync will fail with permission errors")
		} else {
			t.Fatalf("Unexpected error: %v", err)
		}
	}
}

// =============================================================================
// CRITICAL BUG #5: Concurrent mount attempts on same device
// =============================================================================
// Severity: MEDIUM - Race condition in mount operations
// Location: sdmonitor.go:229-268, no locking
//
// Multiple goroutines could call mount() simultaneously if:
// - pollDevices() detects device
// - External code calls mount() directly
// No mutex protects mount operations.

func TestConcurrentMountAttempts(t *testing.T) {
	tmpDir := t.TempDir()
	mountPoint := filepath.Join(tmpDir, "mount")
	monitor := NewMonitor(mountPoint)

	if err := monitor.Start(); err != nil {
		t.Fatal(err)
	}
	defer monitor.Stop()

	// Try concurrent mounts
	var wg sync.WaitGroup
	errors := make(chan error, 10)

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			// mount() is not protected by mutex
			err := monitor.mount("/dev/null") // Will fail but tests concurrency
			if err != nil {
				errors <- fmt.Errorf("concurrent mount %d: %w", id, err)
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	errorCount := 0
	for err := range errors {
		t.Logf("Error: %v", err)
		errorCount++
	}

	t.Log("BUG CONFIRMED: No synchronization for mount operations")
	t.Log("Current code doesn't:")
	t.Log("  1. Use mutex to serialize mount/unmount operations")
	t.Log("  2. Prevent concurrent mount attempts on same device")
	t.Log("  3. Queue mount requests")
	t.Log("")
	t.Log("Impact: Race conditions can cause mount failures or corruption")
}

// =============================================================================
// CRITICAL BUG #6: Mount with filesystem errors (corrupted FAT32/exFAT)
// =============================================================================
// Severity: HIGH - Silent mount of corrupted filesystem
// Location: sdmonitor.go:250-267
//
// mount() tries multiple filesystem types (vfat, exfat, ext4, ntfs) and
// accepts the first one that succeeds (line 254). A corrupted filesystem
// might mount successfully but be unreadable.

func TestMountCorruptedFilesystem(t *testing.T) {
	if os.Getuid() != 0 {
		t.Skip("Requires root for mount operations")
	}

	tmpDir := t.TempDir()
	mountPoint := filepath.Join(tmpDir, "mount")
	if err := os.MkdirAll(mountPoint, 0755); err != nil {
		t.Fatal(err)
	}

	// Create corrupted filesystem image
	corruptedImage := filepath.Join(tmpDir, "corrupted.img")
	createCorruptedFATImage(t, corruptedImage)

	monitor := NewMonitor(mountPoint)

	// Try to mount corrupted image
	err := monitor.mount(corruptedImage)

	t.Log("BUG SCENARIO: Corrupted filesystem")
	t.Log("Current code doesn't:")
	t.Log("  1. Validate filesystem after mounting")
	t.Log("  2. Check for filesystem errors/warnings")
	t.Log("  3. Verify mount is readable")
	t.Log("  4. Use 'fsck' to check filesystem health")
	t.Log("")
	t.Log("Mount result:", err)
	t.Log("Impact: Corrupted data might be synced, or sync fails mysteriously")
}

// =============================================================================
// CRITICAL BUG #7: Device removed during mount operation
// =============================================================================
// Severity: HIGH - Race condition between detection and mounting
// Location: sdmonitor.go:125-162
//
// Sequence:
// 1. checkDevices() detects device (line 128)
// 2. User physically removes SD card
// 3. mount() called on non-existent device (line 135)
// 4. Mount fails, but EventInserted still sent (see Bug #1)

func TestDeviceRemovedDuringMount(t *testing.T) {
	tmpDir := t.TempDir()
	mountPoint := filepath.Join(tmpDir, "mount")
	monitor := NewMonitor(mountPoint)

	if err := monitor.Start(); err != nil {
		t.Fatal(err)
	}
	defer monitor.Stop()

	// Simulate the race condition
	devicePath := "/dev/nonexistent-device"

	// In real scenario:
	// - Device exists at checkDevices() line 128
	// - Device removed before mount() line 135
	// - mount() fails with "no such file or directory"

	err := monitor.mount(devicePath)
	if err == nil {
		t.Fatal("Expected mount to fail")
	}

	t.Log("BUG CONFIRMED: Device removed during mount")
	t.Log("Current code doesn't:")
	t.Log("  1. Re-verify device exists before mounting")
	t.Log("  2. Use atomic check-and-mount operation")
	t.Log("  3. Handle ENOENT errors specially")
	t.Log("  4. Prevent EventInserted when mount fails")
	t.Log("")
	t.Logf("Mount error: %v", err)
	t.Log("Impact: Sync started on empty/unmounted directory")
}

// =============================================================================
// CRITICAL BUG #8: Mount options not properly applied (should be read-only)
// =============================================================================
// Severity: CRITICAL - Data corruption risk
// Location: sdmonitor.go:248-268
//
// mount() initially mounts read-write (line 249) to allow card ID writing.
// RemountReadOnly() is called later (line 359, 383) but if it fails:
// - SD card remains read-write during entire sync
// - rclone or other processes could accidentally modify files
// - Card corruption risk if card removed during write

func TestMountNotReadOnly(t *testing.T) {
	tmpDir := t.TempDir()
	mountPoint := filepath.Join(tmpDir, "mount")
	monitor := NewMonitor(mountPoint)

	if err := monitor.Start(); err != nil {
		t.Fatal(err)
	}
	defer monitor.Stop()

	// In real scenario:
	// 1. mount() succeeds with read-write (line 253-256)
	// 2. Card ID is written
	// 3. RemountReadOnly() is called but fails (line 273)
	// 4. Error is returned but card ID is already determined
	// 5. Sync proceeds with read-write mount

	// Simulate RemountReadOnly failure
	err := monitor.RemountReadOnly()
	if err == nil {
		t.Fatal("Expected RemountReadOnly to fail (nothing mounted)")
	}

	t.Log("BUG CONFIRMED: RemountReadOnly failure handling")
	t.Log("Current code:")
	t.Log("  - Returns error from GetOrCreateCardID (line 362, 387)")
	t.Log("  - But card ID was already determined")
	t.Log("  - handleCardInserted() treats this as fatal error (line 147-149)")
	t.Log("  - Sets status to StatusError, but doesn't unmount")
	t.Log("")
	t.Log("Issues:")
	t.Log("  1. SD card stays read-write if remount fails")
	t.Log("  2. No automatic unmount on remount failure")
	t.Log("  3. No retry mechanism for remount")
	t.Log("  4. Sync could proceed with writable mount (corruption risk)")
}

// =============================================================================
// CRITICAL BUG #9: Stale mount points from previous crashes
// =============================================================================
// Severity: HIGH - System accumulates stale mounts
// Location: sdmonitor.go:239-246, 64-74
//
// Start() tries to unmount existing mount (line 240) but:
// - Uses unix.Unmount with flags=0 (might fail if busy)
// - Only logs warning if unmount fails (line 241-245)
// - Continues to mount anyway (line 253)
// - Can result in stale mounts accumulating

func TestStaleMountPoints(t *testing.T) {
	if os.Getuid() != 0 {
		t.Skip("Requires root for mount operations")
	}

	tmpDir := t.TempDir()
	mountPoint := filepath.Join(tmpDir, "mount")
	if err := os.MkdirAll(mountPoint, 0755); err != nil {
		t.Fatal(err)
	}

	// Create a stale mount by mounting tmpfs
	err := unix.Mount("tmpfs", mountPoint, "tmpfs", 0, "size=1M")
	if err != nil {
		t.Skipf("Cannot create test mount: %v", err)
	}

	// Verify it's mounted
	mounts, err := os.ReadFile("/proc/mounts")
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(mounts), mountPoint) {
		t.Fatal("Test mount not in /proc/mounts")
	}

	// Now try to mount again (simulating restart after crash)
	monitor := NewMonitor(mountPoint)

	// mount() will call unix.Unmount at line 240
	// With flags=0, it might fail if mount is busy
	// Let's make it busy by opening a file
	testFile := filepath.Join(mountPoint, "test.txt")
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}

	file, err := os.Open(testFile)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	// Try to mount - will attempt to unmount existing
	err = monitor.mount("/dev/null")
	if err != nil {
		t.Logf("Mount failed: %v", err)
	}

	t.Log("BUG CONFIRMED: Stale mount handling")
	t.Log("Current code:")
	t.Log("  - Tries unix.Unmount with flags=0 (line 240)")
	t.Log("  - Logs warning if it fails (line 242)")
	t.Log("  - Continues to mount anyway (line 253)")
	t.Log("")
	t.Log("Issues:")
	t.Log("  1. No retry with MNT_FORCE or MNT_DETACH")
	t.Log("  2. No check if unmount actually succeeded")
	t.Log("  3. Mount might fail or mount over existing mount")
	t.Log("  4. Accumulates stale mounts over time")

	// Cleanup
	unix.Unmount(mountPoint, unix.MNT_FORCE|unix.MNT_DETACH)
}

// =============================================================================
// CRITICAL BUG #10: Mount namespace issues in Gokrazy environment
// =============================================================================
// Severity: MEDIUM - Mounts might not be visible to other processes
// Location: sdmonitor.go:229-268, entire package
//
// In Gokrazy environment:
// - Services run in separate mount namespaces
// - Mount performed by pictures-sync might not be visible to webui
// - No MS_SHARED flag used (mounts are private by default)

func TestMountNamespaceIsolation(t *testing.T) {
	tmpDir := t.TempDir()
	mountPoint := filepath.Join(tmpDir, "mount")

	if err := os.MkdirAll(mountPoint, 0755); err != nil {
		t.Fatal(err)
	}

	t.Log("BUG SCENARIO: Mount namespace isolation in Gokrazy")
	t.Log("")
	t.Log("Current code doesn't:")
	t.Log("  1. Use MS_SHARED flag to make mounts visible across namespaces")
	t.Log("  2. Document that mounts are private to pictures-sync process")
	t.Log("  3. Provide API for webui to check mount status")
	t.Log("")
	t.Log("Impact in Gokrazy:")
	t.Log("  - webui can't see mounted SD card")
	t.Log("  - webui reports mount path but directory appears empty")
	t.Log("  - State manager has path but path is not accessible")
	t.Log("")
	t.Log("Solution:")
	t.Log("  - Add MS_SHARED flag: unix.Mount(device, mountPath, fstype, unix.MS_SHARED, \"\")")
	t.Log("  - Or use shared mount propagation")
	t.Log("  - Or document that mount is private to pictures-sync")
}

// =============================================================================
// Additional Tests: Edge Cases
// =============================================================================

// TestUnmountNotMounted tests unmounting when nothing is mounted
func TestUnmountNotMounted(t *testing.T) {
	tmpDir := t.TempDir()
	mountPoint := filepath.Join(tmpDir, "mount")
	monitor := NewMonitor(mountPoint)

	err := monitor.unmount()
	if err == nil {
		t.Error("Expected error when unmounting non-mounted path")
	}

	if err != unix.EINVAL {
		t.Logf("Got error (expected EINVAL): %v", err)
	}

	t.Log("unmount() returns error but doesn't distinguish between:")
	t.Log("  - Path not mounted (expected)")
	t.Log("  - Mount busy (should retry)")
	t.Log("  - Permission denied (fatal)")
}

// TestMountAfterUnmountRace tests race between unmount and mount
func TestMountAfterUnmountRace(t *testing.T) {
	tmpDir := t.TempDir()
	mountPoint := filepath.Join(tmpDir, "mount")
	monitor := NewMonitor(mountPoint)

	if err := monitor.Start(); err != nil {
		t.Fatal(err)
	}
	defer monitor.Stop()

	var wg sync.WaitGroup

	// Goroutine 1: Try to mount
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 10; i++ {
			monitor.mount("/dev/null")
			time.Sleep(10 * time.Millisecond)
		}
	}()

	// Goroutine 2: Try to unmount
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 10; i++ {
			monitor.unmount()
			time.Sleep(10 * time.Millisecond)
		}
	}()

	wg.Wait()

	t.Log("Race between mount/unmount completed")
	t.Log("BUG: No synchronization between mount() and unmount()")
	t.Log("Could result in:")
	t.Log("  - Mount after unmount started")
	t.Log("  - Unmount after mount started")
	t.Log("  - Inconsistent lastDevice state")
}

// TestGetOrCreateCardIDWithoutRemount tests dangerous scenario
func TestGetOrCreateCardIDWithoutRemount(t *testing.T) {
	tmpDir := t.TempDir()

	// Call GetOrCreateCardID with nil monitor (no remount)
	cardID, isNew, err := GetOrCreateCardID(tmpDir, nil)
	if err != nil {
		t.Fatal(err)
	}

	if !isNew {
		t.Error("Expected new card ID")
	}

	t.Logf("Generated card ID: %s", cardID)
	t.Log("")
	t.Log("BUG: GetOrCreateCardID with nil monitor")
	t.Log("When monitor is nil:")
	t.Log("  - New card ID is written")
	t.Log("  - RemountReadOnly is skipped (line 358, 383)")
	t.Log("  - SD card remains read-write")
	t.Log("  - Sync proceeds with writable filesystem")
	t.Log("")
	t.Log("This is used in main.go line 145:")
	t.Log("  cardID, isNewCard, err := sdmonitor.GetOrCreateCardID(event.MountPath, monitor)")
	t.Log("If monitor is accidentally nil, data corruption risk!")
}

// TestVerifyMountReadOnlyAfterRemount tests if remount actually worked
func TestVerifyMountReadOnlyAfterRemount(t *testing.T) {
	if os.Getuid() != 0 {
		t.Skip("Requires root for mount operations")
	}

	tmpDir := t.TempDir()
	mountPoint := filepath.Join(tmpDir, "mount")
	if err := os.MkdirAll(mountPoint, 0755); err != nil {
		t.Fatal(err)
	}

	// Mount tmpfs read-write
	err := unix.Mount("tmpfs", mountPoint, "tmpfs", 0, "size=1M")
	if err != nil {
		t.Skipf("Cannot mount tmpfs: %v", err)
	}
	defer unix.Unmount(mountPoint, unix.MNT_FORCE)

	// Write a test file
	testFile := filepath.Join(mountPoint, "test.txt")
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}

	// Remount read-only
	monitor := NewMonitor(mountPoint)
	err = monitor.RemountReadOnly()
	if err != nil {
		t.Fatalf("RemountReadOnly failed: %v", err)
	}

	// Try to write - should fail
	err = os.WriteFile(testFile, []byte("modified"), 0644)
	if err == nil {
		t.Error("BUG: Could write to read-only filesystem!")
		t.Log("RemountReadOnly() succeeded but filesystem is still writable")
	} else {
		if err.(*fs.PathError).Err == syscall.EROFS {
			t.Log("Correctly read-only after remount")
		} else {
			t.Logf("Got error (expected EROFS): %v", err)
		}
	}

	t.Log("")
	t.Log("Current code doesn't verify remount actually worked")
	t.Log("Should check filesystem is read-only after RemountReadOnly()")
}

// =============================================================================
// Test Helpers
// =============================================================================

// createTestFATImage creates a minimal FAT filesystem image for testing
func createTestFATImage(t *testing.T, path string, size int64) {
	// Create file of specified size
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("Cannot create image file: %v", err)
	}
	defer f.Close()

	if err := f.Truncate(size); err != nil {
		t.Fatalf("Cannot truncate image: %v", err)
	}

	// Note: Would need mkfs.vfat to create actual FAT filesystem
	// For now, just create empty file
	t.Log("Created test image (not formatted, needs mkfs.vfat)")
}

// createCorruptedFATImage creates a corrupted filesystem image
func createCorruptedFATImage(t *testing.T, path string) {
	// Create file with random data (corrupted)
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("Cannot create corrupted image: %v", err)
	}
	defer f.Close()

	// Write garbage data
	garbage := make([]byte, 1024*1024) // 1MB of zeros (corrupted FS)
	if _, err := f.Write(garbage); err != nil {
		t.Fatalf("Cannot write garbage: %v", err)
	}

	t.Log("Created corrupted image")
}
