package wifimanager

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

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
	// Open WiFi client
	client, err := wifi.New()
	if err != nil {
		return nil, fmt.Errorf("failed to create wifi client: %w", err)
	}
	defer client.Close()

	// Get interfaces
	interfaces, err := client.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("failed to get wifi interfaces: %w", err)
	}

	if len(interfaces) == 0 {
		return nil, fmt.Errorf("no wifi interfaces found")
	}

	// Use the first interface (usually wlan0)
	iface := interfaces[0]

	// Trigger an actual WiFi scan with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := client.Scan(ctx, iface); err != nil {
		return nil, fmt.Errorf("failed to scan for networks: %w", err)
	}

	// Get access points (available networks)
	bssList, err := client.AccessPoints(iface)
	if err != nil {
		return nil, fmt.Errorf("failed to get access points: %w", err)
	}

	// Convert to ScanResult
	results := make([]ScanResult, 0, len(bssList))
	seen := make(map[string]bool) // Deduplicate by SSID

	for _, bss := range bssList {
		// Skip hidden networks or duplicates
		if bss.SSID == "" || seen[bss.SSID] {
			continue
		}
		seen[bss.SSID] = true

		// Estimate signal strength from LastSeen (rough approximation)
		// Lower LastSeen typically means stronger/more recent signal
		// Convert to approximate dBm range: -30 (excellent) to -90 (weak)
		signalEstimate := -50 // Default to medium signal
		if bss.LastSeen.Seconds() < 1 {
			signalEstimate = -40 // Very recent = strong
		} else if bss.LastSeen.Seconds() < 5 {
			signalEstimate = -50 // Recent = medium
		} else if bss.LastSeen.Seconds() < 10 {
			signalEstimate = -65 // Older = weak
		} else {
			signalEstimate = -80 // Very old = very weak
		}

		// Check if network is encrypted by looking at RSN info
		encrypted := len(bss.RSN.PairwiseCiphers) > 0 || bss.RSN.GroupCipher != 0

		result := ScanResult{
			SSID:      bss.SSID,
			Signal:    signalEstimate,
			Encrypted: encrypted,
		}
		results = append(results, result)
	}

	return results, nil
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

// GetCurrentSSID returns the currently connected SSID
func (m *Manager) GetCurrentSSID() (string, error) {
	// Open WiFi client
	client, err := wifi.New()
	if err != nil {
		return "", fmt.Errorf("failed to create wifi client: %w", err)
	}
	defer client.Close()

	// Get interfaces
	interfaces, err := client.Interfaces()
	if err != nil {
		return "", fmt.Errorf("failed to get wifi interfaces: %w", err)
	}

	if len(interfaces) == 0 {
		return "", fmt.Errorf("no wifi interfaces found")
	}

	// Check the first interface for connection status
	iface := interfaces[0]

	// Get the BSS (Basic Service Set) - the currently connected network
	bss, err := client.BSS(iface)
	if err != nil {
		return "", fmt.Errorf("not connected to any network")
	}

	if bss.SSID == "" {
		return "", fmt.Errorf("not connected to any network")
	}

	return bss.SSID, nil
}
