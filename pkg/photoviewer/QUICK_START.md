# Quick Start Guide

## Installation

The package is already part of the pictures-sync-s3 project. Import it:

```go
import "github.com/denysvitali/pictures-sync-s3/pkg/photoviewer"
```

## 5-Minute Integration

### Step 1: Add HTTP Handlers

Add to `cmd/webui/main.go`:

```go
import "github.com/denysvitali/pictures-sync-s3/pkg/photoviewer"

// In main() function, add these routes:
http.HandleFunc("/api/photos/list", func(w http.ResponseWriter, r *http.Request) {
    if !stateMgr.GetState().SDCardMounted {
        http.Error(w, "No SD card mounted", http.StatusServiceUnavailable)
        return
    }
    photoviewer.HandleListPhotos(w, r, state.MountDir)
})

http.HandleFunc("/api/photos/view", func(w http.ResponseWriter, r *http.Request) {
    if !stateMgr.GetState().SDCardMounted {
        http.Error(w, "No SD card mounted", http.StatusServiceUnavailable)
        return
    }
    photoviewer.HandleServePhoto(w, r, state.MountDir)
})
```

### Step 2: Test the Endpoints

```bash
# Rebuild and run webui
go build ./cmd/webui
PORT=8080 ./webui

# Test listing photos
curl "http://localhost:8080/api/photos/list?recursive=true"

# Test viewing a photo
curl "http://localhost:8080/api/photos/view?path=100CANON/IMG_0001.JPG" > photo.jpg
```

### Step 3: Add to Web UI

Add JavaScript to your HTML:

```javascript
// Fetch and display photos
fetch('/api/photos/list?recursive=true')
    .then(res => res.json())
    .then(data => {
        const gallery = document.getElementById('gallery');
        data.photos.forEach(photo => {
            const img = document.createElement('img');
            img.src = `/api/photos/view?path=${encodeURIComponent(photo.path)}`;
            img.alt = photo.name;
            img.title = `${photo.name} (${photo.size_human})`;
            gallery.appendChild(img);
        });
    });
```

## Common Use Cases

### Use Case 1: Photo Gallery

```go
func handleGallery(w http.ResponseWriter, r *http.Request) {
    // Get all photos
    result, _ := photoviewer.ScanDCIM(state.MountDir)

    // Filter for images only (no videos/RAW)
    filter := &photoviewer.PhotoFilter{OnlyImage: true}
    photos := filter.Filter(result.Photos)

    // Return first 50 for gallery
    if len(photos) > 50 {
        photos = photos[:50]
    }

    json.NewEncoder(w).Encode(photos)
}
```

### Use Case 2: Directory Browser

```go
func handleBrowse(w http.ResponseWriter, r *http.Request) {
    path := r.URL.Query().Get("path")
    result, _ := photoviewer.ScanDirectory(state.MountDir, path)

    json.NewEncoder(w).Encode(map[string]interface{}{
        "current_path": path,
        "directories":  result.Directories,
        "photos":       result.Photos,
    })
}
```

### Use Case 3: Recent Photos

```go
func handleRecent(w http.ResponseWriter, r *http.Request) {
    result, _ := photoviewer.ScanDCIM(state.MountDir)

    // Sort by date (most recent first)
    photos := result.Photos
    sort.Slice(photos, func(i, j int) bool {
        return photos[i].ModTime.After(photos[j].ModTime)
    })

    // Return 20 most recent
    if len(photos) > 20 {
        photos = photos[:20]
    }

    json.NewEncoder(w).Encode(photos)
}
```

### Use Case 4: Statistics

```go
func handleStats(w http.ResponseWriter, r *http.Request) {
    result, _ := photoviewer.ScanDCIM(state.MountDir)

    stats := struct {
        TotalFiles  int    `json:"total_files"`
        TotalSize   string `json:"total_size"`
        ImageCount  int    `json:"image_count"`
        VideoCount  int    `json:"video_count"`
        RAWCount    int    `json:"raw_count"`
    }{
        TotalFiles: result.TotalCount,
        TotalSize:  formatBytes(result.TotalSize),
    }

    for _, photo := range result.Photos {
        if photo.IsRAW {
            stats.RAWCount++
        } else if photo.IsVideo {
            stats.VideoCount++
        } else {
            stats.ImageCount++
        }
    }

    json.NewEncoder(w).Encode(stats)
}
```

## API Cheat Sheet

### List All Photos
```bash
curl "http://localhost:8080/api/photos/list?recursive=true"
```

### List Specific Folder
```bash
curl "http://localhost:8080/api/photos/list?path=100CANON"
```

### Filter by Type
```bash
# Only RAW files
curl "http://localhost:8080/api/photos/list?recursive=true&only_raw=true"

# Only videos
curl "http://localhost:8080/api/photos/list?recursive=true&only_video=true"

# Only standard images
curl "http://localhost:8080/api/photos/list?recursive=true&only_image=true"
```

### Filter by Size
```bash
# Files larger than 5MB
curl "http://localhost:8080/api/photos/list?recursive=true&min_size=5242880"

# Files smaller than 10MB
curl "http://localhost:8080/api/photos/list?recursive=true&max_size=10485760"
```

### Filter by Date
```bash
# Photos from last week
curl "http://localhost:8080/api/photos/list?recursive=true&after=2025-10-09T00:00:00Z"
```

### Pagination
```bash
# Page 2, 50 items per page
curl "http://localhost:8080/api/photos/list?recursive=true&page=2&page_size=50"
```

### View Photo
```bash
# Display in browser
curl "http://localhost:8080/api/photos/view?path=100CANON/IMG_0001.JPG"

# Download
curl "http://localhost:8080/api/photos/view?path=100CANON/IMG_0001.JPG&download=1" -O
```

### Thumbnail
```bash
# 200x200 thumbnail
curl "http://localhost:8080/api/photos/view?path=100CANON/IMG_0001.JPG"

# Custom size
curl "http://localhost:8080/api/photos/thumbnail?path=100CANON/IMG_0001.JPG&width=400&height=400"
```

## Response Format

### List Response
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

## Error Handling

All errors return JSON:

```json
{
  "error": "No SD card mounted"
}
```

HTTP status codes:
- `200` - Success
- `400` - Bad request (missing/invalid parameters)
- `403` - Forbidden (path validation failed)
- `404` - Not found (file doesn't exist)
- `500` - Internal server error
- `503` - Service unavailable (SD card not mounted)

## Testing

```bash
# Run tests
go test ./pkg/photoviewer -v

# Check coverage
go test ./pkg/photoviewer -cover

# Run specific test
go test ./pkg/photoviewer -run TestScanDCIM
```

## Troubleshooting

### "DCIM directory not found"
- Ensure SD card is mounted at `/perm/pictures-sync/mounts/sdcard`
- Check that DCIM directory exists on the card

### "No photos found"
- Verify files have supported extensions
- Check file permissions
- Try recursive scan: `?recursive=true`

### "Access denied"
- Path validation prevents directory traversal
- Use relative paths from DCIM root
- Don't use `..` or absolute paths

### Slow scanning
- Use non-recursive scan for specific directories
- Implement pagination: `?page=1&page_size=50`
- Consider caching results

## Next Steps

- Read [README.md](README.md) for complete documentation
- See [INTEGRATION_EXAMPLE.go](INTEGRATION_EXAMPLE.go) for advanced patterns
- Check [../../../PHOTOVIEWER_IMPLEMENTATION.md](../../../PHOTOVIEWER_IMPLEMENTATION.md) for full implementation details
