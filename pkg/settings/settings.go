package settings

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
)

const (
	SettingsFile = "/perm/pictures-sync/settings.json"
)

// Settings represents persistent application settings
type Settings struct {
	RemoteName string  `json:"remote_name"`
	RemotePath string  `json:"remote_path"`

	// Card reformat detection threshold (percentage)
	ReformatThreshold float64 `json:"reformat_threshold"`

	mu sync.RWMutex
}

// DefaultSettings returns default settings
func DefaultSettings() *Settings {
	return &Settings{
		RemoteName:        "remote",
		RemotePath:        "/photos",
		ReformatThreshold: 0.3, // 30%
	}
}

// Load reads settings from disk, or returns defaults if not found
func Load() (*Settings, error) {
	data, err := os.ReadFile(SettingsFile)
	if err != nil {
		if os.IsNotExist(err) {
			// No settings file yet, return defaults
			return DefaultSettings(), nil
		}
		return nil, fmt.Errorf("failed to read settings: %w", err)
	}

	s := &Settings{}
	if err := json.Unmarshal(data, s); err != nil {
		return nil, fmt.Errorf("failed to unmarshal settings: %w", err)
	}

	// Apply defaults for missing fields
	if s.RemoteName == "" {
		s.RemoteName = "remote"
	}
	if s.RemotePath == "" {
		s.RemotePath = "/photos"
	}
	if s.ReformatThreshold == 0 {
		s.ReformatThreshold = 0.3
	}

	return s, nil
}

// Save persists settings to disk
func (s *Settings) Save() error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal settings: %w", err)
	}

	// Atomic write
	tmpFile := SettingsFile + ".tmp"
	if err := os.WriteFile(tmpFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write settings file: %w", err)
	}
	if err := os.Rename(tmpFile, SettingsFile); err != nil {
		return fmt.Errorf("failed to rename settings file: %w", err)
	}

	return nil
}

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

// SetRemote updates the remote name and path
func (s *Settings) SetRemote(name, path string) error {
	s.mu.Lock()
	s.RemoteName = name
	s.RemotePath = path
	s.mu.Unlock()

	return s.Save()
}

// SetReformatThreshold updates the reformat detection threshold
func (s *Settings) SetReformatThreshold(threshold float64) error {
	s.mu.Lock()
	s.ReformatThreshold = threshold
	s.mu.Unlock()

	return s.Save()
}

// ToJSON returns settings as JSON for API responses
func (s *Settings) ToJSON() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return map[string]interface{}{
		"remote_name":        s.RemoteName,
		"remote_path":        s.RemotePath,
		"reformat_threshold": s.ReformatThreshold,
	}
}
