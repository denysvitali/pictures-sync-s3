package auth

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/denysvitali/pictures-sync-s3/pkg/ratelimit"
)

// TestBasicAuthSuccess tests successful authentication
func TestBasicAuthSuccess(t *testing.T) {
	limiter := ratelimit.NewLimiter(ratelimit.AuthConfig())
	defer limiter.Stop()

	middleware := BasicAuthMiddleware("testpass", limiter)
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("authenticated"))
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.100:12345"
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("gokrazy:testpass")))

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rr.Code)
	}

	if rr.Body.String() != "authenticated" {
		t.Errorf("Expected 'authenticated', got %s", rr.Body.String())
	}
}

// TestBasicAuthFailure tests failed authentication
func TestBasicAuthFailure(t *testing.T) {
	limiter := ratelimit.NewLimiter(ratelimit.AuthConfig())
	defer limiter.Stop()

	middleware := BasicAuthMiddleware("testpass", limiter)
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.101:12345"
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("gokrazy:wrongpass")))

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401, got %d", rr.Code)
	}

	// Check WWW-Authenticate header
	if auth := rr.Header().Get("WWW-Authenticate"); auth == "" {
		t.Error("Expected WWW-Authenticate header")
	}
}

// TestBasicAuthLockout tests account lockout after MaxAuthAttempts failures
func TestBasicAuthLockout(t *testing.T) {
	// AuthConfig has MaxAuthAttempts: 5; burst is high enough that the
	// per-IP request limiter does not kick in before lockout.
	limiter := ratelimit.NewLimiter(ratelimit.AuthConfig())
	defer limiter.Stop()

	middleware := BasicAuthMiddleware("testpass", limiter)
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	wrongAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("gokrazy:wrongpass"))

	// First MaxAuthAttempts-1 (=4) failed attempts return 401.
	for i := 0; i < 4; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "192.168.1.102:12345"
		req.Header.Set("Authorization", wrongAuth)

		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if rr.Code != http.StatusUnauthorized {
			t.Errorf("Attempt %d: Expected status 401, got %d", i+1, rr.Code)
		}
	}

	// 5th failed attempt trips MaxAuthAttempts and locks out the IP.
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.102:12345"
	req.Header.Set("Authorization", wrongAuth)

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusTooManyRequests {
		t.Errorf("5th failed attempt should trigger lockout (429), got status %d", rr.Code)
	}

	if count := limiter.GetAuthFailureCount("192.168.1.102"); count != 5 {
		t.Errorf("Expected 5 auth failures, got %d", count)
	}
}

// TestBasicAuthResetAfterSuccess tests that successful auth resets failure count
func TestBasicAuthResetAfterSuccess(t *testing.T) {
	limiter := ratelimit.NewLimiter(ratelimit.AuthConfig())
	defer limiter.Stop()

	middleware := BasicAuthMiddleware("testpass", limiter)
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	wrongAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("gokrazy:wrongpass"))
	correctAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("gokrazy:testpass"))

	ip := "192.168.1.103"

	// Make 2 failed attempts
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = ip + ":12345"
		req.Header.Set("Authorization", wrongAuth)

		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
	}

	// Verify failures were recorded
	if count := limiter.GetAuthFailureCount(ip); count != 2 {
		t.Errorf("Expected 2 failures, got %d", count)
	}

	// Successful auth
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = ip + ":12345"
	req.Header.Set("Authorization", correctAuth)

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected successful auth, got status %d", rr.Code)
	}

	// Verify failures were reset
	if count := limiter.GetAuthFailureCount(ip); count != 0 {
		t.Errorf("Expected 0 failures after successful auth, got %d", count)
	}
}

// TestBasicAuthRateLimit tests general rate limiting
func TestBasicAuthRateLimit(t *testing.T) {
	limiter := ratelimit.NewLimiter(ratelimit.AuthConfig())
	defer limiter.Stop()

	middleware := BasicAuthMiddleware("testpass", limiter)
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	correctAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("gokrazy:testpass"))

	burst := ratelimit.AuthConfig().Burst

	for i := 0; i < burst; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "192.168.1.104:12345"
		req.Header.Set("Authorization", correctAuth)

		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("Request %d should succeed (within burst), got status %d", i+1, rr.Code)
		}
	}

	// Next request should be rate limited (burst exhausted)
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.104:12345"
	req.Header.Set("Authorization", correctAuth)

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusTooManyRequests {
		t.Errorf("Request after burst should be rate limited, got status %d", rr.Code)
	}
}

// TestBasicAuthPerIPIsolation tests that auth failures are per-IP
func TestBasicAuthPerIPIsolation(t *testing.T) {
	// Use the actual AuthConfig that the middleware will use
	limiter := ratelimit.NewLimiter(ratelimit.AuthConfig())
	defer limiter.Stop()

	middleware := BasicAuthMiddleware("testpass", limiter)
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	wrongAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("gokrazy:wrongpass"))
	correctAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("gokrazy:testpass"))

	// Verify failed auth attempts are tracked separately per IP.

	// Make 3 failed attempts for IP1.
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "192.168.1.105:12345"
		req.Header.Set("Authorization", wrongAuth)

		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if rr.Code != http.StatusUnauthorized {
			t.Errorf("Attempt %d should fail with 401, got %d", i+1, rr.Code)
		}
	}

	// Verify IP1 has 3 auth failures recorded
	if count := limiter.GetAuthFailureCount("192.168.1.105"); count != 3 {
		t.Errorf("Expected 3 failures for IP1, got %d", count)
	}

	// IP2 should still be able to authenticate
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.106:12345"
	req.Header.Set("Authorization", correctAuth)

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("IP2 should not be affected by IP1's lockout, got status %d", rr.Code)
	}
}

// TestExpensiveOperationMiddleware tests the expensive operation rate limiter
func TestExpensiveOperationMiddleware(t *testing.T) {
	limiter := ratelimit.NewLimiter(ratelimit.ExpensiveOpConfig())
	defer limiter.Stop()

	middleware := ExpensiveOperationMiddleware(limiter)
	handler := middleware(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	burst := ratelimit.ExpensiveOpConfig().Burst

	// All requests within the burst should succeed.
	for i := 0; i < burst; i++ {
		req := httptest.NewRequest("GET", "/api/thumbnail", nil)
		req.RemoteAddr = "192.168.1.107:12345"

		rr := httptest.NewRecorder()
		handler(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("Request %d should succeed (within burst), got status %d", i+1, rr.Code)
		}
	}

	// Next request should be rate limited (burst exhausted)
	req := httptest.NewRequest("GET", "/api/thumbnail", nil)
	req.RemoteAddr = "192.168.1.107:12345"

	rr := httptest.NewRecorder()
	handler(rr, req)

	if rr.Code != http.StatusTooManyRequests {
		t.Errorf("Request after burst should be rate limited, got status %d", rr.Code)
	}
}

func TestCORSMiddlewareAllowAll(t *testing.T) {
	// SECURITY: wildcard '*' is incompatible with credentialed CORS requests,
	// so allowCredentials must be false when using '*'. Verify wildcard
	// reflection works in that mode.
	handler := CORSMiddleware([]string{"*"}, false)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodOptions, "https://192.168.10.124:8080/api/settings", nil)
	req.Header.Set("Origin", "https://example.com")
	req.Header.Set("Access-Control-Request-Method", "GET")

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("Expected preflight status 204, got %d", rr.Code)
	}
	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "https://example.com" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want %q", got, "https://example.com")
	}
	if got := rr.Header().Get("Access-Control-Allow-Credentials"); got != "" {
		t.Fatalf("Access-Control-Allow-Credentials = %q, want empty (credentials must not be combined with '*')", got)
	}
}

// TestCORSMiddleware_WildcardWithCredentialsDropsWildcard verifies that callers
// who misconfigure '*' with allowCredentials=true never see a permissive
// CORS response. The wildcard is silently dropped, so no CORS headers are
// emitted (no Access-Control-Allow-Origin / Access-Control-Allow-Credentials).
func TestCORSMiddleware_WildcardWithCredentialsDropsWildcard(t *testing.T) {
	handler := CORSMiddleware([]string{"*"}, true)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "https://server.local/api/settings", nil)
	req.Host = "server.local"
	req.Header.Set("Origin", "https://example.com")

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want empty (wildcard must be dropped)", got)
	}
	if got := rr.Header().Get("Access-Control-Allow-Credentials"); got != "" {
		t.Fatalf("Access-Control-Allow-Credentials = %q, want empty (must not combine '*' with credentials)", got)
	}
}

// TestExtractIPFromHeaders tests IP extraction from various headers
func TestExtractIPFromHeaders(t *testing.T) {
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
			name:       "X-Forwarded-For",
			remoteAddr: "10.0.0.1:12345",
			xff:        "203.0.113.1",
			expected:   "203.0.113.1",
		},
		{
			name:       "X-Forwarded-For with multiple IPs",
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

// TestCSRFProtection tests CSRF token validation
func TestCSRFProtection(t *testing.T) {
	InitCSRFToken()
	token := GetCSRFToken()

	handler := CSRFProtection(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	// GET request should not require CSRF token
	req := httptest.NewRequest("GET", "/test", nil)
	rr := httptest.NewRecorder()
	handler(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("GET request should succeed without CSRF token, got status %d", rr.Code)
	}

	// POST without token should fail
	req = httptest.NewRequest("POST", "/test", nil)
	rr = httptest.NewRecorder()
	handler(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("POST without CSRF token should fail, got status %d", rr.Code)
	}

	// POST with valid token should succeed
	req = httptest.NewRequest("POST", "/test", nil)
	req.Header.Set("X-CSRF-Token", token)
	rr = httptest.NewRecorder()
	handler(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("POST with valid CSRF token should succeed, got status %d", rr.Code)
	}

	// POST with invalid token should fail
	req = httptest.NewRequest("POST", "/test", nil)
	req.Header.Set("X-CSRF-Token", "invalid-token")
	rr = httptest.NewRecorder()
	handler(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("POST with invalid CSRF token should fail, got status %d", rr.Code)
	}
}

// TestSecurityHeaders tests that security headers are applied
func TestSecurityHeaders(t *testing.T) {
	handler := SecurityHeadersMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	// Check for presence of security headers
	headers := map[string]string{
		"X-Frame-Options":           "DENY",
		"X-Content-Type-Options":    "nosniff",
		"X-XSS-Protection":          "1; mode=block",
		"Strict-Transport-Security": "max-age=31536000; includeSubDomains; preload",
		"Referrer-Policy":           "strict-origin-when-cross-origin",
		"Permissions-Policy":        "geolocation=(), microphone=(), camera=(), payment=()",
	}

	for header, expected := range headers {
		actual := rr.Header().Get(header)
		if actual != expected {
			t.Errorf("Header %s: expected %s, got %s", header, expected, actual)
		}
	}

	// Check for CSP header
	csp := rr.Header().Get("Content-Security-Policy")
	if csp == "" {
		t.Error("Content-Security-Policy header should be present")
	}
}

// TestSecurityHeadersWebSocket tests that CSP is skipped for WebSocket upgrades
func TestSecurityHeadersWebSocket(t *testing.T) {
	handler := SecurityHeadersMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/ws", nil)
	req.Header.Set("Upgrade", "websocket")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	// CSP should not be present for WebSocket
	csp := rr.Header().Get("Content-Security-Policy")
	if csp != "" {
		t.Error("Content-Security-Policy should not be present for WebSocket upgrade")
	}

	// Other headers should still be present
	if rr.Header().Get("X-Frame-Options") != "DENY" {
		t.Error("X-Frame-Options should still be present for WebSocket")
	}
}

// TestBasicAuthWithNilRateLimiter tests that authentication works when rate limiter is nil
func TestBasicAuthWithNilRateLimiter(t *testing.T) {
	// Pass nil rate limiter - this should not crash
	middleware := BasicAuthMiddleware("testpass", nil)
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("authenticated"))
	}))

	t.Run("successful auth with nil limiter", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "192.168.1.200:12345"
		req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("gokrazy:testpass")))

		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", rr.Code)
		}

		if rr.Body.String() != "authenticated" {
			t.Errorf("Expected 'authenticated', got %s", rr.Body.String())
		}
	})

	t.Run("failed auth with nil limiter", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "192.168.1.201:12345"
		req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("gokrazy:wrongpass")))

		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if rr.Code != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d", rr.Code)
		}

		// Check WWW-Authenticate header is still present
		if auth := rr.Header().Get("WWW-Authenticate"); auth == "" {
			t.Error("Expected WWW-Authenticate header")
		}
	})

	t.Run("multiple failed attempts with nil limiter", func(t *testing.T) {
		// With nil limiter, there should be no rate limiting or lockout
		wrongAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("gokrazy:wrongpass"))

		// Make many failed attempts - none should be rate limited
		for i := 0; i < 10; i++ {
			req := httptest.NewRequest("GET", "/test", nil)
			req.RemoteAddr = "192.168.1.202:12345"
			req.Header.Set("Authorization", wrongAuth)

			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			if rr.Code != http.StatusUnauthorized {
				t.Errorf("Attempt %d: Expected status 401 (not rate limited), got %d", i+1, rr.Code)
			}
		}
	})

	t.Run("rapid requests with nil limiter", func(t *testing.T) {
		// With nil limiter, there should be no rate limiting even for rapid requests
		correctAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("gokrazy:testpass"))

		for i := 0; i < 10; i++ {
			req := httptest.NewRequest("GET", "/test", nil)
			req.RemoteAddr = "192.168.1.203:12345"
			req.Header.Set("Authorization", correctAuth)

			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			if rr.Code != http.StatusOK {
				t.Errorf("Request %d: Expected status 200 (not rate limited), got %d", i+1, rr.Code)
			}
		}
	})
}

type mutablePasswordProvider struct {
	password string
}

func (p *mutablePasswordProvider) CurrentPassword() string {
	return p.password
}

func TestBasicAuthMiddlewareWithProviderUsesCurrentPassword(t *testing.T) {
	provider := &mutablePasswordProvider{password: "old-password"}
	middleware := BasicAuthMiddlewareWithProvider(provider, nil)
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.203:12345"
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("gokrazy:old-password")))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("old password should authenticate, got %d", rr.Code)
	}

	provider.password = "new-password"

	req = httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.203:12345"
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("gokrazy:old-password")))
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("old password should fail after provider update, got %d", rr.Code)
	}

	req = httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.203:12345"
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("gokrazy:new-password")))
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("new password should authenticate, got %d", rr.Code)
	}
}
