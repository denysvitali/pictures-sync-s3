package captiveportal

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestEdgeCase_NoWiFiConnection tests behavior when not connected to WiFi
func TestEdgeCase_NoWiFiConnection(t *testing.T) {
	getCurrentSSID := func() (string, error) {
		return "", errors.New("not connected to WiFi")
	}

	auth := NewAuthenticator(getCurrentSSID)
	auth.checkAndAuthenticate()

	// Should not have set lastSSID or lastAuthTime
	if auth.lastSSID != "" {
		t.Errorf("lastSSID should be empty when not connected, got %q", auth.lastSSID)
	}

	if !auth.lastAuthTime.IsZero() {
		t.Error("lastAuthTime should be zero when not connected")
	}
}

// TestEdgeCase_NetworkWithoutIPv4 tests behavior when interface has no IPv4 address
func TestEdgeCase_NetworkWithoutIPv4(t *testing.T) {
	authCalled := false

	getCurrentSSID := func() (string, error) {
		return jinjiangSSID, nil
	}

	auth := NewAuthenticator(getCurrentSSID)
	auth.getLocalIPMAC = func() (string, string, error) {
		return "", "", errors.New("no IPv4 address assigned")
	}
	auth.authenticators[jinjiangSSID] = func(ip, mac string) error {
		authCalled = true
		return nil
	}

	auth.checkAndAuthenticate()

	// Authentication should not be called if we can't get IP/MAC
	if authCalled {
		t.Error("Authentication should not be called when IP/MAC retrieval fails")
	}

	// Should have detected the network change but not authenticated
	if auth.lastSSID != jinjiangSSID {
		t.Errorf("lastSSID should be set to %q, got %q", jinjiangSSID, auth.lastSSID)
	}

	if !auth.lastAuthTime.IsZero() {
		t.Error("lastAuthTime should be zero when IP/MAC retrieval fails")
	}
}

// TestEdgeCase_AuthenticationTimeout tests handling of authentication timeout
func TestEdgeCase_AuthenticationTimeout(t *testing.T) {
	getCurrentSSID := func() (string, error) {
		return jinjiangSSID, nil
	}

	auth := NewAuthenticator(getCurrentSSID)
	auth.getLocalIPMAC = func() (string, string, error) {
		return "192.168.1.100", "aa:bb:cc:dd:ee:ff", nil
	}
	auth.retryBackoff = func(attempt int) time.Duration { return 0 }

	auth.authenticators[jinjiangSSID] = func(ip, mac string) error {
		return context.DeadlineExceeded
	}

	auth.checkAndAuthenticate()

	// Should have failed to authenticate due to timeout
	if !auth.lastAuthTime.IsZero() {
		t.Error("Authentication should have failed due to timeout")
	}
}

// TestEdgeCase_AuthenticationRetry tests the retry logic with exponential backoff
func TestEdgeCase_AuthenticationRetry(t *testing.T) {
	attemptCount := 0

	getCurrentSSID := func() (string, error) {
		return jinjiangSSID, nil
	}

	auth := NewAuthenticator(getCurrentSSID)
	auth.getLocalIPMAC = func() (string, string, error) {
		return "192.168.1.100", "aa:bb:cc:dd:ee:ff", nil
	}
	var backoffs []time.Duration
	auth.retryBackoff = func(attempt int) time.Duration {
		backoff := time.Duration(attempt-1) * 2 * time.Second
		backoffs = append(backoffs, backoff)
		return 0
	}

	// Authenticator that fails twice then succeeds
	auth.authenticators[jinjiangSSID] = func(ip, mac string) error {
		attemptCount++
		if attemptCount < 3 {
			return fmt.Errorf("simulated failure (attempt %d)", attemptCount)
		}
		return nil
	}

	auth.checkAndAuthenticate()

	// Should have succeeded on third attempt
	if attemptCount != 3 {
		t.Errorf("Expected 3 attempts, got %d", attemptCount)
	}

	if auth.lastAuthTime.IsZero() {
		t.Error("Authentication should have succeeded on retry")
	}

	expectedBackoffs := []time.Duration{2 * time.Second, 4 * time.Second}
	if len(backoffs) != len(expectedBackoffs) {
		t.Fatalf("Expected %d backoffs, got %d", len(expectedBackoffs), len(backoffs))
	}
	for i := range expectedBackoffs {
		if backoffs[i] != expectedBackoffs[i] {
			t.Errorf("Backoff %d: expected %v, got %v", i, expectedBackoffs[i], backoffs[i])
		}
	}
}

// TestEdgeCase_AuthenticationAllRetriesFail tests behavior when all retries fail
func TestEdgeCase_AuthenticationAllRetriesFail(t *testing.T) {
	attemptCount := 0

	getCurrentSSID := func() (string, error) {
		return jinjiangSSID, nil
	}

	auth := NewAuthenticator(getCurrentSSID)
	auth.getLocalIPMAC = func() (string, string, error) {
		return "192.168.1.100", "aa:bb:cc:dd:ee:ff", nil
	}
	auth.retryBackoff = func(attempt int) time.Duration { return 0 }

	// Authenticator that always fails
	auth.authenticators[jinjiangSSID] = func(ip, mac string) error {
		attemptCount++
		return fmt.Errorf("authentication failed (attempt %d)", attemptCount)
	}

	auth.checkAndAuthenticate()

	// Should have attempted 3 times (initial + 2 retries)
	expectedAttempts := 3
	if attemptCount != expectedAttempts {
		t.Errorf("Expected %d attempts, got %d", expectedAttempts, attemptCount)
	}

	// Should NOT have set lastAuthTime since all attempts failed
	if !auth.lastAuthTime.IsZero() {
		t.Error("lastAuthTime should not be set when all retries fail")
	}
}

// TestEdgeCase_NetworkChange tests behavior when network changes during operation
func TestEdgeCase_NetworkChange(t *testing.T) {
	currentSSID := "Network1"
	authCount := 0

	getCurrentSSID := func() (string, error) {
		return currentSSID, nil
	}

	auth := NewAuthenticator(getCurrentSSID)
	auth.getLocalIPMAC = func() (string, string, error) {
		return "192.168.1.100", "aa:bb:cc:dd:ee:ff", nil
	}
	auth.authenticators["Network1"] = func(ip, mac string) error {
		authCount++
		return nil
	}
	auth.authenticators["Network2"] = func(ip, mac string) error {
		authCount++
		return nil
	}

	// First check - authenticate to Network1
	auth.checkAndAuthenticate()

	if authCount != 1 {
		t.Errorf("Expected 1 auth for Network1, got %d", authCount)
	}

	if auth.lastSSID != "Network1" {
		t.Errorf("lastSSID should be 'Network1', got %q", auth.lastSSID)
	}

	firstAuthTime := auth.lastAuthTime

	// Change network
	currentSSID = "Network2"

	// Second check - should reset auth time and authenticate to Network2
	auth.checkAndAuthenticate()

	if authCount != 2 {
		t.Errorf("Expected 2 total auths, got %d", authCount)
	}

	if auth.lastSSID != "Network2" {
		t.Errorf("lastSSID should be 'Network2', got %q", auth.lastSSID)
	}

	// Auth time should have been reset and set again
	if auth.lastAuthTime == firstAuthTime {
		t.Error("lastAuthTime should be different after network change")
	}
}

// TestEdgeCase_RecentAuthenticationSkip tests that recent authentication is skipped
func TestEdgeCase_RecentAuthenticationSkip(t *testing.T) {
	authCount := 0

	getCurrentSSID := func() (string, error) {
		return jinjiangSSID, nil
	}

	auth := NewAuthenticator(getCurrentSSID)
	auth.getLocalIPMAC = func() (string, string, error) {
		return "192.168.1.100", "aa:bb:cc:dd:ee:ff", nil
	}
	auth.authenticators[jinjiangSSID] = func(ip, mac string) error {
		authCount++
		return nil
	}

	// First authentication
	auth.checkAndAuthenticate()

	if authCount != 1 {
		t.Errorf("Expected 1 auth, got %d", authCount)
	}

	// Immediate second check - should skip (within 5 minute window)
	auth.checkAndAuthenticate()

	if authCount != 1 {
		t.Errorf("Expected still 1 auth (skip recent), got %d", authCount)
	}
}

// TestEdgeCase_ClearAuthenticationState tests the manual state clearing
func TestEdgeCase_ClearAuthenticationState(t *testing.T) {
	authCount := 0

	getCurrentSSID := func() (string, error) {
		return jinjiangSSID, nil
	}

	auth := NewAuthenticator(getCurrentSSID)
	auth.getLocalIPMAC = func() (string, string, error) {
		return "192.168.1.100", "aa:bb:cc:dd:ee:ff", nil
	}
	auth.authenticators[jinjiangSSID] = func(ip, mac string) error {
		authCount++
		return nil
	}

	// First authentication
	auth.checkAndAuthenticate()

	if authCount != 1 {
		t.Errorf("Expected 1 auth, got %d", authCount)
	}

	// Clear state
	auth.ClearAuthenticationState()

	if !auth.lastAuthTime.IsZero() {
		t.Error("lastAuthTime should be cleared")
	}

	// Should now authenticate again even though it was recent
	auth.checkAndAuthenticate()

	if authCount != 2 {
		t.Errorf("Expected 2 auths after clearing state, got %d", authCount)
	}
}

// TestEdgeCase_InvalidIPMACFormat tests handling of invalid IP/MAC formats
func TestEdgeCase_InvalidIPMACFormat(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping network test in short mode")
	}

	testCases := []struct {
		name string
		ip   string
		mac  string
	}{
		{"empty IP", "", "aa:bb:cc:dd:ee:ff"},
		{"empty MAC", "192.168.1.100", ""},
		{"invalid IP", "999.999.999.999", "aa:bb:cc:dd:ee:ff"},
		{"invalid MAC", "192.168.1.100", "invalid-mac"},
		{"both invalid", "invalid", "invalid"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			getCurrentSSID := func() (string, error) {
				return jinjiangSSID, nil
			}

			auth := NewAuthenticator(getCurrentSSID)
			auth.getLocalIPMAC = func() (string, string, error) {
				return tc.ip, tc.mac, nil
			}
			auth.retryBackoff = func(attempt int) time.Duration { return 0 }
			auth.authenticators[jinjiangSSID] = func(ip, mac string) error {
				if net.ParseIP(ip) == nil {
					return fmt.Errorf("invalid IP %q", ip)
				}
				if _, err := net.ParseMAC(mac); err != nil {
					return err
				}
				return nil
			}

			// Should not panic even with invalid formats
			auth.checkAndAuthenticate()

			// Note: The actual authentication may or may not fail depending on portal validation
			// The important thing is no panic occurs
		})
	}
}

// TestEdgeCase_PortalHTTPErrors tests handling of various HTTP error codes
func TestEdgeCase_PortalHTTPErrors(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping HTTP error test in short mode")
	}

	testCases := []struct {
		statusCode int
		shouldFail bool
	}{
		{http.StatusOK, false},                 // 200 - success
		{http.StatusFound, false},              // 302 - redirect, treated as success
		{http.StatusBadRequest, true},          // 400 - client error
		{http.StatusUnauthorized, true},        // 401 - auth required
		{http.StatusForbidden, true},           // 403 - forbidden
		{http.StatusNotFound, true},            // 404 - not found
		{http.StatusInternalServerError, true}, // 500 - server error
		{http.StatusBadGateway, true},          // 502 - bad gateway
		{http.StatusServiceUnavailable, true},  // 503 - service unavailable
		{http.StatusGatewayTimeout, true},      // 504 - gateway timeout
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("status_%d", tc.statusCode), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.statusCode)
			}))
			defer server.Close()

			getCurrentSSID := func() (string, error) {
				return jinjiangSSID, nil
			}

			auth := NewAuthenticator(getCurrentSSID)
			auth.getLocalIPMAC = func() (string, string, error) {
				return "192.168.1.100", "aa:bb:cc:dd:ee:ff", nil
			}
			auth.retryBackoff = func(attempt int) time.Duration { return 0 }

			// Override authenticator to use test server
			originalAuthFunc := auth.authenticators[jinjiangSSID]
			defer func() { auth.authenticators[jinjiangSSID] = originalAuthFunc }()

			auth.authenticators[jinjiangSSID] = func(ip, mac string) error {
				resp, err := http.Get(server.URL)
				if err != nil {
					return err
				}
				defer resp.Body.Close()

				if resp.StatusCode >= 200 && resp.StatusCode < 400 {
					return nil
				}
				return fmt.Errorf("status %d", resp.StatusCode)
			}

			auth.checkAndAuthenticate()

			// Check if authentication state matches expected result
			authenticated := !auth.lastAuthTime.IsZero()

			if tc.shouldFail && authenticated {
				t.Errorf("Status %d should have failed authentication", tc.statusCode)
			}

			if !tc.shouldFail && !authenticated {
				t.Errorf("Status %d should have succeeded authentication", tc.statusCode)
			}
		})
	}
}

// TestEdgeCase_GetLocalIPMACWithNoInterfaces tests IP/MAC retrieval with no interfaces
func TestEdgeCase_GetLocalIPMACWithNoInterfaces(t *testing.T) {
	// This test verifies the function handles the case where net.Interfaces() returns an error
	// In practice, this is hard to simulate without mocking, so we'll test the happy path
	// and verify the function doesn't panic with edge cases

	ip, mac, err := getLocalIPAndMAC()

	// If we're in a test environment with no wireless interface, this is expected to fail
	if err != nil {
		if ip != "" || mac != "" {
			t.Error("IP and MAC should be empty when function returns error")
		}
		t.Logf("No wireless interface found (expected in test environment): %v", err)
		return
	}

	// If successful, verify we got valid-looking values
	if net.ParseIP(ip) == nil {
		t.Errorf("Invalid IP address returned: %s", ip)
	}

	if mac == "" {
		t.Error("MAC address should not be empty on success")
	}
}

// TestEdgeCase_ConcurrentAuthentication tests thread safety
func TestEdgeCase_ConcurrentAuthentication(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping concurrency test in short mode")
	}

	getCurrentSSID := func() (string, error) {
		return jinjiangSSID, nil
	}

	auth := NewAuthenticator(getCurrentSSID)
	auth.getLocalIPMAC = func() (string, string, error) {
		return "192.168.1.100", "aa:bb:cc:dd:ee:ff", nil
	}
	auth.authenticators[jinjiangSSID] = func(ip, mac string) error {
		time.Sleep(10 * time.Millisecond) // Simulate work
		return nil
	}

	// Run multiple concurrent authentication attempts
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func() {
			auth.checkAndAuthenticate()
			done <- true
		}()
	}

	// Wait for all goroutines to complete
	for i := 0; i < 10; i++ {
		<-done
	}

	// Should have authenticated at least once
	if auth.lastAuthTime.IsZero() {
		t.Error("Authentication should have occurred")
	}

	// Should not have panicked (test passes if we get here)
}
