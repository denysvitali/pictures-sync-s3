package settings

import (
	"fmt"
	"os"

	"github.com/denysvitali/pictures-sync-s3/pkg/utils"
)

const (
	SettingsFile = "/perm/pictures-sync/settings.json"
)

// Load reads settings from disk, or returns defaults if not found
func Load() (*Settings, error) {
	return LoadFrom(SettingsFile)
}

// LoadFrom reads settings from a specific file path (useful for testing)
func LoadFrom(path string) (*Settings, error) {
	s := &Settings{}

	// Load JSON using utils package
	if err := utils.LoadJSON(path, s, nil); err != nil {
		if os.IsNotExist(err) {
			// No settings file yet, return defaults
			return DefaultSettings(), nil
		}
		return nil, utils.WrapError(err, "failed to load settings")
	}

	// Apply defaults for missing fields (backward compatibility)
	applyDefaults(s)

	// Validate loaded settings
	if err := s.Validate(); err != nil {
		return nil, utils.WrapError(err, "loaded settings are invalid")
	}

	return s, nil
}

// applyDefaults fills in default values for any zero-valued fields
// This maintains backward compatibility when loading older config files
func applyDefaults(s *Settings) {
	defaults := DefaultSettings()

	// Only apply defaults if field is truly missing (empty string or zero)
	// Note: This means you cannot explicitly set these to zero/empty
	if s.RemoteName == "" {
		s.RemoteName = defaults.RemoteName
	}
	if s.RemotePath == "" {
		s.RemotePath = defaults.RemotePath
	}
	if s.ReformatThreshold == 0 {
		s.ReformatThreshold = defaults.ReformatThreshold
	}
	if s.Transfers == 0 {
		s.Transfers = defaults.Transfers
	}
	if s.Checkers == 0 {
		s.Checkers = defaults.Checkers
	}
}

// Save persists settings to disk
func (s *Settings) Save() error {
	return s.SaveTo(SettingsFile)
}

// SaveTo persists settings to a specific file path (useful for testing)
func (s *Settings) SaveTo(path string) error {
	// Use full Lock (not RLock) to prevent concurrent saves
	s.mu.Lock()
	defer s.mu.Unlock()

	// Use utils package to save JSON atomically
	return utils.SaveJSON(path, s, 0644)
}

// CleanupTempFiles removes any orphaned .tmp files from previous failed saves
func CleanupTempFiles() error {
	tmpFile := SettingsFile + ".tmp"
	if _, err := os.Stat(tmpFile); err == nil {
		// Temp file exists, remove it
		if err := os.Remove(tmpFile); err != nil {
			return fmt.Errorf("failed to remove orphaned temp file: %w", err)
		}
	}
	return nil
}
