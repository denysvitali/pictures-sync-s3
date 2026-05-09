package handlers

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/denysvitali/pictures-sync-s3/pkg/daemoncontrol"
	"github.com/denysvitali/pictures-sync-s3/pkg/sdcardbrowser"
	"github.com/denysvitali/pictures-sync-s3/pkg/state"
	exif "github.com/dsoprea/go-exif/v3"
	exifcommon "github.com/dsoprea/go-exif/v3/common"
)

var sdCardMountPath = state.MountDir

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
	start := time.Now()

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

	log.Printf("[Gallery] list cloud start path=%q page=%d page_size=%d remote=%s", path, page, pageSize, r.RemoteAddr)

	result, err := ctx.SyncMgr.ListFilesPaginated(path, page, pageSize)
	if err != nil {
		log.Printf("[Gallery] list cloud failed path=%q page=%d page_size=%d duration=%s error=%v", path, page, pageSize, time.Since(start), err)
		JSONResponse(w, map[string]interface{}{
			"error": fmt.Sprintf("Failed to list files: %v", err),
		})
		return
	}

	log.Printf(
		"[Gallery] list cloud complete path=%q page=%d page_size=%d returned=%d total=%d has_more=%t duration=%s",
		path,
		result.Page,
		result.PageSize,
		len(result.Files),
		result.Total,
		result.HasMore,
		time.Since(start),
	)

	JSONResponse(w, result)
}

var fileViewImageContentTypes = map[string]string{
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".png":  "image/png",
	".gif":  "image/gif",
	".webp": "image/webp",
}

var fileViewTextContentTypes = map[string]string{
	".conf":      "text/plain; charset=utf-8",
	".config":    "text/plain; charset=utf-8",
	".css":       "text/css; charset=utf-8",
	".csv":       "text/csv; charset=utf-8",
	".go":        "text/x-go; charset=utf-8",
	".hcl":       "text/x-hcl; charset=utf-8",
	".html":      "text/html; charset=utf-8",
	".ini":       "text/plain; charset=utf-8",
	".java":      "text/x-java-source; charset=utf-8",
	".js":        "text/javascript; charset=utf-8",
	".json":      "application/json; charset=utf-8",
	".log":       "text/plain; charset=utf-8",
	".lua":       "text/x-lua; charset=utf-8",
	".md":        "text/markdown; charset=utf-8",
	".mod":       "text/plain; charset=utf-8",
	".php":       "text/x-php; charset=utf-8",
	".pl":        "text/x-perl; charset=utf-8",
	".py":        "text/x-python; charset=utf-8",
	".rb":        "text/x-ruby; charset=utf-8",
	".rs":        "text/rust; charset=utf-8",
	".sh":        "text/x-sh; charset=utf-8",
	".sql":       "text/x-sql; charset=utf-8",
	".toml":      "text/plain; charset=utf-8",
	".ts":        "text/typescript; charset=utf-8",
	".tsx":       "text/typescript; charset=utf-8",
	".txt":       "text/plain; charset=utf-8",
	".xml":       "application/xml; charset=utf-8",
	".yml":       "text/yaml; charset=utf-8",
	".yaml":      "text/yaml; charset=utf-8",
	".yaml.tpl":  "text/yaml; charset=utf-8",
}

var fileViewTextFilenames = map[string]string{
	"dockfile":   "text/x-dockerfile; charset=utf-8",
	"dockerfile": "text/x-dockerfile; charset=utf-8",
	"makefile":   "text/x-makefile; charset=utf-8",
	"license":    "text/plain; charset=utf-8",
	"changelog":  "text/markdown; charset=utf-8",
	"readme":     "text/markdown; charset=utf-8",
}

func fileViewContentType(filePath string) (string, bool) {
	ext := strings.ToLower(filepath.Ext(filePath))
	if contentType, ok := fileViewImageContentTypes[ext]; ok {
		return contentType, true
	}
	if contentType, ok := fileViewTextContentTypes[ext]; ok {
		return contentType, true
	}

	fileName := strings.ToLower(filepath.Base(filePath))
	if contentType, ok := fileViewTextFilenames[fileName]; ok {
		return contentType, true
	}

	return "", false
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

// HandleFileView serves image and text files from remote storage.
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

	contentType, ok := fileViewContentType(filePath)
	if !ok {
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

// HandleFileLink returns a temporary cloud-provider URL for a remote file.
func (ctx *Context) HandleFileLink(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	filePath := r.URL.Query().Get("path")
	if filePath == "" {
		http.Error(w, "path parameter required", http.StatusBadRequest)
		return
	}

	link, err := ctx.SyncMgr.GetPublicLink(filePath)
	if err != nil {
		log.Printf("Failed to create public link for %s: %v", filePath, err)
		http.Error(w, fmt.Sprintf("failed to create file link: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Cache-Control", "no-store")
	JSONResponse(w, map[string]string{
		"url": link,
	})
}

// HandleThumbnail serves thumbnail images for files being synced
func (ctx *Context) HandleThumbnail(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

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

	log.Printf("[Gallery] thumbnail start path=%q remote=%s", requestedPath, r.RemoteAddr)

	requestCtx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	preview, err := ctx.daemonClient().RequestSDCardThumbnail(requestCtx, requestedPath)
	if err != nil {
		statusCode := daemonHTTPStatus(err)
		log.Printf("[Gallery] thumbnail failed path=%q duration=%s error=%v", requestedPath, time.Since(start), err)
		http.Error(w, daemonErrorMessage(err), statusCode)
		return
	}

	w.Header().Set("Content-Type", preview.ContentType)
	w.Header().Set("Cache-Control", "public, max-age=3600")
	if _, err := w.Write(preview.Data); err != nil {
		log.Printf("[Gallery] thumbnail write failed path=%q duration=%s error=%v", requestedPath, time.Since(start), err)
	}
	log.Printf("[Gallery] thumbnail complete path=%q size=%d duration=%s", requestedPath, len(preview.Data), time.Since(start))
}

// SDCardFileInfo contains file metadata including EXIF data
type SDCardFileInfo struct {
	Name    string                 `json:"name"`
	Path    string                 `json:"path"`
	Size    int64                  `json:"size"`
	ModTime time.Time              `json:"mod_time"`
	IsDir   bool                   `json:"is_dir"`
	IsImage bool                   `json:"is_image"`
	EXIF    map[string]interface{} `json:"exif,omitempty"`
}

// HandleSDCardFiles lists files on the SD card with EXIF metadata
func (ctx *Context) HandleSDCardFiles(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	requestedPath := r.URL.Query().Get("path")

	log.Printf("[Gallery] list sdcard start path=%q remote=%s", requestedPath, r.RemoteAddr)

	requestCtx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	result, err := ctx.daemonClient().RequestSDCardFiles(requestCtx, requestedPath)
	if err != nil {
		log.Printf("[Gallery] list sdcard failed path=%q duration=%s error=%v", requestedPath, time.Since(start), err)
		JSONResponse(w, map[string]interface{}{
			"error": daemonErrorMessage(err),
		})
		return
	}

	log.Printf("[Gallery] list sdcard complete path=%q files=%d duration=%s", requestedPath, len(result.Files), time.Since(start))
	JSONResponse(w, result)
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
	if filepath.IsAbs(requestedPath) || strings.Contains(requestedPath, "..") {
		http.Error(w, "access denied", http.StatusForbidden)
		return
	}

	requestCtx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	preview, err := ctx.daemonClient().RequestSDCardPreview(requestCtx, requestedPath)
	if err != nil {
		http.Error(w, daemonErrorMessage(err), daemonHTTPStatus(err))
		return
	}

	w.Header().Set("Content-Type", preview.ContentType)
	w.Header().Set("Cache-Control", "public, max-age=3600")
	if _, err := w.Write(preview.Data); err != nil {
		log.Printf("[Gallery] sdcard preview write failed path=%q error=%v", requestedPath, err)
	}
}

// HandleSDCardFile streams any regular file from the SD card.
func (ctx *Context) HandleSDCardFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	requestedPath := r.URL.Query().Get("path")
	if requestedPath == "" {
		http.Error(w, "path parameter required", http.StatusBadRequest)
		return
	}

	file, info, contentType, err := sdcardbrowser.OpenFile(sdCardMountPath, requestedPath)
	if err != nil {
		http.Error(w, err.Error(), sdcardFileHTTPStatus(err))
		return
	}
	defer file.Close()

	disposition := "inline"
	if r.URL.Query().Get("download") == "1" {
		disposition = "attachment"
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`%s; filename=%q`, disposition, filepath.Base(requestedPath)))
	w.Header().Set("Cache-Control", "no-store")
	http.ServeContent(w, r, info.Name(), info.ModTime(), file)
}

func sdcardFileHTTPStatus(err error) int {
	message := err.Error()
	switch {
	case strings.Contains(message, "path parameter required"):
		return http.StatusBadRequest
	case strings.Contains(message, "access denied"):
		return http.StatusForbidden
	case strings.Contains(message, "path is a directory"):
		return http.StatusBadRequest
	case os.IsNotExist(err):
		return http.StatusNotFound
	default:
		return http.StatusInternalServerError
	}
}

func daemonErrorMessage(err error) string {
	var commandErr *daemoncontrol.CommandError
	if errors.As(err, &commandErr) && commandErr.Message != "" {
		return commandErr.Message
	}
	return err.Error()
}

func daemonHTTPStatus(err error) int {
	message := daemonErrorMessage(err)
	switch message {
	case "access denied":
		return http.StatusForbidden
	case "unsupported file type", "path parameter required", "no SD card mounted":
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}
