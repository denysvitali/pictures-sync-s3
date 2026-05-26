package syncmanager

import (
	"strings"
	"testing"
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
