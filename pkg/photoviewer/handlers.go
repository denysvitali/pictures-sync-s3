package photoviewer

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/denysvitali/pictures-sync-s3/pkg/httputil"
)

// ServePhoto serves a photo file with proper MIME type and caching headers
func ServePhoto(w http.ResponseWriter, r *http.Request, mountPath, relativePath string) error {
	// Validate and get photo info
	photo, err := GetPhotoByPath(mountPath, relativePath)
	if err != nil {
		return fmt.Errorf("failed to get photo: %w", err)
	}

	// Check if file exists
	file, err := os.Open(photo.AbsolutePath)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	// Get file stat for size and mod time
	stat, err := file.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat file: %w", err)
	}

	// Set headers
	w.Header().Set("Content-Type", photo.MimeType)
	w.Header().Set("Content-Length", strconv.FormatInt(photo.Size, 10))
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable") // 1 year cache for immutable photos
	w.Header().Set("Last-Modified", stat.ModTime().UTC().Format(http.TimeFormat))

	// Set content disposition for downloads (optional)
	if r.URL.Query().Get("download") == "1" {
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", photo.Name))
	} else {
		w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=%q", photo.Name))
	}

	// Support HTTP range requests for large files and video streaming
	http.ServeContent(w, r, photo.Name, stat.ModTime(), file)

	return nil
}

// ServeThumbnail serves a scaled-down thumbnail of an image
// This is a simplified version - a production system would use a thumbnail cache
func ServeThumbnail(w http.ResponseWriter, r *http.Request, mountPath, relativePath string, maxWidth, maxHeight int) error {
	// Validate path and get photo info
	photo, err := GetPhotoByPath(mountPath, relativePath)
	if err != nil {
		return fmt.Errorf("failed to get photo: %w", err)
	}

	// Only generate thumbnails for images (not videos or RAW files)
	if !photo.IsImage || photo.IsRAW {
		return fmt.Errorf("thumbnails only supported for standard image formats")
	}

	// For now, just serve the original image
	// In a production system, you would:
	// 1. Check a thumbnail cache
	// 2. Generate thumbnail if not cached
	// 3. Store in cache for future requests
	return ServePhoto(w, r, mountPath, relativePath)
}

// HandleListPhotos is an HTTP handler that lists photos from the SD card
// mountPath should be the path to the SD card mount point (e.g., /perm/pictures-sync/mounts/sdcard)
func HandleListPhotos(w http.ResponseWriter, r *http.Request, mountPath string) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get query parameters
	path := r.URL.Query().Get("path")
	recursive := r.URL.Query().Get("recursive") == "true"

	var result *ScanResult
	var err error

	if recursive {
		// Full recursive scan of DCIM
		result, err = ScanDCIM(mountPath)
	} else {
		// Single directory scan
		if path == "" {
			path = "."
		}
		result, err = ScanDirectory(mountPath, path)
	}

	if err != nil {
		log.Printf("Failed to scan photos: %v", err)
		httputil.JSON(w, http.StatusInternalServerError, map[string]string{
			"error": fmt.Sprintf("Failed to scan photos: %v", err),
		})
		return
	}

	// Apply filters if provided
	filter := parsePhotoFilter(r)
	if filter != nil {
		result.Photos = filter.Filter(result.Photos)
		result.TotalCount = len(result.Photos)
		result.TotalSize = 0
		for _, p := range result.Photos {
			result.TotalSize += p.Size
		}
	}

	// Pagination
	page, pageSize := parsePagination(r)
	if page > 0 && pageSize > 0 {
		start := (page - 1) * pageSize
		end := start + pageSize

		if start >= len(result.Photos) {
			result.Photos = []PhotoInfo{}
		} else {
			if end > len(result.Photos) {
				end = len(result.Photos)
			}
			result.Photos = result.Photos[start:end]
		}
	}

	httputil.JSON(w, http.StatusOK, result)
}

// HandleServePhoto is an HTTP handler that serves a photo file
// mountPath should be the path to the SD card mount point (e.g., /perm/pictures-sync/mounts/sdcard)
func HandleServePhoto(w http.ResponseWriter, r *http.Request, mountPath string) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get path from query parameter
	path := r.URL.Query().Get("path")
	if path == "" {
		http.Error(w, "path parameter required", http.StatusBadRequest)
		return
	}

	// Serve the photo
	if err := ServePhoto(w, r, mountPath, path); err != nil {
		log.Printf("Failed to serve photo %s: %v", path, err)
		if os.IsNotExist(err) || strings.Contains(err.Error(), "not found") {
			http.Error(w, "File not found", http.StatusNotFound)
		} else if strings.Contains(err.Error(), "access denied") {
			http.Error(w, "Access denied", http.StatusForbidden)
		} else {
			http.Error(w, fmt.Sprintf("Failed to serve photo: %v", err), http.StatusInternalServerError)
		}
		return
	}
}

// HandleServeThumbnail is an HTTP handler that serves a photo thumbnail
// mountPath should be the path to the SD card mount point (e.g., /perm/pictures-sync/mounts/sdcard)
func HandleServeThumbnail(w http.ResponseWriter, r *http.Request, mountPath string) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get parameters
	path := r.URL.Query().Get("path")
	if path == "" {
		http.Error(w, "path parameter required", http.StatusBadRequest)
		return
	}

	// Parse thumbnail dimensions (default to 200x200)
	width := parseInt(r.URL.Query().Get("width"), 200)
	height := parseInt(r.URL.Query().Get("height"), 200)

	// Limit maximum thumbnail size to prevent abuse
	if width > 1000 {
		width = 1000
	}
	if height > 1000 {
		height = 1000
	}

	// Serve the thumbnail
	if err := ServeThumbnail(w, r, mountPath, path, width, height); err != nil {
		log.Printf("Failed to serve thumbnail %s: %v", path, err)
		if os.IsNotExist(err) || strings.Contains(err.Error(), "not found") {
			http.Error(w, "File not found", http.StatusNotFound)
		} else if strings.Contains(err.Error(), "access denied") {
			http.Error(w, "Access denied", http.StatusForbidden)
		} else {
			http.Error(w, fmt.Sprintf("Failed to serve thumbnail: %v", err), http.StatusInternalServerError)
		}
		return
	}
}

func parseInt(s string, defaultValue int) int {
	if s == "" {
		return defaultValue
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return defaultValue
	}
	return v
}

func parsePhotoFilter(r *http.Request) *PhotoFilter {
	q := r.URL.Query()

	// Check if any filters are present
	if !q.Has("min_size") && !q.Has("max_size") && !q.Has("after") && !q.Has("before") &&
		!q.Has("only_raw") && !q.Has("only_video") && !q.Has("only_image") {
		return nil
	}

	filter := &PhotoFilter{}

	// Size filters
	if minSize := q.Get("min_size"); minSize != "" {
		filter.MinSize, _ = strconv.ParseInt(minSize, 10, 64)
	}
	if maxSize := q.Get("max_size"); maxSize != "" {
		filter.MaxSize, _ = strconv.ParseInt(maxSize, 10, 64)
	}

	// Time filters (ISO 8601 format)
	if after := q.Get("after"); after != "" {
		if t, err := time.Parse(time.RFC3339, after); err == nil {
			filter.After = t
		}
	}
	if before := q.Get("before"); before != "" {
		if t, err := time.Parse(time.RFC3339, before); err == nil {
			filter.Before = t
		}
	}

	// Type filters
	filter.OnlyRAW = q.Get("only_raw") == "true"
	filter.OnlyVideo = q.Get("only_video") == "true"
	filter.OnlyImage = q.Get("only_image") == "true"

	return filter
}

func parsePagination(r *http.Request) (page, pageSize int) {
	q := r.URL.Query()

	page = parseInt(q.Get("page"), 0)
	pageSize = parseInt(q.Get("page_size"), 0)

	// Validate pagination
	if page < 0 {
		page = 0
	}
	if pageSize < 0 || pageSize > 1000 {
		pageSize = 100 // Default/max page size
	}

	return page, pageSize
}
