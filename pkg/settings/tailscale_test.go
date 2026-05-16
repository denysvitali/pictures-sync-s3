package settings

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveTailscaleAuthKeyTo(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tailscale", "auth_key")
	authKey := "tskey-auth-test123"

	if err := SaveTailscaleAuthKeyTo(path, authKey); err != nil {
		t.Fatalf("SaveTailscaleAuthKeyTo() error = %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(data) != authKey+"\n" {
		t.Fatalf("stored auth key = %q, want %q", string(data), authKey+"\n")
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if got := info.Mode().Perm(); got != 0600 {
		t.Fatalf("stored auth key permissions = %o, want 0600", got)
	}
}

func TestSaveTailscaleAuthKeyToRejectsInvalidKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tailscale", "auth_key")

	if err := SaveTailscaleAuthKeyTo(path, "not-an-auth-key"); err == nil {
		t.Fatal("SaveTailscaleAuthKeyTo() error = nil, want error")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("auth key file exists after invalid save, stat error = %v", err)
	}
}

func TestHasTailscaleAuthKeyAt(t *testing.T) {
	dir := t.TempDir()
	missingPath := filepath.Join(dir, "missing")
	if configured, err := HasTailscaleAuthKeyAt(missingPath); err != nil || configured {
		t.Fatalf("HasTailscaleAuthKeyAt(missing) = %v, %v; want false, nil", configured, err)
	}

	emptyPath := filepath.Join(dir, "empty")
	if err := os.WriteFile(emptyPath, []byte("\n"), 0600); err != nil {
		t.Fatalf("WriteFile(empty) error = %v", err)
	}
	if configured, err := HasTailscaleAuthKeyAt(emptyPath); err != nil || configured {
		t.Fatalf("HasTailscaleAuthKeyAt(empty) = %v, %v; want false, nil", configured, err)
	}

	keyPath := filepath.Join(dir, "auth_key")
	if err := os.WriteFile(keyPath, []byte("tskey-auth-test123\n"), 0600); err != nil {
		t.Fatalf("WriteFile(auth key) error = %v", err)
	}
	if configured, err := HasTailscaleAuthKeyAt(keyPath); err != nil || !configured {
		t.Fatalf("HasTailscaleAuthKeyAt(auth key) = %v, %v; want true, nil", configured, err)
	}
}

func TestHasTailscaleAuthKeyChecksLegacyPath(t *testing.T) {
	dir := t.TempDir()
	canonicalPath := filepath.Join(dir, "authkey")
	legacyPath := filepath.Join(dir, "auth_key")

	configured, err := hasTailscaleAuthKey(canonicalPath, legacyPath)
	if err != nil || configured {
		t.Fatalf("hasTailscaleAuthKey(missing) = %v, %v; want false, nil", configured, err)
	}

	if err := os.WriteFile(legacyPath, []byte("tskey-auth-test123\n"), 0600); err != nil {
		t.Fatalf("WriteFile(legacy) error = %v", err)
	}

	configured, err = hasTailscaleAuthKey(canonicalPath, legacyPath)
	if err != nil || !configured {
		t.Fatalf("hasTailscaleAuthKey(legacy) = %v, %v; want true, nil", configured, err)
	}
}
