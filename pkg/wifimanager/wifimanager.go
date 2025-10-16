// Package wifimanager manages WiFi network configuration for the Gokrazy appliance.
//
// The package is organized into the following files:
//   - manager.go: Core Manager struct with network management operations (Add, Remove, Get)
//   - scanner.go: WiFi network scanning using nl80211 and connection status
//   - config.go: Configuration file persistence and atomic writes
//
// The Manager provides thread-safe operations for:
//   - Adding and removing WiFi networks
//   - Scanning for available networks
//   - Getting current connection status
//   - Persisting configuration to /perm/extra-wifi.json
//
// Usage:
//
//	mgr, err := wifimanager.NewManager()
//	if err != nil {
//		log.Fatal(err)
//	}
//
//	// Add a network
//	err = mgr.AddNetwork("MyWiFi", "password123")
//
//	// Scan for networks
//	networks, err := mgr.ScanNetworks()
//
//	// Get current connection
//	conn, err := mgr.GetCurrentConnection()
//
// The package integrates with the gokrazy/wifi package for actual network connectivity.
package wifimanager
