package handlers

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/denysvitali/pictures-sync-s3/pkg/state"
	"github.com/disintegration/imaging"
	exif "github.com/dsoprea/go-exif/v3"
	exifcommon "github.com/dsoprea/go-exif/v3/common"
)

// HandleFileCards returns list of card IDs
func (ctx *Context) HandleFileCards(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	cards, err := ctx.SyncMgr.ListCardIDs()
	if err != nil {
		JSONResponse(w, map[string]interface{}{
			"error": fmt.Sprintf("Failed to list cards: %v", err),
		})
		return
	}

	JSONResponse(w, map[string]interface{}{
		"cards": cards,
	})
}

// HandleFilesPaginated returns paginated file listing
func (ctx *Context) HandleFilesPaginated(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	path := r.URL.Query().Get("path")
	page := 1
	pageSize := 100

	if p := r.URL.Query().Get("page"); p != "" {
		if parsed, err := strconv.Atoi(p); err == nil && parsed > 0 {
			page = parsed
		}
	}

	if ps := r.URL.Query().Get("page_size"); ps != "" {
		if parsed, err := strconv.Atoi(ps); err == nil && parsed > 0 && parsed <= 1000 {
			pageSize = parsed
		}
	}

	result, err := ctx.SyncMgr.ListFilesPaginated(path, page, pageSize)
	if err != nil {
		JSONResponse(w, map[string]interface{}{
			"error": fmt.Sprintf("Failed to list files: %v", err),
		})
		return
	}

	JSONResponse(w, result)
}

// HandleFiles lists files on remote
func (ctx *Context) HandleFiles(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get path from query param (defaults to root)
	path := r.URL.Query().Get("path")
	log.Printf("[Gallery] Listing files for path: '%s'", path)

	// List files on remote
	files, err := ctx.SyncMgr.ListFiles(path)
	if err != nil {
		log.Printf("[Gallery] Error listing files: %v", err)
		JSONResponse(w, map[string]interface{}{
			"error": fmt.Sprintf("Failed to list files: %v", err),
		})
		return
	}

	log.Printf("[Gallery] Successfully listed %d files", len(files))
	JSONResponse(w, map[string]interface{}{
		"files": files,
		"path":  path,
	})
}

// HandleFileView serves image files from remote storage
func (ctx *Context) HandleFileView(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get file path from query param
	filePath := r.URL.Query().Get("path")
	if filePath == "" {
		http.Error(w, "path parameter required", http.StatusBadRequest)
		return
	}

	// Check if file is an image
	ext := strings.ToLower(filepath.Ext(filePath))
	var contentType string
	switch ext {
	case ".jpg", ".jpeg":
		contentType = "image/jpeg"
	case ".png":
		contentType = "image/png"
	case ".gif":
		contentType = "image/gif"
	case ".webp":
		contentType = "image/webp"
	default:
		http.Error(w, "unsupported file type", http.StatusBadRequest)
		return
	}

	// Set content type header
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "public, max-age=3600")

	// Stream the file from remote
	if err := ctx.SyncMgr.GetFile(filePath, w); err != nil {
		log.Printf("Failed to get file %s: %v", filePath, err)
		http.Error(w, fmt.Sprintf("failed to retrieve file: %v", err), http.StatusInternalServerError)
		return
	}
}

// HandleThumbnail serves thumbnail images for files being synced
func (ctx *Context) HandleThumbnail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get file path from query param
	requestedPath := r.URL.Query().Get("path")
	if requestedPath == "" {
		http.Error(w, "path parameter required", http.StatusBadRequest)
		return
	}

	// Security: Reject absolute paths and directory traversal attempts
	if filepath.IsAbs(requestedPath) || strings.Contains(requestedPath, "..") {
		http.Error(w, "access denied", http.StatusForbidden)
		return
	}

	// Security: Properly validate path to prevent traversal attacks
	mountPath := state.MountDir

	// Clean the mount path and ensure it ends with separator
	cleanMountPath := filepath.Clean(mountPath)
	if !strings.HasSuffix(cleanMountPath, string(os.PathSeparator)) {
		cleanMountPath += string(os.PathSeparator)
	}

	// Join mount path with requested path and clean the result
	// This resolves any .. or . in the path
	fullPath := filepath.Join(mountPath, filepath.Clean("/"+requestedPath))
	cleanFullPath := filepath.Clean(fullPath)

	// Verify the cleaned path is still within the mount directory
	if !strings.HasPrefix(cleanFullPath, cleanMountPath) {
		http.Error(w, "access denied", http.StatusForbidden)
		return
	}

	// Use the validated path
	filePath := cleanFullPath

	// Check if file is a JPEG
	ext := strings.ToLower(filepath.Ext(filePath))
	if ext != ".jpg" && ext != ".jpeg" {
		http.Error(w, "only JPEG images supported", http.StatusBadRequest)
		return
	}

	// Open and decode the image
	// #nosec G304 -- filePath is validated against mount directory and traversal is blocked above
	file, err := os.Open(filePath)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to open image: %v", err), http.StatusInternalServerError)
		return
	}
	defer file.Close()

	img, err := imaging.Open(filePath)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to decode image: %v", err), http.StatusInternalServerError)
		return
	}

	// Resize to thumbnail (max 200px, preserve aspect ratio)
	thumbnail := imaging.Fit(img, 200, 200, imaging.Lanczos)

	// Encode as JPEG and send
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "public, max-age=3600")

	if err := imaging.Encode(w, thumbnail, imaging.JPEG, imaging.JPEGQuality(80)); err != nil {
		log.Printf("Failed to encode thumbnail: %v", err)
	}
}

// SDCardFileInfo contains file metadata including EXIF data
type SDCardFileInfo struct {
	Name     string                 `json:"name"`
	Path     string                 `json:"path"`
	Size     int64                  `json:"size"`
	ModTime  time.Time              `json:"mod_time"`
	IsDir    bool                   `json:"is_dir"`
	IsImage  bool                   `json:"is_image"`
	EXIF     map[string]interface{} `json:"exif,omitempty"`
}

// HandleSDCardFiles lists files on the SD card with EXIF metadata
func (ctx *Context) HandleSDCardFiles(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get path from query param (defaults to DCIM)
	requestedPath := r.URL.Query().Get("path")
	if requestedPath == "" {
		requestedPath = "DCIM"
	}

	// Security: Reject absolute paths and directory traversal attempts
	if filepath.IsAbs(requestedPath) || strings.Contains(requestedPath, "..") {
		JSONResponse(w, map[string]interface{}{
			"error": "access denied",
		})
		return
	}

	// Security: Validate path
	mountPath := state.MountDir
	cleanMountPath := filepath.Clean(mountPath)
	if !strings.HasSuffix(cleanMountPath, string(os.PathSeparator)) {
		cleanMountPath += string(os.PathSeparator)
	}

	fullPath := filepath.Join(mountPath, filepath.Clean("/"+requestedPath))
	cleanFullPath := filepath.Clean(fullPath)

	if !strings.HasPrefix(cleanFullPath, cleanMountPath) {
		JSONResponse(w, map[string]interface{}{
			"error": "access denied",
		})
		return
	}

	// Check if SD card is mounted
	currentState := ctx.StateMgr.GetState()
	if !currentState.SDCardMounted {
		JSONResponse(w, map[string]interface{}{
			"error": "no SD card mounted",
		})
		return
	}

	// Read directory
	entries, err := os.ReadDir(cleanFullPath)
	if err != nil {
		JSONResponse(w, map[string]interface{}{
			"error": fmt.Sprintf("failed to read directory: %v", err),
		})
		return
	}

	// Build file list with metadata
	var files []SDCardFileInfo
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}

		fileInfo := SDCardFileInfo{
			Name:    entry.Name(),
			Path:    filepath.Join(requestedPath, entry.Name()),
			Size:    info.Size(),
			ModTime: info.ModTime(),
			IsDir:   entry.IsDir(),
		}

		// Check if it's an image and extract EXIF
		if !entry.IsDir() {
			ext := strings.ToLower(filepath.Ext(entry.Name()))
			if ext == ".jpg" || ext == ".jpeg" {
				fileInfo.IsImage = true

				// Extract EXIF data
				filePath := filepath.Join(cleanFullPath, entry.Name())
				if exifData := extractEXIF(filePath); exifData != nil {
					fileInfo.EXIF = exifData
				}
			} else if ext == ".png" || ext == ".gif" || ext == ".webp" {
				fileInfo.IsImage = true
			}
		}

		files = append(files, fileInfo)
	}

	JSONResponse(w, map[string]interface{}{
		"files": files,
		"path":  requestedPath,
	})
}

// extractEXIF extracts EXIF metadata from an image file
func extractEXIF(filePath string) map[string]interface{} {
	// #nosec G304 -- filePath is within validated SD card mount directory
	fileData, err := os.ReadFile(filePath)
	if err != nil {
		return nil
	}

	rawExif, err := exif.SearchAndExtractExif(fileData)
	if err != nil {
		return nil
	}

	entries, _, err := exif.GetFlatExifData(rawExif, nil)
	if err != nil {
		return nil
	}

	data := make(map[string]interface{})

	// Helper function to find tag value
	findTag := func(tagName string) *exif.ExifTag {
		for i := range entries {
			if entries[i].TagName == tagName {
				return &entries[i]
			}
		}
		return nil
	}

	// Extract common EXIF fields
	if tag := findTag("Make"); tag != nil {
		data["camera_make"] = strings.TrimSpace(tag.FormattedFirst)
	}

	if tag := findTag("Model"); tag != nil {
		data["camera_model"] = strings.TrimSpace(tag.FormattedFirst)
	}

	if tag := findTag("DateTimeOriginal"); tag != nil {
		data["date_time"] = tag.FormattedFirst
	}

	if tag := findTag("ISOSpeedRatings"); tag != nil {
		data["iso"] = tag.FormattedFirst
	}

	if tag := findTag("FNumber"); tag != nil {
		if val, ok := tag.Value.([]exifcommon.Rational); ok && len(val) > 0 {
			data["f_number"] = fmt.Sprintf("f/%.1f", float64(val[0].Numerator)/float64(val[0].Denominator))
		}
	}

	if tag := findTag("ExposureTime"); tag != nil {
		if val, ok := tag.Value.([]exifcommon.Rational); ok && len(val) > 0 {
			num := val[0].Numerator
			denom := val[0].Denominator
			if num == 1 {
				data["exposure_time"] = fmt.Sprintf("1/%d", denom)
			} else {
				data["exposure_time"] = fmt.Sprintf("%.2fs", float64(num)/float64(denom))
			}
		}
	}

	if tag := findTag("FocalLength"); tag != nil {
		if val, ok := tag.Value.([]exifcommon.Rational); ok && len(val) > 0 {
			data["focal_length"] = fmt.Sprintf("%.1fmm", float64(val[0].Numerator)/float64(val[0].Denominator))
		}
	}

	// GPS coordinates
	if latTag := findTag("GPSLatitude"); latTag != nil {
		if lonTag := findTag("GPSLongitude"); lonTag != nil {
			if latRefTag := findTag("GPSLatitudeRef"); latRefTag != nil {
				if lonRefTag := findTag("GPSLongitudeRef"); lonRefTag != nil {
					lat := parseGPSCoordinate(latTag, latRefTag)
					lon := parseGPSCoordinate(lonTag, lonRefTag)
					if lat != 0 || lon != 0 {
						data["gps_latitude"] = lat
						data["gps_longitude"] = lon
					}
				}
			}
		}
	}

	return data
}

// parseGPSCoordinate converts GPS EXIF data to decimal degrees
func parseGPSCoordinate(coordTag *exif.ExifTag, refTag *exif.ExifTag) float64 {
	coords, ok := coordTag.Value.([]exifcommon.Rational)
	if !ok || len(coords) < 3 {
		return 0
	}

	ref := refTag.FormattedFirst
	if ref == "" {
		return 0
	}

	degrees := float64(coords[0].Numerator) / float64(coords[0].Denominator)
	minutes := float64(coords[1].Numerator) / float64(coords[1].Denominator)
	seconds := float64(coords[2].Numerator) / float64(coords[2].Denominator)

	decimal := degrees + (minutes / 60.0) + (seconds / 3600.0)

	// Apply hemisphere (S and W are negative)
	if ref == "S" || ref == "W" {
		decimal = -decimal
	}

	return decimal
}

// HandleSDCardPreview serves full-resolution images from SD card
func (ctx *Context) HandleSDCardPreview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	requestedPath := r.URL.Query().Get("path")
	if requestedPath == "" {
		http.Error(w, "path parameter required", http.StatusBadRequest)
		return
	}

	// Security: Reject absolute paths and directory traversal attempts
	if filepath.IsAbs(requestedPath) || strings.Contains(requestedPath, "..") {
		http.Error(w, "access denied", http.StatusForbidden)
		return
	}

	// Security: Validate path
	mountPath := state.MountDir
	cleanMountPath := filepath.Clean(mountPath)
	if !strings.HasSuffix(cleanMountPath, string(os.PathSeparator)) {
		cleanMountPath += string(os.PathSeparator)
	}

	fullPath := filepath.Join(mountPath, filepath.Clean("/"+requestedPath))
	cleanFullPath := filepath.Clean(fullPath)

	if !strings.HasPrefix(cleanFullPath, cleanMountPath) {
		http.Error(w, "access denied", http.StatusForbidden)
		return
	}

	// Check if file is an image
	ext := strings.ToLower(filepath.Ext(cleanFullPath))
	var contentType string
	switch ext {
	case ".jpg", ".jpeg":
		contentType = "image/jpeg"
	case ".png":
		contentType = "image/png"
	case ".gif":
		contentType = "image/gif"
	case ".webp":
		contentType = "image/webp"
	default:
		http.Error(w, "unsupported file type", http.StatusBadRequest)
		return
	}

	// Serve the file
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "public, max-age=3600")
	http.ServeFile(w, r, cleanFullPath)
}
