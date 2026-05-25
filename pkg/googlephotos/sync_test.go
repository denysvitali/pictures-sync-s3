package googlephotos

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/denysvitali/pictures-sync-s3/pkg/syncmanager"
)

type fakeRemoteSyncManager struct {
	files     map[string][]syncmanager.FileInfo
	contents  map[string]string
	listed    []string
	downloads []string
}

func (m *fakeRemoteSyncManager) ListCardIDs() ([]syncmanager.FileInfo, error) {
	return nil, nil
}

func (m *fakeRemoteSyncManager) ListFiles(path string) ([]syncmanager.FileInfo, error) {
	m.listed = append(m.listed, path)
	files, ok := m.files[path]
	if !ok {
		return nil, fmt.Errorf("unexpected list path %q", path)
	}
	return files, nil
}

func (m *fakeRemoteSyncManager) GetFile(path string, w io.Writer) error {
	m.downloads = append(m.downloads, path)
	content, ok := m.contents[path]
	if !ok {
		return fmt.Errorf("unexpected download path %q", path)
	}
	_, err := io.WriteString(w, content)
	return err
}

func TestSyncCardListsDCIMRecursivelyBeforeCreatingAlbum(t *testing.T) {
	remote := &fakeRemoteSyncManager{
		files: map[string][]syncmanager.FileInfo{
			"card-abc/DCIM": {
				{Name: "100CANON", Path: "card-abc/DCIM/100CANON", IsDir: true},
			},
			"card-abc/DCIM/100CANON": {
				{Name: "IMG_0001.JPG", Path: "card-abc/DCIM/100CANON/IMG_0001.JPG"},
			},
		},
		contents: map[string]string{
			"card-abc/DCIM/100CANON/IMG_0001.JPG": "jpeg bytes",
		},
	}

	client := newTestClient(t, func(req *http.Request) (*http.Response, error) {
		switch {
		case req.Method == http.MethodGet && req.URL.Path == "/v1/albums":
			return jsonResponse(http.StatusOK, `{}`), nil
		case req.Method == http.MethodPost && req.URL.Path == "/v1/albums":
			return jsonResponse(http.StatusOK, `{"id":"album-1","title":"Card abc"}`), nil
		case req.Method == http.MethodPost && req.URL.Path == "/v1/uploads":
			return textResponse(http.StatusOK, "upload-token-1"), nil
		case req.Method == http.MethodPost && req.URL.Path == "/v1/mediaItems:batchCreate":
			return jsonResponse(http.StatusOK, `{"newMediaItemResults":[{"uploadToken":"upload-token-1","status":{"code":0}}]}`), nil
		default:
			t.Fatalf("unexpected Google Photos request: %s %s", req.Method, req.URL.String())
			return nil, nil
		}
	})

	manager := NewSyncManager(client, remote)
	manager.progress = &SyncProgress{}
	uploaded, skipped, failed, err := manager.syncCard(context.Background(), "abc", "card-abc")
	if err != nil {
		t.Fatalf("syncCard returned error: %v", err)
	}
	if uploaded != 1 || skipped != 0 || failed != 0 {
		t.Fatalf("syncCard counts = uploaded %d skipped %d failed %d, want 1 0 0", uploaded, skipped, failed)
	}
	if got, want := strings.Join(remote.listed, ","), "card-abc/DCIM,card-abc/DCIM/100CANON"; got != want {
		t.Fatalf("listed paths = %q, want %q", got, want)
	}
	if got, want := strings.Join(remote.downloads, ","), "card-abc/DCIM/100CANON/IMG_0001.JPG"; got != want {
		t.Fatalf("downloaded paths = %q, want %q", got, want)
	}
}

func TestSyncCardDoesNotCreateAlbumWithoutMedia(t *testing.T) {
	remote := &fakeRemoteSyncManager{
		files: map[string][]syncmanager.FileInfo{
			"card-empty/DCIM": {
				{Name: "100CANON", Path: "card-empty/DCIM/100CANON", IsDir: true},
			},
			"card-empty/DCIM/100CANON": {
				{Name: "IMG_0001.CR3", Path: "card-empty/DCIM/100CANON/IMG_0001.CR3"},
			},
		},
	}

	client := newTestClient(t, func(req *http.Request) (*http.Response, error) {
		t.Fatalf("unexpected Google Photos request for card without uploadable media: %s %s", req.Method, req.URL.String())
		return nil, nil
	})

	manager := NewSyncManager(client, remote)
	manager.progress = &SyncProgress{}
	uploaded, skipped, failed, err := manager.syncCard(context.Background(), "empty", "card-empty")
	if err != nil {
		t.Fatalf("syncCard returned error: %v", err)
	}
	if uploaded != 0 || skipped != 1 || failed != 0 {
		t.Fatalf("syncCard counts = uploaded %d skipped %d failed %d, want 0 1 0", uploaded, skipped, failed)
	}
}

func newTestClient(t *testing.T, rt roundTripFunc) *Client {
	t.Helper()

	tokenStore := NewTokenStore(t.TempDir() + "/token.json")
	if err := tokenStore.Save(&OAuthToken{
		AccessToken:  "access-token",
		RefreshToken: "refresh-token",
		TokenType:    "Bearer",
		Expiry:       time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("failed to save token: %v", err)
	}

	client := NewClient("client-id", "client-secret", tokenStore)
	client.httpClient = &http.Client{Transport: rt}
	return client
}

func jsonResponse(statusCode int, body string) *http.Response {
	return &http.Response{
		StatusCode: statusCode,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func textResponse(statusCode int, body string) *http.Response {
	return &http.Response{
		StatusCode: statusCode,
		Header:     http.Header{"Content-Type": []string{"text/plain"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}
