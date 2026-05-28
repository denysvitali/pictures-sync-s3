package googlephotos

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

// mockDownloader implements FileDownloader for testing.
type mockDownloader struct {
	files map[string][]byte
}

func (d *mockDownloader) GetFile(path string, w io.Writer) error {
	data, ok := d.files[path]
	if !ok {
		return fmt.Errorf("file not found: %s", path)
	}
	_, err := w.Write(data)
	return err
}

func TestSortAlbumByShootTime_AlreadySorted(t *testing.T) {
	items := []*MediaItem{
		{ID: "item-1", Filename: "a.jpg", MediaMetadata: MediaMetadata{CreationTime: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)}},
		{ID: "item-2", Filename: "b.jpg", MediaMetadata: MediaMetadata{CreationTime: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)}},
		{ID: "item-3", Filename: "c.jpg", MediaMetadata: MediaMetadata{CreationTime: time.Date(2024, 1, 3, 0, 0, 0, 0, time.UTC)}},
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/mediaItems:search":
			resp := ListMediaItemsResponse{MediaItems: items}
			json.NewEncoder(w).Encode(resp)
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	})

	_, cleanup := setupRemoveTestServer(t, handler)
	defer cleanup()
	client := newRemoveTestClient(t)

	dl := &mockDownloader{files: map[string][]byte{}}
	progress, err := client.SortAlbumByShootTime(context.Background(), "album-1", dl, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if progress.Status != "completed" {
		t.Errorf("expected status completed, got %s", progress.Status)
	}
	if progress.TotalItems != 3 {
		t.Errorf("expected 3 total items, got %d", progress.TotalItems)
	}
}

func TestSortAlbumByShootTime_Reorders(t *testing.T) {
	// Items are out of order: newest first.
	items := []*MediaItem{
		{ID: "item-new", Filename: "new.jpg", MediaMetadata: MediaMetadata{CreationTime: time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)}},
		{ID: "item-old", Filename: "old.jpg", MediaMetadata: MediaMetadata{CreationTime: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)}},
		{ID: "item-mid", Filename: "mid.jpg", MediaMetadata: MediaMetadata{CreationTime: time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)}},
	}

	var removedIDs []string
	var createdBatches [][]*NewMediaItem

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/mediaItems:search":
			json.NewEncoder(w).Encode(ListMediaItemsResponse{MediaItems: items})

		case r.URL.Path == "/v1/albums/album-1:batchRemoveMediaItems":
			var req BatchRemoveMediaItemsRequest
			json.NewDecoder(r.Body).Decode(&req)
			removedIDs = append(removedIDs, req.MediaItemIds...)
			json.NewEncoder(w).Encode(BatchRemoveMediaItemsResponse{})

		case r.URL.Path == "/v1/uploads":
			// Return a mock upload token.
			fmt.Fprint(w, "upload-token-"+r.Header.Get("X-Goog-Upload-File-Name"))

		case r.URL.Path == "/v1/mediaItems:batchCreate":
			var req BatchCreateRequest
			json.NewDecoder(r.Body).Decode(&req)
			createdBatches = append(createdBatches, req.NewMediaItems)
			// Return a success result for each item.
			results := make([]*NewMediaItemResult, len(req.NewMediaItems))
			for i := range results {
				results[i] = &NewMediaItemResult{
					UploadToken: req.NewMediaItems[i].SimpleMediaItem.UploadToken,
					MediaItem:   &MediaItem{ID: "new-" + req.NewMediaItems[i].SimpleMediaItem.FileName},
				}
			}
			json.NewEncoder(w).Encode(BatchCreateResponse{NewMediaItemResults: results})

		default:
			http.Error(w, fmt.Sprintf("unexpected path: %s", r.URL.Path), http.StatusNotFound)
		}
	})

	_, cleanup := setupRemoveTestServer(t, handler)
	defer cleanup()
	client := newRemoveTestClient(t)

	dl := &mockDownloader{files: map[string][]byte{
		"old.jpg": []byte("old-data"),
		"mid.jpg": []byte("mid-data"),
		"new.jpg": []byte("new-data"),
	}}

	progress, err := client.SortAlbumByShootTime(context.Background(), "album-1", dl, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if progress.Status != "completed" {
		t.Errorf("expected status completed, got %s", progress.Status)
	}
	if len(removedIDs) != 3 {
		t.Errorf("expected 3 items removed, got %d", len(removedIDs))
	}
	if progress.AddedItems != 3 {
		t.Errorf("expected 3 items added, got %d", progress.AddedItems)
	}

	// Verify items were created in chronological order.
	var createdOrder []string
	for _, batch := range createdBatches {
		for _, item := range batch {
			createdOrder = append(createdOrder, item.SimpleMediaItem.FileName)
		}
	}
	expectedOrder := []string{"old.jpg", "mid.jpg", "new.jpg"}
	if len(createdOrder) != len(expectedOrder) {
		t.Fatalf("expected %d items created, got %d", len(expectedOrder), len(createdOrder))
	}
	for i, name := range expectedOrder {
		if createdOrder[i] != name {
			t.Errorf("position %d: expected %s, got %s", i, name, createdOrder[i])
		}
	}
}

func TestSortAlbumByShootTime_SingleItem(t *testing.T) {
	items := []*MediaItem{
		{ID: "item-1", Filename: "a.jpg", MediaMetadata: MediaMetadata{CreationTime: time.Now()}},
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(ListMediaItemsResponse{MediaItems: items})
	})

	_, cleanup := setupRemoveTestServer(t, handler)
	defer cleanup()
	client := newRemoveTestClient(t)

	dl := &mockDownloader{files: map[string][]byte{}}
	progress, err := client.SortAlbumByShootTime(context.Background(), "album-1", dl, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if progress.Status != "completed" {
		t.Errorf("expected status completed, got %s", progress.Status)
	}
}

func TestSortAlbumByShootTime_ContextCancelled(t *testing.T) {
	items := make([]*MediaItem, 10)
	for i := range items {
		items[i] = &MediaItem{
			ID:       fmt.Sprintf("item-%d", i),
			Filename: fmt.Sprintf("img%d.jpg", i),
			MediaMetadata: MediaMetadata{
				CreationTime: time.Date(2024, 1, i+1, 0, 0, 0, 0, time.UTC),
			},
		}
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/mediaItems:search":
			json.NewEncoder(w).Encode(ListMediaItemsResponse{MediaItems: items})
		case r.URL.Path == "/v1/albums/album-1:batchRemoveMediaItems":
			json.NewEncoder(w).Encode(BatchRemoveMediaItemsResponse{})
		default:
			http.Error(w, "ok", http.StatusOK)
		}
	})

	_, cleanup := setupRemoveTestServer(t, handler)
	defer cleanup()
	client := newRemoveTestClient(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	dl := &mockDownloader{files: map[string][]byte{
		"img0.jpg": []byte("data"),
	}}
	_, err := client.SortAlbumByShootTime(ctx, "album-1", dl, nil)
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
}

func TestSortAlbumByShootTime_ProgressCallback(t *testing.T) {
	items := []*MediaItem{
		{ID: "item-1", Filename: "b.jpg", MediaMetadata: MediaMetadata{CreationTime: time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)}},
		{ID: "item-2", Filename: "a.jpg", MediaMetadata: MediaMetadata{CreationTime: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)}},
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/mediaItems:search":
			json.NewEncoder(w).Encode(ListMediaItemsResponse{MediaItems: items})
		case r.URL.Path == "/v1/albums/album-1:batchRemoveMediaItems":
			json.NewEncoder(w).Encode(BatchRemoveMediaItemsResponse{})
		case r.URL.Path == "/v1/uploads":
			fmt.Fprint(w, "upload-token-"+r.Header.Get("X-Goog-Upload-File-Name"))
		case r.URL.Path == "/v1/mediaItems:batchCreate":
			var req BatchCreateRequest
			json.NewDecoder(r.Body).Decode(&req)
			results := make([]*NewMediaItemResult, len(req.NewMediaItems))
			for i := range results {
				results[i] = &NewMediaItemResult{UploadToken: "tok"}
			}
			json.NewEncoder(w).Encode(BatchCreateResponse{NewMediaItemResults: results})
		default:
			http.Error(w, "ok", http.StatusOK)
		}
	})

	_, cleanup := setupRemoveTestServer(t, handler)
	defer cleanup()
	client := newRemoveTestClient(t)

	dl := &mockDownloader{files: map[string][]byte{
		"a.jpg": []byte("a-data"),
		"b.jpg": []byte("b-data"),
	}}

	var statuses []string
	progressFn := func(p SortProgress) {
		statuses = append(statuses, p.Status)
	}

	_, err := client.SortAlbumByShootTime(context.Background(), "album-1", dl, progressFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should see: listing, sorting, removing, re-adding (possibly multiple), completed.
	hasCompleted := false
	for _, s := range statuses {
		if s == "completed" {
			hasCompleted = true
		}
	}
	if !hasCompleted {
		t.Errorf("expected completed status in progress callbacks, got: %v", statuses)
	}
}

func TestSortAlbumByShootTime_ZeroCreationTime(t *testing.T) {
	// Items with zero creation time should sort after items with valid times.
	items := []*MediaItem{
		{ID: "item-unknown", Filename: "unknown.jpg", MediaMetadata: MediaMetadata{}},
		{ID: "item-known", Filename: "known.jpg", MediaMetadata: MediaMetadata{CreationTime: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)}},
	}

	var createdOrder []string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/mediaItems:search":
			json.NewEncoder(w).Encode(ListMediaItemsResponse{MediaItems: items})
		case r.URL.Path == "/v1/albums/album-1:batchRemoveMediaItems":
			json.NewEncoder(w).Encode(BatchRemoveMediaItemsResponse{})
		case r.URL.Path == "/v1/uploads":
			fmt.Fprint(w, "upload-token-"+r.Header.Get("X-Goog-Upload-File-Name"))
		case r.URL.Path == "/v1/mediaItems:batchCreate":
			var req BatchCreateRequest
			json.NewDecoder(r.Body).Decode(&req)
			for _, item := range req.NewMediaItems {
				createdOrder = append(createdOrder, item.SimpleMediaItem.FileName)
			}
			results := make([]*NewMediaItemResult, len(req.NewMediaItems))
			for i := range results {
				results[i] = &NewMediaItemResult{UploadToken: "tok"}
			}
			json.NewEncoder(w).Encode(BatchCreateResponse{NewMediaItemResults: results})
		default:
			http.Error(w, "ok", http.StatusOK)
		}
	})

	_, cleanup := setupRemoveTestServer(t, handler)
	defer cleanup()
	client := newRemoveTestClient(t)

	dl := &mockDownloader{files: map[string][]byte{
		"known.jpg":   []byte("data"),
		"unknown.jpg": []byte("data"),
	}}

	_, err := client.SortAlbumByShootTime(context.Background(), "album-1", dl, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// known.jpg (has time) should come before unknown.jpg (zero time).
	if len(createdOrder) != 2 {
		t.Fatalf("expected 2 items created, got %d", len(createdOrder))
	}
	if createdOrder[0] != "known.jpg" {
		t.Errorf("expected known.jpg first, got %s", createdOrder[0])
	}
	if createdOrder[1] != "unknown.jpg" {
		t.Errorf("expected unknown.jpg second, got %s", createdOrder[1])
	}
}

func TestOriginalFilename(t *testing.T) {
	tests := []struct {
		item     *MediaItem
		expected string
	}{
		{&MediaItem{Filename: "IMG_001.jpg"}, "IMG_001.jpg"},
		{&MediaItem{ID: "abc123"}, "abc123.jpg"},
		{&MediaItem{ID: "abc123", Filename: "photo.png"}, "photo.png"},
	}
	for _, tt := range tests {
		got := originalFilename(tt.item)
		if got != tt.expected {
			t.Errorf("originalFilename(%+v) = %q, want %q", tt.item, got, tt.expected)
		}
	}
}

func TestSortAlbumByShootTime_BatchesCorrectly(t *testing.T) {
	// Create more items than maxBatchSize, in reverse order so sort must reorder.
	n := maxBatchSize + 5
	items := make([]*MediaItem, n)
	for i := range items {
		items[i] = &MediaItem{
			ID:       fmt.Sprintf("item-%d", i),
			Filename: fmt.Sprintf("img%03d.jpg", i),
			MediaMetadata: MediaMetadata{
				CreationTime: time.Date(2024, 1, 1, 0, 0, n-i, 0, time.UTC),
			},
		}
	}

	var createCallCount atomic.Int32
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/mediaItems:search":
			json.NewEncoder(w).Encode(ListMediaItemsResponse{MediaItems: items})
		case r.URL.Path == "/v1/albums/album-1:batchRemoveMediaItems":
			json.NewEncoder(w).Encode(BatchRemoveMediaItemsResponse{})
		case r.URL.Path == "/v1/uploads":
			fmt.Fprint(w, "upload-token-"+r.Header.Get("X-Goog-Upload-File-Name"))
		case r.URL.Path == "/v1/mediaItems:batchCreate":
			createCallCount.Add(1)
			var req BatchCreateRequest
			json.NewDecoder(r.Body).Decode(&req)
			results := make([]*NewMediaItemResult, len(req.NewMediaItems))
			for i := range results {
				results[i] = &NewMediaItemResult{UploadToken: "tok"}
			}
			json.NewEncoder(w).Encode(BatchCreateResponse{NewMediaItemResults: results})
		default:
			http.Error(w, "ok", http.StatusOK)
		}
	})

	_, cleanup := setupRemoveTestServer(t, handler)
	defer cleanup()
	client := newRemoveTestClient(t)

	files := make(map[string][]byte)
	for i := range items {
		files[items[i].Filename] = []byte("data")
	}
	dl := &mockDownloader{files: files}

	progress, err := client.SortAlbumByShootTime(context.Background(), "album-1", dl, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if progress.AddedItems != len(items) {
		t.Errorf("expected %d items added, got %d", len(items), progress.AddedItems)
	}

	// Should be at least 2 batch create calls (maxBatchSize + 5 items).
	if count := createCallCount.Load(); count < 2 {
		t.Errorf("expected at least 2 batch create calls, got %d", count)
	}
}
