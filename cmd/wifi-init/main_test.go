package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/denysvitali/pictures-sync-s3/pkg/netiface"
)

func TestWaitForEthernetCarrierNoCarrier(t *testing.T) {
	tmpDir := t.TempDir()
	oldRoot := netiface.CarrierRoot
	netiface.CarrierRoot = tmpDir
	t.Cleanup(func() {
		netiface.CarrierRoot = oldRoot
	})

	dir := filepath.Join(tmpDir, "eth0")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "carrier"), []byte("0\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if waitForEthernetCarrier("eth0", 0) {
		t.Fatal("waitForEthernetCarrier() = true, want false")
	}
}

func TestWaitForEthernetCarrierPresent(t *testing.T) {
	tmpDir := t.TempDir()
	oldRoot := netiface.CarrierRoot
	netiface.CarrierRoot = tmpDir
	t.Cleanup(func() {
		netiface.CarrierRoot = oldRoot
	})

	dir := filepath.Join(tmpDir, "eth0")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "carrier"), []byte("1\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if !waitForEthernetCarrier("eth0", time.Second) {
		t.Fatal("waitForEthernetCarrier() = false, want true")
	}
}

func TestGetenvBool(t *testing.T) {
	t.Setenv("WIFI_INIT_TEST_BOOL", "off")
	if getenvBool("WIFI_INIT_TEST_BOOL", true) {
		t.Fatal("getenvBool(off) = true, want false")
	}

	t.Setenv("WIFI_INIT_TEST_BOOL", "yes")
	if !getenvBool("WIFI_INIT_TEST_BOOL", false) {
		t.Fatal("getenvBool(yes) = false, want true")
	}

	t.Setenv("WIFI_INIT_TEST_BOOL", "not-a-bool")
	if !getenvBool("WIFI_INIT_TEST_BOOL", true) {
		t.Fatal("getenvBool(invalid) = false, want fallback true")
	}
}

func TestEnsureClientRegion(t *testing.T) {
	cases := []struct {
		name, in string
		changed  bool
		region   string
	}{
		{"missing", `{"ssid":"a","psk":"b"}`, true, "DE"},
		{"world", `{"ssid":"a","region":"00"}`, true, "DE"},
		{"kept", `{"ssid":"a","region":"IT"}`, false, "IT"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "wifi.json")
			if err := os.WriteFile(path, []byte(tc.in), 0o600); err != nil {
				t.Fatal(err)
			}
			changed, err := ensureClientRegion(path, "DE")
			if err != nil || changed != tc.changed {
				t.Fatalf("changed=%v err=%v", changed, err)
			}
			var got map[string]any
			data, _ := os.ReadFile(path)
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatal(err)
			}
			if got["region"] != tc.region || got["ssid"] != "a" {
				t.Fatalf("got %v", got)
			}
		})
	}
	if changed, err := ensureClientRegion(filepath.Join(t.TempDir(), "none.json"), "DE"); changed || err != nil {
		t.Fatalf("missing file: changed=%v err=%v", changed, err)
	}
}
