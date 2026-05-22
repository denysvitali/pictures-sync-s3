package ssrf

import (
	"net"
	"strings"
	"testing"
	"time"
)

func TestValidateHostname(t *testing.T) {
	tests := []struct {
		name      string
		hostname  string
		clientIP  string
		shouldErr bool
		errReason string
	}{
		{
			name:      "valid external hostname",
			hostname:  "google.com",
			clientIP:  "192.0.2.1",
			shouldErr: false,
		},
		{
			name:      "valid external hostname - cloudflare",
			hostname:  "cloudflare.com",
			clientIP:  "192.0.2.1",
			shouldErr: false,
		},
		{
			name:      "localhost blocked",
			hostname:  "localhost",
			clientIP:  "192.0.2.1",
			shouldErr: true,
			errReason: "localhost access not allowed",
		},
		{
			name:      "localhost.localdomain blocked",
			hostname:  "localhost.localdomain",
			clientIP:  "192.0.2.1",
			shouldErr: true,
			errReason: "localhost access not allowed",
		},
		{
			name:      "127.0.0.1 blocked",
			hostname:  "127.0.0.1",
			clientIP:  "192.0.2.1",
			shouldErr: true,
			errReason: "loopback address not allowed",
		},
		{
			name:      "AWS metadata IP blocked",
			hostname:  "169.254.169.254",
			clientIP:  "192.0.2.1",
			shouldErr: true,
			errReason: "cloud metadata endpoint access not allowed",
		},
		{
			name:      "Google metadata blocked",
			hostname:  "metadata.google.internal",
			clientIP:  "192.0.2.1",
			shouldErr: true,
			errReason: "cloud metadata endpoint access not allowed",
		},
		{
			name:      "private IP 10.x.x.x blocked",
			hostname:  "10.0.0.1",
			clientIP:  "192.0.2.1",
			shouldErr: true,
			errReason: "private IP address not allowed",
		},
		{
			name:      "private IP 192.168.x.x blocked",
			hostname:  "192.168.1.1",
			clientIP:  "192.0.2.1",
			shouldErr: true,
			errReason: "private IP address not allowed",
		},
		{
			name:      "private IP 172.16.x.x blocked",
			hostname:  "172.16.0.1",
			clientIP:  "192.0.2.1",
			shouldErr: true,
			errReason: "private IP address not allowed",
		},
		{
			name:      "empty hostname",
			hostname:  "",
			clientIP:  "192.0.2.1",
			shouldErr: true,
			errReason: "empty hostname",
		},
		{
			name:      "URL scheme blocked",
			hostname:  "http://google.com",
			clientIP:  "192.0.2.1",
			shouldErr: true,
			errReason: "URL scheme not allowed",
		},
		{
			name:      "credentials in URL blocked",
			hostname:  "user:pass@google.com",
			clientIP:  "192.0.2.1",
			shouldErr: true,
			errReason: "credentials in hostname not allowed",
		},
		{
			name:      ".internal domain blocked",
			hostname:  "service.internal",
			clientIP:  "192.0.2.1",
			shouldErr: true,
			errReason: "internal domain access not allowed",
		},
		{
			name:      ".local domain blocked",
			hostname:  "router.local",
			clientIP:  "192.0.2.1",
			shouldErr: true,
			errReason: "internal domain access not allowed",
		},
		{
			name:      "localhost with trailing dot blocked",
			hostname:  "localhost.",
			clientIP:  "192.0.2.1",
			shouldErr: true,
			errReason: "localhost access not allowed",
		},
		{
			name:      "uppercase LOCALHOST trailing-dot blocked",
			hostname:  "LOCALHOST.",
			clientIP:  "192.0.2.1",
			shouldErr: true,
			errReason: "localhost access not allowed",
		},
		{
			name:      "metadata.google.internal trailing dot blocked",
			hostname:  "metadata.google.internal.",
			clientIP:  "192.0.2.1",
			shouldErr: true,
			errReason: "cloud metadata endpoint access not allowed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a new validator for each test to avoid rate limit interference
			validator := NewValidator(100, time.Minute)
			defer validator.Stop()

			ips, err := validator.ValidateHostname(tt.hostname, tt.clientIP)

			if tt.shouldErr {
				if err == nil {
					t.Errorf("Expected error for %s, got none", tt.hostname)
				} else {
					validationErr, ok := err.(*ValidationError)
					if !ok {
						t.Errorf("Expected ValidationError, got %T", err)
					} else if tt.errReason != "" && !strings.Contains(validationErr.Reason, tt.errReason) {
						t.Errorf("Expected error reason to contain '%s', got '%s'", tt.errReason, validationErr.Reason)
					}
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error for %s, got %v", tt.hostname, err)
				}
				if len(ips) == 0 {
					t.Errorf("Expected IPs for %s, got none", tt.hostname)
				}
			}
		})
	}
}

func TestValidateIP(t *testing.T) {
	tests := []struct {
		name      string
		ipStr     string
		clientIP  string
		shouldErr bool
		errReason string
	}{
		{
			name:      "valid public IPv4",
			ipStr:     "8.8.8.8",
			clientIP:  "192.0.2.1",
			shouldErr: false,
		},
		{
			name:      "loopback blocked",
			ipStr:     "127.0.0.1",
			clientIP:  "192.0.2.1",
			shouldErr: true,
			errReason: "loopback address not allowed",
		},
		{
			name:      "AWS metadata blocked",
			ipStr:     "169.254.169.254",
			clientIP:  "192.0.2.1",
			shouldErr: true,
			errReason: "link-local address not allowed",
		},
		{
			name:      "private 10.0.0.0/8",
			ipStr:     "10.1.2.3",
			clientIP:  "192.0.2.1",
			shouldErr: true,
			errReason: "private IP address not allowed",
		},
		{
			name:      "private 192.168.0.0/16",
			ipStr:     "192.168.100.50",
			clientIP:  "192.0.2.1",
			shouldErr: true,
			errReason: "private IP address not allowed",
		},
		{
			name:      "private 172.16.0.0/12",
			ipStr:     "172.20.0.1",
			clientIP:  "192.0.2.1",
			shouldErr: true,
			errReason: "private IP address not allowed",
		},
		{
			name:      "invalid IP format",
			ipStr:     "not-an-ip",
			clientIP:  "192.0.2.1",
			shouldErr: true,
			errReason: "invalid IP address format",
		},
		{
			name:      "multicast blocked",
			ipStr:     "224.0.0.1",
			clientIP:  "192.0.2.1",
			shouldErr: true,
			errReason: "link-local address not allowed",
		},
		{
			name:      "unspecified blocked",
			ipStr:     "0.0.0.0",
			clientIP:  "192.0.2.1",
			shouldErr: true,
			errReason: "unspecified address not allowed",
		},
		{
			name:      "IPv6 loopback blocked",
			ipStr:     "::1",
			clientIP:  "192.0.2.1",
			shouldErr: true,
			errReason: "loopback address not allowed",
		},
		{
			name:      "0.0.0.0/8 non-zero blocked (0.1.2.3 routes to loopback on Linux)",
			ipStr:     "0.1.2.3",
			clientIP:  "192.0.2.1",
			shouldErr: true,
			errReason: "0.0.0.0/8",
		},
		{
			name:      "0.0.0.0/8 high blocked",
			ipStr:     "0.255.255.255",
			clientIP:  "192.0.2.1",
			shouldErr: true,
			errReason: "0.0.0.0/8",
		},
		{
			name:      "IPv4-mapped IPv6 loopback blocked",
			ipStr:     "::ffff:127.0.0.1",
			clientIP:  "192.0.2.1",
			shouldErr: true,
			errReason: "loopback address not allowed",
		},
		{
			name:      "IPv4-mapped IPv6 private blocked",
			ipStr:     "::ffff:10.0.0.1",
			clientIP:  "192.0.2.1",
			shouldErr: true,
			errReason: "private IP address not allowed",
		},
		{
			name:      "IPv4-mapped IPv6 AWS metadata blocked",
			ipStr:     "::ffff:169.254.169.254",
			clientIP:  "192.0.2.1",
			shouldErr: true,
			errReason: "link-local address not allowed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a new validator for each test to avoid rate limit interference
			validator := NewValidator(100, time.Minute)
			defer validator.Stop()

			ip, err := validator.ValidateIP(tt.ipStr, tt.clientIP)

			if tt.shouldErr {
				if err == nil {
					t.Errorf("Expected error for %s, got none", tt.ipStr)
				} else {
					validationErr, ok := err.(*ValidationError)
					if !ok {
						t.Errorf("Expected ValidationError, got %T", err)
					} else if tt.errReason != "" && !strings.Contains(validationErr.Reason, tt.errReason) {
						t.Errorf("Expected error reason to contain '%s', got '%s'", tt.errReason, validationErr.Reason)
					}
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error for %s, got %v", tt.ipStr, err)
				}
				if ip == nil {
					t.Errorf("Expected IP for %s, got nil", tt.ipStr)
				}
			}
		})
	}
}

func TestIsPrivateIP(t *testing.T) {
	tests := []struct {
		name      string
		ip        string
		isPrivate bool
	}{
		{"10.0.0.0/8 - start", "10.0.0.1", true},
		{"10.0.0.0/8 - middle", "10.128.0.1", true},
		{"10.0.0.0/8 - end", "10.255.255.254", true},
		{"172.16.0.0/12 - start", "172.16.0.1", true},
		{"172.16.0.0/12 - middle", "172.20.0.1", true},
		{"172.16.0.0/12 - end", "172.31.255.254", true},
		{"192.168.0.0/16", "192.168.1.1", true},
		{"100.64.0.0/10 - CGNAT", "100.64.0.1", true},
		{"Public IP - 8.8.8.8", "8.8.8.8", false},
		{"Public IP - 1.1.1.1", "1.1.1.1", false},
		{"Link-local - 169.254.1.1", "169.254.1.1", false}, // Handled separately
		{"fc00::/7 - IPv6 ULA", "fc00::1", true},
		{"Public IPv6", "2001:4860:4860::8888", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip := net.ParseIP(tt.ip)
			if ip == nil {
				t.Fatalf("Failed to parse IP: %s", tt.ip)
			}

			result := isPrivateIP(ip)
			if result != tt.isPrivate {
				t.Errorf("isPrivateIP(%s) = %v, want %v", tt.ip, result, tt.isPrivate)
			}
		})
	}
}

func TestRateLimiting(t *testing.T) {
	// Create validator with low limits for testing
	validator := NewValidator(3, 100*time.Millisecond)
	defer validator.Stop()

	clientIP := "192.0.2.1"

	// First 3 requests should succeed
	for i := 0; i < 3; i++ {
		_, err := validator.ValidateIP("8.8.8.8", clientIP)
		if err != nil {
			t.Errorf("Request %d should succeed, got error: %v", i+1, err)
		}
	}

	// 4th request should fail (rate limit exceeded)
	_, err := validator.ValidateIP("8.8.8.8", clientIP)
	if err == nil {
		t.Error("Expected rate limit error, got none")
	} else {
		validationErr, ok := err.(*ValidationError)
		if !ok {
			t.Errorf("Expected ValidationError, got %T", err)
		} else if !strings.Contains(validationErr.Reason, "rate limit exceeded") {
			t.Errorf("Expected rate limit error, got: %s", validationErr.Reason)
		}
	}

	// Wait for rate limit window to reset
	time.Sleep(150 * time.Millisecond)

	// Should succeed again after window reset
	_, err = validator.ValidateIP("8.8.8.8", clientIP)
	if err != nil {
		t.Errorf("Request after reset should succeed, got error: %v", err)
	}
}

func TestRateLimitingPerClient(t *testing.T) {
	validator := NewValidator(2, 100*time.Millisecond)
	defer validator.Stop()

	client1 := "192.0.2.1"
	client2 := "192.0.2.2"

	// Client 1 uses up their quota
	for i := 0; i < 2; i++ {
		_, err := validator.ValidateIP("8.8.8.8", client1)
		if err != nil {
			t.Errorf("Client 1 request %d should succeed, got error: %v", i+1, err)
		}
	}

	// Client 1 exceeds quota
	_, err := validator.ValidateIP("8.8.8.8", client1)
	if err == nil {
		t.Error("Client 1 should be rate limited")
	}

	// Client 2 should still be able to make requests
	_, err = validator.ValidateIP("8.8.8.8", client2)
	if err != nil {
		t.Errorf("Client 2 should not be rate limited, got error: %v", err)
	}
}

func TestValidationErrorFormat(t *testing.T) {
	err := &ValidationError{
		Reason: "test reason",
		Target: "test.target",
	}

	expected := "SSRF protection: test reason (target: test.target)"
	if err.Error() != expected {
		t.Errorf("Error format mismatch.\nGot:  %s\nWant: %s", err.Error(), expected)
	}
}

func BenchmarkValidateHostname(b *testing.B) {
	validator := NewValidator(1000, time.Minute)
	defer validator.Stop()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = validator.ValidateHostname("google.com", "192.0.2.1")
	}
}

func BenchmarkValidateIP(b *testing.B) {
	validator := NewValidator(1000, time.Minute)
	defer validator.Stop()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = validator.ValidateIP("8.8.8.8", "192.0.2.1")
	}
}
