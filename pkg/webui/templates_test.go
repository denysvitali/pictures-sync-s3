package webui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandleSPAServesEmbeddedIndex(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	HandleSPA(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("HandleSPA status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if contentType := rec.Header().Get("Content-Type"); !strings.Contains(contentType, "text/html") {
		t.Fatalf("Content-Type = %q, want text/html", contentType)
	}
	if body := rec.Body.String(); strings.Contains(body, "SPA index not found") || !strings.Contains(strings.ToLower(body), "<!doctype html>") {
		t.Fatalf("HandleSPA body does not look like embedded index.html: %q", body)
	}
}
