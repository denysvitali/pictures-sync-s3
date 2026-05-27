package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// These tests guard against regressing to the old behavior in which file
// listing handlers responded with 200 OK and an "error" body, masking
// upstream rclone/daemon failures from HTTP-level monitoring and clients.

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
