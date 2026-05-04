package wifimanager

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/denysvitali/pictures-sync-s3/pkg/utils"
)

// WiFiConfigPath is the path to the WiFi configuration file.
// It can be modified for testing purposes.
var WiFiConfigPath = "/perm/extra-wifi.json"
var GokrazyWiFiConfigPath = "/perm/wifi.json"

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

	if err := utils.SaveJSON(WiFiConfigPath, config, 0600); err != nil {
		return err
	}
	return saveGokrazyWiFiConfig(m.networks)
}

// load reads the WiFi configuration from disk
func (m *Manager) load() error {
	var config WiFiConfig

	if err := utils.LoadJSON(WiFiConfigPath, &config, nil); err != nil {
		return err
	}

	m.networks = config.Networks
	if m.networks == nil {
		m.networks = make([]Network, 0)
	}

	if len(m.networks) == 0 {
		networks, err := loadWiFiNetworksFromFile(GokrazyWiFiConfigPath)
		if err != nil {
			return err
		}
		m.networks = networks
	}

	return nil
}

// EnsureFirstBootWiFiConfig is kept for compatibility with older tests/callers.
// Hotspot provisioning is handled by cmd/provision-ap, not by seeding a fake
// client network in /perm/wifi.json.
func (m *Manager) EnsureFirstBootWiFiConfig() error {
	return nil
}

func hasConfiguredNetworks(path string) (bool, error) {
	networks, err := loadWiFiNetworksFromFile(path)
	if err != nil {
		return false, err
	}
	return len(networks) > 0, nil
}

// loadWiFiNetworksFromFile reads either the app list format or gokrazy's
// single-network /perm/wifi.json format.
func loadWiFiNetworksFromFile(path string) ([]Network, error) {
	// #nosec G304 -- path is a well-known config file location (/perm/wifi.json or /perm/extra-wifi.json)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var config WiFiConfig
	if err := json.Unmarshal(data, &config); err == nil && config.Networks != nil {
		return filterConfiguredNetworks(config.Networks), nil
	}

	var networks []Network
	if err := json.Unmarshal(data, &networks); err == nil {
		return filterConfiguredNetworks(networks), nil
	}

	var network Network
	if err := json.Unmarshal(data, &network); err != nil {
		return nil, err
	}

	return filterConfiguredNetworks([]Network{network}), nil
}

func saveGokrazyWiFiConfig(networks []Network) error {
	parent := filepath.Dir(GokrazyWiFiConfigPath)
	if parent == "/perm" {
		if _, err := os.Stat(parent); os.IsNotExist(err) {
			return nil
		}
	}

	networks = filterConfiguredNetworks(networks)
	if len(networks) == 0 {
		if err := os.Remove(GokrazyWiFiConfigPath); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	return utils.SaveJSON(GokrazyWiFiConfigPath, networks[0], 0600)
}

func filterConfiguredNetworks(networks []Network) []Network {
	filtered := networks[:0]
	for _, network := range networks {
		network.SSID = strings.TrimSpace(network.SSID)
		if network.SSID != "" {
			filtered = append(filtered, network)
		}
	}
	return filtered
}
