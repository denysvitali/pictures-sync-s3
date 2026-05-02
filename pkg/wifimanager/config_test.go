package wifimanager

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestGokrazyWiFiConfigMirroring(t *testing.T) {
	tmpDir := t.TempDir()
	originalAppPath := WiFiConfigPath
	originalGokrazyPath := GokrazyWiFiConfigPath
	defer func() {
		WiFiConfigPath = originalAppPath
		GokrazyWiFiConfigPath = originalGokrazyPath
	}()

	WiFiConfigPath = filepath.Join(tmpDir, "extra-wifi.json")
	GokrazyWiFiConfigPath = filepath.Join(tmpDir, "wifi.json")

	mgr, err := NewManager()
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}
	if err := mgr.AddNetwork("HomeWiFi", "password123"); err != nil {
		t.Fatalf("AddNetwork failed: %v", err)
	}

	var active Network
	data, err := os.ReadFile(GokrazyWiFiConfigPath)
	if err != nil {
		t.Fatalf("read gokrazy config: %v", err)
	}
	if err := json.Unmarshal(data, &active); err != nil {
		t.Fatalf("gokrazy config is not a single network object: %v", err)
	}
	if active.SSID != "HomeWiFi" || active.PSK != "password123" {
		t.Fatalf("active network = %+v", active)
	}

	if err := mgr.AddNetwork("TravelWiFi", "password456"); err != nil {
		t.Fatalf("AddNetwork failed: %v", err)
	}
	data, err = os.ReadFile(GokrazyWiFiConfigPath)
	if err != nil {
		t.Fatalf("read gokrazy config: %v", err)
	}
	if err := json.Unmarshal(data, &active); err != nil {
		t.Fatalf("gokrazy config is not a single network object: %v", err)
	}
	if active.SSID != "TravelWiFi" {
		t.Fatalf("new network should become active, got %q", active.SSID)
	}
}

func TestLoadGokrazySingleNetworkObject(t *testing.T) {
	tmpDir := t.TempDir()
	originalAppPath := WiFiConfigPath
	originalGokrazyPath := GokrazyWiFiConfigPath
	defer func() {
		WiFiConfigPath = originalAppPath
		GokrazyWiFiConfigPath = originalGokrazyPath
	}()

	WiFiConfigPath = filepath.Join(tmpDir, "extra-wifi.json")
	GokrazyWiFiConfigPath = filepath.Join(tmpDir, "wifi.json")
	if err := os.WriteFile(GokrazyWiFiConfigPath, []byte(`{"ssid":"HomeWiFi","psk":"password123"}`), 0600); err != nil {
		t.Fatal(err)
	}

	mgr, err := NewManager()
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}
	networks := mgr.GetNetworks()
	if len(networks) != 1 || networks[0].SSID != "HomeWiFi" {
		t.Fatalf("networks = %+v", networks)
	}
}
