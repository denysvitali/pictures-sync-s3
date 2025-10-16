package photoviewer

// This file contains example integration code for the webui.
// It shows how to integrate photoviewer handlers into cmd/webui/main.go

/*

// Example 1: Replace existing SD card file handlers

import (
	"net/http"
	"github.com/denysvitali/pictures-sync-s3/pkg/photoviewer"
	"github.com/denysvitali/pictures-sync-s3/pkg/state"
)

func setupPhotoViewerHandlers() {
	mountPath := state.MountDir

	// Enhanced photo listing with filtering and pagination
	http.HandleFunc("/api/sdcard/photos", func(w http.ResponseWriter, r *http.Request) {
		// Check if SD card is mounted
		currentState := stateMgr.GetState()
		if !currentState.SDCardMounted {
			http.Error(w, "No SD card mounted", http.StatusServiceUnavailable)
			return
		}

		// Delegate to photoviewer handler
		photoviewer.HandleListPhotos(w, r, mountPath)
	})

	// Serve full-resolution photos with proper caching
	http.HandleFunc("/api/sdcard/photo", func(w http.ResponseWriter, r *http.Request) {
		currentState := stateMgr.GetState()
		if !currentState.SDCardMounted {
			http.Error(w, "No SD card mounted", http.StatusServiceUnavailable)
			return
		}

		photoviewer.HandleServePhoto(w, r, mountPath)
	})

	// Serve thumbnails
	http.HandleFunc("/api/sdcard/thumbnail", func(w http.ResponseWriter, r *http.Request) {
		currentState := stateMgr.GetState()
		if !currentState.SDCardMounted {
			http.Error(w, "No SD card mounted", http.StatusServiceUnavailable)
			return
		}

		photoviewer.HandleServeThumbnail(w, r, mountPath)
	})
}

// Example 2: Use photoviewer functions with custom handlers

func handleCustomPhotoList(w http.ResponseWriter, r *http.Request) {
	// Check SD card mount status
	currentState := stateMgr.GetState()
	if !currentState.SDCardMounted {
		jsonResponse(w, map[string]interface{}{
			"error": "No SD card mounted",
		})
		return
	}

	// Get photos using photoviewer
	result, err := photoviewer.ScanDCIM(state.MountDir)
	if err != nil {
		jsonResponse(w, map[string]interface{}{
			"error": fmt.Sprintf("Failed to scan photos: %v", err),
		})
		return
	}

	// Add custom metadata or filtering
	type EnhancedPhoto struct {
		photoviewer.PhotoInfo
		ThumbnailURL string `json:"thumbnail_url"`
		ViewURL      string `json:"view_url"`
	}

	enhanced := make([]EnhancedPhoto, len(result.Photos))
	for i, photo := range result.Photos {
		enhanced[i] = EnhancedPhoto{
			PhotoInfo:    photo,
			ThumbnailURL: fmt.Sprintf("/api/sdcard/thumbnail?path=%s", url.QueryEscape(photo.Path)),
			ViewURL:      fmt.Sprintf("/api/sdcard/photo?path=%s", url.QueryEscape(photo.Path)),
		}
	}

	// Return custom response
	jsonResponse(w, map[string]interface{}{
		"photos":      enhanced,
		"total_count": result.TotalCount,
		"total_size":  result.TotalSize,
	})
}

// Example 3: Photo gallery with filtering

func handlePhotoGallery(w http.ResponseWriter, r *http.Request) {
	if !stateMgr.GetState().SDCardMounted {
		http.Error(w, "No SD card mounted", http.StatusServiceUnavailable)
		return
	}

	// Scan photos
	result, err := photoviewer.ScanDCIM(state.MountDir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Apply filters based on query parameters
	filter := &photoviewer.PhotoFilter{}

	// Filter by type
	switch r.URL.Query().Get("type") {
	case "raw":
		filter.OnlyRAW = true
	case "video":
		filter.OnlyVideo = true
	case "image":
		filter.OnlyImage = true
	}

	// Filter by size
	if minSize := r.URL.Query().Get("min_size"); minSize != "" {
		if size, err := strconv.ParseInt(minSize, 10, 64); err == nil {
			filter.MinSize = size
		}
	}

	// Apply filters
	filtered := filter.Filter(result.Photos)

	// Return filtered results
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"photos":      filtered,
		"total_count": len(filtered),
	})
}

// Example 4: Directory browsing interface

func handlePhotoBrowser(w http.ResponseWriter, r *http.Request) {
	if !stateMgr.GetState().SDCardMounted {
		http.Error(w, "No SD card mounted", http.StatusServiceUnavailable)
		return
	}

	// Get requested path
	path := r.URL.Query().Get("path")
	if path == "" {
		path = "."
	}

	// Scan directory (non-recursive)
	result, err := photoviewer.ScanDirectory(state.MountDir, path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Build response with directory navigation
	type BrowserResponse struct {
		CurrentPath   string                `json:"current_path"`
		Directories   []string              `json:"directories"`
		Photos        []photoviewer.PhotoInfo `json:"photos"`
		PhotoCount    int                   `json:"photo_count"`
		TotalSize     int64                 `json:"total_size"`
		TotalSizeText string                `json:"total_size_text"`
	}

	response := BrowserResponse{
		CurrentPath:   path,
		Directories:   result.Directories,
		Photos:        result.Photos,
		PhotoCount:    result.TotalCount,
		TotalSize:     result.TotalSize,
		TotalSizeText: formatBytes(result.TotalSize),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// Example 5: Statistics endpoint

func handlePhotoStats(w http.ResponseWriter, r *http.Request) {
	if !stateMgr.GetState().SDCardMounted {
		jsonResponse(w, map[string]interface{}{
			"error": "No SD card mounted",
		})
		return
	}

	// Scan all photos
	result, err := photoviewer.ScanDCIM(state.MountDir)
	if err != nil {
		jsonResponse(w, map[string]interface{}{
			"error": fmt.Sprintf("Failed to scan: %v", err),
		})
		return
	}

	// Calculate statistics
	var jpgCount, rawCount, videoCount int
	var jpgSize, rawSize, videoSize int64

	for _, photo := range result.Photos {
		if photo.IsVideo {
			videoCount++
			videoSize += photo.Size
		} else if photo.IsRAW {
			rawCount++
			rawSize += photo.Size
		} else if photo.IsImage {
			jpgCount++
			jpgSize += photo.Size
		}
	}

	// Return statistics
	jsonResponse(w, map[string]interface{}{
		"total_files": result.TotalCount,
		"total_size":  result.TotalSize,
		"statistics": map[string]interface{}{
			"images": map[string]interface{}{
				"count": jpgCount,
				"size":  jpgSize,
				"size_human": formatBytes(jpgSize),
			},
			"raw_files": map[string]interface{}{
				"count": rawCount,
				"size":  rawSize,
				"size_human": formatBytes(rawSize),
			},
			"videos": map[string]interface{}{
				"count": videoCount,
				"size":  videoSize,
				"size_human": formatBytes(videoSize),
			},
		},
		"directories": result.Directories,
	})
}

// Example 6: Recent photos endpoint

func handleRecentPhotos(w http.ResponseWriter, r *http.Request) {
	if !stateMgr.GetState().SDCardMounted {
		http.Error(w, "No SD card mounted", http.StatusServiceUnavailable)
		return
	}

	// Get number of photos to return (default 20)
	count := 20
	if c := r.URL.Query().Get("count"); c != "" {
		if n, err := strconv.Atoi(c); err == nil && n > 0 && n <= 100 {
			count = n
		}
	}

	// Scan all photos
	result, err := photoviewer.ScanDCIM(state.MountDir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Sort by modification time (most recent first)
	photos := result.Photos
	sort.Slice(photos, func(i, j int) bool {
		return photos[i].ModTime.After(photos[j].ModTime)
	})

	// Take the most recent N photos
	if len(photos) > count {
		photos = photos[:count]
	}

	// Return recent photos
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"photos": photos,
		"count":  len(photos),
	})
}

// Example 7: Integration with existing HTML template

func handlePhotoGalleryPage(w http.ResponseWriter, r *http.Request) {
	data := struct {
		Title       string
		SDCardMounted bool
		Photos      []photoviewer.PhotoInfo
		Error       string
	}{
		Title: "Photo Gallery",
		SDCardMounted: stateMgr.GetState().SDCardMounted,
	}

	if data.SDCardMounted {
		result, err := photoviewer.ScanDCIM(state.MountDir)
		if err != nil {
			data.Error = fmt.Sprintf("Failed to load photos: %v", err)
		} else {
			data.Photos = result.Photos
		}
	}

	// Render HTML template
	tmpl := `
<!DOCTYPE html>
<html>
<head>
    <title>{{.Title}}</title>
    <style>
        .gallery { display: grid; grid-template-columns: repeat(auto-fill, minmax(200px, 1fr)); gap: 1rem; padding: 1rem; }
        .photo { border: 1px solid #ddd; border-radius: 4px; overflow: hidden; }
        .photo img { width: 100%; height: 200px; object-fit: cover; }
        .photo-info { padding: 0.5rem; font-size: 0.85rem; }
        .error { color: red; padding: 1rem; }
    </style>
</head>
<body>
    <h1>{{.Title}}</h1>

    {{if not .SDCardMounted}}
        <p class="error">No SD card mounted</p>
    {{else if .Error}}
        <p class="error">{{.Error}}</p>
    {{else}}
        <div class="gallery">
            {{range .Photos}}
            <div class="photo">
                <img src="/api/sdcard/thumbnail?path={{.Path}}" alt="{{.Name}}">
                <div class="photo-info">
                    <div><strong>{{.Name}}</strong></div>
                    <div>{{.SizeHuman}}</div>
                    <div>{{if .IsRAW}}RAW{{else if .IsVideo}}Video{{else}}Image{{end}}</div>
                </div>
            </div>
            {{end}}
        </div>
    {{end}}
</body>
</html>
`
	t, err := template.New("gallery").Parse(tmpl)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	t.Execute(w, data)
}

*/
