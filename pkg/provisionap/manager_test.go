package provisionap

import (
	"context"
	"net"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func ethernetGatedConfig(t *testing.T, ethernetIface string) Config {
	t.Helper()
	cfg := Config{
		Interface:         "wlan0",
		SSID:              "PhotoBackup-Setup",
		PSK:               "photo-backup-setup",
		APIP:              net.IPv4(192, 168, 44, 1),
		Netmask:           net.IPv4Mask(255, 255, 255, 0),
		DHCPStart:         net.IPv4(192, 168, 44, 50),
		DHCPEnd:           net.IPv4(192, 168, 44, 150),
		HostapdPath:       "/usr/bin/hostapd",
		ConfigDir:         t.TempDir(),
		CountryCode:       "US",
		ClientConfigPath:  filepath.Join(t.TempDir(), "wifi.json"),
		AppConfigPath:     filepath.Join(t.TempDir(), "extra-wifi.json"),
		StartupWait:       50 * time.Millisecond,
		ConfigPollDelay:   5 * time.Millisecond,
		EthernetFirst:     true,
		EthernetInterface: ethernetIface,
		EthernetWait:      0,
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("invalid test config: %v", err)
	}
	return cfg
}

func TestWaitForConnectionOrEthernetSkipsAPWhenEthernetAppears(t *testing.T) {
	cfg := ethernetGatedConfig(t, "eth0")

	var carrierCalls atomic.Int32
	carrier := func(string) (bool, error) {
		// First two probes report no carrier so the wait proceeds, then the
		// cable comes up. waitForConnectionOrEthernet must catch this without
		// requiring the wifi check to succeed first.
		return carrierCalls.Add(1) >= 3, nil
	}
	connected := func(Config) bool { return false }

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	got := waitForConnectionOrEthernet(ctx, cfg, connected, carrier)
	if got != waitEthernetUp {
		t.Fatalf("waitForConnectionOrEthernet = %v, want waitEthernetUp", got)
	}
}

func TestWaitForConnectionOrEthernetTimesOutWithoutEthernet(t *testing.T) {
	cfg := ethernetGatedConfig(t, "eth0")
	cfg.StartupWait = 30 * time.Millisecond

	carrier := func(string) (bool, error) { return false, nil }
	connected := func(Config) bool { return false }

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	got := waitForConnectionOrEthernet(ctx, cfg, connected, carrier)
	if got != waitTimedOut {
		t.Fatalf("waitForConnectionOrEthernet = %v, want waitTimedOut", got)
	}
}

func TestWaitForConnectionLossOrEthernetPrefersEthernet(t *testing.T) {
	cfg := ethernetGatedConfig(t, "eth0")

	carrier := func(string) (bool, error) { return true, nil }
	// Wi-Fi reports healthy throughout — the only reason to exit is the
	// ethernet carrier appearing.
	connected := func(Config) bool { return true }

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	got := waitForConnectionLossOrEthernet(ctx, cfg, connected, carrier)
	if got != waitEthernetUp {
		t.Fatalf("waitForConnectionLossOrEthernet = %v, want waitEthernetUp", got)
	}
}

func TestEthernetCarrierPresentDisabledWhenEthernetFirstOff(t *testing.T) {
	cfg := ethernetGatedConfig(t, "eth0")
	cfg.EthernetFirst = false

	called := false
	m := &Manager{Config: cfg, Carrier: func(string) (bool, error) {
		called = true
		return true, nil
	}}
	if m.ethernetCarrierPresent() {
		t.Fatal("ethernetCarrierPresent() = true with EthernetFirst disabled")
	}
	if called {
		t.Fatal("carrier was probed even though EthernetFirst is disabled")
	}
}

func TestEthernetCarrierPresentReportsCarrier(t *testing.T) {
	cfg := ethernetGatedConfig(t, "eth0")
	m := &Manager{Config: cfg, Carrier: func(string) (bool, error) { return true, nil }}
	if !m.ethernetCarrierPresent() {
		t.Fatal("ethernetCarrierPresent() = false, want true")
	}
}
