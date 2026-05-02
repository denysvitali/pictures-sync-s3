package wifimanager

import (
	"fmt"
	"log"
	"sync"
)

// Network represents a WiFi network configuration
type Network struct {
	SSID string `json:"ssid"`
	PSK  string `json:"psk,omitempty"` // Password, optional for open networks
}

// WiFiManager is an interface for WiFi management operations
type WiFiManager interface {
	GetNetworks() []Network
	AddNetwork(ssid, password string) error
	RemoveNetwork(ssid string) error
	ReorderNetworks(orderedSSIDs []string) error
	ScanNetworks() ([]ScanResult, error)
	GetCurrentConnection() (*ConnectionInfo, error)
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

	// Ensure first-boot fallback AP-style network exists when no networks are configured.
	if err := m.EnsureFirstBootWiFiConfig(); err != nil {
		log.Printf("warning: failed to prepare first-boot Wi-Fi config: %v", err)
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

	// Validate password strength for WPA/WPA2 networks
	if password != "" {
		if err := ValidateWiFiPassword(password); err != nil {
			return err
		}
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

// ReorderNetworks updates the order of networks
func (m *Manager) ReorderNetworks(orderedSSIDs []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Validate that all SSIDs exist
	networkMap := make(map[string]Network)
	for _, net := range m.networks {
		networkMap[net.SSID] = net
	}

	// Check all SSIDs are valid
	if len(orderedSSIDs) != len(m.networks) {
		return fmt.Errorf("number of SSIDs doesn't match number of networks")
	}

	newNetworks := make([]Network, 0, len(orderedSSIDs))
	for _, ssid := range orderedSSIDs {
		net, exists := networkMap[ssid]
		if !exists {
			return fmt.Errorf("network not found: %s", ssid)
		}
		newNetworks = append(newNetworks, net)
	}

	m.networks = newNetworks
	return m.save()
}
