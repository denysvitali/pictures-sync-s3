package ratelimit

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestBasicRateLimiting tests basic rate limiting functionality
func TestBasicRateLimiting(t *testing.T) {
	config := Config{
		RequestsPerSecond: 2.0,
		Burst:             2,
		MaxAuthAttempts:   0, // Disable auth tracking for this test
		CleanupInterval:   1 * time.Minute,
		ClientExpiry:      5 * time.Minute,
	}

	limiter := NewLimiter(config)
	defer limiter.Stop()

	ip := "192.168.1.100"

	// First two requests should succeed (burst)
	if !limiter.Allow(ip, config) {
		t.Error("First request should be allowed")
	}
	if !limiter.Allow(ip, config) {
		t.Error("Second request should be allowed (burst)")
	}

	// Third request should fail (exceeded burst)
	if limiter.Allow(ip, config) {
		t.Error("Third request should be rate limited")
	}

	// Wait for token replenishment (0.5 seconds = 1 token at 2/sec)
	time.Sleep(550 * time.Millisecond)

	// Should allow one more request
	if !limiter.Allow(ip, config) {
		t.Error("Request after waiting should be allowed")
	}
}

// TestAuthFailureTracking tests authentication failure tracking
func TestAuthFailureTracking(t *testing.T) {
	config := Config{
		RequestsPerSecond: 10.0, // High rate limit for this test
		Burst:             10,
		MaxAuthAttempts:   3,
		AuthWindow:        1 * time.Minute,
		LockoutDuration:   2 * time.Second,
		CleanupInterval:   1 * time.Minute,
		ClientExpiry:      5 * time.Minute,
	}

	limiter := NewLimiter(config)
	defer limiter.Stop()

	ip := "192.168.1.101"

	// Record first two failures
	if limiter.RecordAuthFailure(ip, config) {
		t.Error("Should not be locked out after first failure")
	}
	if limiter.RecordAuthFailure(ip, config) {
		t.Error("Should not be locked out after second failure")
	}

	// Third failure should trigger lockout
	if !limiter.RecordAuthFailure(ip, config) {
		t.Error("Should be locked out after third failure")
	}

	// Verify lockout status
	if !limiter.IsLockedOut(ip) {
		t.Error("IP should be locked out")
	}

	// Verify failure count
	if count := limiter.GetAuthFailureCount(ip); count != 3 {
		t.Errorf("Expected 3 failures, got %d", count)
	}

	// Wait for lockout to expire
	time.Sleep(2100 * time.Millisecond)

	// Should no longer be locked out
	if limiter.IsLockedOut(ip) {
		t.Error("IP should no longer be locked out after expiry")
	}
}

// TestAuthFailureReset tests resetting authentication failures
func TestAuthFailureReset(t *testing.T) {
	config := Config{
		RequestsPerSecond: 10.0,
		Burst:             10,
		MaxAuthAttempts:   5,
		AuthWindow:        1 * time.Minute,
		LockoutDuration:   1 * time.Minute,
		CleanupInterval:   1 * time.Minute,
		ClientExpiry:      5 * time.Minute,
	}

	limiter := NewLimiter(config)
	defer limiter.Stop()

	ip := "192.168.1.102"

	// Record two failures
	limiter.RecordAuthFailure(ip, config)
	limiter.RecordAuthFailure(ip, config)

	if count := limiter.GetAuthFailureCount(ip); count != 2 {
		t.Errorf("Expected 2 failures, got %d", count)
	}

	// Reset failures (simulating successful auth)
	limiter.ResetAuthFailures(ip)

	if count := limiter.GetAuthFailureCount(ip); count != 0 {
		t.Errorf("Expected 0 failures after reset, got %d", count)
	}

	if limiter.IsLockedOut(ip) {
		t.Error("IP should not be locked out after reset")
	}
}

// TestAuthWindowExpiry tests that failures outside the auth window are reset
func TestAuthWindowExpiry(t *testing.T) {
	config := Config{
		RequestsPerSecond: 10.0,
		Burst:             10,
		MaxAuthAttempts:   3,
		AuthWindow:        1 * time.Second, // Short window for testing
		LockoutDuration:   1 * time.Minute,
		CleanupInterval:   1 * time.Minute,
		ClientExpiry:      5 * time.Minute,
	}

	limiter := NewLimiter(config)
	defer limiter.Stop()

	ip := "192.168.1.103"

	// Record first failure
	limiter.RecordAuthFailure(ip, config)

	if count := limiter.GetAuthFailureCount(ip); count != 1 {
		t.Errorf("Expected 1 failure, got %d", count)
	}

	// Wait for auth window to expire
	time.Sleep(1100 * time.Millisecond)

	// Next failure should reset the counter
	limiter.RecordAuthFailure(ip, config)

	if count := limiter.GetAuthFailureCount(ip); count != 1 {
		t.Errorf("Expected 1 failure after window expiry, got %d", count)
	}
}

// TestPerIPIsolation tests that rate limiting is per-IP
func TestPerIPIsolation(t *testing.T) {
	config := Config{
		RequestsPerSecond: 1.0,
		Burst:             1,
		MaxAuthAttempts:   0,
		CleanupInterval:   1 * time.Minute,
		ClientExpiry:      5 * time.Minute,
	}

	limiter := NewLimiter(config)
	defer limiter.Stop()

	ip1 := "192.168.1.104"
	ip2 := "192.168.1.105"

	// Exhaust limit for IP1
	limiter.Allow(ip1, config)
	if limiter.Allow(ip1, config) {
		t.Error("IP1 should be rate limited")
	}

	// IP2 should still be allowed
	if !limiter.Allow(ip2, config) {
		t.Error("IP2 should not be affected by IP1's rate limit")
	}
}

// TestMiddleware tests the rate limiting middleware
func TestMiddleware(t *testing.T) {
	config := Config{
		RequestsPerSecond: 1.0,
		Burst:             2,
		MaxAuthAttempts:   0,
		CleanupInterval:   1 * time.Minute,
		ClientExpiry:      5 * time.Minute,
	}

	limiter := NewLimiter(config)
	defer limiter.Stop()

	handler := limiter.Middleware(config)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	}))

	// Create test requests
	req1 := httptest.NewRequest("GET", "/test", nil)
	req1.RemoteAddr = "192.168.1.106:12345"
	req2 := httptest.NewRequest("GET", "/test", nil)
	req2.RemoteAddr = "192.168.1.106:12345"
	req3 := httptest.NewRequest("GET", "/test", nil)
	req3.RemoteAddr = "192.168.1.106:12345"

	// First two requests should succeed (burst)
	rr1 := httptest.NewRecorder()
	handler.ServeHTTP(rr1, req1)
	if rr1.Code != http.StatusOK {
		t.Errorf("First request failed: got %d, want %d", rr1.Code, http.StatusOK)
	}

	rr2 := httptest.NewRecorder()
	handler.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusOK {
		t.Errorf("Second request failed: got %d, want %d", rr2.Code, http.StatusOK)
	}

	// Third request should be rate limited
	rr3 := httptest.NewRecorder()
	handler.ServeHTTP(rr3, req3)
	if rr3.Code != http.StatusTooManyRequests {
		t.Errorf("Third request should be rate limited: got %d, want %d", rr3.Code, http.StatusTooManyRequests)
	}
}

// TestLockoutMiddleware tests that locked out IPs are blocked
func TestLockoutMiddleware(t *testing.T) {
	config := Config{
		RequestsPerSecond: 10.0,
		Burst:             10,
		MaxAuthAttempts:   2,
		AuthWindow:        1 * time.Minute,
		LockoutDuration:   1 * time.Second,
		CleanupInterval:   1 * time.Minute,
		ClientExpiry:      5 * time.Minute,
	}

	limiter := NewLimiter(config)
	defer limiter.Stop()

	ip := "192.168.1.107"

	// Record failures to trigger lockout
	limiter.RecordAuthFailure(ip, config)
	limiter.RecordAuthFailure(ip, config)

	if !limiter.IsLockedOut(ip) {
		t.Fatal("IP should be locked out")
	}

	handler := limiter.Middleware(config)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = ip + ":12345"

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusTooManyRequests {
		t.Errorf("Locked out IP should be blocked: got %d, want %d", rr.Code, http.StatusTooManyRequests)
	}

	// Wait for lockout to expire
	time.Sleep(1100 * time.Millisecond)

	// Should now be allowed
	rr2 := httptest.NewRecorder()
	handler.ServeHTTP(rr2, req)

	if rr2.Code != http.StatusOK {
		t.Errorf("Request should succeed after lockout expiry: got %d, want %d", rr2.Code, http.StatusOK)
	}
}

// TestCleanup tests the cleanup of old entries
func TestCleanup(t *testing.T) {
	config := Config{
		RequestsPerSecond: 1.0,
		Burst:             1,
		MaxAuthAttempts:   0,
		CleanupInterval:   100 * time.Millisecond, // Fast cleanup for testing
		ClientExpiry:      200 * time.Millisecond, // Short expiry for testing
	}

	limiter := NewLimiter(config)
	defer limiter.Stop()

	ip := "192.168.1.108"

	// Make a request
	limiter.Allow(ip, config)

	// Check that entry exists
	limiter.mu.RLock()
	if _, exists := limiter.limiters[ip]; !exists {
		t.Fatal("Entry should exist")
	}
	limiter.mu.RUnlock()

	// Wait for cleanup
	time.Sleep(400 * time.Millisecond)

	// Entry should be cleaned up
	limiter.mu.RLock()
	_, exists := limiter.limiters[ip]
	limiter.mu.RUnlock()

	if exists {
		t.Error("Entry should be cleaned up")
	}
}

// TestExtractIP tests IP extraction from various headers
func TestExtractIP(t *testing.T) {
	tests := []struct {
		name       string
		remoteAddr string
		xff        string
		xri        string
		expected   string
	}{
		{
			name:       "Direct connection",
			remoteAddr: "192.168.1.100:12345",
			expected:   "192.168.1.100",
		},
		{
			name:       "X-Forwarded-For single",
			remoteAddr: "10.0.0.1:12345",
			xff:        "203.0.113.1",
			expected:   "203.0.113.1",
		},
		{
			name:       "X-Forwarded-For multiple",
			remoteAddr: "10.0.0.1:12345",
			xff:        "203.0.113.1, 198.51.100.1",
			expected:   "203.0.113.1",
		},
		{
			name:       "X-Real-IP",
			remoteAddr: "10.0.0.1:12345",
			xri:        "203.0.113.2",
			expected:   "203.0.113.2",
		},
		{
			name:       "X-Forwarded-For takes precedence",
			remoteAddr: "10.0.0.1:12345",
			xff:        "203.0.113.3",
			xri:        "203.0.113.4",
			expected:   "203.0.113.3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/test", nil)
			req.RemoteAddr = tt.remoteAddr
			if tt.xff != "" {
				req.Header.Set("X-Forwarded-For", tt.xff)
			}
			if tt.xri != "" {
				req.Header.Set("X-Real-IP", tt.xri)
			}

			ip := extractIP(req)
			if ip != tt.expected {
				t.Errorf("Expected IP %s, got %s", tt.expected, ip)
			}
		})
	}
}

// TestConfigPresets tests the configuration presets
func TestConfigPresets(t *testing.T) {
	// Test default config
	defaultCfg := DefaultConfig()
	if defaultCfg.RequestsPerSecond != 1.0 {
		t.Errorf("Default config should have 1.0 req/sec, got %f", defaultCfg.RequestsPerSecond)
	}

	// Test auth config
	authCfg := AuthConfig()
	if authCfg.MaxAuthAttempts != 5 {
		t.Errorf("Auth config should have 5 max attempts, got %d", authCfg.MaxAuthAttempts)
	}
	if authCfg.AuthWindow != 15*time.Minute {
		t.Errorf("Auth config should have 15 minute window, got %v", authCfg.AuthWindow)
	}

	// Test expensive op config
	expensiveCfg := ExpensiveOpConfig()
	if expensiveCfg.RequestsPerSecond != 1.0 {
		t.Errorf("Expensive op config should have 1.0 req/sec, got %f", expensiveCfg.RequestsPerSecond)
	}
	if expensiveCfg.MaxAuthAttempts != 0 {
		t.Errorf("Expensive op config should have 0 auth attempts, got %d", expensiveCfg.MaxAuthAttempts)
	}
}

// TestConcurrentAccess tests thread safety
func TestConcurrentAccess(t *testing.T) {
	config := Config{
		RequestsPerSecond: 100.0, // High limit for concurrent test
		Burst:             100,
		MaxAuthAttempts:   0,
		CleanupInterval:   1 * time.Minute,
		ClientExpiry:      5 * time.Minute,
	}

	limiter := NewLimiter(config)
	defer limiter.Stop()

	done := make(chan bool)
	concurrency := 10
	requestsPerGoroutine := 100

	// Launch concurrent goroutines
	for i := 0; i < concurrency; i++ {
		go func(id int) {
			ip := "192.168.1.200"
			for j := 0; j < requestsPerGoroutine; j++ {
				limiter.Allow(ip, config)
			}
			done <- true
		}(i)
	}

	// Wait for all to complete
	for i := 0; i < concurrency; i++ {
		<-done
	}

	// If we got here without panicking, the test passed
}

// TestHandlerFunc tests the HandlerFunc wrapper
func TestHandlerFunc(t *testing.T) {
	config := Config{
		RequestsPerSecond: 1.0,
		Burst:             1,
		MaxAuthAttempts:   0,
		CleanupInterval:   1 * time.Minute,
		ClientExpiry:      5 * time.Minute,
	}

	limiter := NewLimiter(config)
	defer limiter.Stop()

	handler := limiter.HandlerFunc(config, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.109:12345"

	// First request should succeed
	rr1 := httptest.NewRecorder()
	handler(rr1, req)
	if rr1.Code != http.StatusOK {
		t.Errorf("First request failed: got %d, want %d", rr1.Code, http.StatusOK)
	}

	// Second request should be rate limited
	rr2 := httptest.NewRecorder()
	handler(rr2, req)
	if rr2.Code != http.StatusTooManyRequests {
		t.Errorf("Second request should be rate limited: got %d, want %d", rr2.Code, http.StatusTooManyRequests)
	}
}
