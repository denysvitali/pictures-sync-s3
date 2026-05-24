package googlephotos

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// UploadMedia uploads media bytes to Google Photos and returns an upload token
func (c *Client) UploadMedia(data []byte, filename string) (string, error) {
	resp, err := c.doUploadRequest(data, filename)
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
	reqBody := map[string]interface{}{
		"album": map[string]string{
			"title": title,
		},
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal album request: %w", err)
	}

	resp, err := c.doRequest("POST", "/albums", bytes.NewReader(jsonBody))
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
	var allAlbums []*Album
	pageToken := ""

	for {
		path := "/albums?pageSize=50"
		if pageToken != "" {
			path += "&pageToken=" + pageToken
		}

		resp, err := c.doRequest("GET", path, nil)
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
	albums, err := c.ListAlbums()
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

	resp, err := c.doRequest("POST", "/mediaItems:batchCreate", bytes.NewReader(jsonBody))
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
