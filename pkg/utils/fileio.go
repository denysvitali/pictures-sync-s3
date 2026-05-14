package utils

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
)

// AtomicWrite writes data to a file atomically by writing to a temp file and renaming.
// This prevents partial writes if the process is interrupted.
//
// To survive power loss on filesystems like ext4, the temp file is fsynced
// before the rename and the parent directory is fsynced afterward so the
// rename itself is durable on disk.
func AtomicWrite(filePath string, data []byte, perm os.FileMode) error {
	tmpFile := filePath + ".tmp"

	// Write data to temp file and fsync it before the rename so the bytes are
	// on disk by the time the rename is durable.
	if err := writeAndSync(tmpFile, data, perm); err != nil {
		os.Remove(tmpFile)
		return err
	}

	if err := os.Rename(tmpFile, filePath); err != nil {
		os.Remove(tmpFile)
		return fmt.Errorf("failed to rename temp file: %w", err)
	}

	syncDir(filepath.Dir(filePath))

	return nil
}

// writeAndSync writes data to path with perm and fsyncs the file before close.
func writeAndSync(path string, data []byte, perm os.FileMode) error {
	// #nosec G304 -- path is a controlled application path
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return fmt.Errorf("failed to open temp file: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return fmt.Errorf("failed to write temp file: %w", err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return fmt.Errorf("failed to sync temp file: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("failed to close temp file: %w", err)
	}
	return nil
}

// syncDir fsyncs the directory at dir. Errors are logged as warnings rather
// than returned because some filesystems (tmpfs, certain network mounts) do
// not support directory fsync and we still want the write to count as a
// success in that case.
func syncDir(dir string) {
	// #nosec G304 -- dir is the parent of a controlled application path
	d, err := os.Open(dir)
	if err != nil {
		log.Printf("AtomicWrite: warning: open parent dir %q for fsync failed: %v", dir, err)
		return
	}
	if err := d.Sync(); err != nil {
		log.Printf("AtomicWrite: warning: fsync parent dir %q failed: %v", dir, err)
	}
	if err := d.Close(); err != nil {
		log.Printf("AtomicWrite: warning: close parent dir %q failed: %v", dir, err)
	}
}

// ReadFileWithDefault reads a file and returns its contents, or returns the default value if the file doesn't exist.
func ReadFileWithDefault(filePath string, defaultValue []byte) ([]byte, error) {
	// #nosec G304 -- filePath is a controlled path set by the application
	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return defaultValue, nil
		}
		return nil, fmt.Errorf("failed to read file: %w", err)
	}
	return data, nil
}

// FileExists checks if a file or directory exists.
func FileExists(filePath string) bool {
	_, err := os.Stat(filePath)
	return err == nil
}

// EnsureDir creates a directory and all parent directories if they don't exist.
func EnsureDir(dirPath string, perm os.FileMode) error {
	if err := os.MkdirAll(dirPath, perm); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}
	return nil
}
