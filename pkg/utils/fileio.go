package utils

import (
	"fmt"
	"os"
)

// AtomicWrite writes data to a file atomically by writing to a temp file and renaming.
// This prevents partial writes if the process is interrupted.
func AtomicWrite(filePath string, data []byte, perm os.FileMode) error {
	tmpFile := filePath + ".tmp"

	// Write to temp file
	if err := os.WriteFile(tmpFile, data, perm); err != nil {
		return fmt.Errorf("failed to write temp file: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tmpFile, filePath); err != nil {
		// Clean up temp file on rename failure
		os.Remove(tmpFile)
		return fmt.Errorf("failed to rename temp file: %w", err)
	}

	return nil
}

// ReadFileWithDefault reads a file and returns its contents, or returns the default value if the file doesn't exist.
func ReadFileWithDefault(filePath string, defaultValue []byte) ([]byte, error) {
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
