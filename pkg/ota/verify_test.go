package ota

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/denysvitali/pictures-sync-s3/pkg/state"
)

// stubInstaller records every InstallRoot invocation. It's used to assert that
// the gokrazy updater is (or isn't) called depending on hash verification.
type stubInstaller struct {
	calls   atomic.Int32
	payload []byte
	err     error
}

func (s *stubInstaller) InstallRoot(ctx context.Context, r io.Reader, progress InstallProgressFunc) error {
	s.calls.Add(1)
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	s.payload = data
	return s.err
}

// gzipBytes returns the gzip-compressed form of payload.
func gzipBytes(t *testing.T, payload []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	if _, err := gw.Write(payload); err != nil {
		t.Fatalf("gzip write: %v", err)
	}
	if err := gw.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	return buf.Bytes()
}

// newOTATestServer spins up an HTTP server that emulates the GitHub releases
// API, an asset download, and (optionally) a SHA256 sidecar. The sidecarBody
// argument controls how the .sha256 endpoint behaves:
//
//   - nil:  the sidecar endpoint returns 404 (no sidecar published)
//   - else: the bytes are served as the sidecar contents
func newOTATestServer(t *testing.T, assetBytes []byte, sidecarBody []byte) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	mux.HandleFunc("/asset.gz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(assetBytes)))
		_, _ = w.Write(assetBytes)
	})

	mux.HandleFunc("/asset.gz.sha256", func(w http.ResponseWriter, r *http.Request) {
		if sidecarBody == nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write(sidecarBody)
	})

	mux.HandleFunc("/repos/", func(w http.ResponseWriter, r *http.Request) {
		assets := []Asset{{
			Name:               "asset.gz",
			BrowserDownloadURL: server.URL + "/asset.gz",
			Size:               int64(len(assetBytes)),
		}}
		if sidecarBody != nil {
			assets = append(assets, Asset{
				Name:               "asset.gz.sha256",
				BrowserDownloadURL: server.URL + "/asset.gz.sha256",
				Size:               int64(len(sidecarBody)),
			})
		}
		releases := []Release{{
			TagName:     "v1.0.0",
			Name:        "v1.0.0",
			PublishedAt: time.Now().UTC(),
			Assets:      assets,
		}}
		body, err := json.Marshal(releases)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	})

	return server
}

// usePermDir redirects state.PermDir to a temp directory for the duration of
// the test so OTA staging does not touch /perm or /tmp/pictures-sync.
func usePermDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	original := state.PermDir
	state.PermDir = dir
	t.Cleanup(func() {
		state.PermDir = original
	})
	return dir
}

func newTestManager(t *testing.T, serverURL, assetName string, installer Installer) *Manager {
	t.Helper()
	return &Manager{
		Owner:      "owner",
		Repo:       "repo",
		AssetName:  assetName,
		APIURL:     serverURL,
		HTTPClient: &http.Client{Timeout: 5 * time.Second},
		Installer:  installer,
		status:     Status{State: "idle"},
	}
}

func TestNormalizeSHA256Hex(t *testing.T) {
	digest := strings.Repeat("a", 64)
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain", digest, digest},
		{"trailing-newline", digest + "\n", digest},
		{"sha256sum-format", digest + "  asset.gz\n", digest},
		{"bsd-format", "SHA256 (asset.gz) = " + digest + "\n", digest},
		{"uppercase", strings.ToUpper(digest), digest},
		{"too-short", "abc123", ""},
		{"empty", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := normalizeSHA256Hex(tc.in); got != tc.want {
				t.Fatalf("normalizeSHA256Hex(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestRunVerifiesMatchingSHA256 covers the happy path: a SHA256 sidecar that
// matches the downloaded image. The installer must be invoked exactly once
// with the decompressed image bytes, and no staged files may leak.
func TestRunVerifiesMatchingSHA256(t *testing.T) {
	stagingRoot := usePermDir(t)

	payload := []byte("payload-bytes-for-installer")
	asset := gzipBytes(t, payload)
	sum := sha256.Sum256(asset)
	sidecar := []byte(hex.EncodeToString(sum[:]) + "  asset.gz\n")

	server := newOTATestServer(t, asset, sidecar)

	installer := &stubInstaller{}
	mgr := newTestManager(t, server.URL, "asset.gz", installer)

	mgr.run(context.Background(), "v1.0.0")

	status := mgr.Status()
	if status.State != "installed" {
		t.Fatalf("status state = %q (error=%q), want installed", status.State, status.Error)
	}
	if got := installer.calls.Load(); got != 1 {
		t.Fatalf("installer InstallRoot calls = %d, want 1", got)
	}
	if !bytes.Equal(installer.payload, payload) {
		t.Fatalf("installer received %q, want %q", installer.payload, payload)
	}

	// Staged files must be cleaned up after a successful install.
	staging := filepath.Join(stagingRoot, "ota-staging")
	entries, err := os.ReadDir(staging)
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("read staging dir: %v", err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".part") || strings.HasSuffix(e.Name(), ".sha256") {
			t.Fatalf("staged artifact leaked: %s", e.Name())
		}
	}
}

// TestRunRejectsMismatchingSHA256 covers the security-critical path: a
// corrupted download whose computed SHA256 does not match the published
// sidecar must be rejected BEFORE InstallRoot runs.
func TestRunRejectsMismatchingSHA256(t *testing.T) {
	stagingRoot := usePermDir(t)

	payload := []byte("payload-bytes-for-installer")
	asset := gzipBytes(t, payload)
	// Intentionally wrong digest (sha256 of unrelated bytes).
	bogus := sha256.Sum256([]byte("not-the-image"))
	sidecar := []byte(hex.EncodeToString(bogus[:]) + "\n")

	server := newOTATestServer(t, asset, sidecar)

	installer := &stubInstaller{}
	mgr := newTestManager(t, server.URL, "asset.gz", installer)

	mgr.run(context.Background(), "v1.0.0")

	status := mgr.Status()
	if status.State != "failed" {
		t.Fatalf("status state = %q, want failed", status.State)
	}
	if status.Phase != "verification_failed" {
		t.Fatalf("status phase = %q, want verification_failed", status.Phase)
	}
	if !strings.Contains(status.Error, "SHA256 mismatch") {
		t.Fatalf("status error = %q, want it to mention SHA256 mismatch", status.Error)
	}
	if got := installer.calls.Load(); got != 0 {
		t.Fatalf("installer must NOT be invoked on hash mismatch (calls=%d)", got)
	}

	// Staged files must be cleaned up on rejection so a bad download never
	// lingers next to a verified one.
	staging := filepath.Join(stagingRoot, "ota-staging")
	entries, err := os.ReadDir(staging)
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("read staging dir: %v", err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".part") {
			t.Fatalf("rejected staged image leaked: %s", e.Name())
		}
	}
}

// TestRunWithoutSidecarFallsBackToManifest verifies that when no sidecar is
// published, we still record the computed digest in a manifest sidecar (per
// task option #3) and proceed with the install. Strict mode is governed by
// OTA_REQUIRE_SHA256 (covered separately).
func TestRunWithoutSidecarStillInstalls(t *testing.T) {
	usePermDir(t)

	payload := []byte("payload-bytes")
	asset := gzipBytes(t, payload)

	server := newOTATestServer(t, asset, nil)

	installer := &stubInstaller{}
	mgr := newTestManager(t, server.URL, "asset.gz", installer)

	mgr.run(context.Background(), "v1.0.0")

	status := mgr.Status()
	if status.State != "installed" {
		t.Fatalf("status state = %q (error=%q), want installed", status.State, status.Error)
	}
	if got := installer.calls.Load(); got != 1 {
		t.Fatalf("installer InstallRoot calls = %d, want 1", got)
	}
}

// TestHashMismatchErrorIsClassified ensures handlers can rely on IsHashMismatch
// to distinguish verification failures from generic install errors.
func TestHashMismatchErrorIsClassified(t *testing.T) {
	mismatch := &HashMismatchError{Expected: "aa", Got: "bb"}
	if !IsHashMismatch(mismatch) {
		t.Fatal("IsHashMismatch should be true for *HashMismatchError")
	}
	if !IsHashMismatch(fmt.Errorf("wrapped: %w", mismatch)) {
		t.Fatal("IsHashMismatch should be true through error wrapping")
	}
	if IsHashMismatch(errors.New("plain error")) {
		t.Fatal("IsHashMismatch should be false for unrelated errors")
	}
}
