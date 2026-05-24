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
	"github.com/denysvitali/pictures-sync-s3/pkg/utils"
	"github.com/denysvitali/pictures-sync-s3/pkg/validation"
)

// Per-handler JSON request body size limits. These guard against unbounded
// memory consumption from hostile or buggy clients posting huge JSON bodies
// to endpoints that previously decoded directly from r.Body without a limit.
const (
	maxPasswordChangeBodyBytes = 4 * 1024  // current+new passwords plus JSON overhead
	maxSettingsBodyBytes       = 64 * 1024 // settings payload incl. tailscale auth key
	maxB2ConfigBodyBytes       = 16 * 1024
	maxOTAInstallBodyBytes     = 4 * 1024
)

var (
	tailscaleAuthKeyPath    = settings.TailscaleAuthKeyFile
	tailscaleBinary         = "/user/tailscale"
	tailscaleCommandContext = exec.CommandContext
)

// HandleConfig handles rclone configuration
func (ctx *Context) HandleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		hasConfig, err := state.EnsureRcloneConfig()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		remotes, _ := ctx.SyncMgr.ListRemotes()
		response := map[string]any{
			"configured": hasConfig,
			"remotes":    remotes,
		}
		if ctx.AppSettings != nil {
			response["remote_name"] = ctx.AppSettings.GetRemoteName()
			response["remote_path"] = ctx.AppSettings.GetRemotePath()
		}
		if hasConfig {
			configBytes, err := os.ReadFile(state.GetRcloneConfigPath())
			if err != nil {
				http.Error(w, fmt.Sprintf("Failed to read config: %v", err), http.StatusInternalServerError)
				return
			}
			redactedConfig, provider := redactRcloneConfig(configBytes)
			response["config_redacted"] = redactedConfig
			if provider != "" {
				response["provider"] = provider
			}
		}
		JSONResponse(w, response)

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
		configPath := state.GetRcloneConfigPath()
		if err := os.MkdirAll(filepath.Dir(configPath), 0750); err != nil {
			logConfigChange(r, "write_error", err.Error())
			http.Error(w, fmt.Sprintf("Failed to create config directory: %v", err), http.StatusInternalServerError)
			return
		}
		if err := utils.AtomicWrite(configPath, body, 0600); err != nil {
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

// safeRcloneConfigKeys is a strict allowlist of rclone config keys whose values
// are safe to display in the redacted config view. Anything not on this list
// is redacted by default. Adding a new backend requires explicit review here,
// which is the whole point of an allowlist over a blacklist.
var safeRcloneConfigKeys = map[string]bool{
	"type":                    true,
	"provider":                true,
	"region":                  true,
	"endpoint":                true,
	"location":                true,
	"location_constraint":     true,
	"acl":                     true,
	"storage_class":           true,
	"bucket":                  true,
	"bucket_acl":              true,
	"chunk_size":              true,
	"upload_concurrency":      true,
	"hard_delete":             true,
	"versions":                true,
	"download_url":            true,
	"copy_cutoff":             true,
	"disable_checksum":        true,
	"force_path_style":        true,
	"server_side_encryption":  true,
	"sse_customer_algorithm":  true,
	"no_check_bucket":         true,
	"team_drive":              true,
	"root_folder_id":          true,
	"scope":                   true,
	"shared_credentials_file": true,
	"profile":                 true,
	"env_auth":                true,
	"account":                 true, // username-style identifier (e.g. B2 account/key id)
	"key_id":                  true,
	"user":                    true,
	"username":                true,
	"vendor":                  true,
	"host":                    true,
	"port":                    true,
	"url":                     true,
}

func redactRcloneConfig(data []byte) (string, string) {
	var provider string
	lines := strings.Split(string(data), "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, ";") {
			continue
		}

		key, _, found := strings.Cut(trimmed, "=")
		if !found {
			continue
		}

		key = strings.TrimSpace(key)
		lowerKey := strings.ToLower(key)
		if lowerKey == "type" && provider == "" {
			_, value, _ := strings.Cut(trimmed, "=")
			provider = strings.TrimSpace(value)
		}
		// Allowlist: redact anything that isn't explicitly known to be safe.
		if !safeRcloneConfigKeys[lowerKey] {
			prefix := line[:strings.Index(line, "=")+1]
			lines[i] = prefix + " [redacted]"
		}
	}

	return strings.TrimRight(strings.Join(lines, "\n"), "\n"), provider
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
	r.Body = http.MaxBytesReader(w, r.Body, maxPasswordChangeBodyBytes)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			http.Error(w, "Request body too large", http.StatusRequestEntityTooLarge)
			return
		}
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

// HandleConfigB2Regions returns available Backblaze B2 regions with their endpoints.
func (ctx *Context) HandleConfigB2Regions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	JSONResponse(w, validation.B2Regions)
}

// logConfigChange logs rclone configuration changes with client information
// HandleConfigB2 handles Backblaze B2 remote configuration
func (ctx *Context) HandleConfigB2(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req b2ConfigRequest
	r.Body = http.MaxBytesReader(w, r.Body, maxB2ConfigBodyBytes)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			http.Error(w, "Request body too large", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	b2cfg, remoteName, remotePath, err := req.toB2Config()
	if err != nil {
		logConfigChange(r, "b2_validation_failed", err.Error())
		w.WriteHeader(http.StatusBadRequest)
		JSONResponse(w, map[string]any{"success": false, "error": err.Error()})
		return
	}

	configBytes, err := validation.BuildB2RcloneConfig(b2cfg)
	if err != nil {
		logConfigChange(r, "b2_validation_failed", err.Error())
		w.WriteHeader(http.StatusBadRequest)
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
		w.WriteHeader(http.StatusBadRequest)
		JSONResponse(w, map[string]any{"success": false, "error": errMsg})
		return
	}

	// Write config file
	configPath := state.GetRcloneConfigPath()
	if err := os.MkdirAll(filepath.Dir(configPath), 0750); err != nil {
		logConfigChange(r, "b2_write_error", err.Error())
		http.Error(w, fmt.Sprintf("Failed to create config directory: %v", err), http.StatusInternalServerError)
		return
	}
	if err := utils.AtomicWrite(configPath, configBytes, 0600); err != nil {
		logConfigChange(r, "b2_write_error", err.Error())
		http.Error(w, fmt.Sprintf("Failed to write config: %v", err), http.StatusInternalServerError)
		return
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

	logConfigChange(r, "b2_success", fmt.Sprintf("Configured B2 remote '%s' with bucket '%s'", remoteName, b2cfg.Bucket))
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
		tailscaleConfigured, err := hasConfiguredTailscaleAuthKey()
		if err != nil {
			log.Printf("Failed to read Tailscale auth key status: %v", err)
		}
		response["tailscale_auth_key_configured"] = tailscaleConfigured
		response["tailscale_auth_key_path"] = tailscaleAuthKeyPath
		JSONResponse(w, response)

	case http.MethodPost:
		var req struct {
			RemoteName               *string  `json:"remote_name"`
			RemotePath               *string  `json:"remote_path"`
			ReformatThreshold        *float64 `json:"reformat_threshold"`
			Transfers                *int     `json:"transfers"`
			Checkers                 *int     `json:"checkers"`
			GooglePhotosEnabled      *bool    `json:"google_photos_enabled"`
			GooglePhotos             *bool    `json:"google_photos"`
			GooglePhotosRemoteName   *string  `json:"google_photos_remote_name"`
			GooglePhotosOAuthEnabled *bool    `json:"google_photos_oauth_enabled"`
			GooglePhotosClientID     *string  `json:"google_photos_client_id"`
			GooglePhotosClientSecret *string  `json:"google_photos_client_secret"`
			Prefer5GHzWiFi           *bool    `json:"prefer_5ghz_wifi"`
			TailscaleAuthKey         *string  `json:"tailscale_auth_key"`
		}

		r.Body = http.MaxBytesReader(w, r.Body, maxSettingsBodyBytes)
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			var maxErr *http.MaxBytesError
			if errors.As(err, &maxErr) {
				http.Error(w, "Request body too large", http.StatusRequestEntityTooLarge)
				return
			}
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		if req.TailscaleAuthKey != nil && *req.TailscaleAuthKey != "" {
			if err := settings.ValidateTailscaleAuthKey(*req.TailscaleAuthKey); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
		}

		// Update settings
		if req.RemoteName != nil || req.RemotePath != nil {
			remoteName := ctx.AppSettings.GetRemoteName()
			remotePath := ctx.AppSettings.GetRemotePath()
			if req.RemoteName != nil {
				remoteName = *req.RemoteName
			}
			if req.RemotePath != nil {
				remotePath = *req.RemotePath
			}
			if err := ctx.AppSettings.SetRemote(remoteName, remotePath); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			// Update sync manager
			ctx.SyncMgr.SetRemote(remoteName, remotePath)
		}

		if req.ReformatThreshold != nil {
			if err := ctx.AppSettings.SetReformatThreshold(*req.ReformatThreshold); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}

		if req.Transfers != nil {
			if err := ctx.AppSettings.SetTransfers(*req.Transfers); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}

		if req.Checkers != nil {
			if err := ctx.AppSettings.SetCheckers(*req.Checkers); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}

		// Update Google Photos settings only when requested. The legacy
		// google_photos key is kept for older UI payloads.
		if req.GooglePhotosEnabled != nil || req.GooglePhotos != nil || req.GooglePhotosRemoteName != nil {
			googlePhotosEnabled := ctx.AppSettings.GetGooglePhotosEnabled()
			googlePhotosRemoteName := ctx.AppSettings.GetGooglePhotosRemoteName()
			if req.GooglePhotosEnabled != nil {
				googlePhotosEnabled = *req.GooglePhotosEnabled
			} else if req.GooglePhotos != nil {
				googlePhotosEnabled = *req.GooglePhotos
			}
			if req.GooglePhotosRemoteName != nil {
				googlePhotosRemoteName = *req.GooglePhotosRemoteName
			}

			if err := ctx.AppSettings.SetGooglePhotos(googlePhotosEnabled, googlePhotosRemoteName); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			// Update sync manager with Google Photos settings
			ctx.SyncMgr.SetGooglePhotos(googlePhotosEnabled, googlePhotosRemoteName)
		}

		// Update Google Photos native OAuth settings
		if req.GooglePhotosOAuthEnabled != nil || req.GooglePhotosClientID != nil || req.GooglePhotosClientSecret != nil {
			oauthEnabled := ctx.AppSettings.GetGooglePhotosOAuthEnabled()
			clientID := ctx.AppSettings.GetGooglePhotosClientID()
			clientSecret := ctx.AppSettings.GetGooglePhotosClientSecret()
			if req.GooglePhotosOAuthEnabled != nil {
				oauthEnabled = *req.GooglePhotosOAuthEnabled
			}
			if req.GooglePhotosClientID != nil {
				clientID = *req.GooglePhotosClientID
			}
			if req.GooglePhotosClientSecret != nil {
				clientSecret = *req.GooglePhotosClientSecret
			}
			if err := ctx.AppSettings.SetGooglePhotosOAuth(oauthEnabled, clientID, clientSecret); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			// Re-initialize Google Photos client if credentials now exist
			ctx.EnsureGooglePhotosClient()
		}

		if req.Prefer5GHzWiFi != nil {
			if err := ctx.AppSettings.SetPrefer5GHzWiFi(*req.Prefer5GHzWiFi); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			if ctx.WiFiMgr != nil {
				ctx.WiFiMgr.SetPrefer5GHzNetworks(*req.Prefer5GHzWiFi)
			}
		}

		response := map[string]any{"status": "ok"}
		if req.TailscaleAuthKey != nil && *req.TailscaleAuthKey != "" {
			if warning, err := configureTailscale(*req.TailscaleAuthKey); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			} else if warning != "" {
				response["warning"] = warning
			}
			response["tailscale_auth_key_configured"] = true
			response["tailscale_auth_key_path"] = tailscaleAuthKeyPath
			log.Println("Tailscale auth key saved")
		}

		log.Println("Settings updated")
		JSONResponse(w, response)

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func hasConfiguredTailscaleAuthKey() (bool, error) {
	if tailscaleAuthKeyPath == settings.TailscaleAuthKeyFile {
		return settings.HasTailscaleAuthKey()
	}
	return settings.HasTailscaleAuthKeyAt(tailscaleAuthKeyPath)
}

type b2ConfigRequest struct {
	Account     string `json:"account_id"`
	KeyID       string `json:"key_id"`
	Key         string `json:"application_key"`
	AppKey      string `json:"app_key"`
	Bucket      string `json:"bucket_name"`
	BucketAlias string `json:"bucket"`
	RemoteName  string `json:"remote_name"`
	RemotePath  string `json:"remote_path"`
	Endpoint    string `json:"endpoint"`
	Region      string `json:"region"`
}

func (req b2ConfigRequest) toB2Config() (*validation.B2Config, string, string, error) {
	account := strings.TrimSpace(req.Account)
	if account == "" {
		account = strings.TrimSpace(req.KeyID)
	}

	key := strings.TrimSpace(req.Key)
	if key == "" {
		key = strings.TrimSpace(req.AppKey)
	}

	bucket := strings.TrimSpace(req.Bucket)
	if bucket == "" {
		bucket = strings.TrimSpace(req.BucketAlias)
	}

	remoteName := strings.TrimSpace(req.RemoteName)
	if remoteName == "" {
		remoteName = "b2"
	}

	remotePath := strings.TrimSpace(req.RemotePath)
	if remotePath == "" && bucket != "" {
		remotePath = fmt.Sprintf("%s/photos", strings.Trim(bucket, "/"))
	}

	endpoint, err := resolveB2Endpoint(req.Endpoint, req.Region)
	if err != nil {
		return nil, "", "", err
	}

	cfg := &validation.B2Config{
		Account:    account,
		Key:        key,
		Bucket:     bucket,
		RemoteName: remoteName,
		RemotePath: remotePath,
		Endpoint:   endpoint,
	}

	return cfg, remoteName, remotePath, nil
}

func resolveB2Endpoint(endpoint, region string) (string, error) {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint != "" {
		return endpoint, nil
	}

	region = strings.TrimSpace(region)
	if region == "" {
		return "", nil
	}

	if strings.HasPrefix(region, "https://") || strings.HasPrefix(region, "http://") {
		return region, nil
	}

	for _, knownRegion := range validation.B2Regions {
		if region == knownRegion.ID || region == knownRegion.Name {
			return knownRegion.Endpoint, nil
		}
	}

	return "", fmt.Errorf("unknown B2 region: %s", region)
}

func configureTailscale(authKey string) (string, error) {
	if err := settings.ValidateTailscaleAuthKey(authKey); err != nil {
		return "", err
	}
	if err := settings.SaveTailscaleAuthKeyTo(tailscaleAuthKeyPath, authKey); err != nil {
		return "", fmt.Errorf("failed to store tailscale auth key: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	args := []string{
		"up",
		"--auth-key=" + authKey,
		"--ssh",
		"--accept-dns=false",
	}
	if hostname, err := os.Hostname(); err == nil && strings.TrimSpace(hostname) != "" {
		args = append(args, "--hostname="+strings.TrimSpace(hostname))
	}

	output, err := tailscaleCommandContext(ctx, tailscaleBinary, args...).CombinedOutput()
	if err != nil {
		details := strings.ReplaceAll(strings.TrimSpace(string(output)), authKey, "[redacted]")
		if details == "" {
			details = err.Error()
		}
		return fmt.Sprintf("Tailscale auth key was saved, but immediate Tailscale connect failed: %s", details), nil
	}

	return "", nil
}
