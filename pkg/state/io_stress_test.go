//go:build stress

package state

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// BenchmarkFileWrite benchmarks raw file write performance
func BenchmarkFileWrite(b *testing.B) {
	tmpDir := b.TempDir()
	data := make([]byte, 1024) // 1KB

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		path := filepath.Join(tmpDir, fmt.Sprintf("file-%d.dat", i))
		if err := os.WriteFile(path, data, 0644); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkFileRead benchmarks raw file read performance
func BenchmarkFileRead(b *testing.B) {
	tmpDir := b.TempDir()
	data := make([]byte, 1024)
	path := filepath.Join(tmpDir, "test.dat")

	if err := os.WriteFile(path, data, 0644); err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := os.ReadFile(path)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkAtomicWrite benchmarks atomic file writes
func BenchmarkAtomicWrite(b *testing.B) {
	tmpDir := b.TempDir()
	data := make([]byte, 1024)
	path := filepath.Join(tmpDir, "test.dat")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tmpPath := path + ".tmp"
		if err := os.WriteFile(tmpPath, data, 0644); err != nil {
			b.Fatal(err)
		}
		if err := os.Rename(tmpPath, path); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkConcurrentFileWrites benchmarks concurrent file writes
func BenchmarkConcurrentFileWrites(b *testing.B) {
	tmpDir := b.TempDir()
	data := make([]byte, 1024)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			path := filepath.Join(tmpDir, fmt.Sprintf("file-%d.dat", i))
			if err := os.WriteFile(path, data, 0644); err != nil {
				b.Fatal(err)
			}
			i++
		}
	})
}

// BenchmarkStateSaveLoad benchmarks state persistence operations
func BenchmarkStateSaveLoad(b *testing.B) {
	tmpDir := b.TempDir()
	oldStateDir := stateDir
	stateDir = tmpDir
	defer func() { stateDir = oldStateDir }()

	m, err := NewManager()
	if err != nil {
		b.Fatal(err)
	}

	// Set up some state
	_, err = m.StartSync("card-test", 1000, 100*1024*1024)
	if err != nil {
		b.Fatal(err)
	}

	b.Run("save", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			m.mu.RLock()
			err := m.saveLocked()
			m.mu.RUnlock()
			if err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("load", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if err := m.loadState(); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("save_and_load", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			m.mu.Lock()
			if err := m.saveLocked(); err != nil {
				m.mu.Unlock()
				b.Fatal(err)
			}
			m.mu.Unlock()

			if err := m.loadState(); err != nil {
				b.Fatal(err)
			}
		}
	})
}

// BenchmarkHistorySaveLoad benchmarks history persistence
func BenchmarkHistorySaveLoad(b *testing.B) {
	historySizes := []int{10, 100, 1000}

	for _, size := range historySizes {
		b.Run(fmt.Sprintf("%d_records", size), func(b *testing.B) {
			tmpDir := b.TempDir()
			oldStateDir := stateDir
			stateDir = tmpDir
			defer func() { stateDir = oldStateDir }()

			m, err := NewManager()
			if err != nil {
				b.Fatal(err)
			}

			// Create history
			for i := 0; i < size; i++ {
				_, err := m.StartSync(fmt.Sprintf("card-%d", i), 1000, 100*1024*1024)
				if err != nil {
					b.Fatal(err)
				}
				if err := m.FinishSync(true, nil); err != nil {
					b.Fatal(err)
				}
			}

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				m.mu.RLock()
				err := m.saveHistory()
				m.mu.RUnlock()
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// TestConcurrentFileAccessStress tests concurrent file I/O safety under stress
func TestConcurrentFileAccessStress(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping concurrent file access test in short mode")
	}

	tmpDir := t.TempDir()
	oldStateDir := stateDir
	stateDir = tmpDir
	defer func() { stateDir = oldStateDir }()

	m, err := NewManager()
	if err != nil {
		t.Fatal(err)
	}

	const (
		writers = 50
		readers = 50
		ops     = 100
	)

	var wg sync.WaitGroup
	errors := make(chan error, (writers+readers)*ops)
	start := time.Now()

	// Writers
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < ops; j++ {
				if err := m.SetStatus(StatusSyncing); err != nil {
					errors <- fmt.Errorf("writer %d: %w", id, err)
				}
			}
		}(i)
	}

	// Readers
	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < ops; j++ {
				if err := m.Reload(); err != nil {
					errors <- fmt.Errorf("reader %d: %w", id, err)
				}
			}
		}(i)
	}

	wg.Wait()
	close(errors)
	elapsed := time.Since(start)

	errorCount := 0
	for err := range errors {
		t.Error(err)
		errorCount++
	}

	totalOps := (writers + readers) * ops
	opsPerSec := float64(totalOps) / elapsed.Seconds()

	t.Logf("Concurrent File Access Test:")
	t.Logf("  Writers: %d", writers)
	t.Logf("  Readers: %d", readers)
	t.Logf("  Operations per goroutine: %d", ops)
	t.Logf("  Total operations: %d", totalOps)
	t.Logf("  Duration: %v", elapsed)
	t.Logf("  Operations/sec: %.2f", opsPerSec)
	t.Logf("  Errors: %d", errorCount)

	if errorCount > 0 {
		t.Errorf("Test had %d errors", errorCount)
	}
}

// TestFileCorruptionDetection tests detection of corrupted files
func TestFileCorruptionDetection(t *testing.T) {
	tmpDir := t.TempDir()
	oldStateDir := stateDir
	stateDir = tmpDir
	defer func() { stateDir = oldStateDir }()

	m, err := NewManager()
	if err != nil {
		t.Fatal(err)
	}

	// Save valid state
	if err := m.SetStatus(StatusSyncing); err != nil {
		t.Fatal(err)
	}

	// Corrupt the state file
	if err := os.WriteFile(StateFile, []byte("corrupted data {{{"), 0644); err != nil {
		t.Fatal(err)
	}

	// Try to reload - should handle gracefully
	err = m.Reload()
	if err == nil {
		t.Error("expected error when loading corrupted file, got nil")
	}
}

// TestPartialWriteRecovery tests recovery from partial writes
func TestPartialWriteRecovery(t *testing.T) {
	tmpDir := t.TempDir()
	oldStateDir := stateDir
	stateDir = tmpDir
	defer func() { stateDir = oldStateDir }()

	m, err := NewManager()
	if err != nil {
		t.Fatal(err)
	}

	// Set initial state
	if err := m.SetStatus(StatusSyncing); err != nil {
		t.Fatal(err)
	}

	originalState := m.GetState()

	// Simulate partial write by creating .tmp file
	tmpFile := StateFile + ".tmp"
	if err := os.WriteFile(tmpFile, []byte("partial"), 0644); err != nil {
		t.Fatal(err)
	}

	// Reload should use the complete file, not the partial tmp
	if err := m.Reload(); err != nil {
		t.Fatal(err)
	}

	reloadedState := m.GetState()
	if reloadedState.Status != originalState.Status {
		t.Errorf("expected status %s, got %s", originalState.Status, reloadedState.Status)
	}
}

// BenchmarkDiskSync benchmarks sync/fsync operations
func BenchmarkDiskSync(b *testing.B) {
	tmpDir := b.TempDir()
	path := filepath.Join(tmpDir, "test.dat")
	data := make([]byte, 1024)

	b.Run("without_sync", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if err := os.WriteFile(path, data, 0644); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("with_sync", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			f, err := os.Create(path)
			if err != nil {
				b.Fatal(err)
			}
			if _, err := f.Write(data); err != nil {
				f.Close()
				b.Fatal(err)
			}
			if err := f.Sync(); err != nil {
				f.Close()
				b.Fatal(err)
			}
			f.Close()
		}
	})
}

// TestDiskFullHandling tests behavior when disk is full
func TestDiskFullHandling(t *testing.T) {
	// Skip when running as root - root bypasses file permission checks
	if os.Getuid() == 0 {
		t.Skip("Skipping disk full handling test when running as root")
	}

	// This test is difficult to simulate reliably without actually filling a disk
	// Instead, we test that errors are properly propagated

	tmpDir := t.TempDir()
	oldStateDir := stateDir
	oldStateFile := StateFile
	oldHistoryFile := HistoryFile
	stateDir = tmpDir
	StateFile = filepath.Join(tmpDir, "state.json")
	HistoryFile = filepath.Join(tmpDir, "sync-history.json")
	defer func() {
		stateDir = oldStateDir
		StateFile = oldStateFile
		HistoryFile = oldHistoryFile
	}()

	m, err := NewManager()
	if err != nil {
		t.Fatal(err)
	}

	// Make state directory read-only to prevent temp file creation
	if err := os.Chmod(tmpDir, 0555); err != nil {
		t.Skip("Cannot change directory permissions")
	}
	defer os.Chmod(tmpDir, 0755)

	// Attempt to write - should fail because atomic write creates a temp file
	err = m.SetStatus(StatusSyncing)
	if err == nil {
		t.Error("expected error when writing to read-only directory, got nil")
	}
}

// BenchmarkLargeStateFiles benchmarks operations with large state files
func BenchmarkLargeStateFiles(b *testing.B) {
	tmpDir := b.TempDir()
	oldStateDir := stateDir
	stateDir = tmpDir
	defer func() { stateDir = oldStateDir }()

	m, err := NewManager()
	if err != nil {
		b.Fatal(err)
	}

	// Create large state with many history records
	for i := 0; i < 10000; i++ {
		_, err := m.StartSync(fmt.Sprintf("card-%d", i), 1000, 100*1024*1024)
		if err != nil {
			b.Fatal(err)
		}
		if err := m.FinishSync(true, nil); err != nil {
			b.Fatal(err)
		}
	}

	fileInfo, err := os.Stat(HistoryFile)
	if err != nil {
		b.Fatal(err)
	}
	b.Logf("History file size: %.2f MB", float64(fileInfo.Size())/(1024*1024))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := m.loadHistory(); err != nil {
			b.Fatal(err)
		}
	}
}

// TestFilePermissions tests file permission handling
func TestFilePermissions(t *testing.T) {
	tmpDir := t.TempDir()
	oldStateDir := stateDir
	stateDir = tmpDir
	defer func() { stateDir = oldStateDir }()

	m, err := NewManager()
	if err != nil {
		t.Fatal(err)
	}

	// Save state
	if err := m.SetStatus(StatusSyncing); err != nil {
		t.Fatal(err)
	}

	// Check file permissions
	fileInfo, err := os.Stat(StateFile)
	if err != nil {
		t.Fatal(err)
	}

	perm := fileInfo.Mode().Perm()
	expectedPerm := os.FileMode(0644)

	if perm != expectedPerm {
		t.Errorf("expected permissions %o, got %o", expectedPerm, perm)
	}
}

// TestStressFileOperations stress tests file I/O under extreme load
func TestStressFileOperations(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress test in short mode")
	}

	tmpDir := t.TempDir()
	oldStateDir := stateDir
	stateDir = tmpDir
	defer func() { stateDir = oldStateDir }()

	m, err := NewManager()
	if err != nil {
		t.Fatal(err)
	}

	const (
		duration    = 500 * time.Millisecond
		concurrency = 20
	)

	var writeCount, readCount, errorCount atomic.Int64
	stopChan := make(chan struct{})

	start := time.Now()
	var wg sync.WaitGroup

	// Spawn workers
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			for {
				select {
				case <-stopChan:
					return
				default:
					// Alternate between reads and writes
					if id%2 == 0 {
						err := m.SetStatus(StatusSyncing)
						if err != nil {
							errorCount.Add(1)
						} else {
							writeCount.Add(1)
						}
					} else {
						err := m.Reload()
						if err != nil {
							errorCount.Add(1)
						} else {
							readCount.Add(1)
						}
					}
				}
			}
		}(i)
	}

	// Run for specified duration
	time.Sleep(duration)
	close(stopChan)
	wg.Wait()

	elapsed := time.Since(start)
	writes := writeCount.Load()
	reads := readCount.Load()
	errors := errorCount.Load()
	totalOps := writes + reads

	t.Logf("File I/O Stress Test Results:")
	t.Logf("  Concurrency: %d", concurrency)
	t.Logf("  Duration: %v", elapsed)
	t.Logf("  Write operations: %d", writes)
	t.Logf("  Read operations: %d", reads)
	t.Logf("  Total operations: %d", totalOps)
	t.Logf("  Errors: %d", errors)
	t.Logf("  Operations/sec: %.2f", float64(totalOps)/elapsed.Seconds())
	t.Logf("  Error rate: %.2f%%", float64(errors)/float64(totalOps+errors)*100)

	if errors > int64(float64(totalOps)*0.01) { // Allow up to 1% error rate
		t.Errorf("Stress test error rate too high: %d errors", errors)
	}
}

// BenchmarkDirectoryCreation benchmarks directory creation overhead
func BenchmarkDirectoryCreation(b *testing.B) {
	tmpDir := b.TempDir()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dir := filepath.Join(tmpDir, fmt.Sprintf("dir-%d", i))
		if err := os.MkdirAll(dir, 0755); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkFileExists benchmarks file existence checks
func BenchmarkFileExists(b *testing.B) {
	tmpDir := b.TempDir()
	existingFile := filepath.Join(tmpDir, "exists.dat")
	nonExistentFile := filepath.Join(tmpDir, "not-exists.dat")

	if err := os.WriteFile(existingFile, []byte("test"), 0644); err != nil {
		b.Fatal(err)
	}

	b.Run("exists", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, err := os.Stat(existingFile)
			if err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("not_exists", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, err := os.Stat(nonExistentFile)
			if err == nil {
				b.Fatal("expected error for non-existent file")
			}
		}
	})
}

// BenchmarkJSONMarshalUnmarshal benchmarks JSON serialization overhead
func BenchmarkJSONMarshalUnmarshal(b *testing.B) {
	state := CurrentState{
		Status: StatusSyncing,
		CurrentSync: &SyncRecord{
			ID:              "test-123",
			StartTime:       time.Now(),
			EndTime:         time.Now().Add(5 * time.Minute),
			Status:          "syncing",
			FilesTotal:      10000,
			FilesSynced:     5000,
			BytesTotal:      1000 * 1024 * 1024,
			BytesSynced:     500 * 1024 * 1024,
			CardID:          "card-test",
			CurrentFile:     "IMG_1234.JPG",
			CurrentFileSize: 5 * 1024 * 1024,
			TransferSpeed:   1.5 * 1024 * 1024,
			ETA:             "5m30s",
		},
		SDCardMounted: true,
		SDCardPath:    "/perm/pictures-sync/mounts/sdcard",
	}

	b.Run("marshal", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			_, err := marshalState(&state)
			if err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("unmarshal", func(b *testing.B) {
		data, err := marshalState(&state)
		if err != nil {
			b.Fatal(err)
		}

		b.ReportAllocs()
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			var s CurrentState
			if err := unmarshalState(data, &s); err != nil {
				b.Fatal(err)
			}
		}
	})
}
