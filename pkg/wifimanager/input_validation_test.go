package wifimanager

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestInputValidation_SSIDValidation tests SSID input validation
func TestInputValidation_SSIDValidation(t *testing.T) {
	tests := []struct {
		name        string
		ssid        string
		password    string
		description string
		severity    string
		expectError bool
	}{
		{
			name:        "ValidSSID",
			ssid:        "MyNetwork",
			password:    "password123",
			description: "Normal SSID should be accepted",
			severity:    "N/A",
			expectError: false,
		},
		{
			name:        "EmptySSID",
			ssid:        "",
			password:    "password",
			description: "Empty SSID should be rejected",
			severity:    "HIGH",
			expectError: true,
		},
		{
			name:        "VeryLongSSID",
			ssid:        strings.Repeat("A", 100000),
			password:    "password",
			description: "Very long SSID could cause memory issues",
			severity:    "HIGH",
			expectError: false, // Currently no length validation
		},
		{
			name:        "SSIDWithNullByte",
			ssid:        "Network\x00Hidden",
			password:    "password",
			description: "Null byte could truncate SSID in WiFi operations",
			severity:    "HIGH",
			expectError: false,
		},
		{
			name:        "SSIDWithControlChars",
			ssid:        "Network\r\n\t",
			password:    "password",
			description: "Control characters in SSID",
			severity:    "MEDIUM",
			expectError: false,
		},
		{
			name:        "SSIDWithUnicode",
			ssid:        "WiFi\u202e",
			password:    "password",
			description: "Unicode direction override in SSID",
			severity:    "MEDIUM",
			expectError: false,
		},
		{
			name:        "SSIDWithFormatStrings",
			ssid:        "%s%s%s%n%n",
			password:    "password",
			description: "Format string specifiers in SSID",
			severity:    "HIGH",
			expectError: false,
		},
		{
			name:        "SSIDWithPathTraversal",
			ssid:        "../../../etc/passwd",
			password:    "password",
			description: "Path traversal in SSID",
			severity:    "MEDIUM",
			expectError: false,
		},
		{
			name:        "VeryLongPassword",
			ssid:        "Network",
			password:    strings.Repeat("P", 100000),
			description: "Very long password stored in config file",
			severity:    "HIGH",
			expectError: false,
		},
		{
			name:        "PasswordWithNullByte",
			ssid:        "Network",
			password:    "pass\x00word",
			description: "Null byte in password",
			severity:    "HIGH",
			expectError: false,
		},
		{
			name:        "PasswordWithSpecialChars",
			ssid:        "Network",
			password:    "!@#$%^&*(){}[]|\\:;\"'<>,.?/~`",
			description: "Special characters in password",
			severity:    "LOW",
			expectError: false,
		},
	}

	tmpDir := t.TempDir()
	originalConfigPath := WiFiConfigPath
	defer func() { WiFiConfigPath = originalConfigPath }()
	WiFiConfigPath = filepath.Join(tmpDir, "wifi.json")

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mgr, err := NewManager()
			if err != nil {
				t.Fatalf("Failed to create manager: %v", err)
			}

			err = mgr.AddNetwork(tt.ssid, tt.password)

			if tt.expectError && err == nil {
				t.Errorf("[%s] %s: Expected error but got none", tt.severity, tt.description)
			} else if !tt.expectError && err != nil && tt.severity != "N/A" {
				t.Logf("[%s] %s: Got error: %v", tt.severity, tt.description, err)
			} else {
				t.Logf("[%s] %s: Added to config", tt.severity, tt.description)

				// Check if it was saved to file
				networks := mgr.GetNetworks()
				found := false
				for _, n := range networks {
					if n.SSID == tt.ssid {
						found = true
						break
					}
				}
				if found {
					t.Logf("  Network saved successfully")
				}
			}
		})
	}
}

// TestInputValidation_ConfigFileInjection tests config file injection vulnerabilities
func TestInputValidation_ConfigFileInjection(t *testing.T) {
	tests := []struct {
		name        string
		ssid        string
		password    string
		description string
		severity    string
	}{
		{
			name:        "JSONInjectionInSSID",
			ssid:        `test","malicious":"value`,
			password:    "pass",
			description: "JSON injection in SSID field",
			severity:    "CRITICAL",
		},
		{
			name:        "JSONInjectionInPassword",
			ssid:        "network",
			password:    `pass","admin":true`,
			description: "JSON injection in password field",
			severity:    "CRITICAL",
		},
		{
			name:        "QuoteEscape",
			ssid:        `test\"escape`,
			password:    "pass",
			description: "Quote escaping attempt",
			severity:    "HIGH",
		},
		{
			name:        "NewlineInjection",
			ssid:        "test\n{\"malicious\":true}",
			password:    "pass",
			description: "Newline injection to add malicious JSON",
			severity:    "HIGH",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			originalConfigPath := WiFiConfigPath
			defer func() { WiFiConfigPath = originalConfigPath }()
			WiFiConfigPath = filepath.Join(tmpDir, "wifi.json")

			mgr, err := NewManager()
			if err != nil {
				t.Fatalf("Failed to create manager: %v", err)
			}

			err = mgr.AddNetwork(tt.ssid, tt.password)
			if err != nil {
				t.Logf("[%s] %s: Rejected with error: %v", tt.severity, tt.description, err)
				return
			}

			// Read the config file and check for injection
			data, err := os.ReadFile(WiFiConfigPath)
			if err != nil {
				t.Logf("Could not read config file: %v", err)
				return
			}

			configStr := string(data)

			// Check if injection succeeded by looking for malicious content
			if strings.Contains(configStr, "malicious") {
				t.Errorf("[%s] VULNERABILITY: %s - Injection succeeded!", tt.severity, tt.description)
				t.Logf("Config content: %s", configStr)
			} else {
				t.Logf("[%s] %s: Injection blocked or escaped", tt.severity, tt.description)
			}

			// Try to reload the config
			mgr2, err := NewManager()
			if err != nil {
				t.Errorf("[%s] VULNERABILITY: Config file corrupted, cannot reload: %v", tt.severity, err)
			} else {
				networks := mgr2.GetNetworks()
				t.Logf("  Reloaded %d networks", len(networks))
			}
		})
	}
}

// TestInputValidation_RemoveNetwork tests network removal vulnerabilities
func TestInputValidation_RemoveNetwork(t *testing.T) {
	tests := []struct {
		name        string
		addSSID     string
		removeSSID  string
		description string
		severity    string
	}{
		{
			name:        "NormalRemoval",
			addSSID:     "Network1",
			removeSSID:  "Network1",
			description: "Normal network removal",
			severity:    "N/A",
		},
		{
			name:        "RemoveNonexistent",
			addSSID:     "Network1",
			removeSSID:  "Network2",
			description: "Removing non-existent network",
			severity:    "LOW",
		},
		{
			name:        "RemoveWithPathTraversal",
			addSSID:     "Network1",
			removeSSID:  "../Network1",
			description: "Path traversal in remove operation",
			severity:    "HIGH",
		},
		{
			name:        "RemoveWithNullByte",
			addSSID:     "Network1",
			removeSSID:  "Network1\x00malicious",
			description: "Null byte in SSID to remove",
			severity:    "MEDIUM",
		},
		{
			name:        "RemoveEmptyString",
			addSSID:     "Network1",
			removeSSID:  "",
			description: "Empty string removal",
			severity:    "LOW",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			originalConfigPath := WiFiConfigPath
			defer func() { WiFiConfigPath = originalConfigPath }()
			WiFiConfigPath = filepath.Join(tmpDir, "wifi.json")

			mgr, _ := NewManager()

			// Add network
			mgr.AddNetwork(tt.addSSID, "password")

			// Try to remove
			err := mgr.RemoveNetwork(tt.removeSSID)

			if err != nil {
				t.Logf("[%s] %s: Remove failed: %v", tt.severity, tt.description, err)
			} else {
				t.Logf("[%s] %s: Remove succeeded", tt.severity, tt.description)
			}

			// Check if the correct network was removed
			networks := mgr.GetNetworks()
			for _, n := range networks {
				if n.SSID == tt.addSSID {
					if tt.removeSSID == tt.addSSID {
						t.Errorf("Network still exists after removal")
					}
				}
			}
		})
	}
}

// TestInputValidation_ConcurrentAccess tests concurrent access vulnerabilities
func TestInputValidation_ConcurrentAccess(t *testing.T) {
	tmpDir := t.TempDir()
	originalConfigPath := WiFiConfigPath
	defer func() { WiFiConfigPath = originalConfigPath }()
	WiFiConfigPath = filepath.Join(tmpDir, "wifi.json")

	mgr, err := NewManager()
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	const numGoroutines = 100
	done := make(chan bool, numGoroutines)

	// Concurrent adds and removes
	for i := 0; i < numGoroutines; i++ {
		go func(index int) {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("Goroutine %d panicked: %v", index, r)
				}
				done <- true
			}()

			ssid := fmt.Sprintf("Network%d", index)
			if index%2 == 0 {
				mgr.AddNetwork(ssid, "password")
			} else {
				mgr.RemoveNetwork(ssid)
			}
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < numGoroutines; i++ {
		<-done
	}

	// Verify config is still valid
	data, err := os.ReadFile(WiFiConfigPath)
	if err != nil {
		t.Errorf("Could not read config after concurrent access: %v", err)
		return
	}

	// Try to load it
	mgr2, err := NewManager()
	if err != nil {
		t.Errorf("Config corrupted after concurrent access: %v", err)
		t.Logf("Config content: %s", string(data))
	} else {
		networks := mgr2.GetNetworks()
		t.Logf("After %d concurrent operations, config has %d networks", numGoroutines, len(networks))
	}
}

// TestInputValidation_ScanResults tests scan result handling
func TestInputValidation_ScanResults(t *testing.T) {
	tmpDir := t.TempDir()
	originalConfigPath := WiFiConfigPath
	defer func() { WiFiConfigPath = originalConfigPath }()
	WiFiConfigPath = filepath.Join(tmpDir, "wifi.json")

	mgr, err := NewManager()
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	// Call ScanNetworks - it returns mock data in Gokrazy
	results, err := mgr.ScanNetworks()

	if err != nil {
		t.Logf("Scan failed: %v", err)
		return
	}

	t.Logf("Scan returned %d results", len(results))

	// Check results for potential issues
	for i, result := range results {
		t.Logf("Result %d: SSID='%s', Signal=%d, Encrypted=%v",
			i, result.SSID, result.Signal, result.Encrypted)

		// Check for injection in SSID
		if strings.Contains(result.SSID, "\x00") {
			t.Errorf("VULNERABILITY: Scan result contains null byte")
		}
		if strings.Contains(result.SSID, "\r") || strings.Contains(result.SSID, "\n") {
			t.Logf("WARNING: Scan result contains control characters")
		}

		// Check signal range
		if result.Signal > 0 || result.Signal < -100 {
			t.Logf("WARNING: Signal value out of normal range: %d", result.Signal)
		}
	}
}

// Add missing import
import "fmt"
