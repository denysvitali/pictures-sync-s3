package main

import (
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
