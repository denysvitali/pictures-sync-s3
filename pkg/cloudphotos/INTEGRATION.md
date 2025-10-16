# Integration Guide

This guide shows how to integrate the `cloudphotos` package into the existing web UI.

## Step 1: Initialize Cloud Photos Manager

In `cmd/webui/main.go`, add cloud photos manager initialization:

```go
import (
    "github.com/denysvitali/pictures-sync-s3/pkg/cloudphotos"
    // ... other imports
)

func main() {
    // ... existing initialization code ...

    // Initialize cloud photos manager
    photoMgr, err := cloudphotos.NewManager(
        state.GetRcloneConfigPath(),
        settings.GetRemoteName(),
        settings.GetRemotePath(),
        syncMgr,
    )
    if err != nil {
        log.Fatalf("Failed to initialize cloud photos manager: %v", err)
    }

    // Update remote when settings change
    // (Add to settings update handler)
    photoMgr.SetRemote(settings.GetRemoteName(), settings.GetRemotePath())
}
```

## Step 2: Add API Endpoints

Add new API endpoints to serve photo data:

```go
// List all cards with photo counts
http.HandleFunc("/api/photos/cards", func(w http.ResponseWriter, r *http.Request) {
    cards, err := photoMgr.ListCards()
    if err != nil {
        log.Printf("Error listing cards: %v", err)
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }

    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(cards)
})

// List photos for a specific card
http.HandleFunc("/api/photos/list", func(w http.ResponseWriter, r *http.Request) {
    cardID := r.URL.Query().Get("card")
    if cardID == "" {
        http.Error(w, "card parameter required", http.StatusBadRequest)
        return
    }

    photos, err := photoMgr.ListPhotos(cardID)
    if err != nil {
        log.Printf("Error listing photos for card %s: %v", cardID, err)
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }

    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(photos)
})

// Download/view a photo
http.HandleFunc("/api/photos/download", func(w http.ResponseWriter, r *http.Request) {
    photoPath := r.URL.Query().Get("path")
    if photoPath == "" {
        http.Error(w, "path parameter required", http.StatusBadRequest)
        return
    }

    // Check if already cached
    cachedPath := photoMgr.GetCachedPhoto(photoPath)
    if cachedPath == "" {
        // Download to cache
        var err error
        cachedPath, err = photoMgr.DownloadPhoto(photoPath)
        if err != nil {
            log.Printf("Error downloading photo %s: %v", photoPath, err)
            http.Error(w, err.Error(), http.StatusInternalServerError)
            return
        }
    }

    // Serve the cached file
    http.ServeFile(w, r, cachedPath)
})

// Get thumbnail (scaled down version)
http.HandleFunc("/api/photos/thumbnail", func(w http.ResponseWriter, r *http.Request) {
    photoPath := r.URL.Query().Get("path")
    if photoPath == "" {
        http.Error(w, "path parameter required", http.StatusBadRequest)
        return
    }

    // For now, serve full image (can add thumbnail generation later)
    cachedPath := photoMgr.GetCachedPhoto(photoPath)
    if cachedPath == "" {
        var err error
        cachedPath, err = photoMgr.DownloadPhoto(photoPath)
        if err != nil {
            log.Printf("Error downloading photo %s: %v", photoPath, err)
            http.Error(w, err.Error(), http.StatusInternalServerError)
            return
        }
    }

    http.ServeFile(w, r, cachedPath)
})

// Get cache statistics
http.HandleFunc("/api/photos/cache-stats", func(w http.ResponseWriter, r *http.Request) {
    stats, err := photoMgr.GetCacheStats()
    if err != nil {
        log.Printf("Error getting cache stats: %v", err)
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }

    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(stats)
})

// Clear cache
http.HandleFunc("/api/photos/clear-cache", func(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
        http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
        return
    }

    if err := photoMgr.ClearCache(); err != nil {
        log.Printf("Error clearing cache: %v", err)
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }

    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(map[string]string{
        "status": "success",
        "message": "Cache cleared",
    })
})
```

## Step 3: Add Web UI Tab

Add a new "Photos" tab to the web UI HTML:

```html
<!-- Add to tabs section -->
<div class="tabs">
  <button class="tab active" onclick="switchTab('status')">Status</button>
  <button class="tab" onclick="switchTab('history')">History</button>
  <button class="tab" onclick="switchTab('photos')">Photos</button>
  <button class="tab" onclick="switchTab('settings')">Settings</button>
  <button class="tab" onclick="switchTab('wifi')">WiFi</button>
</div>

<!-- Add photos tab content -->
<div id="photos-tab" class="tab-content">
  <h2>Photo Gallery</h2>

  <!-- Cache stats -->
  <div class="card">
    <h3>Cache Status</h3>
    <div id="cache-stats">
      <p>Loading cache statistics...</p>
    </div>
    <button onclick="clearPhotoCache()">Clear Cache</button>
  </div>

  <!-- Card selector -->
  <div class="card">
    <h3>Select Card</h3>
    <select id="card-selector" onchange="loadPhotosForCard()">
      <option value="">Loading cards...</option>
    </select>
  </div>

  <!-- Photo grid -->
  <div id="photo-grid" class="photo-grid">
    <p>Select a card to view photos</p>
  </div>
</div>
```

## Step 4: Add JavaScript Functions

Add JavaScript to load and display photos:

```javascript
// Load cards on page load
async function loadPhotoCards() {
    try {
        const response = await fetch('/api/photos/cards');
        const cards = await response.json();

        const selector = document.getElementById('card-selector');
        selector.innerHTML = '<option value="">Select a card...</option>';

        cards.forEach(card => {
            const option = document.createElement('option');
            option.value = card.card_id;
            option.textContent = `${card.card_id} (${card.photo_count} photos, ${card.total_size_human})`;
            selector.appendChild(option);
        });
    } catch (error) {
        console.error('Error loading cards:', error);
    }
}

// Load photos for selected card
async function loadPhotosForCard() {
    const cardID = document.getElementById('card-selector').value;
    if (!cardID) {
        document.getElementById('photo-grid').innerHTML = '<p>Select a card to view photos</p>';
        return;
    }

    document.getElementById('photo-grid').innerHTML = '<p>Loading photos...</p>';

    try {
        const response = await fetch(`/api/photos/list?card=${cardID}`);
        const photos = await response.json();

        const grid = document.getElementById('photo-grid');
        grid.innerHTML = '';

        if (photos.length === 0) {
            grid.innerHTML = '<p>No photos found</p>';
            return;
        }

        photos.forEach(photo => {
            const item = document.createElement('div');
            item.className = 'photo-item';

            const img = document.createElement('img');
            img.src = `/api/photos/thumbnail?path=${encodeURIComponent(photo.path)}`;
            img.alt = photo.name;
            img.loading = 'lazy';
            img.onclick = () => viewPhoto(photo);

            const caption = document.createElement('div');
            caption.className = 'photo-caption';
            caption.textContent = photo.name;

            item.appendChild(img);
            item.appendChild(caption);
            grid.appendChild(item);
        });
    } catch (error) {
        console.error('Error loading photos:', error);
        document.getElementById('photo-grid').innerHTML = '<p>Error loading photos</p>';
    }
}

// View photo in modal/lightbox
function viewPhoto(photo) {
    // Create modal to show full-size photo
    const modal = document.createElement('div');
    modal.className = 'photo-modal';
    modal.onclick = () => modal.remove();

    const img = document.createElement('img');
    img.src = `/api/photos/download?path=${encodeURIComponent(photo.path)}`;
    img.alt = photo.name;

    const info = document.createElement('div');
    info.className = 'photo-info';
    info.innerHTML = `
        <h3>${photo.name}</h3>
        <p>Size: ${photo.size_human}</p>
        <p>Date: ${new Date(photo.mod_time).toLocaleString()}</p>
        <p>Card: ${photo.card_id}</p>
    `;

    modal.appendChild(img);
    modal.appendChild(info);
    document.body.appendChild(modal);
}

// Load cache stats
async function loadCacheStats() {
    try {
        const response = await fetch('/api/photos/cache-stats');
        const stats = await response.json();

        document.getElementById('cache-stats').innerHTML = `
            <p>Files: ${stats.file_count}</p>
            <p>Size: ${stats.total_size_human} / ${stats.max_size_human}</p>
            <p>Usage: ${stats.usage_percent.toFixed(1)}%</p>
            <p>TTL: ${stats.ttl_hours} hours</p>
        `;
    } catch (error) {
        console.error('Error loading cache stats:', error);
    }
}

// Clear photo cache
async function clearPhotoCache() {
    if (!confirm('Clear all cached photos?')) {
        return;
    }

    try {
        const response = await fetch('/api/photos/clear-cache', {
            method: 'POST'
        });
        const result = await response.json();

        alert(result.message);
        loadCacheStats();
    } catch (error) {
        console.error('Error clearing cache:', error);
        alert('Error clearing cache');
    }
}

// Load photos tab when selected
function switchTab(tabName) {
    // ... existing tab switching code ...

    if (tabName === 'photos') {
        loadPhotoCards();
        loadCacheStats();
    }
}
```

## Step 5: Add CSS Styles

Add styles for the photo gallery:

```css
/* Photo grid */
.photo-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(200px, 1fr));
    gap: 1rem;
    margin-top: 1rem;
}

.photo-item {
    border: 1px solid #ddd;
    border-radius: 4px;
    overflow: hidden;
    cursor: pointer;
    transition: transform 0.2s;
}

.photo-item:hover {
    transform: scale(1.05);
    box-shadow: 0 4px 8px rgba(0,0,0,0.1);
}

.photo-item img {
    width: 100%;
    height: 200px;
    object-fit: cover;
}

.photo-caption {
    padding: 0.5rem;
    font-size: 0.9rem;
    text-align: center;
    background: #f5f5f5;
}

/* Photo modal */
.photo-modal {
    position: fixed;
    top: 0;
    left: 0;
    width: 100%;
    height: 100%;
    background: rgba(0,0,0,0.9);
    display: flex;
    align-items: center;
    justify-content: center;
    z-index: 1000;
    padding: 2rem;
}

.photo-modal img {
    max-width: 90%;
    max-height: 90%;
    object-fit: contain;
}

.photo-info {
    position: absolute;
    bottom: 2rem;
    left: 2rem;
    background: rgba(255,255,255,0.9);
    padding: 1rem;
    border-radius: 4px;
    color: #333;
}

.photo-info h3 {
    margin: 0 0 0.5rem 0;
}

.photo-info p {
    margin: 0.25rem 0;
}
```

## Step 6: Update Settings Handler

Update the settings handler to also update the photo manager:

```go
http.HandleFunc("/api/settings", func(w http.ResponseWriter, r *http.Request) {
    if r.Method == http.MethodPost {
        // ... existing settings update code ...

        // Update photo manager remote
        photoMgr.SetRemote(settings.GetRemoteName(), settings.GetRemotePath())
    }
    // ... rest of handler ...
})
```

## Testing the Integration

1. Start the web UI:
   ```bash
   PORT=8080 ./webui
   ```

2. Navigate to `http://localhost:8080`

3. Click the "Photos" tab

4. Select a card from the dropdown

5. Photos should load in a grid

6. Click a photo to view full size

7. Check cache stats in the Cache Status section

## Performance Tips

1. **Lazy Loading**: Images use `loading="lazy"` to only load visible photos

2. **Thumbnail Generation**: Consider adding thumbnail generation for faster loading:
   ```go
   // Use image/jpeg package to generate thumbnails
   img, _ := jpeg.Decode(source)
   thumb := resize.Thumbnail(200, 200, img, resize.Lanczos3)
   jpeg.Encode(dest, thumb, nil)
   ```

3. **Pagination**: For large photo collections, add pagination:
   ```javascript
   async function loadPhotosForCard(page = 1, pageSize = 50) {
       const response = await fetch(
           `/api/photos/list?card=${cardID}&page=${page}&pageSize=${pageSize}`
       );
       // ... handle response ...
   }
   ```

4. **Cache Warming**: Pre-cache thumbnails in background:
   ```go
   go func() {
       photos, _ := photoMgr.ListPhotos(cardID)
       for _, photo := range photos[:10] { // First 10 photos
           photoMgr.DownloadPhoto(photo.Path)
       }
   }()
   ```

## Future Enhancements

1. **EXIF Data**: Extract and display camera settings, GPS coordinates
2. **Search**: Search photos by date, camera, location
3. **Albums**: Group photos by date or event
4. **Sharing**: Generate shareable links for photos
5. **Download Zip**: Download multiple photos as zip file
6. **Slideshow**: Auto-play slideshow mode
7. **Upload**: Upload new photos to cloud storage
8. **Edit**: Basic image editing (crop, rotate, filters)

## Security Considerations

1. **Path Validation**: The package validates all paths to prevent traversal attacks
2. **Authentication**: Add authentication to photo endpoints if needed
3. **Rate Limiting**: Consider rate limiting download endpoints
4. **CORS**: Configure CORS if web UI is on different domain
