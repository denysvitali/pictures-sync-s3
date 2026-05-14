package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetupPersistentLog_RotatesOversizedLog(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "app.log")
	rotatedPath := filepath.Join(dir, "app.log.1")

	original := strings.Repeat("x", 200)
	if err := os.WriteFile(logPath, []byte(original), 0o644); err != nil {
		t.Fatalf("seed log: %v", err)
	}

	f, err := setupPersistentLog(dir, "app.log", "app.log.1", 100)
	if err != nil {
		t.Fatalf("setupPersistentLog: %v", err)
	}
	defer f.Close()

	rotated, err := os.ReadFile(rotatedPath)
	if err != nil {
		t.Fatalf("rotated log missing: %v", err)
	}
	if string(rotated) != original {
		t.Fatalf("rotated log content mismatch: got %d bytes, want %d", len(rotated), len(original))
	}

	info, err := os.Stat(logPath)
	if err != nil {
		t.Fatalf("active log missing after rotation: %v", err)
	}
	if info.Size() != 0 {
		t.Fatalf("active log should be empty after rotation, got %d bytes", info.Size())
	}

	if _, err := f.WriteString("hello\n"); err != nil {
		t.Fatalf("write to active log: %v", err)
	}
}

func TestSetupPersistentLog_OverwritesExistingRotation(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "app.log")
	rotatedPath := filepath.Join(dir, "app.log.1")

	if err := os.WriteFile(rotatedPath, []byte("old-rotated"), 0o644); err != nil {
		t.Fatalf("seed rotated: %v", err)
	}
	newContent := strings.Repeat("y", 200)
	if err := os.WriteFile(logPath, []byte(newContent), 0o644); err != nil {
		t.Fatalf("seed log: %v", err)
	}

	f, err := setupPersistentLog(dir, "app.log", "app.log.1", 100)
	if err != nil {
		t.Fatalf("setupPersistentLog: %v", err)
	}
	defer f.Close()

	rotated, err := os.ReadFile(rotatedPath)
	if err != nil {
		t.Fatalf("rotated log missing: %v", err)
	}
	if string(rotated) != newContent {
		t.Fatalf("rotated log should be overwritten with new content, got %q", string(rotated))
	}
}

func TestSetupPersistentLog_BelowThresholdNoRotation(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "app.log")
	rotatedPath := filepath.Join(dir, "app.log.1")

	small := "small content"
	if err := os.WriteFile(logPath, []byte(small), 0o644); err != nil {
		t.Fatalf("seed log: %v", err)
	}

	f, err := setupPersistentLog(dir, "app.log", "app.log.1", 1024)
	if err != nil {
		t.Fatalf("setupPersistentLog: %v", err)
	}
	defer f.Close()

	if _, err := os.Stat(rotatedPath); !os.IsNotExist(err) {
		t.Fatalf("rotated log should not exist when under threshold, stat err=%v", err)
	}

	got, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("active log missing: %v", err)
	}
	if !strings.HasPrefix(string(got), small) {
		t.Fatalf("active log should retain previous content, got %q", string(got))
	}
}

func TestSetupPersistentLog_CreatesMissingDir(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "a", "b", "c")

	f, err := setupPersistentLog(nested, "app.log", "app.log.1", 1024)
	if err != nil {
		t.Fatalf("setupPersistentLog: %v", err)
	}
	defer f.Close()

	if _, err := os.Stat(filepath.Join(nested, "app.log")); err != nil {
		t.Fatalf("expected log file to be created in nested dir: %v", err)
	}
}

func TestSetupPersistentLog_UnwritableDirReturnsError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permission checks")
	}
	root := t.TempDir()
	readOnly := filepath.Join(root, "ro")
	if err := os.Mkdir(readOnly, 0o555); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	target := filepath.Join(readOnly, "logs")

	if _, err := setupPersistentLog(target, "app.log", "app.log.1", 1024); err == nil {
		t.Fatalf("expected error when log dir cannot be created under read-only parent")
	}
}
