package middleware

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMethodOnly(t *testing.T) {
	tests := []struct {
		name           string
		allowedMethods []string
		requestMethod  string
		expectAllow    bool
	}{
		{"GET allowed", []string{http.MethodGet}, http.MethodGet, true},
		{"POST not allowed", []string{http.MethodGet}, http.MethodPost, false},
		{"Multiple methods allowed", []string{http.MethodGet, http.MethodPost}, http.MethodGet, true},
		{"PUT not in allowed list", []string{http.MethodGet, http.MethodPost}, http.MethodPut, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := MethodOnly(tt.allowedMethods...)(func(w http.ResponseWriter, r *http.Request) error {
				w.WriteHeader(http.StatusOK)
				return nil
			})

			req := httptest.NewRequest(tt.requestMethod, "/test", nil)
			w := httptest.NewRecorder()

			_ = handler(w, req)

			if tt.expectAllow {
				if w.Code != http.StatusOK {
					t.Errorf("Expected status 200 for allowed method, got %d", w.Code)
				}
			} else {
				if w.Code != http.StatusMethodNotAllowed {
					t.Errorf("Expected status 405 for disallowed method, got %d", w.Code)
				}
			}
		})
	}
}

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

func TestChain(t *testing.T) {
	// Test that middlewares are applied in the correct order
	var executionOrder []string

	middleware1 := func(next HandlerFunc) HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) error {
			executionOrder = append(executionOrder, "middleware1_before")
			err := next(w, r)
			executionOrder = append(executionOrder, "middleware1_after")
			return err
		}
	}

	middleware2 := func(next HandlerFunc) HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) error {
			executionOrder = append(executionOrder, "middleware2_before")
			err := next(w, r)
			executionOrder = append(executionOrder, "middleware2_after")
			return err
		}
	}

	handler := func(w http.ResponseWriter, r *http.Request) error {
		executionOrder = append(executionOrder, "handler")
		return nil
	}

	chained := Chain(middleware1, middleware2)(handler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()

	_ = chained(w, req)

	expected := []string{
		"middleware1_before",
		"middleware2_before",
		"handler",
		"middleware2_after",
		"middleware1_after",
	}

	if len(executionOrder) != len(expected) {
		t.Fatalf("Expected %d execution steps, got %d", len(expected), len(executionOrder))
	}

	for i, step := range expected {
		if executionOrder[i] != step {
			t.Errorf("Step %d: expected %s, got %s", i, step, executionOrder[i])
		}
	}
}

func TestAdapt(t *testing.T) {
	tests := []struct {
		name           string
		handler        HandlerFunc
		expectedStatus int
	}{
		{
			name: "successful handler",
			handler: func(w http.ResponseWriter, r *http.Request) error {
				w.WriteHeader(http.StatusOK)
				return nil
			},
			expectedStatus: http.StatusOK,
		},
		{
			name: "handler with error",
			handler: func(w http.ResponseWriter, r *http.Request) error {
				return errors.New("test error")
			},
			expectedStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adapted := Adapt(tt.handler)
			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			w := httptest.NewRecorder()

			adapted(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, w.Code)
			}
		})
	}
}

func TestRequireQueryParam(t *testing.T) {
	tests := []struct {
		name          string
		param         string
		queryString   string
		expectSuccess bool
	}{
		{"param present", "id", "?id=123", true},
		{"param missing", "id", "", false},
		{"different param", "id", "?name=test", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := RequireQueryParam(tt.param)(func(w http.ResponseWriter, r *http.Request) error {
				w.WriteHeader(http.StatusOK)
				return nil
			})

			req := httptest.NewRequest(http.MethodGet, "/test"+tt.queryString, nil)
			w := httptest.NewRecorder()

			_ = handler(w, req)

			if tt.expectSuccess {
				if w.Code != http.StatusOK {
					t.Errorf("Expected status 200 when param present, got %d", w.Code)
				}
			} else {
				if w.Code != http.StatusBadRequest {
					t.Errorf("Expected status 400 when param missing, got %d", w.Code)
				}
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

// TestWriteSuccess tests success response helper
func TestWriteSuccess(t *testing.T) {
	w := httptest.NewRecorder()

	WriteSuccess(w, "operation completed", map[string]interface{}{
		"id": "123",
	})

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var response SuccessResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode success response: %v", err)
	}

	if response.Status != "ok" {
		t.Errorf("Expected status 'ok', got '%s'", response.Status)
	}
	if response.Message != "operation completed" {
		t.Errorf("Expected message 'operation completed', got '%s'", response.Message)
	}
}

// TestDecodeJSON tests JSON decoding helper
func TestDecodeJSON(t *testing.T) {
	tests := []struct {
		name        string
		body        string
		maxBytes    int64
		expectError bool
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
			var result map[string]string

			err := DecodeJSON(req, &result, tt.maxBytes)

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

// TestChain_EmptyChain tests chaining with no middlewares
func TestChain_EmptyChain(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) error {
		w.WriteHeader(http.StatusOK)
		return nil
	}

	chained := Chain()(handler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()

	err := chained(w, req)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
}

// BenchmarkMethodOnly measures method validation performance
func BenchmarkMethodOnly(b *testing.B) {
	handler := MethodOnly(http.MethodGet)(func(w http.ResponseWriter, r *http.Request) error {
		return nil
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		_ = handler(w, req)
	}
}

// BenchmarkChain measures chaining performance
func BenchmarkChain(b *testing.B) {
	middleware1 := func(next HandlerFunc) HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) error {
			return next(w, r)
		}
	}

	middleware2 := func(next HandlerFunc) HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) error {
			return next(w, r)
		}
	}

	handler := func(w http.ResponseWriter, r *http.Request) error {
		return nil
	}

	chained := Chain(middleware1, middleware2)(handler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		_ = chained(w, req)
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
