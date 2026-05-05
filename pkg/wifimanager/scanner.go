package wifimanager

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"time"

	"github.com/mdlayher/wifi"
)

const (
	scanTimeout = 10 * time.Second
)

var (
	// scanWaitDelay is the wait time after triggering a WiFi scan before attempting to read results.
	// This allows the wireless driver and kernel to complete the scan operation and populate
	// the access point list.
	scanWaitDelay = 2 * time.Second
)

// ScanResult represents a scanned WiFi network
type ScanResult struct {
	SSID      string `json:"ssid"`
	Signal    int    `json:"signal"` // Signal strength in dBm
	Encrypted bool   `json:"encrypted"`
}

// ConnectionInfo represents the current WiFi connection details
type ConnectionInfo struct {
	SSID   string
	Signal int // Signal strength in dBm
}

// ScanNetworks scans for available WiFi networks using the wifi library
func (m *Manager) ScanNetworks() ([]ScanResult, error) {
	log.Printf("Starting WiFi network scan...")

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

	log.Printf("Found %d WiFi interface(s)", len(interfaces))
	for i, intf := range interfaces {
		log.Printf("  Interface %d: %s (Type: %s)", i+1, intf.Name, intf.Type.String())
	}

	if len(interfaces) == 0 {
		return nil, fmt.Errorf("no WiFi interfaces found")
	}

	// Collect all unique networks from all interfaces
	networksMap := make(map[string]ScanResult)
	totalAPsFound := 0
	hiddenCount := 0
	skippedCount := 0

	for intfIndex, intf := range interfaces {
		log.Printf("=== Processing interface %d/%d: %s ===", intfIndex+1, len(interfaces), intf.Name)

		accessPoints, err := scanInterface(cl, intf)
		if err != nil {
			log.Printf("ERROR: Failed to scan interface %s: %v", intf.Name, err)
			continue
		}

		log.Printf("Raw scan results from %s: %d access point(s)", intf.Name, len(accessPoints))
		totalAPsFound += len(accessPoints)

		// Process access points
		processed, hidden, skipped := processAccessPoints(accessPoints, networksMap)
		hiddenCount += hidden
		skippedCount += skipped

		log.Printf("=== Interface %s complete: %d APs processed, %d added ===",
			intf.Name, len(accessPoints), processed)
	}

	// Convert map to slice
	results := make([]ScanResult, 0, len(networksMap))
	for ssid, result := range networksMap {
		results = append(results, result)
		log.Printf("Final result: SSID='%s', encrypted=%v", ssid, result.Encrypted)
	}

	log.Printf("=== SCAN SUMMARY ===")
	log.Printf("Total APs found: %d", totalAPsFound)
	log.Printf("Hidden networks: %d", hiddenCount)
	log.Printf("Duplicate SSIDs skipped: %d", skippedCount)
	log.Printf("Unique networks returned: %d", len(results))
	log.Printf("====================")

	return results, nil
}

type scanClient interface {
	AccessPoints(ifi *wifi.Interface) ([]*wifi.BSS, error)
	Scan(ctx context.Context, ifi *wifi.Interface) error
}

// scanInterface scans a single WiFi interface for access points.
func scanInterface(cl scanClient, intf *wifi.Interface) ([]*wifi.BSS, error) {
	log.Printf("Triggering WiFi scan on interface %s...", intf.Name)
	ctx, cancel := context.WithTimeout(context.Background(), scanTimeout)
	defer cancel()

	if err := cl.Scan(ctx, intf); err != nil {
		log.Printf("ERROR: Failed to trigger scan on interface %s: %v", intf.Name, err)
		log.Printf("Falling back to cached access points from interface %s...", intf.Name)
		accessPoints, accessErr := cl.AccessPoints(intf)
		if accessErr != nil {
			log.Printf("ERROR: Failed to get cached access points on interface %s: %v", intf.Name, accessErr)
			return nil, fmt.Errorf("scan failed: %w; cached access points unavailable: %w", err, accessErr)
		}
		return accessPoints, nil
	}

	log.Printf("Scan completed on interface %s, reading access points...", intf.Name)
	if scanWaitDelay > 0 {
		time.Sleep(scanWaitDelay)
	}

	accessPoints, err := cl.AccessPoints(intf)
	if err != nil {
		log.Printf("ERROR: Failed to get access points after scan on interface %s: %v", intf.Name, err)
		return nil, fmt.Errorf("access points unavailable after scan: %w", err)
	}

	return accessPoints, nil
}

// processAccessPoints processes a list of access points and adds unique ones to the map
// Returns: number processed, number hidden, number skipped as duplicates
func processAccessPoints(accessPoints []*wifi.BSS, networksMap map[string]ScanResult) (int, int, int) {
	processed := 0
	hiddenCount := 0
	skippedCount := 0

	for apIndex, ap := range accessPoints {
		log.Printf("  AP %d: BSSID=%s, SSID='%s', Freq=%dMHz",
			apIndex+1, ap.BSSID.String(), ap.SSID, ap.Frequency)

		// Skip hidden networks
		if ap.SSID == "" {
			log.Printf("    → Skipping hidden network (empty SSID)")
			hiddenCount++
			continue
		}

		// Check if network is encrypted
		encrypted := len(ap.RSN.PairwiseCiphers) > 0
		log.Printf("    → Encryption: %v (RSN ciphers: %d)", encrypted, len(ap.RSN.PairwiseCiphers))

		result := ScanResult{
			SSID:      ap.SSID,
			Signal:    0, // No signal data available in BSS scan results
			Encrypted: encrypted,
		}

		// Check for duplicate SSIDs
		key := ap.SSID
		if existing, exists := networksMap[key]; exists {
			log.Printf("    → DUPLICATE SSID '%s' (keeping first, BSSID was %s)",
				ap.SSID, ap.BSSID.String())
			_ = existing // Keep the first one found
			skippedCount++
		} else {
			networksMap[key] = result
			log.Printf("    → ADDED unique network: '%s' (encrypted: %v)", ap.SSID, encrypted)
			processed++
		}
	}

	return processed, hiddenCount, skippedCount
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
		info, err := getInterfaceConnection(cl, intf)
		if err == nil && info != nil {
			return info, nil
		}
	}

	// Not connected to any network
	return nil, fmt.Errorf("not connected to any WiFi network")
}

// getInterfaceConnection checks if a specific interface is connected and returns connection info
func getInterfaceConnection(cl *wifi.Client, intf *wifi.Interface) (*ConnectionInfo, error) {
	stationInfos, err := cl.StationInfo(intf)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// Not connected on this interface
			return nil, err
		}
		log.Printf("Failed to get station info for %s: %v", intf.Name, err)
		return nil, err
	}

	// Check if we have a valid connection
	for _, sta := range stationInfos {
		if !bytes.Equal(sta.HardwareAddr, net.HardwareAddr{}) {
			// We're connected! Now get the SSID
			bss, err := cl.BSS(intf)
			if err == nil && bss != nil && bss.SSID != "" {
				// Connection info is already visible in the UI, no need to log
				return &ConnectionInfo{
					SSID:   bss.SSID,
					Signal: sta.Signal,
				}, nil
			}
		}
	}

	return nil, fmt.Errorf("no active connection")
}

// GetCurrentSSID returns the currently connected SSID (for backward compatibility)
func (m *Manager) GetCurrentSSID() (string, error) {
	conn, err := m.GetCurrentConnection()
	if err != nil {
		return "", err
	}
	return conn.SSID, nil
}

// getCurrentNetworkFromConfig tries to read the current network from gokrazy config
func (m *Manager) getCurrentNetworkFromConfig() string {
	// Try to read gokrazy's wifi.json
	data, err := os.ReadFile("/perm/wifi.json")
	if err == nil {
		var networks []struct {
			SSID string `json:"ssid"`
			PSK  string `json:"psk,omitempty"`
		}

		if err := json.Unmarshal(data, &networks); err == nil && len(networks) > 0 {
			return networks[0].SSID
		}
	}

	// Also check our extra-wifi.json
	if len(m.networks) > 0 {
		return m.networks[0].SSID
	}

	return ""
}
