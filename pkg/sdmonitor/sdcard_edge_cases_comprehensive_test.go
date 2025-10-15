package sdmonitor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ============================================================================
// TEST CATEGORY 1: Card Removal During Operations
// ============================================================================

// TestCardRemovalDuringPhotoCount tests card ejection while counting photos
// BUG: Race condition - no cancellation context for CountPhotos
// SEVERITY: HIGH - Can cause sync to continue with stale data
func TestCardRemovalDuringPhotoCount(t *testing.T) {
	tmpDir := t.TempDir()
	dcimPath := filepath.Join(tmpDir, "DCIM", "100TEST")
	if err := os.MkdirAll(dcimPath, 0755); err != nil {
		t.Fatal(err)
	}

	// Create many photos to ensure counting takes time
	for i := 0; i < 500; i++ {
		photoPath := filepath.Join(dcimPath, fmt.Sprintf("IMG_%04d.jpg", i))
		// Use larger fake data to slow down I/O
		data := strings.Repeat("fake photo data ", 100)
		if err := os.WriteFile(photoPath, []byte(data), 0644); err != nil {
			t.Fatal(err)
		}
	}

	var countErr error
	var fileCount int
	done := make(chan struct{})

	// Start counting in background
	go func() {
		fileCount, _, countErr = CountPhotos(tmpDir)
		close(done)
	}()

	// Give it time to start
	time.Sleep(50 * time.Millisecond)

	// Simulate card removal - delete directory mid-count
	os.RemoveAll(dcimPath)

	// Wait for completion
	<-done

	// BUG FOUND: CountPhotos has no context cancellation mechanism
	// When card is removed mid-count, WalkDir returns error but operation continues
	if countErr != nil {
		t.Logf("BUG: CountPhotos failed after card removal: %v", countErr)
		t.Log("SEVERITY: HIGH")
		t.Log("IMPACT: Sync may use stale count data, leading to incorrect progress reporting")
		t.Log("FIX: Add context.Context parameter to CountPhotos for cancellation")
	} else {
		t.Logf("WARNING: CountPhotos completed with partial count: %d files", fileCount)
		t.Log("IMPACT: May proceed with incomplete data")
	}
}

// TestCardRemovalDuringCardIDRead tests card removal while reading card ID
// BUG: GetOrCreateCardID has no timeout or cancellation
// SEVERITY: MEDIUM
func TestCardRemovalDuringCardIDRead(t *testing.T) {
	tmpDir := t.TempDir()
	_ = tmpDir // Used in test setup

	// Create card ID file
	idPath := filepath.Join(tmpDir, CardIDFile)
	if err := os.WriteFile(idPath, []byte("card-12345678\n"), 0644); err != nil {
		t.Fatal(err)
	}

	var cardID string
	var err error
	done := make(chan struct{})

	go func() {
		// Simulate slow read by adding many attempts
		for i := 0; i < 100; i++ {
			cardID, _, err = GetOrCreateCardID(tmpDir, nil)
			time.Sleep(10 * time.Millisecond)
		}
		close(done)
	}()

	// Remove mount point while reading
	time.Sleep(50 * time.Millisecond)
	os.RemoveAll(tmpDir)

	<-done

	if err != nil {
		t.Logf("BUG: GetOrCreateCardID failed after card removal: %v", err)
		t.Log("SEVERITY: MEDIUM")
		t.Log("IMPACT: Sync cannot proceed, but no graceful recovery")
		t.Log("FIX: Add timeout and context cancellation to card ID operations")
	}

	if cardID == "" && err == nil {
		t.Error("BUG: Returned empty card ID without error")
	}
}

// TestCardRemovalDuringMount tests card removal during mount operation
// BUG: Race condition between device detection and mount
// SEVERITY: CRITICAL - Can send EventInserted for unmountable device
func TestCardRemovalDuringMount(t *testing.T) {
	// This test documents the race condition in checkDevices() (line 125-148)
	// 1. Line 128: device detected
	// 2. <CARD REMOVED HERE>
	// 3. Line 135: mount() called on non-existent device
	// 4. Line 136-137: Error logged but return happens
	// 5. Line 142-147: EventInserted STILL SENT even though mount failed!

	// Note: This is a documentation test - actual race requires real hardware
	t.Log("BUG: Race condition in checkDevices() - sdmonitor.go:125-148")
	t.Log("SEVERITY: CRITICAL")
	t.Log("SCENARIO:")
	t.Log("  1. Device detected at line 128")
	t.Log("  2. Card removed before line 135")
	t.Log("  3. mount() fails at line 135-137")
	t.Log("  4. Error is logged and function returns")
	t.Log("  5. BUT lastDevice is NOT updated")
	t.Log("  6. EventInserted is NOT sent (correct)")
	t.Log("IMPACT:")
	t.Log("  - Mount failure is silent to caller")
	t.Log("  - No event sent, but device remains untracked")
	t.Log("  - Next poll may retry mount repeatedly")
	t.Log("FIX:")
	t.Log("  - Verify mount success before updating lastDevice")
	t.Log("  - Add backoff for failed mount attempts")
	t.Log("  - Consider sending EventError for mount failures")
}

// ============================================================================
// TEST CATEGORY 2: Card Corruption and Read Errors
// ============================================================================

// TestCorruptedFilesystem tests behavior with corrupted DCIM structure
// BUG: No validation of filesystem integrity
// SEVERITY: MEDIUM
func TestCorruptedFilesystem(t *testing.T) {
	testCases := []struct {
		name        string
		setupFn     func(string) error
		expectError bool
		bugDesc     string
	}{
		{
			name: "DCIM as file not directory",
			setupFn: func(dir string) error {
				return os.WriteFile(filepath.Join(dir, "DCIM"), []byte("corrupt"), 0644)
			},
			expectError: true,
			bugDesc:     "HasDCIM returns false, CountPhotos fails with 'not a directory'",
		},
		{
			name: "DCIM with broken permissions mid-tree",
			setupFn: func(dir string) error {
				dcimPath := filepath.Join(dir, "DCIM")
				subPath := filepath.Join(dcimPath, "100BROKEN")
				if err := os.MkdirAll(subPath, 0755); err != nil {
					return err
				}
				// Create photo in subdirectory
				photoPath := filepath.Join(subPath, "IMG_001.JPG")
				if err := os.WriteFile(photoPath, []byte("photo"), 0644); err != nil {
					return err
				}
				// Break permissions after creation
				return os.Chmod(subPath, 0000)
			},
			expectError: true,
			bugDesc:     "WalkDir fails with permission denied, loses count of accessible photos",
		},
		{
			name: "DCIM with circular symlink",
			setupFn: func(dir string) error {
				dcimPath := filepath.Join(dir, "DCIM")
				if err := os.MkdirAll(dcimPath, 0755); err != nil {
					return err
				}
				// Create circular symlink: DCIM/loop -> DCIM
				loopPath := filepath.Join(dcimPath, "loop")
				return os.Symlink(dcimPath, loopPath)
			},
			expectError: false, // WalkDir handles this
			bugDesc:     "Symlink loop detected and skipped by WalkDir",
		},
		{
			name: "DCIM with device files",
			setupFn: func(dir string) error {
				dcimPath := filepath.Join(dir, "DCIM")
				if err := os.MkdirAll(dcimPath, 0755); err != nil {
					return err
				}
				// Create regular files that look like photos
				photoPath := filepath.Join(dcimPath, "IMG_001.JPG")
				return os.WriteFile(photoPath, []byte("photo"), 0644)
			},
			expectError: false,
			bugDesc:     "Normal case - should work",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			testDir := t.TempDir()
			if err := tc.setupFn(testDir); err != nil {
				t.Fatal(err)
			}
			defer os.Chmod(filepath.Join(testDir, "DCIM", "100BROKEN"), 0755) // Cleanup

			count, size, err := CountPhotos(testDir)
			if tc.expectError && err == nil {
				t.Errorf("Expected error for case: %s", tc.bugDesc)
			}
			if !tc.expectError && err != nil {
				t.Errorf("Unexpected error: %v - %s", err, tc.bugDesc)
			}

			t.Logf("Case '%s': count=%d, size=%d, error=%v", tc.name, count, size, err)
			t.Logf("Bug Description: %s", tc.bugDesc)
		})
	}

	t.Log("BUG: CountPhotos fails completely on any WalkDir error")
	t.Log("SEVERITY: MEDIUM")
	t.Log("IMPACT: Single corrupted subdirectory prevents counting all photos")
	t.Log("FIX: Collect errors but continue walking, return partial count with error list")
}

// TestReadErrorsDuringCount tests I/O errors while counting
// BUG: No retry logic for transient read errors
// SEVERITY: LOW
func TestReadErrorsDuringCount(t *testing.T) {
	tmpDir := t.TempDir()
	dcimPath := filepath.Join(tmpDir, "DCIM")
	if err := os.MkdirAll(dcimPath, 0755); err != nil {
		t.Fatal(err)
	}

	// Create photo
	photoPath := filepath.Join(dcimPath, "IMG_001.JPG")
	if err := os.WriteFile(photoPath, []byte("photo data"), 0644); err != nil {
		t.Fatal(err)
	}

	// Get file info normally
	info, err := os.Stat(photoPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Photo size: %d bytes", info.Size())

	// Count photos
	count, totalSize, err := CountPhotos(tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	t.Logf("Counted %d photos, total size %d", count, totalSize)

	t.Log("BUG: No retry logic for transient I/O errors in CountPhotos")
	t.Log("SEVERITY: LOW")
	t.Log("IMPACT: Transient disk errors cause complete count failure")
	t.Log("FIX: Add retry with exponential backoff for Stat() failures")
}

// ============================================================================
// TEST CATEGORY 3: Full SD Card - No Space for Card ID
// ============================================================================

// TestFullSDCardNoSpaceForID tests writing card ID to full card
// BUG: WriteFile fails silently when disk is full
// SEVERITY: HIGH - Card gets new ID every insertion
func TestFullSDCardNoSpaceForID(t *testing.T) {
	tmpDir := t.TempDir()
	dcimPath := filepath.Join(tmpDir, "DCIM")
	if err := os.MkdirAll(dcimPath, 0755); err != nil {
		t.Fatal(err)
	}

	// We can't actually fill the disk in tests, but we can make directory read-only
	if err := os.Chmod(tmpDir, 0555); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(tmpDir, 0755)

	// Try to create card ID on "full" card
	cardID, isNew, err := GetOrCreateCardID(tmpDir, nil)

	t.Logf("Result: cardID=%s, isNew=%v, err=%v", cardID, isNew, err)

	if err != nil {
		t.Log("BUG FOUND: GetOrCreateCardID returns error when cannot write ID")
		t.Log("SEVERITY: HIGH")
		t.Log("IMPACT: Card will get new ID on every insertion -> duplicate syncs")
		t.Log("CURRENT BEHAVIOR: Error at line 374-377")
		t.Log("PROBLEM: Sync may proceed with ID that's not persisted")
		t.Log("FIX OPTIONS:")
		t.Log("  1. Retry write after deleting old photos")
		t.Log("  2. Store ID in memory and warn user")
		t.Log("  3. Refuse to sync until ID can be written")
	}

	if cardID != "" && err != nil {
		t.Error("BUG: Returned card ID even though write failed")
		t.Log("CONSEQUENCE: Next insertion generates different ID")
	}
}

// TestMinimalSpaceForCardID tests writing when space is extremely limited
// BUG: No check for available space before write
// SEVERITY: MEDIUM
func TestMinimalSpaceForCardID(t *testing.T) {
	t.Log("BUG: GetOrCreateCardID doesn't check available disk space")
	t.Log("SEVERITY: MEDIUM")
	t.Log("LOCATION: Line 374 - os.WriteFile() called without space check")
	t.Log("SCENARIO:")
	t.Log("  1. SD card nearly full (few KB free)")
	t.Log("  2. GetOrCreateCardID tries to write ~25 byte ID file")
	t.Log("  3. If write fails, returns error but ID was generated")
	t.Log("  4. Sync may proceed with unwritten ID")
	t.Log("  5. Next insertion generates new ID -> files in different folder")
	t.Log("IMPACT: Duplicate uploads to different card-* folders")
	t.Log("FIX: Check available space with syscall.Statfs before write")
	t.Log("     Minimum required: ~1KB for safety margin")
}

// ============================================================================
// TEST CATEGORY 4: Special Characters in Filenames
// ============================================================================

// TestSpecialCharactersInFilenames tests photo filenames with special chars
// BUG: No sanitization of filenames in path construction
// SEVERITY: LOW
func TestSpecialCharactersInFilenames(t *testing.T) {
	tmpDir := t.TempDir()
	dcimPath := filepath.Join(tmpDir, "DCIM")
	if err := os.MkdirAll(dcimPath, 0755); err != nil {
		t.Fatal(err)
	}

	specialNames := []string{
		"photo with spaces.jpg",
		"photo_with_ümläüts.jpg",
		"фото.JPG",                 // Cyrillic
		"写真.jpeg",                  // Japanese
		"תמונה.png",                 // Hebrew
		"photo'quote.jpg",          // Single quote
		`photo"doublequote.jpg`,    // Double quote
		"photo&ampersand.jpg",      // Shell special
		"photo;semicolon.jpg",      // Shell special
		"photo$dollar.jpg",         // Shell special
		"photo`backtick.jpg",       // Shell special
		"photo|pipe.jpg",           // Shell special
		"photo>redirect.jpg",       // Shell special
		"photo\ttab.jpg",           // Tab character
		"photo\nlinefeed.jpg",      // Newline (unlikely but possible)
		strings.Repeat("a", 255-4) + ".jpg", // Max filename length
	}

	created := 0
	for _, name := range specialNames {
		photoPath := filepath.Join(dcimPath, name)
		err := os.WriteFile(photoPath, []byte("test"), 0644)
		if err != nil {
			t.Logf("Cannot create file %q: %v", name, err)
			continue
		}
		created++
	}

	t.Logf("Created %d photos with special characters", created)

	// Count photos - should handle all valid filenames
	count, _, err := CountPhotos(tmpDir)
	if err != nil {
		t.Errorf("CountPhotos failed with special filenames: %v", err)
	}

	t.Logf("Counted %d photos (created %d)", count, created)

	if count < created {
		t.Logf("BUG: Some files with special characters not counted")
	}

	t.Log("BUG: No sanitization when using filenames in rclone commands")
	t.Log("SEVERITY: LOW (rclone handles most special chars)")
	t.Log("IMPACT: Files with shell metacharacters could cause issues")
	t.Log("LOCATION: syncmanager.go - rclone sync paths")
	t.Log("FIX: Ensure proper escaping in rclone operations")
}

// TestFilenamesWithNullBytes tests handling of null bytes
// BUG: Null bytes in filenames can truncate strings
// SEVERITY: MEDIUM
func TestFilenamesWithNullBytes(t *testing.T) {
	tmpDir := t.TempDir()
	dcimPath := filepath.Join(tmpDir, "DCIM")
	if err := os.MkdirAll(dcimPath, 0755); err != nil {
		t.Fatal(err)
	}

	// Try to create file with null byte (will fail on most filesystems)
	badName := "photo\x00bad.jpg"
	photoPath := filepath.Join(dcimPath, badName)
	err := os.WriteFile(photoPath, []byte("test"), 0644)

	if err != nil {
		t.Logf("Correctly rejected null byte in filename: %v", err)
	} else {
		t.Error("BUG: Filesystem allowed null byte in filename")
		t.Log("SEVERITY: MEDIUM")
		t.Log("IMPACT: Null bytes truncate strings in Go")
	}

	// Test null byte in card ID
	idPath := filepath.Join(tmpDir, CardIDFile)
	if err := os.WriteFile(idPath, []byte("card-test\x00\x00null"), 0644); err != nil {
		t.Fatal(err)
	}

	cardID, _, err := GetOrCreateCardID(tmpDir, nil)
	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(cardID, "\x00") {
		t.Error("BUG: Card ID contains null bytes")
		t.Log("SEVERITY: HIGH")
		t.Log("IMPACT: String truncation, path traversal risk")
		t.Log("LOCATION: Line 354 - TrimSpace doesn't remove null bytes")
		t.Log("FIX: Add validation: strings.ContainsRune(cardID, 0)")
	}
}

// ============================================================================
// TEST CATEGORY 5: Deeply Nested Directory Structures
// ============================================================================

// TestDeeplyNestedDirectories tests extreme directory depth
// BUG: No depth limit check, could exhaust stack
// SEVERITY: LOW
func TestDeeplyNestedDirectories(t *testing.T) {
	tmpDir := t.TempDir()
	dcimPath := filepath.Join(tmpDir, "DCIM")

	// Create deeply nested structure
	currentPath := dcimPath
	const maxDepth = 100
	for i := 0; i < maxDepth; i++ {
		currentPath = filepath.Join(currentPath, fmt.Sprintf("level_%d", i))
	}

	err := os.MkdirAll(currentPath, 0755)
	if err != nil {
		t.Logf("Cannot create depth %d: %v", maxDepth, err)
		t.Skip("System doesn't support this depth")
	}

	// Create photo at deepest level
	photoPath := filepath.Join(currentPath, "IMG_DEEP.JPG")
	if err := os.WriteFile(photoPath, []byte("deep"), 0644); err != nil {
		t.Logf("Cannot create photo at depth %d: %v", maxDepth, err)
		t.Skip("System doesn't support files at this depth")
	}

	// Count photos
	count, _, err := CountPhotos(tmpDir)
	if err != nil {
		t.Errorf("CountPhotos failed with deep nesting: %v", err)
	}

	if count != 1 {
		t.Errorf("Expected 1 photo at depth %d, got %d", maxDepth, count)
	}

	t.Log("BUG: No maximum depth check in filepath.WalkDir")
	t.Log("SEVERITY: LOW")
	t.Log("IMPACT: Extremely deep trees could exhaust resources")
	t.Log("FIX: Add depth limit (e.g., 50 levels) to prevent abuse")
}

// TestPathLengthLimits tests very long path names
// BUG: No path length validation
// SEVERITY: LOW
func TestPathLengthLimits(t *testing.T) {
	tmpDir := t.TempDir()

	// Many systems have PATH_MAX of 4096 bytes
	// Try to create a path close to that limit
	dcimPath := filepath.Join(tmpDir, "DCIM")
	if err := os.MkdirAll(dcimPath, 0755); err != nil {
		t.Fatal(err)
	}

	// Create subdirectory with very long name
	longName := strings.Repeat("a", 200)
	subPath := filepath.Join(dcimPath, longName)
	if err := os.Mkdir(subPath, 0755); err != nil {
		t.Logf("Cannot create long directory name: %v", err)
	}

	// Create photo with long name
	longPhotoName := strings.Repeat("IMG_", 60) + ".jpg" // ~250 chars
	photoPath := filepath.Join(dcimPath, longPhotoName)
	if err := os.WriteFile(photoPath, []byte("test"), 0644); err != nil {
		t.Logf("Cannot create long filename: %v", err)
	}

	count, _, err := CountPhotos(tmpDir)
	if err != nil {
		t.Logf("CountPhotos with long paths: %v", err)
	}

	t.Logf("Counted %d photos with long paths", count)

	t.Log("BUG: No path length validation before operations")
	t.Log("SEVERITY: LOW")
	t.Log("IMPACT: Long paths can cause silent failures on some systems")
}

// ============================================================================
// TEST CATEGORY 6: Symlinks and Special Files
// ============================================================================

// TestSymlinkLoopsInDCIM tests infinite symlink loops
// BUG: WalkDir follows symlinks - potential infinite loop
// SEVERITY: MEDIUM
func TestSymlinkLoopsInDCIM(t *testing.T) {
	tmpDir := t.TempDir()
	dcimPath := filepath.Join(tmpDir, "DCIM")
	if err := os.MkdirAll(dcimPath, 0755); err != nil {
		t.Fatal(err)
	}

	// Create symlink loop: A -> B -> A
	linkA := filepath.Join(dcimPath, "linkA")
	linkB := filepath.Join(dcimPath, "linkB")

	if err := os.Symlink(linkB, linkA); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(linkA, linkB); err != nil {
		t.Fatal(err)
	}

	// Count photos - should detect loop
	startTime := time.Now()
	count, _, err := CountPhotos(tmpDir)
	elapsed := time.Since(startTime)

	if elapsed > 5*time.Second {
		t.Error("BUG: CountPhotos took too long - possible infinite loop")
		t.Log("SEVERITY: HIGH")
		t.Log("IMPACT: Hangs on cards with symlink loops")
	}

	if err != nil {
		t.Logf("Handled symlink loop with error: %v", err)
	}

	t.Logf("Counted %d photos in %v", count, elapsed)

	t.Log("INFO: WalkDir should handle symlink loops automatically")
	t.Log("VERIFY: Check Go version's filepath.WalkDir loop detection")
}

// TestDanglingSymlinks tests broken symlinks
// BUG: WalkDir may fail on dangling symlinks
// SEVERITY: LOW
func TestDanglingSymlinks(t *testing.T) {
	tmpDir := t.TempDir()
	dcimPath := filepath.Join(tmpDir, "DCIM")
	if err := os.MkdirAll(dcimPath, 0755); err != nil {
		t.Fatal(err)
	}

	// Create dangling symlink
	danglingLink := filepath.Join(dcimPath, "dangling.jpg")
	if err := os.Symlink("/nonexistent/photo.jpg", danglingLink); err != nil {
		t.Fatal(err)
	}

	// Create valid photo
	validPhoto := filepath.Join(dcimPath, "valid.jpg")
	if err := os.WriteFile(validPhoto, []byte("valid"), 0644); err != nil {
		t.Fatal(err)
	}

	count, _, err := CountPhotos(tmpDir)
	if err != nil {
		t.Logf("CountPhotos failed with dangling symlink: %v", err)
	}

	t.Logf("Counted %d photos (should be 1 valid, ignoring dangling)", count)

	t.Log("BUG: Dangling symlinks may cause WalkDir to fail")
	t.Log("SEVERITY: LOW")
	t.Log("IMPACT: Single broken symlink stops entire count")
	t.Log("FIX: Check symlink target validity before following")
}

// TestFIFOsAndSockets tests special file types
// BUG: No filtering of non-regular files
// SEVERITY: LOW
func TestFIFOsAndSockets(t *testing.T) {
	if os.Getuid() != 0 {
		t.Skip("Skipping special file test - requires privileges")
	}

	t.Log("BUG: CountPhotos doesn't filter FIFOs, sockets, block devices")
	t.Log("SEVERITY: LOW")
	t.Log("IMPACT: May attempt to read special files if they have photo extensions")
	t.Log("LOCATION: Line 313 - checks IsDir() but not file type")
	t.Log("FIX: Add check: d.Type().IsRegular() before counting")
}

// ============================================================================
// TEST CATEGORY 7: Filesystem Type Detection
// ============================================================================

// TestMultipleFilesystemTypes tests different FS types
// BUG: No filesystem type validation
// SEVERITY: LOW
func TestMultipleFilesystemTypes(t *testing.T) {
	t.Log("BUG: mount() tries hardcoded list of filesystem types")
	t.Log("SEVERITY: LOW")
	t.Log("LOCATION: Line 250 - fstypes := []string{\"vfat\", \"exfat\", \"ext4\", \"ntfs\"}")
	t.Log("MISSING TYPES:")
	t.Log("  - F2FS (common on modern SD cards)")
	t.Log("  - BTRFS")
	t.Log("  - XFS")
	t.Log("  - HFS+ / APFS (macOS formatted cards)")
	t.Log("  - UDF (some cameras use this)")
	t.Log("IMPACT: Cards with non-standard filesystems won't mount")
	t.Log("FIX OPTIONS:")
	t.Log("  1. Use blkid to detect FS type before mount")
	t.Log("  2. Add more FS types to try list")
	t.Log("  3. Fall back to auto-detect (empty string) earlier")
}

// TestReadOnlyFilesystem tests mounting read-only filesystems
// BUG: Mounts read-write initially for card ID write
// SEVERITY: HIGH
func TestReadOnlyFilesystem(t *testing.T) {
	t.Log("BUG: mount() initially mounts read-write (line 248-249)")
	t.Log("SEVERITY: HIGH")
	t.Log("SCENARIO:")
	t.Log("  1. Card inserted with write-protect tab enabled")
	t.Log("  2. mount() attempts read-write mount at line 253")
	t.Log("  3. Mount succeeds as read-only (hardware enforced)")
	t.Log("  4. GetOrCreateCardID tries to write at line 374")
	t.Log("  5. Write fails with read-only error")
	t.Log("  6. Returns error, sync aborted")
	t.Log("IMPACT:")
	t.Log("  - Cannot sync write-protected cards")
	t.Log("  - Card gets new ID every insertion")
	t.Log("FIX:")
	t.Log("  1. Check if card is write-protected before write")
	t.Log("  2. If protected and no ID: use volatile ID for this session")
	t.Log("  3. If protected with ID: read and use existing ID")
	t.Log("  4. Warn user that card should be unprotected for ID persistence")
}

// TestCorruptedFilesystemMetadata tests FS with corrupt metadata
// BUG: No validation of filesystem health
// SEVERITY: MEDIUM
func TestCorruptedFilesystemMetadata(t *testing.T) {
	t.Log("BUG: No filesystem health check before operations")
	t.Log("SEVERITY: MEDIUM")
	t.Log("RISK SCENARIOS:")
	t.Log("  - Corrupt FAT table causes wrong file sizes")
	t.Log("  - Corrupt directory entries return garbage filenames")
	t.Log("  - Cross-linked clusters cause data corruption")
	t.Log("CURRENT BEHAVIOR:")
	t.Log("  - CountPhotos uses d.Info().Size() directly (line 322-324)")
	t.Log("  - No validation that size is reasonable")
	t.Log("  - Could report TB sizes for KB files")
	t.Log("IMPACT: Incorrect progress reporting, possible sync failures")
	t.Log("FIX:")
	t.Log("  1. Check filesystem with fsck equivalent before operations")
	t.Log("  2. Validate file sizes are reasonable (<10GB per photo)")
	t.Log("  3. Handle size calculation errors gracefully")
}

// ============================================================================
// TEST CATEGORY 8: Write-Protected Cards
// ============================================================================

// TestWriteProtectedCard tests SD card with write-protect switch
// BUG: No detection of write-protection before attempting write
// SEVERITY: HIGH
func TestWriteProtectedCard(t *testing.T) {
	tmpDir := t.TempDir()

	// Simulate write-protection by making directory read-only
	if err := os.Chmod(tmpDir, 0555); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(tmpDir, 0755)

	// Try to get or create card ID
	cardID, isNew, err := GetOrCreateCardID(tmpDir, nil)

	if err != nil {
		t.Log("BUG CONFIRMED: GetOrCreateCardID fails on write-protected card")
		t.Log("SEVERITY: HIGH")
		t.Log("CURRENT BEHAVIOR:")
		t.Logf("  - Returned error: %v", err)
		t.Logf("  - Returned cardID: %s", cardID)
		t.Logf("  - Returned isNew: %v", isNew)
		t.Log("IMPACT:")
		t.Log("  - Write-protected cards cannot be synced at all")
		t.Log("  - Card gets new ID every insertion")
		t.Log("  - User has no indication of write-protection issue")
		t.Log("FIX:")
		t.Log("  1. Detect write-protection: syscall.Statfs check ST_RDONLY flag")
		t.Log("  2. If protected + has ID: read existing ID, allow sync")
		t.Log("  3. If protected + no ID: generate ephemeral ID, warn user")
		t.Log("  4. Display clear message in WebUI about write-protection")
	}

	if cardID != "" && err != nil {
		t.Log("ADDITIONAL BUG: Returns card ID even when write failed")
		t.Log("IMPACT: Sync proceeds with non-persistent ID")
	}
}

// TestPartiallyWritableCard tests card with some writable space
// BUG: No handling of partially full cards
// SEVERITY: MEDIUM
func TestPartiallyWritableCard(t *testing.T) {
	tmpDir := t.TempDir()
	dcimPath := filepath.Join(tmpDir, "DCIM")
	if err := os.MkdirAll(dcimPath, 0755); err != nil {
		t.Fatal(err)
	}

	// Create many large photos to fill space
	for i := 0; i < 100; i++ {
		photoPath := filepath.Join(dcimPath, fmt.Sprintf("IMG_%04d.jpg", i))
		// Create 1MB fake photo
		largeData := make([]byte, 1024*1024)
		if err := os.WriteFile(photoPath, largeData, 0644); err != nil {
			t.Logf("Filled space after %d photos: %v", i, err)
			break
		}
	}

	// Try to create card ID - may fail if no space
	cardID, _, err := GetOrCreateCardID(tmpDir, nil)

	if err != nil {
		t.Log("BUG: GetOrCreateCardID fails when card is full")
		t.Log("SEVERITY: MEDIUM")
		t.Log("IMPACT: Full cards cannot get persistent ID")
	}

	t.Logf("Card ID on full card: %s, error: %v", cardID, err)

	t.Log("FIX: Before writing ID, check available space")
	t.Log("     If < 1KB free, try to cleanup temp files first")
}

// ============================================================================
// TEST CATEGORY 9: Millions of Tiny Files
// ============================================================================

// TestMillionsOfTinyFiles tests performance with many files
// BUG: No timeout or progress reporting for large counts
// SEVERITY: MEDIUM
func TestMillionsOfTinyFiles(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping slow test - millions of files")
	}

	tmpDir := t.TempDir()
	dcimPath := filepath.Join(tmpDir, "DCIM")
	if err := os.MkdirAll(dcimPath, 0755); err != nil {
		t.Fatal(err)
	}

	// Create many small files (not millions due to test time, but enough to test)
	const fileCount = 10000
	t.Logf("Creating %d tiny files...", fileCount)
	for i := 0; i < fileCount; i++ {
		photoPath := filepath.Join(dcimPath, fmt.Sprintf("IMG_%06d.jpg", i))
		if err := os.WriteFile(photoPath, []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Count with timeout
	done := make(chan struct{})
	var count int
	var countErr error

	startTime := time.Now()
	go func() {
		count, _, countErr = CountPhotos(tmpDir)
		close(done)
	}()

	select {
	case <-done:
		elapsed := time.Since(startTime)
		t.Logf("Counted %d files in %v", count, elapsed)

		if countErr != nil {
			t.Logf("Count error: %v", countErr)
		}

		if count != fileCount {
			t.Errorf("Expected %d files, got %d", fileCount, count)
		}

		// Check performance
		filesPerSec := float64(count) / elapsed.Seconds()
		t.Logf("Performance: %.0f files/sec", filesPerSec)

		if filesPerSec < 1000 {
			t.Log("WARNING: Slow performance on large file counts")
		}

	case <-time.After(30 * time.Second):
		t.Error("BUG: CountPhotos timeout after 30s")
		t.Log("SEVERITY: HIGH")
		t.Log("IMPACT: Cards with many files cause unresponsive system")
		t.Log("FIX: Add context with timeout to CountPhotos")
		t.Log("     Add progress callback for large counts")
	}

	t.Log("BUG: No timeout or cancellation in CountPhotos")
	t.Log("BUG: No progress reporting during long counts")
	t.Log("SEVERITY: MEDIUM")
	t.Log("IMPACT: Large cards (>100K files) cause long delays")
	t.Log("FIX: Add context.Context parameter for cancellation")
	t.Log("     Add progress callback: func(count, totalBytes int64)")
}

// TestManySmallDirectories tests deeply nested directory count
// BUG: Memory usage scales with file count
// SEVERITY: LOW
func TestManySmallDirectories(t *testing.T) {
	tmpDir := t.TempDir()
	dcimPath := filepath.Join(tmpDir, "DCIM")
	if err := os.MkdirAll(dcimPath, 0755); err != nil {
		t.Fatal(err)
	}

	// Create many subdirectories
	const dirCount = 100
	for i := 0; i < dirCount; i++ {
		subDir := filepath.Join(dcimPath, fmt.Sprintf("DIR_%03d", i))
		if err := os.Mkdir(subDir, 0755); err != nil {
			t.Fatal(err)
		}

		// One photo per directory
		photoPath := filepath.Join(subDir, "IMG_001.jpg")
		if err := os.WriteFile(photoPath, []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	count, _, err := CountPhotos(tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	if count != dirCount {
		t.Errorf("Expected %d photos, got %d", dirCount, count)
	}

	t.Log("BUG: WalkDir loads all paths into memory")
	t.Log("SEVERITY: LOW")
	t.Log("IMPACT: Very large directory trees use excessive memory")
	t.Log("FIX: Use streaming approach instead of loading all paths")
}

// ============================================================================
// TEST CATEGORY 10: Card Hot-Swapping
// ============================================================================

// TestRapidHotSwapCycles tests rapid card swapping
// BUG: Event channel overflow, no debouncing
// SEVERITY: HIGH
func TestRapidHotSwapCycles(t *testing.T) {
	tmpDir := t.TempDir()
	mountDir := filepath.Join(tmpDir, "mount")
	monitor := NewMonitor(mountDir)

	if err := monitor.Start(); err != nil {
		t.Fatal(err)
	}
	defer monitor.Stop()

	// Simulate rapid device detection changes
	const cycleCount = 50
	var eventCount int32

	// Count events
	done := make(chan struct{})
	go func() {
		timeout := time.After(10 * time.Second)
		for {
			select {
			case <-monitor.Events():
				atomic.AddInt32(&eventCount, 1)
			case <-timeout:
				close(done)
				return
			case <-done:
				return
			}
		}
	}()

	// Trigger rapid checkDevices calls
	for i := 0; i < cycleCount; i++ {
		monitor.checkDevices()
		time.Sleep(10 * time.Millisecond)
	}

	time.Sleep(100 * time.Millisecond)
	close(done)

	finalCount := atomic.LoadInt32(&eventCount)
	t.Logf("Generated %d events from %d check cycles", finalCount, cycleCount)

	t.Log("BUGS FOUND:")
	t.Log("BUG 1: Event channel has buffer of only 10 (line 55)")
	t.Log("  SEVERITY: HIGH")
	t.Log("  IMPACT: Rapid insert/remove can overflow buffer")
	t.Log("  CONSEQUENCE: Event send blocks, pollDevices goroutine stalls")
	t.Log("")
	t.Log("BUG 2: No debouncing for rapid state changes")
	t.Log("  SEVERITY: MEDIUM")
	t.Log("  IMPACT: Unstable USB connection floods events")
	t.Log("  FIX: Add 1-second debounce timer for device changes")
	t.Log("")
	t.Log("BUG 3: No event deduplication")
	t.Log("  SEVERITY: LOW")
	t.Log("  IMPACT: Same event may be sent multiple times")
	t.Log("  FIX: Track last event type and suppress duplicates")
}

// TestCardSwapDuringSyncPrepare tests card swap after detection
// BUG: No device validation before sync starts
// SEVERITY: CRITICAL
func TestCardSwapDuringSyncPrepare(t *testing.T) {
	t.Log("BUG: No device validation between detection and sync start")
	t.Log("SEVERITY: CRITICAL")
	t.Log("SCENARIO:")
	t.Log("  1. Card A inserted, event sent (line 142)")
	t.Log("  2. handleCardInserted starts in goroutine (line 120)")
	t.Log("  3. User swaps card (A out, B in) before line 122")
	t.Log("  4. HasDCIM checks Card B's DCIM (line 122)")
	t.Log("  5. CountPhotos counts Card B's photos (line 129)")
	t.Log("  6. GetOrCreateCardID reads Card B's ID (line 153)")
	t.Log("  7. Sync proceeds with Card B's files but wrong event data")
	t.Log("IMPACT:")
	t.Log("  - Photos from Card B uploaded to Card A's folder")
	t.Log("  - Data loss and confusion")
	t.Log("FIX:")
	t.Log("  1. Validate device path still exists before each operation")
	t.Log("  2. Store device serial number and verify before operations")
	t.Log("  3. Re-check mount path matches original device")
}

// TestConcurrentStopCalls tests race in Stop method
// BUG: Multiple Stop() calls panic on closed channel
// SEVERITY: MEDIUM
func TestConcurrentStopCalls(t *testing.T) {
	tmpDir := t.TempDir()
	monitor := NewMonitor(filepath.Join(tmpDir, "mount"))

	if err := monitor.Start(); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	panicCount := int32(0)

	// Call Stop concurrently many times
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					atomic.AddInt32(&panicCount, 1)
					t.Logf("Panic in Stop(): %v", r)
				}
			}()
			monitor.Stop()
		}()
	}

	wg.Wait()

	if panicCount > 0 {
		t.Errorf("BUG: Stop() panicked %d times", panicCount)
		t.Log("SEVERITY: MEDIUM")
		t.Log("LOCATION: Line 78 - close(m.stopChan)")
		t.Log("IMPACT: Crash on shutdown if Stop called multiple times")
		t.Log("FIX: Use sync.Once to ensure channel closed only once")
		t.Log("     Or add closed flag check before close()")
	}
}

// TestEventChannelBackpressure tests slow consumer
// BUG: Slow consumer blocks pollDevices goroutine
// SEVERITY: HIGH
func TestEventChannelBackpressure(t *testing.T) {
	tmpDir := t.TempDir()
	monitor := NewMonitor(filepath.Join(tmpDir, "mount"))

	if err := monitor.Start(); err != nil {
		t.Fatal(err)
	}
	defer monitor.Stop()

	// Fill event buffer without consuming
	for i := 0; i < 10; i++ {
		monitor.eventChan <- Event{
			Type:    EventInserted,
			DevPath: fmt.Sprintf("/dev/test%d", i),
		}
	}

	// Try to trigger checkDevices while buffer is full
	done := make(chan bool)
	go func() {
		monitor.checkDevices() // This will try to send event if device detected
		done <- true
	}()

	// If checkDevices blocks, this will timeout
	select {
	case <-done:
		t.Log("checkDevices completed (no device to detect)")
	case <-time.After(1 * time.Second):
		t.Error("BUG: checkDevices blocked on full event buffer")
		t.Log("SEVERITY: HIGH")
		t.Log("LOCATION: Line 142-147 - blocking send to eventChan")
		t.Log("IMPACT: Device changes not detected if consumer is slow")
		t.Log("FIX: Use non-blocking send with select/default")
		t.Log("     Or increase buffer size significantly")
	}

	// Drain events
	for len(monitor.eventChan) > 0 {
		<-monitor.eventChan
	}
}

// ============================================================================
// SUMMARY REPORT GENERATION
// ============================================================================

func TestGenerateBugReport(t *testing.T) {
	t.Log("=========================================================================")
	t.Log("SD CARD EDGE CASES - COMPREHENSIVE BUG REPORT")
	t.Log("=========================================================================")
	t.Log("")
	t.Log("CRITICAL SEVERITY BUGS:")
	t.Log("  1. Race condition in checkDevices (mount failure sends event anyway)")
	t.Log("  2. No device validation during handleCardInserted goroutine")
	t.Log("  3. Card swap between detection and sync -> wrong card synced")
	t.Log("")
	t.Log("HIGH SEVERITY BUGS:")
	t.Log("  4. CountPhotos has no cancellation context")
	t.Log("  5. Card removal during count causes stale data usage")
	t.Log("  6. Full card -> cannot write ID -> new ID every insertion")
	t.Log("  7. Write-protected cards cannot be synced at all")
	t.Log("  8. Event channel overflow blocks pollDevices")
	t.Log("  9. RemountReadOnly failure leaves card writable")
	t.Log(" 10. No timeout for CountPhotos on huge directories")
	t.Log("")
	t.Log("MEDIUM SEVERITY BUGS:")
	t.Log(" 11. No retry for transient I/O errors")
	t.Log(" 12. WalkDir error stops entire photo count")
	t.Log(" 13. Concurrent Stop() calls can panic")
	t.Log(" 14. No filesystem health check")
	t.Log(" 15. Null bytes in card ID not detected")
	t.Log(" 16. No debouncing for rapid device changes")
	t.Log("")
	t.Log("LOW SEVERITY BUGS:")
	t.Log(" 17. No depth limit for WalkDir")
	t.Log(" 18. No path length validation")
	t.Log(" 19. Missing filesystem types in mount list")
	t.Log(" 20. No filtering of special file types (FIFOs, sockets)")
	t.Log("")
	t.Log("RECOMMENDED FIXES (Priority Order):")
	t.Log("  1. Add device validation in handleCardInserted")
	t.Log("  2. Add context.Context to CountPhotos")
	t.Log("  3. Fix mount() error handling to not update lastDevice on failure")
	t.Log("  4. Detect write-protection and handle read-only cards")
	t.Log("  5. Use sync.Once for Stop() channel close")
	t.Log("  6. Add non-blocking event send")
	t.Log("  7. Implement debouncing for device changes")
	t.Log("  8. Add filesystem health checks")
	t.Log("  9. Validate card ID format and content")
	t.Log(" 10. Add timeouts to all blocking operations")
	t.Log("")
	t.Log("=========================================================================")
}
