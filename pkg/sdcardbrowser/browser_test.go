package sdcardbrowser

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestListFilesClassifiesLocalMedia(t *testing.T) {
	mountPath := t.TempDir()
	if err := os.Mkdir(filepath.Join(mountPath, "DCIM"), 0755); err != nil {
		t.Fatalf("mkdir DCIM: %v", err)
	}
	writeTestFile(t, mountPath, "DCIM/photo.JPG", "jpg")
	writeTestFile(t, mountPath, "DCIM/video.MP4", "mp4")
	writeTestFile(t, mountPath, "DCIM/raw.CR3", "raw")

	result, err := ListFiles(mountPath, "DCIM")
	if err != nil {
		t.Fatalf("ListFiles: %v", err)
	}

	files := map[string]FileInfo{}
	for _, file := range result.Files {
		files[file.Name] = file
	}
	if !files["photo.JPG"].IsImage {
		t.Fatal("photo.JPG should be classified as image")
	}
	if !files["video.MP4"].IsVideo {
		t.Fatal("video.MP4 should be classified as video")
	}
	if !files["raw.CR3"].IsRAW {
		t.Fatal("raw.CR3 should be classified as RAW")
	}
}

func TestOpenFileStreamsAnyRegularFile(t *testing.T) {
	mountPath := t.TempDir()
	writeTestFile(t, mountPath, "DCIM/video.MP4", "0123456789")

	file, info, contentType, err := OpenFile(mountPath, "DCIM/video.MP4")
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	defer file.Close()

	if info.Name() != "video.MP4" {
		t.Fatalf("expected video.MP4, got %s", info.Name())
	}
	if contentType != "video/mp4" {
		t.Fatalf("expected video/mp4, got %s", contentType)
	}
	data, err := io.ReadAll(file)
	if err != nil {
		t.Fatalf("read opened file: %v", err)
	}
	if string(data) != "0123456789" {
		t.Fatalf("unexpected data %q", string(data))
	}
}

func TestOpenFileRejectsTraversal(t *testing.T) {
	mountPath := t.TempDir()
	if _, _, _, err := OpenFile(mountPath, "../secret.MP4"); err == nil {
		t.Fatal("expected traversal to be rejected")
	}
}

func writeTestFile(t *testing.T, mountPath, relativePath, contents string) {
	t.Helper()
	fullPath := filepath.Join(mountPath, relativePath)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(fullPath), err)
	}
	if err := os.WriteFile(fullPath, []byte(contents), 0644); err != nil {
		t.Fatalf("write %s: %v", fullPath, err)
	}
}
