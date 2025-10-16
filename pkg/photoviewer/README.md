# Photo Viewer Package

A comprehensive Go package for listing and serving photos from mounted SD cards in the pictures-sync-s3 project.

## Features

- **Recursive DCIM scanning**: Scan entire DCIM directory tree for media files
- **Directory-level scanning**: Non-recursive scanning of specific directories
- **Multi-format support**:
  - Standard images: JPG, JPEG, PNG, GIF, WebP, BMP, TIFF
  - RAW formats: CR2, CR3, NEF, ARW, DNG, ORF, RW2, PEF, SRW, RAF, and more
  - Videos: MP4, MOV, AVI, MKV, M4V, MPG, MPEG, WMV, FLV, WebM, MTS, M2TS
- **Security**: Path validation prevents directory traversal attacks
- **Metadata extraction**: File size, modification date, MIME type detection
- **Filtering**: Filter photos by size, date, file type (RAW/video/image)
- **Pagination**: Built-in pagination support for large photo collections
- **HTTP serving**: Proper MIME types, caching headers, range request support

## Package Structure

```
pkg/photoviewer/
├── photoviewer.go       # Core scanning and validation functions
├── handlers.go          # HTTP handlers for web integration
├── photoviewer_test.go  # Comprehensive test suite
└── README.md           # This file
```

## Usage

### Basic Photo Scanning

```go
import "github.com/denysvitali/pictures-sync-s3/pkg/photoviewer"

// Scan entire DCIM directory recursively
mountPath := "/perm/pictures-sync/mounts/sdcard"
result, err := photoviewer.ScanDCIM(mountPath)
if err != nil {
    log.Fatal(err)
}

fmt.Printf("Found %d photos, total size: %s\n",
    result.TotalCount,
    formatBytes(result.TotalSize))

// List all photos
for _, photo := range result.Photos {
    fmt.Printf("%s: %s (%s)\n",
        photo.Name,
        photo.Path,
        photo.SizeHuman)
}
```

### Directory Scanning

```go
// Scan a specific directory (non-recursive)
result, err := photoviewer.ScanDirectory(mountPath, "100CANON")
if err != nil {
    log.Fatal(err)
}

// List photos and subdirectories
fmt.Printf("Photos: %d\n", len(result.Photos))
fmt.Printf("Subdirectories: %v\n", result.Directories)
```

### Getting Single Photo Info

```go
// Get information about a specific photo
photo, err := photoviewer.GetPhotoByPath(mountPath, "100CANON/IMG_0001.JPG")
if err != nil {
    log.Fatal(err)
}

fmt.Printf("Name: %s\n", photo.Name)
fmt.Printf("Size: %s\n", photo.SizeHuman)
fmt.Printf("MIME: %s\n", photo.MimeType)
fmt.Printf("Is RAW: %v\n", photo.IsRAW)
fmt.Printf("Modified: %s\n", photo.ModTime)
```

### HTTP Handlers

```go
import (
    "net/http"
    "github.com/denysvitali/pictures-sync-s3/pkg/photoviewer"
    "github.com/denysvitali/pictures-sync-s3/pkg/state"
)

func setupPhotoRoutes() {
    mountPath := state.MountDir

    // List photos (supports pagination and filtering via query params)
    http.HandleFunc("/api/photos/list", func(w http.ResponseWriter, r *http.Request) {
        photoviewer.HandleListPhotos(w, r, mountPath)
    })

    // Serve photo file
    http.HandleFunc("/api/photos/view", func(w http.ResponseWriter, r *http.Request) {
        photoviewer.HandleServePhoto(w, r, mountPath)
    })

    // Serve thumbnail
    http.HandleFunc("/api/photos/thumbnail", func(w http.ResponseWriter, r *http.Request) {
        photoviewer.HandleServeThumbnail(w, r, mountPath)
    })
}
```

### Filtering Photos

```go
// Create a filter
filter := &photoviewer.PhotoFilter{
    MinSize:   1024 * 1024,      // 1MB minimum
    MaxSize:   50 * 1024 * 1024, // 50MB maximum
    OnlyImage: true,              // Exclude videos and RAW
    After:     time.Now().Add(-30 * 24 * time.Hour), // Last 30 days
}

// Apply filter to results
filtered := filter.Filter(result.Photos)
```

## API Endpoints

When using the HTTP handlers, the following query parameters are supported:

### GET /api/photos/list

List photos from the SD card.

**Query Parameters:**
- `path` (string): Directory path relative to DCIM (default: ".")
- `recursive` (bool): Recursively scan subdirectories (default: false)
- `page` (int): Page number for pagination (1-indexed)
- `page_size` (int): Number of items per page (max 1000, default 100)
- `min_size` (int64): Minimum file size in bytes
- `max_size` (int64): Maximum file size in bytes
- `after` (RFC3339): Only files modified after this time
- `before` (RFC3339): Only files modified before this time
- `only_raw` (bool): Only include RAW files
- `only_video` (bool): Only include videos
- `only_image` (bool): Only include standard images (exclude RAW and video)

**Response:**
```json
{
  "photos": [
    {
      "name": "IMG_0001.JPG",
      "path": "100CANON/IMG_0001.JPG",
      "size": 4567890,
      "size_human": "4.4 MB",
      "mod_time": "2025-10-15T10:30:00Z",
      "mime_type": "image/jpeg",
      "extension": ".jpg",
      "is_image": true,
      "is_video": false,
      "is_raw": false
    }
  ],
  "total_count": 150,
  "total_size": 1234567890,
  "directories": ["100CANON", "101NIKON"]
}
```

**Examples:**
```bash
# List all photos recursively
curl "http://localhost:8080/api/photos/list?recursive=true"

# List photos in specific directory
curl "http://localhost:8080/api/photos/list?path=100CANON"

# Filter for large JPGs from last week
curl "http://localhost:8080/api/photos/list?recursive=true&only_image=true&min_size=5000000&after=2025-10-09T00:00:00Z"

# Paginated results
curl "http://localhost:8080/api/photos/list?recursive=true&page=2&page_size=50"
```

### GET /api/photos/view

Serve a photo file with proper MIME type and caching.

**Query Parameters:**
- `path` (string, required): File path relative to DCIM
- `download` (bool): Set to "1" to force download instead of inline display

**Response:**
- Binary file content with appropriate Content-Type header
- Supports HTTP range requests for streaming videos
- Sets cache headers for optimal performance

**Examples:**
```bash
# View photo in browser
curl "http://localhost:8080/api/photos/view?path=100CANON/IMG_0001.JPG"

# Download photo
curl "http://localhost:8080/api/photos/view?path=100CANON/IMG_0001.JPG&download=1" -O
```

### GET /api/photos/thumbnail

Serve a thumbnail of a photo.

**Query Parameters:**
- `path` (string, required): File path relative to DCIM
- `width` (int): Thumbnail width in pixels (max 1000, default 200)
- `height` (int): Thumbnail height in pixels (max 1000, default 200)

**Note:** Current implementation serves the full image. In production, implement thumbnail caching.

**Examples:**
```bash
# Get 200x200 thumbnail
curl "http://localhost:8080/api/photos/thumbnail?path=100CANON/IMG_0001.JPG"

# Get larger 400x400 thumbnail
curl "http://localhost:8080/api/photos/thumbnail?path=100CANON/IMG_0001.JPG&width=400&height=400"
```

## Security

### Path Validation

All file paths are validated to prevent directory traversal attacks:

```go
// This is safe - paths are sanitized
validated, err := photoviewer.ValidatePath(mountPath, "../../../etc/passwd")
// Result: /mnt/sdcard/DCIM/etc/passwd (confined to DCIM)
```

The validation process:
1. Cleans the path using `filepath.Clean()` which resolves `..` and `.`
2. Strips leading `/` to prevent absolute path access
3. Verifies the final path is within the DCIM directory
4. Prevents escaping to parent directories

### Safe Patterns

```go
// These all resolve safely to paths within DCIM:
ValidatePath(mount, "../passwords.txt")     // → DCIM/passwords.txt
ValidatePath(mount, "/etc/passwd")          // → DCIM/etc/passwd
ValidatePath(mount, "foo/../../bar")        // → DCIM/bar
```

## Supported File Formats

### Standard Images
- JPEG (.jpg, .jpeg)
- PNG (.png)
- GIF (.gif)
- WebP (.webp)
- BMP (.bmp)
- TIFF (.tiff, .tif)

### RAW Formats
- Canon (.cr2, .cr3)
- Nikon (.nef, .nrw)
- Sony (.arw)
- Adobe/Universal (.dng)
- Olympus (.orf)
- Panasonic (.rw2)
- Pentax (.pef)
- Samsung (.srw)
- Fujifilm (.raf)
- Hasselblad (.3fr, .fff)
- Minolta (.mrw)
- Leica (.rwl)
- Kodak (.dcr, .kdc)
- Generic (.raw)

### Video Formats
- MP4 (.mp4, .m4v)
- MOV (.mov)
- AVI (.avi)
- MKV (.mkv)
- MPEG (.mpg, .mpeg)
- WMV (.wmv)
- FLV (.flv)
- WebM (.webm)
- AVCHD (.mts, .m2ts)

## Integration with Existing Code

The photoviewer package is designed to complement the existing `handleSDCardFiles` and `handleSDCardPreview` handlers in `cmd/webui/main.go`. You can:

1. **Replace existing handlers** with photoviewer handlers for enhanced functionality
2. **Run side-by-side** with existing handlers during migration
3. **Use core functions** while keeping existing HTTP handler structure

Example integration:
```go
// In cmd/webui/main.go

import "github.com/denysvitali/pictures-sync-s3/pkg/photoviewer"

// Replace or supplement existing endpoints
http.HandleFunc("/api/sdcard/files", func(w http.ResponseWriter, r *http.Request) {
    // Check if SD card is mounted
    if !stateMgr.GetState().SDCardMounted {
        http.Error(w, "No SD card mounted", http.StatusServiceUnavailable)
        return
    }

    // Use photoviewer handler
    photoviewer.HandleListPhotos(w, r, state.MountDir)
})
```

## Testing

Run the comprehensive test suite:

```bash
# Run all tests
go test ./pkg/photoviewer -v

# Run specific test
go test ./pkg/photoviewer -run TestScanDCIM

# Run with coverage
go test ./pkg/photoviewer -cover
```

The test suite includes:
- File type detection tests
- Recursive and non-recursive scanning
- Path validation and security tests
- Filter application tests
- Edge cases and error handling

## Performance Considerations

1. **Recursive scanning** can be slow for large SD cards (1000+ photos)
   - Consider using pagination
   - Cache results when possible
   - Use non-recursive scanning for better performance

2. **Thumbnail generation** (when implemented) should:
   - Use a persistent cache
   - Generate thumbnails on-demand
   - Consider background processing for large collections

3. **MIME type detection** is fast and uses Go's built-in `mime` package

## Future Enhancements

- [ ] Thumbnail generation with caching
- [ ] EXIF metadata extraction (camera, GPS, settings)
- [ ] Image dimension detection
- [ ] Background thumbnail pre-generation
- [ ] Photo deduplication by hash
- [ ] Sorting options (date, size, name)
- [ ] Search by filename pattern
- [ ] Virtual albums/grouping by date

## License

Part of the pictures-sync-s3 project.
