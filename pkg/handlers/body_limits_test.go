package handlers

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/denysvitali/pictures-sync-s3/pkg/auth"
	"github.com/denysvitali/pictures-sync-s3/pkg/ota"
)

// largeJSONBody returns a JSON object whose payload is at least size bytes.
// The shape is a single "filler" field so it is syntactically valid JSON but
// drastically exceeds the per-handler body limit when size is large.
func largeJSONBody(size int) io.Reader {
	var buf bytes.Buffer
	buf.WriteString(`{"filler":"`)
	filler := strings.Repeat("A", size)
	buf.WriteString(filler)
	buf.WriteString(`"}`)
	return &buf
}

func TestHandlePasswordChangeRejectsOversizedBody(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gokr-pw.txt")
	if err := os.WriteFile(path, []byte("old-password\n"), 0600); err != nil {
		t.Fatalf("write password file: %v", err)
	}
	passwordMgr, err := auth.NewPasswordManager(path, "dev-password")
	if err != nil {
		t.Fatalf("NewPasswordManager() error = %v", err)
	}
	ctx := &Context{PasswordMgr: passwordMgr}

	// Body well in excess of maxPasswordChangeBodyBytes (4 KiB).
	req := httptest.NewRequest(http.MethodPost, "/api/auth/password", largeJSONBody(maxPasswordChangeBodyBytes*4))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	ctx.HandlePasswordChange(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusRequestEntityTooLarge, w.Body.String())
	}
	// Password must NOT have changed.
	if got := passwordMgr.CurrentPassword(); got != "old-password" {
		t.Fatalf("CurrentPassword() = %q, want unchanged old-password", got)
	}
}

func TestHandleSettingsRejectsOversizedBody(t *testing.T) {
	ctx, cleanup := setupTestContext(t)
	defer cleanup()

	originalRemote := ctx.AppSettings.GetRemoteName()

	req := httptest.NewRequest(http.MethodPost, "/api/settings", largeJSONBody(maxSettingsBodyBytes*2))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	ctx.HandleSettings(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusRequestEntityTooLarge, w.Body.String())
	}
	if got := ctx.AppSettings.GetRemoteName(); got != originalRemote {
		t.Fatalf("remote_name mutated to %q; expected unchanged %q", got, originalRemote)
	}
}

func TestHandleConfigB2RejectsOversizedBody(t *testing.T) {
	ctx, cleanup := setupTestContext(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/api/config/b2", largeJSONBody(maxB2ConfigBodyBytes*4))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	ctx.HandleConfigB2(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusRequestEntityTooLarge, w.Body.String())
	}
}

func TestHandleOTAInstallRejectsOversizedBody(t *testing.T) {
	// Use a non-nil OTAMgr so the handler reaches the body-decode path.
	ctx := &Context{OTAMgr: &ota.Manager{
		Owner:     "owner",
		Repo:      "repo",
		APIURL:    "https://api.example.invalid",
		AssetName: ota.DefaultAssetName,
	}}

	req := httptest.NewRequest(http.MethodPost, "/api/ota/install", largeJSONBody(maxOTAInstallBodyBytes*4))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	ctx.HandleOTAInstall(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusRequestEntityTooLarge, w.Body.String())
	}
}

// Sanity check: small valid bodies are still accepted (no regression).
func TestHandlePasswordChangeAcceptsNormalBody(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gokr-pw.txt")
	if err := os.WriteFile(path, []byte("old-password\n"), 0600); err != nil {
		t.Fatalf("write password file: %v", err)
	}
	passwordMgr, err := auth.NewPasswordManager(path, "dev-password")
	if err != nil {
		t.Fatalf("NewPasswordManager() error = %v", err)
	}
	ctx := &Context{PasswordMgr: passwordMgr}

	body := strings.NewReader(`{"current_password":"old-password","new_password":"brand-new-password"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/password", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	ctx.HandlePasswordChange(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if got := passwordMgr.CurrentPassword(); got != "brand-new-password" {
		t.Fatalf("CurrentPassword() = %q, want brand-new-password", got)
	}
}
