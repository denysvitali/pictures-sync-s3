package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testAuthorizedKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIEIu0z15sYReuH6m7zLVcFdEi1JBUfzBg4FcNcW6WYfT test@example"

func withBreakglassAuthorizedKeysPath(t *testing.T) string {
	t.Helper()

	oldPath := breakglassAuthorizedKeysPath
	path := filepath.Join(t.TempDir(), "breakglass", "authorized_keys")
	breakglassAuthorizedKeysPath = path
	t.Cleanup(func() {
		breakglassAuthorizedKeysPath = oldPath
	})
	return path
}

func TestHandleBreakglassAuthorizedKeys_PostWritesPermFile(t *testing.T) {
	ctx, cleanup := setupTestContext(t)
	defer cleanup()
	path := withBreakglassAuthorizedKeysPath(t)

	payload, err := json.Marshal(map[string]string{
		"authorized_keys": "\n# local note\n" + testAuthorizedKey + "\n",
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/breakglass/authorized-keys", bytes.NewReader(payload))
	rec := httptest.NewRecorder()
	ctx.HandleBreakglassAuthorizedKeys(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read authorized_keys: %v", err)
	}
	if got := string(data); got != testAuthorizedKey+"\n" {
		t.Fatalf("unexpected authorized_keys content: %q", got)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat authorized_keys: %v", err)
	}
	if got := info.Mode().Perm(); got != 0600 {
		t.Fatalf("expected authorized_keys mode 0600, got %o", got)
	}

	dirInfo, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatalf("stat breakglass dir: %v", err)
	}
	if got := dirInfo.Mode().Perm(); got != 0700 {
		t.Fatalf("expected breakglass dir mode 0700, got %o", got)
	}
}

func TestHandleBreakglassAuthorizedKeys_PostRejectsInvalidKey(t *testing.T) {
	ctx, cleanup := setupTestContext(t)
	defer cleanup()
	withBreakglassAuthorizedKeysPath(t)

	payload, err := json.Marshal(map[string]string{
		"authorized_keys": "not-a-key",
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/breakglass/authorized-keys", bytes.NewReader(payload))
	rec := httptest.NewRecorder()
	ctx.HandleBreakglassAuthorizedKeys(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "line 1") {
		t.Fatalf("expected line-specific validation error, got %q", rec.Body.String())
	}
}

func TestHandleBreakglassAuthorizedKeys_GetMissingFile(t *testing.T) {
	ctx, cleanup := setupTestContext(t)
	defer cleanup()
	path := withBreakglassAuthorizedKeysPath(t)

	req := httptest.NewRequest(http.MethodGet, "/api/breakglass/authorized-keys", nil)
	rec := httptest.NewRecorder()
	ctx.HandleBreakglassAuthorizedKeys(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var response struct {
		AuthorizedKeys string `json:"authorized_keys"`
		Path           string `json:"path"`
		Count          int    `json:"count"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.AuthorizedKeys != "" || response.Count != 0 || response.Path != path {
		t.Fatalf("unexpected response: %+v", response)
	}
}
