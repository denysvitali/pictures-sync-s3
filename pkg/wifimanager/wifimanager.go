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

// ScanNetworks scans for available WiFi networks using the wifi library
func (m *Manager) ScanNetworks() ([]ScanResult, error) {
	log.Printf("Scanning for WiFi networks...")

	cl, err := wifi.New()
	if err != nil {
		log.Printf("Failed to create WiFi client for scanning: %v", err)
		return nil, fmt.Errorf("WiFi client unavailable: %w", err)
	}
	defer cl.Close()

	interfaces, err := cl.Interfaces()
	if err != nil {
		log.Printf("Failed to get WiFi interfaces: %v", err)
		return nil, fmt.Errorf("no WiFi interfaces: %w", err)
	}

	if len(interfaces) == 0 {
		return nil, fmt.Errorf("no WiFi interfaces found")
	}

	// Collect all unique networks from all interfaces
	networksMap := make(map[string]ScanResult)

	for _, intf := range interfaces {
		accessPoints, err := cl.AccessPoints(intf)
		if err != nil {
			log.Printf("Failed to scan on interface %s: %v", intf.Name, err)
			continue
		}

		log.Printf("Found %d access points on interface %s", len(accessPoints), intf.Name)

		for _, ap := range accessPoints {
			// Skip hidden networks
			if ap.SSID == "" {
				continue
			}

			// BSS doesn't contain signal strength from scan, so we'll use a placeholder
			// Real signal can only be obtained from StationInfo when connected
			result := ScanResult{
				SSID:      ap.SSID,
				Signal:    -50, // Placeholder - actual signal not available in scan results
				Encrypted: len(ap.RSN.PairwiseCiphers) > 0,
			}

			// Only add if we don't already have this SSID
			if _, exists := networksMap[ap.SSID]; !exists {
				networksMap[ap.SSID] = result
			}
		}
	}

	// Convert map to slice
	results := make([]ScanResult, 0, len(networksMap))
	for _, result := range networksMap {
		results = append(results, result)
	}

	// Sort by signal strength (strongest first)
	// Simple bubble sort since the list is typically small
	for i := 0; i < len(results)-1; i++ {
		for j := 0; j < len(results)-i-1; j++ {
			if results[j].Signal < results[j+1].Signal {
				results[j], results[j+1] = results[j+1], results[j]
			}
		}
	}

	log.Printf("Scan complete: found %d unique networks", len(results))
	return results, nil
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

// ConnectionInfo represents the current WiFi connection details
type ConnectionInfo struct {
	SSID   string
	Signal int // Signal strength in dBm
}

// GetCurrentConnection returns the currently connected network with signal strength
func (m *Manager) GetCurrentConnection() (*ConnectionInfo, error) {
	// Use the wifi library to get actual connection status
	cl, err := wifi.New()
	if err != nil {
		log.Printf("Failed to create WiFi client: %v", err)
		// Fallback to config file
		ssid := m.getCurrentNetworkFromConfig()
		if ssid != "" {
			return &ConnectionInfo{SSID: ssid, Signal: 0}, fmt.Errorf("WiFi client unavailable")
		}
		return nil, fmt.Errorf("WiFi client unavailable")
	}
	defer cl.Close()

	interfaces, err := cl.Interfaces()
	if err != nil {
		log.Printf("Failed to get WiFi interfaces: %v", err)
		return nil, fmt.Errorf("no WiFi interfaces")
	}

	if len(interfaces) == 0 {
		return nil, fmt.Errorf("no WiFi interfaces found")
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
				bss, err := cl.BSS(intf)
				if err == nil && bss != nil && bss.SSID != "" {
					log.Printf("Currently connected to SSID: %s (signal: %d dBm)", bss.SSID, sta.Signal)
					return &ConnectionInfo{
						SSID:   bss.SSID,
						Signal: sta.Signal,
					}, nil
				}
			}
		}
	}

	// Not connected to any network
	return nil, fmt.Errorf("not connected to any WiFi network")
}

// GetCurrentSSID returns the currently connected SSID (for backward compatibility)
func (m *Manager) GetCurrentSSID() (string, error) {
	conn, err := m.GetCurrentConnection()
	if err != nil {
		return "", err
	}
	return conn.SSID, nil
}
