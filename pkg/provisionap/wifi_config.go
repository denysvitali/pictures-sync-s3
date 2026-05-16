package provisionap

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
)

type network struct {
	SSID string `json:"ssid"`
	PSK  string `json:"psk,omitempty"`
}

type appWiFiConfig struct {
	Networks []network `json:"networks"`
}

type wifiConfigSnapshot struct {
	HasNetworks bool
	Fingerprint string
}

// HasConfiguredNetworks reports whether either supported Wi-Fi config file
// contains at least one user network.
func HasConfiguredNetworks(clientConfigPath, appConfigPath string) (bool, error) {
	snapshot, err := readWiFiConfigSnapshot(clientConfigPath, appConfigPath)
	return snapshot.HasNetworks, err
}

func readWiFiConfigSnapshot(clientConfigPath, appConfigPath string) (wifiConfigSnapshot, error) {
	var (
		fingerprintParts []string
		allErrs          []error
		hasNetworks      bool
	)

	addFile := func(kind, path string, parse func(string, []byte) ([]network, error)) {
		data, exists, err := readConfigFile(path)
		if err != nil {
			fingerprintParts = append(fingerprintParts, kind+":error")
			allErrs = append(allErrs, err)
			return
		}
		if !exists {
			fingerprintParts = append(fingerprintParts, kind+":missing")
			return
		}
		sum := sha256.Sum256(data)
		fingerprintParts = append(fingerprintParts, kind+":"+hex.EncodeToString(sum[:]))

		networks, err := parse(path, data)
		if err != nil {
			allErrs = append(allErrs, err)
			return
		}
		if len(networks) > 0 {
			hasNetworks = true
		}
	}

	addFile("client", clientConfigPath, parseClientNetworks)
	addFile("app", appConfigPath, parseAppNetworks)

	snapshot := wifiConfigSnapshot{
		HasNetworks: hasNetworks,
		Fingerprint: strings.Join(fingerprintParts, "|"),
	}
	if hasNetworks {
		return snapshot, nil
	}
	return snapshot, errors.Join(allErrs...)
}

func loadClientNetworks(path string) ([]network, error) {
	data, exists, err := readConfigFile(path)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, nil
	}
	return parseClientNetworks(path, data)
}

func parseClientNetworks(path string, data []byte) ([]network, error) {
	var networks []network
	if err := json.Unmarshal(data, &networks); err == nil {
		return filterNetworks(networks), nil
	}

	var single network
	if err := json.Unmarshal(data, &single); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return filterNetworks([]network{single}), nil
}

func loadAppNetworks(path string) ([]network, error) {
	data, exists, err := readConfigFile(path)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, nil
	}
	return parseAppNetworks(path, data)
}

func parseAppNetworks(path string, data []byte) ([]network, error) {
	var config appWiFiConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return filterNetworks(config.Networks), nil
}

func readConfigFile(path string) ([]byte, bool, error) {
	if path == "" {
		return nil, false, nil
	}
	// #nosec G304 -- path is a well-known WiFi config location set by the application
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("read %s: %w", path, err)
	}
	return data, true, nil
}

func shouldRebootForWiFiConfigChange(initial, current wifiConfigSnapshot) bool {
	return current.HasNetworks && current.Fingerprint != initial.Fingerprint
}

func filterNetworks(networks []network) []network {
	filtered := networks[:0]
	for _, item := range networks {
		if item.SSID != "" {
			filtered = append(filtered, item)
		}
	}
	return filtered
}
