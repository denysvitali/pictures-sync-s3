package syncmanager

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/denysvitali/pictures-sync-s3/pkg/state"
)

func TestGooglePhotosFlatRemoteUsesAlbumRoot(t *testing.T) {
	used := map[string]int{}
	counts := map[string]int{"DJI_0001.JPG": 1}

	got := googlePhotosFlatRemote("DJI_001/DJI_0001.JPG", counts, used)
	if got != "DJI_0001.JPG" {
		t.Fatalf("googlePhotosFlatRemote() = %q, want %q", got, "DJI_0001.JPG")
	}
	if strings.Contains(got, "/") {
		t.Fatalf("googlePhotosFlatRemote() contains slash: %q", got)
	}
}

func TestGooglePhotosFlatRemoteDisambiguatesDuplicateBasenames(t *testing.T) {
	used := map[string]int{}
	counts := map[string]int{"DJI_0001.JPG": 2}

	first := googlePhotosFlatRemote("DJI_001/DJI_0001.JPG", counts, used)
	second := googlePhotosFlatRemote("DJI_002/DJI_0001.JPG", counts, used)

	if first != "DJI_001_DJI_0001.JPG" {
		t.Fatalf("first remote = %q, want %q", first, "DJI_001_DJI_0001.JPG")
	}
	if second != "DJI_002_DJI_0001.JPG" {
		t.Fatalf("second remote = %q, want %q", second, "DJI_002_DJI_0001.JPG")
	}
	if strings.Contains(first, "/") || strings.Contains(second, "/") {
		t.Fatalf("flattened remotes must not contain slashes: %q %q", first, second)
	}
}

func TestGooglePhotosTransferCount(t *testing.T) {
	// Google Photos serializes batch commits per album; concurrent uploads
	// return "(409 ABORTED) The operation was aborted." Always 1 worker.
	tests := []struct {
		name      string
		transfers int
	}{
		{name: "default", transfers: 0},
		{name: "configured override ignored", transfers: 3},
		{name: "high override ignored", transfers: 64},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &Manager{transfers: tt.transfers}
			if got := m.googlePhotosTransferCount(); got != 1 {
				t.Fatalf("googlePhotosTransferCount() = %d, want 1", got)
			}
		})
	}
}

func TestIsPhotoOrVideo(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		want     bool
	}{
		{"jpg", "photo.jpg", true},
		{"jpeg", "photo.jpeg", true},
		{"mp4", "video.mp4", true},
		{"mov", "video.mov", true},
		{"heic", "photo.heic", true},
		{"ARW", "photo.ARW", false},
		{"arw", "photo.arw", false},
		{"cr2", "photo.cr2", false},
		{"nef", "photo.nef", false},
		{"dng", "photo.dng", false},
		{"txt", "readme.txt", false},
		{"no ext", "README", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isPhotoOrVideo(tt.filename); got != tt.want {
				t.Fatalf("isPhotoOrVideo(%q) = %v, want %v", tt.filename, got, tt.want)
			}
		})
	}
}

func TestGooglePhotosRcloneState(t *testing.T) {
	// Use a temp dir for the state file so tests don't interfere.
	oldPermDir := state.PermDir
	tmpDir := t.TempDir()
	state.PermDir = tmpDir
	defer func() { state.PermDir = oldPermDir }()

	// Fresh load should return empty state.
	s := loadGooglePhotosRcloneState()
	if len(s.Uploaded) != 0 {
		t.Fatalf("expected empty state, got %d entries", len(s.Uploaded))
	}

	// Add an entry and save.
	s.Uploaded["card-123/DCIM/IMG_0001.JPG"] = googlePhotosUploadedFile{
		Size:       12345,
		UploadedAt: timeNow(),
	}
	if err := saveGooglePhotosRcloneState(s); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	// Reload and verify.
	s2 := loadGooglePhotosRcloneState()
	entry, ok := s2.Uploaded["card-123/DCIM/IMG_0001.JPG"]
	if !ok {
		t.Fatal("expected entry after reload")
	}
	if entry.Size != 12345 {
		t.Fatalf("size = %d, want 12345", entry.Size)
	}

	// Verify the file was actually written.
	statePath := filepath.Join(tmpDir, googlePhotosRcloneStateFile)
	if _, err := os.Stat(statePath); err != nil {
		t.Fatalf("state file not found: %v", err)
	}
}

func timeNow() time.Time {
	return time.Now()
}
