package cloudphotos

import (
	"testing"
)

func TestValidateCardID(t *testing.T) {
	tests := []struct {
		name    string
		cardID  string
		wantErr bool
	}{
		{
			name:    "valid card ID",
			cardID:  "card-a1b2c3d4",
			wantErr: false,
		},
		{
			name:    "empty card ID",
			cardID:  "",
			wantErr: true,
		},
		{
			name:    "path traversal with ..",
			cardID:  "../etc/passwd",
			wantErr: true,
		},
		{
			name:    "path with slash",
			cardID:  "card/test",
			wantErr: true,
		},
		{
			name:    "path with backslash",
			cardID:  "card\\test",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateCardID(tt.cardID)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateCardID() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestIsImageFile(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		want     bool
	}{
		{name: "JPG uppercase", filename: "IMG_1234.JPG", want: true},
		{name: "jpg lowercase", filename: "img_1234.jpg", want: true},
		{name: "JPEG", filename: "photo.JPEG", want: true},
		{name: "PNG", filename: "screenshot.png", want: true},
		{name: "HEIC", filename: "IMG_5678.heic", want: true},
		{name: "RAW", filename: "DSC_9876.raw", want: true},
		{name: "CR2", filename: "IMG_0001.CR2", want: true},
		{name: "text file", filename: "readme.txt", want: false},
		{name: "video file", filename: "movie.mp4", want: false},
		{name: "no extension", filename: "noextension", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isImageFile(tt.filename); got != tt.want {
				t.Errorf("isImageFile() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFormatSize(t *testing.T) {
	tests := []struct {
		name  string
		bytes int64
		want  string
	}{
		{name: "bytes", bytes: 512, want: "512 B"},
		{name: "kilobytes", bytes: 2048, want: "2.00 KB"},
		{name: "megabytes", bytes: 5242880, want: "5.00 MB"},
		{name: "gigabytes", bytes: 3221225472, want: "3.00 GB"},
		{name: "terabytes", bytes: 1099511627776, want: "1.00 TB"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatSize(tt.bytes); got != tt.want {
				t.Errorf("formatSize() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetCachePath(t *testing.T) {
	m := &Manager{
		cacheDir: "/tmp/test-cache",
	}

	// Test that cache paths are consistent
	path1 := m.getCachePath("card-12345678/DCIM/IMG_1234.JPG")
	path2 := m.getCachePath("card-12345678/DCIM/IMG_1234.JPG")

	if path1 != path2 {
		t.Errorf("getCachePath() not consistent: %v != %v", path1, path2)
	}

	// Test that different paths get different cache paths
	path3 := m.getCachePath("card-87654321/DCIM/IMG_5678.JPG")
	if path1 == path3 {
		t.Errorf("getCachePath() collision: %v == %v", path1, path3)
	}

	// Test that extension is preserved
	if path1[len(path1)-4:] != ".JPG" {
		t.Errorf("getCachePath() extension not preserved: %v", path1)
	}
}
