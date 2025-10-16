package wifimanager

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"sync"

	"github.com/mdlayher/wifi"
)

const (
	WiFiConfigPath = "/perm/extra-wifi.json"
)

// Network represents a WiFi network configuration
type Network struct {
	SSID string `json:"ssid"`
	PSK  string `json:"psk,omitempty"` // Password, optional for open networks
}

// Manager manages WiFi configuration
type Manager struct {
	mu       sync.RWMutex
	networks []Network
}

// NewManager creates a new WiFi manager
func NewManager() (*Manager, error) {
	m := &Manager{
		networks: make([]Network, 0),
	}

	// Load existing configuration
	if err := m.load(); err != nil {
		// If loading fails, start with empty config
		return m, nil
	}

	return m, nil
}

// GetNetworks returns the list of configured networks
func (m *Manager) GetNetworks() []Network {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Return a copy
	networks := make([]Network, len(m.networks))
	copy(networks, m.networks)
	return networks
}

// AddNetwork adds a new WiFi network
func (m *Manager) AddNetwork(ssid, password string) error {
	if ssid == "" {
		return fmt.Errorf("SSID cannot be empty")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if network already exists
	for i, net := range m.networks {
		if net.SSID == ssid {
			// Update password
			m.networks[i].PSK = password
			return m.save()
		}
	}

	// Add new network
	network := Network{
		SSID: ssid,
		PSK:  password,
	}
	m.networks = append(m.networks, network)

	return m.save()
}

// RemoveNetwork removes a WiFi network by SSID
func (m *Manager) RemoveNetwork(ssid string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Find and remove network
	for i, net := range m.networks {
		if net.SSID == ssid {
			m.networks = append(m.networks[:i], m.networks[i+1:]...)
			return m.save()
		}
	}

	return fmt.Errorf("network not found: %s", ssid)
}

// ScanNetworks scans for available WiFi networks
func (m *Manager) ScanNetworks() ([]ScanResult, error) {
	log.Printf("WiFi scanning in Gokrazy...")

	// IMPORTANT: WiFi scanning is not natively supported in Gokrazy
	// The gokrazy/wifi package only supports connecting to pre-configured networks,
	// not scanning for available ones. The schollz/wifiscan library uses exec
	// to run iwlist, which doesn't exist in Gokrazy.

	// For now, we'll return an informative message to the user
	// In production, you would need to:
	// 1. Pre-configure known networks in /perm/wifi.json
	// 2. Use a separate device/service for network discovery
	// 3. Or implement direct netlink communication (complex)

	// Check if we can at least detect the current network from gokrazy config
	currentNetwork := m.getCurrentNetworkFromConfig()

	if currentNetwork != "" {
		// Return at least the configured network
		return []ScanResult{
			{
				SSID:      currentNetwork,
				Signal:    -50, // Assume decent signal
				Encrypted: true,
			},
			{
				SSID:      "⚠️ Scanning not supported",
				Signal:    -100,
				Encrypted: false,
			},
			{
				SSID:      "Configure networks in /perm/wifi.json",
				Signal:    -100,
				Encrypted: false,
			},
		}, nil
	}

	// Return informative "networks" to show the limitation
	return []ScanResult{
		{
			SSID:      "⚠️ WiFi scanning not available in Gokrazy",
			Signal:    -100,
			Encrypted: false,
		},
		{
			SSID:      "Please configure networks manually",
			Signal:    -100,
			Encrypted: false,
		},
		{
			SSID:      "Add to /perm/wifi.json or extra-wifi.json",
			Signal:    -100,
			Encrypted: false,
		},
	}, nil
}

// getCurrentNetworkFromConfig tries to read the current network from gokrazy config
func (m *Manager) getCurrentNetworkFromConfig() string {
	// Try to read gokrazy's wifi.json
	type GokrazyWiFi struct {
		SSID string `json:"ssid"`
		PSK  string `json:"psk,omitempty"`
	}

	data, err := os.ReadFile("/perm/wifi.json")
	if err == nil {
		var config GokrazyWiFi
		if json.Unmarshal(data, &config) == nil && config.SSID != "" {
			return config.SSID
		}
	}

	// Also check our extra-wifi.json
	if len(m.networks) > 0 {
		return m.networks[0].SSID
	}

	return ""
}


// ScanResult represents a scanned WiFi network
type ScanResult struct {
	SSID      string `json:"ssid"`
	Signal    int    `json:"signal"` // Signal strength in dBm
	Encrypted bool   `json:"encrypted"`
}

// WiFiConfig represents the structure of the WiFi configuration file
type WiFiConfig struct {
	Networks []Network `json:"networks"`
}

// save persists the WiFi configuration to disk
func (m *Manager) save() error {
	config := WiFiConfig{
		Networks: m.networks,
	}

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal wifi config: %w", err)
	}

	// Atomic write
	tmpFile := WiFiConfigPath + ".tmp"
	if err := os.WriteFile(tmpFile, data, 0600); err != nil {
		return fmt.Errorf("failed to write wifi config: %w", err)
	}
	if err := os.Rename(tmpFile, WiFiConfigPath); err != nil {
		// Clean up temp file on rename failure
		os.Remove(tmpFile)
		return fmt.Errorf("failed to rename wifi config: %w", err)
	}

	return nil
}

// load reads the WiFi configuration from disk
func (m *Manager) load() error {
	data, err := os.ReadFile(WiFiConfigPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No config file yet
		}
		return fmt.Errorf("failed to read wifi config: %w", err)
	}

	var config WiFiConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("failed to unmarshal wifi config: %w", err)
	}

	m.networks = config.Networks
	if m.networks == nil {
		m.networks = make([]Network, 0)
	}

	return nil
}

// GetCurrentSSID returns the currently connected SSID by querying the WiFi interface
func (m *Manager) GetCurrentSSID() (string, error) {
	// Use the wifi library to get actual connection status
	cl, err := wifi.New()
	if err != nil {
		log.Printf("Failed to create WiFi client: %v", err)
		// Fallback to config file
		return m.getCurrentNetworkFromConfig(), fmt.Errorf("WiFi client unavailable")
	}
	defer cl.Close()

	interfaces, err := cl.Interfaces()
	if err != nil {
		log.Printf("Failed to get WiFi interfaces: %v", err)
		return m.getCurrentNetworkFromConfig(), fmt.Errorf("no WiFi interfaces")
	}

	if len(interfaces) == 0 {
		return m.getCurrentNetworkFromConfig(), fmt.Errorf("no WiFi interfaces found")
	}

	// Check each interface for connection
	for _, intf := range interfaces {
		stationInfos, err := cl.StationInfo(intf)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				// Not connected on this interface
				continue
			}
			log.Printf("Failed to get station info for %s: %v", intf.Name, err)
			continue
		}

		// Check if we have a valid connection
		for _, sta := range stationInfos {
			if !bytes.Equal(sta.HardwareAddr, net.HardwareAddr{}) {
				// We're connected! Now get the SSID
				// The sta object doesn't directly give us SSID, but we can get it from BSS info
				bss, err := cl.BSS(intf)
				if err == nil && bss != nil && bss.SSID != "" {
					log.Printf("Currently connected to SSID: %s (signal: %d dBm)", bss.SSID, sta.Signal)
					return bss.SSID, nil
				}
			}
		}
	}

	// Not connected to any network
	return "", fmt.Errorf("not connected to any WiFi network")
}
