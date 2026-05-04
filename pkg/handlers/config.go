package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/denysvitali/pictures-sync-s3/pkg/auth"
	"github.com/denysvitali/pictures-sync-s3/pkg/settings"
	"github.com/denysvitali/pictures-sync-s3/pkg/state"
	"github.com/denysvitali/pictures-sync-s3/pkg/validation"
)

var tailscaleCommandContext = exec.CommandContext

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
		JSONResponse(w, map[string]any{
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
			response := map[string]any{
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
		response := map[string]any{
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
		JSONResponse(w, map[string]any{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	JSONResponse(w, map[string]any{"success": true})
}

func (ctx *Context) HandlePasswordChange(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if ctx.PasswordMgr == nil {
		http.Error(w, "Password manager not initialized", http.StatusServiceUnavailable)
		return
	}

	var req struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if err := ctx.PasswordMgr.ChangePassword(req.CurrentPassword, req.NewPassword); err != nil {
		if errors.Is(err, auth.ErrCurrentPasswordInvalid) {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}
		status := http.StatusBadRequest
		if strings.Contains(err.Error(), "password file") || strings.Contains(err.Error(), "password directory") {
			status = http.StatusInternalServerError
		}
		http.Error(w, err.Error(), status)
		return
	}

	logConfigChange(r, "password_changed", "Updated gokrazy UI password")
	JSONResponse(w, map[string]any{"status": "ok"})
}

// logConfigChange logs rclone configuration changes with client information
// HandleConfigB2 handles Backblaze B2 remote configuration
func (ctx *Context) HandleConfigB2(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Account    string `json:"account_id"`
		Key        string `json:"application_key"`
		Bucket     string `json:"bucket_name"`
		RemoteName string `json:"remote_name"`
		RemotePath string `json:"remote_path"`
		Endpoint   string `json:"endpoint"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	b2cfg := &validation.B2Config{
		Account:    req.Account,
		Key:        req.Key,
		Bucket:     req.Bucket,
		RemoteName: req.RemoteName,
		RemotePath: req.RemotePath,
		Endpoint:   req.Endpoint,
	}

	configBytes, err := validation.BuildB2RcloneConfig(b2cfg)
	if err != nil {
		logConfigChange(r, "b2_validation_failed", err.Error())
		JSONResponse(w, map[string]any{"success": false, "error": err.Error()})
		return
	}

	// Validate the generated config
	result, err := validation.ValidateRcloneConfig(configBytes)
	if err != nil || !result.Valid {
		errMsg := "Invalid generated configuration"
		if err != nil {
			errMsg = err.Error()
		} else if len(result.Errors) > 0 {
			errMsg = result.Errors[0].Error()
		}
		logConfigChange(r, "b2_validation_failed", errMsg)
		JSONResponse(w, map[string]any{"success": false, "error": errMsg})
		return
	}

	// Write config file
	configPath := state.GetRcloneConfigPath()
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		logConfigChange(r, "b2_write_error", err.Error())
		http.Error(w, fmt.Sprintf("Failed to create config directory: %v", err), http.StatusInternalServerError)
		return
	}
	if err := os.WriteFile(configPath, configBytes, 0600); err != nil {
		logConfigChange(r, "b2_write_error", err.Error())
		http.Error(w, fmt.Sprintf("Failed to write config: %v", err), http.StatusInternalServerError)
		return
	}

	remoteName := strings.TrimSpace(req.RemoteName)
	if remoteName == "" {
		remoteName = "b2"
	}
	remotePath := strings.TrimSpace(req.RemotePath)
	if remotePath == "" {
		remotePath = "/photos"
	}

	// Update settings
	if err := ctx.AppSettings.SetRemote(remoteName, remotePath); err != nil {
		logConfigChange(r, "b2_settings_error", err.Error())
		http.Error(w, fmt.Sprintf("Failed to save settings: %v", err), http.StatusInternalServerError)
		return
	}
	ctx.SyncMgr.SetRemote(remoteName, remotePath)

	// Test connection
	if err := ctx.SyncMgr.TestConnection(); err != nil {
		logConfigChange(r, "b2_test_failed", err.Error())
		JSONResponse(w, map[string]any{
			"success":     true,
			"remote_name": remoteName,
			"warning":     fmt.Sprintf("Config saved but connection test failed: %v", err),
		})
		return
	}

	logConfigChange(r, "b2_success", fmt.Sprintf("Configured B2 remote '%s' with bucket '%s'", remoteName, req.Bucket))
	JSONResponse(w, map[string]any{
		"success":     true,
		"remote_name": remoteName,
		"remotes":     []string{remoteName},
	})
}

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
		response := ctx.AppSettings.ToJSON()
		tailscaleConfigured, err := settings.HasTailscaleAuthKey()
		if err != nil {
			log.Printf("Failed to read Tailscale auth key status: %v", err)
		}
		response["tailscale_auth_key_configured"] = tailscaleConfigured
		response["tailscale_auth_key_path"] = settings.TailscaleAuthKeyFile
		JSONResponse(w, response)

	case http.MethodPost:
		var req struct {
			RemoteName             string  `json:"remote_name"`
			RemotePath             string  `json:"remote_path"`
			ReformatThreshold      float64 `json:"reformat_threshold"`
			Transfers              int     `json:"transfers"`
			Checkers               int     `json:"checkers"`
			GooglePhotosEnabled    bool    `json:"google_photos_enabled"`
			GooglePhotosRemoteName string  `json:"google_photos_remote_name"`
			TailscaleAuthKey       string  `json:"tailscale_auth_key"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		if req.TailscaleAuthKey != "" {
			if err := settings.ValidateTailscaleAuthKey(req.TailscaleAuthKey); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
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

		if req.TailscaleAuthKey != "" {
			if err := configureTailscale(req.TailscaleAuthKey); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			log.Println("Tailscale configured")
		}

		log.Println("Settings updated")
		JSONResponse(w, map[string]any{"status": "ok"})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func configureTailscale(authKey string) error {
	if err := settings.ValidateTailscaleAuthKey(authKey); err != nil {
		return err
	}
	if err := settings.SaveTailscaleAuthKey(authKey); err != nil {
		return fmt.Errorf("failed to store tailscale auth key: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	args := []string{
		"up",
		"--auth-key=" + authKey,
		"--ssh",
	}
	if hostname, err := os.Hostname(); err == nil && strings.TrimSpace(hostname) != "" {
		args = append(args, "--hostname="+strings.TrimSpace(hostname))
	}

	output, err := tailscaleCommandContext(ctx, "tailscale", args...).CombinedOutput()
	if err != nil {
		details := strings.ReplaceAll(strings.TrimSpace(string(output)), authKey, "[redacted]")
		if details == "" {
			details = err.Error()
		}
		return fmt.Errorf("failed to configure tailscale: %s", details)
	}

	return nil
}
