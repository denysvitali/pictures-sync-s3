package ota

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/denysvitali/pictures-sync-s3/pkg/state"
)

// SHA256SidecarSuffix is the conventional suffix appended to an asset name to
// locate its published SHA256 digest on a GitHub release.
const SHA256SidecarSuffix = ".sha256"

// stagedManifestSuffix is the on-disk sidecar holding the computed digest of a
// staged image so verification status survives a crash between download and
// activation.
const stagedManifestSuffix = ".sha256"

// HashMismatchError signals that the SHA256 of the streamed image did not
// match the expected digest. It is distinct from gokrazy installer failures so
// callers can return a precise 4xx response.
type HashMismatchError struct {
	Expected string
	Got      string
	Source   string // where the expected hash came from (e.g. "github sidecar")
}

func (e *HashMismatchError) Error() string {
	if e.Source != "" {
		return fmt.Sprintf("OTA image SHA256 mismatch (source=%s): expected %s, got %s", e.Source, e.Expected, e.Got)
	}
	return fmt.Sprintf("OTA image SHA256 mismatch: expected %s, got %s", e.Expected, e.Got)
}

// IsHashMismatch reports whether err is (or wraps) a HashMismatchError.
func IsHashMismatch(err error) bool {
	var mismatch *HashMismatchError
	return errors.As(err, &mismatch)
}

// SizeMismatchError signals that the number of bytes received does not match
// the size advertised by the release manifest (or HTTP Content-Length). It is
// raised even when no SHA256 sidecar is published, so a truncated download
// never reaches the gokrazy updater.
type SizeMismatchError struct {
	Expected int64
	Got      int64
	Source   string // e.g. "github asset size" or "Content-Length"
}

func (e *SizeMismatchError) Error() string {
	if e.Source != "" {
		return fmt.Sprintf("OTA image size mismatch (source=%s): expected %d bytes, got %d", e.Source, e.Expected, e.Got)
	}
	return fmt.Sprintf("OTA image size mismatch: expected %d bytes, got %d", e.Expected, e.Got)
}

// IsSizeMismatch reports whether err is (or wraps) a SizeMismatchError.
func IsSizeMismatch(err error) bool {
	var mismatch *SizeMismatchError
	return errors.As(err, &mismatch)
}

// VerifyExpectedSize compares the number of bytes staged against the expected
// size. A non-positive expected value is treated as "unknown" and accepted.
func (s *StagedImage) VerifyExpectedSize(expected int64, source string) error {
	if expected <= 0 {
		return nil
	}
	if s.Bytes != expected {
		return &SizeMismatchError{Expected: expected, Got: s.Bytes, Source: source}
	}
	return nil
}

// StagedImage is the result of downloading and verifying an OTA image before
// applying it. Callers must call Close to remove the staged file and its
// manifest once they are done (success or failure).
type StagedImage struct {
	Path         string
	ManifestPath string
	SHA256Hex    string
	Bytes        int64
}

// Open returns a reader for the staged image. The caller is responsible for
// closing it.
func (s *StagedImage) Open() (*os.File, error) {
	return os.Open(s.Path)
}

// Close removes the staged image and its manifest sidecar. Safe to call
// multiple times.
func (s *StagedImage) Close() error {
	var firstErr error
	if s.Path != "" {
		if err := os.Remove(s.Path); err != nil && !os.IsNotExist(err) {
			firstErr = err
		}
	}
	if s.ManifestPath != "" {
		if err := os.Remove(s.ManifestPath); err != nil && !os.IsNotExist(err) && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// stageReader copies r to a freshly-created file in dir while computing its
// SHA256. The file name is derived from baseName (suffixed with .part to
// indicate it is unverified). On success it returns the populated StagedImage;
// on error any partial file is removed.
func stageReader(dir, baseName string, r io.Reader) (*StagedImage, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create OTA staging dir: %w", err)
	}
	if baseName == "" {
		baseName = "ota-image"
	}
	stagedPath := filepath.Join(dir, baseName+".part")

	f, err := os.OpenFile(stagedPath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, fmt.Errorf("create OTA staging file: %w", err)
	}
	hasher := sha256.New()
	tee := io.TeeReader(r, hasher)
	written, copyErr := io.Copy(f, tee)
	if syncErr := f.Sync(); syncErr != nil && copyErr == nil {
		copyErr = syncErr
	}
	if closeErr := f.Close(); closeErr != nil && copyErr == nil {
		copyErr = closeErr
	}
	if copyErr != nil {
		_ = os.Remove(stagedPath)
		return nil, fmt.Errorf("stream OTA image to staging: %w", copyErr)
	}

	sum := hex.EncodeToString(hasher.Sum(nil))
	manifestPath := stagedPath + stagedManifestSuffix
	if err := os.WriteFile(manifestPath, []byte(sum+"  "+baseName+"\n"), 0o600); err != nil {
		_ = os.Remove(stagedPath)
		return nil, fmt.Errorf("write OTA staging manifest: %w", err)
	}

	return &StagedImage{
		Path:         stagedPath,
		ManifestPath: manifestPath,
		SHA256Hex:    sum,
		Bytes:        written,
	}, nil
}

// VerifyExpected compares the computed digest against the provided expected
// hex digest. Both inputs are normalised to lowercase. Returns a
// *HashMismatchError on mismatch.
func (s *StagedImage) VerifyExpected(expectedHex, source string) error {
	expected := normalizeSHA256Hex(expectedHex)
	got := normalizeSHA256Hex(s.SHA256Hex)
	if expected == "" {
		return errors.New("expected SHA256 digest is empty")
	}
	if expected != got {
		return &HashMismatchError{Expected: expected, Got: got, Source: source}
	}
	return nil
}

// normalizeSHA256Hex extracts the 64-hex-character digest from a string,
// trimming whitespace and any trailing filename present in `sha256sum`-style
// output ("<hex>  <filename>").
func normalizeSHA256Hex(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	// Common GitHub sidecar formats:
	//   "<hex>\n"
	//   "<hex>  <filename>\n"
	//   "SHA256 (<filename>) = <hex>\n" (BSD-style)
	if strings.HasPrefix(raw, "SHA256") {
		if idx := strings.LastIndex(raw, "="); idx >= 0 {
			raw = strings.TrimSpace(raw[idx+1:])
		}
	}
	if fields := strings.Fields(raw); len(fields) > 0 {
		raw = fields[0]
	}
	if len(raw) != 64 {
		return ""
	}
	return strings.ToLower(raw)
}

// downloadSHA256Sidecar fetches and parses a SHA256 sidecar asset from the
// given URL. The returned string is the lowercase hex digest, or an empty
// string with a nil error if the sidecar does not exist (HTTP 404). All other
// non-2xx responses are returned as errors so they are surfaced to the
// operator rather than silently downgrading verification.
func downloadSHA256Sidecar(ctx context.Context, client *http.Client, sidecarURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sidecarURL, nil)
	if err != nil {
		return "", err
	}
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch SHA256 sidecar: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return "", nil
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("fetch SHA256 sidecar: %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8*1024))
	if err != nil {
		return "", fmt.Errorf("read SHA256 sidecar: %w", err)
	}
	digest := normalizeSHA256Hex(string(body))
	if digest == "" {
		return "", fmt.Errorf("SHA256 sidecar at %s did not contain a 64-character hex digest", sidecarURL)
	}
	return digest, nil
}

// otaStagingDir returns the directory used to stage OTA downloads before
// verification. It uses /perm when available (production) and falls back to
// the OS temp dir for development and tests.
func otaStagingDir() string {
	if perm := strings.TrimSpace(state.PermDir); perm != "" {
		return filepath.Join(perm, "ota-staging")
	}
	return filepath.Join(os.TempDir(), "pictures-sync-ota-staging")
}
