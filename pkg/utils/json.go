package utils

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// MarshalJSONIndent marshals a value to indented JSON.
func MarshalJSONIndent(v interface{}) ([]byte, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal JSON: %w", err)
	}
	return data, nil
}

// UnmarshalJSON unmarshals JSON data into a value.
func UnmarshalJSON(data []byte, v interface{}) error {
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("failed to unmarshal JSON: %w", err)
	}
	return nil
}

// SaveJSON marshals a value to JSON and saves it atomically to a file.
// Ensures the parent directory exists before writing.
func SaveJSON(filePath string, v interface{}, perm os.FileMode) error {
	// Ensure parent directory exists
	if err := EnsureDir(filepath.Dir(filePath), 0755); err != nil {
		return err
	}

	data, err := MarshalJSONIndent(v)
	if err != nil {
		return err
	}

	return AtomicWrite(filePath, data, perm)
}

// LoadJSON loads JSON from a file and unmarshals it into a value.
// Returns the provided default value if the file doesn't exist.
func LoadJSON(filePath string, v interface{}, defaultValue interface{}) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			// Copy default value to v
			if defaultValue != nil {
				defaultData, err := json.Marshal(defaultValue)
				if err != nil {
					return fmt.Errorf("failed to marshal default value: %w", err)
				}
				return UnmarshalJSON(defaultData, v)
			}
			return nil // No default, return empty
		}
		return fmt.Errorf("failed to read file: %w", err)
	}

	return UnmarshalJSON(data, v)
}

// LoadJSONOrDefault loads JSON from a file, or returns the default value if the file doesn't exist.
// This is a convenience function that returns the value directly.
func LoadJSONOrDefault(filePath string, defaultValue interface{}) (interface{}, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return defaultValue, nil
		}
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	var result interface{}
	if err := UnmarshalJSON(data, &result); err != nil {
		return nil, err
	}

	return result, nil
}
