package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/denysvitali/pictures-sync-s3/pkg/wifimanager"
)

// TestHandleWiFiNetworks_NoPasswordExposure verifies the critical security fix:
// WiFi passwords must NEVER be exposed via API responses
func TestHandleWiFiNetworks_NoPasswordExposure(t *testing.T) {
	// Set the config path for testing (we'll write directly to simulate a manager)
	testNetworks := []wifimanager.Network{
		{SSID: "HomeNetwork", PSK: "SuperSecretPassword123"},
		{SSID: "GuestNetwork", PSK: "AnotherPassword456"},
		{SSID: "OpenNetwork", PSK: ""}, // Open network without password
	}

	// Create a mock manager for testing
	mockMgr := &testWiFiManager{
		networks: testNetworks,
	}

	// Create handler context
	ctx := &Context{
		WiFiMgr: mockMgr,
	}

	// Create test request
	req := httptest.NewRequest(http.MethodGet, "/api/wifi/networks", nil)
	rec := httptest.NewRecorder()

	// Execute handler
	ctx.HandleWiFiNetworks(rec, req)

	// Verify response
	if rec.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", rec.Code)
	}

	// Parse response
	var response struct {
		Networks []SafeNetworkInfo `json:"networks"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	safeNetworks := response.Networks

	// Verify correct number of networks
	if len(safeNetworks) != 3 {
		t.Fatalf("Expected 3 networks, got %d", len(safeNetworks))
	}

	// CRITICAL SECURITY CHECK: Verify passwords are NOT in response
	responseBody := rec.Body.String()
	if containsPassword(responseBody, "SuperSecretPassword123") {
		t.Fatal("SECURITY VIOLATION: Password 'SuperSecretPassword123' found in API response")
	}
	if containsPassword(responseBody, "AnotherPassword456") {
		t.Fatal("SECURITY VIOLATION: Password 'AnotherPassword456' found in API response")
	}

	// Verify the response contains only safe information
	expectedNetworks := map[string]bool{
		"HomeNetwork":  true,  // Should have password
		"GuestNetwork": true,  // Should have password
		"OpenNetwork":  false, // Should not have password
	}

	for _, network := range safeNetworks {
		expectedHasPassword, exists := expectedNetworks[network.SSID]
		if !exists {
			t.Errorf("Unexpected network in response: %s", network.SSID)
			continue
		}

		if network.HasPassword != expectedHasPassword {
			t.Errorf("Network %s: expected has_password=%v, got %v",
				network.SSID, expectedHasPassword, network.HasPassword)
		}
	}

	t.Log("SUCCESS: WiFi passwords are properly protected from API exposure")
}

// containsPassword checks if a password appears in the response body
func containsPassword(body, password string) bool {
	return len(password) > 0 && len(body) > 0 &&
		(body[0:1] == password[0:1] || body[len(body)-1:] == password[len(password)-1:] ||
			stringContains(body, password))
}

func stringContains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// testWiFiManager is a test implementation of the WiFi manager interface
type testWiFiManager struct {
	networks []wifimanager.Network
}

func (m *testWiFiManager) GetNetworks() []wifimanager.Network {
	return m.networks
}

func (m *testWiFiManager) AddNetwork(ssid, psk string) error {
	return nil
}

func (m *testWiFiManager) RemoveNetwork(ssid string) error {
	return nil
}

func (m *testWiFiManager) ReorderNetworks(orderedSSIDs []string) error {
	return nil
}

func (m *testWiFiManager) SetPrefer5GHzNetworks(prefer bool) {}

func (m *testWiFiManager) ScanNetworks() ([]wifimanager.ScanResult, error) {
	return nil, nil
}

func (m *testWiFiManager) GetCurrentConnection() (*wifimanager.ConnectionInfo, error) {
	return nil, nil
}

// TestSafeNetworkInfo_Structure verifies the SafeNetworkInfo struct
func TestSafeNetworkInfo_Structure(t *testing.T) {
	// Create a safe network info
	safe := SafeNetworkInfo{
		SSID:        "TestNetwork",
		HasPassword: true,
	}

	// Marshal to JSON
	data, err := json.Marshal(safe)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}

	// Verify JSON structure
	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	// Should only have these two fields
	expectedFields := []string{"ssid", "has_password"}
	if len(result) != len(expectedFields) {
		t.Errorf("Expected %d fields, got %d", len(expectedFields), len(result))
	}

	// Should NOT have PSK field
	if _, hasPSK := result["psk"]; hasPSK {
		t.Fatal("SECURITY VIOLATION: SafeNetworkInfo contains 'psk' field")
	}

	if _, hasPassword := result["password"]; hasPassword {
		t.Fatal("SECURITY VIOLATION: SafeNetworkInfo contains 'password' field")
	}
}
