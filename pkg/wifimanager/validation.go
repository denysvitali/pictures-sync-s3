package wifimanager

import (
	"fmt"
	"unicode"
)

// ValidateWiFiPassword validates WiFi password according to WPA/WPA2 requirements
// WPA/WPA2 password requirements:
// - Minimum 8 characters
// - Maximum 63 characters
// - ASCII printable characters only
func ValidateWiFiPassword(password string) error {
	length := len(password)

	// Check minimum length
	if length < 8 {
		return fmt.Errorf("WiFi password must be at least 8 characters (WPA/WPA2 requirement)")
	}

	// Check maximum length
	if length > 63 {
		return fmt.Errorf("WiFi password cannot exceed 63 characters (WPA/WPA2 limitation)")
	}

	// Check for invalid characters
	// WPA/WPA2 accepts ASCII printable characters (32-126)
	for i, r := range password {
		// Check if character is printable ASCII
		if r < 32 || r > 126 {
			return fmt.Errorf("WiFi password contains invalid character at position %d (only ASCII printable characters allowed)", i)
		}

		// Specifically reject control characters
		if unicode.IsControl(r) {
			return fmt.Errorf("WiFi password cannot contain control characters")
		}
	}

	return nil
}
