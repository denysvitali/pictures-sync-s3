package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestB2ConfigRequestAcceptsQuickSetupPayload(t *testing.T) {
	req := b2ConfigRequest{
		KeyID:       "001a1b2c3d4e5f6a7b8c9d0e1f",
		AppKey:      "K001testapplicationkey",
		BucketAlias: "my-photo-backup",
		Region:      "us-west-004",
	}

	cfg, remoteName, remotePath, err := req.toB2Config()
	if err != nil {
		t.Fatalf("toB2Config returned error: %v", err)
	}

	if cfg.Account != req.KeyID {
		t.Fatalf("account = %q, want %q", cfg.Account, req.KeyID)
	}
	if cfg.Key != req.AppKey {
		t.Fatalf("key = %q, want %q", cfg.Key, req.AppKey)
	}
	if cfg.Bucket != req.BucketAlias {
		t.Fatalf("bucket = %q, want %q", cfg.Bucket, req.BucketAlias)
	}
	if cfg.Endpoint != "https://s3.us-west-004.backblazeb2.com" {
		t.Fatalf("endpoint = %q", cfg.Endpoint)
	}
	if remoteName != "b2" {
		t.Fatalf("remoteName = %q, want b2", remoteName)
	}
	if remotePath != "my-photo-backup/photos" {
		t.Fatalf("remotePath = %q, want my-photo-backup/photos", remotePath)
	}
}

func TestB2ConfigRequestAcceptsCanonicalPayload(t *testing.T) {
	req := b2ConfigRequest{
		Account:    "account-id",
		Key:        "application-key",
		Bucket:     "photo-bucket",
		RemoteName: "b2backup",
		RemotePath: "photo-bucket/archive",
		Endpoint:   "https://s3.eu-central-003.backblazeb2.com",
	}

	cfg, remoteName, remotePath, err := req.toB2Config()
	if err != nil {
		t.Fatalf("toB2Config returned error: %v", err)
	}

	if cfg.Account != req.Account || cfg.Key != req.Key || cfg.Bucket != req.Bucket {
		t.Fatalf("unexpected config: %+v", cfg)
	}
	if cfg.Endpoint != req.Endpoint {
		t.Fatalf("endpoint = %q, want %q", cfg.Endpoint, req.Endpoint)
	}
	if remoteName != req.RemoteName {
		t.Fatalf("remoteName = %q, want %q", remoteName, req.RemoteName)
	}
	if remotePath != req.RemotePath {
		t.Fatalf("remotePath = %q, want %q", remotePath, req.RemotePath)
	}
}

func TestB2ConfigRequestRejectsUnknownRegion(t *testing.T) {
	req := b2ConfigRequest{
		KeyID:       "001a1b2c3d4e5f6a7b8c9d0e1f",
		AppKey:      "K001testapplicationkey",
		BucketAlias: "my-photo-backup",
		Region:      "does-not-exist",
	}

	_, _, _, err := req.toB2Config()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "unknown B2 region") {
		t.Fatalf("expected unknown region error, got: %v", err)
	}
}

func TestHandleConfigB2ValidationFailureUsesBadRequest(t *testing.T) {
	ctx, cleanup := setupTestContext(t)
	defer cleanup()

	body := strings.NewReader(`{
		"bucket": "my-photo-backup",
		"key_id": "001a1b2c3d4e5f6a7b8c9d0e1f",
		"region": "us-west-004"
	}`)
	req := httptest.NewRequest(http.MethodPost, "/api/config/b2", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	ctx.HandleConfigB2(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusBadRequest, w.Body.String())
	}

	var response map[string]any
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if response["success"] != false {
		t.Fatalf("success = %v, want false", response["success"])
	}
	if !strings.Contains(response["error"].(string), "B2 application key is required") {
		t.Fatalf("unexpected error response: %v", response["error"])
	}
}

func TestHandleSettingsTailscaleAuthKeySavesWithoutResettingOtherSettings(t *testing.T) {
	ctx, cleanup := setupTestContext(t)
	defer cleanup()

	if err := ctx.AppSettings.SetGooglePhotos(true, "gphotos"); err != nil {
		t.Fatalf("SetGooglePhotos() error = %v", err)
	}

	keyPath := filepath.Join(t.TempDir(), "tailscale", "authkey")
	restore := overrideTailscaleConfigForTest(t, keyPath)

	var gotName string
	var gotArgs []string
	tailscaleCommandContext = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		gotName = name
		gotArgs = append([]string(nil), args...)
		return exec.CommandContext(ctx, "true")
	}
	defer restore()

	body := strings.NewReader(`{"tailscale_auth_key":"tskey-auth-test123"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/settings", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	ctx.HandleSettings(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	data, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatalf("ReadFile(auth key) error = %v", err)
	}
	if string(data) != "tskey-auth-test123\n" {
		t.Fatalf("stored auth key = %q", string(data))
	}
	if !ctx.AppSettings.GetGooglePhotosEnabled() {
		t.Fatal("Google Photos setting was reset by key-only settings update")
	}
	if ctx.AppSettings.GetGooglePhotosRemoteName() != "gphotos" {
		t.Fatalf("Google Photos remote = %q, want gphotos", ctx.AppSettings.GetGooglePhotosRemoteName())
	}
	if gotName != "/user/tailscale" {
		t.Fatalf("tailscale command = %q, want /user/tailscale", gotName)
	}
	if !containsString(gotArgs, "up") || !containsString(gotArgs, "--auth-key=tskey-auth-test123") || !containsString(gotArgs, "--accept-dns=false") {
		t.Fatalf("tailscale args = %#v", gotArgs)
	}
}

func TestHandleSettingsReportsTailscaleAuthKeyStatus(t *testing.T) {
	ctx, cleanup := setupTestContext(t)
	defer cleanup()

	keyPath := filepath.Join(t.TempDir(), "tailscale", "authkey")
	restore := overrideTailscaleConfigForTest(t, keyPath)
	defer restore()

	if err := os.MkdirAll(filepath.Dir(keyPath), 0755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(keyPath, []byte("tskey-auth-test123\n"), 0600); err != nil {
		t.Fatalf("WriteFile(auth key) error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/settings", nil)
	w := httptest.NewRecorder()

	ctx.HandleSettings(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var response map[string]any
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if response["tailscale_auth_key_configured"] != true {
		t.Fatalf("tailscale_auth_key_configured = %v, want true", response["tailscale_auth_key_configured"])
	}
	if response["tailscale_auth_key_path"] != keyPath {
		t.Fatalf("tailscale_auth_key_path = %v, want %s", response["tailscale_auth_key_path"], keyPath)
	}
}

func overrideTailscaleConfigForTest(t *testing.T, keyPath string) func() {
	t.Helper()

	oldAuthKeyPath := tailscaleAuthKeyPath
	oldBinary := tailscaleBinary
	oldCommandContext := tailscaleCommandContext
	tailscaleAuthKeyPath = keyPath
	tailscaleBinary = "/user/tailscale"

	return func() {
		tailscaleAuthKeyPath = oldAuthKeyPath
		tailscaleBinary = oldBinary
		tailscaleCommandContext = oldCommandContext
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

// TestHandleSettingsSavesAllFields verifies that a POST to /api/settings
// correctly persists every field, including the Google Photos OAuth fields
// that were previously missing from the request struct.
func TestHandleSettingsSavesAllFields(t *testing.T) {
	ctx, cleanup := setupTestContext(t)
	defer cleanup()

	body := strings.NewReader(`{
		"remote_name": "b2-remote",
		"remote_path": "/archive/photos",
		"reformat_threshold": 0.75,
		"transfers": 12,
		"checkers": 24,
		"google_photos_enabled": true,
		"google_photos_remote_name": "gphotos-rclone",
		"prefer_5ghz_wifi": false
	}`)
	req := httptest.NewRequest(http.MethodPost, "/api/settings", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	ctx.HandleSettings(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	// Verify every field was persisted
	if got := ctx.AppSettings.GetRemoteName(); got != "b2-remote" {
		t.Errorf("RemoteName = %q, want %q", got, "b2-remote")
	}
	if got := ctx.AppSettings.GetRemotePath(); got != "/archive/photos" {
		t.Errorf("RemotePath = %q, want %q", got, "/archive/photos")
	}
	if got := ctx.AppSettings.GetReformatThreshold(); got != 0.75 {
		t.Errorf("ReformatThreshold = %v, want 0.75", got)
	}
	if got := ctx.AppSettings.GetTransfers(); got != 12 {
		t.Errorf("Transfers = %d, want 12", got)
	}
	if got := ctx.AppSettings.GetCheckers(); got != 24 {
		t.Errorf("Checkers = %d, want 24", got)
	}
	if got := ctx.AppSettings.GetGooglePhotosEnabled(); got != true {
		t.Errorf("GooglePhotosEnabled = %v, want true", got)
	}
	if got := ctx.AppSettings.GetGooglePhotosRemoteName(); got != "gphotos-rclone" {
		t.Errorf("GooglePhotosRemoteName = %q, want %q", got, "gphotos-rclone")
	}
	if got := ctx.AppSettings.GetPrefer5GHzWiFi(); got != false {
		t.Errorf("Prefer5GHzWiFi = %v, want false", got)
	}
}

// TestHandleSettingsPartialUpdateDoesNotResetOthers verifies that sending
// only a subset of fields does not reset unmentioned fields to defaults.
func TestHandleSettingsPartialUpdateDoesNotResetOthers(t *testing.T) {
	ctx, cleanup := setupTestContext(t)
	defer cleanup()

	// Pre-populate some fields
	if err := ctx.AppSettings.SetGooglePhotos(true, "pre-existing-remote"); err != nil {
		t.Fatalf("SetGooglePhotos failed: %v", err)
	}
	if err := ctx.AppSettings.SetRemote("original-remote", "/original/path"); err != nil {
		t.Fatalf("SetRemote failed: %v", err)
	}

	// Now POST only transfers
	body := strings.NewReader(`{"transfers": 2}`)
	req := httptest.NewRequest(http.MethodPost, "/api/settings", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	ctx.HandleSettings(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	// Transfers should be updated
	if got := ctx.AppSettings.GetTransfers(); got != 2 {
		t.Errorf("Transfers = %d, want 2", got)
	}
	// Other fields should be untouched
	if got := ctx.AppSettings.GetRemoteName(); got != "original-remote" {
		t.Errorf("RemoteName = %q, want %q", got, "original-remote")
	}
	if got := ctx.AppSettings.GetGooglePhotosEnabled(); got != true {
		t.Errorf("GooglePhotosEnabled = %v, want true", got)
	}
	if got := ctx.AppSettings.GetGooglePhotosRemoteName(); got != "pre-existing-remote" {
		t.Errorf("GooglePhotosRemoteName = %q, want %q", got, "pre-existing-remote")
	}
}

func TestRedactRcloneConfig(t *testing.T) {
	config := []byte(`[b2]
type = b2
account = account-id
key = application-key
endpoint = https://s3.us-west-004.backblazeb2.com
`)

	redacted, provider := redactRcloneConfig(config)
	if provider != "b2" {
		t.Fatalf("provider = %q, want b2", provider)
	}
	if strings.Contains(redacted, "application-key") {
		t.Fatalf("redacted config leaked secret: %s", redacted)
	}
	if !strings.Contains(redacted, "account = account-id") {
		t.Fatalf("redacted config should keep non-secret fields: %s", redacted)
	}
	if !strings.Contains(redacted, "key = [redacted]") {
		t.Fatalf("redacted config missing redacted key: %s", redacted)
	}
}
