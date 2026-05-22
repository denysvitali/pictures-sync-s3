package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// These tests guard against regressing to the old behavior in which file
// listing handlers responded with 200 OK and an "error" body, masking
// upstream rclone/daemon failures from HTTP-level monitoring and clients.

func TestHandleFileCards_UpstreamErrorReturnsBadGateway(t *testing.T) {
	ctx, cleanup := setupTestContext(t)
	defer cleanup()

	mockSync, ok := ctx.SyncMgr.(*mockSyncManager)
	if !ok {
		t.Fatalf("expected *mockSyncManager, got %T", ctx.SyncMgr)
	}
	mockSync.listCardIDsErr = errors.New("rclone exit status 1")

	req := httptest.NewRequest(http.MethodGet, "/api/files/cards", nil)
	w := httptest.NewRecorder()
	ctx.HandleFileCards(w, req)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusBadGateway, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["error"] == nil {
		t.Fatal("expected error field in response body")
	}
}

func TestHandleFiles_UpstreamErrorReturnsBadGateway(t *testing.T) {
	ctx, cleanup := setupTestContext(t)
	defer cleanup()

	mockSync := ctx.SyncMgr.(*mockSyncManager)
	mockSync.listFilesErr = errors.New("rclone: bucket not found")

	req := httptest.NewRequest(http.MethodGet, "/api/files?path=DCIM", nil)
	w := httptest.NewRecorder()
	ctx.HandleFiles(w, req)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusBadGateway, w.Body.String())
	}
}

func TestHandleFilesPaginated_UpstreamErrorReturnsBadGateway(t *testing.T) {
	ctx, cleanup := setupTestContext(t)
	defer cleanup()

	mockSync := ctx.SyncMgr.(*mockSyncManager)
	mockSync.listPagedErr = errors.New("rclone: auth failed")

	req := httptest.NewRequest(http.MethodGet, "/api/files/paginated?path=DCIM&page=1&page_size=50", nil)
	w := httptest.NewRecorder()
	ctx.HandleFilesPaginated(w, req)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusBadGateway, w.Body.String())
	}
}

// Successful paths must remain unaffected.
func TestHandleFiles_SuccessKeepsTwoHundred(t *testing.T) {
	ctx, cleanup := setupTestContext(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/files", nil)
	w := httptest.NewRecorder()
	ctx.HandleFiles(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
}
