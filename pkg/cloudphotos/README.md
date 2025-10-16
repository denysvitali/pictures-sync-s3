# cloudphotos

Package `cloudphotos` provides functionality for listing and downloading photos from cloud storage using rclone, with built-in caching support.

## Features

- **List Card IDs**: Get all card IDs with photo counts and total sizes
- **List Photos**: List all photos for a specific card with metadata
- **Download Photos**: Download photos from cloud storage with automatic caching
- **Cache Management**: Automatic cache cleanup with configurable TTL and size limits
- **Integration**: Works seamlessly with existing `syncmanager` and rclone configuration

## Installation

```go
import "github.com/denysvitali/pictures-sync-s3/pkg/cloudphotos"
```

## Usage

### Initialize Manager

```go
import (
    "github.com/denysvitali/pictures-sync-s3/pkg/cloudphotos"
    "github.com/denysvitali/pictures-sync-s3/pkg/state"
    "github.com/denysvitali/pictures-sync-s3/pkg/syncmanager"
)

// Initialize state and sync managers
stateMgr, _ := state.NewManager()
syncMgr := syncmanager.NewManager(
    "/perm/pictures-sync/rclone.conf",
    "remote",
    "/photos",
    stateMgr,
    4, // transfers
    8, // checkers
)

// Create cloud photos manager
photoMgr, err := cloudphotos.NewManager(
    "/perm/pictures-sync/rclone.conf",
    "remote",
    "/photos",
    syncMgr,
)
```

### List All Cards

```go
// Get all cards with photo information
cards, err := photoMgr.ListCards()
if err != nil {
    log.Fatal(err)
}

for _, card := range cards {
    fmt.Printf("Card: %s\n", card.CardID)
    fmt.Printf("  Photos: %d\n", card.PhotoCount)
    fmt.Printf("  Total Size: %s\n", card.TotalSizeHuman)
    fmt.Printf("  Last Modified: %s\n", card.LastModified)
}
```

### List Photos for a Card

```go
cardID := "card-a1b2c3d4"
photos, err := photoMgr.ListPhotos(cardID)
if err != nil {
    log.Fatal(err)
}

for _, photo := range photos {
    fmt.Printf("%s - %s (%s)\n",
        photo.Name,
        photo.ModTime.Format("2006-01-02"),
        photo.SizeHuman)
}
```

### Download Photo

```go
// Download photo from cloud storage
photoPath := "card-a1b2c3d4/DCIM/IMG_1234.JPG"
cachedPath, err := photoMgr.DownloadPhoto(photoPath)
if err != nil {
    log.Fatal(err)
}

fmt.Printf("Photo cached at: %s\n", cachedPath)

// Check if photo is already cached (instant if cached)
if cached := photoMgr.GetCachedPhoto(photoPath); cached != "" {
    // Use cached file
    file, _ := os.Open(cached)
    defer file.Close()
}
```

### Cache Management

```go
// Get cache statistics
stats, err := photoMgr.GetCacheStats()
if err != nil {
    log.Fatal(err)
}

fmt.Printf("Cache: %s / %s (%.1f%% full)\n",
    stats["total_size_human"],
    stats["max_size_human"],
    stats["usage_percent"])
fmt.Printf("Files: %d\n", stats["file_count"])
fmt.Printf("TTL: %.0f hours\n", stats["ttl_hours"])

// Clear entire cache if needed
if stats["usage_percent"].(float64) > 90 {
    photoMgr.ClearCache()
}
```

## Data Structures

### PhotoInfo

```go
type PhotoInfo struct {
    Name        string    // Filename (e.g., "IMG_1234.JPG")
    Path        string    // Full path relative to photos root
    Size        int64     // File size in bytes
    SizeHuman   string    // Human-readable size (e.g., "5.2 MB")
    ModTime     time.Time // Last modification time
    CardID      string    // Card ID this photo belongs to
    IsImage     bool      // True if file is an image
    IsCached    bool      // True if file is in local cache
}
```

### CardPhotos

```go
type CardPhotos struct {
    CardID         string      // Unique card identifier
    PhotoCount     int         // Number of photos on card
    TotalSize      int64       // Total size in bytes
    TotalSizeHuman string      // Human-readable total size
    LastModified   time.Time   // Most recent photo timestamp
    Photos         []PhotoInfo // All photos on card
}
```

## Configuration

### Cache Settings

The cache has the following default settings (defined in constants):

- **CacheDir**: `/perm/pictures-sync/photo-cache`
- **CacheMaxSize**: 500 MB (500 * 1024 * 1024 bytes)
- **CacheTTL**: 24 hours

### Automatic Cleanup

The cache automatically cleans up:
1. **Expired entries**: Files older than CacheTTL are removed
2. **Size management**: If cache exceeds CacheMaxSize, oldest files are removed
3. **Cleanup runs**: On manager initialization and after each download

## Implementation Details

### Rclone Integration

The package uses the existing rclone configuration and syncmanager:
- Loads config from `/perm/pictures-sync/rclone.conf`
- Uses the same remote name and path from settings
- Leverages `syncmanager.GetFile()` for downloads
- Supports all rclone backends (B2, S3, Drive, Azure, etc.)

### Path Structure

Photos are organized as:
```
remote:/photos/
  ├── card-a1b2c3d4/
  │   └── DCIM/
  │       ├── 100CANON/
  │       │   ├── IMG_1234.JPG
  │       │   └── IMG_1235.JPG
  │       └── 101CANON/
  │           └── IMG_2345.JPG
  └── card-87654321/
      └── DCIM/
          └── IMG_5678.JPG
```

### Cache Structure

Cached files are stored with SHA256-hashed paths to avoid filesystem issues:
```
/perm/pictures-sync/photo-cache/
  ├── ab/
  │   └── cd/
  │       └── abcd1234...hash.JPG
  └── 12/
      └── 34/
          └── 1234abcd...hash.PNG
```

This structure:
- Prevents path traversal attacks
- Handles special characters safely
- Distributes files across directories
- Preserves original file extensions

### Supported Image Formats

- JPEG (.jpg, .jpeg)
- PNG (.png)
- GIF (.gif)
- BMP (.bmp)
- TIFF (.tiff, .tif)
- WebP (.webp)
- HEIC/HEIF (.heic, .heif)
- RAW formats (.raw, .cr2, .nef, .arw, .dng)

### Thread Safety

All operations are thread-safe:
- Manager uses `sync.RWMutex` for state protection
- Cache operations are atomic
- Safe for concurrent access from multiple goroutines

## Error Handling

The package returns descriptive errors for:
- Invalid card IDs (path traversal attempts)
- Missing or inaccessible remote paths
- Download failures
- Cache operations failures

Example:
```go
photos, err := photoMgr.ListPhotos("../etc/passwd")
// Returns: invalid card ID: card ID contains invalid characters
```

## Performance Considerations

### Listing Performance

- Card listing uses existing `syncmanager.ListCardIDs()`
- Photo listing walks directory tree recursively
- Large photo collections may take several seconds to list
- Results are sorted by modification time (most recent first)

### Download Performance

- First download: Fetches from cloud storage (slow)
- Subsequent access: Instant from cache (if within TTL)
- Parallel downloads supported (uses syncmanager's transfer settings)

### Cache Performance

- Cleanup runs in background goroutine
- Throttled to avoid excessive disk I/O
- LRU eviction based on file modification time

## Integration with Web UI

The package is designed to integrate with the web UI:

```go
// API endpoint to list cards
http.HandleFunc("/api/photos/cards", func(w http.ResponseWriter, r *http.Request) {
    cards, err := photoMgr.ListCards()
    if err != nil {
        http.Error(w, err.Error(), 500)
        return
    }
    json.NewEncoder(w).Encode(cards)
})

// API endpoint to download photo
http.HandleFunc("/api/photos/download", func(w http.ResponseWriter, r *http.Request) {
    photoPath := r.URL.Query().Get("path")

    cachedPath, err := photoMgr.DownloadPhoto(photoPath)
    if err != nil {
        http.Error(w, err.Error(), 500)
        return
    }

    http.ServeFile(w, r, cachedPath)
})
```

## Testing

Run tests:
```bash
go test ./pkg/cloudphotos/...
```

Run tests with coverage:
```bash
go test -cover ./pkg/cloudphotos/...
```

## Dependencies

- `github.com/rclone/rclone` - Cloud storage operations
- `github.com/denysvitali/pictures-sync-s3/pkg/syncmanager` - Sync manager integration
- Standard library: `crypto/sha256`, `path/filepath`, `os`, `time`, etc.

## License

Same as parent project.
