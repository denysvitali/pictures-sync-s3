package wifimanager

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
)

const (
	WiFiConfigPath = "/perm/wifi.json"
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
	// Try using iwlist
	cmd := exec.Command("iwlist", "wlan0", "scan")
	output, err := cmd.Output()
	if err != nil {
		// Fallback to iw
		cmd = exec.Command("iw", "dev", "wlan0", "scan")
		output, err = cmd.Output()
		if err != nil {
			return nil, fmt.Errorf("failed to scan networks: %w", err)
		}
		return m.parseIwScan(string(output)), nil
	}

	return m.parseIwlistScan(string(output)), nil
}

// ScanResult represents a scanned WiFi network
type ScanResult struct {
	SSID      string `json:"ssid"`
	Signal    int    `json:"signal"` // Signal strength in dBm or percentage
	Encrypted bool   `json:"encrypted"`
}

// parseIwlistScan parses output from iwlist
func (m *Manager) parseIwlistScan(output string) []ScanResult {
	results := make([]ScanResult, 0)
	lines := strings.Split(output, "\n")

	var current ScanResult
	for _, line := range lines {
		line = strings.TrimSpace(line)

		if strings.HasPrefix(line, "Cell ") {
			// New cell, save previous
			if current.SSID != "" {
				results = append(results, current)
			}
			current = ScanResult{}
		} else if strings.HasPrefix(line, "ESSID:") {
			ssid := strings.TrimPrefix(line, "ESSID:")
			ssid = strings.Trim(ssid, "\"")
			current.SSID = ssid
		} else if strings.Contains(line, "Quality=") || strings.Contains(line, "Signal level=") {
			// Parse signal strength
			if strings.Contains(line, "Signal level=") {
				parts := strings.Split(line, "Signal level=")
				if len(parts) > 1 {
					signalStr := strings.Fields(parts[1])[0]
					// Parse dBm or percentage
					current.Signal = parseSignal(signalStr)
				}
			}
		} else if strings.Contains(line, "Encryption key:on") {
			current.Encrypted = true
		}
	}

	// Add last network
	if current.SSID != "" {
		results = append(results, current)
	}

	return results
}

// parseIwScan parses output from iw
func (m *Manager) parseIwScan(output string) []ScanResult {
	results := make([]ScanResult, 0)
	lines := strings.Split(output, "\n")

	var current ScanResult
	for _, line := range lines {
		line = strings.TrimSpace(line)

		if strings.HasPrefix(line, "BSS ") {
			// New BSS, save previous
			if current.SSID != "" {
				results = append(results, current)
			}
			current = ScanResult{}
		} else if strings.HasPrefix(line, "SSID: ") {
			current.SSID = strings.TrimPrefix(line, "SSID: ")
		} else if strings.HasPrefix(line, "signal: ") {
			signalStr := strings.TrimPrefix(line, "signal: ")
			current.Signal = parseSignal(signalStr)
		} else if strings.Contains(line, "RSN:") || strings.Contains(line, "WPA:") {
			current.Encrypted = true
		}
	}

	// Add last network
	if current.SSID != "" {
		results = append(results, current)
	}

	return results
}

// parseSignal parses signal strength from various formats
func parseSignal(s string) int {
	s = strings.TrimSpace(s)

	// Remove units
	s = strings.TrimSuffix(s, " dBm")
	s = strings.TrimSuffix(s, "/100")

	// Try to parse as integer
	var val int
	fmt.Sscanf(s, "%d", &val)
	return val
}

// save persists the WiFi configuration to disk
func (m *Manager) save() error {
	data, err := json.MarshalIndent(m.networks, "", "  ")
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

	if err := json.Unmarshal(data, &m.networks); err != nil {
		return fmt.Errorf("failed to unmarshal wifi config: %w", err)
	}

	return nil
}

// GetCurrentSSID returns the currently connected SSID
func (m *Manager) GetCurrentSSID() (string, error) {
	// Try using iwgetid
	cmd := exec.Command("iwgetid", "-r")
	output, err := cmd.Output()
	if err == nil {
		return strings.TrimSpace(string(output)), nil
	}

	// Fallback to iw
	cmd = exec.Command("iw", "dev", "wlan0", "link")
	output, err = cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get current SSID: %w", err)
	}

	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if strings.Contains(line, "SSID:") {
			parts := strings.Split(line, "SSID:")
			if len(parts) > 1 {
				return strings.TrimSpace(parts[1]), nil
			}
		}
	}

	return "", fmt.Errorf("not connected to any network")
}
