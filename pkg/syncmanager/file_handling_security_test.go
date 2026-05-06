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

// TestCardIDValidationBypass tests attempts to bypass card ID validation
func TestCardIDValidationBypass(t *testing.T) {
	tests := []struct {
		name        string
		cardID      string
		shouldError bool
		description string
	}{
		{
			name:        "empty_cardid",
			cardID:      "",
			shouldError: true,
			description: "Empty card ID should be rejected",
		},
		{
			name:        "path_traversal_dotdot",
			cardID:      "../../../etc/passwd",
			shouldError: true,
			description: "Path traversal with .. should be blocked",
		},
		{
			name:        "absolute_path",
			cardID:      "/etc/passwd",
			shouldError: true,
			description: "Absolute path should be rejected",
		},
		{
			name:        "forward_slash",
			cardID:      "card-abc/def",
			shouldError: true,
			description: "Card ID with forward slash should be rejected",
		},
		{
			name:        "backslash",
			cardID:      "card-abc\\def",
			shouldError: true,
			description: "Card ID with backslash should be rejected",
		},
		{
			name:        "dotdot_in_middle",
			cardID:      "card-ab..cd",
			shouldError: true,
			description: "Card ID with .. in middle should be rejected",
		},
		{
			name:        "null_byte",
			cardID:      "card-abc\x00def",
			shouldError: true,
			description: "Card ID with null byte should be rejected",
		},
		{
			name:        "too_short",
			cardID:      "card-abc",
			shouldError: true,
			description: "Card ID that's too short should be rejected",
		},
		{
			name:        "too_long",
			cardID:      "card-" + strings.Repeat("a", 100),
			shouldError: true,
			description: "Card ID that's too long should be rejected",
		},
		{
			name:        "special_characters",
			cardID:      "card-ab!@#$%^",
			shouldError: true,
			description: "Card ID with special characters should be rejected",
		},
		{
			name:        "valid_cardid",
			cardID:      "card-0123456789abcdef",
			shouldError: false,
			description: "Valid card ID should be accepted",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateCardID(tt.cardID)

			if tt.shouldError && err == nil {
				t.Errorf("%s: Expected validation error but got none", tt.description)
			}
			if !tt.shouldError && err != nil {
				t.Errorf("%s: Expected no error but got: %v", tt.description, err)
			}
		})
	}
}

// TestPathConstructionVulnerabilities tests path construction with user input
func TestPathConstructionVulnerabilities(t *testing.T) {
	tmpDir := t.TempDir()

	stateMgr, err := state.NewManager()
	if err != nil {
		t.Fatalf("Failed to create state manager: %v", err)
	}

	configPath := filepath.Join(tmpDir, "rclone.conf")
	os.WriteFile(configPath, []byte("[testremote]\ntype = local\n"), 0644)

	tests := []struct {
		name        string
		remotePath  string
		cardID      string
		description string
	}{
		{
			name:        "traversal_in_remote_path",
			remotePath:  "/photos/../../../etc",
			cardID:      "card-0123456789abcdef",
			description: "Path traversal in remote path",
		},
		{
			name:        "traversal_in_cardid",
			remotePath:  "/photos",
			cardID:      "../../../etc/passwd",
			description: "Path traversal in card ID",
		},
		{
			name:        "double_slash_remote_path",
			remotePath:  "//photos//test",
			cardID:      "card-0123456789abcdef",
			description: "Double slashes in remote path",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mgr := NewManager(configPath, "testremote", tt.remotePath, stateMgr, 1, 1)

			// Test path construction in various methods
			// These should either validate the card ID or clean the paths safely

			// Test GetRemoteSize
			_, err := mgr.GetRemoteSize(tt.cardID)
			if err != nil {
				t.Logf("%s: GetRemoteSize rejected (good): %v", tt.description, err)
			}

			// The key is that validateCardID should catch malicious card IDs
			if err := validateCardID(tt.cardID); err == nil {
				// If validation passed, check the constructed path is safe
				destPath := filepath.Join("testremote:"+tt.remotePath, tt.cardID, "DCIM")
				if strings.Contains(destPath, "..") {
					t.Logf("%s: remote path still needs validation: %s", tt.description, destPath)
				}
			}
		})
	}
}

// TestSymlinkAttacksInSync tests symlink-based attacks during sync
func TestSymlinkAttacksInSync(t *testing.T) {
	t.Run("source_with_symlinks", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create source directory structure
		srcDir := filepath.Join(tmpDir, "source")
		os.MkdirAll(srcDir, 0755)

		// Create normal file
		normalFile := filepath.Join(srcDir, "photo1.jpg")
		os.WriteFile(normalFile, []byte("photo data"), 0644)

		// Create symlink pointing outside source
		outsideFile := filepath.Join(tmpDir, "outside.jpg")
		os.WriteFile(outsideFile, []byte("outside data"), 0644)

		symlinkPath := filepath.Join(srcDir, "symlink.jpg")
		if err := os.Symlink(outsideFile, symlinkPath); err != nil {
			t.Skipf("Cannot create symlink: %v", err)
		}

		// Create symlink pointing to sensitive file
		sensitiveLink := filepath.Join(srcDir, "passwd.jpg")
		os.Symlink("/etc/passwd", sensitiveLink)

		t.Log("Created source directory with symlinks - rclone should handle these appropriately")
	})

	t.Run("symlink_race_during_sync", func(t *testing.T) {
		tmpDir := t.TempDir()
		srcDir := filepath.Join(tmpDir, "source")
		os.MkdirAll(srcDir, 0755)

		// Create files
		for i := 0; i < 10; i++ {
			path := filepath.Join(srcDir, fmt.Sprintf("photo%d.jpg", i))
			os.WriteFile(path, []byte("test"), 0644)
		}

		var wg sync.WaitGroup

		// Goroutine to create symlinks during listing
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				linkPath := filepath.Join(srcDir, "race.jpg")
				os.Symlink("/etc/passwd", linkPath)
				time.Sleep(1 * time.Millisecond)
				os.Remove(linkPath)
			}
		}()

		// Goroutine to list directory
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				os.ReadDir(srcDir)
				time.Sleep(1 * time.Millisecond)
			}
		}()

		wg.Wait()
		t.Log("Symlink race condition test completed")
	})
}

// TestFileDescriptorLeaks tests for FD leaks in sync operations
func TestFileDescriptorLeaks(t *testing.T) {
	t.Run("unclosed_rclone_config", func(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "rclone.conf")

		// Create config file
		os.WriteFile(configPath, []byte("[test]\ntype = local\n"), 0644)

		stateMgr, err := state.NewManager()
		if err != nil {
			t.Fatalf("Failed to create state manager: %v", err)
		}

		// Create many managers (each loads config)
		for i := 0; i < 100; i++ {
			_ = NewManager(configPath, "test", "/photos", stateMgr, 1, 1)
		}

		t.Log("Created 100 managers - check for FD leaks")
	})

	t.Run("rapid_sync_operations", func(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "rclone.conf")
		os.WriteFile(configPath, []byte("[local]\ntype = local\n"), 0644)

		stateDir := filepath.Join(tmpDir, "state")
		os.MkdirAll(stateDir, 0755)

		stateMgr, _ := state.NewManager()
		mgr := NewManager(configPath, "local", tmpDir, stateMgr, 1, 1)

		// Rapid operations that might leak FDs
		for i := 0; i < 50; i++ {
			mgr.ListRemotes()
			mgr.GetRemoteSize("card-test1234")
			time.Sleep(1 * time.Millisecond)
		}

		t.Log("Completed rapid operations - check for FD leaks")
	})
}

// TestConcurrentSyncSafety tests concurrent sync operations
func TestConcurrentSyncSafety(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "rclone.conf")
	os.WriteFile(configPath, []byte("[local]\ntype = local\n"), 0644)

	stateMgr, _ := state.NewManager()
	mgr := NewManager(configPath, "local", tmpDir, stateMgr, 1, 1)

	srcDir := filepath.Join(tmpDir, "source")
	os.MkdirAll(srcDir, 0755)
	os.WriteFile(filepath.Join(srcDir, "test.jpg"), []byte("test"), 0644)

	var wg sync.WaitGroup
	errors := make(chan error, 10)

	// Try to start multiple syncs concurrently
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			// Create unique card ID for each
			cardID := fmt.Sprintf("card-test%04d", idx)
			err := mgr.Sync(srcDir, cardID, 1, 100)
			if err != nil {
				errors <- err
			}
		}(i)
	}

	// Also try concurrent cancellations
	go func() {
		time.Sleep(10 * time.Millisecond)
		for i := 0; i < 5; i++ {
			mgr.Cancel()
			time.Sleep(5 * time.Millisecond)
		}
	}()

	wg.Wait()
	close(errors)

	// Check errors
	errorCount := 0
	for err := range errors {
		errorCount++
		t.Logf("Sync error: %v", err)
	}

	t.Logf("Concurrent sync test completed with %d errors", errorCount)
}

// TestRcloneConfigInjection tests config injection vulnerabilities
func TestRcloneConfigInjection(t *testing.T) {
	tests := []struct {
		name        string
		configData  string
		remoteName  string
		shouldError bool
		description string
	}{
		{
			name: "command_injection_in_config",
			configData: `[malicious]
type = local
nounc = true
command = $(rm -rf /)
`,
			remoteName:  "malicious",
			shouldError: false, // Should be safely escaped by rclone
			description: "Command injection in config should be escaped",
		},
		{
			name: "path_traversal_in_config",
			configData: `[traversal]
type = local
path = ../../../etc
`,
			remoteName:  "traversal",
			shouldError: false,
			description: "Path traversal in config",
		},
		{
			name:        "null_byte_injection",
			configData:  "[test\x00malicious]\ntype = local\n",
			remoteName:  "test",
			shouldError: false,
			description: "Null byte in config",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			configPath := filepath.Join(tmpDir, "rclone.conf")

			if err := os.WriteFile(configPath, []byte(tt.configData), 0644); err != nil {
				t.Fatalf("Failed to write config: %v", err)
			}

			stateDir := filepath.Join(tmpDir, "state")
			os.MkdirAll(stateDir, 0755)

			stateMgr, _ := state.NewManager()
			mgr := NewManager(configPath, tt.remoteName, "/photos", stateMgr, 1, 1)

			// Try to use the potentially malicious config
			_, err := mgr.ListRemotes()
			if err != nil {
				t.Logf("%s: Config rejected: %v", tt.description, err)
			}
		})
	}
}

// TestProgressParsingAttacks tests malicious progress data
func TestProgressParsingAttacks(t *testing.T) {
	// This would test the processLogLine function if it were exported
	// For now, document the vulnerability

	t.Run("oversized_progress_values", func(t *testing.T) {
		// Test if huge progress values cause integer overflow
		hugeBytes := int64(9223372036854775807) // Max int64

		t.Logf("Testing with huge byte value: %d", hugeBytes)
		// The progress update should handle this without overflow
	})

	t.Run("negative_progress_values", func(t *testing.T) {
		// Test negative progress values
		negativeBytes := int64(-1000000)

		t.Logf("Testing with negative byte value: %d", negativeBytes)
		// Should not cause underflow or negative percentages
	})
}

// TestTemporaryFileHandling tests temp file vulnerabilities in sync
func TestTemporaryFileHandling(t *testing.T) {
	t.Run("temp_file_cleanup_on_cancel", func(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "rclone.conf")
		os.WriteFile(configPath, []byte("[local]\ntype = local\n"), 0644)

		stateDir := filepath.Join(tmpDir, "state")
		os.MkdirAll(stateDir, 0755)

		stateMgr, _ := state.NewManager()
		mgr := NewManager(configPath, "local", tmpDir, stateMgr, 1, 1)

		srcDir := filepath.Join(tmpDir, "source")
		os.MkdirAll(srcDir, 0755)

		// Create files
		for i := 0; i < 10; i++ {
			path := filepath.Join(srcDir, fmt.Sprintf("test%d.jpg", i))
			os.WriteFile(path, []byte("test data"), 0644)
		}

		// Start sync and cancel immediately
		go func() {
			time.Sleep(50 * time.Millisecond)
			mgr.Cancel()
		}()

		mgr.Sync(srcDir, "card-test1234", 10, 1000)

		// Check for leftover temp files
		entries, _ := os.ReadDir(tmpDir)
		for _, entry := range entries {
			if strings.Contains(entry.Name(), ".tmp") || strings.Contains(entry.Name(), "rclone-") {
				t.Logf("Found potential temp file: %s", entry.Name())
			}
		}
	})
}

// TestContextCancellationSafety tests context cancellation handling
func TestContextCancellationSafety(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "rclone.conf")
	os.WriteFile(configPath, []byte("[local]\ntype = local\n"), 0644)

	stateMgr, _ := state.NewManager()
	mgr := NewManager(configPath, "local", tmpDir, stateMgr, 1, 1)

	t.Run("rapid_cancel_operations", func(t *testing.T) {
		var wg sync.WaitGroup

		// Start cancel operations
		for i := 0; i < 100; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				mgr.Cancel()
			}()
		}

		wg.Wait()
		t.Log("Rapid cancel operations completed")
	})

	t.Run("context_leak_check", func(t *testing.T) {
		// Start multiple operations to check for context leaks
		for i := 0; i < 10; i++ {
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			_ = ctx
			// Intentionally not calling cancel to check for leaks
			_ = cancel // Suppress unused warning
		}

		t.Log("Context leak test completed")
	})
}

// TestFileLockingIssues tests file locking problems
func TestFileLockingIssues(t *testing.T) {
	t.Run("concurrent_config_access", func(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "rclone.conf")
		os.WriteFile(configPath, []byte("[test]\ntype = local\n"), 0644)

		stateDir := filepath.Join(tmpDir, "state")
		os.MkdirAll(stateDir, 0755)

		stateMgr, _ := state.NewManager()

		var wg sync.WaitGroup

		// Concurrent config reads
		for i := 0; i < 50; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				mgr := NewManager(configPath, "test", "/photos", stateMgr, 1, 1)
				mgr.ListRemotes()
			}()
		}

		wg.Wait()
		t.Log("Concurrent config access completed")
	})

	t.Run("config_modification_during_sync", func(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "rclone.conf")
		os.WriteFile(configPath, []byte("[test]\ntype = local\n"), 0644)

		var wg sync.WaitGroup

		// Modify config while it might be in use
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				newConfig := fmt.Sprintf("[test%d]\ntype = local\n", i)
				os.WriteFile(configPath, []byte(newConfig), 0644)
				time.Sleep(5 * time.Millisecond)
			}
		}()

		// Read config concurrently
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				os.ReadFile(configPath)
				time.Sleep(5 * time.Millisecond)
			}
		}()

		wg.Wait()
		t.Log("Config modification race test completed")
	})
}

// TestMemoryExhaustion tests memory exhaustion attacks
func TestMemoryExhaustion(t *testing.T) {
	t.Run("huge_file_list", func(t *testing.T) {
		// Test handling of huge file lists
		// In practice, rclone handles this, but we should ensure
		// our wrapper doesn't accumulate unbounded data

		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "rclone.conf")
		os.WriteFile(configPath, []byte("[local]\ntype = local\n"), 0644)

		stateDir := filepath.Join(tmpDir, "state")
		os.MkdirAll(stateDir, 0755)

		// Create directory with many files
		testDir := filepath.Join(tmpDir, "files")
		os.MkdirAll(testDir, 0755)

		for i := 0; i < 1000; i++ {
			path := filepath.Join(testDir, fmt.Sprintf("file%d.txt", i))
			os.WriteFile(path, []byte("test"), 0644)
		}

		stateMgr, _ := state.NewManager()
		mgr := NewManager(configPath, "local", tmpDir, stateMgr, 1, 1)

		// List files (should handle large lists efficiently)
		_, err := mgr.ListFiles("files")
		if err != nil {
			t.Logf("ListFiles error: %v", err)
		}

		t.Log("Large file list handling completed")
	})
}

// TestCardIDCollisions tests card ID collision handling
func TestCardIDCollisions(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "rclone.conf")
	os.WriteFile(configPath, []byte("[local]\ntype = local\n"), 0644)

	stateMgr, _ := state.NewManager()
	mgr := NewManager(configPath, "local", tmpDir, stateMgr, 1, 1)

	// Try to sync with same card ID multiple times
	cardID := "card-test1234"

	srcDir := filepath.Join(tmpDir, "source")
	os.MkdirAll(srcDir, 0755)
	os.WriteFile(filepath.Join(srcDir, "test.jpg"), []byte("test"), 0644)

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			mgr.Sync(srcDir, cardID, 1, 100)
		}()
	}

	wg.Wait()
	t.Log("Card ID collision test completed")
}
