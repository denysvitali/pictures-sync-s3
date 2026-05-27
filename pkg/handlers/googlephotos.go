package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/denysvitali/pictures-sync-s3/pkg/googlephotos"
	"github.com/denysvitali/pictures-sync-s3/pkg/httputil"
	"github.com/denysvitali/pictures-sync-s3/pkg/state"
	"github.com/denysvitali/pictures-sync-s3/pkg/syncmanager"
	"github.com/denysvitali/pictures-sync-s3/pkg/utils"
	"github.com/denysvitali/pictures-sync-s3/pkg/validation"
	"golang.org/x/time/rate"
)

// oauthStartRateLimiters tracks per-IP rate limits for the OAuth start endpoint.
// Allows up to 5 auth-start attempts per minute with a burst of 3.
var (
	oauthStartLimiters     = make(map[string]*oauthRateLimitEntry)
	oauthStartLimitersMu   sync.Mutex
	oauthStartIdleTimeout  = 15 * time.Minute
)

type oauthRateLimitEntry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// allowOAuthStart returns true when the given IP is within rate limits for
// starting a Google Photos OAuth flow (5 attempts/min, burst 3).
func allowOAuthStart(ip string) bool {
	now := time.Now()
	oauthStartLimitersMu.Lock()
	defer oauthStartLimitersMu.Unlock()

	// Evict idle entries opportunistically.
	for k, e := range oauthStartLimiters {
		if now.Sub(e.lastSeen) > oauthStartIdleTimeout {
			delete(oauthStartLimiters, k)
		}
	}

	entry, ok := oauthStartLimiters[ip]
	if !ok {
		entry = &oauthRateLimitEntry{
			limiter:  rate.NewLimiter(rate.Every(12*time.Second), 3), // 5/min, burst 3
			lastSeen: now,
		}
		oauthStartLimiters[ip] = entry
	} else {
		entry.lastSeen = now
	}
	return entry.limiter.Allow()
}

// oauthErrorType extracts a safe, non-sensitive error category string from an
// OAuth error. Raw error messages are suppressed because network-level errors
// may reflect the outgoing request body (which contains client_secret).
func oauthErrorType(err error) string {
	if err == nil {
		return "none"
	}
	// Categorise by well-known sentinel types without exposing message text.
	var netErr net.Error
	if errors.As(err, &netErr) {
		if netErr.Timeout() {
			return "network_timeout"
		}
		return "network_error"
	}
	// Generic fallback: just the Go type name, not the message.
	return fmt.Sprintf("%T", errors.Unwrap(err))
}

// HandleGooglePhotosStatus returns whether Google Photos is configured via
// rclone. This reads rclone.conf directly instead of going through rclone's
// global config state, so it stays responsive while a long sync is running.
func (ctx *Context) HandleGooglePhotosStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	configured := false
	gpRemoteName := ""
	if ctx.AppSettings != nil {
		configured = ctx.AppSettings.GetGooglePhotosEnabled()
		gpRemoteName = ctx.AppSettings.GetGooglePhotosRemoteName()
	}

	connected := false
	if gpRemoteName != "" {
		var err error
		connected, err = rcloneConfigHasSection(gpRemoteName)
		if err != nil {
			log.Printf("[GooglePhotos] Failed to read rclone config: %v", err)
		}
	}

	httputil.JSON(w, http.StatusOK, map[string]any{
		"configured": configured && gpRemoteName != "",
		"connected":  connected,
	})
}

func rcloneConfigHasSection(sectionName string) (bool, error) {
	sectionName = strings.TrimSpace(sectionName)
	if sectionName == "" {
		return false, nil
	}

	data, err := os.ReadFile(state.GetRcloneConfigPath())
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}

	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			name := strings.TrimSpace(trimmed[1 : len(trimmed)-1])
			if strings.EqualFold(name, sectionName) {
				return true, nil
			}
		}
	}
	return false, nil
}

// HandleGooglePhotosAuthStart initiates the OAuth PKCE flow for Google Photos.
func (ctx *Context) HandleGooglePhotosAuthStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Rate-limit OAuth start per client IP (5/min) to prevent abuse.
	clientIP := r.RemoteAddr
	if host, _, err := net.SplitHostPort(clientIP); err == nil {
		clientIP = host
	}
	if !allowOAuthStart(clientIP) {
		log.Printf("[GooglePhotos] OAuth start rate limit exceeded for IP %s", clientIP)
		http.Error(w, "Too many requests. Please try again later.", http.StatusTooManyRequests)
		return
	}

	if ctx.GooglePhotosStateStore == nil {
		http.Error(w, "Google Photos OAuth state store not initialized", http.StatusServiceUnavailable)
		return
	}

	clientID := ""
	if ctx.AppSettings != nil {
		clientID = ctx.AppSettings.GetGooglePhotosClientID()
	}
	if clientID == "" {
		http.Error(w, "Google Photos client ID not configured. Set it in Settings first.", http.StatusPreconditionFailed)
		return
	}

	var reqBody struct {
		RedirectURI string `json:"redirect_uri"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16*1024)
	if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
		http.Error(w, "Invalid JSON body", http.StatusBadRequest)
		return
	}

	authState, authURL, err := ctx.GooglePhotosStateStore.StartAuth(clientID, reqBody.RedirectURI)
	if err != nil {
		log.Printf("[GooglePhotos] Failed to start auth: %v", err)
		http.Error(w, "Failed to start OAuth flow", http.StatusInternalServerError)
		return
	}

	httputil.JSON(w, http.StatusOK, map[string]any{
		"auth_url": authURL,
		"state":    authState.State,
	})
}

// HandleGooglePhotosAuthCallback handles the OAuth callback, exchanges the code
// for a token, and writes the rclone googlephotos remote config.
func (ctx *Context) HandleGooglePhotosAuthCallback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")
	if code == "" || state == "" {
		http.Error(w, "Missing code or state parameter", http.StatusBadRequest)
		return
	}

	authState, ok := ctx.GooglePhotosStateStore.ValidateState(state)
	if !ok {
		http.Error(w, "Invalid or expired state", http.StatusBadRequest)
		return
	}

	clientID := ctx.AppSettings.GetGooglePhotosClientID()
	clientSecret := ctx.AppSettings.GetGooglePhotosClientSecret()
	if clientID == "" || clientSecret == "" {
		http.Error(w, "Google Photos credentials not configured", http.StatusPreconditionFailed)
		return
	}

	tokenStore := googlephotos.NewTokenStore("")
	client := googlephotos.NewClient(clientID, clientSecret, tokenStore)
	token, err := client.ExchangeCode(code, authState.RedirectURI, authState.CodeVerifier)
	if err != nil {
		// Log only error category — not the full message, which may reflect the
		// outgoing POST body (containing client_secret) via network error strings.
		log.Printf("[GooglePhotos] Token exchange failed: error_type=%s", oauthErrorType(err))
		http.Error(w, "Token exchange failed", http.StatusBadRequest)
		return
	}

	// Write the rclone config with the googlephotos remote.
	remoteName := ctx.AppSettings.GetGooglePhotosRemoteName()
	if remoteName == "" {
		remoteName = "gphotos"
	}

	rcloneToken := &validation.GooglePhotosToken{
		AccessToken:  token.AccessToken,
		TokenType:    token.TokenType,
		RefreshToken: token.RefreshToken,
		Expiry:       token.Expiry,
	}

	configBytes, err := validation.BuildGooglePhotosRcloneConfig(remoteName, clientID, clientSecret, rcloneToken)
	if err != nil {
		log.Printf("[GooglePhotos] Failed to build rclone config: %v", err)
		http.Error(w, "Failed to build rclone config", http.StatusInternalServerError)
		return
	}

	if err := updateRcloneConfigWithRemote(remoteName, configBytes); err != nil {
		log.Printf("[GooglePhotos] Failed to update rclone config: %v", err)
		http.Error(w, "Failed to update rclone config", http.StatusInternalServerError)
		return
	}

	// Enable Google Photos sync in settings.
	if err := ctx.AppSettings.SetGooglePhotos(true, remoteName); err != nil {
		log.Printf("[GooglePhotos] Failed to save settings: %v", err)
	}
	ctx.SyncMgr.SetGooglePhotos(true, remoteName)

	// Return a simple HTML page that notifies the opener window.
	w.Header().Set("Content-Type", "text/html")
	fmt.Fprint(w, `<!DOCTYPE html>
<html>
<head><title>Google Photos Connected</title></head>
<body>
<script>
  if (window.opener) {
    window.opener.postMessage({type: 'google-photos-connected'}, '*');
  }
  window.close();
</script>
<p>Google Photos connected successfully. You can close this window.</p>
</body>
</html>`)
}

// HandleGooglePhotosAuthDisconnect removes the stored token and googlephotos
// remote from rclone config.
func (ctx *Context) HandleGooglePhotosAuthDisconnect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Remove token file.
	tokenStore := googlephotos.NewTokenStore("")
	_ = tokenStore.Delete()

	// Remove googlephotos remote from rclone config.
	remoteName := ctx.AppSettings.GetGooglePhotosRemoteName()
	if remoteName == "" {
		remoteName = "gphotos"
	}
	if err := removeRemoteFromRcloneConfig(remoteName); err != nil {
		log.Printf("[GooglePhotos] Failed to remove remote from config: %v", err)
	}

	// Disable in settings.
	if err := ctx.AppSettings.SetGooglePhotos(false, ""); err != nil {
		log.Printf("[GooglePhotos] Failed to update settings: %v", err)
	}
	ctx.SyncMgr.SetGooglePhotos(false, "")

	httputil.JSON(w, http.StatusOK, map[string]any{"disconnected": true})
}

// updateRcloneConfigWithRemote adds or replaces a remote section in rclone.conf.
func updateRcloneConfigWithRemote(remoteName string, remoteConfig []byte) error {
	configPath := state.GetRcloneConfigPath()

	var existing []byte
	if data, err := os.ReadFile(configPath); err == nil {
		existing = data
	}

	updated := replaceSectionInRcloneConfig(existing, remoteName, remoteConfig)

	// Validate before writing.
	result, err := validation.ValidateRcloneConfig(updated)
	if err != nil || !result.Valid {
		return fmt.Errorf("generated config failed validation: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(configPath), 0750); err != nil {
		return fmt.Errorf("failed to create config dir: %w", err)
	}
	return utils.AtomicWrite(configPath, updated, 0600)
}

// removeRemoteFromRcloneConfig removes a remote section from rclone.conf.
func removeRemoteFromRcloneConfig(remoteName string) error {
	configPath := state.GetRcloneConfigPath()
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	updated := removeSectionFromRcloneConfig(data, remoteName)
	if len(bytes.TrimSpace(updated)) == 0 {
		// Config is empty, remove it.
		return os.Remove(configPath)
	}

	return utils.AtomicWrite(configPath, updated, 0600)
}

// replaceSectionInRcloneConfig removes the existing section with the given name
// and appends the new section bytes.
func replaceSectionInRcloneConfig(existing []byte, sectionName string, newSection []byte) []byte {
	withoutOld := removeSectionFromRcloneConfig(existing, sectionName)
	withoutOld = bytes.TrimSpace(withoutOld)
	if len(withoutOld) > 0 {
		withoutOld = append(withoutOld, '\n', '\n')
	}
	return append(withoutOld, newSection...)
}

// removeSectionFromRcloneConfig removes a section (and its keys) from rclone config bytes.
func removeSectionFromRcloneConfig(data []byte, sectionName string) []byte {
	lines := strings.Split(string(data), "\n")
	var out []string
	inTargetSection := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			name := strings.TrimSpace(trimmed[1 : len(trimmed)-1])
			inTargetSection = name == sectionName
		}
		if !inTargetSection {
			out = append(out, line)
		}
	}

	return []byte(strings.Join(out, "\n"))
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

	go func() {
		if err := ctx.SyncMgr.SyncCardsToGooglePhotos(context.Background()); err != nil {
			log.Printf("[GooglePhotos] Sync error: %v", err)
		}
	}()

	httputil.JSON(w, http.StatusOK, map[string]any{
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
		httputil.JSON(w, http.StatusOK, map[string]any{"cancelled": false, "status": "idle"})
		return
	}
	if err := ctx.SyncMgr.CancelGooglePhotos(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"cancelled": true, "status": "cancelling"})
}

// HandleGooglePhotosSyncProgress returns the current Google Photos sync progress.
func (ctx *Context) HandleGooglePhotosSyncProgress(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if ctx.SyncMgr == nil {
		httputil.JSON(w, http.StatusOK, map[string]any{
			"status": "not_initialized",
		})
		return
	}

	progress := ctx.SyncMgr.GetGooglePhotosProgress()
	status := progress.Status
	if status == "" && ctx.SyncMgr.IsGooglePhotosRunning() {
		status = "syncing"
	}
	httputil.JSON(w, http.StatusOK, map[string]any{
		"status":            status,
		"current_file":      progress.CurrentFile,
		"current_file_size": progress.CurrentFileSize,
		"transferred_files": progress.TransferredFiles,
		"total_files":       progress.TotalFiles,
		"bytes_transferred": progress.BytesTransferred,
		"percentage":        progress.Percentage,
		"speed":             progress.Speed,
		"eta":               progress.ETA,
		"error":             progress.Error,
	})
}

// HandleGooglePhotosSyncHistoryExport returns 404 — no longer supported.
func (ctx *Context) HandleGooglePhotosSyncHistoryExport(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "Not found", http.StatusNotFound)
}

// albumClearOps tracks in-progress album clear operations keyed by album ID.
var (
	albumClearOps   = make(map[string]*googlephotos.AlbumClearProgress)
	albumClearOpsMu sync.RWMutex
)

// HandleGooglePhotosAlbums handles album operations.
// DELETE /api/googlephotos/albums/{albumId} — removes all media items from an album.
// GET  /api/googlephotos/albums/{albumId}/clear/progress — get clear progress.
func (ctx *Context) HandleGooglePhotosAlbums(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodDelete {
		ctx.handleGooglePhotosAlbumClear(w, r)
		return
	}
	if r.Method == http.MethodGet {
		path := strings.Trim(r.URL.Path, "/")
		parts := strings.Split(path, "/")
		if len(parts) >= 6 && parts[4] == "clear" && parts[5] == "progress" {
			ctx.handleGooglePhotosAlbumClearProgress(w, r)
			return
		}
		ctx.handleGooglePhotosAlbumList(w, r)
		return
	}
	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}

func (ctx *Context) handleGooglePhotosAlbumList(w http.ResponseWriter, r *http.Request) {
	clientID := ""
	clientSecret := ""
	if ctx.AppSettings != nil {
		clientID = ctx.AppSettings.GetGooglePhotosClientID()
		clientSecret = ctx.AppSettings.GetGooglePhotosClientSecret()
	}
	if clientID == "" || clientSecret == "" {
		http.Error(w, "Google Photos credentials not configured", http.StatusPreconditionFailed)
		return
	}

	tokenStore := googlephotos.NewTokenStore("")
	client := googlephotos.NewClient(clientID, clientSecret, tokenStore)
	if !client.IsAuthenticated() {
		httputil.JSON(w, http.StatusOK, map[string]any{"albums": []any{}})
		return
	}

	albums, err := client.ListAlbumsContext(r.Context())
	if err != nil {
		log.Printf("[GooglePhotos] Failed to list albums: %v", err)
		http.Error(w, "Failed to list albums", http.StatusInternalServerError)
		return
	}

	// Only return app-managed albums (card-00000).
	var managed []*googlephotos.Album
	for _, a := range albums {
		if strings.HasPrefix(a.Title, "card-") {
			managed = append(managed, a)
		}
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"albums": managed})
}

func (ctx *Context) handleGooglePhotosAlbumClear(w http.ResponseWriter, r *http.Request) {
	// Extract album ID from path: /api/googlephotos/albums/{albumId}
	path := strings.Trim(r.URL.Path, "/")
	parts := strings.Split(path, "/")
	if len(parts) < 4 {
		http.Error(w, "Missing album ID", http.StatusBadRequest)
		return
	}
	albumID := parts[3]
	if albumID == "" {
		http.Error(w, "Missing album ID", http.StatusBadRequest)
		return
	}

	clientID := ""
	clientSecret := ""
	if ctx.AppSettings != nil {
		clientID = ctx.AppSettings.GetGooglePhotosClientID()
		clientSecret = ctx.AppSettings.GetGooglePhotosClientSecret()
	}
	if clientID == "" || clientSecret == "" {
		http.Error(w, "Google Photos credentials not configured", http.StatusPreconditionFailed)
		return
	}

	tokenStore := googlephotos.NewTokenStore("")
	client := googlephotos.NewClient(clientID, clientSecret, tokenStore)
	if !client.IsAuthenticated() {
		http.Error(w, "Not authenticated with Google Photos", http.StatusUnauthorized)
		return
	}

	// Reject if a clear is already running for this album.
	albumClearOpsMu.RLock()
	existing, busy := albumClearOps[albumID]
	albumClearOpsMu.RUnlock()
	if busy && existing.Status == "clearing" {
		http.Error(w, "Album clear already in progress", http.StatusConflict)
		return
	}

	// Start the clear operation in the background so the HTTP response
	// returns immediately and the UI can poll for progress.
	go ctx.runAlbumClear(client, albumID)

	httputil.JSON(w, http.StatusOK, map[string]any{"started": true, "album_id": albumID})
}

func (ctx *Context) runAlbumClear(client *googlephotos.Client, albumID string) {
	// Use a detached context with a generous timeout so album clearing
	// survives any HTTP request timeout for large albums.
	apiCtx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	// Initialize progress.
	albumClearOpsMu.Lock()
	albumClearOps[albumID] = &googlephotos.AlbumClearProgress{
		AlbumID: albumID,
		Status:  "clearing",
	}
	albumClearOpsMu.Unlock()

	// List all media items in the album.
	items, err := client.ListAlbumMediaItems(apiCtx, albumID)
	if err != nil {
		log.Printf("[GooglePhotos] Failed to list album items: error_type=%T", err)
		albumClearOpsMu.Lock()
		albumClearOps[albumID] = &googlephotos.AlbumClearProgress{
			AlbumID: albumID,
			Status:  "error",
			Error:   "Failed to list album items",
		}
		albumClearOpsMu.Unlock()
		return
	}

	if len(items) == 0 {
		albumClearOpsMu.Lock()
		albumClearOps[albumID] = &googlephotos.AlbumClearProgress{
			AlbumID:      albumID,
			Status:       "completed",
			TotalItems:   0,
			RemovedItems: 0,
		}
		albumClearOpsMu.Unlock()
		return
	}

	ids := make([]string, 0, len(items))
	for _, item := range items {
		if item.ID != "" {
			ids = append(ids, item.ID)
		}
	}

	onProgress := func(removed, total int) {
		albumClearOpsMu.Lock()
		albumClearOps[albumID] = &googlephotos.AlbumClearProgress{
			AlbumID:      albumID,
			Status:       "clearing",
			TotalItems:   total,
			RemovedItems: removed,
		}
		albumClearOpsMu.Unlock()
	}

	if err := client.BatchRemoveMediaItemsWithProgress(apiCtx, albumID, ids, onProgress); err != nil {
		log.Printf("[GooglePhotos] Failed to remove items from album: error_type=%T", err)
		albumClearOpsMu.Lock()
		albumClearOps[albumID] = &googlephotos.AlbumClearProgress{
			AlbumID:      albumID,
			Status:       "error",
			TotalItems:   len(ids),
			RemovedItems: albumClearOps[albumID].RemovedItems,
			Error:        "Failed to remove items from album",
		}
		albumClearOpsMu.Unlock()
		return
	}

	// Clear local upload state so the next sync can re-upload these files.
	albums, _ := client.ListAlbumsContext(apiCtx)
	var albumName string
	for _, a := range albums {
		if a.ID == albumID {
			albumName = a.Title
			break
		}
	}
	if albumName != "" {
		if err := syncmanager.ClearGooglePhotosAlbumState(albumName); err != nil {
			log.Printf("[GooglePhotos] Failed to clear local state for album %s: %v", albumName, err)
		}
	}

	log.Printf("[GooglePhotos] Cleared %d item(s) from album %s", len(ids), albumID)
	albumClearOpsMu.Lock()
	albumClearOps[albumID] = &googlephotos.AlbumClearProgress{
		AlbumID:      albumID,
		Status:       "completed",
		TotalItems:   len(ids),
		RemovedItems: len(ids),
	}
	albumClearOpsMu.Unlock()
}

func (ctx *Context) handleGooglePhotosAlbumClearProgress(w http.ResponseWriter, r *http.Request) {
	// Extract album ID from path: /api/googlephotos/albums/{albumId}/clear/progress
	path := strings.Trim(r.URL.Path, "/")
	parts := strings.Split(path, "/")
	if len(parts) < 4 {
		http.Error(w, "Missing album ID", http.StatusBadRequest)
		return
	}
	albumID := parts[3]
	if albumID == "" {
		http.Error(w, "Missing album ID", http.StatusBadRequest)
		return
	}

	albumClearOpsMu.RLock()
	progress, ok := albumClearOps[albumID]
	albumClearOpsMu.RUnlock()

	if !ok {
		httputil.JSON(w, http.StatusOK, map[string]any{
			"album_id": albumID,
			"status":   "idle",
		})
		return
	}

	httputil.JSON(w, http.StatusOK, map[string]any{
		"album_id":      progress.AlbumID,
		"status":        progress.Status,
		"total_items":   progress.TotalItems,
		"removed_items": progress.RemovedItems,
		"error":         progress.Error,
	})
}
