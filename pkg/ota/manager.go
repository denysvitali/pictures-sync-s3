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
	InstallRoot(ctx context.Context, r io.Reader) error
}

type GokrazyInstaller struct {
	BaseURL            string
	HTTPClient         *http.Client
	InsecureSkipVerify bool
}

func (i GokrazyInstaller) InstallRoot(ctx context.Context, r io.Reader) error {
	baseURL := strings.TrimSpace(i.BaseURL)
	if baseURL == "" {
		baseURL = DefaultUpdateURL
	}

	client := i.httpClient(baseURL)

	target, err := updater.NewTarget(ctx, baseURL, client)
	if err != nil {
		return fmt.Errorf("connect to gokrazy updater: %w", err)
	}
	if err := target.StreamTo(ctx, "root", r); err != nil {
		return fmt.Errorf("stream root image: %w", err)
	}
	if err := target.Switch(ctx); err != nil {
		return fmt.Errorf("switch root partition: %w", err)
	}
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
}

type Status struct {
	State       string    `json:"state"`
	Message     string    `json:"message,omitempty"`
	Release     string    `json:"release,omitempty"`
	Asset       string    `json:"asset,omitempty"`
	AssetURL    string    `json:"asset_url,omitempty"`
	PublishedAt time.Time `json:"published_at,omitempty"`
	StartedAt   time.Time `json:"started_at,omitempty"`
	FinishedAt  time.Time `json:"finished_at,omitempty"`
	Error       string    `json:"error,omitempty"`
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
	TagName     string    `json:"tag_name"`
	Name        string    `json:"name"`
	Draft       bool      `json:"draft"`
	Prerelease  bool      `json:"prerelease"`
	PublishedAt time.Time `json:"published_at"`
	Assets      []Asset   `json:"assets"`
	HTMLURL     string    `json:"html_url"`
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
	m.status = Status{State: "checking", Message: "Checking GitHub releases", StartedAt: time.Now()}
	status := m.status
	m.mu.Unlock()

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
		State:       "downloading",
		Message:     "Downloading OTA image",
		Release:     release.TagName,
		Asset:       asset.Name,
		AssetURL:    asset.BrowserDownloadURL,
		PublishedAt: release.PublishedAt,
		StartedAt:   m.Status().StartedAt,
	})

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

	gz, err := gzip.NewReader(resp.Body)
	if err != nil {
		m.fail(fmt.Errorf("open gzip OTA asset: %w", err))
		return
	}
	defer gz.Close()

	m.updateState("installing", "Installing OTA image")
	if err := m.installer().InstallRoot(ctx, gz); err != nil {
		m.fail(err)
		return
	}

	status := m.Status()
	status.State = "installed"
	status.Message = "OTA image installed; reboot requested"
	status.FinishedAt = time.Now()
	m.set(status)
	m.recordInstallHistory(m.historyFromStatus(status))
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
		if asset := findAsset(release.Assets, m.assetName()); asset != nil {
			release.Assets = []Asset{*asset}
			filtered = append(filtered, release)
		}
	}

	if len(filtered) == 0 {
		return nil, errors.New("no release contains " + m.assetName())
	}

	return filtered, nil
}

func (m *Manager) fetchReleases(ctx context.Context) ([]Release, error) {
	apiURL := strings.TrimRight(m.apiURL(), "/")
	reqURL := fmt.Sprintf("%s/repos/%s/%s/releases?per_page=50", apiURL, url.PathEscape(m.owner()), url.PathEscape(m.repo()))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "pictures-sync-s3-ota")
	if token := strings.TrimSpace(os.Getenv("GITHUB_TOKEN")); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
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

func (m *Manager) set(status Status) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.status = status
}

func (m *Manager) updateState(state, message string) {
	status := m.Status()
	status.State = state
	status.Message = message
	m.set(status)
}

func (m *Manager) fail(err error) {
	status := m.Status()
	status.State = "failed"
	status.Error = err.Error()
	status.Message = "OTA installation failed"
	status.FinishedAt = time.Now()
	m.set(status)
	m.recordInstallHistory(m.historyFromStatus(status))
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
	password := "photo-backup"
	if data, err := os.ReadFile("/etc/gokr-pw.txt"); err == nil {
		if trimmed := strings.TrimSpace(string(data)); trimmed != "" {
			password = trimmed
		}
	}
	u := &url.URL{
		Scheme: "http",
		User:   url.UserPassword("gokrazy", password),
		Host:   "127.0.0.1",
		Path:   "/",
	}
	return u.String()
}
