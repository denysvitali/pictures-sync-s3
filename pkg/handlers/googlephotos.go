package handlers

import (
	"context"
	"log"
	"net/http"
	"strings"
)

// HandleGooglePhotosStatus returns whether Google Photos is configured via
// rclone. "configured" means the gphotos remote exists in rclone config and
// GooglePhotosEnabled + GooglePhotosRemoteName are set.
func (ctx *Context) HandleGooglePhotosStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	remotes, err := ctx.SyncMgr.ListRemotes()
	if err != nil {
		log.Printf("[GooglePhotos] Failed to list remotes: %v", err)
	}

	configured := false
	gpRemoteName := ""
	if ctx.AppSettings != nil {
		configured = ctx.AppSettings.GetGooglePhotosEnabled()
		gpRemoteName = ctx.AppSettings.GetGooglePhotosRemoteName()
	}

	// "connected" means the remote name is present in rclone config.
	connected := false
	if gpRemoteName != "" {
		for _, name := range remotes {
			if strings.EqualFold(name, gpRemoteName) {
				connected = true
				break
			}
		}
	}

	JSONResponse(w, map[string]interface{}{
		"configured": configured && gpRemoteName != "",
		"connected":  connected,
	})
}

// HandleGooglePhotosAuthStart returns 410 Gone — native OAuth was removed.
func (ctx *Context) HandleGooglePhotosAuthStart(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "Native Google Photos OAuth is no longer supported. Configure a googlephotos remote in rclone instead.", http.StatusGone)
}

// HandleGooglePhotosAuthCallback returns 410 Gone.
func (ctx *Context) HandleGooglePhotosAuthCallback(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "Native Google Photos OAuth is no longer supported.", http.StatusGone)
}

// HandleGooglePhotosAuthDisconnect returns 410 Gone.
func (ctx *Context) HandleGooglePhotosAuthDisconnect(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "Native Google Photos OAuth is no longer supported.", http.StatusGone)
}

// HandleGooglePhotosSync triggers a B2 to Google Photos sync via rclone.
func (ctx *Context) HandleGooglePhotosSync(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodDelete {
		ctx.HandleGooglePhotosSyncCancel(w, r)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if ctx.SyncMgr == nil {
		http.Error(w, "Sync manager not initialized", http.StatusServiceUnavailable)
		return
	}

	if ctx.SyncMgr.IsGooglePhotosRunning() {
		http.Error(w, "sync already in progress", http.StatusConflict)
		return
	}

	// Start sync in background.
	go func() {
		if err := ctx.SyncMgr.SyncCardsToGooglePhotos(context.Background()); err != nil {
			log.Printf("[GooglePhotos] Sync error: %v", err)
		}
	}()

	JSONResponse(w, map[string]interface{}{
		"started": true,
		"status":  "syncing",
	})
}

// HandleGooglePhotosSyncCancel cancels the current Google Photos sync.
func (ctx *Context) HandleGooglePhotosSyncCancel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if ctx.SyncMgr == nil {
		http.Error(w, "Sync manager not initialized", http.StatusServiceUnavailable)
		return
	}
	if !ctx.SyncMgr.IsGooglePhotosRunning() {
		JSONResponse(w, map[string]interface{}{"cancelled": false, "status": "idle"})
		return
	}
	if err := ctx.SyncMgr.CancelGooglePhotos(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	JSONResponse(w, map[string]interface{}{"cancelled": true, "status": "cancelling"})
}

// HandleGooglePhotosSyncProgress returns the current Google Photos sync progress.
func (ctx *Context) HandleGooglePhotosSyncProgress(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if ctx.SyncMgr == nil {
		JSONResponse(w, map[string]interface{}{
			"status": "not_initialized",
		})
		return
	}

	progress := ctx.SyncMgr.GetGooglePhotosProgress()
	JSONResponse(w, map[string]interface{}{
		"status":             progress.Status,
		"current_file":       progress.CurrentFile,
		"current_file_size":  progress.CurrentFileSize,
		"transferred_files":  progress.TransferredFiles,
		"total_files":        progress.TotalFiles,
		"bytes_transferred":  progress.BytesTransferred,
		"percentage":         progress.Percentage,
		"speed":              progress.Speed,
		"eta":                progress.ETA,
	})
}

// HandleGooglePhotosSyncHistoryExport returns 404 — no longer supported.
func (ctx *Context) HandleGooglePhotosSyncHistoryExport(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "Not found", http.StatusNotFound)
}

// HandleGooglePhotosAlbums returns 404 — album management via native API removed.
func (ctx *Context) HandleGooglePhotosAlbums(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "Album management is not available with rclone-based Google Photos sync.", http.StatusNotFound)
}
