package main

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/denysvitali/pictures-sync-s3/pkg/paniclog"
)

type testPasswordProvider string

func (p testPasswordProvider) CurrentPassword() string {
	return string(p)
}

func TestParseAllowedOrigins(t *testing.T) {
	t.Setenv("WEBUI_ALLOWED_ORIGINS", "https://Example.com, 192.168.10.124:8080,https://denysvitali.github.io/")

	got := configuredAllowedOrigins()
	want := []string{
		"192.168.10.124:8080",
		"denysvitali.github.io",
		"example.com",
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("configuredAllowedOrigins() = %#v, want %#v", got, want)
	}
}

func TestBuildHandlerAuthBoundary(t *testing.T) {
	appMux := http.NewServeMux()
	appMux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	handler := buildHandler(appMux, testPasswordProvider("secret"), nil, nil, nil)

	t.Run("infra endpoints bypass auth", func(t *testing.T) {
		for _, path := range []string{"/healthz", "/metrics"} {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			rr := httptest.NewRecorder()

			handler.ServeHTTP(rr, req)

			if rr.Code != http.StatusOK {
				t.Fatalf("%s status = %d, want %d", path, rr.Code, http.StatusOK)
			}
		}
	})

	t.Run("app endpoints require auth", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
		rr := httptest.NewRecorder()

		handler.ServeHTTP(rr, req)

		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d", rr.Code, http.StatusUnauthorized)
		}
	})

	t.Run("app endpoints accept configured password", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
		req.SetBasicAuth("gokrazy", "secret")
		rr := httptest.NewRecorder()

		handler.ServeHTTP(rr, req)

		if rr.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want %d", rr.Code, http.StatusNoContent)
		}
	})
}

func TestPanicPersistenceMiddlewareRecoversAndStoresRecord(t *testing.T) {
	path := filepath.Join(t.TempDir(), "panic.json")
	handler := panicPersistenceMiddleware(path)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("handler failed")
	}))

	req := httptest.NewRequest(http.MethodGet, "/panic", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusInternalServerError)
	}
	record, err := paniclog.Read(path)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if record == nil {
		t.Fatal("record = nil, want saved panic")
	}
	if record.Source != "webui-http" {
		t.Fatalf("Source = %q, want webui-http", record.Source)
	}
	if record.Message != "handler failed" {
		t.Fatalf("Message = %q, want handler failed", record.Message)
	}
}
