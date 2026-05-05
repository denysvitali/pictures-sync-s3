package handlers

import (
	"testing"
	"time"
)

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

	tests := []struct {
		name           string
		currentVersion string
		installedAt    time.Time
		releaseTag     string
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
			name:           "newer different release is an update",
			currentVersion: "master-old",
			installedAt:    installedAt,
			releaseTag:     "master-new",
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
			got := isReleaseUpdateAvailable(tt.currentVersion, tt.installedAt, tt.releaseTag, tt.publishedAt)
			if got != tt.want {
				t.Fatalf(
					"isReleaseUpdateAvailable(%q, %v, %q, %v) = %t, want %t",
					tt.currentVersion,
					tt.installedAt,
					tt.releaseTag,
					tt.publishedAt,
					got,
					tt.want,
				)
			}
		})
	}
}
