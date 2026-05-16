package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/denysvitali/pictures-sync-s3/pkg/settings"
)

func TestTailscaleUpArgsDefaultsToSSHWithoutAcceptingDNS(t *testing.T) {
	got := tailscaleUpArgs("")
	want := []string{"--ssh", "--accept-dns=false"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("tailscaleUpArgs() = %#v, want %#v", got, want)
	}
}

func TestTailscaleUpArgsAddsAcceptDNSFalse(t *testing.T) {
	got := tailscaleUpArgs("--ssh --advertise-tags=tag:camera")
	want := []string{"--ssh", "--advertise-tags=tag:camera", "--accept-dns=false"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("tailscaleUpArgs() = %#v, want %#v", got, want)
	}
}

func TestTailscaleUpArgsPreservesExplicitAcceptDNS(t *testing.T) {
	got := tailscaleUpArgs("--ssh --accept-dns=true")
	want := []string{"--ssh", "--accept-dns=true"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("tailscaleUpArgs() = %#v, want %#v", got, want)
	}
}

func TestCandidateAuthKeyPathsDeduplicatesConfiguredPath(t *testing.T) {
	got := candidateAuthKeyPaths(settings.TailscaleAuthKeyFile)
	want := []string{
		settings.TailscaleAuthKeyFile,
		settings.LegacyTailscaleAuthKeyFile,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("candidateAuthKeyPaths() = %#v, want %#v", got, want)
	}
}

func TestReadAuthKeyFallsBackToLaterPath(t *testing.T) {
	dir := t.TempDir()
	missingPath := filepath.Join(dir, "missing")
	legacyPath := filepath.Join(dir, "auth_key")
	if err := os.WriteFile(legacyPath, []byte("tskey-auth-test123\n"), 0600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	key, path, err := readAuthKey([]string{missingPath, legacyPath})
	if err != nil {
		t.Fatalf("readAuthKey() error = %v", err)
	}
	if key != "tskey-auth-test123" {
		t.Fatalf("key = %q, want %q", key, "tskey-auth-test123")
	}
	if path != legacyPath {
		t.Fatalf("path = %q, want %q", path, legacyPath)
	}
}

func TestReadAuthKeySkipsEmptyPath(t *testing.T) {
	dir := t.TempDir()
	emptyPath := filepath.Join(dir, "authkey")
	legacyPath := filepath.Join(dir, "auth_key")
	if err := os.WriteFile(emptyPath, []byte("\n"), 0600); err != nil {
		t.Fatalf("WriteFile(empty) error = %v", err)
	}
	if err := os.WriteFile(legacyPath, []byte("tskey-auth-test123\n"), 0600); err != nil {
		t.Fatalf("WriteFile(legacy) error = %v", err)
	}

	key, path, err := readAuthKey([]string{emptyPath, legacyPath})
	if err != nil {
		t.Fatalf("readAuthKey() error = %v", err)
	}
	if key != "tskey-auth-test123" {
		t.Fatalf("key = %q, want %q", key, "tskey-auth-test123")
	}
	if path != legacyPath {
		t.Fatalf("path = %q, want %q", path, legacyPath)
	}
}
