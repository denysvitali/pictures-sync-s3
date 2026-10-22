// Package wifiregion manages the Wi-Fi regulatory region (ISO 3166-1 alpha-2).
//
// The region decides which channels the radio may actively use; with the
// restrictive world domain "00" the 5 GHz band cannot be joined. The effective
// region is persisted in a tiny file so early-boot services (wifi-init,
// provision-ap) can pick it up before the web UI is running.
package wifiregion

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// File holds the effective region chosen in the settings or detected from
// nearby networks. It can be modified for testing purposes.
var File = "/perm/wifi-region"

// Normalize upper-cases and validates a two-letter country code.
func Normalize(country string) (string, error) {
	country = strings.ToUpper(strings.TrimSpace(country))
	if len(country) != 2 {
		return "", fmt.Errorf("country must be a two-letter ISO 3166-1 alpha-2 code")
	}
	for _, r := range country {
		if r < 'A' || r > 'Z' {
			return "", fmt.Errorf("country must contain only letters")
		}
	}
	if country == "XX" {
		return "", fmt.Errorf("XX is not a country")
	}
	return country, nil
}

// Read returns the persisted region, or "" if none is stored or it is invalid.
func Read() string {
	// #nosec G304 -- File is a well-known config location
	data, err := os.ReadFile(File)
	if err != nil {
		return ""
	}
	country, err := Normalize(string(data))
	if err != nil {
		return ""
	}
	return country
}

// Write persists the effective region atomically.
func Write(country string) error {
	country, err := Normalize(country)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(File), 0o755); err != nil {
		return err
	}
	tmp := File + ".tmp"
	if err := os.WriteFile(tmp, []byte(country+"\n"), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, File); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// SetClientConfigRegion writes the region into a gokrazy wifi.json, which
// otherwise makes gokrazy/wifi fall back to the world domain. Other fields are
// preserved. Without force an existing non-world region is left untouched.
// It reports whether the file changed; a missing file is not an error.
func SetClientConfigRegion(path, country string, force bool) (bool, error) {
	country, err := Normalize(country)
	if err != nil {
		return false, err
	}
	// #nosec G304 -- path is the well-known gokrazy Wi-Fi config location
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	var config map[string]any
	if err := json.Unmarshal(data, &config); err != nil {
		return false, fmt.Errorf("parse %s: %w", path, err)
	}
	region, _ := config["region"].(string)
	if force {
		if region == country {
			return false, nil
		}
	} else if region != "" && region != "00" {
		return false, nil
	}
	config["region"] = country
	out, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return false, err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, out, 0o600); err != nil {
		return false, err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return false, err
	}
	return true, nil
}
