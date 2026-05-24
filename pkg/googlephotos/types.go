package googlephotos

import (
	"strings"
	"time"
)

// Album represents a Google Photos album
type Album struct {
	ID                    string `json:"id"`
	Title                 string `json:"title"`
	ProductURL            string `json:"productUrl"`
	IsWriteable           bool   `json:"isWriteable,omitempty"`
	MediaItemsCount       string `json:"mediaItemsCount,omitempty"`
	CoverPhotoBaseURL     string `json:"coverPhotoBaseUrl,omitempty"`
	CoverPhotoMediaItemID string `json:"coverPhotoMediaItemId,omitempty"`
}

// NewAlbum represents a newly created album request/response
type NewAlbum struct {
	Title string `json:"title"`
}

// MediaItem represents a media item in Google Photos
type MediaItem struct {
	ID            string        `json:"id"`
	Description   string        `json:"description,omitempty"`
	BaseURL       string        `json:"baseUrl"`
	MimeType      string        `json:"mimeType"`
	MediaMetadata MediaMetadata `json:"mediaMetadata,omitempty"`
	Filename      string        `json:"filename"`
}

// MediaMetadata contains metadata about a media item
type MediaMetadata struct {
	CreationTime time.Time `json:"creationTime,omitempty"`
	Width        string    `json:"width,omitempty"`
	Height       string    `json:"height,omitempty"`
	Photo        *Photo    `json:"photo,omitempty"`
	Video        *Video    `json:"video,omitempty"`
}

// Photo contains photo-specific metadata
type Photo struct {
	CameraMake      string `json:"cameraMake,omitempty"`
	CameraModel     string `json:"cameraModel,omitempty"`
	FocalLength     float64 `json:"focalLength,omitempty"`
	ApertureFNumber float64 `json:"apertureFNumber,omitempty"`
	IsoEquivalent   int    `json:"isoEquivalent,omitempty"`
	ExposureTime    string `json:"exposureTime,omitempty"`
}

// Video contains video-specific metadata
type Video struct {
	CameraMake  string `json:"cameraMake,omitempty"`
	CameraModel string `json:"cameraModel,omitempty"`
	FPS         float64 `json:"fps,omitempty"`
	Status      string `json:"status,omitempty"`
}

// SimpleMediaItem is used for batch creation of media items
type SimpleMediaItem struct {
	UploadToken string `json:"uploadToken"`
	FileName    string `json:"fileName,omitempty"`
}

// NewMediaItem represents a media item to be created in a batch
type NewMediaItem struct {
	Description     string           `json:"description,omitempty"`
	SimpleMediaItem *SimpleMediaItem `json:"simpleMediaItem"`
}

// BatchCreateRequest is the request body for batch creating media items
type BatchCreateRequest struct {
	AlbumID       string          `json:"albumId,omitempty"`
	NewMediaItems []*NewMediaItem `json:"newMediaItems"`
	AlbumPosition *AlbumPosition  `json:"albumPosition,omitempty"`
}

// BatchCreateResponse is the response from batch creating media items
type BatchCreateResponse struct {
	NewMediaItemResults []*NewMediaItemResult `json:"newMediaItemResults"`
}

// NewMediaItemResult represents the result of creating a single media item
type NewMediaItemResult struct {
	UploadToken  string      `json:"uploadToken"`
	Status       *Status     `json:"status,omitempty"`
	MediaItem    *MediaItem  `json:"mediaItem,omitempty"`
}

// Status represents the status of an API operation
type Status struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// AlbumPosition specifies where to place media items in an album
type AlbumPosition struct {
	Position string `json:"position,omitempty"`
}

// ListAlbumsResponse is the response from listing albums
type ListAlbumsResponse struct {
	Albums        []*Album `json:"albums,omitempty"`
	NextPageToken string   `json:"nextPageToken,omitempty"`
}

// OAuthToken represents stored OAuth tokens
type OAuthToken struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	TokenType    string    `json:"token_type"`
	Expiry       time.Time `json:"expiry"`
}

// TokenResponse is the response from the token endpoint
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	Scope        string `json:"scope,omitempty"`
}

// AuthState holds the PKCE state for OAuth flow
type AuthState struct {
	CodeVerifier  string
	State         string
	RedirectURI   string
	ExpiresAt     time.Time
}

// CardError tracks an error for a specific card during sync
type CardError struct {
	CardID string `json:"card_id"`
	Error  string `json:"error"`
}

// SyncProgress tracks the progress of a B2 to Google Photos sync
type SyncProgress struct {
	TotalCards      int         `json:"total_cards"`
	CurrentCard     int         `json:"current_card"`
	CurrentCardID   string      `json:"current_card_id"`
	TotalFiles      int         `json:"total_files"`
	ProcessedFiles  int         `json:"processed_files"`
	UploadedFiles   int         `json:"uploaded_files"`
	SkippedFiles    int         `json:"skipped_files"`
	FailedFiles     int         `json:"failed_files"`
	CurrentFile     string      `json:"current_file,omitempty"`
	Status          string      `json:"status"`
	Error           string      `json:"error,omitempty"`
	CardErrors      []CardError `json:"card_errors,omitempty"`
}

// ConnectionStatus represents the Google Photos connection status
type ConnectionStatus struct {
	Connected    bool   `json:"connected"`
	AlbumsCount  int    `json:"albums_count"`
	Email        string `json:"email,omitempty"`
}

// IsPhotoOrVideo returns true if the file extension is a photo or video (not RAW)
func IsPhotoOrVideo(filename string) bool {
	ext := lowerExt(filename)
	return photoVideoExts[ext]
}

// IsRAW returns true if the file extension is a RAW format
func IsRAW(filename string) bool {
	ext := lowerExt(filename)
	return rawExts[ext]
}

func lowerExt(filename string) string {
	for i := len(filename) - 1; i >= 0; i-- {
		if filename[i] == '.' {
			return strings.ToLower(filename[i:])
		}
	}
	return ""
}

var photoVideoExts = map[string]bool{
	".jpg":  true, ".jpeg": true, ".png":  true, ".gif":  true,
	".heic": true, ".heif": true, ".webp": true, ".bmp":  true,
	".tiff": true, ".tif":  true, ".mp4":  true, ".mov":  true,
	".avi":  true, ".mkv":  true, ".wmv":  true, ".flv":  true,
	".m4v":  true, ".3gp":  true,
}

var rawExts = map[string]bool{
	".cr2":  true, ".cr3":  true, ".nef":  true, ".arw":  true,
	".dng":  true, ".raf":  true, ".orf":  true, ".rw2":  true,
	".pef":  true, ".srw":  true, ".3fr":  true, ".erf":  true,
	".mef":  true, ".mos":  true, ".raw":  true, ".nrw":  true,
}
