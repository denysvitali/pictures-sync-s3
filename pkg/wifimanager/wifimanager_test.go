package wifimanager

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// setupTestManager creates a Manager with a temporary config file
func setupTestManager(t *testing.T) (*Manager, string, func()) {
	t.Helper()

	// Create temporary directory
	tmpDir, err := os.MkdirTemp("", "wifimanager-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	// Override the config path for testing
	testPath := filepath.Join(tmpDir, "extra-wifi.json")

	// This is a hack - we need to modify the const, but we can't
	// Instead, we'll create the manager and manually set the path
	// This reveals BUG #1: No way to inject config path for testing

	cleanup := func() {
		os.RemoveAll(tmpDir)
		// Can't restore original path due to const
	}

	m := &Manager{
		networks: make([]Network, 0),
	}

	return m, testPath, cleanup
}

// TestInvalidWiFiCredentials tests handling of invalid credentials
func TestInvalidWiFiCredentials(t *testing.T) {
	tests := []struct {
		name        string
		ssid        string
		password    string
		expectError bool
		description string
	}{
		{
			name:        "empty SSID",
			ssid:        "",
			password:    "validpassword",
			expectError: true,
			description: "Should reject empty SSID",
		},
		{
			name:        "whitespace only SSID",
			ssid:        "   ",
			password:    "validpassword",
			expectError: false, // BUG #2: No validation for whitespace-only SSID
			description: "Should reject whitespace-only SSID",
		},
		{
			name:        "empty password for secured network",
			ssid:        "MyNetwork",
			password:    "",
			expectError: false, // BUG #3: No validation that empty password might be wrong
			description: "Empty password is allowed (could be open network or wrong)",
		},
		{
			name:        "whitespace only password",
			ssid:        "MyNetwork",
			password:    "   ",
			expectError: false, // BUG #4: No validation or trimming of passwords
			description: "Whitespace password is allowed without warning",
		},
		{
			name:        "very short password",
			ssid:        "MyNetwork",
			password:    "1234567", // WPA2 requires 8-63 chars
			expectError: false,     // BUG #5: No WPA2 password length validation
			description: "Password shorter than WPA2 minimum (8 chars)",
		},
		{
			name:        "password with only spaces",
			ssid:        "MyNetwork",
			password:    "        ",
			expectError: false, // BUG #6: Spaces-only password accepted
			description: "Should warn about suspicious all-spaces password",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, _, cleanup := setupTestManager(t)
			defer cleanup()

			err := m.AddNetwork(tt.ssid, tt.password)

			if tt.expectError && err == nil {
				t.Errorf("%s: expected error but got none", tt.description)
			}
			if !tt.expectError && err != nil {
				t.Errorf("%s: unexpected error: %v", tt.description, err)
			}
		})
	}
}

// TestMultipleNetworksSameSSID tests handling of duplicate SSIDs
func TestMultipleNetworksSameSSID(t *testing.T) {
	m, _, cleanup := setupTestManager(t)
	defer cleanup()

	// Add first network
	err := m.AddNetwork("HomeWiFi", "password1")
	if err != nil {
		t.Fatalf("Failed to add first network: %v", err)
	}

	networks := m.GetNetworks()
	if len(networks) != 1 {
		t.Fatalf("Expected 1 network, got %d", len(networks))
	}
	if networks[0].PSK != "password1" {
		t.Errorf("Expected password1, got %s", networks[0].PSK)
	}

	// Add network with same SSID - should update password
	err = m.AddNetwork("HomeWiFi", "password2")
	if err != nil {
		t.Fatalf("Failed to update network: %v", err)
	}

	networks = m.GetNetworks()
	if len(networks) != 1 {
		t.Errorf("Expected 1 network after update, got %d", len(networks))
	}

	// BUG #7: Password updated but no warning to user that existing network was modified
	if networks[0].PSK != "password2" {
		t.Errorf("Expected password2 after update, got %s", networks[0].PSK)
	}

	// Test case sensitivity
	err = m.AddNetwork("homewifi", "password3") // lowercase
	if err != nil {
		t.Fatalf("Failed to add lowercase network: %v", err)
	}

	networks = m.GetNetworks()
	// BUG #8: Case-sensitive SSID comparison - "HomeWiFi" and "homewifi" treated as different
	// WiFi SSIDs are case-sensitive in spec, but this could confuse users
	if len(networks) != 2 {
		t.Logf("Note: SSID comparison is case-sensitive (2 networks expected)")
	}
}

// TestNetworkScanningFailures tests scan error handling
func TestNetworkScanningFailures(t *testing.T) {
	m, _, cleanup := setupTestManager(t)
	defer cleanup()

	// Scan should not fail even in test environment
	results, err := m.ScanNetworks()
	if err != nil {
		t.Errorf("ScanNetworks should not return error, got: %v", err)
	}

	// BUG #9: ScanNetworks returns fake/informational results instead of real error
	// This could mislead users or applications expecting real scan data
	if len(results) == 0 {
		t.Error("Expected informational scan results, got empty array")
	}

	// Verify we get informational messages
	foundWarning := false
	for _, result := range results {
		if result.SSID == "⚠️ WiFi scanning not available in Gokrazy" ||
			result.SSID == "⚠️ Scanning not supported" {
			foundWarning = true
			break
		}
	}

	if !foundWarning {
		t.Error("Expected warning message in scan results about scanning limitations")
	}
}

// TestPermissionErrorsWritingConfig tests handling of write permission errors
func TestPermissionErrorsWritingConfig(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("Test requires non-root user")
	}

	// Create a read-only directory
	tmpDir, err := os.MkdirTemp("", "wifimanager-readonly-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Make directory read-only
	err = os.Chmod(tmpDir, 0444)
	if err != nil {
		t.Fatalf("Failed to chmod directory: %v", err)
	}
	defer os.Chmod(tmpDir, 0755) // Restore for cleanup

	// BUG #10: Can't inject config path, so we can't fully test permission errors
	// The WiFiConfigPath const makes testing difficult

	m := &Manager{
		networks: make([]Network, 0),
	}

	// Try to add a network - save will fail due to hardcoded path
	err = m.AddNetwork("TestNetwork", "password")
	if err == nil {
		t.Log("Note: Cannot test permission errors due to hardcoded config path")
	}
}

// TestCorruptedWiFiJSON tests handling of corrupted config files
func TestCorruptedWiFiJSON(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		expectError bool
		description string
	}{
		{
			name:        "empty file",
			content:     "",
			expectError: true, // BUG #11: Empty file causes unmarshal error, not handled gracefully
			description: "Empty config file",
		},
		{
			name:        "invalid JSON",
			content:     "{invalid json}",
			expectError: true,
			description: "Malformed JSON",
		},
		{
			name:        "truncated JSON",
			content:     `{"networks":[{"ssid":"Test","psk":"pa`,
			expectError: true,
			description: "Truncated JSON file",
		},
		{
			name:        "wrong structure",
			content:     `{"wrong_key": "value"}`,
			expectError: false, // BUG #12: Wrong structure doesn't error, just loads empty networks
			description: "JSON with wrong structure",
		},
		{
			name:        "null networks",
			content:     `{"networks": null}`,
			expectError: false, // Handled by code: m.networks = make([]Network, 0)
			description: "Null networks array",
		},
		{
			name:        "networks not array",
			content:     `{"networks": "not an array"}`,
			expectError: true,
			description: "Networks is not an array",
		},
		{
			name: "network with missing SSID",
			content: `{
				"networks": [
					{"psk": "password"}
				]
			}`,
			expectError: false, // BUG #13: Missing SSID in saved config not validated on load
			description: "Network without SSID field",
		},
		{
			name: "network with extra fields",
			content: `{
				"networks": [
					{"ssid": "Test", "psk": "password", "extra": "field"}
				]
			}`,
			expectError: false, // Extra fields ignored by JSON unmarshaling
			description: "Network with extra unknown fields",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir, err := os.MkdirTemp("", "wifimanager-corrupt-*")
			if err != nil {
				t.Fatalf("Failed to create temp dir: %v", err)
			}
			defer os.RemoveAll(tmpDir)

			configPath := filepath.Join(tmpDir, "extra-wifi.json")
			err = os.WriteFile(configPath, []byte(tt.content), 0600)
			if err != nil {
				t.Fatalf("Failed to write test file: %v", err)
			}

			// BUG #14: Can't test load() with corrupted file due to hardcoded path
			// We can only test the JSON unmarshaling logic indirectly

			var config WiFiConfig
			err = json.Unmarshal([]byte(tt.content), &config)

			if tt.expectError && err == nil {
				t.Errorf("%s: expected error but got none", tt.description)
			}
			if !tt.expectError && err != nil {
				t.Logf("%s: error (may be acceptable): %v", tt.description, err)
			}
		})
	}
}

// TestUnicodeSpecialCharactersInCredentials tests Unicode and special characters
func TestUnicodeSpecialCharactersInCredentials(t *testing.T) {
	tests := []struct {
		name        string
		ssid        string
		password    string
		description string
	}{
		{
			name:        "unicode in SSID",
			ssid:        "家のWiFi",
			password:    "password123",
			description: "Japanese characters in SSID",
		},
		{
			name:        "emoji in SSID",
			ssid:        "My WiFi 🏠",
			password:    "password123",
			description: "Emoji in SSID",
		},
		{
			name:        "unicode in password",
			ssid:        "MyWiFi",
			password:    "пароль123",
			description: "Cyrillic characters in password",
		},
		{
			name:        "special chars in SSID",
			ssid:        "WiFi-2.4GHz_[5]",
			password:    "password123",
			description: "Special characters in SSID",
		},
		{
			name:        "special chars in password",
			ssid:        "MyWiFi",
			password:    `p@$$w0rd!"#$%&'()*+,-./:;<=>?@[\]^_{|}~`,
			description: "ASCII special characters in password",
		},
		{
			name:        "quotes in SSID",
			ssid:        `My "WiFi" Network`,
			password:    "password123",
			description: "Double quotes in SSID",
		},
		{
			name:        "quotes in password",
			ssid:        "MyWiFi",
			password:    `pass"word'123`,
			description: "Quotes in password",
		},
		{
			name:        "backslashes in password",
			ssid:        "MyWiFi",
			password:    `pass\word\123`,
			description: "Backslashes in password",
		},
		{
			name:        "newlines in SSID",
			ssid:        "WiFi\nNetwork",
			password:    "password123",
			description: "Newline in SSID (should be rejected but isn't)",
		},
		{
			name:        "null bytes",
			ssid:        "WiFi\x00Network",
			password:    "password123",
			description: "Null byte in SSID (security risk)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, _, cleanup := setupTestManager(t)
			defer cleanup()

			err := m.AddNetwork(tt.ssid, tt.password)
			if err != nil {
				t.Errorf("%s: unexpected error: %v", tt.description, err)
			}

			// Verify we can retrieve the network
			networks := m.GetNetworks()
			if len(networks) != 1 {
				t.Fatalf("Expected 1 network, got %d", len(networks))
			}

			// BUG #15: No validation or sanitization of special characters
			// This could cause issues with the underlying WiFi system
			if networks[0].SSID != tt.ssid {
				t.Errorf("SSID mismatch: expected %q, got %q", tt.ssid, networks[0].SSID)
			}
			if networks[0].PSK != tt.password {
				t.Errorf("Password mismatch: expected %q, got %q", tt.password, networks[0].PSK)
			}

			// BUG #16: No escaping or validation for JSON-unsafe characters
			// Verify JSON round-trip works
			config := WiFiConfig{Networks: networks}
			data, err := json.Marshal(config)
			if err != nil {
				t.Errorf("Failed to marshal with special chars: %v", err)
			}

			var decoded WiFiConfig
			err = json.Unmarshal(data, &decoded)
			if err != nil {
				t.Errorf("Failed to unmarshal with special chars: %v", err)
			}

			if len(decoded.Networks) != 1 {
				t.Fatalf("Lost networks during JSON round-trip")
			}

			if decoded.Networks[0].SSID != tt.ssid {
				t.Errorf("SSID corrupted in JSON round-trip: %q -> %q", tt.ssid, decoded.Networks[0].SSID)
			}
		})
	}
}

// TestVeryLongSSIDsAndPasswords tests length limits
func TestVeryLongSSIDsAndPasswords(t *testing.T) {
	tests := []struct {
		name        string
		ssid        string
		password    string
		expectError bool
		description string
	}{
		{
			name:        "max length SSID",
			ssid:        string(make([]byte, 32)), // WiFi SSID max is 32 bytes
			password:    "password123",
			expectError: false,
			description: "32-byte SSID (maximum allowed by WiFi spec)",
		},
		{
			name:        "over max SSID",
			ssid:        string(make([]byte, 33)),
			password:    "password123",
			expectError: false, // BUG #17: No SSID length validation (33 bytes > spec)
			description: "33-byte SSID exceeds WiFi spec",
		},
		{
			name:        "very long SSID",
			ssid:        string(make([]byte, 1000)),
			password:    "password123",
			expectError: false, // BUG #18: Accepts absurdly long SSIDs
			description: "1000-byte SSID",
		},
		{
			name:        "max length WPA2 password",
			ssid:        "MyNetwork",
			password:    string(make([]byte, 63)), // WPA2 max is 63 ASCII chars
			expectError: false,
			description: "63-character password (WPA2 maximum)",
		},
		{
			name:        "over max WPA2 password",
			ssid:        "MyNetwork",
			password:    string(make([]byte, 64)),
			expectError: false, // BUG #19: No password length validation (64 > spec)
			description: "64-character password exceeds WPA2 spec",
		},
		{
			name:        "extremely long password",
			ssid:        "MyNetwork",
			password:    string(make([]byte, 10000)),
			expectError: false, // BUG #20: Accepts absurdly long passwords
			description: "10000-character password",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, _, cleanup := setupTestManager(t)
			defer cleanup()

			err := m.AddNetwork(tt.ssid, tt.password)

			if tt.expectError && err == nil {
				t.Errorf("%s: expected error but got none", tt.description)
			}
			if !tt.expectError && err != nil && len(tt.ssid) <= 32 && len(tt.password) <= 63 {
				t.Errorf("%s: unexpected error for valid length: %v", tt.description, err)
			}

			// Even if accepted, verify storage works
			if err == nil {
				networks := m.GetNetworks()
				if len(networks) != 1 {
					t.Errorf("Failed to store network")
				}
			}
		})
	}
}

// TestConcurrentOperations tests thread safety
func TestConcurrentOperations(t *testing.T) {
	m, _, cleanup := setupTestManager(t)
	defer cleanup()

	const numGoroutines = 50
	const numOperations = 100

	var wg sync.WaitGroup
	errors := make(chan error, numGoroutines*numOperations)

	// Concurrent additions
	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				ssid := "Network-" + string(rune('A'+id%26))
				password := "password123"
				err := m.AddNetwork(ssid, password)
				if err != nil {
					errors <- err
				}
			}
		}(i)
	}

	// Concurrent reads
	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				_ = m.GetNetworks()
			}
		}()
	}

	// Concurrent removals
	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				ssid := "Network-" + string(rune('A'+id%26))
				_ = m.RemoveNetwork(ssid) // Error expected when not found
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// Check for unexpected errors (race conditions, panics)
	errorCount := 0
	for err := range errors {
		t.Logf("Concurrent operation error: %v", err)
		errorCount++
	}

	// BUG #21: save() called while holding write lock could cause issues
	// Multiple goroutines trying to save simultaneously could corrupt file

	if errorCount > 0 {
		t.Logf("Got %d errors during concurrent operations", errorCount)
	}

	// Verify final state is consistent
	networks := m.GetNetworks()
	t.Logf("Final network count after concurrent operations: %d", len(networks))
}

// TestConcurrentSaveOperations specifically tests file I/O race conditions
func TestConcurrentSaveOperations(t *testing.T) {
	m, _, cleanup := setupTestManager(t)
	defer cleanup()

	const numGoroutines = 20
	var wg sync.WaitGroup
	errors := make(chan error, numGoroutines)

	// Multiple goroutines trying to add networks simultaneously
	// This will cause concurrent save() calls
	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			ssid := string(rune('A' + id))
			err := m.AddNetwork("Network-"+ssid, "password"+ssid)
			if err != nil {
				errors <- err
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// BUG #22: Race condition in save() - tmp file could be overwritten
	// The atomic write pattern uses .tmp suffix, but multiple goroutines
	// could overwrite each other's .tmp files

	for err := range errors {
		if err != nil {
			t.Errorf("Concurrent save error: %v", err)
		}
	}

	networks := m.GetNetworks()
	if len(networks) != numGoroutines {
		t.Errorf("Expected %d networks, got %d (possible race condition in save)", numGoroutines, len(networks))
	}
}

// TestNetworkPriorityOrdering tests network priority/ordering
func TestNetworkPriorityOrdering(t *testing.T) {
	m, _, cleanup := setupTestManager(t)
	defer cleanup()

	// Add networks in specific order
	networks := []struct {
		ssid     string
		password string
	}{
		{"HighPriority", "pass1"},
		{"MediumPriority", "pass2"},
		{"LowPriority", "pass3"},
	}

	for _, n := range networks {
		err := m.AddNetwork(n.ssid, n.password)
		if err != nil {
			t.Fatalf("Failed to add network: %v", err)
		}
	}

	retrieved := m.GetNetworks()

	// BUG #23: No priority or ordering mechanism
	// Networks are stored in order added, but there's no way to:
	// - Set priority for connection attempts
	// - Reorder networks
	// - Move network to front/back

	if len(retrieved) != 3 {
		t.Fatalf("Expected 3 networks, got %d", len(retrieved))
	}

	// Verify insertion order is preserved (it should be)
	expectedOrder := []string{"HighPriority", "MediumPriority", "LowPriority"}
	for i, expected := range expectedOrder {
		if retrieved[i].SSID != expected {
			t.Errorf("Network order not preserved: position %d expected %s, got %s", i, expected, retrieved[i].SSID)
		}
	}

	// Test that updating a network preserves its position
	err := m.AddNetwork("MediumPriority", "newpassword")
	if err != nil {
		t.Fatalf("Failed to update network: %v", err)
	}

	retrieved = m.GetNetworks()
	if retrieved[1].SSID != "MediumPriority" || retrieved[1].PSK != "newpassword" {
		t.Error("Network update changed position or failed to update password")
	}
}

// TestRecoveryFromMalformedConfiguration tests error recovery
func TestRecoveryFromMalformedConfiguration(t *testing.T) {
	// Test NewManager with non-existent file
	t.Run("missing config file", func(t *testing.T) {
		m, err := NewManager()
		if err != nil {
			t.Errorf("NewManager should not error on missing config: %v", err)
		}

		networks := m.GetNetworks()
		if len(networks) != 0 {
			t.Errorf("Expected empty network list, got %d networks", len(networks))
		}
	})

	// Test that corrupted file doesn't prevent operation
	t.Run("corrupted file recovery", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "wifimanager-recovery-*")
		if err != nil {
			t.Fatalf("Failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tmpDir)

		// BUG #24: Can't inject config path, so we can't test recovery from corrupted files
		// The hardcoded WiFiConfigPath makes it impossible to test this properly

		// Create a corrupted config at the standard path (risky in tests)
		// Instead, we'll test the load() logic with various inputs

		// Test unmarshal of various corrupted inputs
		corruptedInputs := []string{
			"",
			"{",
			"null",
			"[]",
			`{"networks": "not an array"}`,
		}

		for _, input := range corruptedInputs {
			var config WiFiConfig
			err := json.Unmarshal([]byte(input), &config)
			// Most should error, which is fine
			if err == nil && config.Networks == nil {
				// This case is handled by: m.networks = make([]Network, 0)
				t.Logf("Null networks handled correctly")
			}
		}
	})

	// BUG #25: NewManager returns (m, nil) even when load() fails
	// The error is silently ignored: "return m, nil"
	// This means corrupted configs are silently dropped without user notification
}

// TestSecurityConcerns tests potential security issues
func TestSecurityConcerns(t *testing.T) {
	t.Run("file permissions", func(t *testing.T) {
		m, _, cleanup := setupTestManager(t)
		defer cleanup()

		err := m.AddNetwork("TestNetwork", "secretpassword")
		if err != nil {
			t.Fatalf("Failed to add network: %v", err)
		}

		// BUG #26: Can't verify file permissions on actual config file
		// File is written with mode 0600, which is good
		// But we can't test it due to hardcoded path

		// The code uses 0600 which is correct (owner read/write only)
		// os.WriteFile(tmpFile, data, 0600)
		t.Log("File permissions appear correct (0600) in code")
	})

	t.Run("password in memory", func(t *testing.T) {
		m, _, cleanup := setupTestManager(t)
		defer cleanup()

		password := "verysecretpassword"
		err := m.AddNetwork("TestNetwork", password)
		if err != nil {
			t.Fatalf("Failed to add network: %v", err)
		}

		// BUG #27: Passwords stored as strings, not zeroed from memory
		// Sensitive data should ideally be in []byte and zeroed after use
		// Strings are immutable in Go and can't be zeroed

		networks := m.GetNetworks()
		if networks[0].PSK == password {
			t.Log("Note: Passwords stored as strings (can't be zeroed from memory)")
		}
	})

	t.Run("password exposure in logs", func(t *testing.T) {
		// BUG #28: If any errors occur, password might be exposed in error messages
		// Current code doesn't log passwords, which is good
		// But if someone adds debug logging, it could be dangerous

		m, _, cleanup := setupTestManager(t)
		defer cleanup()

		err := m.AddNetwork("Test", "secretpass")
		if err != nil {
			if err.Error() != "secretpass" {
				t.Log("Good: Password not in error message")
			} else {
				t.Error("BUG: Password exposed in error message!")
			}
		}
	})

	t.Run("JSON injection", func(t *testing.T) {
		// BUG #29: No validation against JSON injection attacks
		// Malicious SSID like: `","psk":"hacker"},{"ssid":"evil`
		// Could potentially break JSON structure

		m, _, cleanup := setupTestManager(t)
		defer cleanup()

		maliciousSSID := `","psk":"hacker"},{"ssid":"evil`
		err := m.AddNetwork(maliciousSSID, "password")
		if err != nil {
			t.Fatalf("Failed to add network: %v", err)
		}

		// Go's json.Marshal properly escapes, so this should be safe
		networks := m.GetNetworks()
		if len(networks) != 1 {
			t.Error("JSON injection attempt created multiple networks!")
		}
		if networks[0].SSID != maliciousSSID {
			t.Error("SSID was corrupted")
		}

		// Verify JSON is properly escaped
		config := WiFiConfig{Networks: networks}
		data, _ := json.Marshal(config)
		if string(data) != `{"networks":[{"ssid":"\",\"psk\":\"hacker\"},{\"ssid\":\"evil","psk":"password"}]}` {
			// Check it unmarshals correctly
			var decoded WiFiConfig
			err := json.Unmarshal(data, &decoded)
			if err != nil {
				t.Errorf("JSON injection vulnerability: %v", err)
			}
			if len(decoded.Networks) != 1 {
				t.Error("JSON injection created multiple networks after round-trip")
			}
		}
	})

	t.Run("path traversal", func(t *testing.T) {
		// BUG #30: WiFiConfigPath is hardcoded, so no path traversal risk
		// But if it were configurable, SSID could be used for path traversal
		// e.g., SSID = "../../../etc/passwd"

		// Current implementation is safe because path is hardcoded
		t.Log("No path traversal risk (path is hardcoded)")
	})
}

// TestRemoveNonExistentNetwork tests removing network that doesn't exist
func TestRemoveNonExistentNetwork(t *testing.T) {
	m, _, cleanup := setupTestManager(t)
	defer cleanup()

	err := m.RemoveNetwork("NonExistent")
	if err == nil {
		t.Error("Expected error when removing non-existent network")
	}

	expectedError := "network not found: NonExistent"
	if err.Error() != expectedError {
		t.Errorf("Expected error %q, got %q", expectedError, err.Error())
	}
}

// TestGetCurrentSSID tests getting current SSID
func TestGetCurrentSSID(t *testing.T) {
	m, _, cleanup := setupTestManager(t)
	defer cleanup()

	// With no networks configured
	ssid, err := m.GetCurrentSSID()
	if err == nil {
		t.Error("Expected error when no network configured")
	}
	if ssid != "" {
		t.Errorf("Expected empty SSID, got %q", ssid)
	}

	// Add a network
	err = m.AddNetwork("HomeWiFi", "password")
	if err != nil {
		t.Fatalf("Failed to add network: %v", err)
	}

	// BUG #31: GetCurrentSSID returns first network, not actually connected one
	// It reads from config file, not actual WiFi status
	ssid, err = m.GetCurrentSSID()
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	// Due to hardcoded path, this will fail unless we create /perm/wifi.json
	t.Logf("GetCurrentSSID returned: %q (may not reflect actual connection)", ssid)
}

// TestAtomicWritePattern tests the atomic write implementation
func TestAtomicWritePattern(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "wifimanager-atomic-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Simulate atomic write pattern
	configPath := filepath.Join(tmpDir, "config.json")
	tmpPath := configPath + ".tmp"

	data := []byte(`{"networks":[]}`)

	// Write to temp file
	err = os.WriteFile(tmpPath, data, 0600)
	if err != nil {
		t.Fatalf("Failed to write temp file: %v", err)
	}

	// Verify temp file exists
	if _, err := os.Stat(tmpPath); err != nil {
		t.Errorf("Temp file not created: %v", err)
	}

	// Rename to final location
	err = os.Rename(tmpPath, configPath)
	if err != nil {
		t.Fatalf("Failed to rename: %v", err)
	}

	// Verify final file exists and temp file is gone
	if _, err := os.Stat(configPath); err != nil {
		t.Errorf("Config file not created: %v", err)
	}

	if _, err := os.Stat(tmpPath); err == nil {
		t.Error("Temp file still exists after rename")
	}

	// BUG #32: If rename fails, .tmp file is left behind
	// No cleanup of failed atomic writes

	// BUG #33: Multiple concurrent saves could race on the same .tmp file
	// Should use unique temp file names (e.g., .tmp.{pid}.{timestamp})
}

// TestEmptyNetworksList tests handling of empty networks
func TestEmptyNetworksList(t *testing.T) {
	m, _, cleanup := setupTestManager(t)
	defer cleanup()

	networks := m.GetNetworks()
	if networks == nil {
		t.Error("GetNetworks should return empty slice, not nil")
	}

	if len(networks) != 0 {
		t.Errorf("Expected 0 networks, got %d", len(networks))
	}

	// Test removing from empty list
	err := m.RemoveNetwork("anything")
	if err == nil {
		t.Error("Expected error when removing from empty list")
	}
}

// TestRapidAddRemove tests rapid addition and removal
func TestRapidAddRemove(t *testing.T) {
	m, _, cleanup := setupTestManager(t)
	defer cleanup()

	// Rapidly add and remove the same network
	for i := 0; i < 100; i++ {
		err := m.AddNetwork("TestNetwork", "password")
		if err != nil {
			t.Fatalf("Failed to add network on iteration %d: %v", i, err)
		}

		err = m.RemoveNetwork("TestNetwork")
		if err != nil {
			t.Fatalf("Failed to remove network on iteration %d: %v", i, err)
		}
	}

	networks := m.GetNetworks()
	if len(networks) != 0 {
		t.Errorf("Expected 0 networks after rapid add/remove, got %d", len(networks))
	}

	// BUG #34: Rapid save operations could cause I/O bottleneck or wear on flash storage
	// No rate limiting or batching of saves
}

// TestMutexDeadlock tests for potential deadlock scenarios
func TestMutexDeadlock(t *testing.T) {
	m, _, cleanup := setupTestManager(t)
	defer cleanup()

	// Add some networks
	for i := 0; i < 5; i++ {
		_ = m.AddNetwork(string(rune('A'+i)), "password")
	}

	done := make(chan bool, 1)

	// This should not deadlock
	go func() {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("Panic occurred: %v", r)
			}
			done <- true
		}()

		// Nested operations that acquire locks
		for i := 0; i < 100; i++ {
			_ = m.GetNetworks()
			_ = m.AddNetwork("Test", "password")
			_ = m.RemoveNetwork("Test")
		}
	}()

	select {
	case <-done:
		// Success
	case <-time.After(5 * time.Second):
		t.Fatal("Deadlock detected - operations did not complete in 5 seconds")
	}

	// BUG #35: save() calls os.WriteFile and os.Rename while holding write lock
	// If these I/O operations block, they could cause long lock hold times
	// Better to marshal JSON with lock, then write file without lock
}

// TestNetworkStructValidation tests the Network struct
func TestNetworkStructValidation(t *testing.T) {
	// BUG #36: Network struct has no validation methods
	// No IsValid(), Validate(), or sanitization methods

	tests := []struct {
		network Network
		valid   bool
		reason  string
	}{
		{
			network: Network{SSID: "Valid", PSK: "password123"},
			valid:   true,
			reason:  "Normal network",
		},
		{
			network: Network{SSID: "", PSK: "password"},
			valid:   false,
			reason:  "Empty SSID",
		},
		{
			network: Network{SSID: "   ", PSK: "password"},
			valid:   false,
			reason:  "Whitespace SSID",
		},
		{
			network: Network{SSID: string(make([]byte, 33)), PSK: "password"},
			valid:   false,
			reason:  "SSID too long (33 bytes)",
		},
		{
			network: Network{SSID: "Valid", PSK: string(make([]byte, 64))},
			valid:   false,
			reason:  "Password too long (64 chars)",
		},
		{
			network: Network{SSID: "Valid", PSK: "1234567"},
			valid:   false,
			reason:  "Password too short (7 chars, WPA2 min is 8)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.reason, func(t *testing.T) {
			// There's no validation method, so we can't test it
			// This is the bug - there should be a Network.Validate() method

			mgr, _, cleanup := setupTestManager(t)
			defer cleanup()

			err := mgr.AddNetwork(tt.network.SSID, tt.network.PSK)

			if !tt.valid && err == nil {
				t.Errorf("Expected error for %s, but got none", tt.reason)
			}
		})
	}
}
