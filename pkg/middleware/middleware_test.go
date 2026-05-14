package middleware

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRecovery(t *testing.T) {
	handler := Recovery(func(w http.ResponseWriter, r *http.Request) error {
		panic("test panic")
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()

	// Should not panic, should return 500
	_ = handler(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("Expected status 500 after panic recovery, got %d", w.Code)
	}
}

func TestGetClientIP(t *testing.T) {
	tests := []struct {
		name       string
		headers    map[string]string
		remoteAddr string
		expected   string
	}{
		{
			name:       "X-Forwarded-For single IP",
			headers:    map[string]string{"X-Forwarded-For": "203.0.113.1"},
			remoteAddr: "192.168.1.1:8080",
			expected:   "203.0.113.1",
		},
		{
			name:       "X-Forwarded-For multiple IPs",
			headers:    map[string]string{"X-Forwarded-For": "203.0.113.1, 198.51.100.1"},
			remoteAddr: "192.168.1.1:8080",
			expected:   "203.0.113.1",
		},
		{
			name:       "X-Real-IP",
			headers:    map[string]string{"X-Real-IP": "203.0.113.2"},
			remoteAddr: "192.168.1.1:8080",
			expected:   "203.0.113.2",
		},
		{
			name:       "RemoteAddr fallback",
			headers:    map[string]string{},
			remoteAddr: "192.168.1.1:8080",
			expected:   "192.168.1.1",
		},
		{
			name:       "X-Forwarded-For takes precedence",
			headers:    map[string]string{"X-Forwarded-For": "203.0.113.1", "X-Real-IP": "203.0.113.2"},
			remoteAddr: "192.168.1.1:8080",
			expected:   "203.0.113.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			req.RemoteAddr = tt.remoteAddr
			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}

			ip := GetClientIP(req)
			if ip != tt.expected {
				t.Errorf("Expected IP %s, got %s", tt.expected, ip)
			}
		})
	}
}

// TestWriteJSON tests JSON writing helper
func TestWriteJSON(t *testing.T) {
	w := httptest.NewRecorder()
	data := map[string]string{"key": "value"}

	err := WriteJSON(w, http.StatusOK, data)
	if err != nil {
		t.Fatalf("WriteJSON failed: %v", err)
	}

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	if w.Header().Get("Content-Type") != "application/json" {
		t.Error("Expected Content-Type to be application/json")
	}
}

// TestWriteError tests error response helper
func TestWriteError(t *testing.T) {
	w := httptest.NewRecorder()

	WriteError(w, http.StatusBadRequest, "test error", map[string]interface{}{
		"field": "test_field",
	})

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", w.Code)
	}

	var response ErrorResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode error response: %v", err)
	}

	if response.Error != "test error" {
		t.Errorf("Expected error 'test error', got '%s'", response.Error)
	}
}

// TestDecodeJSON tests JSON decoding helper
func TestDecodeJSON(t *testing.T) {
	tests := []struct {
		name        string
		body        string
		maxBytes    int64
		expectError bool
		useStruct   bool
	}{
		{
			name:        "valid JSON",
			body:        `{"key":"value"}`,
			maxBytes:    1024,
			expectError: false,
		},
		{
			name:        "invalid JSON",
			body:        `{invalid}`,
			maxBytes:    1024,
			expectError: true,
		},
		{
			name:        "unknown fields",
			body:        `{"key":"value","unknown":"field"}`,
			maxBytes:    1024,
			expectError: true, // DisallowUnknownFields is set
			useStruct:   true,
		},
		{
			name:        "too large",
			body:        `{"key":"very long value that exceeds the limit"}`,
			maxBytes:    10,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewBufferString(tt.body))

			var err error
			if tt.useStruct {
				var result struct{ Key string }
				err = DecodeJSON(req, &result, tt.maxBytes)
			} else {
				var result map[string]string
				err = DecodeJSON(req, &result, tt.maxBytes)
			}

			if tt.expectError && err == nil {
				t.Error("Expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}

// TestRequestLogger tests request logging middleware
func TestRequestLogger(t *testing.T) {
	handler := RequestLogger(func(w http.ResponseWriter, r *http.Request) error {
		w.WriteHeader(http.StatusOK)
		return nil
	})

	req := httptest.NewRequest(http.MethodGet, "/test/path", nil)
	w := httptest.NewRecorder()

	err := handler(w, req)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
}

// TestGetClientIP_EdgeCases tests edge cases for IP extraction
func TestGetClientIP_EdgeCases(t *testing.T) {
	tests := []struct {
		name       string
		remoteAddr string
		expected   string
	}{
		{
			name:       "IPv6 address",
			remoteAddr: "[2001:db8::1]:8080",
			expected:   "[2001:db8::1]",
		},
		{
			name:       "No port",
			remoteAddr: "192.168.1.1",
			expected:   "192.168.1.1",
		},
		{
			name:       "Empty",
			remoteAddr: "",
			expected:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			req.RemoteAddr = tt.remoteAddr

			ip := GetClientIP(req)
			if ip != tt.expected {
				t.Errorf("Expected IP %s, got %s", tt.expected, ip)
			}
		})
	}
}

// TestRecovery_NoError tests recovery middleware with successful handler
func TestRecovery_NoError(t *testing.T) {
	handler := Recovery(func(w http.ResponseWriter, r *http.Request) error {
		w.WriteHeader(http.StatusOK)
		return nil
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()

	err := handler(w, req)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
}

// BenchmarkGetClientIP measures IP extraction performance
func BenchmarkGetClientIP(b *testing.B) {
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("X-Forwarded-For", "203.0.113.1, 198.51.100.1, 192.0.2.1")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = GetClientIP(req)
	}
}
