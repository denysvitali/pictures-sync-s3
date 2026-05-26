// Package settings manages persistent application configuration.
// Settings include rclone remote configuration, reformat detection thresholds,
// parallel transfer settings, and Google Photos integration. All settings are
// automatically persisted to disk with thread-safe access and input validation.
package settings

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

// Settings represents persistent application settings
type Settings struct {
	SchemaVersion int `json:"schema_version"`

	RemoteName string `json:"remote_name"`
	RemotePath string `json:"remote_path"`

	// Card reformat detection threshold (percentage)
	ReformatThreshold float64 `json:"reformat_threshold"`

	// Rclone parallel transfer settings
	Transfers int `json:"transfers"` // Number of files to transfer in parallel
	Checkers  int `json:"checkers"`  // Number of checkers to run in parallel

	// Google Photos optional upload settings
	GooglePhotosEnabled    bool   `json:"google_photos_enabled"`     // Enable uploading JPG files to Google Photos
	GooglePhotosRemoteName string `json:"google_photos_remote_name"` // Google Photos rclone remote name

	// Google Photos OAuth credentials (for automatic rclone remote setup)
	GooglePhotosOAuthEnabled bool   `json:"google_photos_oauth_enabled"` // Enable native OAuth flow for Google Photos
	GooglePhotosClientID     string `json:"google_photos_client_id"`     // Google Photos OAuth client ID
	GooglePhotosClientSecret string `json:"google_photos_client_secret"` // Google Photos OAuth client secret

	// WiFi scan behavior
	Prefer5GHzWiFi bool `json:"prefer_5ghz_wifi"` // Prefer 5 GHz APs when duplicate SSIDs are found

	mu sync.RWMutex
}

// UnmarshalJSON rejects malformed configuration shapes before applying fields.
func (s *Settings) UnmarshalJSON(data []byte) error {
	if !utf8.Valid(data) {
		return errors.New("settings JSON must be valid UTF-8")
	}
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return errors.New("settings JSON cannot be null")
	}
	if jsonNestingDepth(data) > 100 {
		return errors.New("settings JSON nesting is too deep")
	}

	type settingsAlias Settings
	var decoded settingsAlias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	s.RemoteName = decoded.RemoteName
	s.SchemaVersion = decoded.SchemaVersion
	s.RemotePath = decoded.RemotePath
	s.ReformatThreshold = decoded.ReformatThreshold
	s.Transfers = decoded.Transfers
	s.Checkers = decoded.Checkers
	s.GooglePhotosEnabled = decoded.GooglePhotosEnabled
	s.GooglePhotosRemoteName = decoded.GooglePhotosRemoteName
	s.GooglePhotosOAuthEnabled = decoded.GooglePhotosOAuthEnabled
	s.GooglePhotosClientID = decoded.GooglePhotosClientID
	s.GooglePhotosClientSecret = decoded.GooglePhotosClientSecret
	s.Prefer5GHzWiFi = decoded.Prefer5GHzWiFi
	if _, ok := raw["prefer_5ghz_wifi"]; !ok {
		s.Prefer5GHzWiFi = true
	}
	return nil
}

func jsonNestingDepth(data []byte) int {
	depth := 0
	maxDepth := 0
	inString := false
	escaped := false

	for _, b := range data {
		if escaped {
			escaped = false
			continue
		}
		if inString {
			if b == '\\' {
				escaped = true
			} else if b == '"' {
				inString = false
			}
			continue
		}
		switch b {
		case '"':
			inString = true
		case '{', '[':
			depth++
			if depth > maxDepth {
				maxDepth = depth
			}
		case '}', ']':
			if depth > 0 {
				depth--
			}
		}
	}

	return maxDepth
}

// Validation constants
const (
	SchemaVersion = 1

	MinTransfers              = 1
	MaxTransfers              = 128
	MinCheckers               = 1
	MaxCheckers               = 256
	MinReformatThreshold      = 0.0
	MaxReformatThreshold      = 1.0
	MaxRemoteNameLength       = 255
	MaxRemotePathLength       = 4096
	MaxTailscaleAuthKeyLength = 512
)

// DefaultSettings returns default settings
func DefaultSettings() *Settings {
	return &Settings{
		SchemaVersion:     SchemaVersion,
		RemoteName:        "remote",
		RemotePath:        "/photos",
		ReformatThreshold: 0.3, // 30%
		Transfers:         4,   // 4 parallel uploads
		Checkers:          8,   // 8 parallel file checkers
		Prefer5GHzWiFi:    true,
	}
}

// Validation functions

// ValidateRemoteName validates a remote name
func ValidateRemoteName(name string) error {
	if name == "" {
		return errors.New("remote name cannot be empty")
	}
	if strings.TrimSpace(name) != name {
		return errors.New("remote name cannot have leading or trailing whitespace")
	}
	if len(name) > MaxRemoteNameLength {
		return fmt.Errorf("remote name exceeds maximum length of %d characters", MaxRemoteNameLength)
	}
	if strings.ContainsAny(name, "\x00\r\n") {
		return errors.New("remote name contains invalid control characters")
	}
	if strings.ContainsAny(name, ";|&$`'\"'\\") {
		return errors.New("remote name contains potentially unsafe shell characters")
	}
	if strings.ContainsFunc(name, func(r rune) bool {
		return r > unicode.MaxASCII || unicode.IsControl(r)
	}) {
		return errors.New("remote name contains non-ASCII or control characters")
	}
	return nil
}

// ValidateRemotePath validates a remote path
func ValidateRemotePath(path string) error {
	if path == "" {
		return errors.New("remote path cannot be empty")
	}
	if strings.TrimSpace(path) != path {
		return errors.New("remote path cannot have leading or trailing whitespace")
	}
	if len(path) > MaxRemotePathLength {
		return fmt.Errorf("remote path exceeds maximum length of %d characters", MaxRemotePathLength)
	}
	if strings.ContainsAny(path, "\x00\r\n") {
		return errors.New("remote path contains invalid control characters")
	}
	if strings.Contains(path, "..") {
		return errors.New("remote path contains path traversal sequences (..)")
	}
	return nil
}

// ValidateReformatThreshold validates the reformat detection threshold
func ValidateReformatThreshold(threshold float64) error {
	if math.IsNaN(threshold) {
		return errors.New("reformat threshold cannot be NaN")
	}
	if math.IsInf(threshold, 0) {
		return errors.New("reformat threshold cannot be infinite")
	}
	if threshold < MinReformatThreshold {
		return fmt.Errorf("reformat threshold must be at least %.2f", MinReformatThreshold)
	}
	if threshold > MaxReformatThreshold {
		return fmt.Errorf("reformat threshold must not exceed %.2f", MaxReformatThreshold)
	}
	return nil
}

// ValidateTransfers validates the number of parallel transfers
func ValidateTransfers(transfers int) error {
	if transfers < MinTransfers {
		return fmt.Errorf("transfers must be at least %d", MinTransfers)
	}
	if transfers > MaxTransfers {
		return fmt.Errorf("transfers must not exceed %d", MaxTransfers)
	}
	return nil
}

// ValidateCheckers validates the number of parallel checkers
func ValidateCheckers(checkers int) error {
	if checkers < MinCheckers {
		return fmt.Errorf("checkers must be at least %d", MinCheckers)
	}
	if checkers > MaxCheckers {
		return fmt.Errorf("checkers must not exceed %d", MaxCheckers)
	}
	return nil
}

// ValidateGooglePhotos validates Google Photos settings
func ValidateGooglePhotos(enabled bool, remoteName string) error {
	if enabled && remoteName == "" {
		return errors.New("google photos remote name is required when enabled")
	}
	if enabled {
		return ValidateRemoteName(remoteName)
	}
	return nil
}

// ValidateGooglePhotosClientID validates a Google Photos OAuth client ID
func ValidateGooglePhotosClientID(id string) error {
	if id == "" {
		return nil // empty is valid (optional)
	}
	if strings.ContainsAny(id, "\x00\r\n\t ") {
		return errors.New("google photos client ID contains invalid whitespace")
	}
	return nil
}

// ValidateGooglePhotosClientSecret validates a Google Photos OAuth client secret
func ValidateGooglePhotosClientSecret(secret string) error {
	if secret == "" {
		return nil // empty is valid (optional)
	}
	if strings.ContainsAny(secret, "\x00\r\n") {
		return errors.New("google photos client secret contains invalid characters")
	}
	return nil
}

// ValidateTailscaleAuthKey validates a Tailscale auth key before it is passed to tailscale.
func ValidateTailscaleAuthKey(authKey string) error {
	if authKey == "" {
		return errors.New("tailscale auth key cannot be empty")
	}
	if strings.TrimSpace(authKey) != authKey {
		return errors.New("tailscale auth key cannot have leading or trailing whitespace")
	}
	if len(authKey) > MaxTailscaleAuthKeyLength {
		return fmt.Errorf("tailscale auth key exceeds maximum length of %d characters", MaxTailscaleAuthKeyLength)
	}
	if strings.ContainsAny(authKey, "\x00\r\n\t ") {
		return errors.New("tailscale auth key contains invalid whitespace or control characters")
	}
	if !strings.HasPrefix(authKey, "tskey-auth-") {
		return errors.New("tailscale auth key must start with tskey-auth-")
	}
	return nil
}

// Validate validates all settings fields
func (s *Settings) Validate() error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.SchemaVersion > SchemaVersion {
		return fmt.Errorf("unsupported settings schema version %d", s.SchemaVersion)
	}
	if err := ValidateRemoteName(s.RemoteName); err != nil {
		return fmt.Errorf("remote name: %w", err)
	}
	if err := ValidateRemotePath(s.RemotePath); err != nil {
		return fmt.Errorf("remote path: %w", err)
	}
	if err := ValidateReformatThreshold(s.ReformatThreshold); err != nil {
		return fmt.Errorf("reformat threshold: %w", err)
	}
	if err := ValidateTransfers(s.Transfers); err != nil {
		return fmt.Errorf("transfers: %w", err)
	}
	if err := ValidateCheckers(s.Checkers); err != nil {
		return fmt.Errorf("checkers: %w", err)
	}
	if err := ValidateGooglePhotos(s.GooglePhotosEnabled, s.GooglePhotosRemoteName); err != nil {
		return fmt.Errorf("google photos: %w", err)
	}
	if err := ValidateGooglePhotosClientID(s.GooglePhotosClientID); err != nil {
		return fmt.Errorf("google photos client id: %w", err)
	}
	if err := ValidateGooglePhotosClientSecret(s.GooglePhotosClientSecret); err != nil {
		return fmt.Errorf("google photos client secret: %w", err)
	}
	return nil
}

// Getters (with read lock)

// GetRemoteName returns the remote name
func (s *Settings) GetRemoteName() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.RemoteName
}

// GetRemotePath returns the remote path
func (s *Settings) GetRemotePath() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.RemotePath
}

// GetReformatThreshold returns the reformat detection threshold
func (s *Settings) GetReformatThreshold() float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.ReformatThreshold
}

// GetTransfers returns the number of parallel transfers
func (s *Settings) GetTransfers() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Transfers
}

// GetCheckers returns the number of parallel checkers
func (s *Settings) GetCheckers() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Checkers
}

// GetGooglePhotosEnabled returns whether Google Photos upload is enabled
func (s *Settings) GetGooglePhotosEnabled() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.GooglePhotosEnabled
}

// GetGooglePhotosRemoteName returns the Google Photos remote name
func (s *Settings) GetGooglePhotosRemoteName() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.GooglePhotosRemoteName
}

// GetGooglePhotosOAuthEnabled returns whether native OAuth is enabled
func (s *Settings) GetGooglePhotosOAuthEnabled() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.GooglePhotosOAuthEnabled
}

// GetGooglePhotosClientID returns the Google Photos OAuth client ID
func (s *Settings) GetGooglePhotosClientID() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.GooglePhotosClientID
}

// GetGooglePhotosClientSecret returns the Google Photos OAuth client secret
func (s *Settings) GetGooglePhotosClientSecret() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.GooglePhotosClientSecret
}

// GetPrefer5GHzWiFi returns whether 5 GHz APs are preferred for duplicate SSIDs.
func (s *Settings) GetPrefer5GHzWiFi() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Prefer5GHzWiFi
}

// Setters (with validation and auto-save)

// SetRemote updates the remote name and path
func (s *Settings) SetRemote(name, path string) error {
	if err := ValidateRemoteName(name); err != nil {
		return fmt.Errorf("invalid remote name: %w", err)
	}
	if err := ValidateRemotePath(path); err != nil {
		return fmt.Errorf("invalid remote path: %w", err)
	}

	s.mu.Lock()
	s.RemoteName = name
	s.RemotePath = path
	s.mu.Unlock()

	return s.Save()
}

// SetReformatThreshold updates the reformat detection threshold
func (s *Settings) SetReformatThreshold(threshold float64) error {
	if err := ValidateReformatThreshold(threshold); err != nil {
		return err
	}

	s.mu.Lock()
	s.ReformatThreshold = threshold
	s.mu.Unlock()

	return s.Save()
}

// SetTransfers updates the number of parallel transfers
func (s *Settings) SetTransfers(transfers int) error {
	if err := ValidateTransfers(transfers); err != nil {
		return err
	}

	s.mu.Lock()
	s.Transfers = transfers
	s.mu.Unlock()

	return s.Save()
}

// SetCheckers updates the number of parallel checkers
func (s *Settings) SetCheckers(checkers int) error {
	if err := ValidateCheckers(checkers); err != nil {
		return err
	}

	s.mu.Lock()
	s.Checkers = checkers
	s.mu.Unlock()

	return s.Save()
}

// SetGooglePhotos updates the Google Photos settings
func (s *Settings) SetGooglePhotos(enabled bool, remoteName string) error {
	if err := ValidateGooglePhotos(enabled, remoteName); err != nil {
		return err
	}

	s.mu.Lock()
	s.GooglePhotosEnabled = enabled
	s.GooglePhotosRemoteName = remoteName
	s.mu.Unlock()

	return s.Save()
}

// SetGooglePhotosOAuth updates the Google Photos OAuth credentials
func (s *Settings) SetGooglePhotosOAuth(enabled bool, clientID, clientSecret string) error {
	if err := ValidateGooglePhotosClientID(clientID); err != nil {
		return err
	}
	if err := ValidateGooglePhotosClientSecret(clientSecret); err != nil {
		return err
	}

	s.mu.Lock()
	s.GooglePhotosOAuthEnabled = enabled
	s.GooglePhotosClientID = clientID
	s.GooglePhotosClientSecret = clientSecret
	s.mu.Unlock()

	return s.Save()
}

// SetPrefer5GHzWiFi updates WiFi scan preference behavior.
func (s *Settings) SetPrefer5GHzWiFi(prefer bool) error {
	s.mu.Lock()
	s.Prefer5GHzWiFi = prefer
	s.mu.Unlock()

	return s.Save()
}

// Helper methods

// ToJSON returns settings as JSON for API responses
func (s *Settings) ToJSON() map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return map[string]any{
		"schema_version":              s.SchemaVersion,
		"remote_name":                 s.RemoteName,
		"remote_path":                 s.RemotePath,
		"reformat_threshold":          s.ReformatThreshold,
		"transfers":                   s.Transfers,
		"checkers":                    s.Checkers,
		"google_photos_enabled":       s.GooglePhotosEnabled,
		"google_photos_remote_name":   s.GooglePhotosRemoteName,
		"google_photos_oauth_enabled": s.GooglePhotosOAuthEnabled,
		"google_photos_client_id":     s.GooglePhotosClientID,
		"google_photos_client_secret": s.GooglePhotosClientSecret,
		"prefer_5ghz_wifi":            s.Prefer5GHzWiFi,
	}
}

// GetRemoteDestination returns the full rclone destination path for a card ID
func (s *Settings) GetRemoteDestination(cardID string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return fmt.Sprintf("%s:%s/%s/DCIM/", s.RemoteName, s.RemotePath, cardID)
}

// GetGooglePhotosDestination returns the Google Photos destination path
func (s *Settings) GetGooglePhotosDestination() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.GooglePhotosEnabled {
		return ""
	}
	return fmt.Sprintf("%s:", s.GooglePhotosRemoteName)
}

// Clone returns a deep copy of the settings (useful for testing)
func (s *Settings) Clone() *Settings {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return &Settings{
		RemoteName:               s.RemoteName,
		RemotePath:               s.RemotePath,
		ReformatThreshold:        s.ReformatThreshold,
		Transfers:                s.Transfers,
		Checkers:                 s.Checkers,
		GooglePhotosEnabled:      s.GooglePhotosEnabled,
		GooglePhotosRemoteName:   s.GooglePhotosRemoteName,
		GooglePhotosOAuthEnabled: s.GooglePhotosOAuthEnabled,
		GooglePhotosClientID:     s.GooglePhotosClientID,
		GooglePhotosClientSecret: s.GooglePhotosClientSecret,
		Prefer5GHzWiFi:           s.Prefer5GHzWiFi,
	}
}
