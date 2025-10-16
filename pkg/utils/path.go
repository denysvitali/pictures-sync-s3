package utils

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// ValidatePath checks if a path is safe (no directory traversal, no absolute paths).
func ValidatePath(path string) error {
	// Check for directory traversal
	if strings.Contains(path, "..") {
		return fmt.Errorf("path contains directory traversal: %s", path)
	}

	// Check for absolute paths (should be relative)
	if strings.HasPrefix(path, "/") || strings.HasPrefix(path, "\\") {
		return fmt.Errorf("path must be relative, not absolute: %s", path)
	}

	return nil
}

// SanitizePath cleans a path to remove traversal attempts and makes it relative.
func SanitizePath(path string) string {
	// Clean the path to remove any traversal attempts
	path = filepath.Clean(path)

	// Ensure path doesn't start with / or \ (should be relative)
	path = strings.TrimPrefix(path, "/")
	path = strings.TrimPrefix(path, "\\")

	return path
}

// ValidateCardID checks if a card ID matches the expected format and is safe to use in paths.
// Expected formats:
//   - card-XXXXXXXXXXXXXXXX (16 hex chars)
//   - card-NNNNNNNNNN (10+ digit timestamp)
func ValidateCardID(cardID string) error {
	if cardID == "" {
		return fmt.Errorf("card ID cannot be empty")
	}

	// Check for path traversal attempts
	if strings.Contains(cardID, "..") || strings.Contains(cardID, "/") || strings.Contains(cardID, "\\") {
		return fmt.Errorf("card ID contains invalid characters")
	}

	// Ensure card ID matches expected format
	validCardID := regexp.MustCompile(`^card-[a-fA-F0-9]{16}$|^card-[0-9]{10,}$`)
	if !validCardID.MatchString(cardID) {
		return fmt.Errorf("card ID format invalid, expected: card-XXXXXXXXXXXXXXXX (16 hex chars) or card-NNNNNNNNNN (timestamp)")
	}

	return nil
}

// JoinPathSafe joins path elements and validates the result is safe.
func JoinPathSafe(base string, elem ...string) (string, error) {
	// Join all elements
	result := filepath.Join(append([]string{base}, elem...)...)

	// Validate the result doesn't escape the base
	cleanResult := filepath.Clean(result)
	cleanBase := filepath.Clean(base)

	// Ensure result is within base directory
	if !strings.HasPrefix(cleanResult, cleanBase) {
		return "", fmt.Errorf("path would escape base directory: %s", result)
	}

	return result, nil
}
