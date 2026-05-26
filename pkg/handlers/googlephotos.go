package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/denysvitali/pictures-sync-s3/pkg/googlephotos"
)

// GooglePhotosManager describes the Google Photos operations used by HTTP handlers.
type GooglePhotosManager interface {
	IsAuthenticated() bool
	GetConnectionStatus() (*googlephotos.ConnectionStatus, error)
	ExchangeCode(code, redirectURI, codeVerifier string) (*googlephotos.OAuthToken, error)
	Disconnect() error
	ListAlbums() ([]*googlephotos.Album, error)
	CreateAlbum(title string) (*googlephotos.Album, error)
}

// GooglePhotosSyncManager describes the sync operations for Google Photos.
type GooglePhotosSyncManager interface {
	IsRunning() bool
	Sync(ctx context.Context) error
	Cancel()
	Progress() *googlephotos.SyncProgress
}

type googlePhotosSyncOptions interface {
	SetSkipDuplicates(bool)
}

// HandleGooglePhotosStatus returns the Google Photos connection status
func (ctx *Context) HandleGooglePhotosStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx.EnsureGooglePhotosClient()
	if ctx.GooglePhotosClient == nil {
		JSONResponse(w, map[string]interface{}{
			"connected":    false,
			"configured":   false,
			"albums_count": 0,
		})
		return
	}

	status, err := ctx.GooglePhotosClient.GetConnectionStatus()
	if err != nil {
		log.Printf("[GooglePhotos] Failed to get connection status: %v", err)
		JSONResponse(w, map[string]interface{}{
			"connected":    false,
			"configured":   ctx.GooglePhotosClient.IsAuthenticated(),
			"albums_count": 0,
			"error":        err.Error(),
		})
		return
	}

	JSONResponse(w, map[string]interface{}{
		"connected":    status.Connected,
		"configured":   ctx.GooglePhotosClient.IsAuthenticated(),
		"albums_count": status.AlbumsCount,
		"email":        status.Email,
	})
}

// HandleGooglePhotosAuthStart initiates the OAuth flow
func (ctx *Context) HandleGooglePhotosAuthStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx.EnsureGooglePhotosClient()

	clientID := ctx.AppSettings.GetGooglePhotosClientID()
	if clientID == "" {
		http.Error(w, "Google Photos OAuth client ID not configured", http.StatusPreconditionRequired)
		return
	}

	// Get redirect URI from request body, or construct from Host header
	var reqBody struct {
		RedirectURI string `json:"redirect_uri"`
	}
	if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
		// If no body, try to construct from request
		reqBody.RedirectURI = ""
	}

	redirectURI := reqBody.RedirectURI
	if redirectURI == "" {
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		redirectURI = fmt.Sprintf("%s://%s/api/googlephotos/auth/callback", scheme, r.Host)
	}

	if ctx.GooglePhotosStateStore == nil {
		http.Error(w, "OAuth state store not initialized", http.StatusInternalServerError)
		return
	}

	_, authURL, err := ctx.GooglePhotosStateStore.StartAuth(clientID, redirectURI)
	if err != nil {
		log.Printf("[GooglePhotos] Failed to start OAuth: %v", err)
		http.Error(w, fmt.Sprintf("failed to start OAuth: %v", err), http.StatusInternalServerError)
		return
	}

	JSONResponse(w, map[string]interface{}{
		"auth_url":     authURL,
		"redirect_uri": redirectURI,
	})
}

// HandleGooglePhotosAuthCallback handles the OAuth callback
func (ctx *Context) HandleGooglePhotosAuthCallback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")
	errorParam := r.URL.Query().Get("error")

	if errorParam != "" {
		log.Printf("[GooglePhotos] OAuth error from provider: %s", errorParam)
		http.Error(w, fmt.Sprintf("OAuth error: %s", errorParam), http.StatusBadRequest)
		return
	}

	if code == "" {
		http.Error(w, "missing authorization code", http.StatusBadRequest)
		return
	}

	if ctx.GooglePhotosStateStore == nil {
		http.Error(w, "OAuth state store not initialized", http.StatusInternalServerError)
		return
	}

	authState, ok := ctx.GooglePhotosStateStore.ValidateState(state)
	if !ok {
		http.Error(w, "invalid or expired state", http.StatusBadRequest)
		return
	}

	ctx.EnsureGooglePhotosClient()
	if ctx.GooglePhotosClient == nil {
		http.Error(w, "Google Photos client not initialized", http.StatusInternalServerError)
		return
	}

	_, err := ctx.GooglePhotosClient.ExchangeCode(code, authState.RedirectURI, authState.CodeVerifier)
	if err != nil {
		log.Printf("[GooglePhotos] Failed to exchange code: %v", err)
		http.Error(w, fmt.Sprintf("failed to exchange code: %v", err), http.StatusInternalServerError)
		return
	}

	log.Println("[GooglePhotos] OAuth connection established successfully")

	// Return a simple HTML page that closes the popup and notifies the parent
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, `<!DOCTYPE html>
<html>
<head><title>Google Photos Connected</title></head>
<body>
<script>
if (window.opener) {
  window.opener.postMessage({ type: 'google-photos-connected', success: true }, '*');
  window.close();
} else {
  document.body.innerHTML = '<h1>Google Photos Connected</h1><p>You can close this window and return to the app.</p>';
}
</script>
</body>
</html>`)
}

// HandleGooglePhotosAuthDisconnect removes the stored OAuth tokens
func (ctx *Context) HandleGooglePhotosAuthDisconnect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx.EnsureGooglePhotosClient()
	if ctx.GooglePhotosClient == nil {
		JSONResponse(w, map[string]interface{}{"disconnected": true})
		return
	}

	if err := ctx.GooglePhotosClient.Disconnect(); err != nil {
		log.Printf("[GooglePhotos] Failed to disconnect: %v", err)
		http.Error(w, fmt.Sprintf("failed to disconnect: %v", err), http.StatusInternalServerError)
		return
	}

	log.Println("[GooglePhotos] OAuth disconnected")
	JSONResponse(w, map[string]interface{}{"disconnected": true})
}

// HandleGooglePhotosSync triggers a B2 to Google Photos sync
func (ctx *Context) HandleGooglePhotosSync(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodDelete {
		ctx.HandleGooglePhotosSyncCancel(w, r)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx.EnsureGooglePhotosClient()
	if ctx.GooglePhotosSyncMgr == nil {
		http.Error(w, "Google Photos sync manager not initialized", http.StatusServiceUnavailable)
		return
	}

	if ctx.GooglePhotosSyncMgr.IsRunning() {
		http.Error(w, "sync already in progress", http.StatusConflict)
		return
	}

	var reqBody struct {
		SkipDuplicates *bool `json:"skip_duplicates"`
	}
	_ = json.NewDecoder(r.Body).Decode(&reqBody)
	if reqBody.SkipDuplicates != nil {
		if configurable, ok := ctx.GooglePhotosSyncMgr.(googlePhotosSyncOptions); ok {
			configurable.SetSkipDuplicates(*reqBody.SkipDuplicates)
		}
	}

	// Start sync in background
	go func() {
		if err := ctx.GooglePhotosSyncMgr.Sync(context.Background()); err != nil {
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
	if ctx.GooglePhotosSyncMgr == nil {
		http.Error(w, "Google Photos sync manager not initialized", http.StatusServiceUnavailable)
		return
	}
	if !ctx.GooglePhotosSyncMgr.IsRunning() {
		JSONResponse(w, map[string]interface{}{"cancelled": false, "status": "idle"})
		return
	}
	ctx.GooglePhotosSyncMgr.Cancel()
	JSONResponse(w, map[string]interface{}{"cancelled": true, "status": "cancelling"})
}

// HandleGooglePhotosSyncProgress returns the current sync progress
func (ctx *Context) HandleGooglePhotosSyncProgress(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if ctx.GooglePhotosSyncMgr == nil {
		JSONResponse(w, map[string]interface{}{
			"status": "not_initialized",
		})
		return
	}

	progress := ctx.GooglePhotosSyncMgr.Progress()
	JSONResponse(w, progress)
}

// HandleGooglePhotosSyncHistoryExport returns compact Google Photos sync summaries.
func (ctx *Context) HandleGooglePhotosSyncHistoryExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if ctx.GooglePhotosSyncMgr == nil {
		http.Error(w, "Google Photos sync manager not initialized", http.StatusServiceUnavailable)
		return
	}
	progress := ctx.GooglePhotosSyncMgr.Progress()
	w.Header().Set("Content-Disposition", `attachment; filename="google-photos-sync-history.json"`)
	JSONResponse(w, map[string]interface{}{"history": progress.History, "last_successful_sync": progress.LastSuccessfulSync})
}

// HandleGooglePhotosAlbums lists or creates Google Photos albums
func (ctx *Context) HandleGooglePhotosAlbums(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		ctx.listGooglePhotosAlbums(w, r)
	case http.MethodPost:
		ctx.createGooglePhotosAlbum(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (ctx *Context) listGooglePhotosAlbums(w http.ResponseWriter, r *http.Request) {
	ctx.EnsureGooglePhotosClient()
	if ctx.GooglePhotosClient == nil {
		http.Error(w, "Google Photos client not initialized", http.StatusServiceUnavailable)
		return
	}

	albums, err := ctx.GooglePhotosClient.ListAlbums()
	if err != nil {
		log.Printf("[GooglePhotos] Failed to list albums: %v", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"error": fmt.Sprintf("failed to list albums: %v", err),
		})
		return
	}

	JSONResponse(w, map[string]interface{}{
		"albums": albums,
		"count":  len(albums),
	})
}

func (ctx *Context) createGooglePhotosAlbum(w http.ResponseWriter, r *http.Request) {
	ctx.EnsureGooglePhotosClient()
	if ctx.GooglePhotosClient == nil {
		http.Error(w, "Google Photos client not initialized", http.StatusServiceUnavailable)
		return
	}

	var reqBody struct {
		Title string `json:"title"`
	}
	if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if strings.TrimSpace(reqBody.Title) == "" {
		http.Error(w, "album title is required", http.StatusBadRequest)
		return
	}

	album, err := ctx.GooglePhotosClient.CreateAlbum(reqBody.Title)
	if err != nil {
		log.Printf("[GooglePhotos] Failed to create album: %v", err)
		http.Error(w, fmt.Sprintf("failed to create album: %v", err), http.StatusInternalServerError)
		return
	}

	JSONResponse(w, album)
}
