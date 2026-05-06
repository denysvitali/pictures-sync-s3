package handlers

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestHandleSDCardFileDownloadsAnyLocalFile(t *testing.T) {
	mountPath := t.TempDir()
	writeHandlerSDCardFile(t, mountPath, "DCIM/raw.CR3", "raw-data")
	withTestSDCardMountPath(t, mountPath)

	req := httptest.NewRequest(http.MethodGet, "/api/sdcard/file?path=DCIM/raw.CR3&download=1", nil)
	rr := httptest.NewRecorder()

	(&Context{}).HandleSDCardFile(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if rr.Body.String() != "raw-data" {
		t.Fatalf("unexpected body %q", rr.Body.String())
	}
	if got := rr.Header().Get("Content-Disposition"); got != `attachment; filename="raw.CR3"` {
		t.Fatalf("unexpected content disposition %q", got)
	}
}

func TestHandleSDCardFileSupportsVideoRangeRequests(t *testing.T) {
	mountPath := t.TempDir()
	writeHandlerSDCardFile(t, mountPath, "DCIM/video.MP4", "0123456789")
	withTestSDCardMountPath(t, mountPath)

	req := httptest.NewRequest(http.MethodGet, "/api/sdcard/file?path=DCIM/video.MP4", nil)
	req.Header.Set("Range", "bytes=2-5")
	rr := httptest.NewRecorder()

	(&Context{}).HandleSDCardFile(rr, req)

	if rr.Code != http.StatusPartialContent {
		t.Fatalf("expected 206, got %d: %s", rr.Code, rr.Body.String())
	}
	if rr.Body.String() != "2345" {
		t.Fatalf("unexpected range body %q", rr.Body.String())
	}
	if got := rr.Header().Get("Content-Type"); got != "video/mp4" {
		t.Fatalf("unexpected content type %q", got)
	}
}

func withTestSDCardMountPath(t *testing.T, mountPath string) {
	t.Helper()
	original := sdCardMountPath
	sdCardMountPath = mountPath
	t.Cleanup(func() {
		sdCardMountPath = original
	})
}

func writeHandlerSDCardFile(t *testing.T, mountPath, relativePath, contents string) {
	t.Helper()
	fullPath := filepath.Join(mountPath, relativePath)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(fullPath), err)
	}
	if err := os.WriteFile(fullPath, []byte(contents), 0644); err != nil {
		t.Fatalf("write %s: %v", fullPath, err)
	}
}
