package settings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestInputValidation_ConfigParsing tests configuration parsing vulnerabilities
func TestInputValidation_ConfigParsing(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "settings.json")

	tests := []struct {
		name        string
		jsonContent string
		description string
		severity    string
		expectError bool
	}{
		{
			name:        "VeryLargeConfig",
			jsonContent: `{"remote_name":"` + strings.Repeat("A", 10000000) + `","remote_path":"/photos"}`,
			description: "Extremely large configuration could exhaust memory",
			severity:    "HIGH",
			expectError: false, // JSON decoder should handle this
		},
		{
			name: "DeeplyNestedJSON",
			jsonContent: func() string {
				nested := `"value"`
				for i := 0; i < 10000; i++ {
					nested = `{"nested":` + nested + `}`
				}
				return nested
			}(),
			description: "Deeply nested JSON could cause stack overflow",
			severity:    "CRITICAL",
			expectError: true,
		},
		{
			name:        "MalformedJSON_TrailingComma",
			jsonContent: `{"remote_name":"test","remote_path":"/photos",}`,
			description: "Trailing comma should be rejected",
			severity:    "LOW",
			expectError: true,
		},
		{
			name:        "MalformedJSON_UnquotedKeys",
			jsonContent: `{remote_name:"test",remote_path:"/photos"}`,
			description: "Unquoted keys should be rejected",
			severity:    "LOW",
			expectError: true,
		},
		{
			name:        "InvalidUTF8",
			jsonContent: "{\"remote_name\":\"\xFF\xFE\",\"remote_path\":\"/photos\"}",
			description: "Invalid UTF-8 sequences should be rejected",
			severity:    "MEDIUM",
			expectError: true,
		},
		{
			name:        "NullBytes",
			jsonContent: "{\"remote_name\":\"test\\u0000evil\",\"remote_path\":\"/photos\"}",
			description: "Null bytes in strings could cause truncation",
			severity:    "HIGH",
			expectError: false, // JSON might allow this
		},
		{
			name:        "ControlCharacters",
			jsonContent: "{\"remote_name\":\"test\r\n\t\",\"remote_path\":\"/photos\"}",
			description: "Control characters should be handled safely",
			severity:    "LOW",
			expectError: false,
		},
		{
			name:        "UnicodeEscapes",
			jsonContent: `{"remote_name":"test\u202e","remote_path":"/photos"}`,
			description: "Unicode direction overrides could cause display issues",
			severity:    "MEDIUM",
			expectError: false,
		},
		{
			name:        "NegativeIntegers",
			jsonContent: `{"transfers":-999,"checkers":-999}`,
			description: "Negative integers should be validated",
			severity:    "MEDIUM",
			expectError: false,
		},
		{
			name:        "ExtremelyLargeNumbers",
			jsonContent: `{"transfers":999999999999999999999999999}`,
			description: "Numbers exceeding int64 range should be handled",
			severity:    "HIGH",
			expectError: true, // Should fail to parse
		},
		{
			name:        "SpecialFloats",
			jsonContent: `{"reformat_threshold":1e308}`,
			description: "Very large floats near limits should be handled",
			severity:    "MEDIUM",
			expectError: false,
		},
		{
			name:        "DuplicateKeys",
			jsonContent: `{"remote_name":"first","remote_name":"second"}`,
			description: "Duplicate keys - last value wins, could bypass validation",
			severity:    "MEDIUM",
			expectError: false,
		},
		{
			name:        "TypeMismatch_StringForInt",
			jsonContent: `{"transfers":"not a number"}`,
			description: "Type mismatch should fail unmarshaling",
			severity:    "MEDIUM",
			expectError: true,
		},
		{
			name:        "TypeMismatch_ArrayForString",
			jsonContent: `{"remote_name":["array","values"]}`,
			description: "Array instead of string should fail",
			severity:    "MEDIUM",
			expectError: true,
		},
		{
			name:        "EmptyJSON",
			jsonContent: ``,
			description: "Empty file should use defaults",
			severity:    "LOW",
			expectError: true,
		},
		{
			name:        "NullJSON",
			jsonContent: `null`,
			description: "Null JSON should be rejected or handled",
			severity:    "MEDIUM",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Write test config
			err := os.WriteFile(testFile, []byte(tt.jsonContent), 0644)
			if err != nil {
				t.Fatalf("Failed to write test file: %v", err)
			}

			// Try to unmarshal
			var s Settings
			data, err := os.ReadFile(testFile)
			if err != nil {
				t.Fatalf("Failed to read test file: %v", err)
			}

			err = json.Unmarshal(data, &s)

			if tt.expectError && err == nil {
				t.Errorf("[%s] %s: Expected error but got none", tt.severity, tt.description)
			} else if !tt.expectError && err != nil {
				t.Logf("[%s] %s: Unexpected error: %v", tt.severity, tt.description, err)
			} else {
				t.Logf("[%s] %s: Behavior as expected", tt.severity, tt.description)
			}

			// Clean up
			os.Remove(testFile)
		})
	}
}

// TestInputValidation_PathTraversal tests path traversal in settings
func TestInputValidation_PathTraversal(t *testing.T) {
	tests := []struct {
		name        string
		remoteName  string
		remotePath  string
		description string
		severity    string
	}{
		{
			name:        "DotDotInRemoteName",
			remoteName:  "../etc/passwd",
			remotePath:  "/photos",
			description: "Path traversal in remote name",
			severity:    "CRITICAL",
		},
		{
			name:        "DotDotInRemotePath",
			remoteName:  "remote",
			remotePath:  "/photos/../../etc/shadow",
			description: "Path traversal in remote path",
			severity:    "CRITICAL",
		},
		{
			name:        "AbsolutePathInRemoteName",
			remoteName:  "/etc/passwd",
			remotePath:  "/photos",
			description: "Absolute path in remote name",
			severity:    "HIGH",
		},
		{
			name:        "BackslashTraversal",
			remoteName:  "remote",
			remotePath:  "\\..\\..\\windows\\system32",
			description: "Windows-style path traversal",
			severity:    "HIGH",
		},
		{
			name:        "URLEncodedTraversal",
			remoteName:  "remote",
			remotePath:  "/photos/%2e%2e/%2e%2e/etc/passwd",
			description: "URL-encoded path traversal",
			severity:    "HIGH",
		},
		{
			name:        "UnicodeTraversal",
			remoteName:  "remote",
			remotePath:  "/photos/\u2024\u2024/\u2024\u2024/etc",
			description: "Unicode lookalike path traversal",
			severity:    "MEDIUM",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := DefaultSettings()
			err := s.SetRemote(tt.remoteName, tt.remotePath)

			if err != nil {
				t.Logf("[%s] %s: Rejected with error: %v", tt.severity, tt.description, err)
			} else {
				t.Logf("[%s] %s: ACCEPTED - potential vulnerability", tt.severity, tt.description)
				t.Logf("  RemoteName: %s", s.GetRemoteName())
				t.Logf("  RemotePath: %s", s.GetRemotePath())
			}
		})
	}
}

// TestInputValidation_SettingsBoundaries tests boundary values for settings
func TestInputValidation_SettingsBoundaries(t *testing.T) {
	tests := []struct {
		name        string
		setter      func(*Settings) error
		description string
		severity    string
	}{
		{
			name: "ZeroTransfers",
			setter: func(s *Settings) error {
				return s.SetTransfers(0)
			},
			description: "Zero transfers could cause no files to be uploaded",
			severity:    "HIGH",
		},
		{
			name: "NegativeTransfers",
			setter: func(s *Settings) error {
				return s.SetTransfers(-10)
			},
			description: "Negative transfers should be rejected",
			severity:    "HIGH",
		},
		{
			name: "MaxIntTransfers",
			setter: func(s *Settings) error {
				return s.SetTransfers(2147483647)
			},
			description: "Very large transfers could exhaust resources",
			severity:    "CRITICAL",
		},
		{
			name: "ZeroCheckers",
			setter: func(s *Settings) error {
				return s.SetCheckers(0)
			},
			description: "Zero checkers could prevent file verification",
			severity:    "HIGH",
		},
		{
			name: "NegativeThreshold",
			setter: func(s *Settings) error {
				return s.SetReformatThreshold(-1.0)
			},
			description: "Negative threshold should be rejected",
			severity:    "MEDIUM",
		},
		{
			name: "ThresholdAboveOne",
			setter: func(s *Settings) error {
				return s.SetReformatThreshold(1.5)
			},
			description: "Threshold above 1.0 (100%) might be invalid",
			severity:    "LOW",
		},
		{
			name: "VeryLargeThreshold",
			setter: func(s *Settings) error {
				return s.SetReformatThreshold(999999.99)
			},
			description: "Very large threshold could cause calculation errors",
			severity:    "MEDIUM",
		},
		{
			name: "ZeroThreshold",
			setter: func(s *Settings) error {
				return s.SetReformatThreshold(0.0)
			},
			description: "Zero threshold might trigger constant reformats",
			severity:    "HIGH",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := DefaultSettings()
			err := tt.setter(s)

			if err != nil {
				t.Logf("[%s] %s: Rejected with error: %v", tt.severity, tt.description, err)
			} else {
				t.Logf("[%s] %s: ACCEPTED - potential vulnerability", tt.severity, tt.description)
			}
		})
	}
}

// TestInputValidation_SettingsInjection tests injection vulnerabilities in settings
func TestInputValidation_SettingsInjection(t *testing.T) {
	tests := []struct {
		name           string
		remoteName     string
		remotePath     string
		description    string
		severity       string
		checkSavedFile bool
	}{
		{
			name:           "JSONInjectionInRemoteName",
			remoteName:     `test","malicious_field":"value`,
			remotePath:     "/photos",
			description:    "JSON injection attempt in remote name",
			severity:       "HIGH",
			checkSavedFile: true,
		},
		{
			name:           "NewlineInjection",
			remoteName:     "remote\n[malicious]\ntype=s3",
			remotePath:     "/photos",
			description:    "Newline injection to add malicious config",
			severity:       "HIGH",
			checkSavedFile: true,
		},
		{
			name:           "QuoteEscaping",
			remoteName:     `test\"escape`,
			remotePath:     "/photos",
			description:    "Quote escaping could break JSON",
			severity:       "MEDIUM",
			checkSavedFile: true,
		},
		{
			name:           "ControlCharacterInjection",
			remoteName:     "test\r\n\t",
			remotePath:     "/photos",
			description:    "Control characters should be escaped in JSON",
			severity:       "LOW",
			checkSavedFile: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			testSettingsFile := filepath.Join(tmpDir, "settings.json")

			s := DefaultSettings()
			// Use SaveTo to save to test file
			if err := s.SaveTo(testSettingsFile); err != nil {
				t.Fatalf("Failed to save initial settings: %v", err)
			}

			err := s.SetRemote(tt.remoteName, tt.remotePath)

			if err != nil {
				t.Logf("[%s] %s: Rejected: %v", tt.severity, tt.description, err)
				return
			}

			// Save again with the new values
			if err := s.SaveTo(testSettingsFile); err != nil {
				t.Logf("Could not save updated settings: %v", err)
				return
			}

			if tt.checkSavedFile {
				// Read the saved file and check for injection
				data, err := os.ReadFile(testSettingsFile)
				if err != nil {
					t.Logf("Could not read saved file: %v", err)
					return
				}

				// Try to parse it back
				var parsed Settings
				if err := json.Unmarshal(data, &parsed); err != nil {
					t.Errorf("[%s] %s: Saved file is invalid JSON: %v", tt.severity, tt.description, err)
				}

				// Check if injection succeeded
				jsonStr := string(data)
				if strings.Contains(jsonStr, "malicious") {
					t.Errorf("[%s] %s: VULNERABILITY - Injection succeeded in saved file", tt.severity, tt.description)
				}

				t.Logf("[%s] %s: Saved as: %s", tt.severity, tt.description, jsonStr)
			}
		})
	}
}

// TestInputValidation_ConcurrentWrites tests race conditions in concurrent writes
func TestInputValidation_ConcurrentWrites(t *testing.T) {
	tmpDir := t.TempDir()
	testSettingsFile := filepath.Join(tmpDir, "settings.json")

	s := DefaultSettings()
	if err := s.SaveTo(testSettingsFile); err != nil {
		t.Fatalf("Failed to save initial settings: %v", err)
	}

	// Perform concurrent writes
	const numGoroutines = 100
	done := make(chan bool, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(index int) {
			defer func() { done <- true }()

			// Each goroutine tries to update different settings
			switch index % 3 {
			case 0:
				s.SetTransfers(index)
			case 1:
				s.SetCheckers(index)
			case 2:
				s.SetReformatThreshold(float64(index) / 100.0)
			}
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < numGoroutines; i++ {
		<-done
	}

	// Try to read the file and ensure it's still valid JSON
	data, err := os.ReadFile(testSettingsFile)
	if err != nil {
		t.Errorf("Could not read settings file after concurrent writes: %v", err)
		return
	}

	var parsed Settings
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Errorf("Settings file corrupted after concurrent writes: %v", err)
		t.Logf("File contents: %s", string(data))
	} else {
		t.Logf("Settings file remains valid after %d concurrent writes", numGoroutines)
	}
}
