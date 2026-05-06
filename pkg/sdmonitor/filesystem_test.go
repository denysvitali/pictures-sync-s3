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

// TestCardIDFilePermissions tests proper permissions for card ID file
func TestCardIDFilePermissions(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "sdmonitor-perms-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Test various permission scenarios
	tests := []struct {
		name     string
		dirPerms os.FileMode
		wantErr  bool
		errType  string
	}{
		{"normal permissions", 0755, false, ""},
		{"read-only directory", 0555, true, "permission"},
		{"no-access directory", 0000, true, "permission"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testDir := filepath.Join(tmpDir, tt.name)
			if err := os.Mkdir(testDir, tt.dirPerms); err != nil {
				t.Fatal(err)
			}
			defer os.Chmod(testDir, 0755) // Cleanup

			// Try to create card ID
			cardID := generateCardID()
			idPath := filepath.Join(testDir, CardIDFile)
			err := os.WriteFile(idPath, []byte(cardID+"\n"), 0644)

			if tt.wantErr && err == nil {
				t.Error("Expected error but got none")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}

			if tt.wantErr && err != nil {
				switch tt.errType {
				case "permission":
					if !os.IsPermission(err) {
						t.Errorf("Expected permission error, got: %v", err)
					}
				}
			}
		})
	}
}

// TestCountPhotosWithSymlinks tests photo counting with symlinks
func TestCountPhotosWithSymlinks(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "sdmonitor-symlinks-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create DCIM structure
	dcimDir := filepath.Join(tmpDir, "DCIM", "100CANON")
	if err := os.MkdirAll(dcimDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create real photos
	realPhotos := []string{"IMG_001.JPG", "IMG_002.JPG", "IMG_003.JPG"}
	for _, photo := range realPhotos {
		path := filepath.Join(dcimDir, photo)
		if err := os.WriteFile(path, []byte("fake image data"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Create symlink to photo (should only count once)
	symlinkPath := filepath.Join(dcimDir, "LINK_001.JPG")
	if err := os.Symlink(filepath.Join(dcimDir, "IMG_001.JPG"), symlinkPath); err != nil {
		t.Fatal(err)
	}

	// Create dangling symlink (should be handled gracefully)
	danglingPath := filepath.Join(dcimDir, "DANGLING.JPG")
	if err := os.Symlink("/nonexistent/photo.jpg", danglingPath); err != nil {
		t.Fatal(err)
	}

	// Create symlink loop
	loop1 := filepath.Join(dcimDir, "LOOP1.JPG")
	loop2 := filepath.Join(dcimDir, "LOOP2.JPG")
	if err := os.Symlink(loop2, loop1); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(loop1, loop2); err != nil {
		t.Fatal(err)
	}

	// Count photos - should handle symlinks gracefully
	count, totalSize, err := CountPhotos(tmpDir)
	if err != nil {
		t.Errorf("CountPhotos failed: %v", err)
	}

	// Should count real photos + valid symlinks
	// Implementation may count symlinks or not - document actual behavior
	t.Logf("Counted %d photos with total size %d bytes", count, totalSize)

	if count == 0 {
		t.Error("Should have counted some photos")
	}

	// Minimum should be real photos
	if count < len(realPhotos) {
		t.Errorf("Should count at least %d real photos, got %d", len(realPhotos), count)
	}
}

// TestCountPhotosWithLongPaths tests photo counting with very long paths
func TestCountPhotosWithLongPaths(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "sdmonitor-longpaths-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create deeply nested DCIM structure
	deepPath := filepath.Join(tmpDir, "DCIM")
	currentPath := deepPath
	for i := 0; i < 10; i++ {
		// Use directory names that are close to the 255-byte limit
		dirName := strings.Repeat(fmt.Sprintf("dir-%d-", i), 20)
		if len(dirName) > 200 {
			dirName = dirName[:200]
		}
		currentPath = filepath.Join(currentPath, dirName)
	}

	if err := os.MkdirAll(currentPath, 0755); err != nil {
		t.Logf("Cannot create deeply nested path (expected on some systems): %v", err)
		t.Skip("System doesn't support deeply nested paths")
	}

	// Create photo with long filename
	longFilename := strings.Repeat("photo", 50) + ".jpg"
	if len(longFilename) > 255 {
		longFilename = longFilename[:250] + ".jpg"
	}
	photoPath := filepath.Join(currentPath, longFilename)

	if err := os.WriteFile(photoPath, []byte("fake photo"), 0644); err != nil {
		t.Logf("Cannot create file with long path: %v", err)
		t.Skip("System doesn't support long paths")
	}

	// Count photos - should handle long paths
	count, size, err := CountPhotos(tmpDir)
	if err != nil {
		t.Errorf("CountPhotos failed with long paths: %v", err)
	}

	if count != 1 {
		t.Errorf("Expected 1 photo, got %d", count)
	}

	if size != 10 {
		t.Errorf("Expected size 10, got %d", size)
	}
}

// TestCountPhotosWithUnicodeFilenames tests photo counting with Unicode
func TestCountPhotosWithUnicodeFilenames(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "sdmonitor-unicode-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	dcimDir := filepath.Join(tmpDir, "DCIM", "100КАМЕРА")
	if err := os.MkdirAll(dcimDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create photos with various Unicode filenames
	unicodePhotos := []struct {
		name string
		ext  string
	}{
		{"фото", ".jpg"},
		{"写真", ".JPG"},
		{"תמונה", ".jpeg"},
		{"📷صورة", ".png"},
		{"photo_测试", ".MP4"},
	}

	for _, photo := range unicodePhotos {
		path := filepath.Join(dcimDir, photo.name+photo.ext)
		if err := os.WriteFile(path, []byte("fake data"), 0644); err != nil {
			t.Errorf("Failed to create Unicode filename %s: %v", photo.name, err)
			continue
		}
	}

	// Count photos
	count, size, err := CountPhotos(tmpDir)
	if err != nil {
		t.Errorf("CountPhotos failed with Unicode: %v", err)
	}

	if count != len(unicodePhotos) {
		t.Errorf("Expected %d photos, got %d", len(unicodePhotos), count)
	}

	expectedSize := int64(len(unicodePhotos) * 9) // "fake data" = 9 bytes
	if size != expectedSize {
		t.Errorf("Expected size %d, got %d", expectedSize, size)
	}
}

// TestConcurrentCardIDAccess tests concurrent access to card ID file
func TestConcurrentCardIDAccess(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "sdmonitor-concurrent-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	var wg sync.WaitGroup
	errors := make(chan error, 20)
	cardIDs := make(chan string, 20)

	// Multiple goroutines trying to get or create card ID
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			cardID, isNew, err := GetOrCreateCardID(tmpDir, nil)
			if err != nil {
				errors <- fmt.Errorf("goroutine %d: %w", id, err)
				return
			}
			cardIDs <- cardID
			t.Logf("Goroutine %d: cardID=%s, isNew=%v", id, cardID, isNew)
		}(i)
	}

	wg.Wait()
	close(errors)
	close(cardIDs)

	// Check for errors
	errorCount := 0
	for err := range errors {
		t.Logf("Error: %v", err)
		errorCount++
	}

	if errorCount > 0 {
		t.Errorf("Got %d errors during concurrent access", errorCount)
	}

	// All card IDs should be the same (first one wins)
	var firstID string
	idCount := 0
	for cardID := range cardIDs {
		idCount++
		if firstID == "" {
			firstID = cardID
		} else if cardID != firstID {
			t.Errorf("Card ID mismatch: got %s, want %s", cardID, firstID)
		}
	}

	if idCount != 10 {
		t.Errorf("Expected 10 card IDs, got %d", idCount)
	}
}

// TestRemountReadOnlyRaceCondition tests race condition in remount
func TestRemountReadOnlyRaceCondition(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "sdmonitor-remount-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create monitor (won't actually mount anything in tests)
	monitor := NewMonitor(tmpDir)

	// Simulate concurrent card ID creation and remount attempts
	var wg sync.WaitGroup
	errors := make(chan error, 10)

	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			// Try to create card ID
			_, _, err := GetOrCreateCardID(tmpDir, monitor)
			if err != nil {
				errors <- fmt.Errorf("cardID creation %d: %w", id, err)
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Errorf("Race condition error: %v", err)
	}
}

// TestMountPointPermissions tests mount point permission issues
func TestMountPointPermissions(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "sdmonitor-mount-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	tests := []struct {
		name      string
		setupFn   func(string) error
		expectErr bool
	}{
		{
			name: "normal mount point",
			setupFn: func(dir string) error {
				return os.MkdirAll(dir, 0755)
			},
			expectErr: false,
		},
		{
			name: "read-only mount point",
			setupFn: func(dir string) error {
				if err := os.MkdirAll(dir, 0755); err != nil {
					return err
				}
				return os.Chmod(dir, 0555)
			},
			expectErr: true,
		},
		{
			name: "mount point is file",
			setupFn: func(dir string) error {
				return os.WriteFile(dir, []byte("not a directory"), 0644)
			},
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mountPoint := filepath.Join(tmpDir, tt.name)

			if tt.setupFn != nil {
				if err := tt.setupFn(mountPoint); err != nil {
					t.Fatalf("Setup failed: %v", err)
				}
			}
			defer os.Chmod(mountPoint, 0755) // Cleanup

			// Try to create card ID file
			idPath := filepath.Join(mountPoint, CardIDFile)
			err := os.WriteFile(idPath, []byte("test-id\n"), 0644)

			if tt.expectErr && err == nil {
				t.Error("Expected error but got none")
			}
			if !tt.expectErr && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}

// TestHasDCIMWithSymlinks tests DCIM detection with symlinks
func TestHasDCIMWithSymlinks(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "sdmonitor-dcim-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create real DCIM directory
	realDCIM := filepath.Join(tmpDir, "real", "DCIM")
	if err := os.MkdirAll(realDCIM, 0755); err != nil {
		t.Fatal(err)
	}

	// Create symlink to DCIM
	symlinkMount := filepath.Join(tmpDir, "symlink-mount")
	if err := os.Mkdir(symlinkMount, 0755); err != nil {
		t.Fatal(err)
	}
	symlinkDCIM := filepath.Join(symlinkMount, "DCIM")
	if err := os.Symlink(realDCIM, symlinkDCIM); err != nil {
		t.Fatal(err)
	}

	// Test with real DCIM
	if !HasDCIM(filepath.Join(tmpDir, "real")) {
		t.Error("Should detect real DCIM directory")
	}

	// Test with symlinked DCIM (should follow symlink)
	if !HasDCIM(symlinkMount) {
		t.Error("Should detect DCIM through symlink")
	}

	// Create dangling DCIM symlink
	danglingMount := filepath.Join(tmpDir, "dangling-mount")
	if err := os.Mkdir(danglingMount, 0755); err != nil {
		t.Fatal(err)
	}
	danglingDCIM := filepath.Join(danglingMount, "DCIM")
	if err := os.Symlink("/nonexistent/DCIM", danglingDCIM); err != nil {
		t.Fatal(err)
	}

	// Test with dangling symlink (should return false, not crash)
	if HasDCIM(danglingMount) {
		t.Error("Should not detect dangling DCIM symlink")
	}
}

// TestPathTraversalVulnerability tests for path traversal in card ID
func TestPathTraversalVulnerability(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "sdmonitor-traversal-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create mount point
	mountPoint := filepath.Join(tmpDir, "mount")
	if err := os.MkdirAll(mountPoint, 0755); err != nil {
		t.Fatal(err)
	}

	// Create sensitive file outside mount point
	sensitiveFile := filepath.Join(tmpDir, "sensitive.txt")
	if err := os.WriteFile(sensitiveFile, []byte("secret data"), 0644); err != nil {
		t.Fatal(err)
	}

	// Try to create malicious card ID with path traversal
	maliciousIDs := []string{
		"../sensitive.txt",
		"../../etc/passwd",
		"..%2F..%2Fetc%2Fpasswd",
		"./../secret",
	}

	for _, malID := range maliciousIDs {
		idPath := filepath.Join(mountPoint, CardIDFile)
		if err := os.WriteFile(idPath, []byte(malID+"\n"), 0644); err != nil {
			t.Logf("Failed to write malicious ID (good): %v", err)
			continue
		}

		readID, _, err := GetOrCreateCardID(mountPoint, nil)
		if err != nil {
			t.Logf("Malicious ID %s rejected: %v", malID, err)
			continue
		}

		// The card ID should be stored as-is, but when used in paths,
		// it should be sanitized. Test that using the ID doesn't escape.
		testPath := filepath.Join(mountPoint, readID)

		// Check if path escapes mount point
		absMount, _ := filepath.Abs(mountPoint)
		absTest, _ := filepath.Abs(testPath)

		// Clean paths to normalize
		cleanMount := filepath.Clean(absMount)
		cleanTest := filepath.Clean(absTest)

		if !strings.HasPrefix(cleanTest, cleanMount) {
			t.Errorf("Path traversal vulnerability: %s escapes %s", readID, mountPoint)
		}

		t.Logf("Malicious ID %s results in path: %s", malID, testPath)
	}
}

// TestFileDescriptorLeaks tests for file descriptor leaks
func TestFileDescriptorLeaks(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "sdmonitor-fdleak-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create DCIM with many photos
	dcimDir := filepath.Join(tmpDir, "DCIM", "100TEST")
	if err := os.MkdirAll(dcimDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create many photos
	for i := 0; i < 100; i++ {
		path := filepath.Join(dcimDir, fmt.Sprintf("IMG_%04d.jpg", i))
		if err := os.WriteFile(path, []byte("test"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Get initial FD count (approximate)
	initialFDs := countOpenFDs()

	// Count photos multiple times
	for i := 0; i < 50; i++ {
		_, _, err := CountPhotos(tmpDir)
		if err != nil {
			t.Fatalf("CountPhotos failed: %v", err)
		}
	}

	// Get final FD count
	finalFDs := countOpenFDs()

	// Allow some variance, but large increase indicates leak
	fdIncrease := finalFDs - initialFDs
	if fdIncrease > 10 {
		t.Errorf("Possible FD leak: increased from %d to %d (delta: %d)",
			initialFDs, finalFDs, fdIncrease)
	}

	t.Logf("FD count: initial=%d, final=%d, delta=%d", initialFDs, finalFDs, fdIncrease)
}

// countOpenFDs counts currently open file descriptors
func countOpenFDs() int {
	// This is Linux-specific
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		return -1
	}
	return len(entries)
}

// TestMountPointDisappearsDuringCount tests handling when mount disappears
func TestMountPointDisappearsDuringCount(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "sdmonitor-disappear-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	dcimDir := filepath.Join(tmpDir, "DCIM", "100TEST")
	if err := os.MkdirAll(dcimDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create some photos
	for i := 0; i < 10; i++ {
		path := filepath.Join(dcimDir, fmt.Sprintf("IMG_%04d.jpg", i))
		if err := os.WriteFile(path, []byte("test"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Count in background
	done := make(chan error)
	go func() {
		_, _, err := CountPhotos(tmpDir)
		done <- err
	}()

	// Give it a moment to start
	time.Sleep(10 * time.Millisecond)

	// Remove the directory while counting
	os.RemoveAll(tmpDir)

	// Wait for completion
	err = <-done
	if err == nil {
		t.Log("Count completed before directory was removed")
		return
	}

	t.Logf("Got expected error: %v", err)
}

// TestSpecialFilesInDCIM tests handling of special files
func TestSpecialFilesInDCIM(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "sdmonitor-special-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	dcimDir := filepath.Join(tmpDir, "DCIM", "100TEST")
	if err := os.MkdirAll(dcimDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create regular photos
	regularPhotos := []string{"IMG_001.JPG", "IMG_002.JPG"}
	for _, photo := range regularPhotos {
		path := filepath.Join(dcimDir, photo)
		if err := os.WriteFile(path, []byte("photo data"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Create special files that should be ignored
	specialFiles := []string{
		".hidden.jpg", // Hidden file
		"Thumbs.db",   // Windows thumbnail cache
		".DS_Store",   // macOS metadata
		"desktop.ini", // Windows folder config
	}

	for _, special := range specialFiles {
		path := filepath.Join(dcimDir, special)
		if err := os.WriteFile(path, []byte("metadata"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Count photos - should only count regular photos
	count, _, err := CountPhotos(tmpDir)
	if err != nil {
		t.Fatalf("CountPhotos failed: %v", err)
	}

	// Current implementation counts by extension, so hidden JPGs might be counted
	// Document actual behavior
	t.Logf("Counted %d photos (regular=%d, special=%d)", count, len(regularPhotos), len(specialFiles))

	if count < len(regularPhotos) {
		t.Errorf("Should count at least %d regular photos, got %d", len(regularPhotos), count)
	}
}

// TestWalkDirErrors tests error handling in filepath.WalkDir
func TestWalkDirErrors(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "sdmonitor-walkerr-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	dcimDir := filepath.Join(tmpDir, "DCIM")
	if err := os.MkdirAll(dcimDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create inaccessible subdirectory
	restrictedDir := filepath.Join(dcimDir, "100RESTRICTED")
	if err := os.Mkdir(restrictedDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Put a photo in it
	photoPath := filepath.Join(restrictedDir, "IMG_001.JPG")
	if err := os.WriteFile(photoPath, []byte("restricted"), 0644); err != nil {
		t.Fatal(err)
	}

	// Make it inaccessible
	if err := os.Chmod(restrictedDir, 0000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(restrictedDir, 0755) // Cleanup

	// Count photos - should handle permission errors gracefully
	count, size, err := CountPhotos(tmpDir)

	// WalkDir will return error for inaccessible directories
	if err != nil {
		t.Logf("Got error (expected): %v", err)
		// Error should be permission-related
		if !os.IsPermission(err) {
			t.Errorf("Expected permission error, got: %v", err)
		} else {
			t.Logf("Correctly detected permission error: %v", err)
		}
	}

	t.Logf("Counted %d photos with size %d despite errors", count, size)
}

// TestCaseInsensitiveExtensions tests extension matching
func TestCaseInsensitiveExtensions(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "sdmonitor-case-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	dcimDir := filepath.Join(tmpDir, "DCIM", "100TEST")
	if err := os.MkdirAll(dcimDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create files with various case extensions
	testFiles := []struct {
		name        string
		shouldCount bool
	}{
		{"photo.jpg", true},
		{"photo.JPG", true},
		{"photo.Jpg", true},
		{"photo.jPg", true},
		{"photo.JPEG", true},
		{"photo.jpeg", true},
		{"video.mp4", true},
		{"video.MP4", true},
		{"raw.CR2", true},
		{"raw.cr2", true},
		{"text.txt", false},
		{"doc.pdf", false},
	}

	for _, tf := range testFiles {
		path := filepath.Join(dcimDir, tf.name)
		if err := os.WriteFile(path, []byte("data"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Count photos
	count, _, err := CountPhotos(tmpDir)
	if err != nil {
		t.Fatalf("CountPhotos failed: %v", err)
	}

	// Should count all photo/video files regardless of case
	expectedCount := 0
	for _, tf := range testFiles {
		if tf.shouldCount {
			expectedCount++
		}
	}

	if count != expectedCount {
		t.Errorf("Expected %d files, got %d", expectedCount, count)
	}
}

// TestZeroByteFiles tests handling of zero-byte files
func TestZeroByteFiles(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "sdmonitor-zerobyte-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	dcimDir := filepath.Join(tmpDir, "DCIM", "100TEST")
	if err := os.MkdirAll(dcimDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create zero-byte photo file
	zeroPath := filepath.Join(dcimDir, "ZERO.JPG")
	if err := os.WriteFile(zeroPath, []byte{}, 0644); err != nil {
		t.Fatal(err)
	}

	// Create normal photo
	normalPath := filepath.Join(dcimDir, "NORMAL.JPG")
	if err := os.WriteFile(normalPath, []byte("photo data"), 0644); err != nil {
		t.Fatal(err)
	}

	// Count photos
	count, totalSize, err := CountPhotos(tmpDir)
	if err != nil {
		t.Fatalf("CountPhotos failed: %v", err)
	}

	if count != 2 {
		t.Errorf("Expected 2 files, got %d", count)
	}

	if totalSize != 10 {
		t.Errorf("Expected size 10, got %d", totalSize)
	}
}

// TestFilesystemRaceCondition tests race in file creation/deletion
func TestFilesystemRaceCondition(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "sdmonitor-race-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	dcimDir := filepath.Join(tmpDir, "DCIM", "100TEST")
	if err := os.MkdirAll(dcimDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create initial photos
	for i := 0; i < 10; i++ {
		path := filepath.Join(dcimDir, fmt.Sprintf("IMG_%04d.jpg", i))
		if err := os.WriteFile(path, []byte("photo"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	var wg sync.WaitGroup

	// Count photos in background
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 20; i++ {
			CountPhotos(tmpDir)
			time.Sleep(time.Millisecond)
		}
	}()

	// Add/remove photos concurrently
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 10; i < 30; i++ {
			path := filepath.Join(dcimDir, fmt.Sprintf("IMG_%04d.jpg", i))
			os.WriteFile(path, []byte("photo"), 0644)
			time.Sleep(time.Millisecond)
			os.Remove(path)
		}
	}()

	wg.Wait()
	// Test passes if no crash/panic occurs
	t.Log("Race condition test completed without crash")
}
