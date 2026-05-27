package googlephotos

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// UploadMedia uploads media bytes to Google Photos and returns an upload token
func (c *Client) UploadMedia(data []byte, filename string) (string, error) {
	return c.UploadMediaReaderContext(context.Background(), bytes.NewReader(data), int64(len(data)), filename)
}

// UploadMediaReader uploads media from a reader to Google Photos and returns an upload token.
func (c *Client) UploadMediaReader(r io.Reader, size int64, filename string) (string, error) {
	return c.UploadMediaReaderContext(context.Background(), r, size, filename)
}

// UploadMediaReaderContext uploads media from a reader to Google Photos and returns an upload token.
func (c *Client) UploadMediaReaderContext(ctx context.Context, r io.Reader, size int64, filename string) (string, error) {
	resp, err := c.doUploadRequestContext(ctx, r, size, filename)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read upload response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("upload failed (%d): %s", resp.StatusCode, string(body))
	}

	return string(body), nil
}

// CreateAlbum creates a new album in Google Photos
func (c *Client) CreateAlbum(title string) (*Album, error) {
	return c.CreateAlbumContext(context.Background(), title)
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

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read album response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("create album failed (%d): %s", resp.StatusCode, string(body))
	}

	var album Album
	if err := json.Unmarshal(body, &album); err != nil {
		return nil, fmt.Errorf("failed to parse album response: %w", err)
	}

	return &album, nil
}

// ListAlbums lists all albums in the user's Google Photos library
func (c *Client) ListAlbums() ([]*Album, error) {
	return c.ListAlbumsContext(context.Background())
}

// ListAlbumsContext lists all albums in the user's Google Photos library.
func (c *Client) ListAlbumsContext(ctx context.Context) ([]*Album, error) {
	var allAlbums []*Album
	pageToken := ""

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
			return nil, fmt.Errorf("list albums failed (%d): %s", resp.StatusCode, string(body))
		}

		var result ListAlbumsResponse
		if err := json.Unmarshal(body, &result); err != nil {
			return nil, fmt.Errorf("failed to parse albums response: %w", err)
		}

		allAlbums = append(allAlbums, result.Albums...)

		if result.NextPageToken == "" {
			break
		}
		pageToken = result.NextPageToken
	}

	return allAlbums, nil
}

// FindAlbumByTitle finds an album by its title
func (c *Client) FindAlbumByTitle(title string) (*Album, error) {
	return c.FindAlbumByTitleContext(context.Background(), title)
}

// FindAlbumByTitleContext finds an album by its title.
func (c *Client) FindAlbumByTitleContext(ctx context.Context, title string) (*Album, error) {
	albums, err := c.ListAlbumsContext(ctx)
	if err != nil {
		return nil, err
	}

	for _, album := range albums {
		if album.Title == title {
			return album, nil
		}
	}

	return nil, nil
}

// BatchCreateMediaItems creates multiple media items in a single request
func (c *Client) BatchCreateMediaItems(albumID string, items []*NewMediaItem) (*BatchCreateResponse, error) {
	return c.BatchCreateMediaItemsContext(context.Background(), albumID, items)
}

// BatchCreateMediaItemsContext creates multiple media items in a single request.
func (c *Client) BatchCreateMediaItemsContext(ctx context.Context, albumID string, items []*NewMediaItem) (*BatchCreateResponse, error) {
	reqBody := BatchCreateRequest{
		NewMediaItems: items,
	}
	if albumID != "" {
		reqBody.AlbumID = albumID
		reqBody.AlbumPosition = &AlbumPosition{Position: "LAST_IN_ALBUM"}
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal batch create request: %w", err)
	}

	resp, err := c.doRequestContext(ctx, "POST", "/mediaItems:batchCreate", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read batch create response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("batch create failed (%d): %s", resp.StatusCode, string(body))
	}

	var result BatchCreateResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse batch create response: %w", err)
	}

	return &result, nil
}

// ListAlbumMediaItems lists all media items in a specific album.
func (c *Client) ListAlbumMediaItems(ctx context.Context, albumID string) ([]*MediaItem, error) {
	var allItems []*MediaItem
	pageToken := ""

	for {
		path := fmt.Sprintf("/mediaItems:search?pageSize=100")
		if pageToken != "" {
			path = fmt.Sprintf("/mediaItems:search?pageSize=100&pageToken=%s", pageToken)
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

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("failed to read media items response: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("list album media items failed (%d): %s", resp.StatusCode, string(body))
		}

		var result ListMediaItemsResponse
		if err := json.Unmarshal(body, &result); err != nil {
			return nil, fmt.Errorf("failed to parse media items response: %w", err)
		}

		allItems = append(allItems, result.MediaItems...)

		if result.NextPageToken == "" {
			break
		}
		pageToken = result.NextPageToken
	}

	return allItems, nil
}

// BatchRemoveMediaItems removes media items from an album.
func (c *Client) BatchRemoveMediaItems(ctx context.Context, albumID string, mediaItemIds []string) error {
	reqBody := BatchRemoveMediaItemsRequest{MediaItemIds: mediaItemIds}
	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal batch remove request: %w", err)
	}

	resp, err := c.doRequestContext(ctx, "POST", fmt.Sprintf("/albums/%s:batchRemoveMediaItems", albumID), bytes.NewReader(jsonBody))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read batch remove response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("batch remove failed (%d): %s", resp.StatusCode, string(body))
	}

	return nil
}
