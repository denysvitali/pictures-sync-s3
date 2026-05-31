package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/denysvitali/pictures-sync-s3/pkg/captiveportal"
	"github.com/denysvitali/pictures-sync-s3/pkg/daemoncontrol"
	"github.com/denysvitali/pictures-sync-s3/pkg/httputil"
	"github.com/denysvitali/pictures-sync-s3/pkg/sdcardbrowser"
	"github.com/denysvitali/pictures-sync-s3/pkg/sdmonitor"
	"github.com/denysvitali/pictures-sync-s3/pkg/settings"
	"github.com/denysvitali/pictures-sync-s3/pkg/ssrf"
	"github.com/denysvitali/pictures-sync-s3/pkg/state"
	"github.com/denysvitali/pictures-sync-s3/pkg/syncmanager"
	"github.com/denysvitali/pictures-sync-s3/pkg/version"
	"github.com/denysvitali/pictures-sync-s3/pkg/wifimanager"
)

// mockSyncManager implements syncmanager.Manager interface for testing
type mockSyncManager struct {
	isRunning           bool
	cancelCalled        bool
	syncError           error
	files               []syncmanager.FileInfo
	cardIDs             []syncmanager.FileInfo
	publicLink          string
	listRemotesFn       func() ([]string, error)
	listFilesErr        error
	listCardIDsErr      error
	listPagedErr        error
	getFileFn           func(path string, w io.Writer) error
	googlePhotosRunning bool
}

func (m *mockSyncManager) IsRunning() bool { return m.isRunning }
func (m *mockSyncManager) Cancel() error   { m.cancelCalled = true; return nil }
func (m *mockSyncManager) Sync(string, string, int, int64) error {
	return m.syncError
}
func (m *mockSyncManager) SetRemote(string, string)     {}
func (m *mockSyncManager) SetGooglePhotos(bool, string) {}
func (m *mockSyncManager) ListRemotes() ([]string, error) {
	if m.listRemotesFn != nil {
		return m.listRemotesFn()
	}
	return []string{"local"}, nil
}
func (m *mockSyncManager) TestConnection() error { return nil }
func (m *mockSyncManager) ListFiles(path string) ([]syncmanager.FileInfo, error) {
	if m.listFilesErr != nil {
		return nil, m.listFilesErr
	}
	return m.files, nil
}
func (m *mockSyncManager) ListCardIDs() ([]syncmanager.FileInfo, error) {
	if m.listCardIDsErr != nil {
		return nil, m.listCardIDsErr
	}
	return m.cardIDs, nil
}
func (m *mockSyncManager) GetFile(path string, w io.Writer) error {
	if m.getFileFn != nil {
		return m.getFileFn(path, w)
	}
	return nil
}
func (m *mockSyncManager) GetPublicLink(path string) (string, error)                     { return m.publicLink, nil }
func (m *mockSyncManager) IsGooglePhotosRunning() bool                                   { return m.googlePhotosRunning }
func (m *mockSyncManager) CancelGooglePhotos() error                                     { return nil }
func (m *mockSyncManager) SyncCardsToGooglePhotos(context.Context, bool, []string) error { return nil }
func (m *mockSyncManager) GetGooglePhotosProgress() syncmanager.Progress {
	return syncmanager.Progress{}
}
func (m *mockSyncManager) GetGooglePhotosCardSummary(context.Context, string) (syncmanager.GooglePhotosCardSummary, error) {
	return syncmanager.GooglePhotosCardSummary{}, nil
}
func (m *mockSyncManager) GetFileRange(string, io.Writer, int64) error { return nil }
func (m *mockSyncManager) ListFilesPaginated(path string, page, pageSize int) (*syncmanager.FileListResult, error) {
	if m.listPagedErr != nil {
		return nil, m.listPagedErr
	}
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
func (m *mockWiFiManager) SetPrefer5GHzNetworks(prefer bool)               {}
func (m *mockWiFiManager) ScanNetworks() ([]wifimanager.ScanResult, error) { return nil, nil }
func (m *mockWiFiManager) ListNetworks() ([]wifimanager.Network, error)    { return nil, nil }
func (m *mockWiFiManager) GetCurrentConnection() (*wifimanager.ConnectionInfo, error) {
	return &wifimanager.ConnectionInfo{SSID: "TestNetwork", Signal: -40}, nil
}

type mockDaemonClient struct {
	stateMgr      *state.Manager
	manualSyncErr error
	cancelSyncErr error
	formatErr     error
	redetectErr   error
}

func (m *mockDaemonClient) RequestManualSync(context.Context, string) error {
	return m.manualSyncErr
}

func (m *mockDaemonClient) RequestCancelSync(context.Context) error {
	return m.cancelSyncErr
}

func (m *mockDaemonClient) RequestFormatSDCard(context.Context, string, string) error {
	return m.formatErr
}

func (m *mockDaemonClient) RequestRedetectSDCard(context.Context) error {
	return m.redetectErr
}

func (m *mockDaemonClient) RequestStatus(context.Context) (state.CurrentState, error) {
	return m.stateMgr.GetState(), nil
}

func (m *mockDaemonClient) RequestHistory(context.Context) ([]state.SyncRecord, error) {
	return m.stateMgr.GetHistory(), nil
}

func (m *mockDaemonClient) RequestDevices(context.Context) ([]sdmonitor.DeviceInfo, error) {
	currentState := m.stateMgr.GetState()
	if !currentState.SDCardMounted {
		return nil, nil
	}
	return []sdmonitor.DeviceInfo{{
		DevicePath: "/dev/sda1",
		DeviceName: "sda1",
		IsMounted:  true,
		MountPath:  currentState.SDCardPath,
		HasDCIM:    true,
	}}, nil
}

func (m *mockDaemonClient) RequestSDCardFiles(_ context.Context, path string) (*sdcardbrowser.FileList, error) {
	if !m.stateMgr.GetState().SDCardMounted {
		return nil, &daemoncontrol.CommandError{
			Code:    daemoncontrol.CodeNoSDCardMounted,
			Message: "no SD card mounted",
		}
	}
	return sdcardbrowser.ListFiles(state.MountDir, path)
}

func (m *mockDaemonClient) RequestSDCardPreview(_ context.Context, path string) (*sdcardbrowser.Preview, error) {
	if !m.stateMgr.GetState().SDCardMounted {
		return nil, &daemoncontrol.CommandError{
			Code:    daemoncontrol.CodeNoSDCardMounted,
			Message: "no SD card mounted",
		}
	}
	return sdcardbrowser.ReadPreview(state.MountDir, path)
}

func (m *mockDaemonClient) RequestSDCardThumbnail(_ context.Context, path string) (*sdcardbrowser.Preview, error) {
	if !m.stateMgr.GetState().SDCardMounted {
		return nil, &daemoncontrol.CommandError{
			Code:    daemoncontrol.CodeNoSDCardMounted,
			Message: "no SD card mounted",
		}
	}
	return sdcardbrowser.ReadThumbnail(state.MountDir, path)
}

func TestHandleDeviceFormat_POST(t *testing.T) {
	ctx, cleanup := setupTestContext(t)
	defer cleanup()

	var requestedPath string
	var requestedLabel string
	ctx.ManualSync = DaemonControlFunc{
		ManualSync: func(context.Context, string) error { return nil },
		CancelSync: func(context.Context) error { return nil },
		FormatSDCard: func(_ context.Context, devicePath, label string) error {
			requestedPath = devicePath
			requestedLabel = label
			return nil
		},
	}

	body := bytes.NewBufferString(`{"device_path":"/dev/sda1","confirmation":"FORMAT","label":"CAMERA_1"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/devices/format", body)
	w := httptest.NewRecorder()

	ctx.HandleDeviceFormat(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected status %d, got %d: %s", http.StatusOK, w.Code, w.Body.String())
	}
	if requestedPath != "/dev/sda1" {
		t.Fatalf("Expected device path %q, got %q", "/dev/sda1", requestedPath)
	}
	if requestedLabel != "CAMERA_1" {
		t.Fatalf("Expected label %q, got %q", "CAMERA_1", requestedLabel)
	}
}

func TestHandleDeviceFormat_RequiresConfirmation(t *testing.T) {
	ctx, cleanup := setupTestContext(t)
	defer cleanup()

	called := false
	ctx.ManualSync = DaemonControlFunc{
		ManualSync: func(context.Context, string) error { return nil },
		CancelSync: func(context.Context) error { return nil },
		FormatSDCard: func(context.Context, string, string) error {
			called = true
			return nil
		},
	}

	body := bytes.NewBufferString(`{"device_path":"/dev/sda1","confirmation":"format"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/devices/format", body)
	w := httptest.NewRecorder()

	ctx.HandleDeviceFormat(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("Expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
	if called {
		t.Fatal("Format should not be requested without exact confirmation")
	}
}

func TestHandleDeviceRedetect_POST(t *testing.T) {
	ctx, cleanup := setupTestContext(t)
	defer cleanup()

	called := false
	ctx.ManualSync = DaemonControlFunc{
		ManualSync:   func(context.Context, string) error { return nil },
		CancelSync:   func(context.Context) error { return nil },
		FormatSDCard: func(context.Context, string, string) error { return nil },
		RedetectSDCard: func(context.Context) error {
			called = true
			return nil
		},
	}

	req := httptest.NewRequest(http.MethodPost, "/api/devices/redetect", nil)
	w := httptest.NewRecorder()

	ctx.HandleDeviceRedetect(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected status %d, got %d: %s", http.StatusOK, w.Code, w.Body.String())
	}
	if !called {
		t.Fatal("Expected re-detect to be requested")
	}
}

// setupTestContext creates a test context with mock dependencies
func setupTestContext(t *testing.T) (*Context, func()) {
	t.Helper()

	tempDir := t.TempDir()
	oldPerm := os.Getenv("PERM_DIR")
	os.Setenv("PERM_DIR", tempDir)
	oldRuntime := os.Getenv("PICTURES_SYNC_STATE_DIR")
	// Isolate runtime state (state.json) per-test; otherwise the default
	// os.TempDir()/pictures-sync directory is shared across tests and one
	// test's SetSDCard(true, ...) leaks into the next test's NewManager().
	os.Setenv("PICTURES_SYNC_STATE_DIR", tempDir)

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
		publicLink: "https://storage.example.com/presigned/file1.jpg",
	}

	mockDaemon := &mockDaemonClient{
		stateMgr: stateMgr,
		manualSyncErr: &daemoncontrol.CommandError{
			Code:    daemoncontrol.CodeNoSDCardMounted,
			Message: "no SD card mounted",
		},
		cancelSyncErr: &daemoncontrol.CommandError{
			Code:    daemoncontrol.CodeSyncAlreadyActive,
			Message: "no sync in progress",
		},
	}

	ctx := &Context{
		StateMgr: stateMgr,
		SyncMgr:  mockSync,
		Daemon:   mockDaemon,
		ManualSync: DaemonControlFunc{
			ManualSync: func(context.Context, string) error {
				return &daemoncontrol.CommandError{
					Code:    daemoncontrol.CodeNoSDCardMounted,
					Message: "no SD card mounted",
				}
			},
			CancelSync: func(context.Context) error {
				return &daemoncontrol.CommandError{
					Code:    daemoncontrol.CodeSyncAlreadyActive,
					Message: "no sync in progress",
				}
			},
		},
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
		if oldRuntime == "" {
			os.Unsetenv("PICTURES_SYNC_STATE_DIR")
		} else {
			os.Setenv("PICTURES_SYNC_STATE_DIR", oldRuntime)
		}
	}

	return ctx, cleanup
}

func TestHandleGooglePhotosStatusReadsConfigWithoutListRemotes(t *testing.T) {
	ctx, cleanup := setupTestContext(t)
	defer cleanup()

	if err := ctx.AppSettings.SetGooglePhotos(true, "gphotos"); err != nil {
		t.Fatalf("SetGooglePhotos() error = %v", err)
	}

	configPath := state.GetRcloneConfigPath()
	if err := os.MkdirAll(filepath.Dir(configPath), 0750); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(configPath, []byte("[gphotos]\ntype = googlephotos\n"), 0600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	mockSync := ctx.SyncMgr.(*mockSyncManager)
	listRemotesCalled := false
	mockSync.listRemotesFn = func() ([]string, error) {
		listRemotesCalled = true
		return []string{"gphotos"}, nil
	}

	req := httptest.NewRequest(http.MethodGet, "/api/googlephotos/status", nil)
	w := httptest.NewRecorder()

	ctx.HandleGooglePhotosStatus(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d: %s", w.Code, w.Body.String())
	}
	if listRemotesCalled {
		t.Fatal("HandleGooglePhotosStatus called ListRemotes; this can block during an active sync")
	}

	var response map[string]bool
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if !response["configured"] {
		t.Fatal("configured = false, want true")
	}
	if !response["connected"] {
		t.Fatal("connected = false, want true")
	}
}

func TestHandleGooglePhotosSyncProgressReportsRunningStatus(t *testing.T) {
	ctx, cleanup := setupTestContext(t)
	defer cleanup()

	mockSync := ctx.SyncMgr.(*mockSyncManager)
	mockSync.googlePhotosRunning = true

	req := httptest.NewRequest(http.MethodGet, "/api/googlephotos/sync/progress", nil)
	w := httptest.NewRecorder()

	ctx.HandleGooglePhotosSyncProgress(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var response map[string]any
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if response["status"] != "syncing" {
		t.Fatalf("status = %v, want syncing", response["status"])
	}
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

	var response map[string]any
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

func TestHandleVersion_GET(t *testing.T) {
	ctx, cleanup := setupTestContext(t)
	defer cleanup()

	oldVersion := version.Version
	oldBuildDate := version.BuildDate
	version.Version = "test-version"
	version.BuildDate = "2026-05-03T00:00:00Z"
	defer func() {
		version.Version = oldVersion
		version.BuildDate = oldBuildDate
	}()

	req := httptest.NewRequest(http.MethodGet, "/api/version", nil)
	w := httptest.NewRecorder()

	ctx.HandleVersion(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var response version.Info
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if response.Version != "test-version" {
		t.Errorf("Expected version test-version, got %q", response.Version)
	}
	if response.BuildDate != "2026-05-03T00:00:00Z" {
		t.Errorf("Expected build date to be preserved, got %q", response.BuildDate)
	}
}

func TestHandleVersion_WrongMethod(t *testing.T) {
	ctx, cleanup := setupTestContext(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/api/version", nil)
	w := httptest.NewRecorder()

	ctx.HandleVersion(w, req)

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

	var response []any
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

	ctx.ManualSync = DaemonControlFunc{
		ManualSync: func(context.Context, string) error { return nil },
		CancelSync: func(context.Context) error { return nil },
	}

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

func TestHandleDeviceSelect_ForwardsRequestToDaemonWithDevicePath(t *testing.T) {
	ctx, cleanup := setupTestContext(t)
	defer cleanup()

	var gotDevicePath string
	ctx.ManualSync = DaemonControlFunc{
		ManualSync: func(_ context.Context, devicePath string) error {
			gotDevicePath = devicePath
			return nil
		},
		CancelSync: func(context.Context) error { return nil },
	}

	const wantedDevicePath = "/dev/sda1"
	body := map[string]string{"device_path": wantedDevicePath}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/devices/select", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	ctx.HandleDeviceSelect(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
	if gotDevicePath != wantedDevicePath {
		t.Fatalf("Expected device_path %q, got %q", wantedDevicePath, gotDevicePath)
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

func TestHandleSyncStart_ForwardsDaemonNoCardError(t *testing.T) {
	ctx, cleanup := setupTestContext(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/api/sync/start", nil)
	w := httptest.NewRecorder()

	ctx.HandleSyncStart(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", w.Code)
	}
}

func TestHandleSyncStart_ForwardsRequestToDaemon(t *testing.T) {
	ctx, cleanup := setupTestContext(t)
	defer cleanup()

	called := false
	ctx.ManualSync = DaemonControlFunc{
		ManualSync: func(context.Context, string) error {
			called = true
			return nil
		},
		CancelSync: func(context.Context) error { return nil },
	}

	req := httptest.NewRequest(http.MethodPost, "/api/sync/start", nil)
	w := httptest.NewRecorder()

	ctx.HandleSyncStart(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	if !called {
		t.Fatal("Expected manual sync request to be forwarded to daemon")
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

	called := false
	ctx.ManualSync = DaemonControlFunc{
		ManualSync: func(context.Context, string) error { return nil },
		CancelSync: func(context.Context) error {
			called = true
			return nil
		},
	}

	req := httptest.NewRequest(http.MethodPost, "/api/sync/cancel", nil)
	w := httptest.NewRecorder()

	ctx.HandleSyncCancel(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	if !called {
		t.Error("Expected cancel request to be forwarded to daemon")
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

	var response map[string]any
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	cards, ok := response["cards"].([]any)
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

	var response map[string]any
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

	var response map[string]any
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

	req := httptest.NewRequest(http.MethodGet, "/api/files/view?path=/file.bin", nil)
	w := httptest.NewRecorder()

	ctx.HandleFileView(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", w.Code)
	}
}

func TestHandleFileLink_GET(t *testing.T) {
	ctx, cleanup := setupTestContext(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/files/link?path=/file1.jpg", nil)
	w := httptest.NewRecorder()

	ctx.HandleFileLink(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var response map[string]string
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if response["url"] != "https://storage.example.com/presigned/file1.jpg" {
		t.Errorf("Expected presigned URL response, got %q", response["url"])
	}
}

func TestHandleFileLink_MissingPath(t *testing.T) {
	ctx, cleanup := setupTestContext(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/files/link", nil)
	w := httptest.NewRecorder()

	ctx.HandleFileLink(w, req)

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

			if w.Code != http.StatusBadRequest && w.Code != http.StatusForbidden {
				t.Errorf("Expected status 400 or 403, got %d for path %s", w.Code, tc.path)
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

	// No SD card mounted is a client-side condition (bad request), not a 200
	// success. The handler used to return 200 with {"error":...}, masking the
	// failure from HTTP-level monitoring; assert the corrected status here.
	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", w.Code)
	}

	var response map[string]any
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

		var response map[string]any
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

// TestJSONResponse tests the httputil.JSON response helper
func TestJSONResponse(t *testing.T) {
	w := httptest.NewRecorder()
	data := map[string]string{"key": "value"}

	httputil.JSON(w, http.StatusOK, data)

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
