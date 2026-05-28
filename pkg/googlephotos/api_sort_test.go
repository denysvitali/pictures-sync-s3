package googlephotos

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

func TestSortAlbumByShootTime_AlreadySorted(t *testing.T) {
	items := []*MediaItem{
		{ID: "item-1", Filename: "a.jpg", MediaMetadata: MediaMetadata{CreationTime: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)}},
		{ID: "item-2", Filename: "b.jpg", MediaMetadata: MediaMetadata{CreationTime: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)}},
		{ID: "item-3", Filename: "c.jpg", MediaMetadata: MediaMetadata{CreationTime: time.Date(2024, 1, 3, 0, 0, 0, 0, time.UTC)}},
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/mediaItems:search":
			json.NewEncoder(w).Encode(ListMediaItemsResponse{MediaItems: items})
		default:
			http.Error(w, fmt.Sprintf("unexpected path: %s", r.URL.Path), http.StatusNotFound)
		}
	})

	_, cleanup := setupRemoveTestServer(t, handler)
	defer cleanup()
	client := newRemoveTestClient(t)

	progress, err := client.SortAlbumByShootTime(context.Background(), "album-1", nil)
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

	var addedIDs []string
	var deletedOldAlbum bool

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/mediaItems:search":
			json.NewEncoder(w).Encode(ListMediaItemsResponse{MediaItems: items})

		case r.URL.Path == "/v1/albums/album-1" && r.Method == "GET":
			json.NewEncoder(w).Encode(Album{ID: "album-1", Title: "card-ABC"})

		case r.URL.Path == "/v1/albums" && r.Method == "POST":
			json.NewEncoder(w).Encode(Album{ID: "new-album-id", Title: "card-ABC (sorted)"})

		case r.URL.Path == "/v1/albums/new-album-id:batchAddMediaItems":
			var req BatchAddMediaItemsRequest
			json.NewDecoder(r.Body).Decode(&req)
			addedIDs = append(addedIDs, req.MediaItemIds...)
			json.NewEncoder(w).Encode("{}")

		case r.URL.Path == "/v1/albums/album-1" && r.Method == "DELETE":
			deletedOldAlbum = true
			json.NewEncoder(w).Encode("{}")

		default:
			http.Error(w, fmt.Sprintf("unexpected: %s %s", r.Method, r.URL.Path), http.StatusNotFound)
		}
	})

	_, cleanup := setupRemoveTestServer(t, handler)
	defer cleanup()
	client := newRemoveTestClient(t)

	progress, err := client.SortAlbumByShootTime(context.Background(), "album-1", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if progress.Status != "completed" {
		t.Errorf("expected status completed, got %s", progress.Status)
	}
	if progress.AddedItems != 3 {
		t.Errorf("expected 3 items added, got %d", progress.AddedItems)
	}
	if progress.NewAlbumID != "new-album-id" {
		t.Errorf("expected new album ID new-album-id, got %s", progress.NewAlbumID)
	}
	if !deletedOldAlbum {
		t.Error("expected old album to be deleted")
	}

	// Verify items were added in chronological order.
	expectedOrder := []string{"item-old", "item-mid", "item-new"}
	if len(addedIDs) != len(expectedOrder) {
		t.Fatalf("expected %d items added, got %d", len(expectedOrder), len(addedIDs))
	}
	for i, id := range expectedOrder {
		if addedIDs[i] != id {
			t.Errorf("position %d: expected %s, got %s", i, id, addedIDs[i])
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

	progress, err := client.SortAlbumByShootTime(context.Background(), "album-1", nil)
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
		default:
			http.Error(w, "ok", http.StatusOK)
		}
	})

	_, cleanup := setupRemoveTestServer(t, handler)
	defer cleanup()
	client := newRemoveTestClient(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	_, err := client.SortAlbumByShootTime(ctx, "album-1", nil)
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
		case r.URL.Path == "/v1/albums/album-1" && r.Method == "GET":
			json.NewEncoder(w).Encode(Album{ID: "album-1", Title: "card-X"})
		case r.URL.Path == "/v1/albums" && r.Method == "POST":
			json.NewEncoder(w).Encode(Album{ID: "new-id", Title: "card-X (sorted)"})
		case r.URL.Path == "/v1/albums/new-id:batchAddMediaItems":
			json.NewEncoder(w).Encode("{}")
		case r.URL.Path == "/v1/albums/album-1" && r.Method == "DELETE":
			json.NewEncoder(w).Encode("{}")
		default:
			http.Error(w, "ok", http.StatusOK)
		}
	})

	_, cleanup := setupRemoveTestServer(t, handler)
	defer cleanup()
	client := newRemoveTestClient(t)

	var statuses []string
	progressFn := func(p SortProgress) {
		statuses = append(statuses, p.Status)
	}

	_, err := client.SortAlbumByShootTime(context.Background(), "album-1", progressFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

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

	var addedIDs []string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/mediaItems:search":
			json.NewEncoder(w).Encode(ListMediaItemsResponse{MediaItems: items})
		case r.URL.Path == "/v1/albums/album-1" && r.Method == "GET":
			json.NewEncoder(w).Encode(Album{ID: "album-1", Title: "test"})
		case r.URL.Path == "/v1/albums" && r.Method == "POST":
			json.NewEncoder(w).Encode(Album{ID: "new-id", Title: "test (sorted)"})
		case r.URL.Path == "/v1/albums/new-id:batchAddMediaItems":
			var req BatchAddMediaItemsRequest
			json.NewDecoder(r.Body).Decode(&req)
			addedIDs = append(addedIDs, req.MediaItemIds...)
			json.NewEncoder(w).Encode("{}")
		case r.URL.Path == "/v1/albums/album-1" && r.Method == "DELETE":
			json.NewEncoder(w).Encode("{}")
		default:
			http.Error(w, "ok", http.StatusOK)
		}
	})

	_, cleanup := setupRemoveTestServer(t, handler)
	defer cleanup()
	client := newRemoveTestClient(t)

	_, err := client.SortAlbumByShootTime(context.Background(), "album-1", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// known.jpg (has time) should come before unknown.jpg (zero time).
	if len(addedIDs) != 2 {
		t.Fatalf("expected 2 items added, got %d", len(addedIDs))
	}
	if addedIDs[0] != "item-known" {
		t.Errorf("expected item-known first, got %s", addedIDs[0])
	}
	if addedIDs[1] != "item-unknown" {
		t.Errorf("expected item-unknown second, got %s", addedIDs[1])
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

	var addCallCount atomic.Int32
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/mediaItems:search":
			json.NewEncoder(w).Encode(ListMediaItemsResponse{MediaItems: items})
		case r.URL.Path == "/v1/albums/album-1" && r.Method == "GET":
			json.NewEncoder(w).Encode(Album{ID: "album-1", Title: "test"})
		case r.URL.Path == "/v1/albums" && r.Method == "POST":
			json.NewEncoder(w).Encode(Album{ID: "new-id", Title: "test (sorted)"})
		case r.URL.Path == "/v1/albums/new-id:batchAddMediaItems":
			addCallCount.Add(1)
			json.NewEncoder(w).Encode("{}")
		case r.URL.Path == "/v1/albums/album-1" && r.Method == "DELETE":
			json.NewEncoder(w).Encode("{}")
		default:
			http.Error(w, "ok", http.StatusOK)
		}
	})

	_, cleanup := setupRemoveTestServer(t, handler)
	defer cleanup()
	client := newRemoveTestClient(t)

	progress, err := client.SortAlbumByShootTime(context.Background(), "album-1", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if progress.AddedItems != n {
		t.Errorf("expected %d items added, got %d", n, progress.AddedItems)
	}

	// Should be at least 2 batch add calls (maxBatchSize + 5 items).
	if count := addCallCount.Load(); count < 2 {
		t.Errorf("expected at least 2 batch add calls, got %d", count)
	}
}
