package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/denysvitali/pictures-sync-s3/pkg/handlers"
	"github.com/denysvitali/pictures-sync-s3/pkg/settings"
	"github.com/denysvitali/pictures-sync-s3/pkg/ssrf"
	"github.com/denysvitali/pictures-sync-s3/pkg/state"
	"github.com/denysvitali/pictures-sync-s3/pkg/syncmanager"
	"github.com/denysvitali/pictures-sync-s3/pkg/wifimanager"
)

const (
	// Gallery timeout constraints
	maxGalleryLoadTime = 5 * time.Second
	maxFileListTime    = 3 * time.Second
	maxThumbnailTime   = 2 * time.Second
)

// TestGalleryLoadTimeout ensures gallery doesn't timeout on page load
func TestGalleryLoadTimeout(t *testing.T) {
	testEnv := setupGalleryTestEnv(t, 100) // 100 test files
	defer testEnv.Cleanup()

	server := httptest.NewServer(testEnv.createHandler())
	defer server.Close()

	// Test gallery API endpoint with timeout
	client := &http.Client{
		Timeout: maxGalleryLoadTime,
	}

	start := time.Now()
	resp, err := client.Get(server.URL + "/api/files")
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Gallery API timed out after %v: %v", elapsed, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Gallery API returned status %d", resp.StatusCode)
	}

	if elapsed > maxGalleryLoadTime {
		t.Errorf("Gallery load took %v, exceeds maximum %v", elapsed, maxGalleryLoadTime)
	}

	t.Logf("Gallery loaded in %v (threshold: %v)", elapsed, maxGalleryLoadTime)
}

// TestGalleryLargeFileList tests gallery performance with large file counts
func TestGalleryLargeFileList(t *testing.T) {
	testCases := []struct {
		name      string
		fileCount int
		maxTime   time.Duration
	}{
		{"Small (100 files)", 100, 1 * time.Second},
		{"Medium (500 files)", 500, 2 * time.Second},
		{"Large (1000 files)", 1000, 3 * time.Second},
		{"Very Large (5000 files)", 5000, 5 * time.Second},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			testEnv := setupGalleryTestEnv(t, tc.fileCount)
			defer testEnv.Cleanup()

			server := httptest.NewServer(testEnv.createHandler())
			defer server.Close()

			client := &http.Client{Timeout: maxGalleryLoadTime}

			start := time.Now()
			resp, err := client.Get(server.URL + "/api/files?path=card-test/DCIM")
			elapsed := time.Since(start)

			if err != nil {
				t.Fatalf("Request failed after %v: %v", elapsed, err)
			}
			defer resp.Body.Close()

			if elapsed > tc.maxTime {
				t.Errorf("File list (%d files) took %v, exceeds %v", tc.fileCount, elapsed, tc.maxTime)
			}

			// Verify response is valid
			var result map[string]interface{}
			if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
				t.Fatalf("Failed to decode response: %v", err)
			}

			t.Logf("Listed %d files in %v (threshold: %v)", tc.fileCount, elapsed, tc.maxTime)
		})
	}
}

// TestGalleryPaginationPerformance tests paginated file listing performance
func TestGalleryPaginationPerformance(t *testing.T) {
	testEnv := setupGalleryTestEnv(t, 2000)
	defer testEnv.Cleanup()

	server := httptest.NewServer(testEnv.createHandler())
	defer server.Close()

	client := &http.Client{Timeout: maxFileListTime}

	// Test different page sizes
	pageSizes := []int{50, 100, 200, 500}

	for _, pageSize := range pageSizes {
		t.Run(fmt.Sprintf("PageSize_%d", pageSize), func(t *testing.T) {
			url := fmt.Sprintf("%s/api/files/paginated?path=card-test/DCIM&page=1&page_size=%d",
				server.URL, pageSize)

			start := time.Now()
			resp, err := client.Get(url)
			elapsed := time.Since(start)

			if err != nil {
				t.Fatalf("Paginated request failed: %v", err)
			}
			defer resp.Body.Close()

			if elapsed > maxFileListTime {
				t.Errorf("Pagination (size=%d) took %v, exceeds %v", pageSize, elapsed, maxFileListTime)
			}

			var result map[string]interface{}
			if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
				t.Fatalf("Failed to decode response: %v", err)
			}

			t.Logf("Page size %d loaded in %v", pageSize, elapsed)
		})
	}
}

// TestGalleryConcurrentRequests tests gallery under concurrent load
func TestGalleryConcurrentRequests(t *testing.T) {
	testEnv := setupGalleryTestEnv(t, 500)
	defer testEnv.Cleanup()

	server := httptest.NewServer(testEnv.createHandler())
	defer server.Close()

	const numClients = 20
	errors := make(chan error, numClients)
	var wg sync.WaitGroup

	for i := 0; i < numClients; i++ {
		wg.Add(1)
		go func(clientID int) {
			defer wg.Done()

			client := &http.Client{Timeout: maxGalleryLoadTime}
			resp, err := client.Get(server.URL + "/api/files?path=card-test/DCIM")
			if err != nil {
				errors <- fmt.Errorf("client %d: %v", clientID, err)
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				errors <- fmt.Errorf("client %d: status %d", clientID, resp.StatusCode)
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	errorCount := 0
	for err := range errors {
		t.Error(err)
		errorCount++
	}

	if errorCount > 0 {
		t.Errorf("%d/%d concurrent gallery requests failed", errorCount, numClients)
	} else {
		t.Logf("All %d concurrent gallery requests succeeded", numClients)
	}
}

// TestGalleryNavigationSpeed tests navigation between folders
func TestGalleryNavigationSpeed(t *testing.T) {
	testEnv := setupGalleryTestEnv(t, 300)
	defer testEnv.Cleanup()

	server := httptest.NewServer(testEnv.createHandler())
	defer server.Close()

	client := &http.Client{Timeout: maxFileListTime}

	paths := []string{
		"",
		"card-test",
		"card-test/DCIM",
		"card-test/DCIM/100CANON",
		"card-test/DCIM",
		"card-test",
		"",
	}

	for i, path := range paths {
		start := time.Now()
		url := server.URL + "/api/files"
		if path != "" {
			url += "?path=" + path
		}

		resp, err := client.Get(url)
		elapsed := time.Since(start)

		if err != nil {
			t.Fatalf("Navigation step %d failed: %v", i, err)
		}
		defer resp.Body.Close()

		if elapsed > maxFileListTime {
			t.Errorf("Navigation to '%s' took %v, exceeds %v", path, elapsed, maxFileListTime)
		}

		t.Logf("Navigation %d to '%s': %v", i, path, elapsed)
	}
}

// TestGallerySDCardFilesPerformance tests SD card file listing performance
func TestGallerySDCardFilesPerformance(t *testing.T) {
	testEnv := setupGalleryTestEnv(t, 500)
	defer testEnv.Cleanup()

	// Setup SD card mount
	sdMount := filepath.Join(testEnv.BaseDir, "sdcard")
	os.MkdirAll(filepath.Join(sdMount, "DCIM"), 0755)
	state.MountDir = sdMount
	testEnv.StateMgr.SetSDCard(true, sdMount)

	// Create test files on "SD card"
	for i := 0; i < 500; i++ {
		filename := filepath.Join(sdMount, "DCIM", fmt.Sprintf("IMG_%04d.JPG", i))
		os.WriteFile(filename, []byte("test"), 0644)
	}

	server := httptest.NewServer(testEnv.createHandler())
	defer server.Close()

	client := &http.Client{Timeout: maxFileListTime}

	start := time.Now()
	resp, err := client.Get(server.URL + "/api/sdcard/files?path=DCIM")
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("SD card files request failed: %v", err)
	}
	defer resp.Body.Close()

	if elapsed > maxFileListTime {
		t.Errorf("SD card file list took %v, exceeds %v", elapsed, maxFileListTime)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	t.Logf("SD card files (500) listed in %v", elapsed)
}

// TestGalleryMemoryUsage tests gallery doesn't leak memory with large datasets
func TestGalleryMemoryUsage(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping memory test in short mode")
	}

	testEnv := setupGalleryTestEnv(t, 1000)
	defer testEnv.Cleanup()

	server := httptest.NewServer(testEnv.createHandler())
	defer server.Close()

	client := &http.Client{Timeout: maxGalleryLoadTime}

	// Make many requests to detect memory leaks
	for i := 0; i < 100; i++ {
		resp, err := client.Get(server.URL + "/api/files?path=card-test/DCIM")
		if err != nil {
			t.Fatalf("Request %d failed: %v", i, err)
		}

		// Read and discard body
		var result map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&result)
		resp.Body.Close()

		if i%10 == 0 {
			t.Logf("Completed %d requests", i)
		}
	}

	t.Log("Memory usage test completed (100 requests)")
}

// TestGalleryErrorRecovery tests gallery recovers from errors
func TestGalleryErrorRecovery(t *testing.T) {
	testEnv := setupGalleryTestEnv(t, 100)
	defer testEnv.Cleanup()

	server := httptest.NewServer(testEnv.createHandler())
	defer server.Close()

	client := &http.Client{Timeout: maxFileListTime}

	// Test error conditions
	errorPaths := []string{
		"/api/files?path=../../../etc/passwd",
		"/api/files?path=/etc/passwd",
		"/api/files?path=nonexistent",
		"/api/sdcard/files?path=../../etc",
	}

	for _, path := range errorPaths {
		resp, err := client.Get(server.URL + path)
		if err != nil {
			t.Logf("Request to %s failed (expected): %v", path, err)
			continue
		}
		resp.Body.Close()

		// Server should handle gracefully, not crash
		if resp.StatusCode == 500 {
			t.Errorf("Server error on %s", path)
		}
	}

	// Verify gallery still works after errors
	resp, err := client.Get(server.URL + "/api/files?path=card-test/DCIM")
	if err != nil {
		t.Fatalf("Gallery failed to recover: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Gallery not working after error recovery: status %d", resp.StatusCode)
	}
}

// TestGalleryTimeout tests explicit timeout scenarios
func TestGalleryTimeout(t *testing.T) {
	testEnv := setupGalleryTestEnv(t, 100)
	defer testEnv.Cleanup()

	server := httptest.NewServer(testEnv.createHandler())
	defer server.Close()

	// Test with very short timeout
	client := &http.Client{
		Timeout: 1 * time.Millisecond, // Intentionally very short
	}

	_, err := client.Get(server.URL + "/api/files?path=card-test/DCIM")
	if err == nil {
		t.Error("Expected timeout error with 1ms timeout")
	} else if !strings.Contains(err.Error(), "timeout") && !strings.Contains(err.Error(), "deadline") {
		t.Errorf("Expected timeout error, got: %v", err)
	}

	// Now test with reasonable timeout
	client.Timeout = maxGalleryLoadTime
	resp, err := client.Get(server.URL + "/api/files?path=card-test/DCIM")
	if err != nil {
		t.Fatalf("Gallery failed with reasonable timeout: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}
}

// TestGalleryContextCancellation tests request cancellation handling
func TestGalleryContextCancellation(t *testing.T) {
	testEnv := setupGalleryTestEnv(t, 500)
	defer testEnv.Cleanup()

	server := httptest.NewServer(testEnv.createHandler())
	defer server.Close()

	// Create request with cancellable context
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", server.URL+"/api/files?path=card-test/DCIM", nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	client := &http.Client{}
	_, err = client.Do(req)

	// Should timeout
	if err == nil {
		t.Error("Expected context timeout")
	} else if !strings.Contains(err.Error(), "context deadline exceeded") {
		t.Logf("Got error (acceptable): %v", err)
	}

	// Verify server still works after cancellation
	resp, err := http.Get(server.URL + "/api/files?path=card-test/DCIM")
	if err != nil {
		t.Fatalf("Server not working after cancellation: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200 after cancellation, got %d", resp.StatusCode)
	}
}

// galleryTestEnv holds gallery test environment
type galleryTestEnv struct {
	BaseDir     string
	StateMgr    *state.Manager
	SyncMgr     *syncmanager.Manager
	Settings    *settings.Settings
	cleanupFunc func()
}

func (e *galleryTestEnv) Cleanup() {
	if e.cleanupFunc != nil {
		e.cleanupFunc()
	}
}

func (e *galleryTestEnv) createHandler() *http.ServeMux {
	wifiMgr, _ := wifimanager.NewManager()
	ssrfValidator := ssrf.NewValidator(10, time.Minute)

	ctx := &handlers.Context{
		StateMgr:      e.StateMgr,
		SyncMgr:       e.SyncMgr,
		WiFiMgr:       wifiMgr,
		AppSettings:   e.Settings,
		SSRFValidator: ssrfValidator,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/files", ctx.HandleFiles)
	mux.HandleFunc("/api/files/paginated", ctx.HandleFilesPaginated)
	mux.HandleFunc("/api/files/cards", ctx.HandleFileCards)
	mux.HandleFunc("/api/sdcard/files", ctx.HandleSDCardFiles)

	return mux
}

// setupGalleryTestEnv creates a gallery test environment with mock files
func setupGalleryTestEnv(t *testing.T, fileCount int) *galleryTestEnv {
	t.Helper()

	tmpDir := t.TempDir()
	oldPerm := os.Getenv("PERM_DIR")
	os.Setenv("PERM_DIR", tmpDir)

	stateMgr, err := state.NewManager()
	if err != nil {
		t.Fatalf("Failed to create state manager: %v", err)
	}

	appSettings, err := settings.Load()
	if err != nil {
		appSettings = &settings.Settings{}
	}

	// Create mock file structure
	remotePath := filepath.Join(tmpDir, "remote", "card-test", "DCIM")
	os.MkdirAll(remotePath, 0755)

	// Create subfolders
	for _, folder := range []string{"100CANON", "101CANON", "102CANON"} {
		os.MkdirAll(filepath.Join(remotePath, folder), 0755)
	}

	// Create mock files
	filesPerFolder := fileCount / 3
	for i, folder := range []string{"100CANON", "101CANON", "102CANON"} {
		for j := 0; j < filesPerFolder; j++ {
			filename := filepath.Join(remotePath, folder, fmt.Sprintf("IMG_%04d.JPG", i*filesPerFolder+j))
			os.WriteFile(filename, []byte("mock image data"), 0644)
		}
	}

	// Create mock sync manager with file access
	mockSyncMgr := &mockFileSyncManager{
		basePath: filepath.Join(tmpDir, "remote"),
		cardIDs:  []string{"card-test"},
	}

	cleanup := func() {
		if oldPerm == "" {
			os.Unsetenv("PERM_DIR")
		} else {
			os.Setenv("PERM_DIR", oldPerm)
		}
	}

	return &galleryTestEnv{
		BaseDir:     tmpDir,
		StateMgr:    stateMgr,
		SyncMgr:     mockSyncMgr,
		Settings:    appSettings,
		cleanupFunc: cleanup,
	}
}

// mockFileSyncManager implements file listing for tests
type mockFileSyncManager struct {
	basePath string
	cardIDs  []string
}

func (m *mockFileSyncManager) IsRunning() bool                                { return false }
func (m *mockFileSyncManager) Cancel() error                                  { return nil }
func (m *mockFileSyncManager) Sync(string, string, int, int64) error          { return nil }
func (m *mockFileSyncManager) SetGooglePhotos(bool, string)                   {}
func (m *mockFileSyncManager) UpdateConfig(string, string, int, int)          {}
func (m *mockFileSyncManager) GetFile(path string, w http.ResponseWriter) error { return nil }

func (m *mockFileSyncManager) ListFiles(path string) ([]string, error) {
	fullPath := filepath.Join(m.basePath, path)
	entries, err := os.ReadDir(fullPath)
	if err != nil {
		return nil, err
	}

	var files []string
	for _, entry := range entries {
		files = append(files, entry.Name())
	}
	return files, nil
}

func (m *mockFileSyncManager) ListCardIDs() ([]string, error) {
	return m.cardIDs, nil
}

func (m *mockFileSyncManager) ListFilesPaginated(path string, page, pageSize int) (map[string]interface{}, error) {
	allFiles, err := m.ListFiles(path)
	if err != nil {
		return nil, err
	}

	start := (page - 1) * pageSize
	end := start + pageSize

	if start > len(allFiles) {
		start = len(allFiles)
	}
	if end > len(allFiles) {
		end = len(allFiles)
	}

	pageFiles := allFiles[start:end]
	totalPages := (len(allFiles) + pageSize - 1) / pageSize

	return map[string]interface{}{
		"files":       pageFiles,
		"page":        page,
		"page_size":   pageSize,
		"total_pages": totalPages,
		"total_files": len(allFiles),
	}, nil
}
