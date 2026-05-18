package handlers

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/denysvitali/pictures-sync-s3/pkg/ota"
	"github.com/denysvitali/pictures-sync-s3/pkg/version"
)

type otaRoundTripFunc func(*http.Request) (*http.Response, error)

func (f otaRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestParseRootPartition(t *testing.T) {
	tests := []struct {
		name string
		root string
		want int
	}{
		{
			name: "mmc partition",
			root: "/dev/mmcblk0p2",
			want: 2,
		},
		{
			name: "mbr partuuid",
			root: "PARTUUID=2e18c40c-03",
			want: 3,
		},
		{
			name: "gpt partuuid partnroff root 2",
			root: "PARTUUID=9f1b0c66-e0f3-4d7e-a45f-4c1d6d6b69f8/PARTNROFF=1",
			want: 2,
		},
		{
			name: "gpt partuuid partnroff root 3",
			root: "PARTUUID=9f1b0c66-e0f3-4d7e-a45f-4c1d6d6b69f8/PARTNROFF=2",
			want: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseRootPartition(tt.root); got != tt.want {
				t.Fatalf("parseRootPartition(%q) = %d, want %d", tt.root, got, tt.want)
			}
		})
	}
}

func TestIsKnownInstalledVersion(t *testing.T) {
	tests := []struct {
		version string
		want    bool
	}{
		{"master-bf99d1223bf685669c008457d5515d9453f06835", true},
		{"v1.2.3", true},
		{"dev", false},
		{"", false},
		{"v0.0.0-00010101000000-000000000000", false},
	}

	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			if got := isKnownInstalledVersion(tt.version); got != tt.want {
				t.Fatalf("isKnownInstalledVersion(%q) = %t, want %t", tt.version, got, tt.want)
			}
		})
	}
}

func TestIsReleaseUpdateAvailable(t *testing.T) {
	installedAt := time.Date(2026, 5, 5, 19, 9, 27, 0, time.UTC)
	const currentMasterVersion = "master-b71ec29c187dbbbdcab7674c345eab0f3003ad05"
	const currentCommit = "b71ec29c187dbbbdcab7674c345eab0f3003ad05"

	tests := []struct {
		name           string
		currentVersion string
		currentCommit  string
		installedAt    time.Time
		releaseTag     string
		releaseCommit  string
		publishedAt    time.Time
		want           bool
	}{
		{
			name:           "same release tag is not an update even if published later",
			currentVersion: "master-43a8bea96ae3159e179a146a2b86fc7efb8d673e",
			installedAt:    installedAt,
			releaseTag:     "master-43a8bea96ae3159e179a146a2b86fc7efb8d673e",
			publishedAt:    installedAt.Add(55 * time.Second),
			want:           false,
		},
		{
			name:           "calver tag at current commit is not an update even if published later",
			currentVersion: currentMasterVersion,
			currentCommit:  currentCommit,
			installedAt:    installedAt,
			releaseTag:     "v2026.5.18.1602",
			releaseCommit:  currentCommit,
			publishedAt:    installedAt.Add(55 * time.Second),
			want:           false,
		},
		{
			name:           "calver tag matching current version commit is not an update when current commit is unavailable",
			currentVersion: currentMasterVersion,
			installedAt:    installedAt,
			releaseTag:     "v2026.5.18.1602",
			releaseCommit:  currentCommit,
			publishedAt:    installedAt.Add(55 * time.Second),
			want:           false,
		},
		{
			name:           "newer different release is an update",
			currentVersion: "master-old",
			currentCommit:  currentCommit,
			installedAt:    installedAt,
			releaseTag:     "master-new",
			releaseCommit:  "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			publishedAt:    installedAt.Add(time.Hour),
			want:           true,
		},
		{
			name:           "older different release is not an update",
			currentVersion: "master-new",
			installedAt:    installedAt,
			releaseTag:     "master-old",
			publishedAt:    installedAt.Add(-time.Hour),
			want:           false,
		},
		{
			name:           "different release is an update when build date is unavailable",
			currentVersion: "dev",
			releaseTag:     "master-new",
			publishedAt:    installedAt,
			want:           true,
		},
		{
			name:           "blank release is not an update when build date is unavailable",
			currentVersion: "dev",
			releaseTag:     " ",
			publishedAt:    installedAt,
			want:           false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isReleaseUpdateAvailable(tt.currentVersion, tt.currentCommit, tt.installedAt, tt.releaseTag, tt.releaseCommit, tt.publishedAt)
			if got != tt.want {
				t.Fatalf(
					"isReleaseUpdateAvailable(%q, %q, %v, %q, %q, %v) = %t, want %t",
					tt.currentVersion,
					tt.currentCommit,
					tt.installedAt,
					tt.releaseTag,
					tt.releaseCommit,
					tt.publishedAt,
					got,
					tt.want,
				)
			}
		})
	}
}

func TestHandleOTAStatusTreatsCalverTagAtCurrentCommitAsInstalled(t *testing.T) {
	const currentCommit = "b71ec29c187dbbbdcab7674c345eab0f3003ad05"
	publishedAt := time.Date(2026, 5, 18, 16, 2, 18, 0, time.UTC)

	oldVersion := version.Version
	oldBuildDate := version.BuildDate
	version.Version = "master-" + currentCommit
	version.BuildDate = "2026-05-18T16:01:09Z"
	defer func() {
		version.Version = oldVersion
		version.BuildDate = oldBuildDate
	}()

	client := &http.Client{Transport: otaRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/repos/owner/repo/releases":
			return otaJSONResponse([]ota.Release{{
				TagName:         "v2026.5.18.1602",
				TargetCommitish: "master",
				PublishedAt:     publishedAt,
				Assets: []ota.Asset{{
					Name:               ota.DefaultAssetName,
					BrowserDownloadURL: "https://example.invalid/photo-backup-rpi4b-root.squashfs.gz",
				}},
			}})
		case "/repos/owner/repo/git/ref/tags/v2026.5.18.1602":
			return otaJSONResponse(map[string]any{
				"object": map[string]any{
					"sha":  currentCommit,
					"type": "commit",
					"url":  "https://api.example.invalid/repos/owner/repo/git/commits/" + currentCommit,
				},
			})
		default:
			t.Fatalf("unexpected OTA request path: %s", req.URL.Path)
			return nil, nil
		}
	})}

	ctx := &Context{OTAMgr: &ota.Manager{
		Owner:      "owner",
		Repo:       "repo",
		APIURL:     "https://api.example.invalid",
		AssetName:  ota.DefaultAssetName,
		HTTPClient: client,
	}}

	req := httptest.NewRequest(http.MethodGet, "/api/ota/status", nil)
	w := httptest.NewRecorder()

	ctx.HandleOTAStatus(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var response otaStatusResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.UpdateAvailable {
		t.Fatal("expected current commit release not to be reported as an update")
	}
	if len(response.Releases) != 1 {
		t.Fatalf("release count = %d, want 1", len(response.Releases))
	}
	if !response.Releases[0].Installed {
		t.Fatal("expected release targeting current commit to be marked installed")
	}
}

func otaJSONResponse(value any) (*http.Response, error) {
	body, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewReader(body)),
	}, nil
}
