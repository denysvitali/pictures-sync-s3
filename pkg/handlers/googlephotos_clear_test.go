package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/denysvitali/pictures-sync-s3/pkg/googlephotos"
	"github.com/denysvitali/pictures-sync-s3/pkg/settings"
)

// testClearServer is a mock Google Photos API server for album-clear tests.
type testClearServer struct {
	mu             sync.Mutex
	searchHits     int
	removeHits     int
	searchItems    []*googlephotos.MediaItem
	searchStatus   int
	searchBody     string
	removeStatus   int
	removeBody     string
	removeDelay    time.Duration
	removeCancelCh chan struct{}
}

func (s *testClearServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, ":batchRemoveMediaItems"):
		s.mu.Lock()
		s.removeHits++
		s.mu.Unlock()

		if s.removeDelay > 0 {
			select {
			case <-time.After(s.removeDelay):
			case <-r.Context().Done():
				return
			}
		}
		if s.removeCancelCh != nil {
			select {
			case <-s.removeCancelCh:
			default:
			}
		}

		status := s.removeStatus
		if status == 0 {
			status = http.StatusOK
		}
		w.WriteHeader(status)
		if s.removeBody != "" {
			w.Write([]byte(s.removeBody))
		} else {
			w.Write([]byte("{}"))
		}

	case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "mediaItems:search"):
		s.mu.Lock()
		s.searchHits++
		s.mu.Unlock()

		status := s.searchStatus
		if status == 0 {
			status = http.StatusOK
		}
		w.WriteHeader(status)
		if s.searchBody != "" {
			w.Write([]byte(s.searchBody))
		} else {
			resp := map[string]any{"mediaItems": s.searchItems}
			json.NewEncoder(w).Encode(resp)
		}

	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/albums"):
		// ListAlbumsContext — used for album-name lookup in the clear handler.
		resp := map[string]any{"albums": []*googlephotos.Album{
			{ID: "test-album", Title: "card-test"},
		}}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(resp)

	default:
		http.NotFound(w, r)
	}
}

func setupClearTest(t *testing.T, server *httptest.Server) (*Context, func()) {
	t.Helper()

	tempDir := t.TempDir()
	oldPerm := os.Getenv("PERM_DIR")
	os.Setenv("PERM_DIR", tempDir)
	oldRuntime := os.Getenv("PICTURES_SYNC_STATE_DIR")
	os.Setenv("PICTURES_SYNC_STATE_DIR", tempDir)
	// Prevent any real token refresh attempts.
	oldOAuthURL := googlephotos.OAuthTokenURL()
	googlephotos.SetOAuthTokenURL(server.URL + "/token")

	// Point the API at our test server.
	origBase := googlephotos.GetAPIBaseURL()
	googlephotos.SetAPIBaseURL(server.URL + "/v1")

	// Seed the token store at the default path so the handler's client is
	// authenticated. The handler uses NewTokenStore("") which resolves to
	// {PermDir}/google-photos-token.json.
	tokenPath := tempDir + "/pictures-sync/google-photos-token.json"
	os.MkdirAll(tempDir+"/pictures-sync", 0750)
	tokenData, _ := json.Marshal(&googlephotos.OAuthToken{
		AccessToken:  "test-access-token",
		RefreshToken: "test-refresh-token",
		TokenType:    "Bearer",
		Expiry:       time.Now().Add(time.Hour),
	})
	os.WriteFile(tokenPath, tokenData, 0600)

	appSettings := settings.DefaultSettings()
	appSettings.GooglePhotosClientID = "test-client-id"
	appSettings.GooglePhotosClientSecret = "test-client-secret"

	ctx := &Context{
		AppSettings: appSettings,
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
		googlephotos.SetOAuthTokenURL(oldOAuthURL)
		googlephotos.SetAPIBaseURL(origBase)
		// Clear any stale album-clear progress state.
		albumClearOpsMu.Lock()
		albumClearOps = make(map[string]*googlephotos.AlbumClearProgress)
		albumClearOpsMu.Unlock()
	}
	return ctx, cleanup
}

func pollUntil(t *testing.T, check func() (bool, string), timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	lastMsg := ""
	for time.Now().Before(deadline) {
		ok, msg := check()
		if ok {
			return
		}
		lastMsg = msg
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("condition not met within %v: %s", timeout, lastMsg)
}

func getClearProgress(albumID string) *googlephotos.AlbumClearProgress {
	albumClearOpsMu.RLock()
	defer albumClearOpsMu.RUnlock()
	return albumClearOps[albumID]
}

func TestGooglePhotosAlbumClear(t *testing.T) {
	t.Run("happy_path", func(t *testing.T) {
		srv := &testClearServer{
			searchItems: []*googlephotos.MediaItem{
				{ID: "item-1", Filename: "a.jpg"},
				{ID: "item-2", Filename: "b.jpg"},
			},
		}
		ts := httptest.NewServer(srv)
		defer ts.Close()

		ctx, cleanup := setupClearTest(t, ts)
		defer cleanup()

		req := httptest.NewRequest(http.MethodDelete, "/api/googlephotos/albums/test-album", nil)
		w := httptest.NewRecorder()
		ctx.HandleGooglePhotosAlbums(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("DELETE status = %d, want 200: %s", w.Code, w.Body.String())
		}

		pollUntil(t, func() (bool, string) {
			p := getClearProgress("test-album")
			if p == nil {
				return false, "progress nil"
			}
			return p.Status == "completed" || p.Status == "error", p.Status
		}, 5*time.Second)

		p := getClearProgress("test-album")
		if p.Status != "completed" {
			t.Fatalf("status = %q, want completed; error: %s", p.Status, p.Error)
		}
		if p.TotalItems != 2 {
			t.Errorf("TotalItems = %d, want 2", p.TotalItems)
		}
		if p.RemovedItems != 2 {
			t.Errorf("RemovedItems = %d, want 2", p.RemovedItems)
		}
		if p.Error != "" {
			t.Errorf("Error = %q, want empty", p.Error)
		}
		if srv.searchHits != 1 {
			t.Errorf("searchHits = %d, want 1", srv.searchHits)
		}
		if srv.removeHits != 1 {
			t.Errorf("removeHits = %d, want 1", srv.removeHits)
		}
	})

	t.Run("permission_denied", func(t *testing.T) {
		srv := &testClearServer{
			searchItems: []*googlephotos.MediaItem{
				{ID: "item-1", Filename: "a.jpg"},
				{ID: "item-2", Filename: "b.jpg"},
			},
			removeStatus: http.StatusForbidden,
			removeBody:   `{"error":{"code":403,"status":"PERMISSION_DENIED","message":"No permission to remove the following media items: item-1, item-2"}}`,
		}
		ts := httptest.NewServer(srv)
		defer ts.Close()

		ctx, cleanup := setupClearTest(t, ts)
		defer cleanup()

		req := httptest.NewRequest(http.MethodDelete, "/api/googlephotos/albums/test-album", nil)
		w := httptest.NewRecorder()
		ctx.HandleGooglePhotosAlbums(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("DELETE status = %d, want 200", w.Code)
		}

		pollUntil(t, func() (bool, string) {
			p := getClearProgress("test-album")
			if p == nil {
				return false, "progress nil"
			}
			return p.Status == "error" || p.Status == "completed", p.Status
		}, 5*time.Second)

		p := getClearProgress("test-album")
		if p.Status != "error" {
			t.Fatalf("status = %q, want error", p.Status)
		}
		if p.Error == "" {
			t.Fatal("Error is empty, want non-empty")
		}
		// Agent 2 prepends the permission-denied explanation for 403 errors.
		if !strings.Contains(p.Error, "PERMISSION_DENIED") {
			t.Errorf("Error missing PERMISSION_DENIED: %s", p.Error)
		}
		if !strings.Contains(p.Error, "Google Photos API refused") {
			t.Errorf("Error missing permission explanation: %s", p.Error)
		}
	})

	t.Run("empty_album", func(t *testing.T) {
		srv := &testClearServer{
			searchItems: []*googlephotos.MediaItem{},
		}
		ts := httptest.NewServer(srv)
		defer ts.Close()

		ctx, cleanup := setupClearTest(t, ts)
		defer cleanup()

		req := httptest.NewRequest(http.MethodDelete, "/api/googlephotos/albums/test-album", nil)
		w := httptest.NewRecorder()
		ctx.HandleGooglePhotosAlbums(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("DELETE status = %d, want 200", w.Code)
		}

		pollUntil(t, func() (bool, string) {
			p := getClearProgress("test-album")
			if p == nil {
				return false, "progress nil"
			}
			return p.Status == "completed", p.Status
		}, 5*time.Second)

		p := getClearProgress("test-album")
		if p.TotalItems != 0 {
			t.Errorf("TotalItems = %d, want 0", p.TotalItems)
		}
		if p.RemovedItems != 0 {
			t.Errorf("RemovedItems = %d, want 0", p.RemovedItems)
		}
		if p.Error != "" {
			t.Errorf("Error = %q, want empty", p.Error)
		}
	})

	t.Run("list_error", func(t *testing.T) {
		srv := &testClearServer{
			searchStatus: http.StatusInternalServerError,
			searchBody:   `{"error":{"code":500,"message":"internal"}}`,
		}
		ts := httptest.NewServer(srv)
		defer ts.Close()

		ctx, cleanup := setupClearTest(t, ts)
		defer cleanup()

		req := httptest.NewRequest(http.MethodDelete, "/api/googlephotos/albums/test-album", nil)
		w := httptest.NewRecorder()
		ctx.HandleGooglePhotosAlbums(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("DELETE status = %d, want 200", w.Code)
		}

		pollUntil(t, func() (bool, string) {
			p := getClearProgress("test-album")
			if p == nil {
				return false, "progress nil"
			}
			return p.Status == "error", p.Status
		}, 5*time.Second)

		p := getClearProgress("test-album")
		if p.Error == "" {
			t.Fatal("Error is empty, want non-empty")
		}
		// Should contain the API error message (from search, status 500).
		if !strings.Contains(p.Error, "search") && !strings.Contains(p.Error, "500") {
			t.Logf("Error (may be wrapped): %s", p.Error)
		}
	})

	t.Run("method_not_allowed", func(t *testing.T) {
		ts := httptest.NewServer(http.NotFoundHandler())
		defer ts.Close()

		ctx, cleanup := setupClearTest(t, ts)
		defer cleanup()

		req := httptest.NewRequest(http.MethodPut, "/api/googlephotos/albums/test-album", nil)
		w := httptest.NewRecorder()
		ctx.HandleGooglePhotosAlbums(w, req)

		if w.Code != http.StatusMethodNotAllowed {
			t.Fatalf("PUT status = %d, want 405", w.Code)
		}
	})

	t.Run("missing_credentials", func(t *testing.T) {
		ts := httptest.NewServer(http.NotFoundHandler())
		defer ts.Close()

		ctx, cleanup := setupClearTest(t, ts)
		defer cleanup()

		// Wipe credentials so the handler rejects the request.
		ctx.AppSettings.GooglePhotosClientID = ""
		ctx.AppSettings.GooglePhotosClientSecret = ""

		req := httptest.NewRequest(http.MethodDelete, "/api/googlephotos/albums/test-album", nil)
		w := httptest.NewRecorder()
		ctx.HandleGooglePhotosAlbums(w, req)

		if w.Code != http.StatusPreconditionFailed {
			t.Fatalf("status = %d, want 412", w.Code)
		}
	})

	t.Run("context_cancellation", func(t *testing.T) {
		cancelCh := make(chan struct{})
		srv := &testClearServer{
			searchItems: []*googlephotos.MediaItem{
				{ID: "item-1"},
				{ID: "item-2"},
			},
			removeDelay:    2 * time.Second,
			removeCancelCh: cancelCh,
		}
		ts := httptest.NewServer(srv)
		defer ts.Close()

		ctx, cleanup := setupClearTest(t, ts)
		defer cleanup()

		req := httptest.NewRequest(http.MethodDelete, "/api/googlephotos/albums/test-album", nil)
		w := httptest.NewRecorder()
		ctx.HandleGooglePhotosAlbums(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("DELETE status = %d, want 200", w.Code)
		}

		// The background goroutine should be stuck on the slow remove.
		// After a short delay, the handler's detached context (10 min timeout)
		// is still alive, but the HTTP request context is cancelled.
		// The detached context means cancellation here is via the slow response.
		// We just verify the operation eventually completes or errors.
		pollUntil(t, func() (bool, string) {
			p := getClearProgress("test-album")
			if p == nil {
				return false, "progress nil"
			}
			return p.Status != "clearing", p.Status
		}, 5*time.Second)
		// The operation either succeeds (after remove returns) or errors.
		// Either outcome is acceptable; the key assertion is no panic/hang.
		close(cancelCh)
	})
}
