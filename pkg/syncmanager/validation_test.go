package syncmanager

import (
	"testing"
)

func TestValidateCardID(t *testing.T) {
	tests := []struct {
		name    string
		cardID  string
		wantErr bool
	}{
		{
			name:    "Valid card ID",
			cardID:  "card-12345678",
			wantErr: false,
		},
		{
			name:    "Valid card ID with letters",
			cardID:  "card-abcd1234",
			wantErr: false,
		},
		{
			name:    "Empty card ID",
			cardID:  "",
			wantErr: true,
		},
		{
			name:    "Path traversal attempt with ..",
			cardID:  "card-../../etc",
			wantErr: true,
		},
		{
			name:    "Path traversal with forward slash",
			cardID:  "card-abc/def",
			wantErr: true,
		},
		{
			name:    "Path traversal with backslash",
			cardID:  "card-abc\\def",
			wantErr: true,
		},
		{
			name:    "Invalid format - too short",
			cardID:  "card-123",
			wantErr: true,
		},
		{
			name:    "Invalid format - too long",
			cardID:  "card-123456789",
			wantErr: true,
		},
		{
			name:    "Invalid format - no prefix",
			cardID:  "12345678",
			wantErr: true,
		},
		{
			name:    "Invalid format - wrong prefix",
			cardID:  "disk-12345678",
			wantErr: true,
		},
		{
			name:    "Contains special characters",
			cardID:  "card-12!@#$%^",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateCardID(tt.cardID)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateCardID(%q) error = %v, wantErr %v", tt.cardID, err, tt.wantErr)
			}
		})
	}
}

func TestIsRetryableError(t *testing.T) {
	tests := []struct {
		name  string
		err   error
		want  bool
	}{
		{
			name:  "Nil error",
			err:   nil,
			want:  false,
		},
		// Add more test cases as needed
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isRetryableError(tt.err); got != tt.want {
				t.Errorf("isRetryableError() = %v, want %v", got, tt.want)
			}
		})
	}
}