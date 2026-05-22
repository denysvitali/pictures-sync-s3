package sdmonitor

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// BenchmarkCountPhotos benchmarks the photo counting operation with various file counts
func BenchmarkCountPhotos(b *testing.B) {
	testCases := []struct {
		name      string
		fileCount int
	}{
		{"10_files", 10},
		{"100_files", 100},
		{"1000_files", 1000},
		{"5000_files", 5000},
	}

	for _, tc := range testCases {
		b.Run(tc.name, func(b *testing.B) {
			tmpDir := b.TempDir()
			dcimPath := filepath.Join(tmpDir, "DCIM", "100CANON")
			if err := os.MkdirAll(dcimPath, 0755); err != nil {
				b.Fatal(err)
			}

			// Create test files
			for i := 0; i < tc.fileCount; i++ {
				filename := filepath.Join(dcimPath, fmt.Sprintf("IMG_%04d.JPG", i))
				if err := os.WriteFile(filename, []byte("test"), 0644); err != nil {
					b.Fatal(err)
				}
			}

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				count, _, err := CountPhotos(tmpDir)
				if err != nil {
					b.Fatal(err)
				}
				if count != tc.fileCount {
					b.Fatalf("expected %d files, got %d", tc.fileCount, count)
				}
			}
		})
	}
}

// BenchmarkCountPhotosMemory measures memory allocations during photo counting
func BenchmarkCountPhotosMemory(b *testing.B) {
	tmpDir := b.TempDir()
	dcimPath := filepath.Join(tmpDir, "DCIM", "100CANON")
	if err := os.MkdirAll(dcimPath, 0755); err != nil {
		b.Fatal(err)
	}

	// Create 1000 test files
	for i := 0; i < 1000; i++ {
		filename := filepath.Join(dcimPath, fmt.Sprintf("IMG_%04d.JPG", i))
		if err := os.WriteFile(filename, []byte("test"), 0644); err != nil {
			b.Fatal(err)
		}
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, _, err := CountPhotos(tmpDir)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkCountPhotosParallel benchmarks photo counting under parallel load
func BenchmarkCountPhotosParallel(b *testing.B) {
	tmpDir := b.TempDir()
	dcimPath := filepath.Join(tmpDir, "DCIM", "100CANON")
	if err := os.MkdirAll(dcimPath, 0755); err != nil {
		b.Fatal(err)
	}

	// Create 500 test files
	for i := 0; i < 500; i++ {
		filename := filepath.Join(dcimPath, fmt.Sprintf("IMG_%04d.JPG", i))
		if err := os.WriteFile(filename, []byte("test"), 0644); err != nil {
			b.Fatal(err)
		}
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			count, _, err := CountPhotos(tmpDir)
			if err != nil {
				b.Fatal(err)
			}
			if count != 500 {
				b.Fatalf("expected 500 files, got %d", count)
			}
		}
	})
}

// BenchmarkCountPhotosNestedDirs benchmarks counting with nested directory structures
func BenchmarkCountPhotosNestedDirs(b *testing.B) {
	tmpDir := b.TempDir()

	// Create multiple subdirectories with files
	dirs := []string{"100CANON", "101CANON", "102CANON", "103CANON", "104CANON"}
	totalFiles := 0

	for _, dir := range dirs {
		dcimPath := filepath.Join(tmpDir, "DCIM", dir)
		if err := os.MkdirAll(dcimPath, 0755); err != nil {
			b.Fatal(err)
		}

		// Create 100 files per directory
		for i := 0; i < 100; i++ {
			filename := filepath.Join(dcimPath, fmt.Sprintf("IMG_%04d.JPG", i))
			if err := os.WriteFile(filename, []byte("test"), 0644); err != nil {
				b.Fatal(err)
			}
			totalFiles++
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		count, _, err := CountPhotos(tmpDir)
		if err != nil {
			b.Fatal(err)
		}
		if count != totalFiles {
			b.Fatalf("expected %d files, got %d", totalFiles, count)
		}
	}
}

// BenchmarkCountPhotosMixedFiles benchmarks counting with mixed file types
func BenchmarkCountPhotosMixedFiles(b *testing.B) {
	tmpDir := b.TempDir()
	dcimPath := filepath.Join(tmpDir, "DCIM", "100CANON")
	if err := os.MkdirAll(dcimPath, 0755); err != nil {
		b.Fatal(err)
	}

	// Create mix of files — CountPhotos counts every regular file, matching
	// what rclone uploads.
	const totalCount = 200
	for i := 0; i < totalCount; i++ {
		var filename string
		if i%10 < 7 {
			filename = filepath.Join(dcimPath, fmt.Sprintf("IMG_%04d.JPG", i))
		} else {
			filename = filepath.Join(dcimPath, fmt.Sprintf("file_%04d.txt", i))
		}
		if err := os.WriteFile(filename, []byte("test"), 0644); err != nil {
			b.Fatal(err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		count, _, err := CountPhotos(tmpDir)
		if err != nil {
			b.Fatal(err)
		}
		if count != totalCount {
			b.Fatalf("expected %d files, got %d", totalCount, count)
		}
	}
}

// BenchmarkFindStorageDevice benchmarks device detection
func BenchmarkFindStorageDevice(b *testing.B) {
	// This benchmark measures the overhead of the device detection logic
	// It won't find actual devices in test environment, but measures the search time
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = findStorageDevice("")
	}
}

// BenchmarkMonitorCreation benchmarks monitor initialization
func BenchmarkMonitorCreation(b *testing.B) {
	tmpDir := b.TempDir()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m := NewMonitor(filepath.Join(tmpDir, fmt.Sprintf("mount%d", i)))
		_ = m
	}
}

// BenchmarkMonitorStartStop benchmarks monitor lifecycle
func BenchmarkMonitorStartStop(b *testing.B) {
	tmpDir := b.TempDir()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m := NewMonitor(filepath.Join(tmpDir, fmt.Sprintf("mount%d", i)))
		if err := m.Start(); err != nil {
			b.Fatal(err)
		}
		m.Stop()
	}
}

// BenchmarkGetCachedMounts benchmarks the mount cache mechanism
func BenchmarkGetCachedMounts(b *testing.B) {
	m := NewMonitor(b.TempDir())

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = m.getCachedMounts()
	}
}

// BenchmarkGetCachedMountsParallel benchmarks mount cache under concurrent access
func BenchmarkGetCachedMountsParallel(b *testing.B) {
	m := NewMonitor(b.TempDir())

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _ = m.getCachedMounts()
		}
	})
}
