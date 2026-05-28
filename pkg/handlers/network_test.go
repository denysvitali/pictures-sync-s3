package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/denysvitali/pictures-sync-s3/pkg/ssrf"
)

// netTestNewContext returns a handlers.Context with a freshly created
// (real) SSRF validator. Using a real validator is safe because we only
// exercise it with hostnames that fail format/dangerous-name checks
// before any DNS lookup is performed.
func netTestNewContext() *Context {
	return &Context{
		SSRFValidator: ssrf.NewValidator(1000, time.Minute),
	}
}

// netTestNewContextNoSSRF returns a context with a nil SSRFValidator,
// exercising the "service unavailable" branch.
func netTestNewContextNoSSRF() *Context {
	return &Context{}
}

// netTestDoRequest issues an HTTP request against an http.HandlerFunc
// and returns the response recorder.
func netTestDoRequest(handler http.HandlerFunc, method, body string) *httptest.ResponseRecorder {
	var reqBody *bytes.Buffer
	if body != "" {
		reqBody = bytes.NewBufferString(body)
	} else {
		reqBody = bytes.NewBuffer(nil)
	}
	req := httptest.NewRequest(method, "/", reqBody)
	req.RemoteAddr = "192.0.2.10:12345"
	rr := httptest.NewRecorder()
	handler(rr, req)
	return rr
}

// --- HandleNetworkDNS -------------------------------------------------------

func TestHandleNetworkDNS_GetReturnsJSON(t *testing.T) {
	ctx := netTestNewContext()
	rr := netTestDoRequest(ctx.HandleNetworkDNS, http.MethodGet, "")

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", rr.Code, rr.Body.String())
	}
	if ct := rr.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("expected JSON Content-Type, got %q", ct)
	}
	var payload map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to decode body: %v", err)
	}
	if _, ok := payload["resolv_conf"]; !ok {
		t.Errorf("response missing resolv_conf key: %v", payload)
	}
}

func TestHandleNetworkDNS_MethodNotAllowed(t *testing.T) {
	ctx := netTestNewContext()
	rr := netTestDoRequest(ctx.HandleNetworkDNS, http.MethodPost, "")
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rr.Code)
	}
}

// --- HandleNetworkInterfaces ------------------------------------------------

func TestHandleNetworkInterfaces_GetReturnsJSON(t *testing.T) {
	ctx := netTestNewContext()
	rr := netTestDoRequest(ctx.HandleNetworkInterfaces, http.MethodGet, "")

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", rr.Code, rr.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to decode body: %v", err)
	}
	if _, ok := payload["interfaces"]; !ok {
		t.Errorf("response missing interfaces key: %v", payload)
	}
}

func TestHandleNetworkInterfaces_MethodNotAllowed(t *testing.T) {
	ctx := netTestNewContext()
	rr := netTestDoRequest(ctx.HandleNetworkInterfaces, http.MethodDelete, "")
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rr.Code)
	}
}

// --- HandleDNSLookup --------------------------------------------------------

func TestHandleDNSLookup_MethodNotAllowed(t *testing.T) {
	ctx := netTestNewContext()
	rr := netTestDoRequest(ctx.HandleDNSLookup, http.MethodGet, "")
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rr.Code)
	}
}

func TestHandleDNSLookup_InvalidJSON(t *testing.T) {
	ctx := netTestNewContext()
	rr := netTestDoRequest(ctx.HandleDNSLookup, http.MethodPost, "{not json")
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid JSON, got %d", rr.Code)
	}
}

func TestHandleDNSLookup_EmptyHostname(t *testing.T) {
	ctx := netTestNewContext()
	rr := netTestDoRequest(ctx.HandleDNSLookup, http.MethodPost, `{"hostname":""}`)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty hostname, got %d", rr.Code)
	}
}

func TestHandleDNSLookup_NoSSRFValidator(t *testing.T) {
	ctx := netTestNewContextNoSSRF()
	rr := netTestDoRequest(ctx.HandleDNSLookup, http.MethodPost, `{"hostname":"example.com"}`)
	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 when SSRFValidator nil, got %d", rr.Code)
	}
}

func TestHandleDNSLookup_BlockedByValidator(t *testing.T) {
	ctx := netTestNewContext()
	// "localhost" is in the dangerous hostnames list -> blocked
	rr := netTestDoRequest(ctx.HandleDNSLookup, http.MethodPost, `{"hostname":"localhost"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 with JSON error body, got %d", rr.Code)
	}
	var payload map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to decode body: %v", err)
	}
	if _, ok := payload["error"]; !ok {
		t.Errorf("expected error field for blocked hostname, got %v", payload)
	}
}

// --- HandlePing -------------------------------------------------------------

func TestHandlePing_MethodNotAllowed(t *testing.T) {
	ctx := netTestNewContext()
	rr := netTestDoRequest(ctx.HandlePing, http.MethodGet, "")
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rr.Code)
	}
}

func TestHandlePing_InvalidJSON(t *testing.T) {
	ctx := netTestNewContext()
	rr := netTestDoRequest(ctx.HandlePing, http.MethodPost, "{nope")
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid JSON, got %d", rr.Code)
	}
}

func TestHandlePing_EmptyHostname(t *testing.T) {
	ctx := netTestNewContext()
	rr := netTestDoRequest(ctx.HandlePing, http.MethodPost, `{"hostname":""}`)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty hostname, got %d", rr.Code)
	}
}

func TestHandlePing_NoSSRFValidator(t *testing.T) {
	ctx := netTestNewContextNoSSRF()
	rr := netTestDoRequest(ctx.HandlePing, http.MethodPost, `{"hostname":"example.com","count":2}`)
	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 when SSRFValidator nil, got %d", rr.Code)
	}
}

func TestHandlePing_BlockedByValidator(t *testing.T) {
	ctx := netTestNewContext()
	// Negative count should be normalized to default 4 before the SSRF check
	rr := netTestDoRequest(ctx.HandlePing, http.MethodPost, `{"hostname":"localhost","count":-3}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 with JSON error body, got %d", rr.Code)
	}
	var payload map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to decode body: %v", err)
	}
	if _, ok := payload["error"]; !ok {
		t.Errorf("expected error field for blocked hostname, got %v", payload)
	}
}

func TestHandlePing_CountClamping(t *testing.T) {
	// Excessive count must be clamped to 10 -- we don't run a real ping,
	// so we trigger a block via the dangerous-hostnames path.
	ctx := netTestNewContext()
	rr := netTestDoRequest(ctx.HandlePing, http.MethodPost, `{"hostname":"localhost","count":99}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

// --- HandleNetworkDiagnostics ----------------------------------------------

func TestHandleNetworkDiagnostics_MethodNotAllowed(t *testing.T) {
	ctx := netTestNewContext()
	rr := netTestDoRequest(ctx.HandleNetworkDiagnostics, http.MethodGet, "")
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rr.Code)
	}
}

func TestHandleNetworkDiagnostics_NoSSRFValidator(t *testing.T) {
	// With no SSRF validator, the handler still responds 200 but marks
	// internet checks as false. ICMP pings to public IPs will fail
	// (no privileges in the test env), but the handler must not crash.
	ctx := netTestNewContextNoSSRF()
	rr := netTestDoRequest(ctx.HandleNetworkDiagnostics, http.MethodPost, "")
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", rr.Code, rr.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to decode body: %v", err)
	}
	for _, k := range []string{"dns_google", "dns_cloudflare", "internet_google", "internet_cloudflare", "routes"} {
		if _, ok := payload[k]; !ok {
			t.Errorf("expected key %q in diagnostics result", k)
		}
	}
	if v, ok := payload["internet_google"].(bool); !ok || v {
		t.Errorf("expected internet_google=false when SSRFValidator nil, got %v", payload["internet_google"])
	}
}
