package websocket

import (
	"net/http/httptest"
	"testing"
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
// when LAN auto-trust has been explicitly enabled.
func TestCheckOriginStrict_PrivateIPRanges(t *testing.T) {
	prev := lanOriginsTrusted()
	SetTrustLANOrigins(true)
	defer SetTrustLANOrigins(prev)
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

// TestIsPrivateIP verifies private IP range detection including Tailscale
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

		// Tailscale CGNAT range (100.64.0.0/10) - FIX FOR TLS HANDSHAKE ERRORS
		{"100.64.0.0", true},      // lower bound
		{"100.106.81.42", true},   // actual IP from error logs
		{"100.100.100.100", true}, // mid-range
		{"100.127.255.255", true}, // upper bound
		{"100.63.255.255", false}, // just below range
		{"100.128.0.0", false},    // just above range

		// Loopback
		{"127.0.0.1", true},
		{"127.0.0.2", true},

		// Public IPs
		{"8.8.8.8", false},
		{"1.1.1.1", false},
		{"172.32.1.1", false},

		// IPv6
		{"::1", true},                   // loopback
		{"fc00::1", true},               // unique local
		{"fe80::1", true},               // link-local
		{"fd7a:115c:a1e0::1", true},     // Tailscale IPv6
		{"2001:4860:4860::8888", false}, // public (Google DNS)

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

// TestCheckOriginStrict_WildcardIsFiltered verifies that a wildcard "*" entry
// in the allowlist is dropped rather than honored. WebSocket upgrades carry
// credentials (Basic Auth + ws-token) so a permissive wildcard would defeat
// origin validation; operators must enumerate hosts explicitly.
func TestCheckOriginStrict_WildcardIsFiltered(t *testing.T) {
	SetAllowedOrigins([]string{"*"})
	defer SetAllowedOrigins([]string{})

	if got := GetAllowedOrigins(); len(got) != 0 {
		t.Fatalf("wildcard should be filtered, got %v", got)
	}

	req := httptest.NewRequest("GET", "/ws", nil)
	req.Host = "192.168.10.124:8080"
	req.RemoteAddr = "192.168.10.10:12345"
	req.Header.Set("Origin", "https://example.com")

	if checkOriginStrict(req) {
		t.Error("wildcard should not allow arbitrary cross-origin connections")
	}
}
