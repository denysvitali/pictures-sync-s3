package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/denysvitali/pictures-sync-s3/pkg/httputil"
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
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": fmt.Sprintf("Failed to list cards: %v", err),
		})
		return
	}

	httputil.JSON(w, http.StatusOK, map[string]any{
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
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(w).Encode(map[string]any{
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

	httputil.JSON(w, http.StatusOK, result)
}

var fileViewImageContentTypes = map[string]string{
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".png":  "image/png",
	".gif":  "image/gif",
	".webp": "image/webp",
}

var fileViewTextContentTypes = map[string]string{
	".conf":     "text/plain; charset=utf-8",
	".config":   "text/plain; charset=utf-8",
	".css":      "text/css; charset=utf-8",
	".csv":      "text/csv; charset=utf-8",
	".go":       "text/x-go; charset=utf-8",
	".hcl":      "text/x-hcl; charset=utf-8",
	".html":     "text/html; charset=utf-8",
	".ini":      "text/plain; charset=utf-8",
	".java":     "text/x-java-source; charset=utf-8",
	".js":       "text/javascript; charset=utf-8",
	".json":     "application/json; charset=utf-8",
	".log":      "text/plain; charset=utf-8",
	".lua":      "text/x-lua; charset=utf-8",
	".md":       "text/markdown; charset=utf-8",
	".mod":      "text/plain; charset=utf-8",
	".php":      "text/x-php; charset=utf-8",
	".pl":       "text/x-perl; charset=utf-8",
	".py":       "text/x-python; charset=utf-8",
	".rb":       "text/x-ruby; charset=utf-8",
	".rs":       "text/rust; charset=utf-8",
	".sh":       "text/x-sh; charset=utf-8",
	".sql":      "text/x-sql; charset=utf-8",
	".toml":     "text/plain; charset=utf-8",
	".ts":       "text/typescript; charset=utf-8",
	".tsx":      "text/typescript; charset=utf-8",
	".txt":      "text/plain; charset=utf-8",
	".xml":      "application/xml; charset=utf-8",
	".yml":      "text/yaml; charset=utf-8",
	".yaml":     "text/yaml; charset=utf-8",
	".yaml.tpl": "text/yaml; charset=utf-8",
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
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": fmt.Sprintf("Failed to list files: %v", err),
		})
		return
	}

	log.Printf("[Gallery] Successfully listed %d files", len(files))
	httputil.JSON(w, http.StatusOK, map[string]any{
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

	// Stage headers but defer flushing until the first byte is actually
	// received from the upstream. Otherwise an early failure inside GetFile
	// leaves the client with an implicit 200 and a plain-text error tail.
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "public, max-age=3600")
	deferred := &deferredHeaderWriter{w: w, status: http.StatusOK}

	if err := ctx.SyncMgr.GetFile(filePath, deferred); err != nil {
		log.Printf("Failed to get file %s: %v", filePath, err)
		if deferred.wrote {
			// Headers already flushed; we cannot send a different status.
			// Log and abort — the client will see a truncated body.
			return
		}
		// Reset the staged content-type so http.Error's text/plain is honored.
		w.Header().Del("Content-Type")
		w.Header().Del("Cache-Control")
		http.Error(w, fmt.Sprintf("failed to retrieve file: %v", err), http.StatusBadGateway)
		return
	}

	// Empty upstream content still needs a valid status to be emitted.
	if !deferred.wrote {
		w.WriteHeader(http.StatusOK)
	}
}

// deferredHeaderWriter postpones the implicit WriteHeader(200) until the first
// byte is written. This lets the handler issue an error status if the upstream
// streaming source fails before producing any output.
type deferredHeaderWriter struct {
	w      http.ResponseWriter
	status int
	wrote  bool
}

func (d *deferredHeaderWriter) Write(p []byte) (int, error) {
	if !d.wrote {
		d.wrote = true
		d.w.WriteHeader(d.status)
	}
	return d.w.Write(p)
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
	httputil.JSON(w, http.StatusOK, map[string]string{
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

	resolvedPath, err := httputil.ValidatePath(sdCardMountPath, requestedPath)
	if err != nil {
		log.Printf("[Gallery] thumbnail rejected path=%q error=%v", requestedPath, err)
		http.Error(w, "access denied", http.StatusForbidden)
		return
	}
	// Symlink-safe secondary check: re-evaluate symlinks on the already-resolved
	// path and confirm the result is still under the SD card mount root. This
	// catches any intermediate symlink chains that could escape the root between
	// the initial ValidatePath call and actual filesystem access.
	if err := verifyNoSymlinkEscape(sdCardMountPath, resolvedPath); err != nil {
		log.Printf("[Gallery] thumbnail symlink escape rejected path=%q error=%v", requestedPath, err)
		http.Error(w, "access denied", http.StatusForbidden)
		return
	}

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

// HandleFileThumbnail serves a small JPEG thumbnail for an image stored on the
// remote (B2/cloud). It backs the Google Photos page previews, which show what
// each B2 card will sync. To stay cheap it first fetches only the leading bytes
// of a JPEG to read the embedded EXIF thumbnail, falling back to downloading and
// decoding the full image when no embedded thumbnail is available.
func (ctx *Context) HandleFileThumbnail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	filePath := r.URL.Query().Get("path")
	if filePath == "" {
		http.Error(w, "path parameter required", http.StatusBadRequest)
		return
	}

	ext := strings.ToLower(filepath.Ext(filePath))
	if _, ok := fileViewImageContentTypes[ext]; !ok {
		http.Error(w, "unsupported file type", http.StatusBadRequest)
		return
	}

	// Fast path: only JPEGs carry an embedded EXIF thumbnail. Pull ~1 MiB —
	// enough to cover the APP1 segment — and try to extract it without ever
	// downloading the full-resolution image.
	if ext == ".jpg" || ext == ".jpeg" {
		const exifPrefixBytes = 1 << 20
		var buf bytes.Buffer
		if err := ctx.SyncMgr.GetFileRange(filePath, &buf, exifPrefixBytes); err == nil {
			if preview, err := sdcardbrowser.ThumbnailFromBytes(buf.Bytes()); err == nil {
				writeThumbnailResponse(w, preview)
				return
			}
		}
	}

	// Fallback: download the whole image (bounded) and decode + resize.
	const maxFullBytes = 50 << 20
	var buf bytes.Buffer
	if err := ctx.SyncMgr.GetFileRange(filePath, &buf, maxFullBytes); err != nil {
		log.Printf("[Gallery] B2 thumbnail fetch failed path=%q error=%v", filePath, err)
		http.Error(w, "failed to retrieve file", http.StatusBadGateway)
		return
	}

	preview, err := sdcardbrowser.ThumbnailFromBytes(buf.Bytes())
	if err != nil {
		log.Printf("[Gallery] B2 thumbnail decode failed path=%q error=%v", filePath, err)
		http.Error(w, "failed to generate thumbnail", http.StatusUnprocessableEntity)
		return
	}
	writeThumbnailResponse(w, preview)
}

func writeThumbnailResponse(w http.ResponseWriter, preview *sdcardbrowser.Preview) {
	w.Header().Set("Content-Type", preview.ContentType)
	w.Header().Set("Cache-Control", "public, max-age=3600")
	if _, err := w.Write(preview.Data); err != nil {
		log.Printf("[Gallery] thumbnail write failed error=%v", err)
	}
}

// SDCardFileInfo contains file metadata including EXIF data
type SDCardFileInfo struct {
	Name    string         `json:"name"`
	Path    string         `json:"path"`
	Size    int64          `json:"size"`
	ModTime time.Time      `json:"mod_time"`
	IsDir   bool           `json:"is_dir"`
	IsImage bool           `json:"is_image"`
	EXIF    map[string]any `json:"exif,omitempty"`
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
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(daemonHTTPStatus(err))
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": daemonErrorMessage(err),
		})
		return
	}

	log.Printf("[Gallery] list sdcard complete path=%q files=%d duration=%s", requestedPath, len(result.Files), time.Since(start))
	httputil.JSON(w, http.StatusOK, result)
}

// extractEXIF extracts EXIF metadata from an image file
func extractEXIF(filePath string) map[string]any {
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

	data := make(map[string]any)

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
	resolvedPreviewPath, err := httputil.ValidatePath(sdCardMountPath, requestedPath)
	if err != nil {
		log.Printf("[Gallery] sdcard preview rejected path=%q error=%v", requestedPath, err)
		http.Error(w, "access denied", http.StatusForbidden)
		return
	}
	if err := verifyNoSymlinkEscape(sdCardMountPath, resolvedPreviewPath); err != nil {
		log.Printf("[Gallery] sdcard preview symlink escape rejected path=%q error=%v", requestedPath, err)
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
	// Preserve the source filename so browser "Save As" doesn't default to
	// "preview" (derived from the URL path) plus the served MIME extension.
	w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=%q", filepath.Base(requestedPath)))
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

// verifyNoSymlinkEscape performs a secondary symlink-safety check after
// httputil.ValidatePath. It re-evaluates symlinks on the already-resolved path
// and confirms the result still lives under sdRoot. This guards against TOCTOU
// races or multi-hop symlink chains where an intermediate directory is itself a
// symlink that was created between the initial check and filesystem access.
func verifyNoSymlinkEscape(sdRoot, resolvedPath string) error {
	// Re-resolve the path to catch any newly-created or changed symlinks.
	reResolved, err := filepath.EvalSymlinks(resolvedPath)
	if err != nil {
		// If the path no longer exists (e.g. card was unmounted) treat as escape.
		return fmt.Errorf("re-eval symlinks failed: %w", err)
	}
	sep := string(os.PathSeparator)
	cleanRoot := filepath.Clean(sdRoot)
	if reResolved != cleanRoot && !strings.HasPrefix(reResolved+sep, cleanRoot+sep) {
		return fmt.Errorf("path %q escapes SD card root after symlink re-evaluation", reResolved)
	}
	return nil
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
	case errors.Is(err, fs.ErrNotExist):
		return http.StatusNotFound
	default:
		return http.StatusInternalServerError
	}
}
