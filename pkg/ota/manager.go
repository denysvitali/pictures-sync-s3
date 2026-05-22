package ota

import (
	"compress/gzip"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/denysvitali/pictures-sync-s3/pkg/auth"
	"github.com/denysvitali/pictures-sync-s3/pkg/state"
	"github.com/denysvitali/pictures-sync-s3/pkg/utils"
	"github.com/gokrazy/updater"
)

const (
	DefaultOwner        = "denysvitali"
	DefaultRepo         = "pictures-sync-s3"
	DefaultAssetName    = "photo-backup-rpi4b-root.squashfs.gz"
	FlashAssetName      = "photo-backup-rpi4b.img.gz"
	DefaultGitHubAPIURL = "https://api.github.com"
	// UpdateInsecureEnv enables TLS verification bypass for self-signed gokrazy updater endpoints.
	UpdateInsecureEnv = "OTA_GOKRAZY_INSECURE"
	// #nosec G101 -- default gokrazy updater URL with well-known local device password
	DefaultUpdateURL = "http://gokrazy:photo-backup@127.0.0.1/"

	maxInstallHistoryEntries = 20
)

type Installer interface {
	InstallRoot(ctx context.Context, r io.Reader, progress InstallProgressFunc) error
}

type InstallProgressFunc func(InstallProgress)

type InstallProgress struct {
	Phase           string
	Message         string
	ProgressPercent float64
}

type GokrazyInstaller struct {
	BaseURL            string
	HTTPClient         *http.Client
	InsecureSkipVerify bool
}

func (i GokrazyInstaller) InstallRoot(ctx context.Context, r io.Reader, progress InstallProgressFunc) error {
	baseURL := normalizeUpdateBaseURL(i.BaseURL)
	if baseURL == "" {
		baseURL = DefaultUpdateURL
	}

	client := i.httpClient(baseURL)

	target, err := NewUpdateTarget(ctx, baseURL, client)
	if err != nil {
		return fmt.Errorf("connect to gokrazy updater: %w", err)
	}
	reportInstallProgress(progress, "flashing", "Downloading and flashing OTA image", 10)
	if err := target.StreamTo(ctx, "root", r); err != nil {
		return fmt.Errorf("stream root image: %w", err)
	}
	reportInstallProgress(progress, "switching", "Switching root partition", 90)
	if err := target.Switch(ctx); err != nil {
		return fmt.Errorf("switch root partition: %w", err)
	}
	reportInstallProgress(progress, "rebooting", "Requesting reboot", 95)
	if err := target.Reboot(ctx); err != nil {
		return fmt.Errorf("reboot: %w", err)
	}
	return nil
}

func (i GokrazyInstaller) httpClient(baseURL string) *http.Client {
	client := i.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Minute}
	}
	return configureUpdateHTTPClient(client, baseURL, i.InsecureSkipVerify)
}

type Manager struct {
	Owner     string
	Repo      string
	AssetName string
	APIURL    string

	HTTPClient *http.Client
	Installer  Installer

	mu             sync.Mutex
	status         Status
	installHistory []InstallHistoryEntry
	subscribers    []*statusSubscriber
}

type statusSubscriber struct {
	mu     sync.Mutex
	ch     chan Status
	closed bool
}

func (s *statusSubscriber) send(status Status) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	select {
	case s.ch <- status:
	default:
	}
}

func (s *statusSubscriber) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.closed = true
	close(s.ch)
}

type Status struct {
	State            string    `json:"state"`
	Phase            string    `json:"phase,omitempty"`
	Message          string    `json:"message,omitempty"`
	Release          string    `json:"release,omitempty"`
	Asset            string    `json:"asset,omitempty"`
	AssetURL         string    `json:"asset_url,omitempty"`
	PublishedAt      time.Time `json:"published_at,omitempty"`
	StartedAt        time.Time `json:"started_at,omitempty"`
	FinishedAt       time.Time `json:"finished_at,omitempty"`
	ProgressPercent  float64   `json:"progress_percent,omitempty"`
	DownloadedBytes  int64     `json:"downloaded_bytes,omitempty"`
	TotalBytes       int64     `json:"total_bytes,omitempty"`
	DownloadSpeedBps float64   `json:"download_speed_bps,omitempty"`
	Error            string    `json:"error,omitempty"`
}

type InstallHistoryEntry struct {
	Release    string    `json:"release"`
	Asset      string    `json:"asset"`
	AssetURL   string    `json:"asset_url"`
	State      string    `json:"state"`
	Message    string    `json:"message,omitempty"`
	Error      string    `json:"error,omitempty"`
	StartedAt  time.Time `json:"started_at,omitempty"`
	FinishedAt time.Time `json:"finished_at,omitempty"`
}

type Release struct {
	TagName         string    `json:"tag_name"`
	TargetCommitish string    `json:"target_commitish"`
	Name            string    `json:"name"`
	Draft           bool      `json:"draft"`
	Prerelease      bool      `json:"prerelease"`
	PublishedAt     time.Time `json:"published_at"`
	Assets          []Asset   `json:"assets"`
	HTMLURL         string    `json:"html_url"`
}

type Asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

func NewManager() *Manager {
	updateURL := strings.TrimSpace(os.Getenv("OTA_GOKRAZY_UPDATE_URL"))
	if updateURL == "" {
		updateURL = defaultUpdateURLFromPassword()
	}

	httpClient := &http.Client{Timeout: 30 * time.Minute}
	insecureUpdate := envBool(UpdateInsecureEnv, false)
	mgr := &Manager{
		Owner:      envDefault("OTA_GITHUB_OWNER", DefaultOwner),
		Repo:       envDefault("OTA_GITHUB_REPO", DefaultRepo),
		AssetName:  envDefault("OTA_RELEASE_ASSET", DefaultAssetName),
		APIURL:     envDefault("OTA_GITHUB_API_URL", DefaultGitHubAPIURL),
		HTTPClient: httpClient,
		Installer: GokrazyInstaller{
			BaseURL:            updateURL,
			HTTPClient:         NewUpdateHTTPClient(updateURL, 30*time.Minute, insecureUpdate),
			InsecureSkipVerify: insecureUpdate,
		},
		status:         Status{State: "idle"},
		installHistory: make([]InstallHistoryEntry, 0),
		subscribers:    make([]*statusSubscriber, 0),
	}
	_ = mgr.loadInstallHistory()
	return mgr
}

func (m *Manager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.status
}

func (m *Manager) InstallationHistory() []InstallHistoryEntry {
	m.mu.Lock()
	defer m.mu.Unlock()

	history := make([]InstallHistoryEntry, len(m.installHistory))
	copy(history, m.installHistory)
	return history
}

func (m *Manager) Subscribe() chan Status {
	m.mu.Lock()
	defer m.mu.Unlock()

	ch := make(chan Status, 16)
	m.subscribers = append(m.subscribers, &statusSubscriber{ch: ch})
	return ch
}

func (m *Manager) Unsubscribe(ch chan Status) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i, subscriber := range m.subscribers {
		if subscriber.ch == ch {
			m.subscribers = append(m.subscribers[:i], m.subscribers[i+1:]...)
			subscriber.close()
			break
		}
	}
}

func (m *Manager) loadInstallHistory() error {
	var history []InstallHistoryEntry
	if err := utils.LoadJSON(m.installHistoryPath(), &history, []InstallHistoryEntry{}); err != nil {
		return err
	}

	if len(history) > maxInstallHistoryEntries {
		history = history[len(history)-maxInstallHistoryEntries:]
	}

	m.mu.Lock()
	m.installHistory = history
	m.mu.Unlock()

	return nil
}

func (m *Manager) installHistoryPath() string {
	return filepath.Join(state.PermDir, "ota-install-history.json")
}

func (m *Manager) recordInstallHistory(entry InstallHistoryEntry) {
	// Keep history in memory and persist best effort.
	m.mu.Lock()
	m.installHistory = append(m.installHistory, entry)
	if len(m.installHistory) > maxInstallHistoryEntries {
		m.installHistory = m.installHistory[len(m.installHistory)-maxInstallHistoryEntries:]
	}
	history := make([]InstallHistoryEntry, len(m.installHistory))
	copy(history, m.installHistory)
	m.mu.Unlock()

	_ = utils.SaveJSON(m.installHistoryPath(), history, 0644)
}

func (m *Manager) historyFromStatus(status Status) InstallHistoryEntry {
	return InstallHistoryEntry{
		Release:    status.Release,
		Asset:      status.Asset,
		AssetURL:   status.AssetURL,
		State:      status.State,
		Message:    status.Message,
		Error:      status.Error,
		StartedAt:  status.StartedAt,
		FinishedAt: status.FinishedAt,
	}
}

func (m *Manager) Start(ctx context.Context) (Status, error) {
	return m.StartWithRelease(ctx, "")
}

func (m *Manager) StartWithRelease(ctx context.Context, release string) (Status, error) {
	release = strings.TrimSpace(release)

	m.mu.Lock()
	if m.status.State == "checking" || m.status.State == "downloading" || m.status.State == "installing" {
		status := m.status
		m.mu.Unlock()
		return status, errors.New("OTA installation is already running")
	}
	m.status = Status{
		State:           "checking",
		Phase:           "checking",
		Message:         "Checking GitHub releases",
		StartedAt:       time.Now(),
		ProgressPercent: 2,
	}
	status := m.status
	subscribers := m.subscribersSnapshotLocked()
	m.mu.Unlock()
	publishStatus(status, subscribers)

	runCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Hour)
	go func() {
		defer cancel()
		m.run(runCtx, release)
	}()
	return status, nil
}

func (m *Manager) LatestRelease(ctx context.Context) (*Release, *Asset, error) {
	releases, err := m.AvailableReleases(ctx)
	if err != nil {
		return nil, nil, err
	}
	if len(releases) == 0 {
		return nil, nil, errors.New("no GitHub releases found")
	}

	return &releases[0], &releases[0].Assets[0], nil
}

func (m *Manager) SelectRelease(ctx context.Context, tag string) (*Release, *Asset, error) {
	if strings.EqualFold(strings.TrimSpace(tag), "latest") || strings.TrimSpace(tag) == "" {
		return m.LatestRelease(ctx)
	}

	releases, err := m.AvailableReleases(ctx)
	if err != nil {
		return nil, nil, err
	}
	for i := range releases {
		if releases[i].TagName == tag {
			return &releases[i], &releases[i].Assets[0], nil
		}
	}
	return nil, nil, fmt.Errorf("release %q not found", tag)
}

func (m *Manager) run(ctx context.Context, releaseTag string) {
	release, asset, err := m.SelectRelease(ctx, releaseTag)
	if err != nil {
		m.fail(err)
		return
	}

	m.set(Status{
		State:           "downloading",
		Phase:           "downloading",
		Message:         "Preparing OTA download",
		Release:         release.TagName,
		Asset:           asset.Name,
		AssetURL:        asset.BrowserDownloadURL,
		PublishedAt:     release.PublishedAt,
		StartedAt:       m.Status().StartedAt,
		ProgressPercent: 5,
		TotalBytes:      asset.Size,
	})

	// Look up the SHA256 sidecar (if the release publishes one) before
	// downloading the image, so we know up front whether we can perform a
	// strict integrity check.
	expectedHash, sidecarURL, err := m.resolveExpectedSHA256(ctx, release, asset)
	if err != nil {
		m.fail(fmt.Errorf("resolve OTA SHA256 sidecar: %w", err))
		return
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.BrowserDownloadURL, nil)
	if err != nil {
		m.fail(err)
		return
	}
	resp, err := m.client().Do(req)
	if err != nil {
		m.fail(fmt.Errorf("download OTA asset: %w", err))
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		m.fail(fmt.Errorf("download OTA asset: GitHub returned %s", resp.Status))
		return
	}

	totalBytes := asset.Size
	if totalBytes <= 0 && resp.ContentLength > 0 {
		totalBytes = resp.ContentLength
	}
	body := newDownloadProgressReader(resp.Body, totalBytes, m.updateDownloadProgress)

	// Stage the download to disk while computing SHA256. We must verify the
	// image BEFORE handing any bytes to the gokrazy updater so a corrupted
	// download never reaches the inactive partition.
	staged, err := stageReader(otaStagingDir(), asset.Name, body)
	if err != nil {
		m.fail(fmt.Errorf("stage OTA image: %w", err))
		return
	}
	defer func() {
		_ = staged.Close()
	}()

	// Size verification runs even when no SHA256 sidecar is published. A
	// truncated HTTP body (proxy/CDN cutoff, server EOF without error) would
	// otherwise be flashed as if it were complete and brick the device. Prefer
	// the GitHub release asset size (signed by the release manifest) over the
	// Content-Length header, but fall back to Content-Length when the API did
	// not report a size.
	expectedSize := asset.Size
	sizeSource := "github asset size"
	if expectedSize <= 0 && resp.ContentLength > 0 {
		expectedSize = resp.ContentLength
		sizeSource = "Content-Length"
	}
	if err := staged.VerifyExpectedSize(expectedSize, sizeSource); err != nil {
		m.fail(err)
		return
	}

	if expectedHash != "" {
		source := "github-sidecar"
		if sidecarURL != "" {
			source = sidecarURL
		}
		if err := staged.VerifyExpected(expectedHash, source); err != nil {
			m.fail(err)
			return
		}
	}

	imageFile, err := staged.Open()
	if err != nil {
		m.fail(fmt.Errorf("open staged OTA image: %w", err))
		return
	}
	defer imageFile.Close()

	gz, err := gzip.NewReader(imageFile)
	if err != nil {
		m.fail(fmt.Errorf("open gzip OTA asset: %w", err))
		return
	}
	defer gz.Close()

	m.updateInstallProgress(InstallProgress{
		Phase:           "flashing",
		Message:         "Flashing verified OTA image",
		ProgressPercent: 85,
	})
	if err := m.installer().InstallRoot(ctx, gz, m.updateInstallProgress); err != nil {
		m.fail(fmt.Errorf("apply OTA image: %w", err))
		return
	}

	var snapshot Status
	m.mutateStatus(func(status *Status) {
		status.State = "installed"
		status.Phase = "installed"
		status.Message = "OTA image installed; reboot requested"
		status.FinishedAt = time.Now()
		status.ProgressPercent = 100
		snapshot = *status
	})
	m.recordInstallHistory(m.historyFromStatus(snapshot))
}

// resolveExpectedSHA256 looks for a SHA256 sidecar accompanying the release
// asset and returns the parsed hex digest. It first checks for an asset named
// "<asset>.sha256" attached to the same release, falling back to deriving the
// sidecar URL from the asset's BrowserDownloadURL when releases.Assets has
// already been filtered down. Returns an empty digest (with no error) when no
// sidecar is present, unless OTA_REQUIRE_SHA256 is set.
func (m *Manager) resolveExpectedSHA256(ctx context.Context, release *Release, asset *Asset) (string, string, error) {
	sidecarName := asset.Name + SHA256SidecarSuffix
	sidecar := findAsset(release.Assets, sidecarName)

	var sidecarURL string
	if sidecar != nil {
		sidecarURL = sidecar.BrowserDownloadURL
	} else if asset.BrowserDownloadURL != "" {
		// Fallback: derive "<asset_url>.sha256" so operators can publish a
		// sidecar without us having to re-list release assets. Only used if
		// the sibling asset was not found in the release listing.
		sidecarURL = asset.BrowserDownloadURL + SHA256SidecarSuffix
	}

	if sidecarURL == "" {
		if envBool("OTA_REQUIRE_SHA256", false) {
			return "", "", fmt.Errorf("no SHA256 sidecar available for asset %q and OTA_REQUIRE_SHA256 is set", asset.Name)
		}
		return "", "", nil
	}

	digest, err := downloadSHA256Sidecar(ctx, m.client(), sidecarURL)
	if err != nil {
		return "", sidecarURL, err
	}
	if digest == "" && envBool("OTA_REQUIRE_SHA256", false) {
		return "", sidecarURL, fmt.Errorf("SHA256 sidecar at %s is missing and OTA_REQUIRE_SHA256 is set", sidecarURL)
	}
	return digest, sidecarURL, nil
}

func (m *Manager) AvailableReleases(ctx context.Context) ([]Release, error) {
	releases, err := m.fetchReleases(ctx)
	if err != nil {
		return nil, err
	}

	sort.SliceStable(releases, func(i, j int) bool {
		return releases[i].PublishedAt.After(releases[j].PublishedAt)
	})

	filtered := make([]Release, 0, len(releases))
	for i := range releases {
		release := releases[i]
		if release.Draft {
			continue
		}
		asset := findAsset(release.Assets, m.assetName())
		if asset == nil {
			continue
		}
		kept := []Asset{*asset}
		// Retain the optional SHA256 sidecar so the installer can verify
		// the downloaded image before applying it. The main asset must
		// remain at index 0 — existing callers (e.g. the WebUI status
		// handler) read release.Assets[0] as the image.
		if sidecar := findAsset(release.Assets, m.assetName()+SHA256SidecarSuffix); sidecar != nil {
			kept = append(kept, *sidecar)
		}
		release.Assets = kept
		filtered = append(filtered, release)
	}

	if len(filtered) == 0 {
		return nil, errors.New("no release contains " + m.assetName())
	}

	return filtered, nil
}

func (m *Manager) ReleaseTagCommit(ctx context.Context, tag string) (string, error) {
	tag = strings.TrimSpace(tag)
	if tag == "" {
		return "", nil
	}

	apiURL := strings.TrimRight(m.apiURL(), "/")
	reqURL := fmt.Sprintf("%s/repos/%s/%s/git/ref/tags/%s", apiURL, url.PathEscape(m.owner()), url.PathEscape(m.repo()), url.PathEscape(tag))
	obj, err := m.fetchGitObject(ctx, reqURL)
	if err != nil {
		return "", err
	}

	for range 5 {
		switch obj.Type {
		case "commit":
			return obj.SHA, nil
		case "tag":
			if strings.TrimSpace(obj.URL) == "" {
				return "", errors.New("tag object URL is empty")
			}
			obj, err = m.fetchGitObject(ctx, obj.URL)
			if err != nil {
				return "", err
			}
		default:
			return "", fmt.Errorf("tag %q points to unsupported object type %q", tag, obj.Type)
		}
	}

	return "", fmt.Errorf("tag %q resolves through too many nested tag objects", tag)
}

type gitObject struct {
	SHA  string `json:"sha"`
	Type string `json:"type"`
	URL  string `json:"url"`
}

type gitObjectResponse struct {
	Object gitObject `json:"object"`
}

func (m *Manager) fetchGitObject(ctx context.Context, reqURL string) (gitObject, error) {
	req, err := m.newGitHubRequest(ctx, reqURL)
	if err != nil {
		return gitObject{}, err
	}

	resp, err := m.client().Do(req)
	if err != nil {
		return gitObject{}, fmt.Errorf("fetch GitHub object: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return gitObject{}, fmt.Errorf("fetch GitHub object: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var ref gitObjectResponse
	if err := json.NewDecoder(resp.Body).Decode(&ref); err != nil {
		return gitObject{}, fmt.Errorf("decode GitHub object: %w", err)
	}
	if ref.Object.SHA == "" {
		return gitObject{}, errors.New("decode GitHub object: object is missing")
	}
	return ref.Object, nil
}

func (m *Manager) fetchReleases(ctx context.Context) ([]Release, error) {
	apiURL := strings.TrimRight(m.apiURL(), "/")
	reqURL := fmt.Sprintf("%s/repos/%s/%s/releases?per_page=50", apiURL, url.PathEscape(m.owner()), url.PathEscape(m.repo()))
	req, err := m.newGitHubRequest(ctx, reqURL)
	if err != nil {
		return nil, err
	}

	resp, err := m.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch GitHub releases: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("fetch GitHub releases: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var releases []Release
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return nil, fmt.Errorf("decode GitHub releases: %w", err)
	}
	return releases, nil
}

func (m *Manager) newGitHubRequest(ctx context.Context, reqURL string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "pictures-sync-s3-ota")
	if token := strings.TrimSpace(os.Getenv("GITHUB_TOKEN")); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return req, nil
}

func (m *Manager) set(status Status) {
	m.mu.Lock()
	m.status = status
	subscribers := m.subscribersSnapshotLocked()
	m.mu.Unlock()

	publishStatus(status, subscribers)
}

// mutateStatus applies fn to the current status under m.mu and publishes the
// result atomically. It exists to close a read-modify-write race in the
// download/install progress callbacks: with separate Status() + set() calls,
// two concurrent callbacks (e.g. download-progress and install-progress, or
// the installer's own progress goroutines) could clobber each other's updates
// and emit a stale snapshot to WebSocket subscribers.
func (m *Manager) mutateStatus(fn func(*Status)) {
	m.mu.Lock()
	fn(&m.status)
	status := m.status
	subscribers := m.subscribersSnapshotLocked()
	m.mu.Unlock()

	publishStatus(status, subscribers)
}

func (m *Manager) subscribersSnapshotLocked() []*statusSubscriber {
	subscribers := make([]*statusSubscriber, len(m.subscribers))
	copy(subscribers, m.subscribers)
	return subscribers
}

func publishStatus(status Status, subscribers []*statusSubscriber) {
	for _, subscriber := range subscribers {
		subscriber.send(status)
	}
}

func (m *Manager) updateInstallProgress(progress InstallProgress) {
	m.mutateStatus(func(status *Status) {
		status.State = "installing"
		status.Phase = progress.Phase
		status.Message = progress.Message
		if progress.ProgressPercent > status.ProgressPercent {
			status.ProgressPercent = progress.ProgressPercent
		}
	})
}

func (m *Manager) updateDownloadProgress(progress downloadProgress) {
	m.mutateStatus(func(status *Status) {
		status.DownloadedBytes = progress.downloaded
		status.TotalBytes = progress.total
		status.DownloadSpeedBps = progress.speedBps
		if progress.total > 0 {
			downloadPercent := (float64(progress.downloaded) / float64(progress.total)) * 75
			status.ProgressPercent = minFloat64(85, maxFloat64(status.ProgressPercent, 10+downloadPercent))
		}
		if status.Phase == "" || status.Phase == "downloading" {
			status.Phase = "flashing"
			status.Message = "Downloading and flashing OTA image"
			status.State = "installing"
		}
	})
}

func (m *Manager) fail(err error) {
	var snapshot Status
	m.mutateStatus(func(status *Status) {
		status.State = "failed"
		status.Phase = "failed"
		status.Error = err.Error()
		status.Message = "OTA installation failed"
		// Surface verification failures distinctly so the UI (and operators)
		// can tell a corrupted download apart from an install-time error.
		if IsHashMismatch(err) {
			status.Phase = "verification_failed"
			status.Message = "OTA image rejected: SHA256 mismatch"
		}
		if IsSizeMismatch(err) {
			status.Phase = "verification_failed"
			status.Message = "OTA image rejected: size mismatch (truncated download)"
		}
		status.FinishedAt = time.Now()
		snapshot = *status
	})
	m.recordInstallHistory(m.historyFromStatus(snapshot))
}

func reportInstallProgress(progress InstallProgressFunc, phase, message string, progressPercent float64) {
	if progress == nil {
		return
	}
	progress(InstallProgress{
		Phase:           phase,
		Message:         message,
		ProgressPercent: progressPercent,
	})
}

type downloadProgress struct {
	downloaded int64
	total      int64
	speedBps   float64
}

type downloadProgressReader struct {
	reader         io.Reader
	total          int64
	started        time.Time
	lastReport     time.Time
	downloaded     int64
	reportProgress func(downloadProgress)
}

func newDownloadProgressReader(reader io.Reader, total int64, reportProgress func(downloadProgress)) *downloadProgressReader {
	now := time.Now()
	return &downloadProgressReader{
		reader:         reader,
		total:          total,
		started:        now,
		lastReport:     now,
		reportProgress: reportProgress,
	}
}

func (r *downloadProgressReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	if n > 0 {
		r.downloaded += int64(n)
		r.report(false)
	}
	if err != nil {
		r.report(true)
	}
	return n, err
}

func (r *downloadProgressReader) report(force bool) {
	if r.reportProgress == nil {
		return
	}
	now := time.Now()
	if !force && now.Sub(r.lastReport) < time.Second && (r.total <= 0 || r.downloaded < r.total) {
		return
	}
	elapsed := now.Sub(r.started).Seconds()
	speed := 0.0
	if elapsed > 0 {
		speed = float64(r.downloaded) / elapsed
	}
	r.lastReport = now
	r.reportProgress(downloadProgress{
		downloaded: r.downloaded,
		total:      r.total,
		speedBps:   speed,
	})
}

func minFloat64(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func maxFloat64(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func (m *Manager) client() *http.Client {
	if m.HTTPClient != nil {
		return m.HTTPClient
	}
	return http.DefaultClient
}

func (m *Manager) installer() Installer {
	if m.Installer != nil {
		return m.Installer
	}
	updateURL := defaultUpdateURLFromPassword()
	insecureUpdate := envBool(UpdateInsecureEnv, false)
	return GokrazyInstaller{
		BaseURL:            updateURL,
		HTTPClient:         NewUpdateHTTPClient(updateURL, 30*time.Minute, insecureUpdate),
		InsecureSkipVerify: insecureUpdate,
	}
}

func (m *Manager) owner() string {
	return valueDefault(m.Owner, DefaultOwner)
}

func (m *Manager) repo() string {
	return valueDefault(m.Repo, DefaultRepo)
}

func (m *Manager) assetName() string {
	return valueDefault(m.AssetName, DefaultAssetName)
}

func (m *Manager) apiURL() string {
	return valueDefault(m.APIURL, DefaultGitHubAPIURL)
}

func findAsset(assets []Asset, name string) *Asset {
	for i := range assets {
		if assets[i].Name == name {
			return &assets[i]
		}
	}
	return nil
}

func envDefault(key, fallback string) string {
	return valueDefault(os.Getenv(key), fallback)
}

func envBool(key string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	switch strings.ToLower(value) {
	case "1", "true", "t", "yes", "y", "on":
		return true
	case "0", "false", "f", "no", "n", "off":
		return false
	default:
		return fallback
	}
}

func valueDefault(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}

// NewUpdateHTTPClient returns an HTTP client configured for gokrazy updater uploads.
func NewUpdateHTTPClient(rawURL string, timeout time.Duration, insecureSkipVerify bool) *http.Client {
	return configureUpdateHTTPClient(&http.Client{Timeout: timeout}, rawURL, insecureSkipVerify)
}

// NewUpdateTarget returns a gokrazy updater target after resolving gokrazy's
// HTTP-to-HTTPS redirect. Uploads stream request bodies, so following a 302
// during PUT would turn the upload into a GET and fail at /update/root.
func NewUpdateTarget(ctx context.Context, rawBaseURL string, client *http.Client) (*updater.Target, error) {
	baseURL := normalizeUpdateBaseURL(rawBaseURL)
	if baseURL == "" {
		baseURL = DefaultUpdateURL
	}
	if client == nil {
		client = http.DefaultClient
	}

	resolvedBaseURL, err := resolveUpdateBaseURL(ctx, baseURL, client)
	if err != nil {
		return nil, err
	}
	return updater.NewTarget(ctx, resolvedBaseURL, client)
}

func normalizeUpdateBaseURL(rawURL string) string {
	baseURL := strings.TrimSpace(rawURL)
	if baseURL == "" || strings.HasSuffix(baseURL, "/") {
		return baseURL
	}
	return baseURL + "/"
}

func resolveUpdateBaseURL(ctx context.Context, baseURL string, client *http.Client) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"update/features", nil)
	if err != nil {
		return "", err
	}

	noRedirectClient := *client
	noRedirectClient.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}

	resp, err := noRedirectClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther, http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
	default:
		return baseURL, nil
	}

	location := strings.TrimSpace(resp.Header.Get("Location"))
	if location == "" {
		return baseURL, nil
	}
	redirectURL, err := req.URL.Parse(location)
	if err != nil {
		return "", err
	}
	if redirectURL.User == nil {
		redirectURL.User = req.URL.User
	}

	redirectURL.RawQuery = ""
	redirectURL.Fragment = ""
	redirectURL.Path = strings.TrimSuffix(redirectURL.Path, "update/features")
	if redirectURL.Path == "" {
		redirectURL.Path = "/"
	}
	if !strings.HasSuffix(redirectURL.Path, "/") {
		redirectURL.Path += "/"
	}
	return redirectURL.String(), nil
}

func configureUpdateHTTPClient(client *http.Client, rawURL string, insecureSkipVerify bool) *http.Client {
	if !insecureSkipVerify && !shouldSkipUpdateTLSVerify(rawURL) {
		return client
	}

	cloned := *client
	var transport *http.Transport
	if client.Transport == nil {
		transport = http.DefaultTransport.(*http.Transport).Clone()
	} else {
		existing, ok := client.Transport.(*http.Transport)
		if !ok {
			return client
		}
		transport = existing.Clone()
	}

	tlsConfig := &tls.Config{}
	if transport.TLSClientConfig != nil {
		tlsConfig = transport.TLSClientConfig.Clone()
	}
	// #nosec G402 -- explicit OTA updater option; loopback gokrazy updater uses self-signed TLS
	tlsConfig.InsecureSkipVerify = true
	transport.TLSClientConfig = tlsConfig
	cloned.Transport = transport
	configureLoopbackRedirectAuth(&cloned, rawURL)
	return &cloned
}

func configureLoopbackRedirectAuth(client *http.Client, rawURL string) {
	baseURL, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || baseURL.User == nil || !isLoopbackHost(baseURL.Hostname()) {
		return
	}

	username := baseURL.User.Username()
	password, _ := baseURL.User.Password()
	checkRedirect := client.CheckRedirect
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) > 0 && isLoopbackHost(req.URL.Hostname()) && req.Header.Get("Authorization") == "" {
			req.SetBasicAuth(username, password)
		}
		if checkRedirect != nil {
			return checkRedirect(req, via)
		}
		if len(via) >= 10 {
			return errors.New("stopped after 10 redirects")
		}
		return nil
	}
}

func shouldSkipUpdateTLSVerify(rawURL string) bool {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	// gokrazy's loopback HTTP updater endpoint may redirect to its self-signed
	// HTTPS endpoint before /update/features is read.
	return isLoopbackHost(u.Hostname())
}

func isLoopbackHost(host string) bool {
	host = strings.TrimSpace(strings.TrimSuffix(host, "."))
	if strings.EqualFold(host, "localhost") {
		return true
	}
	if parsedHost, _, err := net.SplitHostPort(host); err == nil {
		host = parsedHost
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func defaultUpdateURLFromPassword() string {
	u := &url.URL{
		Scheme: "http",
		User:   url.UserPassword("gokrazy", auth.CurrentGokrazyPassword("photo-backup")),
		Host:   "127.0.0.1",
		Path:   "/",
	}
	return u.String()
}
