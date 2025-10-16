package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSecurityHeadersMiddleware(t *testing.T) {
	// Create a simple handler that just returns 200 OK
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	// Wrap with security headers middleware
	handler := SecurityHeadersMiddleware(nextHandler)

	tests := []struct {
		name           string
		requestHeaders map[string]string
		expectedHeader string
		expectedValue  string
		checkContains  bool // If true, check that value contains expectedValue, not exact match
	}{
		{
			name:           "X-Frame-Options is set",
			expectedHeader: "X-Frame-Options",
			expectedValue:  "DENY",
		},
		{
			name:           "X-Content-Type-Options is set",
			expectedHeader: "X-Content-Type-Options",
			expectedValue:  "nosniff",
		},
		{
			name:           "X-XSS-Protection is set",
			expectedHeader: "X-XSS-Protection",
			expectedValue:  "1; mode=block",
		},
		{
			name:           "Strict-Transport-Security is set",
			expectedHeader: "Strict-Transport-Security",
			expectedValue:  "max-age=31536000; includeSubDomains; preload",
		},
		{
			name:           "Referrer-Policy is set",
			expectedHeader: "Referrer-Policy",
			expectedValue:  "strict-origin-when-cross-origin",
		},
		{
			name:           "Permissions-Policy is set",
			expectedHeader: "Permissions-Policy",
			expectedValue:  "geolocation=(), microphone=(), camera=(), payment=()",
		},
		{
			name:           "Content-Security-Policy is set for normal requests",
			expectedHeader: "Content-Security-Policy",
			expectedValue:  "default-src 'self'",
			checkContains:  true,
		},
		{
			name: "Content-Security-Policy allows inline scripts",
			requestHeaders: map[string]string{
				"Accept": "text/html",
			},
			expectedHeader: "Content-Security-Policy",
			expectedValue:  "script-src 'self' 'unsafe-inline'",
			checkContains:  true,
		},
		{
			name: "Content-Security-Policy allows inline styles",
			requestHeaders: map[string]string{
				"Accept": "text/html",
			},
			expectedHeader: "Content-Security-Policy",
			expectedValue:  "style-src 'self' 'unsafe-inline'",
			checkContains:  true,
		},
		{
			name: "Content-Security-Policy allows WebSocket connections",
			requestHeaders: map[string]string{
				"Accept": "text/html",
			},
			expectedHeader: "Content-Security-Policy",
			expectedValue:  "connect-src 'self' ws: wss:",
			checkContains:  true,
		},
		{
			name: "Content-Security-Policy blocks plugins",
			requestHeaders: map[string]string{
				"Accept": "text/html",
			},
			expectedHeader: "Content-Security-Policy",
			expectedValue:  "object-src 'none'",
			checkContains:  true,
		},
		{
			name: "Content-Security-Policy prevents framing",
			requestHeaders: map[string]string{
				"Accept": "text/html",
			},
			expectedHeader: "Content-Security-Policy",
			expectedValue:  "frame-ancestors 'none'",
			checkContains:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)

			// Add any custom request headers
			for k, v := range tt.requestHeaders {
				req.Header.Set(k, v)
			}

			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			// Check that the expected header is present
			actualValue := rr.Header().Get(tt.expectedHeader)
			if actualValue == "" {
				t.Errorf("Expected header %s to be set, but it was empty", tt.expectedHeader)
				return
			}

			// Check the value
			if tt.checkContains {
				if !strings.Contains(actualValue, tt.expectedValue) {
					t.Errorf("Expected header %s to contain %q, got %q", tt.expectedHeader, tt.expectedValue, actualValue)
				}
			} else {
				if actualValue != tt.expectedValue {
					t.Errorf("Expected header %s to be %q, got %q", tt.expectedHeader, tt.expectedValue, actualValue)
				}
			}
		})
	}
}

func TestSecurityHeadersMiddleware_WebSocket(t *testing.T) {
	// Create a simple handler
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := SecurityHeadersMiddleware(nextHandler)

	// Test WebSocket upgrade request
	req := httptest.NewRequest("GET", "/ws", nil)
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Connection", "Upgrade")

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	// For WebSocket upgrades, CSP should NOT be set
	csp := rr.Header().Get("Content-Security-Policy")
	if csp != "" {
		t.Errorf("Content-Security-Policy should not be set for WebSocket upgrades, but got: %q", csp)
	}

	// But other security headers should still be present
	xfo := rr.Header().Get("X-Frame-Options")
	if xfo != "DENY" {
		t.Errorf("X-Frame-Options should still be set for WebSocket upgrades, got: %q", xfo)
	}

	hsts := rr.Header().Get("Strict-Transport-Security")
	if hsts == "" {
		t.Error("Strict-Transport-Security should still be set for WebSocket upgrades")
	}
}

func TestSecurityHeadersMiddleware_Integration(t *testing.T) {
	// Test that the middleware doesn't break the response
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})

	handler := SecurityHeadersMiddleware(nextHandler)

	req := httptest.NewRequest("GET", "/api/status", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	// Check that the response is still correct
	if rr.Code != http.StatusOK {
		t.Errorf("Expected status code 200, got %d", rr.Code)
	}

	if rr.Body.String() != `{"status":"ok"}` {
		t.Errorf("Expected response body %q, got %q", `{"status":"ok"}`, rr.Body.String())
	}

	// Check that security headers are present
	if rr.Header().Get("X-Frame-Options") == "" {
		t.Error("Security headers should be present in the response")
	}

	// Check that custom headers from handler are preserved
	if rr.Header().Get("Content-Type") != "application/json" {
		t.Error("Handler's custom headers should be preserved")
	}
}
