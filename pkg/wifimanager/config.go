package wifimanager

import (
	"encoding/json"
	"log"
	"os"
	"strings"

	"github.com/denysvitali/pictures-sync-s3/pkg/utils"
)

// WiFiConfigPath is the path to the WiFi configuration file.
// It can be modified for testing purposes.
var WiFiConfigPath = "/perm/extra-wifi.json"
var FirstBootWiFiConfigPath = "/perm/wifi.json"
var FirstBootWiFiSSID = "PhotoBackup-Setup"
var FirstBootWiFiPassword = "photo-backup-setup"

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

// EnsureFirstBootWiFiConfig creates a default AP-style Wi-Fi network only when no
// configured Wi-Fi networks exist (both in /perm/wifi.json and /perm/extra-wifi.json).
// This helps first-time setup by creating an immediate local way to connect.
func (m *Manager) EnsureFirstBootWiFiConfig() error {
	hasGokrazyNetworks, err := hasConfiguredNetworks(FirstBootWiFiConfigPath)
	if err != nil {
		return err
	}

	hasAppNetworks, err := hasConfiguredNetworks(WiFiConfigPath)
	if err != nil {
		return err
	}

	if hasGokrazyNetworks || hasAppNetworks {
		return nil
	}

	defaultSSID := strings.TrimSpace(os.Getenv("SETUP_WIFI_SSID"))
	if defaultSSID == "" {
		defaultSSID = FirstBootWiFiSSID
	}

	defaultPSK := strings.TrimSpace(os.Getenv("SETUP_WIFI_PSK"))
	if defaultPSK == "" {
		defaultPSK = FirstBootWiFiPassword
	}

	config := []Network{{
		SSID: defaultSSID,
		PSK:  defaultPSK,
	}}

	if err := utils.SaveJSON(FirstBootWiFiConfigPath, config, 0600); err != nil {
		return err
	}

	log.Printf("No Wi-Fi networks found; seeded first-boot setup network %q in %s", defaultSSID, FirstBootWiFiConfigPath)
	return nil
}

func hasConfiguredNetworks(path string) (bool, error) {
	networks, err := loadWiFiNetworksFromFile(path)
	if err != nil {
		return false, err
	}
	return len(networks) > 0, nil
}

// loadWiFiNetworksFromFile reads a Wi-Fi config in array format.
func loadWiFiNetworksFromFile(path string) ([]Network, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var networks []Network
	if err := json.Unmarshal(data, &networks); err != nil {
		return nil, err
	}

	return networks, nil
}
