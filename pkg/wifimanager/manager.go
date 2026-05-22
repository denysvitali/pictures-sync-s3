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
	SetPrefer5GHzNetworks(prefer bool)
	ScanNetworks() ([]ScanResult, error)
	GetCurrentConnection() (*ConnectionInfo, error)
}

// Manager manages WiFi configuration
type Manager struct {
	mu                 sync.RWMutex
	networks           []Network
	prefer5GHzNetworks bool
}

// NewManager creates a new WiFi manager
func NewManager() (*Manager, error) {
	m := &Manager{
		networks:           make([]Network, 0),
		prefer5GHzNetworks: true,
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

// SetPrefer5GHzNetworks sets whether scans should prefer 5 GHz APs for duplicate SSIDs.
func (m *Manager) SetPrefer5GHzNetworks(prefer bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.prefer5GHzNetworks = prefer
}

func (m *Manager) getPrefer5GHzNetworks() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.prefer5GHzNetworks
}

// AddNetwork adds a new WiFi network
func (m *Manager) AddNetwork(ssid, password string) error {
	if ssid == "" {
		return fmt.Errorf("SSID cannot be empty")
	}

	// Validate SSID length (WiFi spec: max 32 bytes)
	if len(ssid) > 32 {
		return fmt.Errorf("SSID exceeds maximum length of 32 bytes")
	}

	// Validate password strength for WPA/WPA2 networks
	if password != "" {
		if err := ValidateWiFiPassword(password); err != nil {
			return err
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Snapshot existing networks so we can roll back if persistence fails.
	// Without rollback, a failed save (disk full, /perm read-only, etc.)
	// leaves the new credentials only in memory; GetNetworks() would return
	// a PSK that was never persisted, masking the failure and risking that
	// the secret is later treated as authoritative by callers.
	previous := m.cloneNetworks()

	// Check if network already exists
	for i, net := range m.networks {
		if net.SSID == ssid {
			// Update password
			m.networks[i].PSK = password
			if err := m.save(); err != nil {
				m.networks = previous
				return err
			}
			return nil
		}
	}

	// Add new network as the active gokrazy client profile.
	network := Network{
		SSID: ssid,
		PSK:  password,
	}
	m.networks = append([]Network{network}, m.networks...)

	if err := m.save(); err != nil {
		m.networks = previous
		return err
	}
	return nil
}

// RemoveNetwork removes a WiFi network by SSID
func (m *Manager) RemoveNetwork(ssid string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	previous := m.cloneNetworks()

	// Find and remove network
	for i, net := range m.networks {
		if net.SSID == ssid {
			m.networks = append(m.networks[:i], m.networks[i+1:]...)
			if err := m.save(); err != nil {
				m.networks = previous
				return err
			}
			return nil
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

	previous := m.networks
	m.networks = newNetworks
	if err := m.save(); err != nil {
		m.networks = previous
		return err
	}
	return nil
}

// cloneNetworks returns a deep copy of the configured networks. Callers must
// hold m.mu. This is used to snapshot state before mutating m.networks so we
// can roll back on persistence failures.
func (m *Manager) cloneNetworks() []Network {
	out := make([]Network, len(m.networks))
	copy(out, m.networks)
	return out
}
