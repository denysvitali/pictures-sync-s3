package auth

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestConstantTimeEqualString verifies behavioral correctness of the
// length-safe constant-time comparison helper. Timing properties cannot be
// proven by a unit test, but correctness across edge cases can.
func TestConstantTimeEqualString(t *testing.T) {
	cases := []struct {
		name string
		a, b string
		want bool
	}{
		{name: "equal non-empty", a: "photo-backup", b: "photo-backup", want: true},
		{name: "equal empty", a: "", b: "", want: true},
		{name: "different same length", a: "photo-backup", b: "photo-bakcup", want: false},
		{name: "shorter b", a: "photo-backup", b: "photo", want: false},
		{name: "longer b", a: "photo", b: "photo-backup", want: false},
		{name: "empty vs non-empty", a: "", b: "photo", want: false},
		{name: "non-empty vs empty", a: "photo", b: "", want: false},
		{name: "binary bytes", a: "\x00\x01\x02", b: "\x00\x01\x02", want: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := constantTimeEqualString(tc.a, tc.b); got != tc.want {
				t.Fatalf("constantTimeEqualString(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
			}
		})
	}
}

// TestBasicAuthRejectsOversizedFields proves that the BasicAuth middleware
// caps the size of username/password values to prevent CPU amplification
// attacks via huge Authorization headers.
func TestBasicAuthRejectsOversizedFields(t *testing.T) {
	mw := BasicAuthMiddleware("testpass", nil)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// A password far larger than maxBasicAuthFieldLen must be rejected
	// without ever reaching the protected handler, regardless of its
	// content.
	huge := strings.Repeat("A", maxBasicAuthFieldLen+1)
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.250:12345"
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("gokrazy:"+huge)))

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("oversized password should yield 401, got %d", rr.Code)
	}

	// And the legitimate password of normal length must still work.
	req = httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.251:12345"
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("gokrazy:testpass")))
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("valid creds should yield 200, got %d", rr.Code)
	}
}

// TestBasicAuthLengthMismatchStillFails proves that callers presenting a
// password whose length differs from the configured password are still
// rejected (the length-safe compare must not accept length-mismatched
// inputs as equal).
func TestBasicAuthLengthMismatchStillFails(t *testing.T) {
	mw := BasicAuthMiddleware("testpass", nil)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for _, attempt := range []string{"", "t", "test", "testpas", "testpassX", "testpasstesttest"} {
		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "192.168.1.252:12345"
		req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("gokrazy:"+attempt)))
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %q should yield 401, got %d", attempt, rr.Code)
		}
	}
}
