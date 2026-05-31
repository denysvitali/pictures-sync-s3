package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/denysvitali/pictures-sync-s3/pkg/daemoncontrol"
	"github.com/denysvitali/pictures-sync-s3/pkg/sdcardbrowser"
	"github.com/denysvitali/pictures-sync-s3/pkg/state"
	"github.com/denysvitali/pictures-sync-s3/pkg/syncmanager"
)

// filesTestMethodCase exercises wrong-method rejection for files.go handlers.
type filesTestMethodCase struct {
	name    string
	handler func(*Context, http.ResponseWriter, *http.Request)
	url     string
}

func TestFilesHandlers_MethodNotAllowed(t *testing.T) {
	cases := []filesTestMethodCase{
		{"HandleFileCards", (*Context).HandleFileCards, "/api/files/cards"},
		{"HandleFilesPaginated", (*Context).HandleFilesPaginated, "/api/files/paginated"},
		{"HandleFiles", (*Context).HandleFiles, "/api/files"},
		{"HandleFileView", (*Context).HandleFileView, "/api/files/view?path=README.md"},
		{"HandleFileLink", (*Context).HandleFileLink, "/api/files/link?path=README.md"},
		{"HandleThumbnail", (*Context).HandleThumbnail, "/api/thumbnail?path=DCIM/x.jpg"},
		{"HandleSDCardFiles", (*Context).HandleSDCardFiles, "/api/sdcard/files"},
		{"HandleSDCardPreview", (*Context).HandleSDCardPreview, "/api/sdcard/preview?path=DCIM/x.jpg"},
		{"HandleSDCardFile", (*Context).HandleSDCardFile, "/api/sdcard/file?path=DCIM/x.jpg"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cleanup := setupTestContext(t)
			defer cleanup()

			req := httptest.NewRequest(http.MethodPost, tc.url, nil)
			w := httptest.NewRecorder()
			tc.handler(ctx, w, req)

			if w.Code != http.StatusMethodNotAllowed {
				t.Fatalf("expected 405, got %d: %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestHandleFileCards_UpstreamErrorReturnsBadGateway(t *testing.T) {
	ctx, cleanup := setupTestContext(t)
	defer cleanup()

	mock := ctx.SyncMgr.(*mockSyncManager)
	mock.listCardIDsErr = errors.New("rclone failed")

	req := httptest.NewRequest(http.MethodGet, "/api/files/cards", nil)
	w := httptest.NewRecorder()
	ctx.HandleFileCards(w, req)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d: %s", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["error"] == nil {
		t.Fatalf("expected error field, got %#v", body)
	}
}

func TestHandleFiles_UpstreamErrorReturnsBadGateway(t *testing.T) {
	ctx, cleanup := setupTestContext(t)
	defer cleanup()

	mock := ctx.SyncMgr.(*mockSyncManager)
	mock.listFilesErr = errors.New("connection refused")

	req := httptest.NewRequest(http.MethodGet, "/api/files?path=p", nil)
	w := httptest.NewRecorder()
	ctx.HandleFiles(w, req)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleFilesPaginated_UpstreamErrorReturnsBadGateway(t *testing.T) {
	ctx, cleanup := setupTestContext(t)
	defer cleanup()

	mock := ctx.SyncMgr.(*mockSyncManager)
	mock.listPagedErr = errors.New("backend exploded")

	req := httptest.NewRequest(http.MethodGet, "/api/files/paginated?path=p&page=2&page_size=10", nil)
	w := httptest.NewRecorder()
	ctx.HandleFilesPaginated(w, req)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleFilesPaginated_HonorsValidParams(t *testing.T) {
	ctx, cleanup := setupTestContext(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/files/paginated?path=p&page=3&page_size=25", nil)
	w := httptest.NewRecorder()
	ctx.HandleFilesPaginated(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp syncmanager.FileListResult
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Page != 3 {
		t.Fatalf("page = %d, want 3", resp.Page)
	}
	if resp.PageSize != 25 {
		t.Fatalf("page_size = %d, want 25", resp.PageSize)
	}
}

func TestHandleFilesPaginated_TooLargePageSizeFallsBackToDefault(t *testing.T) {
	ctx, cleanup := setupTestContext(t)
	defer cleanup()

	// page_size > 1000 falls back to default 100.
	req := httptest.NewRequest(http.MethodGet, "/api/files/paginated?page_size=99999", nil)
	w := httptest.NewRecorder()
	ctx.HandleFilesPaginated(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp syncmanager.FileListResult
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.PageSize != 100 {
		t.Fatalf("page_size = %d, want default 100", resp.PageSize)
	}
}

func TestHandleFilesPaginated_GarbagePageParamsIgnored(t *testing.T) {
	ctx, cleanup := setupTestContext(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/files/paginated?page=abc&page_size=xyz", nil)
	w := httptest.NewRecorder()
	ctx.HandleFilesPaginated(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp syncmanager.FileListResult
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Page != 1 || resp.PageSize != 100 {
		t.Fatalf("expected defaults page=1, page_size=100, got %d / %d", resp.Page, resp.PageSize)
	}
}

func TestHandleFileView_ImageContentTypes(t *testing.T) {
	cases := map[string]string{
		"a.jpg":  "image/jpeg",
		"b.jpeg": "image/jpeg",
		"c.png":  "image/png",
		"d.gif":  "image/gif",
		"e.webp": "image/webp",
	}
	for name, want := range cases {
		t.Run(name, func(t *testing.T) {
			ctx, cleanup := setupTestContext(t)
			defer cleanup()

			payload := []byte("binary-bytes")
			mock := ctx.SyncMgr.(*mockSyncManager)
			mock.getFileFn = func(_ string, w io.Writer) error {
				_, err := w.Write(payload)
				return err
			}

			req := httptest.NewRequest(http.MethodGet, "/api/file/view?path="+name, nil)
			w := httptest.NewRecorder()
			ctx.HandleFileView(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", w.Code)
			}
			if got := w.Header().Get("Content-Type"); got != want {
				t.Fatalf("Content-Type = %q, want %q", got, want)
			}
			if w.Body.String() != string(payload) {
				t.Fatalf("body = %q", w.Body.String())
			}
		})
	}
}

func TestHandleFileView_TextContentTypesByFilename(t *testing.T) {
	cases := map[string]string{
		"Dockerfile":  "text/x-dockerfile",
		"Makefile":    "text/x-makefile",
		"LICENSE":     "text/plain",
		"README":      "text/markdown",
		"CHANGELOG":   "text/markdown",
		"config.json": "application/json",
		"data.yaml":   "text/yaml",
	}
	for name, prefix := range cases {
		t.Run(name, func(t *testing.T) {
			ctx, cleanup := setupTestContext(t)
			defer cleanup()

			mock := ctx.SyncMgr.(*mockSyncManager)
			mock.getFileFn = func(_ string, w io.Writer) error {
				_, _ = w.Write([]byte("hello"))
				return nil
			}

			req := httptest.NewRequest(http.MethodGet, "/api/file/view?path="+name, nil)
			w := httptest.NewRecorder()
			ctx.HandleFileView(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("status = %d", w.Code)
			}
			if got := w.Header().Get("Content-Type"); !strings.HasPrefix(got, prefix) {
				t.Fatalf("Content-Type = %q, want prefix %q", got, prefix)
			}
		})
	}
}

func TestHandleFileView_EmptyUpstreamStillEmitsOK(t *testing.T) {
	ctx, cleanup := setupTestContext(t)
	defer cleanup()

	mock := ctx.SyncMgr.(*mockSyncManager)
	mock.getFileFn = func(_ string, _ io.Writer) error {
		return nil
	}

	req := httptest.NewRequest(http.MethodGet, "/api/file/view?path=empty.txt", nil)
	w := httptest.NewRecorder()
	ctx.HandleFileView(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestHandleFileLink_PublicLinkError(t *testing.T) {
	ctx, cleanup := setupTestContext(t)
	defer cleanup()

	// Replace the mock with a sync manager that returns an error for public links.
	ctx.SyncMgr = &filesTestErroringSync{}

	req := httptest.NewRequest(http.MethodGet, "/api/files/link?path=DCIM/foo.jpg", nil)
	w := httptest.NewRecorder()
	ctx.HandleFileLink(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleFileLink_SuccessSetsNoStoreCache(t *testing.T) {
	ctx, cleanup := setupTestContext(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/files/link?path=p.jpg", nil)
	w := httptest.NewRecorder()
	ctx.HandleFileLink(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
}

func TestHandleThumbnail_RejectsAbsolutePathTraversal(t *testing.T) {
	ctx, cleanup := setupTestContext(t)
	defer cleanup()

	for _, badPath := range []string{
		"../../etc/passwd",
		"DCIM/../../../etc/shadow",
		"/etc/passwd",
		"/..",
	} {
		req := httptest.NewRequest(http.MethodGet, "/api/thumbnail?path="+badPath, nil)
		w := httptest.NewRecorder()
		ctx.HandleThumbnail(w, req)

		if w.Code != http.StatusForbidden && w.Code != http.StatusBadRequest {
			t.Fatalf("path %q: expected 400/403, got %d", badPath, w.Code)
		}
	}
}

func TestHandleThumbnail_SuccessReturnsJPEG(t *testing.T) {
	ctx, cleanup := setupTestContext(t)
	defer cleanup()
	rel := "DCIM/IMG_1.jpg"
	filesTestWriteJPEG(t, state.MountDir, rel)
	filesTestWithMountPath(t, state.MountDir)
	ctx.StateMgr.SetSDCard(true, state.MountDir)

	req := httptest.NewRequest(http.MethodGet, "/api/thumbnail?path="+rel, nil)
	w := httptest.NewRecorder()
	ctx.HandleThumbnail(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Content-Type"); !strings.HasPrefix(got, "image/") {
		t.Fatalf("Content-Type = %q, want image/*", got)
	}
	if got := w.Header().Get("Cache-Control"); got == "" {
		t.Fatalf("expected Cache-Control header, got empty")
	}
}

func TestHandleSDCardFiles_MountedSuccess(t *testing.T) {
	ctx, cleanup := setupTestContext(t)
	defer cleanup()
	filesTestWriteJPEG(t, state.MountDir, "DCIM/IMG_001.jpg")
	filesTestWithMountPath(t, state.MountDir)
	ctx.StateMgr.SetSDCard(true, state.MountDir)

	req := httptest.NewRequest(http.MethodGet, "/api/sdcard/files?path=DCIM", nil)
	w := httptest.NewRecorder()
	ctx.HandleSDCardFiles(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp sdcardbrowser.FileList
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Files) == 0 {
		t.Fatalf("expected at least one file in DCIM, got empty list")
	}
}

func TestHandleSDCardPreview_MissingPath(t *testing.T) {
	ctx, cleanup := setupTestContext(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/sdcard/preview", nil)
	w := httptest.NewRecorder()
	ctx.HandleSDCardPreview(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleSDCardPreview_Success(t *testing.T) {
	ctx, cleanup := setupTestContext(t)
	defer cleanup()
	rel := "DCIM/IMG_2.jpg"
	filesTestWriteJPEG(t, state.MountDir, rel)
	filesTestWithMountPath(t, state.MountDir)
	ctx.StateMgr.SetSDCard(true, state.MountDir)

	req := httptest.NewRequest(http.MethodGet, "/api/sdcard/preview?path="+rel, nil)
	w := httptest.NewRecorder()
	ctx.HandleSDCardPreview(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Content-Type"); !strings.HasPrefix(got, "image/") {
		t.Fatalf("Content-Type = %q, want image/*", got)
	}
	if got := w.Header().Get("Content-Disposition"); !strings.Contains(got, "IMG_2.jpg") {
		t.Fatalf("Content-Disposition = %q, expected to contain filename", got)
	}
}

func TestHandleSDCardFile_MissingPath(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/sdcard/file", nil)
	w := httptest.NewRecorder()
	(&Context{}).HandleSDCardFile(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleSDCardFile_NotFoundReturns404(t *testing.T) {
	mountPath := t.TempDir()
	filesTestWithMountPath(t, mountPath)

	req := httptest.NewRequest(http.MethodGet, "/api/sdcard/file?path=DCIM/missing.JPG", nil)
	w := httptest.NewRecorder()
	(&Context{}).HandleSDCardFile(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleSDCardFile_TraversalDenied(t *testing.T) {
	mountPath := t.TempDir()
	filesTestWithMountPath(t, mountPath)

	for _, p := range []string{"../../etc/passwd", "/etc/shadow", "../escape.txt"} {
		req := httptest.NewRequest(http.MethodGet, "/api/sdcard/file?path="+p, nil)
		w := httptest.NewRecorder()
		(&Context{}).HandleSDCardFile(w, req)

		if w.Code == http.StatusOK {
			t.Fatalf("path %q served file (status 200) but expected denial", p)
		}
	}
}

func TestHandleSDCardFile_InlineDispositionByDefault(t *testing.T) {
	mountPath := t.TempDir()
	rel := "DCIM/photo.jpg"
	filesTestWriteJPEG(t, mountPath, rel)
	filesTestWithMountPath(t, mountPath)

	req := httptest.NewRequest(http.MethodGet, "/api/sdcard/file?path="+rel, nil)
	w := httptest.NewRecorder()
	(&Context{}).HandleSDCardFile(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if got := w.Header().Get("Content-Disposition"); !strings.HasPrefix(got, "inline;") {
		t.Fatalf("expected inline content-disposition, got %q", got)
	}
}

func TestDaemonHTTPStatus(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"plain error => 500", errors.New("boom"), http.StatusInternalServerError},
		{"access denied => 403", &daemoncontrol.CommandError{Message: "access denied"}, http.StatusForbidden},
		{"unsupported file type => 400", &daemoncontrol.CommandError{Message: "unsupported file type"}, http.StatusBadRequest},
		{"path parameter required => 400", &daemoncontrol.CommandError{Message: "path parameter required"}, http.StatusBadRequest},
		{"no SD card mounted => 400", &daemoncontrol.CommandError{Code: daemoncontrol.CodeNoSDCardMounted, Message: "no SD card mounted"}, http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := daemonHTTPStatus(tc.err); got != tc.want {
				t.Fatalf("daemonHTTPStatus(%v) = %d, want %d", tc.err, got, tc.want)
			}
		})
	}
}

func TestDaemonErrorMessage(t *testing.T) {
	plain := errors.New("plain text error")
	if got := daemonErrorMessage(plain); got != "plain text error" {
		t.Fatalf("plain error: got %q", got)
	}

	cmdErr := &daemoncontrol.CommandError{Code: "x", Message: "structured message"}
	if got := daemonErrorMessage(cmdErr); got != "structured message" {
		t.Fatalf("command error: got %q", got)
	}
}

func TestSDCardFileHTTPStatus(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"path parameter required", errors.New("path parameter required"), http.StatusBadRequest},
		{"access denied", errors.New("access denied"), http.StatusForbidden},
		{"path is a directory", errors.New("path is a directory"), http.StatusBadRequest},
		{"not exist", os.ErrNotExist, http.StatusNotFound},
		{"other => 500", errors.New("io: short read"), http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := sdcardFileHTTPStatus(tc.err); got != tc.want {
				t.Fatalf("sdcardFileHTTPStatus(%v) = %d, want %d", tc.err, got, tc.want)
			}
		})
	}
}

func TestExtractEXIF_GarbageBytesReturnsNil(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, "garbage.jpg")
	if err := os.WriteFile(p, []byte("not actually a jpeg"), 0600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got := extractEXIF(p); got != nil {
		t.Fatalf("expected nil EXIF for garbage data, got %v", got)
	}
}

func TestDeferredHeaderWriter_FirstWriteSendsStatus(t *testing.T) {
	w := httptest.NewRecorder()
	d := &deferredHeaderWriter{w: w, status: http.StatusOK}

	if d.wrote {
		t.Fatalf("wrote flag should be false before Write")
	}
	n, err := d.Write([]byte("hi"))
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if n != 2 {
		t.Fatalf("n = %d, want 2", n)
	}
	if !d.wrote {
		t.Fatalf("wrote flag should be true after Write")
	}
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if w.Body.String() != "hi" {
		t.Fatalf("body = %q", w.Body.String())
	}

	// Subsequent writes must not double-emit the header.
	if _, err := d.Write([]byte("!")); err != nil {
		t.Fatalf("second write: %v", err)
	}
	if w.Body.String() != "hi!" {
		t.Fatalf("body = %q", w.Body.String())
	}
}

func TestFileViewContentType_UnknownExtension(t *testing.T) {
	if ct, ok := fileViewContentType("foo.unknownext"); ok {
		t.Fatalf("expected unknown extension to be unsupported, got %q", ct)
	}
}

func TestVerifyNoSymlinkEscape_DetectsEscape(t *testing.T) {
	sdRoot := t.TempDir()
	outside := t.TempDir()
	outsideFile := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(outsideFile, []byte("secret"), 0600); err != nil {
		t.Fatalf("write outside: %v", err)
	}

	// Create a symlink inside sdRoot that points to a file outside.
	symlink := filepath.Join(sdRoot, "leak.txt")
	if err := os.Symlink(outsideFile, symlink); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	if err := verifyNoSymlinkEscape(sdRoot, symlink); err == nil {
		t.Fatalf("expected escape error for symlink pointing outside SD root")
	}
}

func TestVerifyNoSymlinkEscape_AllowsLegitimatePath(t *testing.T) {
	sdRoot := t.TempDir()
	inside := filepath.Join(sdRoot, "DCIM", "IMG.jpg")
	filesTestWriteJPEG(t, sdRoot, "DCIM/IMG.jpg")

	if err := verifyNoSymlinkEscape(sdRoot, inside); err != nil {
		t.Fatalf("expected legitimate path to pass, got %v", err)
	}
}

// --- helpers (prefixed with filesTest to avoid collisions) ---

func filesTestWithMountPath(t *testing.T, mountPath string) {
	t.Helper()
	original := sdCardMountPath
	sdCardMountPath = mountPath
	t.Cleanup(func() { sdCardMountPath = original })
}

// filesTestWriteJPEG writes a tiny but fully-decodable JPEG so handlers that
// run an actual image decode (ReadThumbnail/ReadPreview) succeed instead of
// rejecting a hand-rolled SOI/EOI stub for missing scan data.
func filesTestWriteJPEG(t *testing.T, mountPath, rel string) {
	t.Helper()
	full := filepath.Join(mountPath, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(full), err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{255, 0, 0, 255})
	img.Set(1, 1, color.RGBA{0, 255, 0, 255})
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 80}); err != nil {
		t.Fatalf("encode jpeg: %v", err)
	}
	if err := os.WriteFile(full, buf.Bytes(), 0644); err != nil {
		t.Fatalf("write %s: %v", full, err)
	}
}

// filesTestErroringSync is a SyncManager that returns errors for the bits
// HandleFileLink exercises. All other methods are no-ops or return zero values.
type filesTestErroringSync struct{}

func (filesTestErroringSync) IsRunning() bool                       { return false }
func (filesTestErroringSync) Cancel() error                         { return nil }
func (filesTestErroringSync) Sync(string, string, int, int64) error { return nil }
func (filesTestErroringSync) SetRemote(string, string)              {}
func (filesTestErroringSync) SetGooglePhotos(bool, string)          {}
func (filesTestErroringSync) ListRemotes() ([]string, error)        { return nil, nil }
func (filesTestErroringSync) TestConnection() error                 { return nil }
func (filesTestErroringSync) ListCardIDs() ([]syncmanager.FileInfo, error) {
	return nil, nil
}
func (filesTestErroringSync) ListFiles(string) ([]syncmanager.FileInfo, error) {
	return nil, nil
}
func (filesTestErroringSync) ListFilesPaginated(string, int, int) (*syncmanager.FileListResult, error) {
	return &syncmanager.FileListResult{}, nil
}
func (filesTestErroringSync) GetFile(string, io.Writer) error { return nil }
func (filesTestErroringSync) GetPublicLink(string) (string, error) {
	return "", errors.New("signing service unavailable")
}
func (filesTestErroringSync) IsGooglePhotosRunning() bool                         { return false }
func (filesTestErroringSync) CancelGooglePhotos() error                           { return nil }
func (filesTestErroringSync) SyncCardsToGooglePhotos(context.Context, bool) error { return nil }
func (filesTestErroringSync) GetGooglePhotosProgress() syncmanager.Progress {
	return syncmanager.Progress{}
}
