package googlephotos

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/denysvitali/pictures-sync-s3/pkg/syncmanager"
)

type fakeRemoteSyncManager struct {
	mu        sync.Mutex
	files     map[string][]syncmanager.FileInfo
	contents  map[string]string
	listed    []string
	downloads []string
}

func (m *fakeRemoteSyncManager) ListCardIDs() ([]syncmanager.FileInfo, error) {
	return nil, nil
}

func (m *fakeRemoteSyncManager) ListFiles(path string) ([]syncmanager.FileInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.listed = append(m.listed, path)
	files, ok := m.files[path]
	if !ok {
		return nil, fmt.Errorf("unexpected list path %q", path)
	}
	return files, nil
}

func (m *fakeRemoteSyncManager) GetFile(path string, w io.Writer) error {
	m.mu.Lock()
	m.downloads = append(m.downloads, path)
	content, ok := m.contents[path]
	m.mu.Unlock()
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

func TestSyncCardPopulatesDetailedProgress(t *testing.T) {
	remote := &fakeRemoteSyncManager{
		files: map[string][]syncmanager.FileInfo{
			"card-abc/DCIM": {
				{Name: "IMG_0001.JPG", Path: "card-abc/DCIM/IMG_0001.JPG", Size: 11},
			},
		},
		contents: map[string]string{
			"card-abc/DCIM/IMG_0001.JPG": "jpeg bytes!",
		},
	}

	client := newTestClient(t, func(req *http.Request) (*http.Response, error) {
		switch {
		case req.Method == http.MethodGet && req.URL.Path == "/v1/albums":
			return jsonResponse(http.StatusOK, `{}`), nil
		case req.Method == http.MethodPost && req.URL.Path == "/v1/albums":
			return jsonResponse(http.StatusOK, `{"id":"album-1","title":"Card abc"}`), nil
		case req.Method == http.MethodPost && req.URL.Path == "/v1/uploads":
			if _, err := io.Copy(io.Discard, req.Body); err != nil {
				t.Fatalf("failed to read upload body: %v", err)
			}
			return textResponse(http.StatusOK, "upload-token-1"), nil
		case req.Method == http.MethodPost && req.URL.Path == "/v1/mediaItems:batchCreate":
			return jsonResponse(http.StatusOK, `{"newMediaItemResults":[{"uploadToken":"upload-token-1","status":{"code":0}}]}`), nil
		default:
			t.Fatalf("unexpected Google Photos request: %s %s", req.Method, req.URL.String())
			return nil, nil
		}
	})

	manager := NewSyncManager(client, remote)
	manager.progress = &SyncProgress{Status: "syncing"}
	uploaded, skipped, failed, err := manager.syncCard(context.Background(), "abc", "card-abc")
	if err != nil {
		t.Fatalf("syncCard returned error: %v", err)
	}
	if uploaded != 1 || skipped != 0 || failed != 0 {
		t.Fatalf("syncCard counts = uploaded %d skipped %d failed %d, want 1 0 0", uploaded, skipped, failed)
	}

	progress := manager.Progress()
	if progress.CurrentCardFiles != 1 {
		t.Fatalf("CurrentCardFiles = %d, want 1", progress.CurrentCardFiles)
	}
	if progress.CurrentFileIndex != 1 {
		t.Fatalf("CurrentFileIndex = %d, want 1", progress.CurrentFileIndex)
	}
	if progress.TotalBytes != 11 || progress.ProcessedBytes != 11 {
		t.Fatalf("bytes = processed %d total %d, want 11/11", progress.ProcessedBytes, progress.TotalBytes)
	}
	if progress.CurrentFile != "IMG_0001.JPG" || progress.CurrentFilePath != "card-abc/DCIM/IMG_0001.JPG" {
		t.Fatalf("current file = %q path %q", progress.CurrentFile, progress.CurrentFilePath)
	}
	if progress.CurrentFilePercent != 100 || progress.CurrentFileBytesUploaded != 11 {
		t.Fatalf("current file progress = %d%%/%d bytes, want 100%%/11 bytes", progress.CurrentFilePercent, progress.CurrentFileBytesUploaded)
	}
	if progress.UploadedFiles != 1 || progress.ProcessedFiles != 1 {
		t.Fatalf("progress counts = uploaded %d processed %d, want 1/1", progress.UploadedFiles, progress.ProcessedFiles)
	}
	if progress.BatchPendingFiles != 0 {
		t.Fatalf("BatchPendingFiles = %d, want 0", progress.BatchPendingFiles)
	}
	if progress.UpdatedAt == nil {
		t.Fatal("UpdatedAt is nil")
	}
}

func TestSyncCardAddsMediaToAlbumInModTimeOrder(t *testing.T) {
	oldest := time.Date(2026, 1, 2, 9, 0, 0, 0, time.UTC)
	middle := time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC)
	newest := time.Date(2026, 1, 2, 11, 0, 0, 0, time.UTC)
	remote := &fakeRemoteSyncManager{
		files: map[string][]syncmanager.FileInfo{
			"card-time/DCIM": {
				{Name: "IMG_0003.JPG", Path: "card-time/DCIM/IMG_0003.JPG", ModTime: newest},
				{Name: "IMG_0001.JPG", Path: "card-time/DCIM/IMG_0001.JPG", ModTime: oldest},
				{Name: "IMG_0002.JPG", Path: "card-time/DCIM/IMG_0002.JPG", ModTime: middle},
			},
		},
		contents: map[string]string{
			"card-time/DCIM/IMG_0001.JPG": "jpeg 1",
			"card-time/DCIM/IMG_0002.JPG": "jpeg 2",
			"card-time/DCIM/IMG_0003.JPG": "jpeg 3",
		},
	}

	var batchFilenames []string
	client := newTestClient(t, func(req *http.Request) (*http.Response, error) {
		switch {
		case req.Method == http.MethodGet && req.URL.Path == "/v1/albums":
			return jsonResponse(http.StatusOK, `{}`), nil
		case req.Method == http.MethodPost && req.URL.Path == "/v1/albums":
			return jsonResponse(http.StatusOK, `{"id":"album-1","title":"Card time"}`), nil
		case req.Method == http.MethodPost && req.URL.Path == "/v1/uploads":
			filename := req.Header.Get("X-Goog-Upload-File-Name")
			return textResponse(http.StatusOK, "upload-token-"+filename), nil
		case req.Method == http.MethodPost && req.URL.Path == "/v1/mediaItems:batchCreate":
			var batch BatchCreateRequest
			if err := json.NewDecoder(req.Body).Decode(&batch); err != nil {
				t.Fatalf("failed to decode batch create request: %v", err)
			}
			for _, item := range batch.NewMediaItems {
				batchFilenames = append(batchFilenames, item.SimpleMediaItem.FileName)
			}
			return jsonResponse(http.StatusOK, `{"newMediaItemResults":[{"status":{"code":0}},{"status":{"code":0}},{"status":{"code":0}}]}`), nil
		default:
			t.Fatalf("unexpected Google Photos request: %s %s", req.Method, req.URL.String())
			return nil, nil
		}
	})

	manager := NewSyncManager(client, remote)
	manager.progress = &SyncProgress{}
	uploaded, skipped, failed, err := manager.syncCard(context.Background(), "time", "card-time")
	if err != nil {
		t.Fatalf("syncCard returned error: %v", err)
	}
	if uploaded != 3 || skipped != 0 || failed != 0 {
		t.Fatalf("syncCard counts = uploaded %d skipped %d failed %d, want 3 0 0", uploaded, skipped, failed)
	}
	if got, want := strings.Join(batchFilenames, ","), "IMG_0001.JPG,IMG_0002.JPG,IMG_0003.JPG"; got != want {
		t.Fatalf("batch create filenames = %q, want %q", got, want)
	}
}

func TestSyncCardUploadsMediaInParallelAndAddsAlbumInModTimeOrder(t *testing.T) {
	times := []time.Time{
		time.Date(2026, 1, 2, 9, 0, 0, 0, time.UTC),
		time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC),
		time.Date(2026, 1, 2, 11, 0, 0, 0, time.UTC),
		time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC),
	}
	remote := &fakeRemoteSyncManager{
		files: map[string][]syncmanager.FileInfo{
			"card-parallel/DCIM": {
				{Name: "IMG_0004.JPG", Path: "card-parallel/DCIM/IMG_0004.JPG", ModTime: times[3]},
				{Name: "IMG_0002.JPG", Path: "card-parallel/DCIM/IMG_0002.JPG", ModTime: times[1]},
				{Name: "IMG_0001.JPG", Path: "card-parallel/DCIM/IMG_0001.JPG", ModTime: times[0]},
				{Name: "IMG_0003.JPG", Path: "card-parallel/DCIM/IMG_0003.JPG", ModTime: times[2]},
			},
		},
		contents: map[string]string{
			"card-parallel/DCIM/IMG_0001.JPG": "jpeg 1",
			"card-parallel/DCIM/IMG_0002.JPG": "jpeg 2",
			"card-parallel/DCIM/IMG_0003.JPG": "jpeg 3",
			"card-parallel/DCIM/IMG_0004.JPG": "jpeg 4",
		},
	}

	uploadStarted := make(chan string, 4)
	releaseUploads := make(chan struct{})
	sawConcurrentUploads := make(chan struct{})
	var once sync.Once
	var batchMu sync.Mutex
	var batchFilenames []string

	client := newTestClient(t, func(req *http.Request) (*http.Response, error) {
		switch {
		case req.Method == http.MethodGet && req.URL.Path == "/v1/albums":
			return jsonResponse(http.StatusOK, `{}`), nil
		case req.Method == http.MethodPost && req.URL.Path == "/v1/albums":
			return jsonResponse(http.StatusOK, `{"id":"album-1","title":"Card parallel"}`), nil
		case req.Method == http.MethodPost && req.URL.Path == "/v1/uploads":
			filename := req.Header.Get("X-Goog-Upload-File-Name")
			uploadStarted <- filename
			if len(uploadStarted) >= 2 {
				once.Do(func() { close(sawConcurrentUploads) })
			}
			<-releaseUploads
			return textResponse(http.StatusOK, "upload-token-"+filename), nil
		case req.Method == http.MethodPost && req.URL.Path == "/v1/mediaItems:batchCreate":
			var batch BatchCreateRequest
			if err := json.NewDecoder(req.Body).Decode(&batch); err != nil {
				t.Fatalf("failed to decode batch create request: %v", err)
			}
			batchMu.Lock()
			for _, item := range batch.NewMediaItems {
				batchFilenames = append(batchFilenames, item.SimpleMediaItem.FileName)
			}
			batchMu.Unlock()
			return jsonResponse(http.StatusOK, `{"newMediaItemResults":[{"status":{"code":0}},{"status":{"code":0}},{"status":{"code":0}},{"status":{"code":0}}]}`), nil
		default:
			t.Fatalf("unexpected Google Photos request: %s %s", req.Method, req.URL.String())
			return nil, nil
		}
	})

	manager := NewSyncManager(client, remote)
	manager.progress = &SyncProgress{}
	done := make(chan error, 1)
	go func() {
		uploaded, skipped, failed, err := manager.syncCard(context.Background(), "parallel", "card-parallel")
		if err != nil {
			done <- err
			return
		}
		if uploaded != 4 || skipped != 0 || failed != 0 {
			done <- fmt.Errorf("syncCard counts = uploaded %d skipped %d failed %d, want 4 0 0", uploaded, skipped, failed)
			return
		}
		done <- nil
	}()

	select {
	case <-sawConcurrentUploads:
	case err := <-done:
		t.Fatalf("syncCard finished before concurrent uploads were observed: %v", err)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for concurrent uploads")
	}
	close(releaseUploads)

	if err := <-done; err != nil {
		t.Fatalf("syncCard returned error: %v", err)
	}

	batchMu.Lock()
	got := strings.Join(batchFilenames, ",")
	batchMu.Unlock()
	if want := "IMG_0001.JPG,IMG_0002.JPG,IMG_0003.JPG,IMG_0004.JPG"; got != want {
		t.Fatalf("batch create filenames = %q, want %q", got, want)
	}
}

func TestSyncCardCreatesMultipleBatchesBeyondGoogleLimit(t *testing.T) {
	const fileCount = 72
	files := make([]syncmanager.FileInfo, 0, fileCount)
	contents := make(map[string]string, fileCount)
	for i := 0; i < fileCount; i++ {
		name := fmt.Sprintf("IMG_%04d.JPG", i+1)
		path := "card-many/DCIM/" + name
		files = append(files, syncmanager.FileInfo{
			Name:    name,
			Path:    path,
			Size:    int64(len(name)),
			ModTime: time.Date(2026, 1, 2, 9, i, 0, 0, time.UTC),
		})
		contents[path] = "jpeg " + name
	}

	remote := &fakeRemoteSyncManager{
		files: map[string][]syncmanager.FileInfo{
			"card-many/DCIM": files,
		},
		contents: contents,
	}

	var batchSizes []int
	client := newTestClient(t, func(req *http.Request) (*http.Response, error) {
		switch {
		case req.Method == http.MethodGet && req.URL.Path == "/v1/albums":
			return jsonResponse(http.StatusOK, `{}`), nil
		case req.Method == http.MethodPost && req.URL.Path == "/v1/albums":
			return jsonResponse(http.StatusOK, `{"id":"album-1","title":"Card many"}`), nil
		case req.Method == http.MethodPost && req.URL.Path == "/v1/uploads":
			return textResponse(http.StatusOK, "upload-token-"+req.Header.Get("X-Goog-Upload-File-Name")), nil
		case req.Method == http.MethodPost && req.URL.Path == "/v1/mediaItems:batchCreate":
			var batch BatchCreateRequest
			if err := json.NewDecoder(req.Body).Decode(&batch); err != nil {
				t.Fatalf("failed to decode batch create request: %v", err)
			}
			if len(batch.NewMediaItems) > 50 {
				t.Fatalf("batch size = %d, Google Photos maximum is 50", len(batch.NewMediaItems))
			}
			batchSizes = append(batchSizes, len(batch.NewMediaItems))
			results := make([]map[string]any, len(batch.NewMediaItems))
			for i := range results {
				results[i] = map[string]any{"status": map[string]any{"code": 0}}
			}
			body, err := json.Marshal(map[string]any{"newMediaItemResults": results})
			if err != nil {
				t.Fatalf("failed to marshal batch response: %v", err)
			}
			return jsonResponse(http.StatusOK, string(body)), nil
		default:
			t.Fatalf("unexpected Google Photos request: %s %s", req.Method, req.URL.String())
			return nil, nil
		}
	})

	manager := NewSyncManager(client, remote)
	manager.progress = &SyncProgress{}
	uploaded, skipped, failed, err := manager.syncCard(context.Background(), "many", "card-many")
	if err != nil {
		t.Fatalf("syncCard returned error: %v", err)
	}
	if uploaded != fileCount || skipped != 0 || failed != 0 {
		t.Fatalf("syncCard counts = uploaded %d skipped %d failed %d, want %d 0 0", uploaded, skipped, failed, fileCount)
	}
	if len(batchSizes) < 2 {
		t.Fatalf("batchCreate calls = %d, want multiple calls for %d files", len(batchSizes), fileCount)
	}
	total := 0
	for _, size := range batchSizes {
		total += size
	}
	if total != fileCount {
		t.Fatalf("total batch items = %d, want %d (batch sizes %v)", total, fileCount, batchSizes)
	}
}

func TestSortMediaForUploadUsesModTime(t *testing.T) {
	oldest := time.Date(2026, 1, 2, 9, 0, 0, 0, time.UTC)
	middle := time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC)
	newest := time.Date(2026, 1, 2, 11, 0, 0, 0, time.UTC)
	files := []syncmanager.FileInfo{
		{Name: "IMG_0003.JPG", Path: "card/DCIM/IMG_0003.JPG", Size: 1000, ModTime: newest},
		{Name: "IMG_0001.JPG", Path: "card/DCIM/IMG_0001.JPG", Size: 5000, ModTime: oldest},
		{Name: "DJI_0002.MOV", Path: "card/DCIM/DJI_0002.MOV", Size: 2000, ModTime: middle},
		{Name: "IMG_0002.JPG", Path: "card/DCIM/IMG_0002.JPG", Size: 1000, ModTime: middle},
	}

	sortMediaForUpload(files)

	var names []string
	for _, file := range files {
		names = append(names, file.Name)
	}
	if got, want := strings.Join(names, ","), "IMG_0001.JPG,DJI_0002.MOV,IMG_0002.JPG,IMG_0003.JPG"; got != want {
		t.Fatalf("sorted files = %q, want %q", got, want)
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
