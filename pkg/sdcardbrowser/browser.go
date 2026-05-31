package sdcardbrowser

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/disintegration/imaging"
	exif "github.com/dsoprea/go-exif/v3"
	exifcommon "github.com/dsoprea/go-exif/v3/common"
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

// ReadThumbnail returns a thumbnail for an SD card JPEG. It prefers the
// thumbnail embedded in the EXIF metadata (IFD1) to avoid decoding the
// full-resolution image, falling back to decoding and resizing only when no
// embedded thumbnail is present. The thumbnail is rotated to match the EXIF
// Orientation tag so it displays the same way as the full image.
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

	if data, orientation, err := extractEXIFThumbnail(cleanFullPath); err == nil {
		oriented, err := applyJPEGOrientation(data, orientation)
		if err == nil {
			return &Preview{ContentType: "image/jpeg", Data: oriented}, nil
		}
		// If the rotation pass failed (corrupt embedded thumbnail) fall through
		// to the full decode path below.
	}

	// #nosec G304 -- path is resolved and constrained to the SD card mount path above.
	img, err := imaging.Open(cleanFullPath, imaging.AutoOrientation(true))
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

// ThumbnailFromBytes builds a JPEG thumbnail (max 200×200) from in-memory image
// bytes — the same strategy as ReadThumbnail but for data fetched from a remote
// (e.g. an rclone/B2 object) rather than a local file. It prefers the embedded
// EXIF thumbnail for JPEGs, falling back to decoding and resizing the full
// image. The thumbnail is rotated to match the EXIF Orientation tag.
//
// For JPEGs without an embedded thumbnail (or non-JPEG formats) the caller must
// supply the full image bytes; a truncated prefix will fail the fallback decode.
func ThumbnailFromBytes(data []byte) (*Preview, error) {
	if thumb, orientation, err := extractEXIFThumbnailFromReader(bytes.NewReader(data)); err == nil {
		if oriented, oerr := applyJPEGOrientation(thumb, orientation); oerr == nil {
			return &Preview{ContentType: "image/jpeg", Data: oriented}, nil
		}
		// Corrupt embedded thumbnail — fall through to a full decode.
	}

	img, err := imaging.Decode(bytes.NewReader(data), imaging.AutoOrientation(true))
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

// extractEXIFThumbnail returns the JPEG thumbnail stored in IFD1 of the EXIF
// metadata along with the IFD0 Orientation value (defaulting to 1 when absent).
// It only reads the JPEG's APP1 segment instead of slurping the entire image,
// which keeps thumbnail latency in the milliseconds even on multi-megabyte
// source files.
func extractEXIFThumbnail(filePath string) ([]byte, int, error) {
	// #nosec G304 -- caller resolves and validates the path within the mount.
	f, err := os.Open(filePath)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()
	return extractEXIFThumbnailFromReader(f)
}

// extractEXIFThumbnailFromReader is the reader-based core of extractEXIFThumbnail
// so both local files and in-memory remote bytes share one implementation.
func extractEXIFThumbnailFromReader(r io.Reader) ([]byte, int, error) {
	rawExif, err := readJPEGExifSegment(r)
	if err != nil {
		return nil, 0, err
	}

	im, err := exifcommon.NewIfdMappingWithStandard()
	if err != nil {
		return nil, 0, err
	}

	_, index, err := exif.Collect(im, exif.NewTagIndex(), rawExif)
	if err != nil {
		return nil, 0, err
	}

	orientation := readEXIFOrientation(index.RootIfd)

	ifd1 := index.RootIfd.NextIfd()
	if ifd1 == nil {
		return nil, orientation, exif.ErrNoThumbnail
	}

	data, err := ifd1.Thumbnail()
	if err != nil {
		return nil, orientation, err
	}
	return data, orientation, nil
}

// readEXIFOrientation returns the IFD0 Orientation tag value (1..8), or 1 when
// the tag is missing or unreadable.
func readEXIFOrientation(ifd *exif.Ifd) int {
	const orientationTagID = 0x0112
	tags, err := ifd.FindTagWithId(orientationTagID)
	if err != nil || len(tags) == 0 {
		return 1
	}
	value, err := tags[0].Value()
	if err != nil {
		return 1
	}
	switch v := value.(type) {
	case []uint16:
		if len(v) > 0 && v[0] >= 1 && v[0] <= 8 {
			return int(v[0])
		}
	case uint16:
		if v >= 1 && v <= 8 {
			return int(v)
		}
	}
	return 1
}

// applyJPEGOrientation decodes a JPEG, applies the transform corresponding to
// the given EXIF Orientation (1..8), and re-encodes it. Orientation 1 is a
// no-op and returns the input unchanged.
func applyJPEGOrientation(data []byte, orientation int) ([]byte, error) {
	if orientation <= 1 || orientation > 8 {
		return data, nil
	}
	img, err := imaging.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	// EXIF Orientation values map to these display transforms:
	//   1 normal, 2 flip-H, 3 rotate 180, 4 flip-V,
	//   5 transpose, 6 rotate 90 CW, 7 transverse, 8 rotate 90 CCW.
	// imaging.Rotate90 rotates CCW; Rotate270 rotates CW.
	var transformed = img
	switch orientation {
	case 2:
		transformed = imaging.FlipH(img)
	case 3:
		transformed = imaging.Rotate180(img)
	case 4:
		transformed = imaging.FlipV(img)
	case 5:
		transformed = imaging.Rotate270(imaging.FlipH(img))
	case 6:
		transformed = imaging.Rotate270(img)
	case 7:
		transformed = imaging.Rotate90(imaging.FlipH(img))
	case 8:
		transformed = imaging.Rotate90(img)
	}
	var buf bytes.Buffer
	if err := imaging.Encode(&buf, transformed, imaging.JPEG, imaging.JPEGQuality(85)); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// errNoEXIFSegment indicates the JPEG was readable but contained no APP1/EXIF.
var errNoEXIFSegment = errors.New("no EXIF segment in JPEG")

// readJPEGExifSegment parses JPEG markers and returns the TIFF-formatted EXIF
// payload from the APP1 segment. It reads only the segment headers and the
// APP1 payload, so the cost is bounded by the EXIF size (≤64 KiB) regardless
// of the full image size.
func readJPEGExifSegment(r io.Reader) ([]byte, error) {
	br := bufio.NewReaderSize(r, 8192)

	var soi [2]byte
	if _, err := io.ReadFull(br, soi[:]); err != nil {
		return nil, err
	}
	if soi[0] != 0xFF || soi[1] != 0xD8 {
		return nil, fmt.Errorf("not a JPEG file")
	}

	for {
		// Scan for the next marker (0xFF followed by a non-0x00, non-0xFF byte).
		b, err := br.ReadByte()
		if err != nil {
			return nil, err
		}
		if b != 0xFF {
			continue
		}
		var marker byte
		for {
			marker, err = br.ReadByte()
			if err != nil {
				return nil, err
			}
			if marker != 0xFF {
				break
			}
		}
		// 0xFF 0x00 is a stuffed byte inside compressed data; skip.
		if marker == 0x00 {
			continue
		}
		// Stand-alone markers without a length field.
		if marker == 0xD8 /* SOI */ || marker == 0xD9 /* EOI */ ||
			(marker >= 0xD0 && marker <= 0xD7) /* RST0..RST7 */ {
			continue
		}
		// SOS marks the start of compressed image data — no more metadata.
		if marker == 0xDA {
			return nil, errNoEXIFSegment
		}

		var lenBuf [2]byte
		if _, err := io.ReadFull(br, lenBuf[:]); err != nil {
			return nil, err
		}
		length := int(binary.BigEndian.Uint16(lenBuf[:]))
		if length < 2 {
			return nil, fmt.Errorf("invalid JPEG segment length")
		}
		payloadLen := length - 2

		if marker == 0xE1 /* APP1 */ {
			payload := make([]byte, payloadLen)
			if _, err := io.ReadFull(br, payload); err != nil {
				return nil, err
			}
			const exifSig = "Exif\x00\x00"
			if len(payload) >= len(exifSig) && bytes.HasPrefix(payload, []byte(exifSig)) {
				return payload[len(exifSig):], nil
			}
			// Other APP1 payload (e.g. XMP); keep scanning.
			continue
		}

		if _, err := io.CopyN(io.Discard, br, int64(payloadLen)); err != nil {
			return nil, err
		}
	}
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
	fullPath := filepath.Join(mountPath, filepath.Clean("/"+requestedPath))
	cleanFullPath := filepath.Clean(fullPath)

	if cleanFullPath != cleanMountPath && !strings.HasPrefix(cleanFullPath, cleanMountPath+string(os.PathSeparator)) {
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
