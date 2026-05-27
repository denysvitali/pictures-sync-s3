package httputil

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

// TestDecodeJSONWithLimit covers the custom-size-limit variant.
func TestDecodeJSONWithLimit(t *testing.T) {
	type payload struct {
		Name string `json:"name"`
	}

	tests := []struct {
		name      string
		body      string
		maxBytes  int64
		expectErr bool
	}{
		{"valid small body within limit", `{"name":"ok"}`, 1024, false},
		{"body exceeds tiny limit", `{"name":"this-string-is-way-too-long-for-the-tiny-limit"}`, 8, true},
		{"unknown field rejected", `{"name":"x","extra":1}`, 1024, true},
		{"malformed json rejected", `{"name":`, 1024, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tt.body))
			var p payload
			err := DecodeJSONWithLimit(req, &p, tt.maxBytes)
			if tt.expectErr && err == nil {
				t.Fatalf("expected error, got nil (decoded=%+v)", p)
			}
			if !tt.expectErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

// TestDecodeJSONOversize ensures the default 10MB limit actually clamps oversize bodies.
func TestDecodeJSONOversize(t *testing.T) {
	type payload struct {
		Blob string `json:"blob"`
	}

	// Build a >10MB JSON body.
	var buf bytes.Buffer
	buf.WriteString(`{"blob":"`)
	chunk := strings.Repeat("a", 1024)
	for i := 0; i < 11*1024; i++ { // ~11MB of 'a' inside the string
		buf.WriteString(chunk)
	}
	buf.WriteString(`"}`)

	req := httptest.NewRequest(http.MethodPost, "/", &buf)
	var p payload
	if err := DecodeJSON(req, &p); err == nil {
		t.Fatal("expected error for body exceeding 10MB default limit, got nil")
	}
}

// TestValidatePathNonExistentRoot exercises the EvalSymlinks-fallback branch
// when the supplied allowedRoot does not exist on disk.
func TestValidatePathNonExistentRoot(t *testing.T) {
	tmp := t.TempDir()
	ghostRoot := filepath.Join(tmp, "does", "not", "exist")

	// Contained relative path must resolve under the ghost root.
	got, err := ValidatePath(ghostRoot, "sub/file.txt")
	if err != nil {
		t.Fatalf("unexpected error for non-existent root: %v", err)
	}
	want := filepath.Join(ghostRoot, "sub", "file.txt")
	if got != want {
		t.Fatalf("resolved=%q want=%q", got, want)
	}

	// Traversal must still be rejected even when root does not exist.
	if _, err := ValidatePath(ghostRoot, "../escape"); !errors.Is(err, ErrPathEscapesRoot) {
		t.Fatalf("expected ErrPathEscapesRoot, got %v", err)
	}
}
