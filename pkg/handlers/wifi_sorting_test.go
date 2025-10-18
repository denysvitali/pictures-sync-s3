package handlers

import (
	"reflect"
	"testing"

	"github.com/denysvitali/pictures-sync-s3/pkg/wifimanager"
)

func TestSortWiFiNetworks(t *testing.T) {
	// Create test networks
	networks := []wifimanager.ScanResult{
		{SSID: "HomeNetwork", Signal: -45, Encrypted: true},
		{SSID: "CoffeeShop", Signal: -70, Encrypted: false},
		{SSID: "OfficeWiFi", Signal: -55, Encrypted: true},
		{SSID: "GuestNetwork", Signal: -80, Encrypted: false},
		{SSID: "TestNetwork", Signal: -60, Encrypted: true},
	}

	tests := []struct {
		name     string
		sortBy   string
		expected []string // Expected order of SSIDs
	}{
		{
			name:     "Sort by signal strength",
			sortBy:   "signal",
			expected: []string{"HomeNetwork", "OfficeWiFi", "TestNetwork", "CoffeeShop", "GuestNetwork"},
		},
		{
			name:     "Sort by name",
			sortBy:   "name",
			expected: []string{"CoffeeShop", "GuestNetwork", "HomeNetwork", "OfficeWiFi", "TestNetwork"},
		},
		{
			name:     "Sort by SSID",
			sortBy:   "ssid",
			expected: []string{"CoffeeShop", "GuestNetwork", "HomeNetwork", "OfficeWiFi", "TestNetwork"},
		},
		{
			name:     "Sort by security",
			sortBy:   "security",
			expected: []string{"HomeNetwork", "OfficeWiFi", "TestNetwork", "CoffeeShop", "GuestNetwork"},
		},
		{
			name:     "Default sort (signal)",
			sortBy:   "",
			expected: []string{"HomeNetwork", "OfficeWiFi", "TestNetwork", "CoffeeShop", "GuestNetwork"},
		},
		{
			name:     "Invalid sort option (falls back to signal)",
			sortBy:   "invalid",
			expected: []string{"HomeNetwork", "OfficeWiFi", "TestNetwork", "CoffeeShop", "GuestNetwork"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sorted := sortWiFiNetworks(networks, tt.sortBy)

			// Extract SSIDs from sorted results
			var resultSSIDs []string
			for _, network := range sorted {
				resultSSIDs = append(resultSSIDs, network.SSID)
			}

			if !reflect.DeepEqual(resultSSIDs, tt.expected) {
				t.Errorf("sortWiFiNetworks(%s) = %v, want %v", tt.sortBy, resultSSIDs, tt.expected)
			}

			// Verify original slice wasn't modified
			if len(networks) != 5 || networks[0].SSID != "HomeNetwork" {
				t.Error("Original slice was modified")
			}
		})
	}
}

func TestSortWiFiNetworksEdgeCases(t *testing.T) {
	t.Run("Empty slice", func(t *testing.T) {
		networks := []wifimanager.ScanResult{}
		sorted := sortWiFiNetworks(networks, "signal")
		if len(sorted) != 0 {
			t.Errorf("Expected empty slice, got %v", sorted)
		}
	})

	t.Run("Single network", func(t *testing.T) {
		networks := []wifimanager.ScanResult{
			{SSID: "OnlyNetwork", Signal: -50, Encrypted: true},
		}
		sorted := sortWiFiNetworks(networks, "signal")
		if len(sorted) != 1 || sorted[0].SSID != "OnlyNetwork" {
			t.Errorf("Expected single network unchanged, got %v", sorted)
		}
	})

	t.Run("Networks with same signal strength", func(t *testing.T) {
		networks := []wifimanager.ScanResult{
			{SSID: "Network_B", Signal: -50, Encrypted: true},
			{SSID: "Network_A", Signal: -50, Encrypted: false},
		}
		sorted := sortWiFiNetworks(networks, "name")
		expected := []string{"Network_A", "Network_B"}

		var resultSSIDs []string
		for _, network := range sorted {
			resultSSIDs = append(resultSSIDs, network.SSID)
		}

		if !reflect.DeepEqual(resultSSIDs, expected) {
			t.Errorf("Expected %v, got %v", expected, resultSSIDs)
		}
	})

	t.Run("Security sort with mixed security levels", func(t *testing.T) {
		networks := []wifimanager.ScanResult{
			{SSID: "Open_Weak", Signal: -80, Encrypted: false},
			{SSID: "Secure_Strong", Signal: -40, Encrypted: true},
			{SSID: "Open_Strong", Signal: -45, Encrypted: false},
			{SSID: "Secure_Weak", Signal: -75, Encrypted: true},
		}
		sorted := sortWiFiNetworks(networks, "security")

		// Should be: encrypted networks first (sorted by signal), then open (sorted by signal)
		expected := []string{"Secure_Strong", "Secure_Weak", "Open_Strong", "Open_Weak"}

		var resultSSIDs []string
		for _, network := range sorted {
			resultSSIDs = append(resultSSIDs, network.SSID)
		}

		if !reflect.DeepEqual(resultSSIDs, expected) {
			t.Errorf("Expected %v, got %v", expected, resultSSIDs)
		}
	})
}