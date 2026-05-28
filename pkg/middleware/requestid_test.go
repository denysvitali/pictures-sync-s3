package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func applyRequestID(req *http.Request) (id string, respID string) {
	rec := httptest.NewRecorder()
	var ctxID string
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctxID = RequestIDFromContext(r.Context())
	})
	RequestID(inner).ServeHTTP(rec, req)
	return ctxID, rec.Header().Get("X-Request-ID")
}

func TestGeneratesIDWhenAbsent(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	id, _ := applyRequestID(req)
	if id == "" {
		t.Fatal("expected non-empty generated ID")
	}
	// 16 random bytes → 32 hex chars
	if len(id) != 32 {
		t.Fatalf("expected 32-char hex ID, got len=%d: %q", len(id), id)
	}
}

func TestPreservesProvidedID(t *testing.T) {
	const provided = "my-custom-request-id"
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Request-ID", provided)
	id, _ := applyRequestID(req)
	if id != provided {
		t.Fatalf("want %q, got %q", provided, id)
	}
}

func TestResponseHeaderEchosID(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	id, respID := applyRequestID(req)
	if respID == "" {
		t.Fatal("response X-Request-ID header is empty")
	}
	if id != respID {
		t.Fatalf("context ID %q != response header ID %q", id, respID)
	}
}

func TestContextIDMatchesResponseHeader(t *testing.T) {
	const provided = "abc-123"
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Request-ID", provided)
	id, respID := applyRequestID(req)
	if id != provided || respID != provided {
		t.Fatalf("mismatch: ctx=%q resp=%q want=%q", id, respID, provided)
	}
}

func TestTwoRequestsGetDifferentIDs(t *testing.T) {
	req1 := httptest.NewRequest("GET", "/", nil)
	req2 := httptest.NewRequest("GET", "/", nil)
	id1, _ := applyRequestID(req1)
	id2, _ := applyRequestID(req2)
	if id1 == id2 {
		t.Fatalf("expected distinct IDs, both got %q", id1)
	}
}
