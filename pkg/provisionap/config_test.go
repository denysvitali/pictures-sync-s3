package provisionap

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHasConfiguredNetworks(t *testing.T) {
	t.Run("no files", func(t *testing.T) {
		tmpDir := t.TempDir()
		ok, err := HasConfiguredNetworks(filepath.Join(tmpDir, "wifi.json"), filepath.Join(tmpDir, "extra-wifi.json"))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ok {
			t.Fatal("expected no configured networks")
		}
	})

	t.Run("gokrazy single object", func(t *testing.T) {
		tmpDir := t.TempDir()
		clientPath := filepath.Join(tmpDir, "wifi.json")
		if err := os.WriteFile(clientPath, []byte(`{"ssid":"Home","psk":"password123"}`), 0600); err != nil {
			t.Fatal(err)
		}

		ok, err := HasConfiguredNetworks(clientPath, filepath.Join(tmpDir, "extra-wifi.json"))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !ok {
			t.Fatal("expected configured network")
		}
	})

	t.Run("app network list", func(t *testing.T) {
		tmpDir := t.TempDir()
		appPath := filepath.Join(tmpDir, "extra-wifi.json")
		if err := os.WriteFile(appPath, []byte(`{"networks":[{"ssid":"Home","psk":"password123"}]}`), 0600); err != nil {
			t.Fatal(err)
		}

		ok, err := HasConfiguredNetworks(filepath.Join(tmpDir, "wifi.json"), appPath)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !ok {
			t.Fatal("expected configured network")
		}
	})

	t.Run("malformed empty config", func(t *testing.T) {
		tmpDir := t.TempDir()
		clientPath := filepath.Join(tmpDir, "wifi.json")
		if err := os.WriteFile(clientPath, []byte(`{`), 0600); err != nil {
			t.Fatal(err)
		}

		ok, err := HasConfiguredNetworks(clientPath, filepath.Join(tmpDir, "extra-wifi.json"))
		if err == nil {
			t.Fatal("expected parse error")
		}
		if ok {
			t.Fatal("expected no configured networks")
		}
	})
}

func TestWiFiConfigChangeTriggersRebootForExistingConfig(t *testing.T) {
	tmpDir := t.TempDir()
	clientPath := filepath.Join(tmpDir, "wifi.json")
	appPath := filepath.Join(tmpDir, "extra-wifi.json")

	if err := os.WriteFile(clientPath, []byte(`{"ssid":"Old","psk":"password123"}`), 0600); err != nil {
		t.Fatal(err)
	}
	initial, err := readWiFiConfigSnapshot(clientPath, appPath)
	if err != nil {
		t.Fatalf("initial snapshot: %v", err)
	}

	if err := os.WriteFile(clientPath, []byte(`{"ssid":"New","psk":"password123"}`), 0600); err != nil {
		t.Fatal(err)
	}
	current, err := readWiFiConfigSnapshot(clientPath, appPath)
	if err != nil {
		t.Fatalf("current snapshot: %v", err)
	}

	if !shouldRebootForWiFiConfigChange(initial, current) {
		t.Fatal("expected changed Wi-Fi config to trigger reboot")
	}
	if shouldRebootForWiFiConfigChange(current, current) {
		t.Fatal("unchanged Wi-Fi config should not trigger reboot")
	}
}

func TestRenderHostapdConfig(t *testing.T) {
	cfg := testConfig(t)
	rendered := RenderHostapdConfig(cfg)

	for _, want := range []string{
		"interface=wlan0",
		"driver=nl80211",
		"ssid=PhotoBackup-Setup",
		"wpa=2",
		"wpa_passphrase=photo-backup-setup",
		"rsn_pairwise=CCMP",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("hostapd config missing %q:\n%s", want, rendered)
		}
	}
}

func TestConfigValidation(t *testing.T) {
	cfg := testConfig(t)
	if err := cfg.Validate(); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}

	cfg.PSK = "short"
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected short PSK error")
	}
}

func TestDHCPResponse(t *testing.T) {
	cfg := testConfig(t)
	server := newDHCPServer(cfg)
	packet := make([]byte, 240)
	packet[0] = 1
	packet[1] = 1
	packet[2] = 6
	packet[4] = 0xaa
	copy(packet[28:34], []byte{0, 1, 2, 3, 4, 5})
	copy(packet[236:240], []byte{99, 130, 83, 99})
	packet = appendDHCPOption(packet, 53, []byte{dhcpDiscover})
	packet = appendDHCPOption(packet, 255, nil)

	response, err := server.response(packet)
	if err != nil {
		t.Fatalf("response failed: %v", err)
	}
	if response[0] != 2 {
		t.Fatalf("expected bootreply, got %d", response[0])
	}
	if got := net.IP(response[16:20]); !got.Equal(cfg.DHCPStart) {
		t.Fatalf("lease = %s, want %s", got, cfg.DHCPStart)
	}
	options := parseDHCPOptions(response[240:])
	if got := firstOptionByte(options[53]); got != dhcpOffer {
		t.Fatalf("message type = %d, want offer", got)
	}
}

func TestDNSResponse(t *testing.T) {
	server := newDNSServer(net.IPv4(192, 168, 44, 1))
	query := []byte{
		0x12, 0x34, 0x01, 0x00, 0x00, 0x01, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x07, 'e', 'x', 'a',
		'm', 'p', 'l', 'e', 0x03, 'c', 'o', 'm', 0x00,
		0x00, 0x01, 0x00, 0x01,
	}

	response, err := server.response(query)
	if err != nil {
		t.Fatalf("response failed: %v", err)
	}
	if got := response[len(response)-4:]; !net.IP(got).Equal(net.IPv4(192, 168, 44, 1)) {
		t.Fatalf("A response = %v", got)
	}
}

func testConfig(t *testing.T) Config {
	t.Helper()
	return Config{
		Interface:        "wlan0",
		SSID:             "PhotoBackup-Setup",
		PSK:              "photo-backup-setup",
		APIP:             net.IPv4(192, 168, 44, 1),
		Netmask:          net.IPv4Mask(255, 255, 255, 0),
		DHCPStart:        net.IPv4(192, 168, 44, 50),
		DHCPEnd:          net.IPv4(192, 168, 44, 150),
		HostapdPath:      "/usr/bin/hostapd",
		ConfigDir:        t.TempDir(),
		ClientConfigPath: filepath.Join(t.TempDir(), "wifi.json"),
		AppConfigPath:    filepath.Join(t.TempDir(), "extra-wifi.json"),
		StartupWait:      time.Millisecond,
		ConfigPollDelay:  time.Millisecond,
	}
}
