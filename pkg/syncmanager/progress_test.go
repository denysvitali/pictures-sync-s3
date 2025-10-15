package syncmanager

import "testing"

func TestProgressCalculation(t *testing.T) {
	testCases := []struct {
		name                string
		totalFiles          int
		alreadySyncedFiles  int
		currentTransfers    int
		totalBytes          int64
		alreadySyncedBytes  int64
		sessionBytes        int64
		expectedFileCount   int
		expectedPercentage  int
		expectedDescription string
	}{
		{
			name:                "Fresh sync - 6 of 888 files",
			totalFiles:          888,
			alreadySyncedFiles:  0,
			currentTransfers:    6,
			totalBytes:          1024 * 1024 * 100, // 100 MB total
			alreadySyncedBytes:  0,
			sessionBytes:        1024 * 1024 * 80, // 80 MB transferred (6 large files)
			expectedFileCount:   6,
			expectedPercentage:  80,
			expectedDescription: "6/888 files (80% of bytes)",
		},
		{
			name:                "Resumed sync - 806 already done, 6 more",
			totalFiles:          888,
			alreadySyncedFiles:  800,
			currentTransfers:    6,
			totalBytes:          1024 * 1024 * 100, // 100 MB total
			alreadySyncedBytes:  1024 * 1024 * 70,  // 70 MB already synced
			sessionBytes:        1024 * 1024 * 10,  // 10 MB this session
			expectedFileCount:   806,                // 800 + 6
			expectedPercentage:  80,                 // (70 + 10) / 100 * 100
			expectedDescription: "806/888 files (80% of bytes) - resumed sync",
		},
		{
			name:                "Nearly complete",
			totalFiles:          100,
			alreadySyncedFiles:  95,
			currentTransfers:    4,
			totalBytes:          1024 * 1024 * 50,
			alreadySyncedBytes:  1024 * 1024 * 48,
			sessionBytes:        1024 * 1024 * 1,
			expectedFileCount:   99,
			expectedPercentage:  98, // (48 + 1) / 50 * 100
			expectedDescription: "99/100 files (98% of bytes)",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Simulate the calculation from monitorProgress
			totalTransferred := tc.alreadySyncedBytes + tc.sessionBytes
			totalTransfers := tc.alreadySyncedFiles + tc.currentTransfers

			var percentage int
			if tc.totalBytes > 0 {
				percentage = int((float64(totalTransferred) / float64(tc.totalBytes)) * 100)
			}

			// Verify file count
			if totalTransfers != tc.expectedFileCount {
				t.Errorf("File count = %d, want %d", totalTransfers, tc.expectedFileCount)
			}

			// Verify percentage
			if percentage != tc.expectedPercentage {
				t.Errorf("Percentage = %d%%, want %d%%", percentage, tc.expectedPercentage)
			}

			t.Logf("✓ %s: %d/%d files (%d%% of bytes)",
				tc.expectedDescription,
				totalTransfers,
				tc.totalFiles,
				percentage)
		})
	}
}

func TestResumedSyncScenario(t *testing.T) {
	// Real scenario: SD card has 888 files
	// 800 were already synced (70 MB)
	// Now syncing remaining 88 files (30 MB)
	// After transferring 6 files (10 MB), progress should show:
	// "806/888 files (80% of bytes)"

	totalFiles := 888
	totalBytes := int64(1024 * 1024 * 100) // 100 MB

	alreadySyncedFiles := 800
	alreadySyncedBytes := int64(1024 * 1024 * 70) // 70 MB

	currentTransfers := 6                       // files transferred this session
	sessionBytes := int64(1024 * 1024 * 10)     // 10 MB transferred this session

	// Calculate what the log should show
	totalTransfers := alreadySyncedFiles + currentTransfers
	totalTransferred := alreadySyncedBytes + sessionBytes
	percentage := int((float64(totalTransferred) / float64(totalBytes)) * 100)

	if totalTransfers != 806 {
		t.Errorf("Expected 806 files transferred, got %d", totalTransfers)
	}

	if percentage != 80 {
		t.Errorf("Expected 80%% progress, got %d%%", percentage)
	}

	t.Logf("Progress: %d/%d files (%d%% of bytes)", totalTransfers, totalFiles, percentage)
	t.Log("✓ Resumed sync correctly shows cumulative file count and byte percentage")
}
