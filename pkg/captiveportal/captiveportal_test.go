package captiveportal

import (
	"errors"
	"testing"
	"time"
)

func TestNewAuthenticator(t *testing.T) {
	getCurrentSSID := func() (string, error) {
		return "TestSSID", nil
	}

	auth := NewAuthenticator(getCurrentSSID)
	if auth == nil {
		t.Fatal("NewAuthenticator returned nil")
	}

	if auth.getCurrentSSID == nil {
		t.Error("getCurrentSSID function not set")
	}

	if auth.authenticators == nil {
		t.Error("authenticators map not initialized")
	}

	if _, exists := auth.authenticators[jinjiangSSID]; !exists {
		t.Error("JinJiang authenticator not registered")
	}
}

func TestCheckAndAuthenticate_NoConnection(t *testing.T) {
	getCurrentSSID := func() (string, error) {
		return "", errors.New("not connected")
	}

	auth := NewAuthenticator(getCurrentSSID)
	auth.checkAndAuthenticate()

	if auth.lastSSID != "" {
		t.Error("lastSSID should be empty when not connected")
	}
}

func TestCheckAndAuthenticate_UnknownNetwork(t *testing.T) {
	getCurrentSSID := func() (string, error) {
		return "SomeOtherNetwork", nil
	}

	auth := NewAuthenticator(getCurrentSSID)
	auth.checkAndAuthenticate()

	if auth.lastSSID != "SomeOtherNetwork" {
		t.Errorf("lastSSID = %q, want %q", auth.lastSSID, "SomeOtherNetwork")
	}

	// Note: With the new logging behavior, lastAuthTime is set to prevent repeated logging
	// This is expected behavior to reduce log spam
	if auth.lastAuthTime.IsZero() {
		t.Error("lastAuthTime should be set after first check (to prevent repeated logging)")
	}
}

func TestCheckAndAuthenticate_RecentAuth(t *testing.T) {
	authenticated := false
	getCurrentSSID := func() (string, error) {
		return jinjiangSSID, nil
	}

	auth := NewAuthenticator(getCurrentSSID)
	auth.lastAuthTime = time.Now().Add(-1 * time.Minute) // Recent auth
	auth.authenticators[jinjiangSSID] = func(ip, mac string) error {
		authenticated = true
		return nil
	}

	auth.checkAndAuthenticate()

	if authenticated {
		t.Error("Should not authenticate when recently authenticated")
	}
}

func TestCheckAndAuthenticate_JinJiangNetwork(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping network test in short mode")
	}

	authCalled := false
	var capturedIP, capturedMAC string

	getCurrentSSID := func() (string, error) {
		return jinjiangSSID, nil
	}

	auth := NewAuthenticator(getCurrentSSID)
	auth.getLocalIPMAC = func() (string, string, error) {
		return "192.168.1.100", "aa:bb:cc:dd:ee:ff", nil
	}
	auth.authenticators[jinjiangSSID] = func(ip, mac string) error {
		authCalled = true
		capturedIP = ip
		capturedMAC = mac
		return nil
	}

	auth.checkAndAuthenticate()

	if !authCalled {
		t.Error("Authentication function was not called for JinJiang network")
	}

	if capturedIP != "192.168.1.100" {
		t.Errorf("IP = %q, want %q", capturedIP, "192.168.1.100")
	}

	if capturedMAC != "aa:bb:cc:dd:ee:ff" {
		t.Errorf("MAC = %q, want %q", capturedMAC, "aa:bb:cc:dd:ee:ff")
	}

	if auth.lastAuthTime.IsZero() {
		t.Error("lastAuthTime should be set after successful auth")
	}
}

func TestGetLocalIPAndMAC(t *testing.T) {
	ip, mac, err := getLocalIPAndMAC()

	// This test may fail if there's no wireless interface
	// We just check that the function doesn't panic
	if err != nil {
		t.Logf("No wireless interface found (expected in many environments): %v", err)
		return
	}

	if ip == "" {
		t.Error("IP address should not be empty when successful")
	}

	if mac == "" {
		t.Error("MAC address should not be empty when successful")
	}

	t.Logf("Found IP: %s, MAC: %s", ip, mac)
}

func TestAuthenticateJinJiang_InvalidIP(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping network test in short mode")
	}

	// Note: The JinJiang server actually accepts any IP/MAC combination and returns 200 OK
	// This test just verifies that the function doesn't crash with unusual input
	err := authenticateJinJiang("invalid-ip", "aa:bb:cc:dd:ee:ff")
	// The server may accept it or we may get an error - either is acceptable
	if err != nil {
		t.Logf("Authentication with invalid IP failed as expected: %v", err)
	} else {
		t.Logf("Server accepted invalid IP (permissive authentication)")
	}
}
