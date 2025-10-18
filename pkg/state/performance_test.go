package state

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// BenchmarkManagerCreation benchmarks state manager creation
func BenchmarkManagerCreation(b *testing.B) {
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		tmpDir := b.TempDir()
		oldStateDir := stateDir
		stateDir = tmpDir
		b.StartTimer()

		m, err := NewManager()
		if err != nil {
			b.Fatal(err)
		}
		_ = m

		b.StopTimer()
		stateDir = oldStateDir
		b.StartTimer()
	}
}

// BenchmarkGetState benchmarks reading current state
func BenchmarkGetState(b *testing.B) {
	tmpDir := b.TempDir()
	oldStateDir := stateDir
	stateDir = tmpDir
	defer func() { stateDir = oldStateDir }()

	m, err := NewManager()
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		state := m.GetState()
		_ = state
	}
}

// BenchmarkGetStateParallel benchmarks concurrent state reads
func BenchmarkGetStateParallel(b *testing.B) {
	tmpDir := b.TempDir()
	oldStateDir := stateDir
	stateDir = tmpDir
	defer func() { stateDir = oldStateDir }()

	m, err := NewManager()
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			state := m.GetState()
			_ = state
		}
	})
}

// BenchmarkSetStatus benchmarks status updates
func BenchmarkSetStatus(b *testing.B) {
	tmpDir := b.TempDir()
	oldStateDir := stateDir
	stateDir = tmpDir
	defer func() { stateDir = oldStateDir }()

	m, err := NewManager()
	if err != nil {
		b.Fatal(err)
	}

	statuses := []SyncStatus{StatusIdle, StatusDetected, StatusSyncing, StatusSuccess, StatusError}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		status := statuses[i%len(statuses)]
		if err := m.SetStatus(status); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkSetStatusMemory measures memory allocations during status updates
func BenchmarkSetStatusMemory(b *testing.B) {
	tmpDir := b.TempDir()
	oldStateDir := stateDir
	stateDir = tmpDir
	defer func() { stateDir = oldStateDir }()

	m, err := NewManager()
	if err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		if err := m.SetStatus(StatusSyncing); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkStartSync benchmarks sync start operations
func BenchmarkStartSync(b *testing.B) {
	tmpDir := b.TempDir()
	oldStateDir := stateDir
	stateDir = tmpDir
	defer func() { stateDir = oldStateDir }()

	m, err := NewManager()
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		// Finish previous sync if any
		if m.currentState.CurrentSync != nil {
			m.FinishSync(true, nil)
		}
		b.StartTimer()

		cardID := fmt.Sprintf("card-%d", i)
		_, err := m.StartSync(cardID, 100, 1024*1024)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkUpdateSyncProgress benchmarks progress updates
func BenchmarkUpdateSyncProgress(b *testing.B) {
	tmpDir := b.TempDir()
	oldStateDir := stateDir
	stateDir = tmpDir
	defer func() { stateDir = oldStateDir }()

	m, err := NewManager()
	if err != nil {
		b.Fatal(err)
	}

	// Start a sync
	_, err = m.StartSync("card-test", 1000, 100*1024*1024)
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err := m.UpdateSyncProgress(
			int64(i%1000),
			int64((i%1000)*100*1024),
			fmt.Sprintf("IMG_%04d.JPG", i),
			100*1024,
			1.5*1024*1024, // 1.5 MB/s
			"5m30s",
		)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkUpdateSyncProgressParallel benchmarks concurrent progress updates
func BenchmarkUpdateSyncProgressParallel(b *testing.B) {
	tmpDir := b.TempDir()
	oldStateDir := stateDir
	stateDir = tmpDir
	defer func() { stateDir = oldStateDir }()

	m, err := NewManager()
	if err != nil {
		b.Fatal(err)
	}

	// Start a sync
	_, err = m.StartSync("card-test", 1000, 100*1024*1024)
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			err := m.UpdateSyncProgress(
				int64(i%1000),
				int64((i%1000)*100*1024),
				fmt.Sprintf("IMG_%04d.JPG", i),
				100*1024,
				1.5*1024*1024,
				"5m30s",
			)
			if err != nil {
				b.Fatal(err)
			}
			i++
		}
	})
}

// BenchmarkFinishSync benchmarks sync completion
func BenchmarkFinishSync(b *testing.B) {
	tmpDir := b.TempDir()
	oldStateDir := stateDir
	stateDir = tmpDir
	defer func() { stateDir = oldStateDir }()

	m, err := NewManager()
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		// Start a new sync
		cardID := fmt.Sprintf("card-%d", i)
		_, err := m.StartSync(cardID, 100, 1024*1024)
		if err != nil {
			b.Fatal(err)
		}
		b.StartTimer()

		if err := m.FinishSync(true, nil); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkGetHistory benchmarks history retrieval
func BenchmarkGetHistory(b *testing.B) {
	tmpDir := b.TempDir()
	oldStateDir := stateDir
	stateDir = tmpDir
	defer func() { stateDir = oldStateDir }()

	m, err := NewManager()
	if err != nil {
		b.Fatal(err)
	}

	// Create some history
	for i := 0; i < 10; i++ {
		cardID := fmt.Sprintf("card-%d", i)
		_, err := m.StartSync(cardID, 100, 1024*1024)
		if err != nil {
			b.Fatal(err)
		}
		if err := m.FinishSync(true, nil); err != nil {
			b.Fatal(err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		history := m.GetHistory()
		if len(history) != 10 {
			b.Fatalf("expected 10 history items, got %d", len(history))
		}
	}
}

// BenchmarkGetHistoryLarge benchmarks history retrieval with large history
func BenchmarkGetHistoryLarge(b *testing.B) {
	tmpDir := b.TempDir()
	oldStateDir := stateDir
	stateDir = tmpDir
	defer func() { stateDir = oldStateDir }()

	m, err := NewManager()
	if err != nil {
		b.Fatal(err)
	}

	// Create large history
	for i := 0; i < 1000; i++ {
		cardID := fmt.Sprintf("card-%d", i)
		_, err := m.StartSync(cardID, 100, 1024*1024)
		if err != nil {
			b.Fatal(err)
		}
		if err := m.FinishSync(true, nil); err != nil {
			b.Fatal(err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		history := m.GetHistory()
		if len(history) != 1000 {
			b.Fatalf("expected 1000 history items, got %d", len(history))
		}
	}
}

// BenchmarkFindLastSyncByCardID benchmarks card ID lookup
func BenchmarkFindLastSyncByCardID(b *testing.B) {
	tmpDir := b.TempDir()
	oldStateDir := stateDir
	stateDir = tmpDir
	defer func() { stateDir = oldStateDir }()

	m, err := NewManager()
	if err != nil {
		b.Fatal(err)
	}

	// Create history with different card IDs
	for i := 0; i < 100; i++ {
		cardID := fmt.Sprintf("card-%d", i)
		_, err := m.StartSync(cardID, 100, 1024*1024)
		if err != nil {
			b.Fatal(err)
		}
		if err := m.FinishSync(true, nil); err != nil {
			b.Fatal(err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cardID := fmt.Sprintf("card-%d", i%100)
		record := m.FindLastSyncByCardID(cardID)
		if record == nil {
			b.Fatalf("expected record for %s, got nil", cardID)
		}
	}
}

// BenchmarkReload benchmarks state reload from disk
func BenchmarkReload(b *testing.B) {
	tmpDir := b.TempDir()
	oldStateDir := stateDir
	stateDir = tmpDir
	defer func() { stateDir = oldStateDir }()

	m, err := NewManager()
	if err != nil {
		b.Fatal(err)
	}

	// Set some state
	if err := m.SetStatus(StatusSyncing); err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := m.Reload(); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkSubscribeUnsubscribe benchmarks notification subscription
func BenchmarkSubscribeUnsubscribe(b *testing.B) {
	tmpDir := b.TempDir()
	oldStateDir := stateDir
	stateDir = tmpDir
	defer func() { stateDir = oldStateDir }()

	m, err := NewManager()
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ch := m.Subscribe()
		m.Unsubscribe(ch)
	}
}

// BenchmarkNotifyListeners benchmarks notification broadcasting
func BenchmarkNotifyListeners(b *testing.B) {
	tmpDir := b.TempDir()
	oldStateDir := stateDir
	stateDir = tmpDir
	defer func() { stateDir = oldStateDir }()

	m, err := NewManager()
	if err != nil {
		b.Fatal(err)
	}

	// Create some subscribers
	const numSubscribers = 10
	channels := make([]chan CurrentState, numSubscribers)
	for i := 0; i < numSubscribers; i++ {
		channels[i] = m.Subscribe()
		// Drain channels in background
		go func(ch chan CurrentState) {
			for range ch {
			}
		}(channels[i])
	}
	defer func() {
		for _, ch := range channels {
			m.Unsubscribe(ch)
		}
	}()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := m.SetStatus(StatusSyncing); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkConcurrentOperations benchmarks mixed concurrent operations
func BenchmarkConcurrentOperations(b *testing.B) {
	tmpDir := b.TempDir()
	oldStateDir := stateDir
	stateDir = tmpDir
	defer func() { stateDir = oldStateDir }()

	m, err := NewManager()
	if err != nil {
		b.Fatal(err)
	}

	// Start a sync for updates
	_, err = m.StartSync("card-concurrent", 1000, 100*1024*1024)
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()

	var wg sync.WaitGroup
	errors := make(chan error, b.N*3)

	for i := 0; i < b.N; i++ {
		// Read state
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = m.GetState()
		}()

		// Update progress
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			err := m.UpdateSyncProgress(
				int64(idx%1000),
				int64((idx%1000)*100*1024),
				fmt.Sprintf("IMG_%04d.JPG", idx),
				100*1024,
				1.5*1024*1024,
				"5m30s",
			)
			if err != nil {
				errors <- err
			}
		}(i)

		// Get history
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = m.GetHistory()
		}()
	}

	wg.Wait()
	close(errors)

	if len(errors) > 0 {
		b.Fatalf("got errors during concurrent operations: %v", <-errors)
	}
}

// BenchmarkPersistenceOverhead benchmarks disk I/O overhead
func BenchmarkPersistenceOverhead(b *testing.B) {
	tmpDir := b.TempDir()
	oldStateDir := stateDir
	stateDir = tmpDir
	defer func() { stateDir = oldStateDir }()

	m, err := NewManager()
	if err != nil {
		b.Fatal(err)
	}

	b.Run("with_persistence", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if err := m.SetStatus(StatusSyncing); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("memory_only", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			m.mu.Lock()
			m.currentState.Status = StatusSyncing
			m.mu.Unlock()
		}
	})
}

// BenchmarkThrottledUpdates benchmarks progress update throttling
func BenchmarkThrottledUpdates(b *testing.B) {
	tmpDir := b.TempDir()
	oldStateDir := stateDir
	stateDir = tmpDir
	defer func() { stateDir = oldStateDir }()

	m, err := NewManager()
	if err != nil {
		b.Fatal(err)
	}

	// Set aggressive throttling
	m.progressSaveDelay = 100 * time.Millisecond

	// Start a sync
	_, err = m.StartSync("card-throttle", 1000, 100*1024*1024)
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err := m.UpdateSyncProgress(
			int64(i%1000),
			int64((i%1000)*100*1024),
			fmt.Sprintf("IMG_%04d.JPG", i),
			100*1024,
			1.5*1024*1024,
			"5m30s",
		)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkFilePersistence benchmarks raw file operations
func BenchmarkFilePersistence(b *testing.B) {
	tmpDir := b.TempDir()

	state := CurrentState{
		Status: StatusSyncing,
		CurrentSync: &SyncRecord{
			ID:          "test-123",
			StartTime:   time.Now(),
			Status:      "syncing",
			FilesTotal:  1000,
			FilesSynced: 500,
			BytesTotal:  100 * 1024 * 1024,
			BytesSynced: 50 * 1024 * 1024,
			CardID:      "card-test",
		},
	}

	b.Run("marshal", func(b *testing.B) {
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

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			var s CurrentState
			if err := unmarshalState(data, &s); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("write_file", func(b *testing.B) {
		data, err := marshalState(&state)
		if err != nil {
			b.Fatal(err)
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			path := filepath.Join(tmpDir, fmt.Sprintf("state-%d.json", i))
			if err := os.WriteFile(path, data, 0644); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("atomic_write", func(b *testing.B) {
		data, err := marshalState(&state)
		if err != nil {
			b.Fatal(err)
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			path := filepath.Join(tmpDir, "state-atomic.json")
			tmpPath := path + ".tmp"
			if err := os.WriteFile(tmpPath, data, 0644); err != nil {
				b.Fatal(err)
			}
			if err := os.Rename(tmpPath, path); err != nil {
				b.Fatal(err)
			}
		}
	})
}
