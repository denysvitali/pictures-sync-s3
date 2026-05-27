package googlephotos

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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
