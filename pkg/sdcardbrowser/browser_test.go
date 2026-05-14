package sdcardbrowser

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/disintegration/imaging"
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

func TestListFilesDefaultsToMountRoot(t *testing.T) {
	mountPath := t.TempDir()
	if err := os.Mkdir(filepath.Join(mountPath, "PRIVATE"), 0755); err != nil {
		t.Fatalf("mkdir PRIVATE: %v", err)
	}

	result, err := ListFiles(mountPath, "")
	if err != nil {
		t.Fatalf("ListFiles: %v", err)
	}

	if result.Path != "" {
		t.Fatalf("path = %q, want root", result.Path)
	}
	if len(result.Files) != 1 || result.Files[0].Name != "PRIVATE" {
		t.Fatalf("unexpected root listing: %+v", result.Files)
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

func TestReadThumbnailFallsBackWhenNoEXIF(t *testing.T) {
	mountPath := t.TempDir()
	img := image.NewRGBA(image.Rect(0, 0, 400, 300))
	for y := 0; y < 300; y++ {
		for x := 0; x < 400; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 128, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := imaging.Encode(&buf, img, imaging.JPEG, imaging.JPEGQuality(85)); err != nil {
		t.Fatalf("encode JPEG: %v", err)
	}
	writeTestFile(t, mountPath, "DCIM/plain.jpg", buf.String())

	preview, err := ReadThumbnail(mountPath, "DCIM/plain.jpg")
	if err != nil {
		t.Fatalf("ReadThumbnail: %v", err)
	}
	if preview.ContentType != "image/jpeg" {
		t.Fatalf("content type = %q, want image/jpeg", preview.ContentType)
	}
	if len(preview.Data) == 0 || len(preview.Data) >= buf.Len() {
		t.Fatalf("fallback thumbnail size %d not smaller than source %d", len(preview.Data), buf.Len())
	}
}

func TestReadJPEGExifSegmentReadsOnlyAPP1(t *testing.T) {
	exifPayload := []byte("TIFFHEADERANDIFDS")
	var jpeg bytes.Buffer
	jpeg.Write([]byte{0xFF, 0xD8}) // SOI

	// APP0 (JFIF) segment to exercise skipping.
	app0 := []byte("JFIF\x00\x01\x01\x00\x00\x01\x00\x01\x00\x00")
	jpeg.Write([]byte{0xFF, 0xE0})
	var app0Len [2]byte
	binary.BigEndian.PutUint16(app0Len[:], uint16(len(app0)+2))
	jpeg.Write(app0Len[:])
	jpeg.Write(app0)

	// APP1 (EXIF) segment.
	app1 := append([]byte("Exif\x00\x00"), exifPayload...)
	jpeg.Write([]byte{0xFF, 0xE1})
	var app1Len [2]byte
	binary.BigEndian.PutUint16(app1Len[:], uint16(len(app1)+2))
	jpeg.Write(app1Len[:])
	jpeg.Write(app1)

	// Trailer that should never be read — if the parser slurps to EOF this
	// data shows up where it shouldn't.
	jpeg.Write([]byte{0xFF, 0xDA}) // SOS would short-circuit; we put it last.
	jpeg.Write(bytes.Repeat([]byte{0xAA}, 1<<20))

	got, err := readJPEGExifSegment(bytes.NewReader(jpeg.Bytes()))
	if err != nil {
		t.Fatalf("readJPEGExifSegment: %v", err)
	}
	if !bytes.Equal(got, exifPayload) {
		t.Fatalf("payload = %q, want %q", got, exifPayload)
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
