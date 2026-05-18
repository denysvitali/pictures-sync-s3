package auth

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPasswordManagerChangePassword(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gokr-pw.txt")
	if err := os.WriteFile(path, []byte("old-password\n"), 0600); err != nil {
		t.Fatalf("write password file: %v", err)
	}

	manager, err := NewPasswordManager(path, "dev-password")
	if err != nil {
		t.Fatalf("NewPasswordManager() error = %v", err)
	}

	if got := manager.CurrentPassword(); got != "old-password" {
		t.Fatalf("CurrentPassword() = %q, want old-password", got)
	}
	if err := manager.ChangePassword("wrong-password", "new-password"); err != ErrCurrentPasswordInvalid {
		t.Fatalf("ChangePassword() error = %v, want ErrCurrentPasswordInvalid", err)
	}
	if err := manager.ChangePassword("old-password", "new-password"); err != nil {
		t.Fatalf("ChangePassword() error = %v", err)
	}
	if got := manager.CurrentPassword(); got != "new-password" {
		t.Fatalf("CurrentPassword() = %q, want new-password", got)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read password file: %v", err)
	}
	if string(data) != "new-password\n" {
		t.Fatalf("password file = %q, want new-password newline", string(data))
	}
}

func TestPasswordManagerChangePasswordConstantTime(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gokr-pw.txt")
	if err := os.WriteFile(path, []byte("photo-backup\n"), 0600); err != nil {
		t.Fatalf("write password file: %v", err)
	}

	manager, err := NewPasswordManager(path, "dev-password")
	if err != nil {
		t.Fatalf("NewPasswordManager() error = %v", err)
	}

	cases := []struct {
		name    string
		attempt string
	}{
		{name: "empty", attempt: ""},
		{name: "shorter", attempt: "photo"},
		{name: "longer", attempt: "photo-backup-extra"},
		{name: "different same length", attempt: "photo-bakcup"},
		{name: "prefix match", attempt: "photo-backupX"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := manager.ChangePassword(tc.attempt, "new-password-1"); err != ErrCurrentPasswordInvalid {
				t.Fatalf("ChangePassword(%q) error = %v, want ErrCurrentPasswordInvalid", tc.attempt, err)
			}
			if got := manager.CurrentPassword(); got != "photo-backup" {
				t.Fatalf("CurrentPassword() = %q, want photo-backup (must be unchanged)", got)
			}
		})
	}

	if err := manager.ChangePassword("photo-backup", "new-password-1"); err != nil {
		t.Fatalf("ChangePassword(correct) error = %v", err)
	}
}

func TestValidateGokrazyPassword(t *testing.T) {
	tests := []struct {
		name     string
		password string
		wantErr  bool
	}{
		{name: "valid", password: "photo-backup-2"},
		{name: "too short", password: "short", wantErr: true},
		{name: "leading whitespace", password: " photo-backup-2", wantErr: true},
		{name: "newline", password: "photo\nbackup", wantErr: true},
		{name: "non-ascii", password: "photo-backup-é", wantErr: true},
		{name: "emoji", password: "photo-backup-🔒", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateGokrazyPassword(tt.password)
			if tt.wantErr && err == nil {
				t.Fatal("ValidateGokrazyPassword() expected error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("ValidateGokrazyPassword() error = %v", err)
			}
		})
	}
}
