package handlers

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/denysvitali/pictures-sync-s3/pkg/captiveportal"
	"github.com/denysvitali/pictures-sync-s3/pkg/settings"
	"github.com/denysvitali/pictures-sync-s3/pkg/ssrf"
	"github.com/denysvitali/pictures-sync-s3/pkg/state"
	"github.com/denysvitali/pictures-sync-s3/pkg/syncmanager"
	"github.com/denysvitali/pictures-sync-s3/pkg/wifimanager"
)

// mockSyncManager implements syncmanager.Manager interface for testing
type mockSyncManager struct {
	isRunning    bool
	cancelCalled bool
	syncError    error
	files        []syncmanager.FileInfo
	cardIDs      []syncmanager.FileInfo
}

func (m *mockSyncManager) IsRunning() bool                       { return m.isRunning }
func (m *mockSyncManager) Cancel() error                         { m.cancelCalled = true; return nil }
func (m *mockSyncManager) Sync(string, string, int, int64) error { return m.syncError }
func (m *mockSyncManager) SetRemote(string, string)              {}
func (m *mockSyncManager) SetGooglePhotos(bool, string)          {}
func (m *mockSyncManager) ListRemotes() ([]string, error)        { return []string{"local"}, nil }
func (m *mockSyncManager) TestConnection() error                 { return nil }
func (m *mockSyncManager) ListFiles(path string) ([]syncmanager.FileInfo, error) {
	return m.files, nil
}
func (m *mockSyncManager) ListCardIDs() ([]syncmanager.FileInfo, error) { return m.cardIDs, nil }
func (m *mockSyncManager) GetFile(path string, w io.Writer) error       { return nil }
func (m *mockSyncManager) ListFilesPaginated(path string, page, pageSize int) (*syncmanager.FileListResult, error) {
	return &syncmanager.FileListResult{
		Files:      m.files,
		Page:       page,
		PageSize:   pageSize,
		TotalPages: 1,
		Total:      len(m.files),
	}, nil
}

// mockWiFiManager implements wifimanager.WiFiManager for testing
type mockWiFiManager struct{}

func (m *mockWiFiManager) GetNetworks() []wifimanager.Network              { return nil }
func (m *mockWiFiManager) AddNetwork(ssid, password string) error          { return nil }
func (m *mockWiFiManager) RemoveNetwork(ssid string) error                 { return nil }
func (m *mockWiFiManager) ReorderNetworks(ssids []string) error            { return nil }
func (m *mockWiFiManager) ScanNetworks() ([]wifimanager.ScanResult, error) { return nil, nil }
func (m *mockWiFiManager) ListNetworks() ([]wifimanager.Network, error)    { return nil, nil }
func (m *mockWiFiManager) GetCurrentConnection() (*wifimanager.ConnectionInfo, error) {
	return &wifimanager.ConnectionInfo{SSID: "TestNetwork", Signal: -40}, nil
}

// setupTestContext creates a test context with mock dependencies
func setupTestContext(t *testing.T) (*Context, func()) {
	t.Helper()

	tempDir := t.TempDir()
	oldPerm := os.Getenv("PERM_DIR")
	os.Setenv("PERM_DIR", tempDir)

	stateMgr, err := state.NewManager()
	if err != nil {
		t.Fatalf("Failed to create state manager: %v", err)
	}

	appSettings, err := settings.Load()
	if err != nil {
		t.Fatalf("Failed to load settings: %v", err)
	}

	mockSync := &mockSyncManager{
		files: []syncmanager.FileInfo{
			{Name: "file1.jpg", Path: "file1.jpg"},
			{Name: "file2.jpg", Path: "file2.jpg"},
		},
		cardIDs: []syncmanager.FileInfo{
			{Name: "card-123", Path: "card-123", IsDir: true},
			{Name: "card-456", Path: "card-456", IsDir: true},
		},
	}

	ctx := &Context{
		StateMgr:      stateMgr,
		SyncMgr:       mockSync,
		WiFiMgr:       &mockWiFiManager{},
		AppSettings:   appSettings,
		SSRFValidator: ssrf.NewValidator(100, time.Minute),
		CaptivePortal: captiveportal.NewAuthenticator(func() (string, error) {
			return "TestNetwork", nil
		}),
	}

	cleanup := func() {
		if oldPerm == "" {
			os.Unsetenv("PERM_DIR")
		} else {
			os.Setenv("PERM_DIR", oldPerm)
		}
	}

	return ctx, cleanup
}

// TestHandleStatus_GET tests status endpoint
func TestHandleStatus_GET(t *testing.T) {
	ctx, cleanup := setupTestContext(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	w := httptest.NewRecorder()

	ctx.HandleStatus(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var response map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if response["status"] == nil {
		t.Error("Expected status field in response")
	}
}

// TestHandleStatus_WrongMethod tests method validation
func TestHandleStatus_WrongMethod(t *testing.T) {
	ctx, cleanup := setupTestContext(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/api/status", nil)
	w := httptest.NewRecorder()

	ctx.HandleStatus(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected status 405, got %d", w.Code)
	}
}

// TestHandleHistory_GET tests history endpoint
func TestHandleHistory_GET(t *testing.T) {
	ctx, cleanup := setupTestContext(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/history", nil)
	w := httptest.NewRecorder()

	ctx.HandleHistory(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var response []interface{}
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
}

// TestHandleDevices_GET tests device listing
func TestHandleDevices_GET(t *testing.T) {
	ctx, cleanup := setupTestContext(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/devices", nil)
	w := httptest.NewRecorder()

	ctx.HandleDevices(w, req)

	// Should succeed even if no devices found
	if w.Code != http.StatusOK && w.Code != http.StatusInternalServerError {
		t.Errorf("Expected status 200 or 500, got %d", w.Code)
	}
}

// TestHandleDeviceSelect_POST tests device selection
func TestHandleDeviceSelect_POST(t *testing.T) {
	ctx, cleanup := setupTestContext(t)
	defer cleanup()

	body := map[string]string{"device_path": "/dev/sda1"}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/devices/select", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	ctx.HandleDeviceSelect(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
}

// TestHandleDeviceSelect_MissingDevicePath tests validation
func TestHandleDeviceSelect_MissingDevicePath(t *testing.T) {
	ctx, cleanup := setupTestContext(t)
	defer cleanup()

	body := map[string]string{}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/devices/select", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	ctx.HandleDeviceSelect(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", w.Code)
	}
}

// TestHandleDeviceSelect_InvalidJSON tests JSON parsing
func TestHandleDeviceSelect_InvalidJSON(t *testing.T) {
	ctx, cleanup := setupTestContext(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/api/devices/select", bytes.NewReader([]byte("invalid json")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	ctx.HandleDeviceSelect(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", w.Code)
	}
}

// TestHandleSyncStart_NoCard tests sync start without mounted card
func TestHandleSyncStart_NoCard(t *testing.T) {
	ctx, cleanup := setupTestContext(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/api/sync/start", nil)
	w := httptest.NewRecorder()

	ctx.HandleSyncStart(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", w.Code)
	}
}

// TestHandleSyncStart_AlreadyRunning tests sync start with running sync
func TestHandleSyncStart_AlreadyRunning(t *testing.T) {
	ctx, cleanup := setupTestContext(t)
	defer cleanup()

	// Mock sync manager as running
	mockSync := ctx.SyncMgr.(*mockSyncManager)
	mockSync.isRunning = true

	req := httptest.NewRequest(http.MethodPost, "/api/sync/start", nil)
	w := httptest.NewRecorder()

	ctx.HandleSyncStart(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("Expected status 409, got %d", w.Code)
	}
}

// TestHandleSyncCancel_NoSync tests cancel without running sync
func TestHandleSyncCancel_NoSync(t *testing.T) {
	ctx, cleanup := setupTestContext(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/api/sync/cancel", nil)
	w := httptest.NewRecorder()

	ctx.HandleSyncCancel(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", w.Code)
	}
}

// TestHandleSyncCancel_Success tests successful sync cancellation
func TestHandleSyncCancel_Success(t *testing.T) {
	ctx, cleanup := setupTestContext(t)
	defer cleanup()

	mockSync := ctx.SyncMgr.(*mockSyncManager)
	mockSync.isRunning = true

	req := httptest.NewRequest(http.MethodPost, "/api/sync/cancel", nil)
	w := httptest.NewRecorder()

	ctx.HandleSyncCancel(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	if !mockSync.cancelCalled {
		t.Error("Expected Cancel() to be called on sync manager")
	}
}

// TestHandleFileCards_GET tests card listing
func TestHandleFileCards_GET(t *testing.T) {
	ctx, cleanup := setupTestContext(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/files/cards", nil)
	w := httptest.NewRecorder()

	ctx.HandleFileCards(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var response map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	cards, ok := response["cards"].([]interface{})
	if !ok {
		t.Error("Expected cards field in response")
	}
	if len(cards) != 2 {
		t.Errorf("Expected 2 cards, got %d", len(cards))
	}
}

// TestHandleFiles_GET tests file listing
func TestHandleFiles_GET(t *testing.T) {
	ctx, cleanup := setupTestContext(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/files?path=/photos", nil)
	w := httptest.NewRecorder()

	ctx.HandleFiles(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var response map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if response["files"] == nil {
		t.Error("Expected files field in response")
	}
}

// TestHandleFilesPaginated_GET tests paginated file listing
func TestHandleFilesPaginated_GET(t *testing.T) {
	ctx, cleanup := setupTestContext(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/files/paginated?path=/photos&page=1&page_size=50", nil)
	w := httptest.NewRecorder()

	ctx.HandleFilesPaginated(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var response map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if response["page"] == nil {
		t.Error("Expected page field in response")
	}
}

// TestHandleFilesPaginated_InvalidPage tests invalid page parameter
func TestHandleFilesPaginated_InvalidPage(t *testing.T) {
	ctx, cleanup := setupTestContext(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/files/paginated?page=-1", nil)
	w := httptest.NewRecorder()

	ctx.HandleFilesPaginated(w, req)

	// Should default to page 1
	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200 with default page, got %d", w.Code)
	}
}

// TestHandleFileView_MissingPath tests file view without path
func TestHandleFileView_MissingPath(t *testing.T) {
	ctx, cleanup := setupTestContext(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/files/view", nil)
	w := httptest.NewRecorder()

	ctx.HandleFileView(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", w.Code)
	}
}

// TestHandleFileView_UnsupportedType tests unsupported file types
func TestHandleFileView_UnsupportedType(t *testing.T) {
	ctx, cleanup := setupTestContext(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/files/view?path=/file.txt", nil)
	w := httptest.NewRecorder()

	ctx.HandleFileView(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", w.Code)
	}
}

// TestHandleThumbnail_MissingPath tests thumbnail without path
func TestHandleThumbnail_MissingPath(t *testing.T) {
	ctx, cleanup := setupTestContext(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/thumbnail", nil)
	w := httptest.NewRecorder()

	ctx.HandleThumbnail(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", w.Code)
	}
}

// TestHandleThumbnail_PathTraversal tests path traversal protection
func TestHandleThumbnail_PathTraversal(t *testing.T) {
	ctx, cleanup := setupTestContext(t)
	defer cleanup()

	testCases := []struct {
		name string
		path string
	}{
		{"Parent directory", "../../../etc/passwd"},
		{"Absolute path", "/etc/passwd"},
		{"Mixed traversal", "DCIM/../../etc/passwd"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/thumbnail?path="+tc.path, nil)
			w := httptest.NewRecorder()

			ctx.HandleThumbnail(w, req)

			if w.Code != http.StatusForbidden && w.Code != http.StatusInternalServerError {
				t.Errorf("Expected status 403 or 500, got %d for path %s", w.Code, tc.path)
			}
		})
	}
}

// TestHandleSDCardFiles_NoCard tests SD card file listing without mounted card
func TestHandleSDCardFiles_NoCard(t *testing.T) {
	ctx, cleanup := setupTestContext(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/sdcard/files", nil)
	w := httptest.NewRecorder()

	ctx.HandleSDCardFiles(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var response map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if response["error"] == nil {
		t.Error("Expected error field for no mounted card")
	}
}

// TestHandleSDCardFiles_PathTraversal tests path traversal protection
func TestHandleSDCardFiles_PathTraversal(t *testing.T) {
	ctx, cleanup := setupTestContext(t)
	defer cleanup()

	// Set SD card as mounted
	ctx.StateMgr.SetSDCard(true, "/tmp/sdcard")

	testCases := []string{
		"../../etc/passwd",
		"/etc/passwd",
		"../../../root/.ssh/id_rsa",
	}

	for _, path := range testCases {
		req := httptest.NewRequest(http.MethodGet, "/api/sdcard/files?path="+path, nil)
		w := httptest.NewRecorder()

		ctx.HandleSDCardFiles(w, req)

		var response map[string]interface{}
		json.NewDecoder(w.Body).Decode(&response)

		if response["error"] == nil || response["error"] != "access denied" {
			t.Errorf("Expected access denied error for path %s", path)
		}
	}
}

// TestHandleSDCardPreview_PathTraversal tests preview path security
func TestHandleSDCardPreview_PathTraversal(t *testing.T) {
	ctx, cleanup := setupTestContext(t)
	defer cleanup()

	testCases := []string{
		"../../../etc/passwd",
		"/etc/passwd",
		"DCIM/../../root/.ssh/id_rsa",
	}

	for _, path := range testCases {
		req := httptest.NewRequest(http.MethodGet, "/api/sdcard/preview?path="+path, nil)
		w := httptest.NewRecorder()

		ctx.HandleSDCardPreview(w, req)

		if w.Code != http.StatusForbidden {
			t.Errorf("Expected status 403 for path %s, got %d", path, w.Code)
		}
	}
}

// TestJSONResponse tests JSON response helper
func TestJSONResponse(t *testing.T) {
	w := httptest.NewRecorder()
	data := map[string]string{"key": "value"}

	JSONResponse(w, data)

	if w.Header().Get("Content-Type") != "application/json" {
		t.Error("Expected Content-Type to be application/json")
	}

	var response map[string]string
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if response["key"] != "value" {
		t.Errorf("Expected key=value, got %s", response["key"])
	}
}

// TestExtractEXIF tests EXIF extraction function
func TestExtractEXIF(t *testing.T) {
	tempDir := t.TempDir()

	// Create a test JPEG file (minimal valid JPEG)
	testFile := filepath.Join(tempDir, "test.jpg")
	// This is a minimal valid JPEG with no EXIF
	jpegData := []byte{
		0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 0x4A, 0x46,
		0x49, 0x46, 0x00, 0x01, 0x01, 0x00, 0x00, 0x01,
		0x00, 0x01, 0x00, 0x00, 0xFF, 0xD9,
	}
	if err := os.WriteFile(testFile, jpegData, 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	result := extractEXIF(testFile)
	// Should return nil or empty map for file without EXIF
	if result != nil && len(result) > 0 {
		t.Logf("Note: EXIF data found in minimal JPEG (unexpected but not an error)")
	}

	// Test with non-existent file
	result = extractEXIF(filepath.Join(tempDir, "nonexistent.jpg"))
	if result != nil {
		t.Error("Expected nil for non-existent file")
	}
}

// BenchmarkHandleStatus measures status endpoint performance
func BenchmarkHandleStatus(b *testing.B) {
	tempDir := b.TempDir()
	os.Setenv("PERM_DIR", tempDir)
	defer os.Unsetenv("PERM_DIR")

	stateMgr, _ := state.NewManager()
	appSettings, _ := settings.Load()

	ctx := &Context{
		StateMgr:    stateMgr,
		SyncMgr:     &mockSyncManager{},
		AppSettings: appSettings,
	}

	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		ctx.HandleStatus(w, req)
	}
}

// BenchmarkHandleFilesPaginated measures file listing performance
func BenchmarkHandleFilesPaginated(b *testing.B) {
	tempDir := b.TempDir()
	os.Setenv("PERM_DIR", tempDir)
	defer os.Unsetenv("PERM_DIR")

	stateMgr, _ := state.NewManager()
	appSettings, _ := settings.Load()

	// Create mock with many files
	mockSync := &mockSyncManager{
		files: make([]syncmanager.FileInfo, 1000),
	}
	for i := 0; i < 1000; i++ {
		name := "file" + string(rune(i)) + ".jpg"
		mockSync.files[i] = syncmanager.FileInfo{Name: name, Path: name}
	}

	ctx := &Context{
		StateMgr:    stateMgr,
		SyncMgr:     mockSync,
		AppSettings: appSettings,
	}

	req := httptest.NewRequest(http.MethodGet, "/api/files/paginated?page=1&page_size=100", nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		ctx.HandleFilesPaginated(w, req)
	}
}
