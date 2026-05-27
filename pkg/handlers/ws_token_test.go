package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

type fixedPasswordProvider string

func (p fixedPasswordProvider) CurrentPassword() string { return string(p) }

func TestWSTokenHandler_RequiresBasicAuth(t *testing.T) {
	handler := WSTokenHandler(fixedPasswordProvider("correct-horse-battery"))

	t.Run("missing credentials", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/ws-token", nil)
		req.RemoteAddr = "10.0.0.1:1234"
		rec := httptest.NewRecorder()
		handler(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", rec.Code)
		}
		if got := rec.Header().Get("WWW-Authenticate"); got == "" {
			t.Fatalf("missing WWW-Authenticate header")
		}
	})

	t.Run("wrong password", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/ws-token", nil)
		req.RemoteAddr = "10.0.0.2:1234"
		req.SetBasicAuth("gokrazy", "wrong")
		rec := httptest.NewRecorder()
		handler(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", rec.Code)
		}
	})

	t.Run("wrong username", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/ws-token", nil)
		req.RemoteAddr = "10.0.0.3:1234"
		req.SetBasicAuth("admin", "correct-horse-battery")
		rec := httptest.NewRecorder()
		handler(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", rec.Code)
		}
	})

	t.Run("correct credentials succeed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/ws-token", nil)
		req.RemoteAddr = "10.0.0.4:1234"
		req.SetBasicAuth("gokrazy", "correct-horse-battery")
		rec := httptest.NewRecorder()
		handler(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d body = %q, want 200", rec.Code, rec.Body.String())
		}
		if body := rec.Body.String(); body == "" || body[0] != '{' {
			t.Fatalf("unexpected body: %q", body)
		}
	})

	t.Run("non-GET rejected", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/ws-token", nil)
		req.RemoteAddr = "10.0.0.5:1234"
		req.SetBasicAuth("gokrazy", "correct-horse-battery")
		rec := httptest.NewRecorder()
		handler(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("status = %d, want 405", rec.Code)
		}
	})
}
