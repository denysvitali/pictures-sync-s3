// Package cloudphotos provides functionality for browsing and downloading photos from cloud storage.
// It integrates with rclone to list photos organized by card ID, supports local caching for
// performance, and handles photo retrieval from remote storage backends.
package cloudphotos

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/denysvitali/pictures-sync-s3/pkg/syncmanager"
	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/config"
	"github.com/rclone/rclone/fs/config/configfile"
	"github.com/rclone/rclone/fs/walk"

	// Import storage backends - same as syncmanager
	_ "github.com/rclone/rclone/backend/b2"
	_ "github.com/rclone/rclone/backend/s3"
	_ "github.com/rclone/rclone/backend/drive"
	_ "github.com/rclone/rclone/backend/azureblob"
	_ "github.com/rclone/rclone/backend/local"
)

const (
	// CacheDir is the directory where downloaded photos are cached
	CacheDir = "/perm/pictures-sync/photo-cache"

	// CacheMaxSize is the maximum cache size in bytes (500MB)
	CacheMaxSize = 500 * 1024 * 1024

	// CacheTTL is how long cached files remain valid
	CacheTTL = 24 * time.Hour
)

// PhotoInfo represents a photo file in cloud storage
type PhotoInfo struct {
	Name        string    `json:"name"`          // Filename (e.g., "IMG_1234.JPG")
	Path        string    `json:"path"`          // Full path relative to photos root
	Size        int64     `json:"size"`          // File size in bytes
	SizeHuman   string    `json:"size_human"`    // Human-readable size
	ModTime     time.Time `json:"mod_time"`      // Last modification time
	CardID      string    `json:"card_id"`       // Card ID this photo belongs to
	IsImage     bool      `json:"is_image"`      // True if file is an image
	IsCached    bool      `json:"is_cached"`     // True if file is in local cache
	ThumbnailURL string   `json:"thumbnail_url,omitempty"` // URL to thumbnail
	DownloadURL  string   `json:"download_url,omitempty"`  // URL to download
}

// CardPhotos represents photos organized by card ID
type CardPhotos struct {
	CardID      string      `json:"card_id"`
	PhotoCount  int         `json:"photo_count"`
	TotalSize   int64       `json:"total_size"`
	TotalSizeHuman string   `json:"total_size_human"`
	LastModified time.Time  `json:"last_modified"`
	Photos      []PhotoInfo `json:"photos"`
}

// Manager manages cloud photo operations
type Manager struct {
	configPath string
	remoteName string
	remotePath string
	cacheDir   string
	mu         sync.RWMutex
	syncMgr    *syncmanager.Manager
}

// NewManager creates a new cloud photos manager
func NewManager(configPath, remoteName, remotePath string, syncMgr *syncmanager.Manager) (*Manager, error) {
	m := &Manager{
		configPath: configPath,
		remoteName: remoteName,
		remotePath: remotePath,
		cacheDir:   CacheDir,
		syncMgr:    syncMgr,
	}

	// Ensure cache directory exists
	if err := os.MkdirAll(m.cacheDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create cache directory: %w", err)
	}

	// Clean up old cache entries on startup
	go m.cleanupCache()

	return m, nil
}

// ListCards returns all card IDs with their photo counts and sizes
func (m *Manager) ListCards() ([]CardPhotos, error) {
	// Use syncmanager's ListCardIDs which already implements this
	cardDirs, err := m.syncMgr.ListCardIDs()
	if err != nil {
		return nil, fmt.Errorf("failed to list card IDs: %w", err)
	}

	cards := make([]CardPhotos, 0, len(cardDirs))
	for _, cardDir := range cardDirs {
		cardID := cardDir.Name

		// List DCIM folder for this card
		dcimPath := filepath.Join(cardID, "DCIM")
		photos, err := m.listPhotosRecursive(dcimPath, cardID)
		if err != nil {
			log.Printf("Warning: failed to list photos for card %s: %v", cardID, err)
			continue
		}

		if len(photos) == 0 {
			continue
		}

		// Calculate total size and find last modified
		var totalSize int64
		var lastModified time.Time
		for _, photo := range photos {
			totalSize += photo.Size
			if photo.ModTime.After(lastModified) {
				lastModified = photo.ModTime
			}
		}

		cards = append(cards, CardPhotos{
			CardID:         cardID,
			PhotoCount:     len(photos),
			TotalSize:      totalSize,
			TotalSizeHuman: formatSize(totalSize),
			LastModified:   lastModified,
			Photos:         photos,
		})
	}

	// Sort by last modified (most recent first)
	sort.Slice(cards, func(i, j int) bool {
		return cards[i].LastModified.After(cards[j].LastModified)
	})

	return cards, nil
}

// ListPhotos lists all photos for a specific card ID
func (m *Manager) ListPhotos(cardID string) ([]PhotoInfo, error) {
	// Validate cardID
	if err := validateCardID(cardID); err != nil {
		return nil, err
	}

	// List DCIM folder for this card
	dcimPath := filepath.Join(cardID, "DCIM")
	return m.listPhotosRecursive(dcimPath, cardID)
}

// listPhotosRecursive lists all photos recursively in a path
func (m *Manager) listPhotosRecursive(path string, cardID string) ([]PhotoInfo, error) {
	// Load rclone config
	if err := config.SetConfigPath(m.configPath); err != nil {
		return nil, fmt.Errorf("failed to set config path: %w", err)
	}
	configfile.Install()
	storage := &configfile.Storage{}
	if err := storage.Load(); err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	ctx := context.Background()

	// Construct full remote path
	fullPath := m.remoteName + ":" + filepath.Join(m.remotePath, path)

	// Create remote filesystem
	fsys, err := fs.NewFs(ctx, fullPath)
	if err != nil {
		return nil, fmt.Errorf("failed to access remote path: %w", err)
	}

	var photos []PhotoInfo

	// Walk the directory tree using rclone's walk package
	err = walk.Walk(ctx, fsys, "", true, -1, func(dirPath string, entries fs.DirEntries, err error) error {
		if err != nil {
			return err
		}

		for _, entry := range entries {
			switch obj := entry.(type) {
			case fs.Object:
				name := obj.Remote()

				// Only include image files
				if !isImageFile(name) {
					continue
				}

				// Construct full path relative to photos root
				fullRelPath := filepath.Join(path, name)

				// Check if file is cached
				isCached := m.isFileCached(fullRelPath)

				photos = append(photos, PhotoInfo{
					Name:      filepath.Base(name),
					Path:      fullRelPath,
					Size:      obj.Size(),
					SizeHuman: formatSize(obj.Size()),
					ModTime:   obj.ModTime(ctx),
					CardID:    cardID,
					IsImage:   true,
					IsCached:  isCached,
				})
			}
		}
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to walk directory: %w", err)
	}

	// Sort by modification time (most recent first)
	sort.Slice(photos, func(i, j int) bool {
		return photos[i].ModTime.After(photos[j].ModTime)
	})

	return photos, nil
}

// DownloadPhoto downloads a photo from cloud storage to local cache
// Returns the path to the cached file
func (m *Manager) DownloadPhoto(photoPath string) (string, error) {
	// Validate path
	if strings.Contains(photoPath, "..") {
		return "", fmt.Errorf("invalid path: contains directory traversal")
	}

	// Clean the path
	photoPath = filepath.Clean(photoPath)
	photoPath = strings.TrimPrefix(photoPath, "/")

	// Check if already cached
	cachedPath := m.getCachePath(photoPath)
	if m.isFileCached(photoPath) {
		// Update access time
		os.Chtimes(cachedPath, time.Now(), time.Now())
		return cachedPath, nil
	}

	// Ensure cache directory structure exists
	cacheDir := filepath.Dir(cachedPath)
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create cache directory: %w", err)
	}

	// Create temporary file for download
	tmpPath := cachedPath + ".tmp"
	tmpFile, err := os.Create(tmpPath)
	if err != nil {
		return "", fmt.Errorf("failed to create temporary file: %w", err)
	}
	defer func() {
		tmpFile.Close()
		// Clean up tmp file if it still exists (failed download)
		os.Remove(tmpPath)
	}()

	// Download file using syncmanager's GetFile
	if err := m.syncMgr.GetFile(photoPath, tmpFile); err != nil {
		return "", fmt.Errorf("failed to download file: %w", err)
	}

	// Close the file before rename
	if err := tmpFile.Close(); err != nil {
		return "", fmt.Errorf("failed to close temporary file: %w", err)
	}

	// Atomic rename to final location
	if err := os.Rename(tmpPath, cachedPath); err != nil {
		return "", fmt.Errorf("failed to rename file: %w", err)
	}

	log.Printf("Downloaded photo to cache: %s", photoPath)

	// Clean up cache if it's getting too large
	go m.cleanupCache()

	return cachedPath, nil
}

// GetCachedPhoto returns the path to a cached photo, or empty string if not cached
func (m *Manager) GetCachedPhoto(photoPath string) string {
	if m.isFileCached(photoPath) {
		return m.getCachePath(photoPath)
	}
	return ""
}

// isFileCached checks if a file exists in cache and is still valid
func (m *Manager) isFileCached(photoPath string) bool {
	cachedPath := m.getCachePath(photoPath)

	stat, err := os.Stat(cachedPath)
	if err != nil {
		return false
	}

	// Check if cache entry is still valid (TTL)
	if time.Since(stat.ModTime()) > CacheTTL {
		// Cache entry expired
		return false
	}

	return true
}

// getCachePath returns the local cache path for a given remote photo path
func (m *Manager) getCachePath(photoPath string) string {
	// Use hash of path to avoid filesystem issues with special characters
	hash := sha256.Sum256([]byte(photoPath))
	hashStr := hex.EncodeToString(hash[:])

	// Keep original extension
	ext := filepath.Ext(photoPath)

	// Create cache path: cache/ab/cd/abcd...hash.jpg
	return filepath.Join(m.cacheDir, hashStr[0:2], hashStr[2:4], hashStr+ext)
}

// cleanupCache removes old or expired cache entries to keep cache size under limit
func (m *Manager) cleanupCache() {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Get all cached files with their info
	type cacheEntry struct {
		path    string
		size    int64
		modTime time.Time
	}

	var entries []cacheEntry
	var totalSize int64

	// Walk cache directory
	filepath.Walk(m.cacheDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			return nil
		}

		entries = append(entries, cacheEntry{
			path:    path,
			size:    info.Size(),
			modTime: info.ModTime(),
		})
		totalSize += info.Size()

		return nil
	})

	// Sort by modification time (oldest first)
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].modTime.Before(entries[j].modTime)
	})

	// Remove expired entries
	now := time.Now()
	for _, entry := range entries {
		if now.Sub(entry.modTime) > CacheTTL {
			log.Printf("Removing expired cache entry: %s", entry.path)
			os.Remove(entry.path)
			totalSize -= entry.size
		}
	}

	// Remove oldest entries if cache is too large
	if totalSize > CacheMaxSize {
		log.Printf("Cache size (%d bytes) exceeds limit (%d bytes), cleaning up...", totalSize, CacheMaxSize)

		for _, entry := range entries {
			if totalSize <= CacheMaxSize {
				break
			}

			// Check if file is still expired (already removed)
			if _, err := os.Stat(entry.path); os.IsNotExist(err) {
				continue
			}

			log.Printf("Removing old cache entry to free space: %s", entry.path)
			if err := os.Remove(entry.path); err == nil {
				totalSize -= entry.size
			}
		}
	}

	log.Printf("Cache cleanup complete. Current size: %d bytes (%d MB)", totalSize, totalSize/(1024*1024))
}

// ClearCache removes all cached photos
func (m *Manager) ClearCache() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := os.RemoveAll(m.cacheDir); err != nil {
		return fmt.Errorf("failed to clear cache: %w", err)
	}

	if err := os.MkdirAll(m.cacheDir, 0755); err != nil {
		return fmt.Errorf("failed to recreate cache directory: %w", err)
	}

	log.Printf("Cache cleared successfully")
	return nil
}

// GetCacheStats returns information about the cache
func (m *Manager) GetCacheStats() (map[string]interface{}, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var totalSize int64
	var fileCount int

	err := filepath.Walk(m.cacheDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() {
			totalSize += info.Size()
			fileCount++
		}
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to calculate cache stats: %w", err)
	}

	return map[string]interface{}{
		"total_size":       totalSize,
		"total_size_human": formatSize(totalSize),
		"file_count":       fileCount,
		"max_size":         CacheMaxSize,
		"max_size_human":   formatSize(CacheMaxSize),
		"usage_percent":    float64(totalSize) / float64(CacheMaxSize) * 100,
		"ttl_hours":        CacheTTL.Hours(),
	}, nil
}

// SetRemote updates the remote name and path
func (m *Manager) SetRemote(remoteName, remotePath string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.remoteName = remoteName
	m.remotePath = remotePath
}

// validateCardID checks if a card ID is safe to use in paths
func validateCardID(cardID string) error {
	if cardID == "" {
		return fmt.Errorf("card ID cannot be empty")
	}

	// Check for path traversal attempts
	if strings.Contains(cardID, "..") || strings.Contains(cardID, "/") || strings.Contains(cardID, "\\") {
		return fmt.Errorf("card ID contains invalid characters")
	}

	// Note: We're more lenient here than syncmanager since we're also reading
	// card IDs from sync history which might have different formats
	return nil
}

// isImageFile checks if a filename is an image file
func isImageFile(filename string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	imageExts := map[string]bool{
		".jpg":  true,
		".jpeg": true,
		".png":  true,
		".gif":  true,
		".bmp":  true,
		".tiff": true,
		".tif":  true,
		".webp": true,
		".heic": true,
		".heif": true,
		".raw":  true,
		".cr2":  true,
		".nef":  true,
		".arw":  true,
		".dng":  true,
	}
	return imageExts[ext]
}

// formatSize formats bytes into human-readable size
func formatSize(bytes int64) string {
	const (
		KB = 1024
		MB = 1024 * KB
		GB = 1024 * MB
		TB = 1024 * GB
	)

	switch {
	case bytes >= TB:
		return fmt.Sprintf("%.2f TB", float64(bytes)/float64(TB))
	case bytes >= GB:
		return fmt.Sprintf("%.2f GB", float64(bytes)/float64(GB))
	case bytes >= MB:
		return fmt.Sprintf("%.2f MB", float64(bytes)/float64(MB))
	case bytes >= KB:
		return fmt.Sprintf("%.2f KB", float64(bytes)/float64(KB))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}
