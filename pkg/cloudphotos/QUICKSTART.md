# Quick Start Guide

Get started with the cloudphotos package in 5 minutes.

## Installation

The package is already part of the project. No additional installation needed.

## Basic Usage

### 1. Create a Simple Photo Viewer

```go
package main

import (
    "fmt"
    "log"

    "github.com/denysvitali/pictures-sync-s3/pkg/cloudphotos"
    "github.com/denysvitali/pictures-sync-s3/pkg/state"
    "github.com/denysvitali/pictures-sync-s3/pkg/syncmanager"
)

func main() {
    // Setup managers
    stateMgr, _ := state.NewManager()
    syncMgr := syncmanager.NewManager(
        "/perm/pictures-sync/rclone.conf",
        "remote",
        "/photos",
        stateMgr,
        4, 8,
    )

    // Create photo manager
    photoMgr, err := cloudphotos.NewManager(
        "/perm/pictures-sync/rclone.conf",
        "remote",
        "/photos",
        syncMgr,
    )
    if err != nil {
        log.Fatal(err)
    }

    // List cards
    cards, _ := photoMgr.ListCards()
    for _, card := range cards {
        fmt.Printf("%s: %d photos\n", card.CardID, card.PhotoCount)
    }
}
```

### 2. Add HTTP Endpoints

```go
package main

import (
    "encoding/json"
    "net/http"

    "github.com/denysvitali/pictures-sync-s3/pkg/cloudphotos"
    // ... other imports
)

func main() {
    // ... initialize photoMgr ...

    // List cards endpoint
    http.HandleFunc("/api/cards", func(w http.ResponseWriter, r *http.Request) {
        cards, _ := photoMgr.ListCards()
        json.NewEncoder(w).Encode(cards)
    })

    // List photos endpoint
    http.HandleFunc("/api/photos", func(w http.ResponseWriter, r *http.Request) {
        cardID := r.URL.Query().Get("card")
        photos, _ := photoMgr.ListPhotos(cardID)
        json.NewEncoder(w).Encode(photos)
    })

    // Download photo endpoint
    http.HandleFunc("/api/download", func(w http.ResponseWriter, r *http.Request) {
        path := r.URL.Query().Get("path")
        cachedPath, _ := photoMgr.DownloadPhoto(path)
        http.ServeFile(w, r, cachedPath)
    })

    http.ListenAndServe(":8080", nil)
}
```

### 3. Add Simple Web UI

Save this as `index.html`:

```html
<!DOCTYPE html>
<html>
<head>
    <title>Photo Gallery</title>
    <style>
        body { font-family: sans-serif; margin: 20px; }
        .photo-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(200px, 1fr)); gap: 10px; }
        .photo { border: 1px solid #ddd; padding: 10px; }
        .photo img { width: 100%; height: auto; cursor: pointer; }
    </style>
</head>
<body>
    <h1>Photo Gallery</h1>

    <select id="cards" onchange="loadPhotos()">
        <option>Loading cards...</option>
    </select>

    <div id="photos" class="photo-grid"></div>

    <script>
        // Load cards
        fetch('/api/cards')
            .then(r => r.json())
            .then(cards => {
                const select = document.getElementById('cards');
                select.innerHTML = '<option value="">Select a card</option>';
                cards.forEach(c => {
                    const opt = document.createElement('option');
                    opt.value = c.card_id;
                    opt.textContent = `${c.card_id} (${c.photo_count} photos)`;
                    select.appendChild(opt);
                });
            });

        // Load photos for selected card
        function loadPhotos() {
            const cardID = document.getElementById('cards').value;
            if (!cardID) return;

            fetch(`/api/photos?card=${cardID}`)
                .then(r => r.json())
                .then(photos => {
                    const grid = document.getElementById('photos');
                    grid.innerHTML = '';

                    photos.forEach(p => {
                        const div = document.createElement('div');
                        div.className = 'photo';
                        div.innerHTML = `
                            <img src="/api/download?path=${encodeURIComponent(p.path)}"
                                 alt="${p.name}"
                                 loading="lazy">
                            <div>${p.name}</div>
                            <div>${p.size_human}</div>
                        `;
                        grid.appendChild(div);
                    });
                });
        }
    </script>
</body>
</html>
```

Serve it with:
```go
http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
    http.ServeFile(w, r, "index.html")
})
```

## Common Tasks

### List All Photos
```go
cards, _ := photoMgr.ListCards()
for _, card := range cards {
    photos, _ := photoMgr.ListPhotos(card.CardID)
    for _, photo := range photos {
        fmt.Printf("%s/%s\n", card.CardID, photo.Name)
    }
}
```

### Download Specific Photo
```go
path := "card-a1b2c3d4/DCIM/IMG_1234.JPG"
cachedPath, err := photoMgr.DownloadPhoto(path)
if err != nil {
    log.Fatal(err)
}
fmt.Printf("Photo saved to: %s\n", cachedPath)
```

### Check Cache Status
```go
stats, _ := photoMgr.GetCacheStats()
fmt.Printf("Cache: %s / %s\n",
    stats["total_size_human"],
    stats["max_size_human"])
fmt.Printf("Files: %d\n", stats["file_count"])
fmt.Printf("Usage: %.1f%%\n", stats["usage_percent"])
```

### Clear Cache
```go
if err := photoMgr.ClearCache(); err != nil {
    log.Fatal(err)
}
fmt.Println("Cache cleared")
```

## Integration with Existing Code

### In cmd/webui/main.go

```go
// Add near other manager initializations
photoMgr, err := cloudphotos.NewManager(
    state.GetRcloneConfigPath(),
    settingsMgr.GetRemoteName(),
    settingsMgr.GetRemotePath(),
    syncMgr,
)
if err != nil {
    log.Printf("Warning: Failed to initialize photo manager: %v", err)
}

// Add API endpoints
http.HandleFunc("/api/photos/cards", func(w http.ResponseWriter, r *http.Request) {
    cards, err := photoMgr.ListCards()
    if err != nil {
        http.Error(w, err.Error(), 500)
        return
    }
    json.NewEncoder(w).Encode(cards)
})

http.HandleFunc("/api/photos/list", func(w http.ResponseWriter, r *http.Request) {
    cardID := r.URL.Query().Get("card")
    photos, err := photoMgr.ListPhotos(cardID)
    if err != nil {
        http.Error(w, err.Error(), 500)
        return
    }
    json.NewEncoder(w).Encode(photos)
})

http.HandleFunc("/api/photos/download", func(w http.ResponseWriter, r *http.Request) {
    path := r.URL.Query().Get("path")
    cachedPath, err := photoMgr.DownloadPhoto(path)
    if err != nil {
        http.Error(w, err.Error(), 500)
        return
    }
    http.ServeFile(w, r, cachedPath)
})
```

## Testing

### Run Tests
```bash
go test ./pkg/cloudphotos/...
```

### Run with Coverage
```bash
go test -cover ./pkg/cloudphotos/...
```

### Run Verbose
```bash
go test -v ./pkg/cloudphotos/...
```

## Troubleshooting

### "Failed to access remote path"
- Check rclone config exists at `/perm/pictures-sync/rclone.conf`
- Verify remote name matches config
- Test with `rclone ls remote:` command

### "Invalid card ID"
- Card IDs must be in format `card-XXXXXXXX`
- No `..`, `/`, or `\` characters allowed
- Check sync history for actual card IDs

### Photos not appearing
- Verify photos are in `{cardID}/DCIM/` path structure
- Check file extensions (only image files shown)
- Try listing with rclone: `rclone ls remote:/photos/card-xxx/DCIM/`

### Cache growing too large
```go
// Check cache size
stats, _ := photoMgr.GetCacheStats()
fmt.Printf("Cache: %.1f%% full\n", stats["usage_percent"])

// Clear if needed
if stats["usage_percent"].(float64) > 90 {
    photoMgr.ClearCache()
}
```

### Slow photo listing
- Large photo collections (10000+) take time to list
- Consider pagination for large collections
- Cache list results in your application

## Next Steps

1. Read the full [README.md](README.md) for detailed documentation
2. Check [INTEGRATION.md](INTEGRATION.md) for web UI integration
3. Review [examples_test.go](examples_test.go) for more usage patterns
4. See [SUMMARY.md](SUMMARY.md) for architecture overview

## Support

For issues or questions:
1. Check existing documentation
2. Review test files for usage examples
3. Check rclone documentation for backend-specific issues
4. Verify file structure matches expected format

## Quick Reference

```go
// Create manager
photoMgr, _ := cloudphotos.NewManager(configPath, remoteName, remotePath, syncMgr)

// List cards
cards, _ := photoMgr.ListCards()

// List photos
photos, _ := photoMgr.ListPhotos(cardID)

// Download photo
cachedPath, _ := photoMgr.DownloadPhoto(photoPath)

// Get cached photo
cachedPath := photoMgr.GetCachedPhoto(photoPath)

// Cache stats
stats, _ := photoMgr.GetCacheStats()

// Clear cache
photoMgr.ClearCache()

// Update remote
photoMgr.SetRemote(remoteName, remotePath)
```

That's it! You're ready to use the cloudphotos package.
