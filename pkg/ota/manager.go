package ota

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gokrazy/updater"
)

const (
	DefaultOwner        = "denysvitali"
	DefaultRepo         = "pictures-sync-s3"
	DefaultAssetName    = "photo-backup-rpi4b-root.squashfs.gz"
	FlashAssetName      = "photo-backup-rpi4b.img.gz"
	DefaultGitHubAPIURL = "https://api.github.com"
	DefaultUpdateURL    = "http://gokrazy:photo-backup@127.0.0.1/"
)

type Installer interface {
	InstallRoot(ctx context.Context, r io.Reader) error
}

type GokrazyInstaller struct {
	BaseURL    string
	HTTPClient *http.Client
}

func (i GokrazyInstaller) InstallRoot(ctx context.Context, r io.Reader) error {
	baseURL := strings.TrimSpace(i.BaseURL)
	if baseURL == "" {
		baseURL = DefaultUpdateURL
	}

	client := i.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}

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

type Manager struct {
	Owner     string
	Repo      string
	AssetName string
	APIURL    string

	HTTPClient *http.Client
	Installer  Installer

	mu     sync.Mutex
	status Status
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
	return &Manager{
		Owner:      envDefault("OTA_GITHUB_OWNER", DefaultOwner),
		Repo:       envDefault("OTA_GITHUB_REPO", DefaultRepo),
		AssetName:  envDefault("OTA_RELEASE_ASSET", DefaultAssetName),
		APIURL:     envDefault("OTA_GITHUB_API_URL", DefaultGitHubAPIURL),
		HTTPClient: httpClient,
		Installer:  GokrazyInstaller{BaseURL: updateURL, HTTPClient: httpClient},
		status:     Status{State: "idle"},
	}
}

func (m *Manager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.status
}

func (m *Manager) Start(ctx context.Context) (Status, error) {
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
		m.run(runCtx)
	}()
	return status, nil
}

func (m *Manager) LatestRelease(ctx context.Context) (*Release, *Asset, error) {
	releases, err := m.fetchReleases(ctx)
	if err != nil {
		return nil, nil, err
	}
	if len(releases) == 0 {
		return nil, nil, errors.New("no GitHub releases found")
	}

	sort.SliceStable(releases, func(i, j int) bool {
		return releases[i].PublishedAt.After(releases[j].PublishedAt)
	})

	for i := range releases {
		release := &releases[i]
		if release.Draft {
			continue
		}
		if asset := findAsset(release.Assets, m.assetName()); asset != nil {
			return release, asset, nil
		}
		if asset := findAsset(release.Assets, FlashAssetName); asset != nil {
			return release, nil, fmt.Errorf("latest release %s only contains %s; publish %s for gokrazy OTA", release.TagName, FlashAssetName, m.assetName())
		}
	}

	return nil, nil, fmt.Errorf("no release contains %s", m.assetName())
}

func (m *Manager) run(ctx context.Context) {
	release, asset, err := m.LatestRelease(ctx)
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
	return GokrazyInstaller{BaseURL: defaultUpdateURLFromPassword(), HTTPClient: m.client()}
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

func valueDefault(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
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
