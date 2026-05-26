package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/denysvitali/pictures-sync-s3/pkg/googlephotos"
	"github.com/denysvitali/pictures-sync-s3/pkg/state"
	"github.com/denysvitali/pictures-sync-s3/pkg/utils"
	"github.com/denysvitali/pictures-sync-s3/pkg/validation"
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

// HandleGooglePhotosAuthStart initiates the OAuth PKCE flow for Google Photos.
func (ctx *Context) HandleGooglePhotosAuthStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
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

	JSONResponse(w, map[string]interface{}{
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
		log.Printf("[GooglePhotos] Token exchange failed: %v", err)
		http.Error(w, fmt.Sprintf("Token exchange failed: %v", err), http.StatusBadRequest)
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

	JSONResponse(w, map[string]interface{}{"disconnected": true})
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
		"status":            progress.Status,
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

// HandleGooglePhotosAlbums returns 404 — album management via native API removed.
// Albums are created automatically during sync via rclone.
func (ctx *Context) HandleGooglePhotosAlbums(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "Album management is not available with rclone-based Google Photos sync. Albums are created automatically during sync.", http.StatusNotFound)
}
