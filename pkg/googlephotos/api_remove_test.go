package googlephotos

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// setupRemoveTestServer starts an httptest.Server and points apiBaseURL at it.
// The returned cleanup must be deferred to restore apiBaseURL.
func setupRemoveTestServer(t *testing.T, handler http.Handler) (*httptest.Server, func()) {
	t.Helper()
	server := httptest.NewServer(handler)
	origBase := apiBaseURL
	apiBaseURL = server.URL + "/v1"
	cleanup := func() {
		apiBaseURL = origBase
		server.Close()
	}
	return server, cleanup
}

// newRemoveTestClient builds a Client with a non-expired access token so the
// OAuth refresh path is never exercised.
func newRemoveTestClient(t *testing.T) *Client {
	t.Helper()
	tokenStore := NewTokenStore(t.TempDir() + "/token.json")
	if err := tokenStore.Save(&OAuthToken{
		AccessToken:  "test-access-token",
		RefreshToken: "test-refresh-token",
		TokenType:    "Bearer",
		Expiry:       time.Now().Add(24 * time.Hour),
	}); err != nil {
		t.Fatalf("failed to seed token: %v", err)
	}
	return NewClient("client-id", "client-secret", tokenStore)
}

// captureHandler records request bodies and returns a fixed status code/body.
type captureHandler struct {
	mu      sync.Mutex
	bodies  [][]byte
	paths   []string
	status  int
	body    string
	delay   time.Duration
	albumID string
	t       *testing.T
}

func (h *captureHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.t.Errorf("unexpected method %s", r.Method)
	}
	if !strings.HasSuffix(r.URL.Path, ":batchRemoveMediaItems") {
		h.t.Errorf("unexpected path suffix: %s", r.URL.Path)
	}
	if h.albumID != "" {
		wantPath := "/v1/albums/" + h.albumID + ":batchRemoveMediaItems"
		if r.URL.Path != wantPath {
			h.t.Errorf("path = %q, want %q", r.URL.Path, wantPath)
		}
	}
	if got := r.Header.Get("Authorization"); got != "Bearer test-access-token" {
		h.t.Errorf("Authorization = %q, want Bearer test-access-token", got)
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		h.t.Errorf("failed to read body: %v", err)
	}
	if h.delay > 0 {
		select {
		case <-time.After(h.delay):
		case <-r.Context().Done():
			return
		}
	}
	h.mu.Lock()
	h.bodies = append(h.bodies, body)
	h.paths = append(h.paths, r.URL.Path)
	h.mu.Unlock()

	status := h.status
	if status == 0 {
		status = http.StatusOK
	}
	w.WriteHeader(status)
	if h.body != "" {
		_, _ = w.Write([]byte(h.body))
	} else {
		_, _ = w.Write([]byte("{}"))
	}
}

func (h *captureHandler) hits() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.bodies)
}

func (h *captureHandler) snapshot() [][]byte {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([][]byte, len(h.bodies))
	copy(out, h.bodies)
	return out
}

func parseRemoveBody(t *testing.T, raw []byte) []string {
	t.Helper()
	var req BatchRemoveMediaItemsRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		t.Fatalf("failed to parse request body %q: %v", raw, err)
	}
	return req.MediaItemIds
}

func TestBatchRemoveMediaItems(t *testing.T) {
	t.Run("happy_path_single_chunk", func(t *testing.T) {
		h := &captureHandler{t: t, albumID: "album-1"}
		_, cleanup := setupRemoveTestServer(t, h)
		defer cleanup()

		client := newRemoveTestClient(t)

		var calls []struct{ removed, total int }
		var mu sync.Mutex
		onProgress := func(removed, total int) {
			mu.Lock()
			calls = append(calls, struct{ removed, total int }{removed, total})
			mu.Unlock()
		}

		ids := []string{"a", "b", "c"}
		err := client.BatchRemoveMediaItemsWithProgress(context.Background(), "album-1", ids, onProgress)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if h.hits() != 1 {
			t.Fatalf("hits = %d, want 1", h.hits())
		}
		got := parseRemoveBody(t, h.snapshot()[0])
		if len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "c" {
			t.Fatalf("body ids = %v, want [a b c]", got)
		}
		if len(calls) != 1 || calls[0].removed != 3 || calls[0].total != 3 {
			t.Fatalf("progress calls = %v, want [{3 3}]", calls)
		}
	})

	t.Run("happy_path_multi_chunk", func(t *testing.T) {
		h := &captureHandler{t: t, albumID: "album-2"}
		_, cleanup := setupRemoveTestServer(t, h)
		defer cleanup()

		client := newRemoveTestClient(t)

		ids := make([]string, 125)
		for i := range ids {
			ids[i] = fmt.Sprintf("id-%d", i)
		}

		var calls []struct{ removed, total int }
		var mu sync.Mutex
		onProgress := func(removed, total int) {
			mu.Lock()
			calls = append(calls, struct{ removed, total int }{removed, total})
			mu.Unlock()
		}

		err := client.BatchRemoveMediaItemsWithProgress(context.Background(), "album-2", ids, onProgress)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if h.hits() != 3 {
			t.Fatalf("hits = %d, want 3", h.hits())
		}

		snap := h.snapshot()
		wantSizes := []int{50, 50, 25}
		for i, want := range wantSizes {
			got := parseRemoveBody(t, snap[i])
			if len(got) != want {
				t.Errorf("chunk %d size = %d, want %d", i, len(got), want)
				continue
			}
			startIdx := 0
			for j := 0; j < i; j++ {
				startIdx += wantSizes[j]
			}
			for k, id := range got {
				wantID := fmt.Sprintf("id-%d", startIdx+k)
				if id != wantID {
					t.Errorf("chunk %d[%d] = %q, want %q", i, k, id, wantID)
				}
			}
		}

		wantCalls := []struct{ removed, total int }{
			{50, 125},
			{100, 125},
			{125, 125},
		}
		if len(calls) != len(wantCalls) {
			t.Fatalf("progress calls = %v, want %v", calls, wantCalls)
		}
		for i, want := range wantCalls {
			if calls[i] != want {
				t.Errorf("progress[%d] = %+v, want %+v", i, calls[i], want)
			}
		}
	})

	t.Run("forbidden_permission_denied", func(t *testing.T) {
		h := &captureHandler{
			t:      t,
			status: http.StatusForbidden,
			body:   `{"error":{"code":403,"status":"PERMISSION_DENIED","message":"No permission to remove items not added by this app"}}`,
		}
		_, cleanup := setupRemoveTestServer(t, h)
		defer cleanup()

		client := newRemoveTestClient(t)

		err := client.BatchRemoveMediaItemsWithProgress(context.Background(), "album-3", []string{"x"}, nil)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		var apiErr *APIError
		if !errors.As(err, &apiErr) {
			t.Fatalf("error %v is not *APIError", err)
		}
		if apiErr.Status != 403 {
			t.Errorf("Status = %d, want 403", apiErr.Status)
		}
		if !IsPermissionDenied(err) {
			t.Errorf("IsPermissionDenied = false, want true")
		}
		msg := err.Error()
		if !strings.Contains(msg, "PERMISSION_DENIED") {
			t.Errorf("error %q does not contain PERMISSION_DENIED", msg)
		}
		if !strings.Contains(msg, "No permission") {
			t.Errorf("error %q does not contain 'No permission'", msg)
		}
	})

	t.Run("server_500_plain_body", func(t *testing.T) {
		h := &captureHandler{
			t:      t,
			status: http.StatusInternalServerError,
			body:   "boom",
		}
		_, cleanup := setupRemoveTestServer(t, h)
		defer cleanup()

		client := newRemoveTestClient(t)

		err := client.BatchRemoveMediaItemsWithProgress(context.Background(), "album-4", []string{"x"}, nil)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		var apiErr *APIError
		if !errors.As(err, &apiErr) {
			t.Fatalf("error %v is not *APIError", err)
		}
		if apiErr.Status != 500 {
			t.Errorf("Status = %d, want 500", apiErr.Status)
		}
		if apiErr.Body != "boom" {
			t.Errorf("Body = %q, want %q", apiErr.Body, "boom")
		}
		if !strings.Contains(err.Error(), "boom") {
			t.Errorf("error %q does not contain 'boom'", err.Error())
		}
	})

	t.Run("context_cancellation_mid_chunk", func(t *testing.T) {
		h := &captureHandler{t: t, delay: 200 * time.Millisecond}
		_, cleanup := setupRemoveTestServer(t, h)
		defer cleanup()

		client := newRemoveTestClient(t)

		ids := make([]string, 125)
		for i := range ids {
			ids[i] = fmt.Sprintf("id-%d", i)
		}

		var calls []struct{ removed, total int }
		var mu sync.Mutex
		onProgress := func(removed, total int) {
			mu.Lock()
			calls = append(calls, struct{ removed, total int }{removed, total})
			mu.Unlock()
		}

		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			time.Sleep(50 * time.Millisecond)
			cancel()
		}()

		err := client.BatchRemoveMediaItemsWithProgress(ctx, "album-5", ids, onProgress)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !errors.Is(err, context.Canceled) {
			t.Errorf("error %v does not wrap context.Canceled", err)
		}

		mu.Lock()
		gotCalls := append([]struct{ removed, total int }(nil), calls...)
		mu.Unlock()

		// onProgress is invoked after a chunk completes. If cancellation arrives
		// during the first chunk's request, no progress should have fired.
		// In rare scheduling, the first chunk could complete before cancellation;
		// in that case at most one call is acceptable but never a later chunk.
		for _, c := range gotCalls {
			if c.removed > 50 {
				t.Errorf("progress reported removed=%d after cancel; want <=50", c.removed)
			}
		}
	})

	t.Run("empty_ids", func(t *testing.T) {
		h := &captureHandler{t: t}
		_, cleanup := setupRemoveTestServer(t, h)
		defer cleanup()

		client := newRemoveTestClient(t)

		progressCalled := false
		onProgress := func(removed, total int) {
			progressCalled = true
		}

		err := client.BatchRemoveMediaItemsWithProgress(context.Background(), "album-6", nil, onProgress)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if h.hits() != 0 {
			t.Errorf("server hits = %d, want 0", h.hits())
		}
		if progressCalled {
			t.Error("onProgress was called for empty ids")
		}

		// also test with explicit empty slice
		err = client.BatchRemoveMediaItemsWithProgress(context.Background(), "album-6", []string{}, onProgress)
		if err != nil {
			t.Fatalf("unexpected error on empty slice: %v", err)
		}
		if h.hits() != 0 {
			t.Errorf("server hits after empty slice = %d, want 0", h.hits())
		}
		if progressCalled {
			t.Error("onProgress was called for empty slice")
		}
	})

	t.Run("nil_onProgress", func(t *testing.T) {
		h := &captureHandler{t: t, albumID: "album-7"}
		_, cleanup := setupRemoveTestServer(t, h)
		defer cleanup()

		client := newRemoveTestClient(t)

		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("panicked with nil onProgress: %v", r)
			}
		}()

		err := client.BatchRemoveMediaItemsWithProgress(context.Background(), "album-7", []string{"a", "b", "c"}, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if h.hits() != 1 {
			t.Fatalf("hits = %d, want 1", h.hits())
		}
	})
}
