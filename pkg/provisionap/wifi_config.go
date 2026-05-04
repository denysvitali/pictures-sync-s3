package provisionap

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

type network struct {
	SSID string `json:"ssid"`
	PSK  string `json:"psk,omitempty"`
}

type appWiFiConfig struct {
	Networks []network `json:"networks"`
}

// HasConfiguredNetworks reports whether either supported Wi-Fi config file
// contains at least one user network.
func HasConfiguredNetworks(clientConfigPath, appConfigPath string) (bool, error) {
	clientNetworks, clientErr := loadClientNetworks(clientConfigPath)
	appNetworks, appErr := loadAppNetworks(appConfigPath)

	if len(clientNetworks) > 0 || len(appNetworks) > 0 {
		return true, nil
	}

	return false, errors.Join(clientErr, appErr)
}

func loadClientNetworks(path string) ([]network, error) {
	if path == "" {
		return nil, nil
	}

	// #nosec G304 -- path is a well-known WiFi config location set by the application
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

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
	if path == "" {
		return nil, nil
	}

	// #nosec G304 -- path is a well-known WiFi config location set by the application
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	var config appWiFiConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return filterNetworks(config.Networks), nil
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
