package ota

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gokrazy/updater"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestLatestReleaseSortsByPublishedAt(t *testing.T) {
	now := time.Now().UTC()
	releases := []Release{
		{
			TagName:     "older",
			PublishedAt: now.Add(-time.Hour),
			Assets: []Asset{{
				Name:               DefaultAssetName,
				BrowserDownloadURL: "https://example.invalid/older.squashfs.gz",
			}},
		},
		{
			TagName:     "newer",
			PublishedAt: now,
			Assets: []Asset{{
				Name:               DefaultAssetName,
				BrowserDownloadURL: "https://example.invalid/newer.squashfs.gz",
			}},
		},
	}

	body, err := json.Marshal(releases)
	if err != nil {
		t.Fatalf("marshal releases: %v", err)
	}

	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/repos/owner/repo/releases" {
			t.Fatalf("unexpected path: %s", req.URL.Path)
		}
		return jsonResponse(body), nil
	})}

	manager := &Manager{
		Owner:      "owner",
		Repo:       "repo",
		APIURL:     "https://api.example.invalid",
		AssetName:  DefaultAssetName,
		HTTPClient: client,
	}

	release, asset, err := manager.LatestRelease(context.Background())
	if err != nil {
		t.Fatalf("LatestRelease returned error: %v", err)
	}
	if release.TagName != "newer" {
		t.Fatalf("release tag = %q, want newer", release.TagName)
	}
	if asset.BrowserDownloadURL != "https://example.invalid/newer.squashfs.gz" {
		t.Fatalf("asset URL = %q", asset.BrowserDownloadURL)
	}
}

func TestLatestReleaseRejectsFlashOnlyAsset(t *testing.T) {
	releases := []Release{{
		TagName:     "v1",
		PublishedAt: time.Now().UTC(),
		Assets: []Asset{{
			Name:               FlashAssetName,
			BrowserDownloadURL: "https://example.invalid/photo-backup-rpi4b.img.gz",
		}},
	}}

	body, err := json.Marshal(releases)
	if err != nil {
		t.Fatalf("marshal releases: %v", err)
	}
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return jsonResponse(body), nil
	})}

	manager := &Manager{
		Owner:      "owner",
		Repo:       "repo",
		APIURL:     "https://api.example.invalid",
		AssetName:  DefaultAssetName,
		HTTPClient: client,
	}

	_, _, err = manager.LatestRelease(context.Background())
	if err == nil {
		t.Fatal("LatestRelease succeeded for flash-only release")
	}
}

func TestSelectReleaseFetchesSpecificTagDirectly(t *testing.T) {
	release := Release{
		TagName:     "v2026.5.25.1222",
		PublishedAt: time.Now().UTC(),
		Assets: []Asset{
			{
				Name:               DefaultAssetName,
				BrowserDownloadURL: "https://example.invalid/photo-backup-rpi4b-root.squashfs.gz",
				Size:               1234,
			},
			{
				Name:               DefaultAssetName + SHA256SidecarSuffix,
				BrowserDownloadURL: "https://example.invalid/photo-backup-rpi4b-root.squashfs.gz.sha256",
				Size:               64,
			},
		},
	}

	body, err := json.Marshal(release)
	if err != nil {
		t.Fatalf("marshal release: %v", err)
	}

	var requestedPath string
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requestedPath = req.URL.Path
		if req.URL.Path != "/repos/owner/repo/releases/tags/v2026.5.25.1222" {
			t.Fatalf("unexpected path: %s", req.URL.Path)
		}
		return jsonResponse(body), nil
	})}

	manager := &Manager{
		Owner:      "owner",
		Repo:       "repo",
		APIURL:     "https://api.example.invalid",
		AssetName:  DefaultAssetName,
		HTTPClient: client,
	}

	gotRelease, asset, err := manager.SelectRelease(context.Background(), "v2026.5.25.1222")
	if err != nil {
		t.Fatalf("SelectRelease returned error: %v", err)
	}
	if requestedPath == "" {
		t.Fatal("expected SelectRelease to fetch the release by tag")
	}
	if gotRelease.TagName != release.TagName {
		t.Fatalf("release tag = %q, want %q", gotRelease.TagName, release.TagName)
	}
	if asset.Name != DefaultAssetName {
		t.Fatalf("asset name = %q, want %q", asset.Name, DefaultAssetName)
	}
	if len(gotRelease.Assets) != 2 {
		t.Fatalf("release asset count = %d, want 2", len(gotRelease.Assets))
	}
	if gotRelease.Assets[1].Name != DefaultAssetName+SHA256SidecarSuffix {
		t.Fatalf("sidecar asset name = %q, want %q", gotRelease.Assets[1].Name, DefaultAssetName+SHA256SidecarSuffix)
	}
}

func TestSelectReleaseRejectsSpecificTagWithoutRootAsset(t *testing.T) {
	release := Release{
		TagName:     "v2026.5.25.1222",
		PublishedAt: time.Now().UTC(),
		Assets: []Asset{{
			Name:               FlashAssetName,
			BrowserDownloadURL: "https://example.invalid/photo-backup-rpi4b.img.gz",
		}},
	}

	body, err := json.Marshal(release)
	if err != nil {
		t.Fatalf("marshal release: %v", err)
	}

	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/repos/owner/repo/releases/tags/v2026.5.25.1222" {
			t.Fatalf("unexpected path: %s", req.URL.Path)
		}
		return jsonResponse(body), nil
	})}

	manager := &Manager{
		Owner:      "owner",
		Repo:       "repo",
		APIURL:     "https://api.example.invalid",
		AssetName:  DefaultAssetName,
		HTTPClient: client,
	}

	_, _, err = manager.SelectRelease(context.Background(), "v2026.5.25.1222")
	if err == nil {
		t.Fatal("SelectRelease succeeded for a release without the root OTA asset")
	}
}

func TestSubscribeReceivesStatusUpdates(t *testing.T) {
	manager := &Manager{status: Status{State: "idle"}}
	updates := manager.Subscribe()
	defer manager.Unsubscribe(updates)

	manager.set(Status{
		State:           "installing",
		Phase:           "flashing",
		Message:         "Downloading and flashing OTA image",
		ProgressPercent: 42,
	})

	select {
	case status := <-updates:
		if status.State != "installing" {
			t.Fatalf("status state = %q, want installing", status.State)
		}
		if status.Phase != "flashing" {
			t.Fatalf("status phase = %q, want flashing", status.Phase)
		}
		if status.ProgressPercent != 42 {
			t.Fatalf("progress = %.1f, want 42", status.ProgressPercent)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for OTA status update")
	}
}

// TestConcurrentProgressUpdatesAreAtomic exercises the read-modify-write
// progress callbacks from many goroutines at once. The download-progress and
// install-progress callbacks share a Status struct: download writes
// DownloadedBytes monotonically while install reads the whole Status,
// updates ProgressPercent, and writes it back. With separate Status() + set()
// calls the install callback's stale-snapshot writeback clobbered
// DownloadedBytes updates that occurred between its read and its set,
// regressing the value stored in Manager.status.
func TestConcurrentProgressUpdatesAreAtomic(t *testing.T) {
	manager := &Manager{status: Status{State: "idle"}}

	const writers = 16
	const iterations = 500

	// Sample the manager's stored status while writers race. The downloader
	// writes a strictly increasing counter; if a concurrent install-progress
	// callback's RMW is not atomic, its writeback observes a stale snapshot
	// and clobbers DownloadedBytes with the older value.
	var (
		regressions atomic.Int64
		sampleStop  atomic.Bool
		sampleDone  = make(chan struct{})
	)
	go func() {
		defer close(sampleDone)
		var last int64
		for !sampleStop.Load() {
			cur := manager.Status().DownloadedBytes
			if cur < last {
				regressions.Add(1)
			} else {
				last = cur
			}
		}
		// Final check after writers settled.
		cur := manager.Status().DownloadedBytes
		if cur < last {
			regressions.Add(1)
		}
	}()

	// One dedicated downloader writes a strictly increasing counter.
	doneDL := make(chan struct{})
	go func() {
		defer close(doneDL)
		for i := 1; i <= iterations; i++ {
			manager.updateDownloadProgress(downloadProgress{
				downloaded: int64(i),
				total:      int64(iterations),
				speedBps:   float64(i),
			})
		}
	}()

	// Many install-progress writers race with the downloader.
	doneIN := make(chan struct{}, writers)
	for w := 0; w < writers; w++ {
		go func(id int) {
			defer func() { doneIN <- struct{}{} }()
			for i := 0; i < iterations; i++ {
				manager.updateInstallProgress(InstallProgress{
					Phase:           "flashing",
					Message:         "test",
					ProgressPercent: float64(id),
				})
			}
		}(w)
	}

	<-doneDL
	for w := 0; w < writers; w++ {
		<-doneIN
	}
	sampleStop.Store(true)
	<-sampleDone

	if r := regressions.Load(); r != 0 {
		t.Fatalf("DownloadedBytes regressed %d times in Manager.status; install-progress RMW clobbered downloader writes", r)
	}
	if got := manager.Status().DownloadedBytes; got != int64(iterations) {
		t.Fatalf("final DownloadedBytes = %d, want %d (writes were lost)", got, iterations)
	}
}

func TestUnsubscribeStopsStatusUpdates(t *testing.T) {
	manager := &Manager{status: Status{State: "idle"}}
	updates := manager.Subscribe()
	manager.Unsubscribe(updates)

	manager.set(Status{State: "installing"})

	select {
	case _, ok := <-updates:
		if ok {
			t.Fatal("unsubscribed channel received a status update")
		}
	case <-time.After(time.Second):
		t.Fatal("unsubscribed channel was not closed")
	}
}

func TestShouldSkipUpdateTLSVerify(t *testing.T) {
	tests := []struct {
		name   string
		rawURL string
		want   bool
	}{
		{name: "https localhost", rawURL: "https://localhost/update/", want: true},
		{name: "https localhost with dot", rawURL: "https://localhost./update/", want: true},
		{name: "https ipv4 loopback", rawURL: "https://127.0.0.1/update/", want: true},
		{name: "https ipv6 loopback", rawURL: "https://[::1]/update/", want: true},
		{name: "https loopback with auth and port", rawURL: "https://gokrazy:pass@127.0.0.1:443/update/", want: true},
		{name: "http loopback may redirect to self-signed https", rawURL: "http://127.0.0.1/update/", want: true},
		{name: "https remote ip", rawURL: "https://192.168.1.50/update/", want: false},
		{name: "https remote host", rawURL: "https://photo-backup.local/update/", want: false},
		{name: "invalid", rawURL: "://", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldSkipUpdateTLSVerify(tt.rawURL); got != tt.want {
				t.Fatalf("shouldSkipUpdateTLSVerify(%q) = %v, want %v", tt.rawURL, got, tt.want)
			}
		})
	}
}

func TestNewUpdateHTTPClientSkipsTLSVerifyForHTTPSLoopback(t *testing.T) {
	client := NewUpdateHTTPClient("https://127.0.0.1/update/", time.Minute, false)
	if !transportSkipsTLSVerify(client.Transport) {
		t.Fatal("loopback HTTPS client should skip TLS verification")
	}

	client = NewUpdateHTTPClient("https://photo-backup.local/update/", time.Minute, false)
	if transportSkipsTLSVerify(client.Transport) {
		t.Fatal("remote HTTPS client should not skip TLS verification")
	}
}

func TestGokrazyUpdaterTargetSkipsTLSVerifyForLoopbackHTTPS(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/update/features" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"features":"streaming"}`))
	}))
	defer server.Close()

	client := NewUpdateHTTPClient(server.URL+"/", time.Minute, false)
	if !transportSkipsTLSVerify(client.Transport) {
		t.Fatal("loopback HTTPS client should skip TLS verification")
	}

	if _, err := updater.NewTarget(context.Background(), server.URL+"/", client); err != nil {
		t.Fatalf("updater.NewTarget should accept loopback self-signed TLS: %v", err)
	}
}

func TestGokrazyUpdaterTargetSkipsTLSVerifyAfterLoopbackHTTPRedirect(t *testing.T) {
	tlsServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/update/features" {
			http.NotFound(w, r)
			return
		}
		user, password, ok := r.BasicAuth()
		if !ok {
			http.Error(w, "no Basic Authorization header set", http.StatusUnauthorized)
			return
		}
		if user != "gokrazy" || password != "photo-backup" {
			http.Error(w, "invalid credentials", http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"features":"streaming"}`))
	}))
	defer tlsServer.Close()

	redirectServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, tlsServer.URL+r.URL.Path, http.StatusTemporaryRedirect)
	}))
	defer redirectServer.Close()

	baseURL, err := url.Parse(redirectServer.URL + "/")
	if err != nil {
		t.Fatalf("parse redirect server URL: %v", err)
	}
	baseURL.User = url.UserPassword("gokrazy", "photo-backup")

	client := NewUpdateHTTPClient(baseURL.String(), time.Minute, false)
	if !transportSkipsTLSVerify(client.Transport) {
		t.Fatal("loopback HTTP client should allow a self-signed HTTPS updater redirect")
	}

	if _, err := updater.NewTarget(context.Background(), baseURL.String(), client); err != nil {
		t.Fatalf("updater.NewTarget should accept loopback HTTP to self-signed HTTPS redirect: %v", err)
	}
}

func TestGokrazyInstallerResolvesHTTPRedirectBeforeStreamingRoot(t *testing.T) {
	const username = "gokrazy"
	const password = "photo-backup"
	var rootMethod string

	tlsServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != username || pass != password {
			http.Error(w, "invalid credentials", http.StatusUnauthorized)
			return
		}

		switch r.URL.Path {
		case "/update/features":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"features":""}`))
		case "/update/root":
			rootMethod = r.Method
			if r.Method != http.MethodPut {
				http.Error(w, "expected a PUT request", http.StatusBadRequest)
				return
			}
			body, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			sum := sha256.Sum256(body)
			_, _ = fmt.Fprintf(w, "%x", sum)
		case "/update/switch", "/reboot":
			if r.Method != http.MethodPost {
				http.Error(w, "expected a POST request", http.StatusBadRequest)
				return
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer tlsServer.Close()

	redirectServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, tlsServer.URL+r.URL.Path, http.StatusFound)
	}))
	defer redirectServer.Close()

	baseURL, err := url.Parse(redirectServer.URL + "/")
	if err != nil {
		t.Fatalf("parse redirect server URL: %v", err)
	}
	baseURL.User = url.UserPassword(username, password)

	client := NewUpdateHTTPClient(baseURL.String(), time.Minute, false)
	installer := GokrazyInstaller{
		BaseURL:    baseURL.String(),
		HTTPClient: client,
	}

	var phases []string
	if err := installer.InstallRoot(context.Background(), bytes.NewReader([]byte("root-image")), func(progress InstallProgress) {
		phases = append(phases, progress.Phase)
	}); err != nil {
		t.Fatalf("InstallRoot returned error: %v", err)
	}
	if rootMethod != http.MethodPut {
		t.Fatalf("/update/root method = %q, want %q", rootMethod, http.MethodPut)
	}
	wantPhases := []string{"flashing", "switching", "rebooting"}
	if len(phases) != len(wantPhases) {
		t.Fatalf("progress phases = %#v, want %#v", phases, wantPhases)
	}
	for i := range wantPhases {
		if phases[i] != wantPhases[i] {
			t.Fatalf("progress phases = %#v, want %#v", phases, wantPhases)
		}
	}
}

func TestNewUpdateHTTPClientSkipsTLSVerifyWhenExplicitlyConfigured(t *testing.T) {
	client := NewUpdateHTTPClient("https://photo-backup.local/update/", time.Minute, true)
	if !transportSkipsTLSVerify(client.Transport) {
		t.Fatal("explicitly insecure HTTPS client should skip TLS verification")
	}
}

func TestGokrazyInstallerSkipsTLSVerifyForLoopbackHTTPSClient(t *testing.T) {
	base := &http.Client{Timeout: time.Second}
	installer := GokrazyInstaller{
		BaseURL:    "https://127.0.0.1/",
		HTTPClient: base,
	}

	client := installer.httpClient(installer.BaseURL)
	if client == base {
		t.Fatal("loopback HTTPS client should be cloned before changing transport")
	}
	if !transportSkipsTLSVerify(client.Transport) {
		t.Fatal("loopback HTTPS installer client should skip TLS verification")
	}
	if base.Transport != nil {
		t.Fatal("installer should not mutate the caller-provided client")
	}
}

func TestGokrazyInstallerKeepsTLSVerifyForRemoteHTTPSClient(t *testing.T) {
	base := &http.Client{Timeout: time.Second}
	installer := GokrazyInstaller{
		BaseURL:    "https://photo-backup.local/",
		HTTPClient: base,
	}

	client := installer.httpClient(installer.BaseURL)
	if client != base {
		t.Fatal("remote HTTPS installer client should not be replaced")
	}
	if transportSkipsTLSVerify(client.Transport) {
		t.Fatal("remote HTTPS installer client should not skip TLS verification")
	}
}

func TestGokrazyInstallerSkipsTLSVerifyForExplicitRemoteHTTPSClient(t *testing.T) {
	base := &http.Client{Timeout: time.Second}
	installer := GokrazyInstaller{
		BaseURL:            "https://photo-backup.local/",
		HTTPClient:         base,
		InsecureSkipVerify: true,
	}

	client := installer.httpClient(installer.BaseURL)
	if client == base {
		t.Fatal("explicitly insecure remote HTTPS client should be cloned before changing transport")
	}
	if !transportSkipsTLSVerify(client.Transport) {
		t.Fatal("explicitly insecure remote HTTPS installer client should skip TLS verification")
	}
	if base.Transport != nil {
		t.Fatal("installer should not mutate the caller-provided client")
	}
}

func transportSkipsTLSVerify(transport http.RoundTripper) bool {
	t, ok := transport.(*http.Transport)
	if !ok || t.TLSClientConfig == nil {
		return false
	}
	return t.TLSClientConfig.InsecureSkipVerify
}

func jsonResponse(body []byte) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewReader(body)),
	}
}
