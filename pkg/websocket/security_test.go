package websocket

import (
	"net/http/httptest"
	"testing"
	"time"
)

// TestCheckOriginStrict_EmptyOrigin verifies empty origins are rejected
func TestCheckOriginStrict_EmptyOrigin(t *testing.T) {
	req := httptest.NewRequest("GET", "/ws", nil)
	// No Origin header set

	if checkOriginStrict(req) {
		t.Error("Expected empty origin to be rejected, but it was accepted")
	}
}

// TestCheckOriginStrict_InvalidOrigin verifies malformed origins are rejected
func TestCheckOriginStrict_InvalidOrigin(t *testing.T) {
	req := httptest.NewRequest("GET", "/ws", nil)
	req.Header.Set("Origin", "ht!tp://invalid")

	if checkOriginStrict(req) {
		t.Error("Expected invalid origin to be rejected, but it was accepted")
	}
}

// TestCheckOriginStrict_SameHost verifies same-host origins are accepted
func TestCheckOriginStrict_SameHost(t *testing.T) {
	req := httptest.NewRequest("GET", "/ws", nil)
	req.Host = "192.168.1.100:8080"
	req.Header.Set("Origin", "http://192.168.1.100:8080")

	if !checkOriginStrict(req) {
		t.Error("Expected same-host origin to be accepted, but it was rejected")
	}
}

// TestCheckOriginStrict_PrivateIPRanges verifies RFC 1918 private IPs are accepted
func TestCheckOriginStrict_PrivateIPRanges(t *testing.T) {
	tests := []struct {
		name   string
		origin string
		want   bool
	}{
		// Valid RFC 1918 ranges
		{"10.x range", "http://10.0.0.1", true},
		{"10.x max", "http://10.255.255.255", true},
		{"192.168.x range", "http://192.168.1.1", true},
		{"192.168.x max", "http://192.168.255.255", true},
		{"172.16-31 start", "http://172.16.0.1", true},
		{"172.16-31 mid", "http://172.20.1.1", true},
		{"172.16-31 end", "http://172.31.255.255", true},

		// Invalid 172.x ranges (SECURITY FIX: these should be rejected)
		{"172.32.x (non-private)", "http://172.32.0.1", false},
		{"172.100.x (non-private)", "http://172.100.1.1", false},
		{"172.255.x (non-private)", "http://172.255.255.255", false},

		// Link-local
		{"Link-local", "http://169.254.1.1", true},

		// Loopback
		{"Loopback", "http://127.0.0.1", true},
		{"Localhost", "http://localhost", true},

		// mDNS .local domains
		{"mDNS domain", "http://raspberrypi.local", true},

		// Public IPs should be rejected
		{"Public IP 8.8.8.8", "http://8.8.8.8", false},
		{"Public IP 1.1.1.1", "http://1.1.1.1", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/ws", nil)
			req.Host = "192.168.1.100:8080"
			req.Header.Set("Origin", tt.origin)

			got := checkOriginStrict(req)
			if got != tt.want {
				t.Errorf("checkOriginStrict(%s) = %v, want %v", tt.origin, got, tt.want)
			}
		})
	}
}

// TestCheckOriginStrict_Whitelist verifies whitelist functionality
func TestCheckOriginStrict_Whitelist(t *testing.T) {
	// Set up whitelist
	SetAllowedOrigins([]string{"example.com", "trusted.example.com"})
	defer SetAllowedOrigins([]string{}) // Clean up

	tests := []struct {
		name   string
		origin string
		want   bool
	}{
		{"Whitelisted origin", "http://example.com", true},
		{"Whitelisted subdomain", "http://trusted.example.com", true},
		{"Non-whitelisted", "http://evil.com", false},
		{"Private IP (blocked by whitelist)", "http://192.168.1.1", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/ws", nil)
			req.Host = "example.com"
			req.Header.Set("Origin", tt.origin)

			got := checkOriginStrict(req)
			if got != tt.want {
				t.Errorf("checkOriginStrict(%s) with whitelist = %v, want %v", tt.origin, got, tt.want)
			}
		})
	}
}

// TestIsPrivateIP verifies private IP range detection
func TestIsPrivateIP(t *testing.T) {
	tests := []struct {
		ip   string
		want bool
	}{
		// RFC 1918 ranges
		{"10.0.0.1", true},
		{"10.255.255.255", true},
		{"172.16.0.0", true},
		{"172.31.255.255", true},
		{"192.168.0.0", true},
		{"192.168.255.255", true},

		// SECURITY FIX: 172.32+ should NOT be private
		{"172.32.0.1", false},
		{"172.100.1.1", false},
		{"172.255.255.255", false},

		// Link-local
		{"169.254.1.1", true},

		// Loopback
		{"127.0.0.1", true},
		{"127.0.0.2", true},

		// Public IPs
		{"8.8.8.8", false},
		{"1.1.1.1", false},
		{"172.32.1.1", false},

		// IPv6
		{"::1", true},                           // loopback
		{"fc00::1", true},                       // unique local
		{"fe80::1", true},                       // link-local
		{"2001:4860:4860::8888", false},         // public (Google DNS)

		// Invalid IPs
		{"not-an-ip", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.ip, func(t *testing.T) {
			got := isPrivateIP(tt.ip)
			if got != tt.want {
				t.Errorf("isPrivateIP(%s) = %v, want %v", tt.ip, got, tt.want)
			}
		})
	}
}

// TestRateLimiting verifies rate limiting per IP
func TestRateLimiting(t *testing.T) {
	// Skip this test in short mode due to time.Sleep
	if testing.Short() {
		t.Skip("Skipping rate limit test in short mode")
	}

	limiter := NewConnectionRateLimiter()

	// First 2 requests should succeed (burst)
	ip := "192.168.1.100"
	l := limiter.GetLimiter(ip)

	if !l.Allow() {
		t.Error("First request should be allowed")
	}
	if !l.Allow() {
		t.Error("Second request should be allowed (burst)")
	}

	// Third request should be blocked
	if l.Allow() {
		t.Error("Third request should be rate limited")
	}

	// After waiting, request should succeed
	time.Sleep(13 * time.Second) // Wait for rate limit to reset
	if !l.Allow() {
		t.Error("Request after wait should be allowed")
	}
}

// TestRateLimiter_DifferentIPs verifies rate limits are per-IP
func TestRateLimiter_DifferentIPs(t *testing.T) {
	limiter := NewConnectionRateLimiter()

	ip1 := "192.168.1.100"
	ip2 := "192.168.1.101"

	l1 := limiter.GetLimiter(ip1)
	l2 := limiter.GetLimiter(ip2)

	// Exhaust IP1's limit
	l1.Allow()
	l1.Allow()

	// IP1 should be blocked
	if l1.Allow() {
		t.Error("IP1 should be rate limited")
	}

	// IP2 should still work
	if !l2.Allow() {
		t.Error("IP2 should not be affected by IP1's rate limit")
	}
}

// TestAllowedOriginsConfiguration verifies whitelist management
func TestAllowedOriginsConfiguration(t *testing.T) {
	// Initial state
	origins := GetAllowedOrigins()
	if len(origins) != 0 {
		t.Errorf("Initial origins should be empty, got %v", origins)
	}

	// Set whitelist
	testOrigins := []string{"example.com", "test.com"}
	SetAllowedOrigins(testOrigins)

	// Verify get returns copy
	origins = GetAllowedOrigins()
	if len(origins) != 2 {
		t.Errorf("Expected 2 origins, got %d", len(origins))
	}

	// Modify returned slice (should not affect internal state)
	origins[0] = "modified.com"

	// Verify internal state unchanged
	origins = GetAllowedOrigins()
	if origins[0] != "example.com" {
		t.Error("Internal state should not be modified by external changes")
	}

	// Clear whitelist
	SetAllowedOrigins([]string{})
	origins = GetAllowedOrigins()
	if len(origins) != 0 {
		t.Error("Origins should be cleared")
	}
}
