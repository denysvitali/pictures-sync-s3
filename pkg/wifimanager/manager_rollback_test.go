package wifimanager

import (
	"os"
	"path/filepath"
	"testing"
)

// makeUnwritable returns paths inside a non-writable parent directory so that
// SaveJSON's parent dir creation succeeds (the parent itself already exists
// inside the read-only directory) but the file open fails. This simulates a
// disk full / read-only /perm situation where the in-memory mutation must
// be rolled back to avoid serving credentials that were never persisted.
func makeUnwritable(t *testing.T) (string, string) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("cannot exercise permission-denied write as root")
	}
	tmpDir := t.TempDir()
	roDir := filepath.Join(tmpDir, "ro")
	if err := os.MkdirAll(roDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Pre-create both target files with mode 0 so the atomic-write open fails.
	extra := filepath.Join(roDir, "extra-wifi.json")
	gokrazy := filepath.Join(roDir, "wifi.json")
	for _, p := range []string{extra, gokrazy} {
		if err := os.WriteFile(p, []byte("{}"), 0o600); err != nil {
			t.Fatalf("seed %s: %v", p, err)
		}
	}
	// Make the directory unwritable so the rename inside the parent fails.
	if err := os.Chmod(roDir, 0o555); err != nil {
		t.Fatalf("chmod ro: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(roDir, 0o755)
	})
	return extra, gokrazy
}

func TestAddNetworkRollsBackOnSaveFailure(t *testing.T) {
	extraPath, gokrazyPath := makeUnwritable(t)

	origExtra := WiFiConfigPath
	origGokrazy := GokrazyWiFiConfigPath
	defer func() {
		WiFiConfigPath = origExtra
		GokrazyWiFiConfigPath = origGokrazy
	}()
	WiFiConfigPath = extraPath
	GokrazyWiFiConfigPath = gokrazyPath

	mgr := &Manager{networks: []Network{{SSID: "ExistingNet", PSK: "originalPass1!"}}}

	// AddNetwork must fail because save() cannot write into the read-only dir.
	err := mgr.AddNetwork("NewNet", "brandNewPass2024!")
	if err == nil {
		t.Fatal("AddNetwork succeeded despite read-only config dir; expected error")
	}

	nets := mgr.GetNetworks()
	if len(nets) != 1 || nets[0].SSID != "ExistingNet" {
		t.Fatalf("rollback failed: networks = %+v, want only original ExistingNet", nets)
	}
	for _, n := range nets {
		if n.SSID == "NewNet" {
			t.Fatalf("uncommitted credentials leaked into in-memory state: %+v", n)
		}
	}
}

func TestAddNetworkPasswordUpdateRollsBackOnSaveFailure(t *testing.T) {
	extraPath, gokrazyPath := makeUnwritable(t)

	origExtra := WiFiConfigPath
	origGokrazy := GokrazyWiFiConfigPath
	defer func() {
		WiFiConfigPath = origExtra
		GokrazyWiFiConfigPath = origGokrazy
	}()
	WiFiConfigPath = extraPath
	GokrazyWiFiConfigPath = gokrazyPath

	const originalPSK = "originalSecret1!"
	const newPSK = "rotatedSecret2!"
	mgr := &Manager{networks: []Network{{SSID: "Home", PSK: originalPSK}}}

	if err := mgr.AddNetwork("Home", newPSK); err == nil {
		t.Fatal("AddNetwork succeeded despite read-only config dir; expected error")
	}

	nets := mgr.GetNetworks()
	if len(nets) != 1 {
		t.Fatalf("expected 1 network, got %d: %+v", len(nets), nets)
	}
	if nets[0].PSK != originalPSK {
		t.Fatalf("PSK rolled forward despite save failure: got %q want %q", nets[0].PSK, originalPSK)
	}
}

func TestRemoveNetworkRollsBackOnSaveFailure(t *testing.T) {
	extraPath, gokrazyPath := makeUnwritable(t)

	origExtra := WiFiConfigPath
	origGokrazy := GokrazyWiFiConfigPath
	defer func() {
		WiFiConfigPath = origExtra
		GokrazyWiFiConfigPath = origGokrazy
	}()
	WiFiConfigPath = extraPath
	GokrazyWiFiConfigPath = gokrazyPath

	mgr := &Manager{networks: []Network{
		{SSID: "Home", PSK: "secret123!"},
		{SSID: "Office", PSK: "office123!"},
	}}

	if err := mgr.RemoveNetwork("Home"); err == nil {
		t.Fatal("RemoveNetwork succeeded despite read-only config dir; expected error")
	}

	nets := mgr.GetNetworks()
	if len(nets) != 2 {
		t.Fatalf("rollback failed: got %d networks, want 2: %+v", len(nets), nets)
	}
	if nets[0].SSID != "Home" || nets[1].SSID != "Office" {
		t.Fatalf("network ordering changed after rollback: %+v", nets)
	}
}

func TestReorderNetworksRollsBackOnSaveFailure(t *testing.T) {
	extraPath, gokrazyPath := makeUnwritable(t)

	origExtra := WiFiConfigPath
	origGokrazy := GokrazyWiFiConfigPath
	defer func() {
		WiFiConfigPath = origExtra
		GokrazyWiFiConfigPath = origGokrazy
	}()
	WiFiConfigPath = extraPath
	GokrazyWiFiConfigPath = gokrazyPath

	mgr := &Manager{networks: []Network{
		{SSID: "Home", PSK: "secret123!"},
		{SSID: "Office", PSK: "office123!"},
	}}

	if err := mgr.ReorderNetworks([]string{"Office", "Home"}); err == nil {
		t.Fatal("ReorderNetworks succeeded despite read-only config dir; expected error")
	}

	nets := mgr.GetNetworks()
	if len(nets) != 2 {
		t.Fatalf("expected 2 networks, got %d", len(nets))
	}
	if nets[0].SSID != "Home" || nets[1].SSID != "Office" {
		t.Fatalf("ordering changed despite save failure: %+v", nets)
	}
}
