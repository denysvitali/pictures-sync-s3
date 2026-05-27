package middleware

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// closeTrackingBody wraps an io.Reader and records how many times Close is
// invoked, so tests can assert that DecodeJSON closes the body.
type closeTrackingBody struct {
	io.Reader
	closes atomic.Int32
}

func (c *closeTrackingBody) Close() error {
	c.closes.Add(1)
	return nil
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

	WriteError(w, http.StatusBadRequest, "test error", map[string]any{
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

// TestDecodeJSON_ClosesBody verifies DecodeJSON closes the request body so
// HTTP/1.1 keep-alive connections can be reused. Covers success, parse-error,
// and size-limit-exceeded paths.
func TestDecodeJSON_ClosesBody(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		maxBytes int64
	}{
		{name: "success", body: `{"key":"value"}`, maxBytes: 1024},
		{name: "parse error", body: `{not json`, maxBytes: 1024},
		{name: "too large", body: `{"key":"a very long value that exceeds the cap"}`, maxBytes: 10},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tracker := &closeTrackingBody{Reader: bytes.NewBufferString(tt.body)}
			req := httptest.NewRequest(http.MethodPost, "/test", nil)
			req.Body = tracker

			var result map[string]string
			_ = DecodeJSON(req, &result, tt.maxBytes)

			if got := tracker.closes.Load(); got < 1 {
				t.Errorf("Expected request body to be closed at least once, got %d closes", got)
			}
		})
	}
}

// TestRecovery_ReRaisesErrAbortHandler verifies http.ErrAbortHandler panics
// propagate out of Recovery so the http.Server can abort the connection.
func TestRecovery_ReRaisesErrAbortHandler(t *testing.T) {
	handler := Recovery(func(w http.ResponseWriter, r *http.Request) error {
		panic(http.ErrAbortHandler)
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()

	defer func() {
		rec := recover()
		if rec == nil {
			t.Fatal("Expected Recovery to re-raise http.ErrAbortHandler, but no panic occurred")
		}
		if rec != http.ErrAbortHandler {
			t.Errorf("Expected recovered value to be http.ErrAbortHandler, got %v", rec)
		}
	}()

	_ = handler(w, req)
}

// TestRecovery_NoDoubleWriteAfterPartialResponse ensures Recovery does not
// emit a superfluous 500 header or append a JSON error body when the wrapped
// handler has already written part of the response before panicking.
func TestRecovery_NoDoubleWriteAfterPartialResponse(t *testing.T) {
	handler := Recovery(func(w http.ResponseWriter, r *http.Request) error {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("partial-body"))
		panic("boom after write")
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()

	_ = handler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status to remain 200 (first WriteHeader wins), got %d", w.Code)
	}
	body := w.Body.String()
	if body != "partial-body" {
		t.Errorf("Expected body to be exactly %q (no appended error JSON), got %q", "partial-body", body)
	}
}

// failingResponseWriter forces WriteJSON's json.Encoder to fail so we can
// exercise the WriteError logging path that handles a write error.
type failingResponseWriter struct {
	header http.Header
	code   int
}

func (f *failingResponseWriter) Header() http.Header {
	if f.header == nil {
		f.header = http.Header{}
	}
	return f.header
}
func (f *failingResponseWriter) WriteHeader(code int)         { f.code = code }
func (f *failingResponseWriter) Write(b []byte) (int, error) { return 0, io.ErrClosedPipe }

// TestWriteError_WriteFailure exercises the branch where WriteJSON returns an
// error inside WriteError (the log.Printf path). It must not panic.
func TestWriteError_WriteFailure(t *testing.T) {
	f := &failingResponseWriter{}
	WriteError(f, http.StatusInternalServerError, "boom", nil)
	if f.code != http.StatusInternalServerError {
		t.Errorf("Expected status 500, got %d", f.code)
	}
}

// TestDecodeJSON_NilBody verifies DecodeJSON gracefully returns io.EOF when
// the request body is nil rather than panicking.
func TestDecodeJSON_NilBody(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/test", nil)
	req.Body = nil
	var result map[string]string
	err := DecodeJSON(req, &result, 1024)
	if err != io.EOF {
		t.Errorf("Expected io.EOF for nil body, got %v", err)
	}
}

// TestDecodeJSON_NoMaxBytes verifies that maxBytes <= 0 disables the size
// limiter (covers the `if maxBytes > 0` false branch).
func TestDecodeJSON_NoMaxBytes(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewBufferString(`{"key":"value"}`))
	var result map[string]string
	if err := DecodeJSON(req, &result, 0); err != nil {
		t.Errorf("Unexpected error with maxBytes=0: %v", err)
	}
	if result["key"] != "value" {
		t.Errorf("Expected key=value, got %v", result)
	}
}

// TestGetClientIP_UntrustedProxySource verifies that X-Forwarded-For /
// X-Real-IP are ignored when RemoteAddr is a public (non-private, non-loopback)
// address — the spoofing-prevention path.
func TestGetClientIP_UntrustedProxySource(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.RemoteAddr = "8.8.8.8:1234"
	req.Header.Set("X-Forwarded-For", "203.0.113.1")
	req.Header.Set("X-Real-IP", "203.0.113.2")

	ip := GetClientIP(req)
	if ip != "8.8.8.8" {
		t.Errorf("Expected untrusted RemoteAddr to be used, got %s", ip)
	}
}

// TestGetClientIP_LoopbackTrusted verifies loopback RemoteAddr is treated as
// a trusted proxy source and forwarded headers are honored.
func TestGetClientIP_LoopbackTrusted(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("X-Forwarded-For", "203.0.113.5")

	if ip := GetClientIP(req); ip != "203.0.113.5" {
		t.Errorf("Expected loopback to trust XFF, got %s", ip)
	}
}

// TestGetClientIP_IPv6LoopbackTrusted ensures bracketed IPv6 loopback is also
// treated as a trusted proxy source (covers the bracket-stripping branch).
func TestGetClientIP_IPv6LoopbackTrusted(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.RemoteAddr = "[::1]:1234"
	req.Header.Set("X-Real-IP", "203.0.113.7")

	if ip := GetClientIP(req); ip != "203.0.113.7" {
		t.Errorf("Expected IPv6 loopback to trust X-Real-IP, got %s", ip)
	}
}

// TestRecovery_Integration runs Recovery against a real httptest.NewServer to
// confirm a panic in the wrapped handler still produces a usable 500 response
// over a live HTTP connection.
func TestRecovery_Integration(t *testing.T) {
	handler := Recovery(func(w http.ResponseWriter, r *http.Request) error {
		panic("integration boom")
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = handler(w, r)
	}))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/whatever")
	if err != nil {
		t.Fatalf("HTTP GET failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("Expected 500, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	var er ErrorResponse
	if err := json.Unmarshal(body, &er); err != nil {
		t.Fatalf("Expected JSON error body, got %q (err=%v)", string(body), err)
	}
	if er.Error == "" {
		t.Error("Expected non-empty error message")
	}
}

// TestRequestLogger_Integration confirms RequestLogger forwards to the next
// handler and preserves status codes over a real HTTP server.
func TestRequestLogger_Integration(t *testing.T) {
	handler := RequestLogger(func(w http.ResponseWriter, r *http.Request) error {
		w.WriteHeader(http.StatusTeapot)
		return nil
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = handler(w, r)
	}))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatalf("HTTP GET failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusTeapot {
		t.Errorf("Expected 418, got %d", resp.StatusCode)
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
