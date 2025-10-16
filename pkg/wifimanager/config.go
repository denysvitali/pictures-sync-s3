package wifimanager

import (
	"os"

	"github.com/denysvitali/pictures-sync-s3/pkg/utils"
)

// WiFiConfigPath is the path to the WiFi configuration file.
// It can be modified for testing purposes.
var WiFiConfigPath = "/perm/extra-wifi.json"

// WiFiConfig represents the structure of the WiFi configuration file
type WiFiConfig struct {
	Networks []Network `json:"networks"`
}

// save persists the WiFi configuration to disk
// Must be called with m.mu held (either read or write lock)
func (m *Manager) save() error {
	config := WiFiConfig{
		Networks: m.networks,
	}

	return utils.SaveJSON(WiFiConfigPath, config, 0600)
}

// load reads the WiFi configuration from disk
func (m *Manager) load() error {
	var config WiFiConfig

	// Load WiFi config from file
	if err := utils.LoadJSON(WiFiConfigPath, &config, nil); err != nil {
		return err
	}

	m.networks = config.Networks
	if m.networks == nil {
		m.networks = make([]Network, 0)
	}

	return nil
}

// atomicWriteFile writes data to a file atomically using a temp file
// Uses the utils package for consistent atomic writes across the codebase
func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	return utils.AtomicWrite(path, data, perm)
}
