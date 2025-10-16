package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"

	"github.com/denysvitali/pictures-sync-s3/pkg/state"
	"github.com/denysvitali/pictures-sync-s3/pkg/validation"
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
		// Update rclone config with comprehensive validation
		body, err := io.ReadAll(io.LimitReader(r.Body, validation.MaxConfigSize+1))
		if err != nil {
			logConfigChange(r, "read_error", err.Error())
			http.Error(w, "Failed to read body", http.StatusBadRequest)
			return
		}

		// Validate config format and content
		result, err := validation.ValidateRcloneConfig(body)
		if err != nil || !result.Valid {
			errMsg := "Invalid configuration"
			if err != nil {
				errMsg = err.Error()
			} else if len(result.Errors) > 0 {
				errMsg = result.Errors[0].Error()
			}

			logConfigChange(r, "validation_failed", errMsg)

			// Return detailed validation errors to help legitimate users
			response := map[string]interface{}{
				"status": "error",
				"error":  errMsg,
			}
			if len(result.Errors) > 1 {
				response["errors"] = formatErrors(result.Errors)
			}
			if len(result.Warnings) > 0 {
				response["warnings"] = result.Warnings
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(response)
			return
		}

		// Sanitize config content
		body = validation.SanitizeConfig(body)

		// Write config file atomically with restricted permissions
		if err := os.WriteFile(state.GetRcloneConfigPath(), body, 0600); err != nil {
			logConfigChange(r, "write_error", err.Error())
			http.Error(w, fmt.Sprintf("Failed to write config: %v", err), http.StatusInternalServerError)
			return
		}

		// Log successful update with details
		logConfigChange(r, "success", fmt.Sprintf("Updated config with %d remote(s): %v",
			len(result.Remotes), result.Remotes))

		// Include warnings in response if any
		response := map[string]interface{}{
			"status":  "ok",
			"remotes": result.Remotes,
		}
		if len(result.Warnings) > 0 {
			response["warnings"] = result.Warnings
			log.Printf("Config uploaded with warnings: %v", result.Warnings)
		}

		JSONResponse(w, response)

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

// logConfigChange logs rclone configuration changes with client information
func logConfigChange(r *http.Request, status, details string) {
	// Extract client IP, handling proxy headers
	clientIP := r.RemoteAddr
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		clientIP = forwarded
	} else if realIP := r.Header.Get("X-Real-IP"); realIP != "" {
		clientIP = realIP
	}

	// Log with timestamp, IP, and details
	log.Printf("[SECURITY] Rclone config change: status=%s, client=%s, user_agent=%s, details=%s",
		status, clientIP, r.UserAgent(), details)
}

// formatErrors converts error slice to string slice for JSON response
func formatErrors(errors []error) []string {
	result := make([]string, len(errors))
	for i, err := range errors {
		result[i] = err.Error()
	}
	return result
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
