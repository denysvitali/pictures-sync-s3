package wifimanager

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateWiFiPassword(t *testing.T) {
	tests := []struct {
		name     string
		password string
		wantErr  bool
		errMsg   string
	}{
		// Valid passwords
		{
			name:     "minimum valid password",
			password: "12345678",
			wantErr:  false,
		},
		{
			name:     "typical strong password",
			password: "MySecureP@ssw0rd!",
			wantErr:  false,
		},
		{
			name:     "maximum length password",
			password: strings.Repeat("a", 63),
			wantErr:  false,
		},
		{
			name:     "password with special characters",
			password: "P@ss!W0rd#2023$",
			wantErr:  false,
		},
		{
			name:     "password with spaces",
			password: "My WiFi Password 2023",
			wantErr:  false,
		},

		// Invalid passwords - too short
		// Note: Empty password is tested separately for open networks
		{
			name:     "single character",
			password: "a",
			wantErr:  true,
			errMsg:   "at least 8 characters",
		},
		{
			name:     "7 characters (below minimum)",
			password: "1234567",
			wantErr:  true,
			errMsg:   "at least 8 characters",
		},

		// Invalid passwords - too long
		{
			name:     "64 characters (exceeds maximum)",
			password: strings.Repeat("a", 64),
			wantErr:  true,
			errMsg:   "cannot exceed 63 characters",
		},
		{
			name:     "100 characters (way too long)",
			password: strings.Repeat("a", 100),
			wantErr:  true,
			errMsg:   "cannot exceed 63 characters",
		},

		// Invalid passwords - control characters
		{
			name:     "password with null byte",
			password: "password\x00test",
			wantErr:  true,
			errMsg:   "invalid character",
		},
		{
			name:     "password with newline",
			password: "password\ntest",
			wantErr:  true,
			errMsg:   "invalid character",
		},
		{
			name:     "password with tab",
			password: "password\ttest",
			wantErr:  true,
			errMsg:   "invalid character",
		},
		{
			name:     "password with carriage return",
			password: "password\rtest",
			wantErr:  true,
			errMsg:   "invalid character",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateWiFiPassword(tt.password)

			if tt.wantErr {
				if err == nil {
					t.Errorf("ValidateWiFiPassword() expected error for password %q, but got nil", tt.password)
					return
				}
				if tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("ValidateWiFiPassword() error = %v, want error containing %q", err, tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("ValidateWiFiPassword() unexpected error for password %q: %v", tt.password, err)
				}
			}
		})
	}
}

func TestAddNetwork_PasswordValidation(t *testing.T) {
	// Use temp directory for testing to avoid permission issues
	tmpDir := t.TempDir()
	oldPath := WiFiConfigPath
	WiFiConfigPath = filepath.Join(tmpDir, "wifi.json")
	defer func() { WiFiConfigPath = oldPath }()

	mgr, err := NewManager()
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	// Test that weak passwords are rejected
	weakPasswords := []struct {
		password string
		desc     string
	}{
		{"a", "single char"},
		{"1234567", "only 7 chars"},
		{"password\x00", "null byte"},
	}

	for _, wp := range weakPasswords {
		err := mgr.AddNetwork("TestSSID", wp.password)
		if err == nil {
			t.Errorf("AddNetwork() should reject weak password (%s): %q", wp.desc, wp.password)
		} else {
			t.Logf("✓ Correctly rejected weak password (%s): %v", wp.desc, err)
		}
	}

	// Test that strong passwords are accepted
	strongPassword := "MySecureP@ssw0rd2023!"
	err = mgr.AddNetwork("TestSSID", strongPassword)
	if err != nil {
		t.Errorf("AddNetwork() should accept strong password: %v", err)
	} else {
		t.Log("✓ Strong password accepted")
	}
}

func TestAddNetwork_OpenNetworks(t *testing.T) {
	// Use temp directory for testing to avoid permission issues
	tmpDir := t.TempDir()
	oldPath := WiFiConfigPath
	WiFiConfigPath = filepath.Join(tmpDir, "wifi.json")
	defer func() { WiFiConfigPath = oldPath }()

	mgr, err := NewManager()
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	// Empty password should bypass validation (open network)
	err = mgr.AddNetwork("OpenNetwork", "")
	if err != nil {
		t.Errorf("Open network (empty password) should be allowed: %v", err)
	} else {
		t.Log("✓ Empty password allowed (open network)")
	}
}
