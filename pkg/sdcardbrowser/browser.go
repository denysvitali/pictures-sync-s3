package sdcardbrowser

import (
	"bytes"
	"fmt"
	"mime"
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
	IsVideo bool                   `json:"is_video"`
	IsRAW   bool                   `json:"is_raw"`
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
			ext := filepath.Ext(entry.Name())
			fileInfo.IsImage = isImageExt(ext)
			fileInfo.IsVideo = isVideoExt(ext)
			fileInfo.IsRAW = isRAWExt(ext)
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

	contentType := imageContentTypeForExt(filepath.Ext(cleanFullPath))
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

// OpenFile opens any regular file under the SD card mount path for streaming.
func OpenFile(mountPath, requestedPath string) (*os.File, os.FileInfo, string, error) {
	if requestedPath == "" {
		return nil, nil, "", fmt.Errorf("path parameter required")
	}

	cleanFullPath, err := resolvePath(mountPath, requestedPath)
	if err != nil {
		return nil, nil, "", err
	}

	// #nosec G304 -- path is resolved and constrained to the SD card mount path above.
	file, err := os.Open(cleanFullPath)
	if err != nil {
		return nil, nil, "", fmt.Errorf("failed to open file: %w", err)
	}

	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, nil, "", fmt.Errorf("failed to stat file: %w", err)
	}
	if info.IsDir() {
		_ = file.Close()
		return nil, nil, "", fmt.Errorf("path is a directory")
	}

	return file, info, contentTypeForExt(filepath.Ext(cleanFullPath)), nil
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
	return imageContentTypeForExt(ext) != ""
}

func isVideoExt(ext string) bool {
	switch strings.ToLower(ext) {
	case ".mp4", ".m4v", ".mov", ".avi", ".mkv", ".mts", ".m2ts", ".3gp", ".webm":
		return true
	default:
		return false
	}
}

func isRAWExt(ext string) bool {
	switch strings.ToLower(ext) {
	case ".raw", ".cr2", ".cr3", ".nef", ".nrw", ".arw", ".dng", ".rw2", ".orf", ".raf", ".pef", ".srw":
		return true
	default:
		return false
	}
}

func imageContentTypeForExt(ext string) string {
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

func contentTypeForExt(ext string) string {
	if contentType := imageContentTypeForExt(ext); contentType != "" {
		return contentType
	}

	switch strings.ToLower(ext) {
	case ".mp4", ".m4v":
		return "video/mp4"
	case ".mov":
		return "video/quicktime"
	case ".avi":
		return "video/x-msvideo"
	case ".mkv":
		return "video/x-matroska"
	case ".mts", ".m2ts":
		return "video/mp2t"
	case ".3gp":
		return "video/3gpp"
	case ".webm":
		return "video/webm"
	case ".raw", ".cr2", ".cr3", ".nef", ".nrw", ".arw", ".dng", ".rw2", ".orf", ".raf", ".pef", ".srw":
		return "application/octet-stream"
	default:
		if contentType := mime.TypeByExtension(ext); contentType != "" {
			return contentType
		}
		return "application/octet-stream"
	}
}
