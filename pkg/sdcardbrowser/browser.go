package sdcardbrowser

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/disintegration/imaging"
)

// FileInfo contains SD card file metadata exposed to the WebUI.
type FileInfo struct {
	Name    string                 `json:"name"`
	Path    string                 `json:"path"`
	Size    int64                  `json:"size"`
	ModTime time.Time              `json:"mod_time"`
	IsDir   bool                   `json:"is_dir"`
	IsImage bool                   `json:"is_image"`
	EXIF    map[string]interface{} `json:"exif,omitempty"`
}

// FileList contains a directory listing for the SD card browser.
type FileList struct {
	Files []FileInfo `json:"files"`
	Path  string     `json:"path"`
}

// Preview contains image bytes for an SD card preview response.
type Preview struct {
	ContentType string `json:"content_type"`
	Data        []byte `json:"data"`
}

// ListFiles lists files under the SD card mount path.
func ListFiles(mountPath, requestedPath string) (*FileList, error) {
	if requestedPath == "" {
		requestedPath = "DCIM"
	}

	cleanFullPath, err := resolvePath(mountPath, requestedPath)
	if err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(cleanFullPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read directory: %w", err)
	}

	files := make([]FileInfo, 0, len(entries))
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}

		fileInfo := FileInfo{
			Name:    entry.Name(),
			Path:    filepath.Join(requestedPath, entry.Name()),
			Size:    info.Size(),
			ModTime: info.ModTime(),
			IsDir:   entry.IsDir(),
		}

		if !entry.IsDir() {
			fileInfo.IsImage = isImageExt(filepath.Ext(entry.Name()))
		}

		files = append(files, fileInfo)
	}

	return &FileList{Files: files, Path: requestedPath}, nil
}

// ReadPreview reads an image file under the SD card mount path.
func ReadPreview(mountPath, requestedPath string) (*Preview, error) {
	if requestedPath == "" {
		return nil, fmt.Errorf("path parameter required")
	}

	cleanFullPath, err := resolvePath(mountPath, requestedPath)
	if err != nil {
		return nil, err
	}

	contentType := contentTypeForExt(filepath.Ext(cleanFullPath))
	if contentType == "" {
		return nil, fmt.Errorf("unsupported file type")
	}

	// #nosec G304 -- path is resolved and constrained to the SD card mount path above.
	data, err := os.ReadFile(cleanFullPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read preview: %w", err)
	}

	return &Preview{ContentType: contentType, Data: data}, nil
}

// ReadThumbnail reads and resizes an SD card JPEG for thumbnail display.
func ReadThumbnail(mountPath, requestedPath string) (*Preview, error) {
	if requestedPath == "" {
		return nil, fmt.Errorf("path parameter required")
	}

	cleanFullPath, err := resolvePath(mountPath, requestedPath)
	if err != nil {
		return nil, err
	}

	ext := strings.ToLower(filepath.Ext(cleanFullPath))
	if ext != ".jpg" && ext != ".jpeg" {
		return nil, fmt.Errorf("only JPEG images supported")
	}

	// #nosec G304 -- path is resolved and constrained to the SD card mount path above.
	img, err := imaging.Open(cleanFullPath)
	if err != nil {
		return nil, fmt.Errorf("failed to decode image: %w", err)
	}

	thumbnail := imaging.Fit(img, 200, 200, imaging.Lanczos)
	var buf bytes.Buffer
	if err := imaging.Encode(&buf, thumbnail, imaging.JPEG, imaging.JPEGQuality(80)); err != nil {
		return nil, fmt.Errorf("failed to encode thumbnail: %w", err)
	}

	return &Preview{ContentType: "image/jpeg", Data: buf.Bytes()}, nil
}

func resolvePath(mountPath, requestedPath string) (string, error) {
	if filepath.IsAbs(requestedPath) || strings.Contains(requestedPath, "..") {
		return "", fmt.Errorf("access denied")
	}

	cleanMountPath := filepath.Clean(mountPath)
	if !strings.HasSuffix(cleanMountPath, string(os.PathSeparator)) {
		cleanMountPath += string(os.PathSeparator)
	}

	fullPath := filepath.Join(mountPath, filepath.Clean("/"+requestedPath))
	cleanFullPath := filepath.Clean(fullPath)

	if !strings.HasPrefix(cleanFullPath, cleanMountPath) {
		return "", fmt.Errorf("access denied")
	}

	return cleanFullPath, nil
}

func isImageExt(ext string) bool {
	return contentTypeForExt(ext) != ""
}

func contentTypeForExt(ext string) string {
	switch strings.ToLower(ext) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	default:
		return ""
	}
}
