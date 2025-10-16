// Package photoviewer provides functionality for browsing and accessing photos on mounted SD cards.
// It scans DCIM directories, identifies media files (images and videos), and provides secure
// path validation to prevent directory traversal attacks. Supports filtering by size, date, and media type.
package photoviewer

import (
	"fmt"
	"io/fs"
	"mime"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// PhotoInfo represents detailed information about a photo file
type PhotoInfo struct {
	Name         string    `json:"name"`
	Path         string    `json:"path"`          // Relative path from DCIM root
	AbsolutePath string    `json:"-"`             // Not exposed to API
	Size         int64     `json:"size"`
	SizeHuman    string    `json:"size_human"`
	ModTime      time.Time `json:"mod_time"`
	MimeType     string    `json:"mime_type"`
	Extension    string    `json:"extension"`
	IsImage      bool      `json:"is_image"`
	IsVideo      bool      `json:"is_video"`
	IsRAW        bool      `json:"is_raw"`
}

// ScanResult represents the result of scanning a DCIM directory
type ScanResult struct {
	Photos     []PhotoInfo `json:"photos"`
	TotalCount int         `json:"total_count"`
	TotalSize  int64       `json:"total_size"`
	Directories []string    `json:"directories,omitempty"`
}

// supportedImageExtensions defines image file extensions we support
var supportedImageExtensions = map[string]bool{
	".jpg":  true,
	".jpeg": true,
	".png":  true,
	".gif":  true,
	".webp": true,
	".bmp":  true,
	".tiff": true,
	".tif":  true,
}

// supportedRAWExtensions defines RAW image file extensions
var supportedRAWExtensions = map[string]bool{
	".raw":  true,
	".cr2":  true,  // Canon
	".cr3":  true,  // Canon
	".nef":  true,  // Nikon
	".arw":  true,  // Sony
	".dng":  true,  // Adobe/Universal
	".orf":  true,  // Olympus
	".rw2":  true,  // Panasonic
	".pef":  true,  // Pentax
	".srw":  true,  // Samsung
	".raf":  true,  // Fujifilm
	".3fr":  true,  // Hasselblad
	".fff":  true,  // Hasselblad
	".mrw":  true,  // Minolta
	".nrw":  true,  // Nikon
	".rwl":  true,  // Leica
	".dcr":  true,  // Kodak
	".kdc":  true,  // Kodak
}

// supportedVideoExtensions defines video file extensions
var supportedVideoExtensions = map[string]bool{
	".mp4":  true,
	".mov":  true,
	".avi":  true,
	".mkv":  true,
	".m4v":  true,
	".mpg":  true,
	".mpeg": true,
	".wmv":  true,
	".flv":  true,
	".webm": true,
	".mts":  true,
	".m2ts": true,
}

// IsImageFile checks if a file extension indicates an image file
func IsImageFile(ext string) bool {
	ext = strings.ToLower(ext)
	return supportedImageExtensions[ext] || supportedRAWExtensions[ext]
}

// IsVideoFile checks if a file extension indicates a video file
func IsVideoFile(ext string) bool {
	ext = strings.ToLower(ext)
	return supportedVideoExtensions[ext]
}

// IsMediaFile checks if a file is any supported media type
func IsMediaFile(ext string) bool {
	return IsImageFile(ext) || IsVideoFile(ext)
}

// ScanDCIM recursively scans a DCIM directory and returns all photo/video files
func ScanDCIM(mountPath string) (*ScanResult, error) {
	dcimPath := filepath.Join(mountPath, "DCIM")

	// Verify DCIM directory exists
	info, err := os.Stat(dcimPath)
	if err != nil {
		return nil, fmt.Errorf("DCIM directory not found: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("DCIM is not a directory")
	}

	result := &ScanResult{
		Photos:      make([]PhotoInfo, 0, 100),
		Directories: make([]string, 0, 10),
	}

	// Walk the DCIM directory tree
	err = filepath.WalkDir(dcimPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// Log but continue on individual file errors
			return nil
		}

		// Track directories
		if d.IsDir() {
			if path != dcimPath {
				relPath, _ := filepath.Rel(dcimPath, path)
				result.Directories = append(result.Directories, relPath)
			}
			return nil
		}

		// Check if it's a media file
		ext := strings.ToLower(filepath.Ext(path))
		if !IsMediaFile(ext) {
			return nil
		}

		// Get file info
		fileInfo, err := d.Info()
		if err != nil {
			return nil // Skip files we can't stat
		}

		// Build relative path from DCIM root
		relPath, err := filepath.Rel(dcimPath, path)
		if err != nil {
			relPath = filepath.Base(path)
		}

		// Determine MIME type
		mimeType := mime.TypeByExtension(ext)
		if mimeType == "" {
			// Fallback MIME types for common extensions
			if IsImageFile(ext) {
				mimeType = "image/" + strings.TrimPrefix(ext, ".")
			} else if IsVideoFile(ext) {
				mimeType = "video/" + strings.TrimPrefix(ext, ".")
			} else {
				mimeType = "application/octet-stream"
			}
		}

		// Create photo info
		photo := PhotoInfo{
			Name:         filepath.Base(path),
			Path:         relPath,
			AbsolutePath: path,
			Size:         fileInfo.Size(),
			SizeHuman:    formatBytes(fileInfo.Size()),
			ModTime:      fileInfo.ModTime(),
			MimeType:     mimeType,
			Extension:    ext,
			IsImage:      supportedImageExtensions[ext] || supportedRAWExtensions[ext],
			IsVideo:      supportedVideoExtensions[ext],
			IsRAW:        supportedRAWExtensions[ext],
		}

		result.Photos = append(result.Photos, photo)
		result.TotalCount++
		result.TotalSize += fileInfo.Size()

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to scan DCIM: %w", err)
	}

	return result, nil
}

// ScanDirectory scans a specific directory (non-recursive) for media files
func ScanDirectory(mountPath, relativePath string) (*ScanResult, error) {
	// Build and validate full path
	fullPath := filepath.Join(mountPath, "DCIM", filepath.Clean("/"+relativePath))

	// Security: ensure path is within DCIM
	dcimPath := filepath.Join(mountPath, "DCIM")
	cleanFullPath := filepath.Clean(fullPath)
	cleanDCIMPath := filepath.Clean(dcimPath)

	if !strings.HasPrefix(cleanFullPath, cleanDCIMPath) {
		return nil, fmt.Errorf("access denied: path outside DCIM directory")
	}

	// Read directory entries
	entries, err := os.ReadDir(cleanFullPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read directory: %w", err)
	}

	result := &ScanResult{
		Photos:      make([]PhotoInfo, 0, len(entries)),
		Directories: make([]string, 0),
	}

	for _, entry := range entries {
		// Track subdirectories
		if entry.IsDir() {
			result.Directories = append(result.Directories, entry.Name())
			continue
		}

		// Check if it's a media file
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		if !IsMediaFile(ext) {
			continue
		}

		// Get file info
		fileInfo, err := entry.Info()
		if err != nil {
			continue // Skip files we can't stat
		}

		// Build paths
		fullFilePath := filepath.Join(cleanFullPath, entry.Name())
		relPath, err := filepath.Rel(dcimPath, fullFilePath)
		if err != nil {
			relPath = entry.Name()
		}

		// Determine MIME type
		mimeType := mime.TypeByExtension(ext)
		if mimeType == "" {
			if IsImageFile(ext) {
				mimeType = "image/" + strings.TrimPrefix(ext, ".")
			} else if IsVideoFile(ext) {
				mimeType = "video/" + strings.TrimPrefix(ext, ".")
			} else {
				mimeType = "application/octet-stream"
			}
		}

		photo := PhotoInfo{
			Name:         entry.Name(),
			Path:         relPath,
			AbsolutePath: fullFilePath,
			Size:         fileInfo.Size(),
			SizeHuman:    formatBytes(fileInfo.Size()),
			ModTime:      fileInfo.ModTime(),
			MimeType:     mimeType,
			Extension:    ext,
			IsImage:      supportedImageExtensions[ext] || supportedRAWExtensions[ext],
			IsVideo:      supportedVideoExtensions[ext],
			IsRAW:        supportedRAWExtensions[ext],
		}

		result.Photos = append(result.Photos, photo)
		result.TotalCount++
		result.TotalSize += fileInfo.Size()
	}

	return result, nil
}

// GetPhotoByPath retrieves a single photo's information by its path
func GetPhotoByPath(mountPath, relativePath string) (*PhotoInfo, error) {
	// Build and validate full path
	fullPath := filepath.Join(mountPath, "DCIM", filepath.Clean("/"+relativePath))

	// Security: ensure path is within DCIM
	dcimPath := filepath.Join(mountPath, "DCIM")
	cleanFullPath := filepath.Clean(fullPath)
	cleanDCIMPath := filepath.Clean(dcimPath)

	if !strings.HasPrefix(cleanFullPath, cleanDCIMPath) {
		return nil, fmt.Errorf("access denied: path outside DCIM directory")
	}

	// Check if file exists and is a regular file
	fileInfo, err := os.Stat(cleanFullPath)
	if err != nil {
		return nil, fmt.Errorf("file not found: %w", err)
	}

	if fileInfo.IsDir() {
		return nil, fmt.Errorf("path is a directory, not a file")
	}

	// Check if it's a media file
	ext := strings.ToLower(filepath.Ext(cleanFullPath))
	if !IsMediaFile(ext) {
		return nil, fmt.Errorf("not a supported media file")
	}

	// Determine MIME type
	mimeType := mime.TypeByExtension(ext)
	if mimeType == "" {
		if IsImageFile(ext) {
			mimeType = "image/" + strings.TrimPrefix(ext, ".")
		} else if IsVideoFile(ext) {
			mimeType = "video/" + strings.TrimPrefix(ext, ".")
		} else {
			mimeType = "application/octet-stream"
		}
	}

	photo := &PhotoInfo{
		Name:         filepath.Base(cleanFullPath),
		Path:         relativePath,
		AbsolutePath: cleanFullPath,
		Size:         fileInfo.Size(),
		SizeHuman:    formatBytes(fileInfo.Size()),
		ModTime:      fileInfo.ModTime(),
		MimeType:     mimeType,
		Extension:    ext,
		IsImage:      supportedImageExtensions[ext] || supportedRAWExtensions[ext],
		IsVideo:      supportedVideoExtensions[ext],
		IsRAW:        supportedRAWExtensions[ext],
	}

	return photo, nil
}

// ValidatePath ensures a path is safe and within the DCIM directory
func ValidatePath(mountPath, relativePath string) (string, error) {
	dcimPath := filepath.Join(mountPath, "DCIM")

	// Clean the DCIM path and ensure it ends with separator for prefix matching
	cleanDCIMPath := filepath.Clean(dcimPath)
	if !strings.HasSuffix(cleanDCIMPath, string(filepath.Separator)) {
		cleanDCIMPath += string(filepath.Separator)
	}

	// Clean the relative path to prevent ".." and other tricks
	cleanRelPath := filepath.Clean("/" + relativePath)
	cleanRelPath = strings.TrimPrefix(cleanRelPath, "/")

	// Build full path
	fullPath := filepath.Join(dcimPath, cleanRelPath)
	cleanFullPath := filepath.Clean(fullPath)

	// Ensure it hasn't escaped DCIM directory
	if !strings.HasPrefix(cleanFullPath+string(filepath.Separator), cleanDCIMPath) && cleanFullPath != strings.TrimSuffix(cleanDCIMPath, string(filepath.Separator)) {
		return "", fmt.Errorf("access denied: path outside DCIM directory")
	}

	return cleanFullPath, nil
}

// formatBytes formats bytes to human-readable string
func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// FilterPhotos applies filters to a list of photos
type PhotoFilter struct {
	MinSize   int64     // Minimum file size in bytes
	MaxSize   int64     // Maximum file size in bytes (0 = no limit)
	After     time.Time // Only files modified after this time
	Before    time.Time // Only files modified before this time
	OnlyRAW   bool      // Only include RAW files
	OnlyVideo bool      // Only include video files
	OnlyImage bool      // Only include non-RAW image files
}

// Filter applies the filter to a slice of photos
func (f *PhotoFilter) Filter(photos []PhotoInfo) []PhotoInfo {
	if f == nil {
		return photos
	}

	filtered := make([]PhotoInfo, 0, len(photos))

	for _, photo := range photos {
		// Size filters
		if f.MinSize > 0 && photo.Size < f.MinSize {
			continue
		}
		if f.MaxSize > 0 && photo.Size > f.MaxSize {
			continue
		}

		// Time filters
		if !f.After.IsZero() && photo.ModTime.Before(f.After) {
			continue
		}
		if !f.Before.IsZero() && photo.ModTime.After(f.Before) {
			continue
		}

		// Type filters
		if f.OnlyRAW && !photo.IsRAW {
			continue
		}
		if f.OnlyVideo && !photo.IsVideo {
			continue
		}
		if f.OnlyImage && (photo.IsVideo || photo.IsRAW) {
			continue
		}

		filtered = append(filtered, photo)
	}

	return filtered
}
