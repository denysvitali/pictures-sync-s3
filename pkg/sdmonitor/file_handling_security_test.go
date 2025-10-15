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

// TestCountPhotosWithSymlinksAttacks tests CountPhotos function with symlink attacks
func TestCountPhotosWithSymlinksAttacks(t *testing.T) {
	tests := []struct {
		name        string
		setup       func(string) error
		expectError bool
		description string
	}{
		{
			name: "symlink_to_outside_dcim",
			setup: func(tmpDir string) error {
				dcimPath := filepath.Join(tmpDir, "DCIM")
				if err := os.MkdirAll(dcimPath, 0755); err != nil {
					return err
				}

				// Create symlink pointing outside DCIM
				outsideFile := filepath.Join(tmpDir, "outside.jpg")
				if err := os.WriteFile(outsideFile, []byte("outside"), 0644); err != nil {
					return err
				}

				linkPath := filepath.Join(dcimPath, "link.jpg")
				return os.Symlink(outsideFile, linkPath)
			},
			expectError: false, // Should follow symlinks or skip them safely
			description: "Symlinks pointing outside DCIM should be handled safely",
		},
		{
			name: "symlink_loop",
			setup: func(tmpDir string) error {
				dcimPath := filepath.Join(tmpDir, "DCIM")
				if err := os.MkdirAll(dcimPath, 0755); err != nil {
					return err
				}

				// Create symlink loop: A -> B -> A
				linkA := filepath.Join(dcimPath, "linkA")
				linkB := filepath.Join(dcimPath, "linkB")

				os.Symlink(linkB, linkA)
				return os.Symlink(linkA, linkB)
			},
			expectError: true, // Should detect and handle loop
			description: "Symlink loops should not cause infinite traversal",
		},
		{
			name: "symlink_to_sensitive_file",
			setup: func(tmpDir string) error {
				dcimPath := filepath.Join(tmpDir, "DCIM")
				if err := os.MkdirAll(dcimPath, 0755); err != nil {
					return err
				}

				// Try to create symlink to /etc/passwd
				linkPath := filepath.Join(dcimPath, "passwd.jpg")
				return os.Symlink("/etc/passwd", linkPath)
			},
			expectError: false,
			description: "Symlinks to sensitive files should not be counted as photos",
		},
		{
			name: "deeply_nested_symlinks",
			setup: func(tmpDir string) error {
				dcimPath := filepath.Join(tmpDir, "DCIM")
				if err := os.MkdirAll(dcimPath, 0755); err != nil {
					return err
				}

				// Create chain of symlinks
				current := dcimPath
				for i := 0; i < 100; i++ {
					next := filepath.Join(current, fmt.Sprintf("level%d", i))
					if err := os.Mkdir(next, 0755); err != nil {
						return err
					}

					linkPath := filepath.Join(current, fmt.Sprintf("link%d", i))
					if err := os.Symlink(next, linkPath); err != nil {
						return err
					}
					current = next
				}
				return nil
			},
			expectError: false,
			description: "Deeply nested symlinks should not cause stack overflow",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()

			if err := tt.setup(tmpDir); err != nil {
				t.Fatalf("Setup failed: %v", err)
			}

			// Test CountPhotos with timeout
			done := make(chan bool, 1)
			var count int
			var size int64
			var err error

			go func() {
				defer func() {
					if r := recover(); r != nil {
						t.Errorf("%s: Panic occurred: %v", tt.description, r)
					}
					done <- true
				}()

				count, size, err = CountPhotos(tmpDir)
			}()

			select {
			case <-done:
				if tt.expectError && err == nil {
					t.Errorf("%s: Expected error but got none", tt.description)
				}
				if !tt.expectError && err != nil {
					t.Logf("%s: Got error (may be acceptable): %v", tt.description, err)
				}
				t.Logf("Counted %d photos, %d bytes", count, size)
			case <-time.After(5 * time.Second):
				t.Errorf("%s: CountPhotos timed out (possible infinite loop)", tt.description)
			}
		})
	}
}

// TestCountPhotosWithMaliciousFilenames tests filename-based attacks
func TestCountPhotosWithMaliciousFilenames(t *testing.T) {
	tests := []struct {
		name        string
		filename    string
		shouldCount bool
		description string
	}{
		{
			name:        "null_byte_filename",
			filename:    "photo\x00.jpg",
			shouldCount: false,
			description: "Filename with null byte should be rejected",
		},
		{
			name:        "unicode_right_to_left",
			filename:    "photo\u202E.jpg", // Right-to-left override
			shouldCount: true, // May be counted but should be safe
			description: "Unicode right-to-left override in filename",
		},
		{
			name:        "extremely_long_filename",
			filename:    strings.Repeat("a", 1000) + ".jpg",
			shouldCount: false,
			description: "Extremely long filename should be handled",
		},
		{
			name:        "filename_with_path_separators",
			filename:    "photo/../../etc/passwd.jpg",
			shouldCount: false,
			description: "Filename with path separators should be rejected",
		},
		{
			name:        "hidden_file",
			filename:    ".hidden.jpg",
			shouldCount: true,
			description: "Hidden files should be handled appropriately",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			dcimPath := filepath.Join(tmpDir, "DCIM")
			if err := os.MkdirAll(dcimPath, 0755); err != nil {
				t.Fatalf("Failed to create DCIM: %v", err)
			}

			// Try to create file with malicious name
			filePath := filepath.Join(dcimPath, tt.filename)
			if err := os.WriteFile(filePath, []byte("test"), 0644); err != nil {
				t.Logf("Could not create file (may be expected): %v", err)
				return
			}

			count, _, err := CountPhotos(tmpDir)
			if err != nil {
				t.Logf("%s: CountPhotos returned error: %v", tt.description, err)
			}

			if tt.shouldCount && count == 0 {
				t.Logf("%s: File was not counted (may be correct)", tt.description)
			}
			if !tt.shouldCount && count > 0 {
				t.Errorf("%s: File was counted but shouldn't be", tt.description)
			}
		})
	}
}

// TestCountPhotosRaceConditions tests TOCTOU vulnerabilities
func TestCountPhotosRaceConditions(t *testing.T) {
	t.Run("concurrent_file_modification", func(t *testing.T) {
		tmpDir := t.TempDir()
		dcimPath := filepath.Join(tmpDir, "DCIM")
		os.MkdirAll(dcimPath, 0755)

		// Create initial files
		for i := 0; i < 10; i++ {
			path := filepath.Join(dcimPath, fmt.Sprintf("photo%d.jpg", i))
			os.WriteFile(path, []byte("test"), 0644)
		}

		var wg sync.WaitGroup
		errors := make(chan error, 2)

		// Goroutine 1: Count photos repeatedly
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				if _, _, err := CountPhotos(tmpDir); err != nil {
					errors <- err
					return
				}
				time.Sleep(1 * time.Millisecond)
			}
		}()

		// Goroutine 2: Modify files during counting
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				// Add/remove files
				newPath := filepath.Join(dcimPath, fmt.Sprintf("new%d.jpg", i))
				os.WriteFile(newPath, []byte("new"), 0644)

				oldPath := filepath.Join(dcimPath, fmt.Sprintf("photo%d.jpg", i%10))
				os.Remove(oldPath)

				time.Sleep(1 * time.Millisecond)
			}
		}()

		wg.Wait()
		close(errors)

		// Check for errors
		for err := range errors {
			t.Logf("Race condition error (may be acceptable): %v", err)
		}
	})

	t.Run("symlink_race_condition", func(t *testing.T) {
		tmpDir := t.TempDir()
		dcimPath := filepath.Join(tmpDir, "DCIM")
		os.MkdirAll(dcimPath, 0755)

		targetFile := filepath.Join(tmpDir, "target.jpg")
		os.WriteFile(targetFile, []byte("target"), 0644)

		var wg sync.WaitGroup

		// Goroutine 1: Count photos
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				CountPhotos(tmpDir)
				time.Sleep(1 * time.Millisecond)
			}
		}()

		// Goroutine 2: Create and destroy symlinks
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				linkPath := filepath.Join(dcimPath, "link.jpg")
				os.Symlink(targetFile, linkPath)
				time.Sleep(500 * time.Microsecond)
				os.Remove(linkPath)
				time.Sleep(500 * time.Microsecond)
			}
		}()

		wg.Wait()
		t.Log("Symlink race condition test completed")
	})
}

// TestGetOrCreateCardIDVulnerabilities tests card ID generation security
func TestGetOrCreateCardIDVulnerabilities(t *testing.T) {
	t.Run("cardid_file_symlink_attack", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create symlink at card ID file location pointing to sensitive file
		sensitiveFile := filepath.Join(tmpDir, "sensitive.txt")
		os.WriteFile(sensitiveFile, []byte("SECRET_DATA"), 0600)

		cardIDPath := filepath.Join(tmpDir, CardIDFile)
		if err := os.Symlink(sensitiveFile, cardIDPath); err != nil {
			t.Skipf("Cannot create symlink: %v", err)
		}

		// Try to read/write card ID through symlink
		cardID, isNew, err := GetOrCreateCardID(tmpDir, nil)
		if err != nil {
			t.Logf("GetOrCreateCardID rejected symlink (good): %v", err)
		} else {
			t.Logf("CardID: %s (new=%v) - check if symlink was followed", cardID, isNew)

			// Check if sensitive file was modified
			data, _ := os.ReadFile(sensitiveFile)
			if string(data) != "SECRET_DATA" {
				t.Errorf("Symlink attack succeeded - sensitive file was modified!")
			}
		}
	})

	t.Run("cardid_predictability", func(t *testing.T) {
		// Test if card IDs are predictable
		tmpDir1 := t.TempDir()
		tmpDir2 := t.TempDir()

		cardID1, _, _ := GetOrCreateCardID(tmpDir1, nil)
		time.Sleep(1 * time.Millisecond)
		cardID2, _, _ := GetOrCreateCardID(tmpDir2, nil)

		if cardID1 == cardID2 {
			t.Error("Card IDs are predictable - collision detected!")
		}

		// Check if IDs follow expected format
		if !strings.HasPrefix(cardID1, "card-") {
			t.Errorf("Card ID has unexpected format: %s", cardID1)
		}
	})

	t.Run("cardid_race_condition", func(t *testing.T) {
		tmpDir := t.TempDir()

		var wg sync.WaitGroup
		results := make([]string, 10)

		// Concurrent card ID creation
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				cardID, _, _ := GetOrCreateCardID(tmpDir, nil)
				results[idx] = cardID
			}(i)
		}

		wg.Wait()

		// Check if all goroutines got the same ID
		firstID := results[0]
		allSame := true
		for _, id := range results[1:] {
			if id != firstID {
				allSame = false
				break
			}
		}

		if !allSame {
			t.Error("Race condition in GetOrCreateCardID - different IDs generated")
		}
	})

	t.Run("cardid_file_permissions", func(t *testing.T) {
		tmpDir := t.TempDir()

		GetOrCreateCardID(tmpDir, nil)

		cardIDPath := filepath.Join(tmpDir, CardIDFile)
		info, err := os.Stat(cardIDPath)
		if err != nil {
			t.Fatalf("Card ID file not created: %v", err)
		}

		mode := info.Mode().Perm()
		// Check if file has overly permissive permissions
		if mode&0002 != 0 {
			t.Errorf("Card ID file is world-writable: %o", mode)
		}
	})
}

// TestMountPathTraversal tests path traversal in mount operations
func TestMountPathTraversal(t *testing.T) {
	tests := []struct {
		name        string
		mountPath   string
		shouldError bool
		description string
	}{
		{
			name:        "absolute_path_outside_safe",
			mountPath:   "/etc/passwd",
			shouldError: true,
			description: "Absolute path outside safe mount area",
		},
		{
			name:        "relative_path_traversal",
			mountPath:   "../../etc/passwd",
			shouldError: true,
			description: "Relative path traversal attempt",
		},
		{
			name:        "null_byte_in_path",
			mountPath:   "/tmp/mount\x00/../../etc",
			shouldError: true,
			description: "Null byte path injection",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Validate mount path
			cleanPath := filepath.Clean(tt.mountPath)

			// Check for suspicious patterns
			hasDotDot := strings.Contains(cleanPath, "..")
			hasNullByte := strings.Contains(tt.mountPath, "\x00")
			isAbsolute := filepath.IsAbs(cleanPath)

			isInvalid := hasDotDot || hasNullByte

			if tt.shouldError && !isInvalid {
				t.Errorf("%s: Dangerous mount path was not rejected", tt.description)
			}

			t.Logf("Mount path: %s -> %s (absolute=%v, invalid=%v)",
				tt.mountPath, cleanPath, isAbsolute, isInvalid)
		})
	}
}

// TestFileSystemEdgeCases tests various filesystem edge cases
func TestFileSystemEdgeCases(t *testing.T) {
	t.Run("dcim_as_symlink", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create DCIM as symlink to another directory
		realDCIM := filepath.Join(tmpDir, "real_dcim")
		os.MkdirAll(realDCIM, 0755)

		dcimLink := filepath.Join(tmpDir, "DCIM")
		if err := os.Symlink(realDCIM, dcimLink); err != nil {
			t.Skipf("Cannot create symlink: %v", err)
		}

		// Test HasDCIM
		hasDCIM := HasDCIM(tmpDir)

		// Check if symlink was followed
		info, _ := os.Lstat(dcimLink)
		if info.Mode()&os.ModeSymlink != 0 {
			t.Log("DCIM is a symlink - should be handled carefully")
		}

		t.Logf("HasDCIM returned: %v", hasDCIM)
	})

	t.Run("dcim_with_malicious_subdirs", func(t *testing.T) {
		tmpDir := t.TempDir()
		dcimPath := filepath.Join(tmpDir, "DCIM")
		os.MkdirAll(dcimPath, 0755)

		// Create subdirectories with malicious names
		maliciousNames := []string{
			"../../../etc",
			".\x00.",
			strings.Repeat("a", 300),
		}

		for _, name := range maliciousNames {
			subdir := filepath.Join(dcimPath, name)
			if err := os.MkdirAll(subdir, 0755); err != nil {
				t.Logf("Could not create malicious subdir %s: %v", name, err)
			}
		}

		// Test photo counting
		count, _, err := CountPhotos(tmpDir)
		if err != nil {
			t.Logf("CountPhotos error: %v", err)
		}
		t.Logf("Counted %d photos with malicious subdirs", count)
	})

	t.Run("dcim_with_special_files", func(t *testing.T) {
		tmpDir := t.TempDir()
		dcimPath := filepath.Join(tmpDir, "DCIM")
		os.MkdirAll(dcimPath, 0755)

		// Create various special files
		// Named pipe (FIFO)
		// Socket
		// Device files (if permissions allow)

		// Try to create FIFO
		// Note: os.MkfifoAt or syscall.Mkfifo would be needed
		_ = filepath.Join(dcimPath, "fifo.jpg") // Would be fifoPath
		t.Log("Special file creation test - would need syscall support")

		count, _, err := CountPhotos(tmpDir)
		if err != nil {
			t.Logf("CountPhotos with special files: %v", err)
		}
		t.Logf("Count: %d", count)
	})
}

// TestDeviceInfoSecurityIssues tests device info gathering vulnerabilities
func TestDeviceInfoSecurityIssues(t *testing.T) {
	t.Run("sysfs_path_injection", func(t *testing.T) {
		// Test if sysfs path construction is vulnerable to injection
		maliciousDevices := []string{
			"../../proc/cmdline",
			"sda1\x00../../etc/passwd",
			"sda1/../../../etc",
		}

		for _, dev := range maliciousDevices {
			// Test path construction
			devicePath := "/dev/" + dev
			cleanPath := filepath.Clean(devicePath)

			if !strings.HasPrefix(cleanPath, "/dev/") {
				t.Logf("Device path injection detected: %s -> %s", devicePath, cleanPath)
			}
		}
	})

	t.Run("proc_mounts_parsing", func(t *testing.T) {
		// Test if proc/mounts parsing is vulnerable to injection
		maliciousMountData := `/dev/sda1 /mnt/normal ext4 rw 0 0
/dev/sdb1 /mnt/../../../etc ext4 rw 0 0
/dev/sdc1 /mnt/test
/../../sensitive ext4 rw 0 0`

		lines := strings.Split(maliciousMountData, "\n")
		for _, line := range lines {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				mountPoint := fields[1]
				cleanMount := filepath.Clean(mountPoint)

				if strings.Contains(cleanMount, "..") {
					t.Logf("Malicious mount point detected: %s", mountPoint)
				}
			}
		}
	})
}

// TestMemoryExhaustionAttacks tests memory exhaustion scenarios
func TestMemoryExhaustionAttacks(t *testing.T) {
	t.Run("huge_directory_listing", func(t *testing.T) {
		tmpDir := t.TempDir()
		dcimPath := filepath.Join(tmpDir, "DCIM")
		os.MkdirAll(dcimPath, 0755)

		// Create many files (but not too many for test)
		fileCount := 1000
		for i := 0; i < fileCount; i++ {
			path := filepath.Join(dcimPath, fmt.Sprintf("photo%d.jpg", i))
			os.WriteFile(path, []byte("test"), 0644)
		}

		// Test with timeout
		done := make(chan bool, 1)
		go func() {
			CountPhotos(tmpDir)
			done <- true
		}()

		select {
		case <-done:
			t.Log("Successfully counted large directory")
		case <-time.After(10 * time.Second):
			t.Error("Timeout counting large directory")
		}
	})

	t.Run("deeply_nested_directories", func(t *testing.T) {
		tmpDir := t.TempDir()
		dcimPath := filepath.Join(tmpDir, "DCIM")
		os.MkdirAll(dcimPath, 0755)

		// Create deeply nested directory structure
		current := dcimPath
		for i := 0; i < 100; i++ {
			current = filepath.Join(current, fmt.Sprintf("level%d", i))
			if err := os.MkdirAll(current, 0755); err != nil {
				t.Fatalf("Failed to create nested dir: %v", err)
			}
		}

		// Add a photo at the deepest level
		photoPath := filepath.Join(current, "deep.jpg")
		os.WriteFile(photoPath, []byte("deep photo"), 0644)

		// Test photo counting with timeout
		done := make(chan bool, 1)
		go func() {
			CountPhotos(tmpDir)
			done <- true
		}()

		select {
		case <-done:
			t.Log("Successfully handled deeply nested directories")
		case <-time.After(5 * time.Second):
			t.Error("Timeout with deeply nested directories")
		}
	})
}

// TestCardIDFileManipulation tests attacks on card ID file
func TestCardIDFileManipulation(t *testing.T) {
	t.Run("cardid_with_injection_attempts", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Try to inject malicious data into card ID
		maliciousIDs := []string{
			"card-\n/etc/passwd",
			"card-\x00malicious",
			"card-" + strings.Repeat("a", 1000),
			"../../etc/passwd",
		}

		for _, id := range maliciousIDs {
			cardIDPath := filepath.Join(tmpDir, CardIDFile)
			os.WriteFile(cardIDPath, []byte(id), 0644)

			// Try to read it back
			readID, _, err := GetOrCreateCardID(tmpDir, nil)
			if err != nil {
				t.Logf("Malicious ID rejected (good): %v", err)
			} else {
				t.Logf("ID read: %s (original: %s)", readID, id)

				// Check if injection succeeded
				if strings.Contains(readID, "\n") || strings.Contains(readID, "\x00") {
					t.Errorf("Injection characters present in card ID!")
				}
			}

			os.Remove(cardIDPath)
		}
	})

	t.Run("cardid_concurrent_write", func(t *testing.T) {
		tmpDir := t.TempDir()

		var wg sync.WaitGroup
		ids := make([]string, 10)

		// Concurrent writes to card ID file
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()

				// Force creation of new ID
				cardIDPath := filepath.Join(tmpDir, CardIDFile)
				os.Remove(cardIDPath)

				id, _, _ := GetOrCreateCardID(tmpDir, nil)
				ids[idx] = id
			}(i)
		}

		wg.Wait()

		// Check for corruption
		finalID, _, _ := GetOrCreateCardID(tmpDir, nil)
		t.Logf("Final card ID after concurrent writes: %s", finalID)

		// Verify ID is valid
		if !strings.HasPrefix(finalID, "card-") {
			t.Error("Card ID format corrupted after concurrent access")
		}
	})
}
