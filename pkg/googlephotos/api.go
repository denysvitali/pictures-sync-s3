package googlephotos

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
)

const maxBatchSize = 50

// UploadMediaReaderContext uploads media from a reader to Google Photos and returns an upload token.
func (c *Client) UploadMediaReaderContext(ctx context.Context, r io.Reader, size int64, filename string) (string, error) {
	resp, err := c.doUploadRequestContext(ctx, r, size, filename)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	// Upload tokens are small strings; 1 MB is more than sufficient.
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("failed to read upload response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", newAPIError("upload", resp.StatusCode, body)
	}

	return string(body), nil
}

// CreateAlbumContext creates a new album in Google Photos.
func (c *Client) CreateAlbumContext(ctx context.Context, title string) (*Album, error) {
	reqBody := map[string]interface{}{
		"album": map[string]string{
			"title": title,
		},
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal album request: %w", err)
	}

	resp, err := c.doRequestContext(ctx, "POST", "/albums", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// Album metadata is small JSON; 10 MB is a safe ceiling.
	body, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return nil, fmt.Errorf("failed to read album response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, newAPIError("createAlbum", resp.StatusCode, body)
	}

	var album Album
	if err := json.Unmarshal(body, &album); err != nil {
		return nil, fmt.Errorf("failed to parse album response: %w", err)
	}

	return &album, nil
}

// ListAlbumsContext lists all albums in the user's Google Photos library.
func (c *Client) ListAlbumsContext(ctx context.Context) ([]*Album, error) {
	var allAlbums []*Album
	pageToken := ""
	seenTokens := make(map[string]struct{})

	for {
		path := "/albums?pageSize=50"
		if pageToken != "" {
			path += "&pageToken=" + pageToken
		}

		resp, err := c.doRequestContext(ctx, "GET", path, nil)
		if err != nil {
			return nil, err
		}

		// A page of album metadata (50 albums) is well under 10 MB.
		body, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("failed to read albums response: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			return nil, newAPIError("listAlbums", resp.StatusCode, body)
		}

		var result ListAlbumsResponse
		if err := json.Unmarshal(body, &result); err != nil {
			return nil, fmt.Errorf("failed to parse albums response: %w", err)
		}

		allAlbums = append(allAlbums, result.Albums...)

		if result.NextPageToken == "" {
			break
		}
		if _, dup := seenTokens[result.NextPageToken]; dup {
			return nil, fmt.Errorf("list albums: pagination loop detected at token %q", result.NextPageToken)
		}
		seenTokens[result.NextPageToken] = struct{}{}
		pageToken = result.NextPageToken
	}

	return allAlbums, nil
}

// FindAlbumByTitleContext finds an album by its title, stopping pagination as
// soon as a match is found. For users with many albums this avoids paginating
// the entire library when the target sits on an early page.
func (c *Client) FindAlbumByTitleContext(ctx context.Context, title string) (*Album, error) {
	pageToken := ""
	seenTokens := make(map[string]struct{})
	for {
		path := "/albums?pageSize=50"
		if pageToken != "" {
			path += "&pageToken=" + pageToken
		}
		resp, err := c.doRequestContext(ctx, "GET", path, nil)
		if err != nil {
			return nil, err
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("failed to read albums response: %w", err)
		}
		if resp.StatusCode != http.StatusOK {
			return nil, newAPIError("listAlbums", resp.StatusCode, body)
		}
		var result ListAlbumsResponse
		if err := json.Unmarshal(body, &result); err != nil {
			return nil, fmt.Errorf("failed to parse albums response: %w", err)
		}
		for _, album := range result.Albums {
			if album.Title == title {
				return album, nil
			}
		}
		if result.NextPageToken == "" {
			return nil, nil
		}
		if _, dup := seenTokens[result.NextPageToken]; dup {
			return nil, fmt.Errorf("find album by title: pagination loop detected at token %q", result.NextPageToken)
		}
		seenTokens[result.NextPageToken] = struct{}{}
		pageToken = result.NextPageToken
	}
}

// chunkSlice invokes fn on consecutive slices of items of at most size elements.
// Returns the first error fn returns, stopping iteration.
func chunkSlice[T any](items []T, size int, fn func(chunk []T) error) error {
	for i := 0; i < len(items); i += size {
		end := i + size
		if end > len(items) {
			end = len(items)
		}
		if err := fn(items[i:end]); err != nil {
			return err
		}
	}
	return nil
}

// BatchCreateMediaItemsContext creates multiple media items in a single request.
// If more than 50 items are provided they are sent in multiple chunked requests.
func (c *Client) BatchCreateMediaItemsContext(ctx context.Context, albumID string, items []*NewMediaItem) (*BatchCreateResponse, error) {
	var combined BatchCreateResponse
	err := chunkSlice(items, maxBatchSize, func(chunk []*NewMediaItem) error {
		reqBody := BatchCreateRequest{NewMediaItems: chunk}
		if albumID != "" {
			reqBody.AlbumID = albumID
			reqBody.AlbumPosition = &AlbumPosition{Position: "LAST_IN_ALBUM"}
		}

		jsonBody, err := json.Marshal(reqBody)
		if err != nil {
			return fmt.Errorf("failed to marshal batch create request: %w", err)
		}

		resp, err := c.doRequestContext(ctx, "POST", "/mediaItems:batchCreate", bytes.NewReader(jsonBody))
		if err != nil {
			return err
		}

		// A batch create response for 50 items is small JSON; 10 MB ceiling.
		body, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
		resp.Body.Close()
		if err != nil {
			return fmt.Errorf("failed to read batch create response: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			return newAPIError("batchCreate", resp.StatusCode, body)
		}

		var result BatchCreateResponse
		if err := json.Unmarshal(body, &result); err != nil {
			return fmt.Errorf("failed to parse batch create response: %w", err)
		}
		combined.NewMediaItemResults = append(combined.NewMediaItemResults, result.NewMediaItemResults...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &combined, nil
}

// ListAlbumMediaItems lists all media items in a specific album.
func (c *Client) ListAlbumMediaItems(ctx context.Context, albumID string) ([]*MediaItem, error) {
	var allItems []*MediaItem
	pageToken := ""
	seenTokens := make(map[string]struct{})

	for {
		path := "/mediaItems:search?pageSize=100"
		if pageToken != "" {
			path += "&pageToken=" + pageToken
		}

		reqBody := map[string]string{"albumId": albumID}
		jsonBody, err := json.Marshal(reqBody)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal search request: %w", err)
		}

		resp, err := c.doRequestContext(ctx, "POST", path, bytes.NewReader(jsonBody))
		if err != nil {
			return nil, err
		}

		// Each page holds up to 100 media item records; 10 MB is a safe bound.
		body, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("failed to read media items response: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			return nil, newAPIError("search", resp.StatusCode, body)
		}

		var result ListMediaItemsResponse
		if err := json.Unmarshal(body, &result); err != nil {
			return nil, fmt.Errorf("failed to parse media items response: %w", err)
		}

		allItems = append(allItems, result.MediaItems...)

		if result.NextPageToken == "" {
			break
		}
		if _, dup := seenTokens[result.NextPageToken]; dup {
			return nil, fmt.Errorf("list album media items: pagination loop detected at token %q", result.NextPageToken)
		}
		seenTokens[result.NextPageToken] = struct{}{}
		pageToken = result.NextPageToken
	}

	return allItems, nil
}

// ListAlbumMediaItemsPage returns up to pageSize media items from the first
// page of an album's contents using a single search request. It is intended for
// cheap previews where paginating the entire album would be wasteful.
func (c *Client) ListAlbumMediaItemsPage(ctx context.Context, albumID string, pageSize int) ([]*MediaItem, error) {
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 100
	}

	reqBody := map[string]string{"albumId": albumID}
	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal search request: %w", err)
	}

	resp, err := c.doRequestContext(ctx, "POST", fmt.Sprintf("/mediaItems:search?pageSize=%d", pageSize), bytes.NewReader(jsonBody))
	if err != nil {
		return nil, err
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	resp.Body.Close()
	if err != nil {
		return nil, fmt.Errorf("failed to read media items response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, newAPIError("search", resp.StatusCode, body)
	}

	var result ListMediaItemsResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse media items response: %w", err)
	}

	if len(result.MediaItems) > pageSize {
		result.MediaItems = result.MediaItems[:pageSize]
	}
	return result.MediaItems, nil
}

// BatchRemoveMediaItems removes media items from an album.
// Requests are automatically chunked to respect the 50-item API limit.
func (c *Client) BatchRemoveMediaItems(ctx context.Context, albumID string, mediaItemIds []string) error {
	return c.BatchRemoveMediaItemsWithProgress(ctx, albumID, mediaItemIds, nil)
}

// BatchRemoveMediaItemsWithProgress removes media items from an album with
// per-chunk progress callbacks. onProgress is invoked after each 50-item
// chunk completes with the cumulative removed count and total.
func (c *Client) BatchRemoveMediaItemsWithProgress(ctx context.Context, albumID string, mediaItemIds []string, onProgress func(removed, total int)) error {
	removed := 0
	return chunkSlice(mediaItemIds, maxBatchSize, func(chunk []string) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		reqBody := BatchRemoveMediaItemsRequest{MediaItemIds: chunk}
		jsonBody, err := json.Marshal(reqBody)
		if err != nil {
			return fmt.Errorf("failed to marshal batch remove request: %w", err)
		}

		resp, err := c.doRequestContext(ctx, "POST", fmt.Sprintf("/albums/%s:batchRemoveMediaItems", albumID), bytes.NewReader(jsonBody))
		if err != nil {
			return err
		}

		// Batch remove response is minimal JSON; 1 MB is more than enough.
		body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		if err != nil {
			return fmt.Errorf("failed to read batch remove response: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			return newAPIError("batchRemoveMediaItems", resp.StatusCode, body)
		}

		removed += len(chunk)
		if onProgress != nil {
			onProgress(removed, len(mediaItemIds))
		}
		return nil
	})
}

// BatchAddMediaItemsContext adds existing media items (by ID) to an album.
// Items are appended in the order given. Requests are chunked to respect the
// 50-item API limit.
func (c *Client) BatchAddMediaItemsContext(ctx context.Context, albumID string, mediaItemIds []string) error {
	return c.BatchAddMediaItemsWithProgress(ctx, albumID, mediaItemIds, nil)
}

// BatchAddMediaItemsWithProgress adds existing media items to an album with
// per-chunk progress callbacks. onProgress is invoked after each 50-item chunk
// completes with the cumulative added count and total.
func (c *Client) BatchAddMediaItemsWithProgress(ctx context.Context, albumID string, mediaItemIds []string, onProgress func(added, total int)) error {
	added := 0
	return chunkSlice(mediaItemIds, maxBatchSize, func(chunk []string) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		reqBody := BatchAddMediaItemsRequest{MediaItemIds: chunk}
		jsonBody, err := json.Marshal(reqBody)
		if err != nil {
			return fmt.Errorf("failed to marshal batch add request: %w", err)
		}

		resp, err := c.doRequestContext(ctx, "POST", fmt.Sprintf("/albums/%s:batchAddMediaItems", albumID), bytes.NewReader(jsonBody))
		if err != nil {
			return err
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		if err != nil {
			return fmt.Errorf("failed to read batch add response: %w", err)
		}
		if resp.StatusCode != http.StatusOK {
			return newAPIError("batchAddMediaItems", resp.StatusCode, body)
		}

		added += len(chunk)
		if onProgress != nil {
			onProgress(added, len(mediaItemIds))
		}
		return nil
	})
}

// DeleteAlbumContext deletes an album created by the app.
// UpdateAlbumTitleContext renames an album the app created via albums.patch.
func (c *Client) UpdateAlbumTitleContext(ctx context.Context, albumID, title string) (*Album, error) {
	reqBody := map[string]interface{}{
		"title": title,
	}
	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal album patch request: %w", err)
	}

	resp, err := c.doRequestContext(ctx, "PATCH", "/albums/"+albumID+"?updateMask=title", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, err
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	resp.Body.Close()
	if err != nil {
		return nil, fmt.Errorf("failed to read album patch response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, newAPIError("patchAlbum", resp.StatusCode, body)
	}

	var album Album
	if err := json.Unmarshal(body, &album); err != nil {
		return nil, fmt.Errorf("failed to parse album patch response: %w", err)
	}
	return &album, nil
}

func (c *Client) DeleteAlbumContext(ctx context.Context, albumID string) error {
	resp, err := c.doRequestContext(ctx, "DELETE", "/albums/"+albumID, nil)
	if err != nil {
		return err
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	resp.Body.Close()
	if err != nil {
		return fmt.Errorf("failed to read delete album response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return newAPIError("deleteAlbum", resp.StatusCode, body)
	}
	return nil
}

// SortProgress tracks the progress of an album sort-by-shoot-time operation.
type SortProgress struct {
	Status       string `json:"status"` // "listing", "sorting", "creating-album", "adding", "deleting-old", "completed", "error"
	TotalItems   int    `json:"total_items"`
	CurrentItem  int    `json:"current_item"`
	AddedItems   int    `json:"added_items"`
	Inaccessible int    `json:"inaccessible,omitempty"` // items in the album the app cannot see (uploaded by a different OAuth client)
	NewAlbumID   string `json:"new_album_id,omitempty"`
	Error        string `json:"error,omitempty"`
}

// SortAlbumByShootTime reorders all media items in an album by photo shoot time
// (MediaMetadata.CreationTime). It creates a new album with the items in
// chronological order, then deletes the old album. This avoids re-uploading
// bytes — it uses batchAddMediaItems to place existing library items into
// the new album in the desired order.
//
// onProgress is called periodically with progress updates (may be nil).
func (c *Client) SortAlbumByShootTime(ctx context.Context, albumID string, onProgress func(SortProgress)) (SortProgress, error) {
	report := func(p SortProgress) {
		if onProgress != nil {
			onProgress(p)
		}
	}

	// Phase 1: list all items.
	report(SortProgress{Status: "listing"})
	items, err := c.ListAlbumMediaItems(ctx, albumID)
	if err != nil {
		p := SortProgress{Status: "error", Error: err.Error()}
		report(p)
		return p, fmt.Errorf("list album items: %w", err)
	}

	// Phase 1b: verify the app can see every item before doing anything
	// destructive. The Library API only returns app-created media (scope
	// readonly.appcreateddata), so items uploaded by a different OAuth client
	// (e.g. an rclone googlephotos remote using its own client_id) are invisible
	// here. Album metadata still reports the true total, so a mismatch means a
	// sort would silently drop the inaccessible items — refuse and leave the
	// album untouched rather than delete it.
	oldAlbum, err := c.GetAlbumContext(ctx, albumID)
	if err != nil {
		p := SortProgress{Status: "error", TotalItems: len(items), Error: err.Error()}
		report(p)
		return p, fmt.Errorf("get album: %w", err)
	}
	if reported, perr := strconv.Atoi(strings.TrimSpace(oldAlbum.MediaItemsCount)); perr == nil && reported > len(items) {
		inaccessible := reported - len(items)
		msg := fmt.Sprintf("album reports %d items but only %d are accessible to this app; %d item(s) were uploaded by a different OAuth client and would be lost by a sort. Aborted — album left unchanged. Re-upload those items through this app's client (or unify the rclone/app client_id) and retry.", reported, len(items), inaccessible)
		p := SortProgress{Status: "error", TotalItems: len(items), Inaccessible: inaccessible, Error: msg}
		report(p)
		log.Printf("[GooglePhotos] Album %s sort aborted: %s", albumID, msg)
		return p, fmt.Errorf("sort aborted: %s", msg)
	}

	if len(items) <= 1 {
		p := SortProgress{Status: "completed", TotalItems: len(items)}
		report(p)
		return p, nil
	}

	// Phase 2: sort by shoot time.
	report(SortProgress{Status: "sorting", TotalItems: len(items)})
	sorted := make([]*MediaItem, len(items))
	copy(sorted, items)
	sort.SliceStable(sorted, func(i, j int) bool {
		ti := sorted[i].MediaMetadata.CreationTime
		tj := sorted[j].MediaMetadata.CreationTime
		if ti.IsZero() && tj.IsZero() {
			return sorted[i].ID < sorted[j].ID
		}
		if ti.IsZero() {
			return false
		}
		if tj.IsZero() {
			return true
		}
		return ti.Before(tj)
	})

	// Check if already sorted.
	alreadySorted := true
	for i := range items {
		if items[i].ID != sorted[i].ID {
			alreadySorted = false
			break
		}
	}
	if alreadySorted {
		p := SortProgress{Status: "completed", TotalItems: len(items)}
		report(p)
		log.Printf("[GooglePhotos] Album %s already sorted by shoot time", albumID)
		return p, nil
	}

	// Phase 3: create a new sorted album (old album metadata already fetched above).
	report(SortProgress{Status: "creating-album", TotalItems: len(items)})
	newTitle := oldAlbum.Title + " (sorted)"
	newAlbum, err := c.CreateAlbumContext(ctx, newTitle)
	if err != nil {
		p := SortProgress{Status: "error", TotalItems: len(items), Error: err.Error()}
		report(p)
		return p, fmt.Errorf("create sorted album: %w", err)
	}

	// Phase 4: add items to new album in sorted order.
	sortedIDs := make([]string, len(sorted))
	for i, item := range sorted {
		sortedIDs[i] = item.ID
	}
	total := len(sortedIDs)
	report(SortProgress{Status: "adding", TotalItems: total, NewAlbumID: newAlbum.ID})
	addErr := c.BatchAddMediaItemsWithProgress(ctx, newAlbum.ID, sortedIDs, func(added, t int) {
		report(SortProgress{Status: "adding", TotalItems: t, AddedItems: added, NewAlbumID: newAlbum.ID})
	})
	if addErr != nil {
		p := SortProgress{Status: "error", TotalItems: total, NewAlbumID: newAlbum.ID, Error: addErr.Error()}
		report(p)
		return p, fmt.Errorf("batch add to new album: %w", addErr)
	}

	// Phase 5: delete the old unsorted album, then rename the new album back to
	// the original title so the sort looks in-place to the user.
	report(SortProgress{Status: "deleting-old", TotalItems: total, AddedItems: total, NewAlbumID: newAlbum.ID})
	deleteOK := true
	if err := c.DeleteAlbumContext(ctx, albumID); err != nil {
		deleteOK = false
		log.Printf("[GooglePhotos] warning: failed to delete old album %s: %v", albumID, err)
		// Non-fatal — the sorted album is already created.
	}

	// Only reclaim the original title once the old album is gone, to avoid two
	// albums sharing a name.
	if deleteOK {
		if _, err := c.UpdateAlbumTitleContext(ctx, newAlbum.ID, oldAlbum.Title); err != nil {
			log.Printf("[GooglePhotos] warning: failed to rename sorted album %s to %q: %v", newAlbum.ID, oldAlbum.Title, err)
			// Non-fatal — items are sorted, the album just keeps the "(sorted)" title.
		}
	}

	p := SortProgress{Status: "completed", TotalItems: total, AddedItems: total, NewAlbumID: newAlbum.ID}
	report(p)
	log.Printf("[GooglePhotos] Album %s sorted by shoot time: created %s with %d items", albumID, newAlbum.ID, total)
	return p, nil
}

// GetAlbumContext fetches a single album by ID.
func (c *Client) GetAlbumContext(ctx context.Context, albumID string) (*Album, error) {
	resp, err := c.doRequestContext(ctx, "GET", "/albums/"+albumID, nil)
	if err != nil {
		return nil, err
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	resp.Body.Close()
	if err != nil {
		return nil, fmt.Errorf("failed to read album response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, newAPIError("getAlbum", resp.StatusCode, body)
	}
	var album Album
	if err := json.Unmarshal(body, &album); err != nil {
		return nil, fmt.Errorf("failed to parse album response: %w", err)
	}
	return &album, nil
}
