// Package settings manages persistent application configuration.
// Settings include rclone remote configuration, reformat detection thresholds,
// parallel transfer settings, and Google Photos integration. All settings are
// automatically persisted to disk with thread-safe access and input validation.
package settings

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
)

// Settings represents persistent application settings
type Settings struct {
	RemoteName string  `json:"remote_name"`
	RemotePath string  `json:"remote_path"`

	// Card reformat detection threshold (percentage)
	ReformatThreshold float64 `json:"reformat_threshold"`

	// Rclone parallel transfer settings
	Transfers int `json:"transfers"` // Number of files to transfer in parallel
	Checkers  int `json:"checkers"`  // Number of checkers to run in parallel

	// Google Photos optional upload settings
	GooglePhotosEnabled    bool   `json:"google_photos_enabled"`     // Enable uploading JPG files to Google Photos
	GooglePhotosRemoteName string `json:"google_photos_remote_name"` // Google Photos rclone remote name

	mu sync.RWMutex
}

// Validation constants
const (
	MinTransfers         = 1
	MaxTransfers         = 128
	MinCheckers          = 1
	MaxCheckers          = 256
	MinReformatThreshold = 0.0
	MaxReformatThreshold = 1.0
	MaxRemoteNameLength  = 255
	MaxRemotePathLength  = 4096
)

// DefaultSettings returns default settings
func DefaultSettings() *Settings {
	return &Settings{
		RemoteName:        "remote",
		RemotePath:        "/photos",
		ReformatThreshold: 0.3, // 30%
		Transfers:         4,   // 4 parallel uploads
		Checkers:          8,   // 8 parallel file checkers
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
	if strings.ContainsAny(name, ";|&$`") {
		return errors.New("remote name contains potentially unsafe shell characters")
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

// Validate validates all settings fields
func (s *Settings) Validate() error {
	s.mu.RLock()
	defer s.mu.RUnlock()

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

// Helper methods

// ToJSON returns settings as JSON for API responses
func (s *Settings) ToJSON() map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return map[string]any{
		"remote_name":               s.RemoteName,
		"remote_path":               s.RemotePath,
		"reformat_threshold":        s.ReformatThreshold,
		"transfers":                 s.Transfers,
		"checkers":                  s.Checkers,
		"google_photos_enabled":     s.GooglePhotosEnabled,
		"google_photos_remote_name": s.GooglePhotosRemoteName,
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
		RemoteName:             s.RemoteName,
		RemotePath:             s.RemotePath,
		ReformatThreshold:      s.ReformatThreshold,
		Transfers:              s.Transfers,
		Checkers:               s.Checkers,
		GooglePhotosEnabled:    s.GooglePhotosEnabled,
		GooglePhotosRemoteName: s.GooglePhotosRemoteName,
	}
}
