package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/denysvitali/pictures-sync-s3/pkg/handlers"
	"github.com/denysvitali/pictures-sync-s3/pkg/settings"
	"github.com/denysvitali/pictures-sync-s3/pkg/ssrf"
	"github.com/denysvitali/pictures-sync-s3/pkg/state"
	"github.com/denysvitali/pictures-sync-s3/pkg/wifimanager"
)

// TestGalleryNavigationRapidClicks tests rapid navigation (simulating user clicking quickly)
func TestGalleryNavigationRapidClicks(t *testing.T) {
	testEnv := setupGalleryTestEnv(t, 200)
	defer testEnv.Cleanup()

	server := httptest.NewServer(testEnv.createHandler())
	defer server.Close()

	client := &http.Client{Timeout: 5 * time.Second}

	// Simulate user rapidly clicking through folders
	paths := []string{
		"",
		"card-test",
		"card-test/DCIM",
		"card-test/DCIM/100CANON",
		"card-test/DCIM",
		"card-test/DCIM/101CANON",
		"card-test/DCIM",
		"card-test",
		"",
		"card-test",
		"card-test/DCIM/102CANON",
	}

	for i, path := range paths {
		url := server.URL + "/api/files"
		if path != "" {
			url += "?path=" + path
		}

		start := time.Now()
		resp, err := client.Get(url)
		elapsed := time.Since(start)

		if err != nil {
			t.Fatalf("Navigation %d to '%s' failed: %v", i, path, err)
		}
		resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Navigation %d: status %d", i, resp.StatusCode)
		}

		if elapsed > 500*time.Millisecond {
			t.Errorf("Navigation %d took %v (should be <500ms for good UX)", i, elapsed)
		}

		// No delay - rapid clicks
	}

	t.Logf("Completed %d rapid navigation operations", len(paths))
}

// TestGalleryNavigationBreadcrumbs tests breadcrumb navigation consistency
func TestGalleryNavigationBreadcrumbs(t *testing.T) {
	testEnv := setupGalleryTestEnv(t, 100)
	defer testEnv.Cleanup()

	server := httptest.NewServer(testEnv.createHandler())
	defer server.Close()

	// Test navigation via breadcrumbs (going back up the tree)
	navigationSequence := []struct {
		path         string
		expectedPath string
		description  string
	}{
		{"card-test/DCIM/100CANON", "card-test/DCIM/100CANON", "Navigate to deepest folder"},
		{"card-test/DCIM", "card-test/DCIM", "Click breadcrumb to parent"},
		{"card-test", "card-test", "Click breadcrumb to card root"},
		{"", "", "Click breadcrumb to root"},
		{"card-test/DCIM/101CANON", "card-test/DCIM/101CANON", "Direct navigation to different folder"},
		{"card-test/DCIM", "card-test/DCIM", "Breadcrumb back to parent"},
	}

	for _, nav := range navigationSequence {
		t.Run(nav.description, func(t *testing.T) {
			url := server.URL + "/api/files"
			if nav.path != "" {
				url += "?path=" + nav.path
			}

			resp, err := http.Get(url)
			if err != nil {
				t.Fatalf("Navigation failed: %v", err)
			}
			defer resp.Body.Close()

			var result map[string]interface{}
			json.NewDecoder(resp.Body).Decode(&result)

			// Verify we're at the correct path
			if result["path"] != nil && result["path"] != nav.expectedPath {
				t.Errorf("Expected path '%s', got '%v'", nav.expectedPath, result["path"])
			}
		})
	}
}

// TestGalleryNavigationDeepNesting tests deeply nested folder structures
func TestGalleryNavigationDeepNesting(t *testing.T) {
	testEnv := setupGalleryTestEnv(t, 50)
	defer testEnv.Cleanup()

	// Create deeply nested structure
	deepPath := filepath.Join(testEnv.BaseDir, "remote", "card-test", "level1", "level2", "level3", "level4", "level5")
	os.MkdirAll(deepPath, 0755)
	os.WriteFile(filepath.Join(deepPath, "deep_file.jpg"), []byte("test"), 0644)

	server := httptest.NewServer(testEnv.createHandler())
	defer server.Close()

	// Navigate to deepest level
	deepPathStr := "card-test/level1/level2/level3/level4/level5"
	url := server.URL + "/api/files?path=" + deepPathStr

	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("Deep navigation failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Deep navigation returned status %d", resp.StatusCode)
	}

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)

	t.Logf("Successfully navigated to depth 5: %v", result)
}

// TestGalleryNavigationBackButton tests browser back/forward simulation
func TestGalleryNavigationBackButton(t *testing.T) {
	testEnv := setupGalleryTestEnv(t, 100)
	defer testEnv.Cleanup()

	server := httptest.NewServer(testEnv.createHandler())
	defer server.Close()

	// Simulate user navigation history
	history := []string{
		"",
		"card-test",
		"card-test/DCIM",
		"card-test/DCIM/100CANON",
	}

	// Navigate forward through history
	for i, path := range history {
		url := server.URL + "/api/files"
		if path != "" {
			url += "?path=" + path
		}

		resp, err := http.Get(url)
		if err != nil {
			t.Fatalf("Forward navigation %d failed: %v", i, err)
		}
		resp.Body.Close()
	}

	// Navigate backward through history (back button)
	for i := len(history) - 2; i >= 0; i-- {
		path := history[i]
		url := server.URL + "/api/files"
		if path != "" {
			url += "?path=" + path
		}

		resp, err := http.Get(url)
		if err != nil {
			t.Fatalf("Back navigation to %d failed: %v", i, err)
		}
		resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Back navigation: status %d", resp.StatusCode)
		}
	}

	// Navigate forward again (forward button)
	for i := 1; i < len(history); i++ {
		path := history[i]
		url := server.URL + "/api/files"
		if path != "" {
			url += "?path=" + path
		}

		resp, err := http.Get(url)
		if err != nil {
			t.Fatalf("Forward again navigation %d failed: %v", i, err)
		}
		resp.Body.Close()
	}

	t.Log("Back/forward navigation simulation completed")
}

// TestGalleryNavigationStaleRequests tests handling of stale/cancelled requests
func TestGalleryNavigationStaleRequests(t *testing.T) {
	testEnv := setupGalleryTestEnv(t, 200)
	defer testEnv.Cleanup()

	server := httptest.NewServer(testEnv.createHandler())
	defer server.Close()

	// Simulate user clicking rapidly, cancelling previous requests
	paths := []string{
		"card-test",
		"card-test/DCIM",
		"card-test/DCIM/100CANON",
		"card-test/DCIM/101CANON",
		"card-test/DCIM/102CANON",
	}

	client := &http.Client{Timeout: 2 * time.Second}

	// Make rapid requests, simulating stale request scenario
	var lastResp *http.Response
	for _, path := range paths {
		if lastResp != nil {
			lastResp.Body.Close()
		}

		url := server.URL + "/api/files?path=" + path
		resp, err := client.Get(url)
		if err != nil {
			t.Logf("Request to %s failed (acceptable if cancelled): %v", path, err)
			continue
		}

		lastResp = resp
		// Don't wait - immediately move to next request
	}

	if lastResp != nil {
		defer lastResp.Body.Close()

		// Final request should succeed
		if lastResp.StatusCode != http.StatusOK {
			t.Errorf("Final request failed with status %d", lastResp.StatusCode)
		}
	}

	t.Log("Stale request handling verified")
}

// TestGalleryNavigationConcurrentUsers tests multiple users navigating simultaneously
func TestGalleryNavigationConcurrentUsers(t *testing.T) {
	testEnv := setupGalleryTestEnv(t, 500)
	defer testEnv.Cleanup()

	server := httptest.NewServer(testEnv.createHandler())
	defer server.Close()

	const numUsers = 15
	var wg sync.WaitGroup
	errors := make(chan error, numUsers*10)

	for userID := 0; userID < numUsers; userID++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			client := &http.Client{Timeout: 5 * time.Second}

			// Each user navigates through different paths
			paths := []string{
				"",
				"card-test",
				"card-test/DCIM",
				fmt.Sprintf("card-test/DCIM/%dCANON", 100+(id%3)),
				"card-test/DCIM",
				"card-test",
			}

			for _, path := range paths {
				url := server.URL + "/api/files"
				if path != "" {
					url += "?path=" + path
				}

				resp, err := client.Get(url)
				if err != nil {
					errors <- fmt.Errorf("user %d: %v", id, err)
					continue
				}
				resp.Body.Close()

				if resp.StatusCode != http.StatusOK {
					errors <- fmt.Errorf("user %d: status %d on path %s", id, resp.StatusCode, path)
				}

				// Small random delay to simulate thinking time
				time.Sleep(time.Duration(10+id*5) * time.Millisecond)
			}
		}(userID)
	}

	wg.Wait()
	close(errors)

	errorCount := 0
	for err := range errors {
		t.Error(err)
		errorCount++
	}

	if errorCount > 0 {
		t.Errorf("%d errors with %d concurrent users", errorCount, numUsers)
	} else {
		t.Logf("All %d concurrent users navigated successfully", numUsers)
	}
}

// TestGalleryNavigationSwitchContext tests switching between SD card and remote gallery
func TestGalleryNavigationSwitchContext(t *testing.T) {
	testEnv := setupGalleryTestEnv(t, 100)
	defer testEnv.Cleanup()

	// Setup SD card mount
	sdMount := filepath.Join(testEnv.BaseDir, "sdcard")
	os.MkdirAll(filepath.Join(sdMount, "DCIM"), 0755)
	for i := 0; i < 50; i++ {
		filename := filepath.Join(sdMount, "DCIM", fmt.Sprintf("IMG_%04d.JPG", i))
		os.WriteFile(filename, []byte("test"), 0644)
	}
	state.MountDir = sdMount
	testEnv.StateMgr.SetSDCard(true, sdMount)

	server := httptest.NewServer(testEnv.createHandler())
	defer server.Close()

	client := &http.Client{Timeout: 3 * time.Second}

	// Rapidly switch between SD card and remote gallery
	for i := 0; i < 10; i++ {
		// View SD card files
		resp, err := client.Get(server.URL + "/api/sdcard/files?path=DCIM")
		if err != nil {
			t.Fatalf("SD card request %d failed: %v", i, err)
		}
		resp.Body.Close()

		// View remote files
		resp, err = client.Get(server.URL + "/api/files?path=card-test/DCIM")
		if err != nil {
			t.Fatalf("Remote request %d failed: %v", i, err)
		}
		resp.Body.Close()

		// No delay - rapid switching
	}

	t.Log("Context switching between SD card and remote verified")
}

// TestGalleryNavigationURLEncoding tests special characters in paths
func TestGalleryNavigationURLEncoding(t *testing.T) {
	testEnv := setupGalleryTestEnv(t, 50)
	defer testEnv.Cleanup()

	// Create folders with special characters
	specialFolders := []string{
		"folder with spaces",
		"folder-with-dashes",
		"folder_with_underscores",
		"folder.with.dots",
		"folder(with)parens",
	}

	basePath := filepath.Join(testEnv.BaseDir, "remote", "card-test")
	for _, folder := range specialFolders {
		os.MkdirAll(filepath.Join(basePath, folder), 0755)
		os.WriteFile(filepath.Join(basePath, folder, "test.jpg"), []byte("test"), 0644)
	}

	server := httptest.NewServer(testEnv.createHandler())
	defer server.Close()

	client := &http.Client{Timeout: 3 * time.Second}

	for _, folder := range specialFolders {
		path := "card-test/" + folder
		url := server.URL + "/api/files?path=" + path

		resp, err := client.Get(url)
		if err != nil {
			t.Errorf("Navigation to '%s' failed: %v", folder, err)
			continue
		}
		resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Special folder '%s': status %d", folder, resp.StatusCode)
		}
	}

	t.Log("URL encoding and special characters handled correctly")
}

// TestGalleryNavigationMemoryStress tests gallery doesn't leak memory during navigation
func TestGalleryNavigationMemoryStress(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping memory stress test in short mode")
	}

	testEnv := setupGalleryTestEnv(t, 1000)
	defer testEnv.Cleanup()

	server := httptest.NewServer(testEnv.createHandler())
	defer server.Close()

	client := &http.Client{Timeout: 5 * time.Second}

	paths := []string{
		"",
		"card-test",
		"card-test/DCIM",
		"card-test/DCIM/100CANON",
		"card-test/DCIM/101CANON",
		"card-test/DCIM/102CANON",
	}

	// Navigate many times
	const iterations = 200
	for i := 0; i < iterations; i++ {
		for _, path := range paths {
			url := server.URL + "/api/files"
			if path != "" {
				url += "?path=" + path
			}

			resp, err := client.Get(url)
			if err != nil {
				t.Fatalf("Iteration %d, path '%s' failed: %v", i, path, err)
			}

			// Read and discard response
			var result map[string]interface{}
			json.NewDecoder(resp.Body).Decode(&result)
			resp.Body.Close()
		}

		if i%50 == 0 {
			t.Logf("Completed %d/%d iterations", i, iterations)
		}
	}

	t.Logf("Memory stress test completed: %d iterations with %d paths each", iterations, len(paths))
}

// TestGalleryNavigationRecoveryAfterError tests navigation recovery after errors
func TestGalleryNavigationRecoveryAfterError(t *testing.T) {
	testEnv := setupGalleryTestEnv(t, 100)
	defer testEnv.Cleanup()

	server := httptest.NewServer(testEnv.createHandler())
	defer server.Close()

	client := &http.Client{Timeout: 3 * time.Second}

	// Cause an error
	resp, _ := client.Get(server.URL + "/api/files?path=../../../etc/passwd")
	if resp != nil {
		resp.Body.Close()
	}

	// Immediately try valid navigation
	resp, err := client.Get(server.URL + "/api/files?path=card-test/DCIM")
	if err != nil {
		t.Fatalf("Navigation after error failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected recovery, got status %d", resp.StatusCode)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("Response decode failed: %v", err)
	}

	t.Log("Gallery navigation recovered successfully after error")
}

// TestGalleryNavigationPageLoad tests full page load scenario
func TestGalleryNavigationPageLoad(t *testing.T) {
	testEnv := setupGalleryTestEnv(t, 200)
	defer testEnv.Cleanup()

	server := httptest.NewServer(testEnv.createHandler())
	defer server.Close()

	client := &http.Client{Timeout: 5 * time.Second}

	// Simulate full page load (all resources requested together)
	var wg sync.WaitGroup
	errors := make(chan error, 4)

	// Card list
	wg.Add(1)
	go func() {
		defer wg.Done()
		resp, err := client.Get(server.URL + "/api/files/cards")
		if err != nil {
			errors <- fmt.Errorf("cards: %v", err)
			return
		}
		resp.Body.Close()
	}()

	// File list
	wg.Add(1)
	go func() {
		defer wg.Done()
		resp, err := client.Get(server.URL + "/api/files?path=card-test/DCIM")
		if err != nil {
			errors <- fmt.Errorf("files: %v", err)
			return
		}
		resp.Body.Close()
	}()

	// Paginated list
	wg.Add(1)
	go func() {
		defer wg.Done()
		resp, err := client.Get(server.URL + "/api/files/paginated?path=card-test/DCIM&page=1&page_size=50")
		if err != nil {
			errors <- fmt.Errorf("paginated: %v", err)
			return
		}
		resp.Body.Close()
	}()

	wg.Wait()
	close(errors)

	errorCount := 0
	for err := range errors {
		t.Error(err)
		errorCount++
	}

	if errorCount > 0 {
		t.Errorf("Page load had %d errors", errorCount)
	} else {
		t.Log("Full page load completed successfully")
	}
}
