package handlers

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// HandleFileView previously called w.Header().Set(...) before invoking the
// streaming GetFile, then on error called http.Error(...). Because the
// content-type header was already staged, http.Error's superfluous WriteHeader
// would race with the implicit 200 OK, leaving clients with a 200 response
// whose body was a plain-text error. After the fix the deferred-header writer
// suppresses status emission until the first byte is written, so a pre-stream
// failure yields a proper 5xx with no body corruption.

func TestHandleFileView_UpstreamErrorReturnsBadGateway(t *testing.T) {
	ctx, cleanup := setupTestContext(t)
	defer cleanup()

	mockSync := ctx.SyncMgr.(*mockSyncManager)
	mockSync.getFileFn = func(path string, w io.Writer) error {
		// Simulate a connection/auth failure that produces no bytes.
		return errors.New("rclone: NoSuchKey")
	}

	req := httptest.NewRequest(http.MethodGet, "/api/file/view?path=DCIM/IMG_001.jpg", nil)
	w := httptest.NewRecorder()
	ctx.HandleFileView(w, req)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusBadGateway, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); strings.HasPrefix(ct, "image/") {
		t.Fatalf("Content-Type leaked image MIME on error response: %q", ct)
	}
	if !strings.Contains(w.Body.String(), "failed to retrieve file") {
		t.Fatalf("body does not contain error message: %q", w.Body.String())
	}
}

func TestHandleFileView_SuccessfulStreamRetainsHeaders(t *testing.T) {
	ctx, cleanup := setupTestContext(t)
	defer cleanup()

	payload := []byte("hello world")
	mockSync := ctx.SyncMgr.(*mockSyncManager)
	mockSync.getFileFn = func(path string, w io.Writer) error {
		_, err := w.Write(payload)
		return err
	}

	req := httptest.NewRequest(http.MethodGet, "/api/file/view?path=README.md", nil)
	w := httptest.NewRecorder()
	ctx.HandleFileView(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/markdown") {
		t.Fatalf("Content-Type = %q, want text/markdown prefix", ct)
	}
	if w.Body.String() != string(payload) {
		t.Fatalf("body = %q, want %q", w.Body.String(), string(payload))
	}
}

func TestHandleFileView_RejectsUnsupportedType(t *testing.T) {
	ctx, cleanup := setupTestContext(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/file/view?path=DCIM/movie.mov", nil)
	w := httptest.NewRecorder()
	ctx.HandleFileView(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}
