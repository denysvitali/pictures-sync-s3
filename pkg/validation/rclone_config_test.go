package validation

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

func TestValidateRcloneConfig_ValidConfigs(t *testing.T) {
	tests := []struct {
		name        string
		config      string
		wantRemotes []string
	}{
		{
			name: "valid S3 config",
			config: `[s3backup]
type = s3
provider = AWS
access_key_id = AKIAIOSFODNN7EXAMPLE
secret_access_key = wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY
region = us-east-1
`,
			wantRemotes: []string{"s3backup"},
		},
		{
			name: "valid Backblaze B2 config",
			config: `[b2backup]
type = b2
account = 0123456789abcdef0123456789abcdef01234567
key = K001abcdefghijklmnopqrstuvwxyz123456789AB
hard_delete = false
`,
			wantRemotes: []string{"b2backup"},
		},
		{
			name: "multiple remotes",
			config: `[s3backup]
type = s3
provider = AWS
access_key_id = test123
secret_access_key = secret123
region = us-west-2

[b2backup]
type = b2
account = testaccount
key = testkey
`,
			wantRemotes: []string{"s3backup", "b2backup"},
		},
		{
			name: "config with comments",
			config: `# This is a comment
[s3backup]
# Another comment
type = s3
provider = AWS
access_key_id = test123
; Semicolon comment
secret_access_key = secret123
region = us-east-1
`,
			wantRemotes: []string{"s3backup"},
		},
		{
			name: "config with spaces",
			config: `[s3backup]
type = s3
provider    =    AWS
access_key_id=test123
secret_access_key = secret123
region = us-east-1
`,
			wantRemotes: []string{"s3backup"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ValidateRcloneConfig([]byte(tt.config))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !result.Valid {
				t.Errorf("config should be valid, errors: %v", result.Errors)
			}
			if len(result.Remotes) != len(tt.wantRemotes) {
				t.Errorf("got %d remotes, want %d", len(result.Remotes), len(tt.wantRemotes))
			}
		})
	}
}

func TestValidateRcloneConfig_InvalidConfigs(t *testing.T) {
	tests := []struct {
		name        string
		config      string
		wantInvalid bool
		description string
	}{
		{
			name:        "empty config",
			config:      "",
			wantInvalid: true,
			description: "should reject empty config",
		},
		{
			name:        "whitespace only",
			config:      "   \n\t\n  ",
			wantInvalid: true,
			description: "should reject whitespace-only config",
		},
		{
			name: "missing type field",
			config: `[s3backup]
provider = AWS
access_key_id = test
`,
			wantInvalid: true,
			description: "should reject config without type field",
		},
		{
			name: "invalid type",
			config: `[malicious]
type = /bin/bash
command = rm -rf /
`,
			wantInvalid: true,
			description: "should reject invalid remote type",
		},
		{
			name: "key outside section",
			config: `type = s3
[s3backup]
provider = AWS
`,
			wantInvalid: true,
			description: "should reject key-value pair outside section",
		},
		{
			name: "invalid section name",
			config: `[s3; rm -rf /]
type = s3
`,
			wantInvalid: true,
			description: "should reject section name with special characters",
		},
		{
			name: "section name too long",
			config: `[` + strings.Repeat("a", MaxSectionNameLength+1) + `]
type = s3
`,
			wantInvalid: true,
			description: "should reject overly long section names",
		},
		{
			name: "key too long",
			config: `[s3backup]
type = s3
` + strings.Repeat("a", MaxKeyLength+1) + ` = value
`,
			wantInvalid: true,
			description: "should reject overly long key names",
		},
		{
			name: "value too long",
			config: `[s3backup]
type = s3
key = ` + strings.Repeat("a", MaxValueLength+1),
			wantInvalid: true,
			description: "should reject overly long values",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, _ := ValidateRcloneConfig([]byte(tt.config))
			if tt.wantInvalid && (result == nil || result.Valid) {
				t.Errorf("config should be invalid: %s", tt.description)
			}
		})
	}
}

func TestValidateRcloneConfig_SuspiciousContent(t *testing.T) {
	tests := []struct {
		name           string
		config         string
		expectWarnings bool
		expectInvalid  bool
		description    string
	}{
		{
			name: "command injection attempt",
			config: `[malicious]
type = s3
endpoint = https://example.com
access_key_id = test; rm -rf /
`,
			expectWarnings: true,
			expectInvalid:  false,
			description:    "should warn about command injection patterns",
		},
		{
			name: "path traversal attempt",
			config: `[malicious]
type = s3
endpoint = ../../../etc/passwd
`,
			expectWarnings: true,
			expectInvalid:  false,
			description:    "should warn about path traversal",
		},
		{
			name: "environment variable injection",
			config: `[malicious]
type = s3
access_key_id = ${AWS_SECRET}
`,
			expectWarnings: true,
			expectInvalid:  false,
			description:    "should warn about environment variable injection",
		},
		{
			name: "backtick execution",
			config: `[malicious]
type = s3
endpoint = ` + "`rm -rf /`",
			expectWarnings: true,
			expectInvalid:  false,
			description:    "should warn about backtick command execution",
		},
		{
			name: "file URL scheme",
			config: `[malicious]
type = s3
endpoint = file:///etc/passwd
`,
			expectWarnings: true,
			expectInvalid:  false,
			description:    "should warn about file:// URL scheme",
		},
		{
			name: "legitimate HTTPS URL",
			config: `[s3backup]
type = s3
endpoint = https://s3.amazonaws.com
`,
			expectWarnings: false,
			expectInvalid:  false,
			description:    "should not warn about legitimate HTTPS URLs",
		},
		{
			name: "legitimate Backblaze URL",
			config: `[b2backup]
type = b2
endpoint = https://s3.us-west-002.backblazeb2.com
`,
			expectWarnings: false,
			expectInvalid:  false,
			description:    "should not warn about legitimate Backblaze URLs",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ValidateRcloneConfig([]byte(tt.config))
			if tt.expectInvalid {
				if result != nil && result.Valid {
					t.Errorf("expected invalid config: %s", tt.description)
				}
			} else {
				if err != nil || result == nil || !result.Valid {
					t.Errorf("expected valid config but got invalid: %s (err=%v)", tt.description, err)
				}
				if tt.expectWarnings && len(result.Warnings) == 0 {
					t.Errorf("expected warnings but got none: %s", tt.description)
				}
				if !tt.expectWarnings && len(result.Warnings) > 0 {
					t.Errorf("unexpected warnings: %v (%s)", result.Warnings, tt.description)
				}
			}
		})
	}
}

func TestValidateRcloneConfig_SizeLimits(t *testing.T) {
	tests := []struct {
		name    string
		size    int
		wantErr error
	}{
		{
			name:    "reasonable size",
			size:    1024,
			wantErr: nil,
		},
		{
			name:    "over max size",
			size:    MaxConfigSize + 1,
			wantErr: ErrConfigTooLarge,
		},
		{
			name:    "far over max size",
			size:    MaxConfigSize * 10,
			wantErr: ErrConfigTooLarge,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a valid config with multiple short sections to avoid scanner limits
			var buf bytes.Buffer
			buf.WriteString("[s3backup]\ntype = s3\n\n")

			// Add padding as multiple small comment lines
			remaining := tt.size - buf.Len()
			lineLen := 70 // Safe line length
			for remaining > 0 {
				toWrite := lineLen
				if toWrite > remaining {
					toWrite = remaining
				}
				buf.WriteString("# " + strings.Repeat("a", toWrite-3) + "\n")
				remaining -= toWrite
			}

			result, err := ValidateRcloneConfig(buf.Bytes())
			if tt.wantErr != nil {
				if err != tt.wantErr {
					t.Errorf("got error %v, want %v", err, tt.wantErr)
				}
			} else if err != nil {
				t.Errorf("unexpected error: %v", err)
			} else if result == nil || !result.Valid {
				t.Errorf("config should be valid")
			}
		})
	}
}

func TestValidateRcloneConfig_TooManySections(t *testing.T) {
	var buf bytes.Buffer
	for i := 0; i < MaxSections+1; i++ {
		buf.WriteString(fmt.Sprintf("[remote%d]\n", i))
		buf.WriteString("type = s3\n\n")
	}

	result, err := ValidateRcloneConfig(buf.Bytes())
	if err != ErrTooManySections {
		t.Errorf("got error %v, want %v", err, ErrTooManySections)
	}
	if result != nil && result.Valid {
		t.Error("config should be invalid")
	}
}

func TestValidateRcloneConfig_TooManyKeys(t *testing.T) {
	var buf bytes.Buffer
	buf.WriteString("[s3backup]\n")
	buf.WriteString("type = s3\n")
	for i := 0; i < MaxKeysPerSection+1; i++ {
		buf.WriteString(fmt.Sprintf("key%d = value%d\n", i, i))
	}

	result, err := ValidateRcloneConfig(buf.Bytes())
	if result != nil && result.Valid {
		t.Error("config should be invalid due to too many keys")
	}
	if err == nil && (result == nil || len(result.Errors) == 0) {
		t.Error("expected error for too many keys")
	}
}

func TestSanitizeConfig(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "remove null bytes",
			input:    "type = s3\x00malicious",
			expected: "type = s3malicious",
		},
		{
			name:     "normalize CRLF to LF",
			input:    "[s3]\r\ntype = s3\r\n",
			expected: "[s3]\ntype = s3\n",
		},
		{
			name:     "normalize CR to LF",
			input:    "[s3]\rtype = s3\r",
			expected: "[s3]\ntype = s3\n",
		},
		{
			name:     "preserve LF",
			input:    "[s3]\ntype = s3\n",
			expected: "[s3]\ntype = s3\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SanitizeConfig([]byte(tt.input))
			if string(result) != tt.expected {
				t.Errorf("got %q, want %q", string(result), tt.expected)
			}
		})
	}
}

func TestIsValidRemoteType(t *testing.T) {
	tests := []struct {
		remoteType string
		want       bool
	}{
		// Valid types
		{"s3", true},
		{"b2", true},
		{"gcs", true},
		{"azureblob", true},
		{"drive", true},
		{"dropbox", true},
		{"sftp", true},
		{"ftp", true},
		{"webdav", true},
		{"crypt", true},

		// Invalid types
		{"/bin/bash", false},
		{"$(rm -rf /)", false},
		{"invalid", false},
		{"", false},
		{"s3; malicious", false},
	}

	for _, tt := range tests {
		t.Run(tt.remoteType, func(t *testing.T) {
			got := isValidRemoteType(tt.remoteType)
			if got != tt.want {
				t.Errorf("isValidRemoteType(%q) = %v, want %v", tt.remoteType, got, tt.want)
			}
		})
	}
}

func TestIsValidSectionName(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		// Valid names
		{"s3backup", true},
		{"my-backup", true},
		{"backup_2024", true},
		{"remote.backup", true},
		{"Remote123", true},

		// Invalid names
		{"remote;malicious", false},
		{"remote space", false},
		{"remote$(cmd)", false},
		{"remote`cmd`", false},
		{"remote|cmd", false},
		{"remote&cmd", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isValidSectionName(tt.name)
			if got != tt.want {
				t.Errorf("isValidSectionName(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

func TestIsValidKeyName(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		// Valid names
		{"type", true},
		{"access_key_id", true},
		{"secret-key", true},
		{"key123", true},
		{"KEY_NAME", true},

		// Invalid names
		{"key;malicious", false},
		{"key space", false},
		{"key$(cmd)", false},
		{"key=value", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isValidKeyName(tt.name)
			if got != tt.want {
				t.Errorf("isValidKeyName(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

func TestValidateRcloneConfig_RealWorldConfigs(t *testing.T) {
	tests := []struct {
		name    string
		config  string
		wantErr bool
	}{
		{
			name: "AWS S3 with all standard fields",
			config: `[aws-s3]
type = s3
provider = AWS
env_auth = false
access_key_id = AKIAIOSFODNN7EXAMPLE
secret_access_key = wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY
region = us-east-1
location_constraint =
acl = private
server_side_encryption =
storage_class =
`,
			wantErr: false,
		},
		{
			name: "Backblaze B2 standard config",
			config: `[b2]
type = b2
account = 0123456789abcdef0123456789abcdef01234567
key = K001abcdefghijklmnopqrstuvwxyz123456789AB
hard_delete = false
upload_cutoff = 200M
chunk_size = 96M
`,
			wantErr: false,
		},
		{
			name: "MinIO S3-compatible",
			config: `[minio]
type = s3
provider = Minio
env_auth = false
access_key_id = minioadmin
secret_access_key = minioadmin
endpoint = https://minio.example.com:9000
`,
			wantErr: false,
		},
		{
			name: "Wasabi",
			config: `[wasabi]
type = s3
provider = Wasabi
env_auth = false
access_key_id = WJKK1234567890ABCDEF
secret_access_key = ABCDEFghijklmnop1234567890ABCDEFGHIJKLMN
region = us-east-1
endpoint = https://s3.wasabisys.com
`,
			wantErr: false,
		},
		{
			name: "Google Cloud Storage",
			config: `[gcs]
type = google cloud storage
bucket_policy_only = true
location = us
storage_class = STANDARD
token = {"access_token":"ya29.abc123","token_type":"Bearer","refresh_token":"1//def456","expiry":"2024-01-01T00:00:00Z"}
`,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ValidateRcloneConfig([]byte(tt.config))
			if tt.wantErr {
				if err == nil && (result == nil || result.Valid) {
					t.Error("expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if result == nil || !result.Valid {
					t.Errorf("config should be valid, errors: %v", result.Errors)
				}
			}
		})
	}
}

// Benchmark validation performance
func BenchmarkValidateRcloneConfig(b *testing.B) {
	config := []byte(`[s3backup]
type = s3
provider = AWS
access_key_id = AKIAIOSFODNN7EXAMPLE
secret_access_key = wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY
region = us-east-1

[b2backup]
type = b2
account = 0123456789abcdef0123456789abcdef01234567
key = K001abcdefghijklmnopqrstuvwxyz123456789AB
`)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ValidateRcloneConfig(config)
	}
}
