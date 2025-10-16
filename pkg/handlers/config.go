package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"

	"github.com/denysvitali/pictures-sync-s3/pkg/state"
)

// HandleConfig handles rclone configuration
func (ctx *Context) HandleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		// Return current config status (but NOT the content with credentials)
		hasConfig, err := state.EnsureRcloneConfig()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		remotes, _ := ctx.SyncMgr.ListRemotes()
		JSONResponse(w, map[string]interface{}{
			"configured": hasConfig,
			"remotes":    remotes,
			// SECURITY: Never return config content - it contains cloud credentials
			// Users can view/edit config via rclone config commands on the device
		})

	case http.MethodPost:
		// Update rclone config
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "Failed to read body", http.StatusBadRequest)
			return
		}

		// Write config file
		if err := os.WriteFile(state.GetRcloneConfigPath(), body, 0600); err != nil {
			http.Error(w, fmt.Sprintf("Failed to write config: %v", err), http.StatusInternalServerError)
			return
		}

		log.Println("Rclone configuration updated")
		JSONResponse(w, map[string]string{"status": "ok"})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// HandleConfigTest tests rclone connection
func (ctx *Context) HandleConfigTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := ctx.SyncMgr.TestConnection(); err != nil {
		JSONResponse(w, map[string]interface{}{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	JSONResponse(w, map[string]bool{"success": true})
}

// HandleSettings manages application settings
func (ctx *Context) HandleSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		JSONResponse(w, ctx.AppSettings.ToJSON())

	case http.MethodPost:
		var req struct {
			RemoteName             string  `json:"remote_name"`
			RemotePath             string  `json:"remote_path"`
			ReformatThreshold      float64 `json:"reformat_threshold"`
			Transfers              int     `json:"transfers"`
			Checkers               int     `json:"checkers"`
			GooglePhotosEnabled    bool    `json:"google_photos_enabled"`
			GooglePhotosRemoteName string  `json:"google_photos_remote_name"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		// Update settings
		if req.RemoteName != "" || req.RemotePath != "" {
			if err := ctx.AppSettings.SetRemote(req.RemoteName, req.RemotePath); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			// Update sync manager
			ctx.SyncMgr.SetRemote(req.RemoteName, req.RemotePath)
		}

		if req.ReformatThreshold > 0 {
			if err := ctx.AppSettings.SetReformatThreshold(req.ReformatThreshold); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}

		if req.Transfers > 0 {
			if err := ctx.AppSettings.SetTransfers(req.Transfers); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}

		if req.Checkers > 0 {
			if err := ctx.AppSettings.SetCheckers(req.Checkers); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}

		// Update Google Photos settings
		if err := ctx.AppSettings.SetGooglePhotos(req.GooglePhotosEnabled, req.GooglePhotosRemoteName); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		// Update sync manager with Google Photos settings
		ctx.SyncMgr.SetGooglePhotos(req.GooglePhotosEnabled, req.GooglePhotosRemoteName)

		log.Println("Settings updated")
		JSONResponse(w, map[string]string{"status": "ok"})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}
