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

// TestStageReaderFsyncsStagingDir asserts that after stageReader succeeds the
// staged file and its manifest are present on disk. The fsync of the parent
// directory is what makes that observation durable across a crash; the test
// verifies the success path still returns the staged image and that bytes /
// digest are correct, ensuring the new fsync does not regress functionality.
func TestStageReaderFsyncsStagingDir(t *testing.T) {
	dir := t.TempDir()
	payload := []byte("hello-ota")
	staged, err := stageReader(dir, "img", bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("stageReader: %v", err)
	}
	defer staged.Close()

	if staged.Bytes != int64(len(payload)) {
		t.Fatalf("staged bytes = %d, want %d", staged.Bytes, len(payload))
	}

	sum := sha256.Sum256(payload)
	if staged.SHA256Hex != hex.EncodeToString(sum[:]) {
		t.Fatalf("staged digest = %s, want %s", staged.SHA256Hex, hex.EncodeToString(sum[:]))
	}

	if _, err := os.Stat(staged.Path); err != nil {
		t.Fatalf("staged path missing: %v", err)
	}
	if _, err := os.Stat(staged.ManifestPath); err != nil {
		t.Fatalf("staged manifest missing: %v", err)
	}
}

// TestSyncDirIsBestEffortOnUnsupportedFS exercises the syncDir code path for a
// non-existent directory (which must return an error) and a regular directory
// (which must succeed). Regression guard against the helper accidentally
// hiding real filesystem errors.
func TestSyncDirReportsRealErrors(t *testing.T) {
	if err := syncDir(filepath.Join(t.TempDir(), "does-not-exist")); err == nil {
		t.Fatal("syncDir on missing directory should return an error")
	}
	if err := syncDir(t.TempDir()); err != nil {
		t.Fatalf("syncDir on real directory: %v", err)
	}
}

// TestRunRejectsTruncatedDownload covers the security-critical path where the
// HTTP body ends short of the size advertised by the GitHub release manifest.
// Without sidecar-based verification this used to be silently accepted and
// flashed; the size check must reject it before InstallRoot runs.
func TestRunRejectsTruncatedDownload(t *testing.T) {
	stagingRoot := usePermDir(t)

	payload := []byte("payload-bytes-for-installer")
	asset := gzipBytes(t, payload)

	// Serve the asset normally but advertise a larger size in the GitHub API
	// response. The downloaded length will not match the manifest size.
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	mux.HandleFunc("/asset.gz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		// Deliberately omit Content-Length so the size check relies on the
		// release-manifest size advertised below.
		_, _ = w.Write(asset)
	})
	mux.HandleFunc("/asset.gz.sha256", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})
	mux.HandleFunc("/repos/", func(w http.ResponseWriter, r *http.Request) {
		releases := []Release{{
			TagName:     "v1.0.0",
			PublishedAt: time.Now().UTC(),
			Assets: []Asset{{
				Name:               "asset.gz",
				BrowserDownloadURL: server.URL + "/asset.gz",
				// Advertise a size one byte larger than what the server will
				// actually deliver. This simulates a truncated CDN response.
				Size: int64(len(asset)) + 1,
			}},
		}}
		body, _ := json.Marshal(releases)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	})

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
	if !strings.Contains(status.Error, "size mismatch") {
		t.Fatalf("status error = %q, want it to mention size mismatch", status.Error)
	}
	if got := installer.calls.Load(); got != 0 {
		t.Fatalf("installer must NOT be invoked on size mismatch (calls=%d)", got)
	}

	// Truncated staged image must be cleaned up.
	staging := filepath.Join(stagingRoot, "ota-staging")
	entries, err := os.ReadDir(staging)
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("read staging dir: %v", err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".part") {
			t.Fatalf("truncated staged image leaked: %s", e.Name())
		}
	}
}

// TestSizeMismatchErrorIsClassified ensures handlers can rely on IsSizeMismatch
// to distinguish size verification failures from generic install errors.
func TestSizeMismatchErrorIsClassified(t *testing.T) {
	mismatch := &SizeMismatchError{Expected: 100, Got: 50}
	if !IsSizeMismatch(mismatch) {
		t.Fatal("IsSizeMismatch should be true for *SizeMismatchError")
	}
	if !IsSizeMismatch(fmt.Errorf("wrapped: %w", mismatch)) {
		t.Fatal("IsSizeMismatch should be true through error wrapping")
	}
	if IsSizeMismatch(errors.New("plain error")) {
		t.Fatal("IsSizeMismatch should be false for unrelated errors")
	}
	if IsSizeMismatch(&HashMismatchError{}) {
		t.Fatal("IsSizeMismatch should not match HashMismatchError")
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
