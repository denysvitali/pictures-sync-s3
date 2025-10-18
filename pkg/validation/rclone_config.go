package validation

import (
	"bufio"
	"bytes"
	"fmt"
	"regexp"
	"strings"
)

const (
	// MaxConfigSize limits config file size to 1MB
	MaxConfigSize = 1024 * 1024

	// MaxSectionNameLength limits section names to prevent DoS
	MaxSectionNameLength = 256

	// MaxKeyLength limits key names to prevent DoS
	MaxKeyLength = 256

	// MaxValueLength limits value length to prevent DoS
	MaxValueLength = 8192

	// MaxSections limits number of sections to prevent DoS
	MaxSections = 100

	// MaxKeysPerSection limits keys per section to prevent DoS
	MaxKeysPerSection = 100
)

// Validation errors
var (
	ErrConfigTooLarge       = fmt.Errorf("config file exceeds maximum size of %d bytes", MaxConfigSize)
	ErrTooManySections      = fmt.Errorf("config has too many sections (max %d)", MaxSections)
	ErrTooManyKeys          = fmt.Errorf("section has too many keys (max %d)", MaxKeysPerSection)
	ErrInvalidINIFormat     = fmt.Errorf("invalid INI format")
	ErrSectionNameTooLong   = fmt.Errorf("section name exceeds maximum length of %d", MaxSectionNameLength)
	ErrKeyNameTooLong       = fmt.Errorf("key name exceeds maximum length of %d", MaxKeyLength)
	ErrValueTooLong         = fmt.Errorf("value exceeds maximum length of %d", MaxValueLength)
	ErrMissingRequiredField = fmt.Errorf("missing required field")
	ErrSuspiciousContent    = fmt.Errorf("config contains suspicious content")
	ErrEmptyConfig          = fmt.Errorf("config file is empty")
	ErrNoValidSections      = fmt.Errorf("config has no valid remote sections")
)

// Suspicious patterns that could indicate malicious configs
var suspiciousPatterns = []*regexp.Regexp{
	// Command injection attempts
	regexp.MustCompile("(?i)(\\$\\(|`|&&|\\|\\||;|\\|)"),

	// Path traversal attempts
	regexp.MustCompile(`\.\./`),

	// Environment variable injection
	regexp.MustCompile(`(?i)\$\{.*\}`),

	// Script execution attempts
	regexp.MustCompile(`(?i)(bash|sh|python|perl|ruby|node|exec|eval)\s`),

	// URL schemes that could be used for SSRF (excluding legitimate storage URLs)
	regexp.MustCompile(`(?i)^(file|gopher|dict|ftp|tftp|ldap)://`),

	// Null bytes
	regexp.MustCompile(`\x00`),
}

// Known legitimate URL patterns that are allowed
var legitimateURLPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)^https?://`), // HTTP/HTTPS
	regexp.MustCompile(`(?i)^s3://`),     // S3
	regexp.MustCompile(`(?i)\.amazonaws\.com`),
	regexp.MustCompile(`(?i)\.backblazeb2\.com`),
	regexp.MustCompile(`(?i)\.wasabisys\.com`),
	regexp.MustCompile(`(?i)\.digitaloceanspaces\.com`),
	regexp.MustCompile(`(?i)\.r2\.cloudflarestorage\.com`),
}

// RcloneConfig represents a parsed rclone configuration
type RcloneConfig struct {
	Sections map[string]Section
}

// Section represents a configuration section
type Section struct {
	Name   string
	Fields map[string]string
}

// ValidationResult contains validation details
type ValidationResult struct {
	Valid    bool
	Errors   []error
	Warnings []string
	Remotes  []string
}

// ValidateRcloneConfig performs comprehensive validation of rclone configuration
func ValidateRcloneConfig(data []byte) (*ValidationResult, error) {
	result := &ValidationResult{
		Valid:    true,
		Errors:   make([]error, 0),
		Warnings: make([]string, 0),
		Remotes:  make([]string, 0),
	}

	// Check size limit
	if len(data) > MaxConfigSize {
		result.Valid = false
		result.Errors = append(result.Errors, ErrConfigTooLarge)
		return result, ErrConfigTooLarge
	}

	// Check for empty config
	if len(bytes.TrimSpace(data)) == 0 {
		result.Valid = false
		result.Errors = append(result.Errors, ErrEmptyConfig)
		return result, ErrEmptyConfig
	}

	// Parse INI format
	config, err := parseINI(data)
	if err != nil {
		result.Valid = false
		result.Errors = append(result.Errors, err)
		return result, err
	}

	// Check for too many sections
	if len(config.Sections) > MaxSections {
		result.Valid = false
		result.Errors = append(result.Errors, ErrTooManySections)
		return result, ErrTooManySections
	}

	// Validate each section
	validRemoteCount := 0
	for _, section := range config.Sections {
		if err := validateSection(section); err != nil {
			result.Valid = false
			result.Errors = append(result.Errors, fmt.Errorf("section [%s]: %w", section.Name, err))
			continue
		}

		// Check for suspicious content in section
		if warnings := checkSuspiciousContent(section); len(warnings) > 0 {
			result.Warnings = append(result.Warnings, warnings...)
		}

		// Validate required fields for rclone remotes
		if err := validateRemoteFields(section); err != nil {
			result.Errors = append(result.Errors, fmt.Errorf("section [%s]: %w", section.Name, err))
		} else {
			validRemoteCount++
			result.Remotes = append(result.Remotes, section.Name)
		}
	}

	// Ensure at least one valid remote section
	if validRemoteCount == 0 {
		result.Valid = false
		result.Errors = append(result.Errors, ErrNoValidSections)
	}

	// If we have errors, mark as invalid
	if len(result.Errors) > 0 {
		result.Valid = false
	}

	return result, nil
}

// parseINI parses INI format data into RcloneConfig
func parseINI(data []byte) (*RcloneConfig, error) {
	config := &RcloneConfig{
		Sections: make(map[string]Section),
	}

	scanner := bufio.NewScanner(bytes.NewReader(data))
	var currentSection *Section
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}

		// Check for section header [name]
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			sectionName := strings.TrimSpace(line[1 : len(line)-1])

			// Validate section name length
			if len(sectionName) > MaxSectionNameLength {
				return nil, fmt.Errorf("line %d: %w", lineNum, ErrSectionNameTooLong)
			}

			// Validate section name format (alphanumeric, dash, underscore)
			if !isValidSectionName(sectionName) {
				return nil, fmt.Errorf("line %d: invalid section name format", lineNum)
			}

			currentSection = &Section{
				Name:   sectionName,
				Fields: make(map[string]string),
			}
			config.Sections[sectionName] = *currentSection
			continue
		}

		// Parse key = value
		if currentSection == nil {
			return nil, fmt.Errorf("line %d: key-value pair outside of section", lineNum)
		}

		key, value, found := strings.Cut(line, "=")
		if !found {
			return nil, fmt.Errorf("line %d: invalid key-value format", lineNum)
		}

		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)

		// Validate key and value lengths
		if len(key) > MaxKeyLength {
			return nil, fmt.Errorf("line %d: %w", lineNum, ErrKeyNameTooLong)
		}
		if len(value) > MaxValueLength {
			return nil, fmt.Errorf("line %d: %w", lineNum, ErrValueTooLong)
		}

		// Validate key format
		if !isValidKeyName(key) {
			return nil, fmt.Errorf("line %d: invalid key name format", lineNum)
		}

		currentSection.Fields[key] = value
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading config: %w", err)
	}

	return config, nil
}

// validateSection checks section-level constraints
func validateSection(section Section) error {
	if len(section.Fields) > MaxKeysPerSection {
		return ErrTooManyKeys
	}
	return nil
}

// validateRemoteFields checks for required rclone remote fields
func validateRemoteFields(section Section) error {
	// All rclone remotes must have a 'type' field
	remoteType, hasType := section.Fields["type"]
	if !hasType {
		return fmt.Errorf("%w: missing 'type' field", ErrMissingRequiredField)
	}

	// Validate type is a known rclone backend
	if !isValidRemoteType(remoteType) {
		return fmt.Errorf("unknown remote type: %s", remoteType)
	}

	return nil
}

// checkSuspiciousContent scans for malicious patterns
func checkSuspiciousContent(section Section) []string {
	warnings := make([]string, 0)

	for key, value := range section.Fields {
		// Check for suspicious patterns
		for _, pattern := range suspiciousPatterns {
			if pattern.MatchString(value) {
				// Check if it's a legitimate URL pattern
				isLegitimate := false
				for _, legitPattern := range legitimateURLPatterns {
					if legitPattern.MatchString(value) {
						isLegitimate = true
						break
					}
				}

				if !isLegitimate {
					warnings = append(warnings,
						fmt.Sprintf("suspicious pattern in [%s] %s: matches %s",
							section.Name, key, pattern.String()))
				}
			}
		}

		// Check for excessively long values that might indicate injection
		if len(value) > 1000 && !strings.HasPrefix(key, "token") && !strings.HasPrefix(key, "password") {
			warnings = append(warnings,
				fmt.Sprintf("unusually long value in [%s] %s: %d bytes",
					section.Name, key, len(value)))
		}
	}

	return warnings
}

// isValidSectionName checks if section name uses safe characters
func isValidSectionName(name string) bool {
	// Allow alphanumeric, dash, underscore, and dot
	matched, _ := regexp.MatchString(`^[a-zA-Z0-9_.-]+$`, name)
	return matched
}

// isValidKeyName checks if key name uses safe characters
func isValidKeyName(name string) bool {
	// Allow alphanumeric, dash, and underscore
	matched, _ := regexp.MatchString(`^[a-zA-Z0-9_-]+$`, name)
	return matched
}

// isValidRemoteType checks if the remote type is a known rclone backend
func isValidRemoteType(remoteType string) bool {
	validTypes := map[string]bool{
		// Cloud storage providers
		"s3":                     true,
		"b2":                     true,
		"gcs":                    true,
		"google cloud storage":   true, // Alternative name for GCS
		"azureblob":              true,
		"swift":                  true,
		"drive":                  true,
		"onedrive":               true,
		"dropbox":                true,
		"box":                    true,
		"googlephotos":           true,
		"google photos":          true, // Alternative name
		"mega":                   true,
		"pcloud":                 true,
		"koofr":                  true,
		"putio":                  true,
		"premiumizeme":           true,
		"sugarsync":              true,
		"jottacloud":             true,
		"seafile":                true,
		"storj":                  true,
		"fichier":                true,
		"qingstor":               true,
		"internetarchive":        true,

		// Network/local storage
		"sftp":                   true,
		"ftp":                    true,
		"ftps":                   true,
		"webdav":                 true,
		"http":                   true,

		// Virtual/special
		"crypt":                  true,
		"compress":               true,
		"chunker":                true,
		"union":                  true,
		"cache":                  true,
		"combine":                true,
		"alias":                  true,

		// Enterprise
		"hdfs":                   true,
		"smb":                    true,
	}

	return validTypes[strings.ToLower(remoteType)]
}

// SanitizeConfig performs additional sanitization on validated config
func SanitizeConfig(data []byte) []byte {
	// Remove any null bytes
	data = bytes.ReplaceAll(data, []byte{0}, []byte{})

	// Normalize line endings to Unix-style
	data = bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))
	data = bytes.ReplaceAll(data, []byte("\r"), []byte("\n"))

	return data
}
