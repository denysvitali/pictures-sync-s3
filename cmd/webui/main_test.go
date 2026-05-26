package main

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/denysvitali/pictures-sync-s3/pkg/paniclog"
)

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
