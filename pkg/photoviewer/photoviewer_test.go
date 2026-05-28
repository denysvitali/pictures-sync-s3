package photoviewer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// setupTestDCIM creates a test DCIM directory structure
func setupTestDCIM(t *testing.T) string {
	tmpDir := t.TempDir()
	dcimDir := filepath.Join(tmpDir, "DCIM")

	// Create directory structure
	dirs := []string{
		filepath.Join(dcimDir, "100CANON"),
		filepath.Join(dcimDir, "101NIKON"),
		filepath.Join(dcimDir, "MISC"),
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("Failed to create test directory %s: %v", dir, err)
		}
	}

	// Create test files
	testFiles := map[string]string{
		filepath.Join(dcimDir, "100CANON", "IMG_0001.JPG"):  "fake jpeg data",
		filepath.Join(dcimDir, "100CANON", "IMG_0002.JPG"):  "fake jpeg data",
		filepath.Join(dcimDir, "100CANON", "IMG_0003.CR2"):  "fake raw data",
		filepath.Join(dcimDir, "101NIKON", "DSC_0001.NEF"):  "fake raw data",
		filepath.Join(dcimDir, "101NIKON", "DSC_0002.JPG"):  "fake jpeg data",
		filepath.Join(dcimDir, "101NIKON", "VID_0001.MP4"):  "fake video data",
		filepath.Join(dcimDir, "MISC", "photo.png"):         "fake png data",
		filepath.Join(dcimDir, "MISC", "readme.txt"):        "not a photo",
	}

	for path, content := range testFiles {
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to create test file %s: %v", path, err)
		}
	}

	return tmpDir
}

func TestIsImageFile(t *testing.T) {
	tests := []struct {
		ext      string
		expected bool
	}{
		{".jpg", true},
		{".JPG", true},
		{".jpeg", true},
		{".png", true},
		{".gif", true},
		{".raw", true},
		{".cr2", true},
		{".nef", true},
		{".arw", true},
		{".txt", false},
		{".mp4", false},
		{".doc", false},
	}

	for _, tt := range tests {
		t.Run(tt.ext, func(t *testing.T) {
			result := IsImageFile(tt.ext)
			if result != tt.expected {
				t.Errorf("IsImageFile(%s) = %v, want %v", tt.ext, result, tt.expected)
			}
		})
	}
}

func TestIsVideoFile(t *testing.T) {
	tests := []struct {
		ext      string
		expected bool
	}{
		{".mp4", true},
		{".MP4", true},
		{".mov", true},
		{".avi", true},
		{".mkv", true},
		{".jpg", false},
		{".txt", false},
	}

	for _, tt := range tests {
		t.Run(tt.ext, func(t *testing.T) {
			result := IsVideoFile(tt.ext)
			if result != tt.expected {
				t.Errorf("IsVideoFile(%s) = %v, want %v", tt.ext, result, tt.expected)
			}
		})
	}
}

func TestIsMediaFile(t *testing.T) {
	tests := []struct {
		ext      string
		expected bool
	}{
		{".jpg", true},
		{".mp4", true},
		{".raw", true},
		{".nef", true},
		{".txt", false},
		{".pdf", false},
	}

	for _, tt := range tests {
		t.Run(tt.ext, func(t *testing.T) {
			result := IsMediaFile(tt.ext)
			if result != tt.expected {
				t.Errorf("IsMediaFile(%s) = %v, want %v", tt.ext, result, tt.expected)
			}
		})
	}
}

func TestScanDCIM(t *testing.T) {
	mountPath := setupTestDCIM(t)

	result, err := ScanDCIM(mountPath)
	if err != nil {
		t.Fatalf("ScanDCIM failed: %v", err)
	}

	// Should find 7 media files (excluding readme.txt)
	expectedCount := 7
	if result.TotalCount != expectedCount {
		t.Errorf("Expected %d files, got %d", expectedCount, result.TotalCount)
	}

	if len(result.Photos) != expectedCount {
		t.Errorf("Expected %d photos in result, got %d", expectedCount, len(result.Photos))
	}

	// Check that we have the expected directories
	expectedDirs := 3 // 100CANON, 101NIKON, MISC
	if len(result.Directories) != expectedDirs {
		t.Errorf("Expected %d directories, got %d", expectedDirs, len(result.Directories))
	}

	// Verify total size is calculated
	if result.TotalSize == 0 {
		t.Error("Expected TotalSize to be > 0")
	}

	// Check file types
	var jpgCount, rawCount, videoCount int
	for _, photo := range result.Photos {
		if photo.IsVideo {
			videoCount++
		} else if photo.IsRAW {
			rawCount++
		} else if photo.IsImage {
			jpgCount++
		}
	}

	if jpgCount != 4 { // IMG_0001.JPG, IMG_0002.JPG, DSC_0002.JPG, photo.png
		t.Errorf("Expected 4 standard images, got %d", jpgCount)
	}
	if rawCount != 2 { // IMG_0003.CR2, DSC_0001.NEF
		t.Errorf("Expected 2 RAW files, got %d", rawCount)
	}
	if videoCount != 1 { // VID_0001.MP4
		t.Errorf("Expected 1 video, got %d", videoCount)
	}
}

func TestScanDCIM_NoDirectory(t *testing.T) {
	tmpDir := t.TempDir()

	_, err := ScanDCIM(tmpDir)
	if err == nil {
		t.Error("Expected error when DCIM directory doesn't exist")
	}
}

func TestScanDirectory(t *testing.T) {
	mountPath := setupTestDCIM(t)

	// Scan the 100CANON directory
	result, err := ScanDirectory(mountPath, "100CANON")
	if err != nil {
		t.Fatalf("ScanDirectory failed: %v", err)
	}

	// Should find 3 files in 100CANON
	expectedCount := 3
	if result.TotalCount != expectedCount {
		t.Errorf("Expected %d files, got %d", expectedCount, result.TotalCount)
	}

	// Verify files are from correct directory
	for _, photo := range result.Photos {
		if !filepath.HasPrefix(photo.Path, "100CANON") {
			t.Errorf("Expected file path to start with '100CANON', got %s", photo.Path)
		}
	}
}

func TestScanDirectory_Root(t *testing.T) {
	mountPath := setupTestDCIM(t)

	// Scan root (current directory)
	result, err := ScanDirectory(mountPath, ".")
	if err != nil {
		t.Fatalf("ScanDirectory failed: %v", err)
	}

	// Root should have no files, only subdirectories
	if result.TotalCount != 0 {
		t.Errorf("Expected 0 files in root, got %d", result.TotalCount)
	}

	// Should list subdirectories
	if len(result.Directories) == 0 {
		t.Error("Expected to find subdirectories in root")
	}
}

func TestGetPhotoByPath(t *testing.T) {
	mountPath := setupTestDCIM(t)

	photo, err := GetPhotoByPath(mountPath, "100CANON/IMG_0001.JPG")
	if err != nil {
		t.Fatalf("GetPhotoByPath failed: %v", err)
	}

	if photo.Name != "IMG_0001.JPG" {
		t.Errorf("Expected name 'IMG_0001.JPG', got %s", photo.Name)
	}

	if !photo.IsImage {
		t.Error("Expected IsImage to be true")
	}

	if photo.IsVideo {
		t.Error("Expected IsVideo to be false")
	}

	if photo.MimeType != "image/jpeg" {
		t.Errorf("Expected MIME type 'image/jpeg', got %s", photo.MimeType)
	}
}

func TestGetPhotoByPath_NotFound(t *testing.T) {
	mountPath := setupTestDCIM(t)

	_, err := GetPhotoByPath(mountPath, "nonexistent.jpg")
	if err == nil {
		t.Error("Expected error for non-existent file")
	}
}

func TestGetPhotoByPath_DirectoryTraversal(t *testing.T) {
	mountPath := setupTestDCIM(t)

	// Try to access file outside DCIM
	// Note: path traversal is sanitized, so "../../etc/passwd" becomes "etc/passwd"
	// which is inside DCIM and safe, but the file won't exist
	_, err := GetPhotoByPath(mountPath, "../../etc/passwd")
	if err == nil {
		t.Error("Expected error (file not found)")
	}

	// The error should be "file not found" because the sanitized path
	// (etc/passwd inside DCIM) doesn't exist, not "access denied"
	if err != nil && !strings.Contains(err.Error(), "not found") && !strings.Contains(err.Error(), "no such file") {
		t.Errorf("Expected 'not found' error, got: %v", err)
	}
}

func TestValidatePath(t *testing.T) {
	mountPath := "/mnt/sdcard"

	tests := []struct {
		name         string
		relativePath string
		shouldError  bool
		expectedPath string // expected resolved path
	}{
		{"valid path", "100CANON/IMG_0001.JPG", false, "/mnt/sdcard/DCIM/100CANON/IMG_0001.JPG"},
		{"valid subdirectory", "MISC/photo.png", false, "/mnt/sdcard/DCIM/MISC/photo.png"},
		{"root file", "test.jpg", false, "/mnt/sdcard/DCIM/test.jpg"},
		// Path traversal attempts are sanitized by filepath.Clean()
		// "../etc/passwd" becomes "etc/passwd" which is INSIDE DCIM and safe
		{"directory traversal sanitized", "../etc/passwd", false, "/mnt/sdcard/DCIM/etc/passwd"},
		{"absolute path sanitized", "/etc/passwd", false, "/mnt/sdcard/DCIM/etc/passwd"},
		{"complex traversal sanitized", "subdir/../../etc/passwd", false, "/mnt/sdcard/DCIM/etc/passwd"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ValidatePath(mountPath, tt.relativePath)
			if tt.shouldError && err == nil {
				t.Error("Expected error but got none")
			}
			if !tt.shouldError && err != nil {
				t.Errorf("Expected no error but got: %v", err)
			}
			if !tt.shouldError && result != tt.expectedPath {
				t.Errorf("Expected path %s, got %s", tt.expectedPath, result)
			}
		})
	}
}


func TestPhotoFilter(t *testing.T) {
	now := time.Now()
	yesterday := now.Add(-24 * time.Hour)
	tomorrow := now.Add(24 * time.Hour)

	photos := []PhotoInfo{
		{Name: "small.jpg", Size: 100, ModTime: yesterday, IsImage: true, IsRAW: false},
		{Name: "large.jpg", Size: 10000, ModTime: now, IsImage: true, IsRAW: false},
		{Name: "raw.cr2", Size: 5000, ModTime: now, IsImage: true, IsRAW: true},
		{Name: "video.mp4", Size: 20000, ModTime: tomorrow, IsVideo: true},
	}

	t.Run("min size filter", func(t *testing.T) {
		filter := &PhotoFilter{MinSize: 1000}
		filtered := filter.Filter(photos)
		if len(filtered) != 3 {
			t.Errorf("Expected 3 photos, got %d", len(filtered))
		}
	})

	t.Run("max size filter", func(t *testing.T) {
		filter := &PhotoFilter{MaxSize: 5000}
		filtered := filter.Filter(photos)
		if len(filtered) != 2 {
			t.Errorf("Expected 2 photos, got %d", len(filtered))
		}
	})

	t.Run("time filter - after", func(t *testing.T) {
		filter := &PhotoFilter{After: yesterday.Add(1 * time.Hour)}
		filtered := filter.Filter(photos)
		if len(filtered) != 3 {
			t.Errorf("Expected 3 photos, got %d", len(filtered))
		}
	})

	t.Run("time filter - before", func(t *testing.T) {
		filter := &PhotoFilter{Before: now.Add(1 * time.Hour)}
		filtered := filter.Filter(photos)
		if len(filtered) != 3 {
			t.Errorf("Expected 3 photos, got %d", len(filtered))
		}
	})

	t.Run("only RAW filter", func(t *testing.T) {
		filter := &PhotoFilter{OnlyRAW: true}
		filtered := filter.Filter(photos)
		if len(filtered) != 1 {
			t.Errorf("Expected 1 RAW photo, got %d", len(filtered))
		}
		if filtered[0].Name != "raw.cr2" {
			t.Errorf("Expected raw.cr2, got %s", filtered[0].Name)
		}
	})

	t.Run("only video filter", func(t *testing.T) {
		filter := &PhotoFilter{OnlyVideo: true}
		filtered := filter.Filter(photos)
		if len(filtered) != 1 {
			t.Errorf("Expected 1 video, got %d", len(filtered))
		}
		if filtered[0].Name != "video.mp4" {
			t.Errorf("Expected video.mp4, got %s", filtered[0].Name)
		}
	})

	t.Run("only image filter (excludes RAW and video)", func(t *testing.T) {
		filter := &PhotoFilter{OnlyImage: true}
		filtered := filter.Filter(photos)
		if len(filtered) != 2 {
			t.Errorf("Expected 2 standard images, got %d", len(filtered))
		}
	})

	t.Run("combined filters", func(t *testing.T) {
		filter := &PhotoFilter{
			MinSize: 1000,
			MaxSize: 15000,
			OnlyImage: true,
		}
		filtered := filter.Filter(photos)
		if len(filtered) != 1 {
			t.Errorf("Expected 1 photo matching all criteria, got %d", len(filtered))
		}
		if filtered[0].Name != "large.jpg" {
			t.Errorf("Expected large.jpg, got %s", filtered[0].Name)
		}
	})
}

func TestPhotoInfo_Fields(t *testing.T) {
	mountPath := setupTestDCIM(t)

	photo, err := GetPhotoByPath(mountPath, "100CANON/IMG_0001.JPG")
	if err != nil {
		t.Fatalf("Failed to get photo: %v", err)
	}

	// Verify all required fields are populated
	if photo.Name == "" {
		t.Error("Name should not be empty")
	}
	if photo.Path == "" {
		t.Error("Path should not be empty")
	}
	if photo.AbsolutePath == "" {
		t.Error("AbsolutePath should not be empty")
	}
	if photo.Size == 0 {
		t.Error("Size should not be 0")
	}
	if photo.SizeHuman == "" {
		t.Error("SizeHuman should not be empty")
	}
	if photo.ModTime.IsZero() {
		t.Error("ModTime should not be zero")
	}
	if photo.MimeType == "" {
		t.Error("MimeType should not be empty")
	}
	if photo.Extension == "" {
		t.Error("Extension should not be empty")
	}
}
