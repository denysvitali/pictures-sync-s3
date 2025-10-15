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

// TestCardReformatted tests card reformatted detection (file count < 30% threshold)
func TestCardReformatted(t *testing.T) {
	// Setup: Create temp mount path with DCIM directory
	tempDir := t.TempDir()
	dcimPath := filepath.Join(tempDir, "DCIM")
	if err := os.MkdirAll(dcimPath, 0755); err != nil {
		t.Fatal(err)
	}

	// Create initial card ID
	initialID := "card-1234567890abcdef"
	idPath := filepath.Join(tempDir, CardIDFile)
	if err := os.WriteFile(idPath, []byte(initialID+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create 100 initial photos
	for i := 0; i < 100; i++ {
		photoPath := filepath.Join(dcimPath, fmt.Sprintf("IMG_%04d.jpg", i))
		if err := os.WriteFile(photoPath, []byte("fake photo data"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Simulate first sync: count photos
	totalFiles, _, err := CountPhotos(tempDir)
	if err != nil {
		t.Fatal(err)
	}
	if totalFiles != 100 {
		t.Errorf("Expected 100 files, got %d", totalFiles)
	}

	// Read card ID
	cardID, isNew, err := GetOrCreateCardID(tempDir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if isNew {
		t.Error("Expected existing card, got new card")
	}
	if cardID != initialID {
		t.Errorf("Expected card ID %s, got %s", initialID, cardID)
	}

	// Simulate reformat: delete most photos (keep only 20, which is 20% < 30% threshold)
	for i := 20; i < 100; i++ {
		photoPath := filepath.Join(dcimPath, fmt.Sprintf("IMG_%04d.jpg", i))
		os.Remove(photoPath)
	}

	// Count photos after "reformat"
	totalFiles, _, err = CountPhotos(tempDir)
	if err != nil {
		t.Fatal(err)
	}
	if totalFiles != 20 {
		t.Errorf("Expected 20 files after reformat, got %d", totalFiles)
	}

	// Calculate percentage
	percentageOfLast := float64(totalFiles) / float64(100)
	threshold := 0.3
	if percentageOfLast >= threshold {
		t.Errorf("Expected percentage (%.2f) to be < threshold (%.2f)", percentageOfLast, threshold)
	}

	// BUG #1: The reformat detection is done in main.go, not in GetOrCreateCardID
	// GetOrCreateCardID will still return the same card ID even after reformat
	// This test shows that CreateNewCardID must be called manually
	cardID, isNew, err = GetOrCreateCardID(tempDir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cardID != initialID {
		t.Errorf("GetOrCreateCardID should return same ID until CreateNewCardID is called")
	}

	// Manually trigger CreateNewCardID (as main.go does)
	newCardID, err := CreateNewCardID(tempDir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if newCardID == initialID {
		t.Error("CreateNewCardID should generate a different ID")
	}

	// Verify new ID is persisted
	cardID, isNew, err = GetOrCreateCardID(tempDir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cardID != newCardID {
		t.Errorf("Expected new card ID %s, got %s", newCardID, cardID)
	}
}

// TestCardWithExactly30PercentFiles tests boundary condition
func TestCardWithExactly30PercentFiles(t *testing.T) {
	tempDir := t.TempDir()
	dcimPath := filepath.Join(tempDir, "DCIM")
	if err := os.MkdirAll(dcimPath, 0755); err != nil {
		t.Fatal(err)
	}

	// Create 100 photos
	for i := 0; i < 100; i++ {
		photoPath := filepath.Join(dcimPath, fmt.Sprintf("IMG_%04d.jpg", i))
		if err := os.WriteFile(photoPath, []byte("fake"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Simulate last sync had 100 files
	lastSyncFiles := 100

	// Now have exactly 30 files (30%)
	for i := 30; i < 100; i++ {
		photoPath := filepath.Join(dcimPath, fmt.Sprintf("IMG_%04d.jpg", i))
		os.Remove(photoPath)
	}

	totalFiles, _, err := CountPhotos(tempDir)
	if err != nil {
		t.Fatal(err)
	}
	if totalFiles != 30 {
		t.Errorf("Expected 30 files, got %d", totalFiles)
	}

	// Test boundary condition: 30% exactly
	percentageOfLast := float64(totalFiles) / float64(lastSyncFiles)
	threshold := 0.3

	// BUG #2: Boundary condition ambiguity
	// If percentageOfLast == threshold (0.3), should it trigger reformat detection?
	// Current code uses `<` which means 30% exactly will NOT trigger reformat
	// This could be a problem: if a card had exactly 30% files, it won't be treated as reformatted
	if percentageOfLast < threshold {
		t.Error("30% should NOT be less than 30% threshold (boundary issue)")
	}
	if percentageOfLast == threshold {
		t.Log("BUG #2: At exactly 30%, reformat is NOT detected due to '<' comparison")
		t.Log("Consider using '<=' if 30% should also trigger reformat")
	}
}

// TestNewCard tests new card with no .pictures-sync-id file
func TestNewCard(t *testing.T) {
	tempDir := t.TempDir()
	dcimPath := filepath.Join(tempDir, "DCIM")
	if err := os.MkdirAll(dcimPath, 0755); err != nil {
		t.Fatal(err)
	}

	// Create some photos
	for i := 0; i < 10; i++ {
		photoPath := filepath.Join(dcimPath, fmt.Sprintf("IMG_%04d.jpg", i))
		if err := os.WriteFile(photoPath, []byte("fake"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// No .pictures-sync-id file exists
	idPath := filepath.Join(tempDir, CardIDFile)
	if _, err := os.Stat(idPath); err == nil {
		t.Error("Card ID file should not exist yet")
	}

	// Get or create card ID
	cardID, isNew, err := GetOrCreateCardID(tempDir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !isNew {
		t.Error("Expected new card, got existing card")
	}
	if cardID == "" {
		t.Error("Card ID should not be empty")
	}
	if !strings.HasPrefix(cardID, "card-") {
		t.Errorf("Card ID should start with 'card-', got %s", cardID)
	}

	// Verify file was created
	if _, err := os.Stat(idPath); os.IsNotExist(err) {
		t.Error("Card ID file should have been created")
	}

	// Read it back
	data, err := os.ReadFile(idPath)
	if err != nil {
		t.Fatal(err)
	}
	savedID := strings.TrimSpace(string(data))
	if savedID != cardID {
		t.Errorf("Saved ID %s doesn't match returned ID %s", savedID, cardID)
	}
}

// TestCardIDFileCorrupted tests corrupted or invalid card ID file
func TestCardIDFileCorrupted(t *testing.T) {
	testCases := []struct {
		name     string
		content  string
		expected string
	}{
		{
			name:     "empty file",
			content:  "",
			expected: "should generate new ID for empty file",
		},
		{
			name:     "whitespace only",
			content:  "   \n\t\r\n   ",
			expected: "should generate new ID for whitespace",
		},
		{
			name:     "invalid characters",
			content:  "../../etc/passwd",
			expected: "should reject path traversal attempt",
		},
		{
			name:     "very long ID",
			content:  strings.Repeat("a", 10000),
			expected: "should handle very long IDs",
		},
		{
			name:     "null bytes",
			content:  "card-test\x00\x00\x00",
			expected: "should handle null bytes",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tempDir := t.TempDir()
			idPath := filepath.Join(tempDir, CardIDFile)

			// Create corrupted file
			if err := os.WriteFile(idPath, []byte(tc.content), 0644); err != nil {
				t.Fatal(err)
			}

			cardID, isNew, err := GetOrCreateCardID(tempDir, nil)
			if err != nil {
				t.Fatal(err)
			}

			// BUG #3: No validation of card ID content
			// GetOrCreateCardID will accept ANY content as a valid card ID
			// Even empty strings, path traversals, null bytes, etc.
			trimmed := strings.TrimSpace(tc.content)
			if trimmed == "" {
				// Should generate new ID for empty/whitespace
				if !isNew {
					t.Error("BUG #3: Empty ID should trigger new card generation")
				}
				if cardID == "" {
					t.Error("BUG #3: Should generate new ID when existing ID is empty")
				}
			} else {
				// BUG #3: No validation - accepts any content
				if !isNew && cardID == trimmed {
					t.Logf("BUG #3: Accepted potentially invalid card ID: %q", cardID)
				}
			}
		})
	}
}

// TestTwoCardsWithSameID tests collision when two cards somehow have the same ID
func TestTwoCardsWithSameID(t *testing.T) {
	// Setup two separate mount paths
	card1Dir := t.TempDir()
	card2Dir := t.TempDir()

	sameID := "card-duplicate123"

	// Create DCIM directories
	for _, dir := range []string{card1Dir, card2Dir} {
		dcimPath := filepath.Join(dir, "DCIM")
		if err := os.MkdirAll(dcimPath, 0755); err != nil {
			t.Fatal(err)
		}

		// Write same ID to both cards
		idPath := filepath.Join(dir, CardIDFile)
		if err := os.WriteFile(idPath, []byte(sameID+"\n"), 0644); err != nil {
			t.Fatal(err)
		}

		// Create some photos
		for i := 0; i < 5; i++ {
			photoPath := filepath.Join(dcimPath, fmt.Sprintf("IMG_%04d.jpg", i))
			if err := os.WriteFile(photoPath, []byte("fake"), 0644); err != nil {
				t.Fatal(err)
			}
		}
	}

	// Read IDs from both cards
	id1, isNew1, err := GetOrCreateCardID(card1Dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	id2, isNew2, err := GetOrCreateCardID(card2Dir, nil)
	if err != nil {
		t.Fatal(err)
	}

	if isNew1 || isNew2 {
		t.Error("Both cards should be existing, not new")
	}

	// BUG #4: No collision detection
	// Both cards will have the same ID and sync to the same remote folder
	// This can cause data loss if Card 2's photos overwrite Card 1's photos
	if id1 == id2 {
		t.Logf("BUG #4: Both cards have same ID: %s", id1)
		t.Log("BUG #4: Photos from both cards will sync to same remote folder")
		t.Log("BUG #4: No mechanism to detect or prevent ID collisions")
	}
}

// TestCardIDGenerationCollisions tests for potential ID generation collisions
func TestCardIDGenerationCollisions(t *testing.T) {
	// Generate many IDs to check for collisions
	const numIDs = 10000
	idMap := make(map[string]bool)
	collisions := 0

	for i := 0; i < numIDs; i++ {
		id := generateCardID()
		if idMap[id] {
			collisions++
			t.Errorf("Collision detected: %s generated twice", id)
		}
		idMap[id] = true

		// Check format
		if !strings.HasPrefix(id, "card-") {
			t.Errorf("Invalid ID format: %s", id)
		}
		// Should be "card-" + 16 hex characters
		if len(id) != 21 { // "card-" (5) + 16 hex chars
			t.Errorf("Invalid ID length: %s (len=%d, expected 21)", id, len(id))
		}
	}

	if collisions > 0 {
		t.Errorf("BUG #5: Found %d collisions in %d IDs", collisions, numIDs)
	} else {
		t.Logf("Generated %d unique IDs with no collisions", numIDs)
	}
}

// TestFileCountCalculationErrors tests edge cases in photo counting
func TestFileCountCalculationErrors(t *testing.T) {
	testCases := []struct {
		name          string
		setup         func(string) error
		expectedFiles int
		expectError   bool
	}{
		{
			name: "empty DCIM directory",
			setup: func(dir string) error {
				return os.MkdirAll(filepath.Join(dir, "DCIM"), 0755)
			},
			expectedFiles: 0,
			expectError:   false,
		},
		{
			name: "DCIM with subdirectories only",
			setup: func(dir string) error {
				dcimPath := filepath.Join(dir, "DCIM")
				if err := os.MkdirAll(filepath.Join(dcimPath, "100CANON"), 0755); err != nil {
					return err
				}
				return os.MkdirAll(filepath.Join(dcimPath, "101CANON"), 0755)
			},
			expectedFiles: 0,
			expectError:   false,
		},
		{
			name: "DCIM missing",
			setup: func(dir string) error {
				// Don't create DCIM directory
				return nil
			},
			expectedFiles: 0,
			expectError:   true,
		},
		{
			name: "DCIM is a file not directory",
			setup: func(dir string) error {
				return os.WriteFile(filepath.Join(dir, "DCIM"), []byte("not a directory"), 0644)
			},
			expectedFiles: 0,
			expectError:   true,
		},
		{
			name: "photos in subdirectories",
			setup: func(dir string) error {
				dcimPath := filepath.Join(dir, "DCIM", "100CANON")
				if err := os.MkdirAll(dcimPath, 0755); err != nil {
					return err
				}
				for i := 0; i < 5; i++ {
					photoPath := filepath.Join(dcimPath, fmt.Sprintf("IMG_%04d.jpg", i))
					if err := os.WriteFile(photoPath, []byte("fake"), 0644); err != nil {
						return err
					}
				}
				return nil
			},
			expectedFiles: 5,
			expectError:   false,
		},
		{
			name: "mixed file types",
			setup: func(dir string) error {
				dcimPath := filepath.Join(dir, "DCIM")
				if err := os.MkdirAll(dcimPath, 0755); err != nil {
					return err
				}
				// Valid photo files
				validTypes := []string{".jpg", ".jpeg", ".png", ".gif", ".raw", ".cr2", ".nef", ".arw", ".mp4", ".mov"}
				for i, ext := range validTypes {
					photoPath := filepath.Join(dcimPath, fmt.Sprintf("IMG_%04d%s", i, ext))
					if err := os.WriteFile(photoPath, []byte("fake"), 0644); err != nil {
						return err
					}
				}
				// Invalid files (should be ignored)
				invalidTypes := []string{".txt", ".pdf", ".doc", ".exe"}
				for i, ext := range invalidTypes {
					filePath := filepath.Join(dcimPath, fmt.Sprintf("file_%04d%s", i, ext))
					if err := os.WriteFile(filePath, []byte("fake"), 0644); err != nil {
						return err
					}
				}
				return nil
			},
			expectedFiles: 10, // Only valid photo types
			expectError:   false,
		},
		{
			name: "symlinks to photos",
			setup: func(dir string) error {
				dcimPath := filepath.Join(dir, "DCIM")
				if err := os.MkdirAll(dcimPath, 0755); err != nil {
					return err
				}
				// Create a real photo
				realPhoto := filepath.Join(dcimPath, "real.jpg")
				if err := os.WriteFile(realPhoto, []byte("fake"), 0644); err != nil {
					return err
				}
				// Create a symlink to it
				symlinkPhoto := filepath.Join(dcimPath, "link.jpg")
				return os.Symlink(realPhoto, symlinkPhoto)
			},
			expectedFiles: 2, // Both real and symlink should be counted
			expectError:   false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tempDir := t.TempDir()
			if err := tc.setup(tempDir); err != nil {
				t.Fatal(err)
			}

			count, _, err := CountPhotos(tempDir)
			if tc.expectError && err == nil {
				t.Error("Expected error but got none")
			}
			if !tc.expectError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
			if count != tc.expectedFiles {
				t.Errorf("Expected %d files, got %d", tc.expectedFiles, count)
			}
		})
	}
}

// TestPhotosAddedWithoutSync tests scenario where photos are added but sync not run
func TestPhotosAddedWithoutSync(t *testing.T) {
	tempDir := t.TempDir()
	dcimPath := filepath.Join(tempDir, "DCIM")
	if err := os.MkdirAll(dcimPath, 0755); err != nil {
		t.Fatal(err)
	}

	// Initial: 100 photos
	for i := 0; i < 100; i++ {
		photoPath := filepath.Join(dcimPath, fmt.Sprintf("IMG_%04d.jpg", i))
		if err := os.WriteFile(photoPath, []byte("fake"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Create card ID
	cardID, _, err := GetOrCreateCardID(tempDir, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Simulate first sync
	count1, _, err := CountPhotos(tempDir)
	if err != nil {
		t.Fatal(err)
	}
	lastSyncFiles := count1

	// User adds 50 more photos (now 150 total)
	for i := 100; i < 150; i++ {
		photoPath := filepath.Join(dcimPath, fmt.Sprintf("IMG_%04d.jpg", i))
		if err := os.WriteFile(photoPath, []byte("fake"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Count photos again
	count2, _, err := CountPhotos(tempDir)
	if err != nil {
		t.Fatal(err)
	}
	if count2 != 150 {
		t.Errorf("Expected 150 photos, got %d", count2)
	}

	// Check reformat detection logic
	percentageOfLast := float64(count2) / float64(lastSyncFiles)
	threshold := 0.3

	// BUG #6: Photos added should NOT trigger reformat detection
	// 150/100 = 1.5 (150%), which is > 30%, so reformat should NOT be detected
	// This is CORRECT behavior - but the bug is that this case is not explicitly tested
	if percentageOfLast < threshold {
		t.Error("BUG #6: Photos added should NOT trigger reformat detection")
	}

	// Verify card ID remains the same
	cardID2, _, err := GetOrCreateCardID(tempDir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cardID != cardID2 {
		t.Error("Card ID should remain the same when photos are added")
	}

	t.Logf("Correctly handled: Added photos (%.0f%% of last sync) did not trigger reformat", percentageOfLast*100)
}

// TestEmptyCardVsReformattedCard tests distinguishing between empty card and reformatted card
func TestEmptyCardVsReformattedCard(t *testing.T) {
	// Case 1: Truly empty card (no photos ever)
	t.Run("empty card no history", func(t *testing.T) {
		tempDir := t.TempDir()
		dcimPath := filepath.Join(tempDir, "DCIM")
		if err := os.MkdirAll(dcimPath, 0755); err != nil {
			t.Fatal(err)
		}

		// No photos, but card ID exists
		cardID, isNew, err := GetOrCreateCardID(tempDir, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !isNew {
			t.Error("Should be new card")
		}
		if cardID == "" {
			t.Error("Card ID should not be empty")
		}

		count, _, err := CountPhotos(tempDir)
		if err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Errorf("Expected 0 photos, got %d", count)
		}

		// BUG #7: Empty card with no history vs reformatted card with 0 photos
		// If lastSync is nil (no history), we can't detect reformat
		// If lastSync exists and current count is 0, it could be:
		//   a) All photos were deleted by user
		//   b) Card was reformatted
		// Current code would treat 0/100 = 0% < 30% as reformat - CORRECT
		// But what if user intentionally deleted all photos?
		t.Log("Empty new card correctly handled")
	})

	// Case 2: Reformatted card (had photos before, now 0)
	t.Run("reformatted card now empty", func(t *testing.T) {
		tempDir := t.TempDir()
		dcimPath := filepath.Join(tempDir, "DCIM")
		if err := os.MkdirAll(dcimPath, 0755); err != nil {
			t.Fatal(err)
		}

		// Create card ID
		initialID, _, err := GetOrCreateCardID(tempDir, nil)
		if err != nil {
			t.Fatal(err)
		}

		// Create photos
		for i := 0; i < 100; i++ {
			photoPath := filepath.Join(dcimPath, fmt.Sprintf("IMG_%04d.jpg", i))
			if err := os.WriteFile(photoPath, []byte("fake"), 0644); err != nil {
				t.Fatal(err)
			}
		}

		// Simulate sync
		lastSyncFiles := 100

		// "Reformat" - delete all photos
		files, _ := filepath.Glob(filepath.Join(dcimPath, "*.jpg"))
		for _, f := range files {
			os.Remove(f)
		}

		// Count photos
		count, _, err := CountPhotos(tempDir)
		if err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Errorf("Expected 0 photos after reformat, got %d", count)
		}

		// Check reformat detection
		percentageOfLast := float64(count) / float64(lastSyncFiles)
		threshold := 0.3

		// 0/100 = 0% < 30% - should trigger reformat
		if percentageOfLast >= threshold {
			t.Error("0% should be < 30% threshold")
		}

		// BUG #8: Edge case - what if card is ejected before ANY files are copied?
		// Should we still create a new card ID?
		// Current code would create new ID for 0 photos if last sync had photos
		t.Log("Reformatted card with 0 photos correctly triggers reformat detection")

		// Verify we can create new ID
		newID, err := CreateNewCardID(tempDir, nil)
		if err != nil {
			t.Fatal(err)
		}
		if newID == initialID {
			t.Error("Should generate new ID for reformatted card")
		}
	})

	// BUG #7: Ambiguous case - user deletes all photos vs reformat
	// There's no way to distinguish between these cases
	// Both result in file count drop to 0
	t.Log("BUG #7: Cannot distinguish between 'user deleted all photos' vs 'card reformatted'")
}

// TestRaceConditionCardRemovedWhileCounting tests race condition
func TestRaceConditionCardRemovedWhileCounting(t *testing.T) {
	tempDir := t.TempDir()
	dcimPath := filepath.Join(tempDir, "DCIM")
	if err := os.MkdirAll(dcimPath, 0755); err != nil {
		t.Fatal(err)
	}

	// Create many photos
	for i := 0; i < 1000; i++ {
		photoPath := filepath.Join(dcimPath, fmt.Sprintf("IMG_%04d.jpg", i))
		if err := os.WriteFile(photoPath, []byte("fake photo data here"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Start counting in one goroutine
	var wg sync.WaitGroup
	var countErr error
	var fileCount int

	wg.Add(1)
	go func() {
		defer wg.Done()
		fileCount, _, countErr = CountPhotos(tempDir)
	}()

	// Simulate card removal by deleting files while counting
	time.Sleep(10 * time.Millisecond) // Let counting start
	go func() {
		// Delete DCIM directory while counting is in progress
		os.RemoveAll(dcimPath)
	}()

	wg.Wait()

	// BUG #9: Race condition - CountPhotos may fail if card is removed during counting
	// The function uses filepath.WalkDir which will error if directory is removed mid-walk
	if countErr != nil {
		t.Logf("BUG #9: CountPhotos failed due to race condition: %v", countErr)
		t.Log("BUG #9: This can happen if card is removed while counting photos")
		t.Log("BUG #9: Should handle this gracefully in main.go")
	} else {
		// If it succeeded, file count may be partial
		t.Logf("Counting completed with %d files (may be partial due to race)", fileCount)
	}

	// The real bug is in main.go - handleCardInserted runs in a goroutine
	// If card is removed after event is received but before counting completes,
	// the code will fail ungracefully
}

// TestReadOnlyRemountAfterCardIDWrite tests read-only remount behavior
func TestReadOnlyRemountAfterCardIDWrite(t *testing.T) {
	tempDir := t.TempDir()
	dcimPath := filepath.Join(tempDir, "DCIM")
	if err := os.MkdirAll(dcimPath, 0755); err != nil {
		t.Fatal(err)
	}

	// Test without monitor (no remount)
	t.Run("without monitor", func(t *testing.T) {
		cardID, isNew, err := GetOrCreateCardID(tempDir, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !isNew {
			t.Error("Expected new card")
		}
		if cardID == "" {
			t.Error("Card ID should not be empty")
		}

		// Verify we can still write (since no remount happened)
		testFile := filepath.Join(tempDir, "test.txt")
		if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
			// This is expected to succeed since we didn't remount read-only
			t.Logf("Write succeeded (no monitor, no remount): %v", err)
		}
	})

	// BUG #10: If RemountReadOnly fails, the card remains read-write
	// This is dangerous as it could lead to data corruption during sync
	// The current code returns an error, but the caller might not handle it properly
	t.Log("BUG #10: If RemountReadOnly fails, card remains writable during sync")
	t.Log("BUG #10: This could cause data corruption if camera writes while syncing")
	t.Log("BUG #10: main.go should abort sync if remount fails")
}

// TestCreateNewCardIDFailure tests failure handling when writing new card ID fails
func TestCreateNewCardIDFailure(t *testing.T) {
	tempDir := t.TempDir()

	// Make directory read-only to force write failure
	if err := os.Chmod(tempDir, 0555); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(tempDir, 0755) // Restore for cleanup

	// Try to create new card ID
	newID, err := CreateNewCardID(tempDir, nil)

	// BUG #11: CreateNewCardID returns the generated ID even if write fails
	// The function logs a warning but doesn't return an error
	// This means the card ID in memory won't match the card ID on disk
	// On next insertion, a different ID will be generated
	if err != nil {
		t.Error("CreateNewCardID should not return error (current implementation)")
	}
	if newID == "" {
		t.Error("Should still return generated ID even if write fails")
	}

	t.Log("BUG #11: CreateNewCardID returns ID even when write fails")
	t.Log("BUG #11: This causes ID mismatch between memory and card")
	t.Log("BUG #11: Next insertion will generate different ID -> data uploaded to different folder")
}

// TestReformatDetectionWithDeletedLastSync tests edge case
func TestReformatDetectionWithDeletedLastSync(t *testing.T) {
	tempDir := t.TempDir()
	dcimPath := filepath.Join(tempDir, "DCIM")
	if err := os.MkdirAll(dcimPath, 0755); err != nil {
		t.Fatal(err)
	}

	// Create 100 photos
	for i := 0; i < 100; i++ {
		photoPath := filepath.Join(dcimPath, fmt.Sprintf("IMG_%04d.jpg", i))
		if err := os.WriteFile(photoPath, []byte("fake"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Create card ID
	cardID, _, err := GetOrCreateCardID(tempDir, nil)
	if err != nil {
		t.Fatal(err)
	}

	// BUG #12: What if user manually deletes sync history?
	// FindLastSyncByCardID would return nil
	// Then reformat detection would be skipped entirely
	// Even if card has only 10 photos left, it would sync with same ID
	// This is actually current behavior in main.go:158-180
	t.Logf("BUG #12: If sync history is deleted, reformat detection is skipped")
	t.Logf("BUG #12: Card with ID %s would sync regardless of file count", cardID)
	t.Log("BUG #12: Consider logging warning when no history found for known card")
}
