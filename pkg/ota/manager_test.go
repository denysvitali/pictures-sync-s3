package ota

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"
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

func jsonResponse(body []byte) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewReader(body)),
	}
}
