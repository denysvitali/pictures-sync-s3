package state

import (
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"
)

// BenchmarkMemoryUsageIdle measures baseline memory usage
func BenchmarkMemoryUsageIdle(b *testing.B) {
	tmpDir := b.TempDir()
	oldStateDir := stateDir
	stateDir = tmpDir
	defer func() { stateDir = oldStateDir }()

	m, err := NewManager()
	if err != nil {
		b.Fatal(err)
	}

	runtime.GC()
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = m.GetState()
	}
}

// BenchmarkMemoryUsageWithHistory measures memory with large history
func BenchmarkMemoryUsageWithHistory(b *testing.B) {
	historySizes := []int{100, 1000, 10000}

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

			runtime.GC()
			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				history := m.GetHistory()
				if len(history) != size {
					b.Fatalf("expected %d records, got %d", size, len(history))
				}
			}
		})
	}
}

// BenchmarkMemoryUsageSubscribers measures memory with many subscribers
func BenchmarkMemoryUsageSubscribers(b *testing.B) {
	subscriberCounts := []int{10, 100, 1000}

	for _, count := range subscriberCounts {
		b.Run(fmt.Sprintf("%d_subscribers", count), func(b *testing.B) {
			tmpDir := b.TempDir()
			oldStateDir := stateDir
			stateDir = tmpDir
			defer func() { stateDir = oldStateDir }()

			m, err := NewManager()
			if err != nil {
				b.Fatal(err)
			}

			// Create subscribers
			channels := make([]chan CurrentState, count)
			for i := 0; i < count; i++ {
				ch := m.Subscribe()
				channels[i] = ch
				// Drain in background
				go func(c chan CurrentState) {
					for range c {
					}
				}(ch)
			}
			defer func() {
				for _, ch := range channels {
					m.Unsubscribe(ch)
				}
			}()

			runtime.GC()
			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				if err := m.SetStatus(StatusSyncing); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkMemoryLeakDetection tests for memory leaks in long-running operations
func BenchmarkMemoryLeakDetection(b *testing.B) {
	tmpDir := b.TempDir()
	oldStateDir := stateDir
	stateDir = tmpDir
	defer func() { stateDir = oldStateDir }()

	m, err := NewManager()
	if err != nil {
		b.Fatal(err)
	}

	var memStatsBefore, memStatsAfter runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&memStatsBefore)

	b.ResetTimer()

	// Simulate long-running operations
	for i := 0; i < b.N; i++ {
		// Subscribe and unsubscribe
		ch := m.Subscribe()
		m.Unsubscribe(ch)

		// Start and finish sync
		_, err := m.StartSync(fmt.Sprintf("card-%d", i), 100, 1024*1024)
		if err != nil {
			b.Fatal(err)
		}

		// Update progress
		for j := 0; j < 10; j++ {
			m.UpdateSyncProgress(int64(j*10), int64(j*10*1024), "test.jpg", 1024, 1.5*1024*1024, "1m")
		}

		if err := m.FinishSync(true, nil); err != nil {
			b.Fatal(err)
		}
	}

	runtime.GC()
	runtime.ReadMemStats(&memStatsAfter)

	heapGrowth := memStatsAfter.HeapAlloc - memStatsBefore.HeapAlloc
	perOp := heapGrowth / uint64(b.N)

	b.ReportMetric(float64(perOp), "bytes/op")
	b.ReportMetric(float64(heapGrowth)/(1024*1024), "MB_total_growth")
}

// BenchmarkConcurrentMemoryUsage measures memory under concurrent load
func BenchmarkConcurrentMemoryUsage(b *testing.B) {
	tmpDir := b.TempDir()
	oldStateDir := stateDir
	stateDir = tmpDir
	defer func() { stateDir = oldStateDir }()

	m, err := NewManager()
	if err != nil {
		b.Fatal(err)
	}

	// Start a sync for progress updates
	_, err = m.StartSync("card-concurrent", 10000, 1000*1024*1024)
	if err != nil {
		b.Fatal(err)
	}

	runtime.GC()
	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			switch i % 4 {
			case 0:
				_ = m.GetState()
			case 1:
				_ = m.GetHistory()
			case 2:
				m.UpdateSyncProgress(int64(i%10000), int64(i%10000*1024), "test.jpg", 1024, 1.5*1024*1024, "5m")
			case 3:
				_ = m.FindLastSyncByCardID(fmt.Sprintf("card-%d", i%100))
			}
			i++
		}
	})
}

// TestMemoryGrowthPattern tests memory growth over time
func TestMemoryGrowthPattern(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping memory growth test in short mode")
	}

	tmpDir := t.TempDir()
	oldStateDir := stateDir
	stateDir = tmpDir
	defer func() { stateDir = oldStateDir }()

	m, err := NewManager()
	if err != nil {
		t.Fatal(err)
	}

	measurements := make([]uint64, 10)

	for iteration := 0; iteration < 10; iteration++ {
		runtime.GC()
		var ms runtime.MemStats
		runtime.ReadMemStats(&ms)
		measurements[iteration] = ms.HeapAlloc

		// Perform operations
		for i := 0; i < 100; i++ {
			// Subscribe/unsubscribe cycle
			ch := m.Subscribe()
			m.Unsubscribe(ch)

			// Sync cycle
			_, err := m.StartSync(fmt.Sprintf("card-%d", i), 1000, 100*1024*1024)
			if err != nil {
				t.Fatal(err)
			}

			for j := 0; j < 50; j++ {
				m.UpdateSyncProgress(int64(j*20), int64(j*20*1024), "test.jpg", 1024, 1.5*1024*1024, "5m")
			}

			if err := m.FinishSync(true, nil); err != nil {
				t.Fatal(err)
			}
		}

		time.Sleep(100 * time.Millisecond)
	}

	// Analyze growth pattern
	t.Logf("Memory Growth Pattern:")
	for i, mem := range measurements {
		t.Logf("  Iteration %d: %.2f MB", i, float64(mem)/(1024*1024))
	}

	// Check for excessive growth (more than 10x from start to end)
	if measurements[9] > measurements[0]*10 {
		t.Errorf("Excessive memory growth detected: %.2f MB -> %.2f MB",
			float64(measurements[0])/(1024*1024),
			float64(measurements[9])/(1024*1024))
	}
}

// BenchmarkGarbageCollectionImpact measures GC impact
func BenchmarkGarbageCollectionImpact(b *testing.B) {
	tmpDir := b.TempDir()
	oldStateDir := stateDir
	stateDir = tmpDir
	defer func() { stateDir = oldStateDir }()

	m, err := NewManager()
	if err != nil {
		b.Fatal(err)
	}

	// Create some history to increase GC pressure
	for i := 0; i < 1000; i++ {
		_, err := m.StartSync(fmt.Sprintf("card-%d", i), 100, 1024*1024)
		if err != nil {
			b.Fatal(err)
		}
		if err := m.FinishSync(true, nil); err != nil {
			b.Fatal(err)
		}
	}

	b.Run("without_gc", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = m.GetHistory()
		}
	})

	b.Run("with_gc", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = m.GetHistory()
			if i%100 == 0 {
				runtime.GC()
			}
		}
	})
}

// BenchmarkChannelMemoryUsage measures memory usage of notification channels
func BenchmarkChannelMemoryUsage(b *testing.B) {
	tmpDir := b.TempDir()
	oldStateDir := stateDir
	stateDir = tmpDir
	defer func() { stateDir = oldStateDir }()

	m, err := NewManager()
	if err != nil {
		b.Fatal(err)
	}

	b.Run("small_buffer", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			ch := make(chan CurrentState, 10)
			close(ch)
			_ = ch
		}
	})

	b.Run("large_buffer", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			ch := make(chan CurrentState, 1000)
			close(ch)
			_ = ch
		}
	})

	b.Run("subscribe_unsubscribe", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			ch := m.Subscribe()
			m.Unsubscribe(ch)
		}
	})
}

// TestConcurrentMemorySafety tests memory safety under concurrent access
func TestConcurrentMemorySafety(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping concurrent memory safety test in short mode")
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
		goroutines = 100
		iterations = 1000
	)

	var wg sync.WaitGroup
	errors := make(chan error, goroutines*iterations)

	// Start a sync for updates
	_, err = m.StartSync("card-test", 10000, 1000*1024*1024)
	if err != nil {
		t.Fatal(err)
	}

	start := time.Now()

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			for j := 0; j < iterations; j++ {
				switch j % 5 {
				case 0:
					_ = m.GetState()
				case 1:
					_ = m.GetHistory()
				case 2:
					err := m.UpdateSyncProgress(
						int64(j%10000),
						int64(j%10000*1024),
						fmt.Sprintf("test-%d.jpg", j),
						1024,
						1.5*1024*1024,
						"5m",
					)
					if err != nil && err.Error() != "no sync in progress" {
						errors <- err
					}
				case 3:
					ch := m.Subscribe()
					m.Unsubscribe(ch)
				case 4:
					_ = m.FindLastSyncByCardID(fmt.Sprintf("card-%d", j%100))
				}
			}
		}(i)
	}

	wg.Wait()
	close(errors)
	elapsed := time.Since(start)

	if len(errors) > 0 {
		t.Fatalf("got errors during concurrent execution: %v", <-errors)
	}

	opsPerSec := float64(goroutines*iterations) / elapsed.Seconds()
	t.Logf("Concurrent Memory Safety Test:")
	t.Logf("  Goroutines: %d", goroutines)
	t.Logf("  Iterations per goroutine: %d", iterations)
	t.Logf("  Total operations: %d", goroutines*iterations)
	t.Logf("  Duration: %v", elapsed)
	t.Logf("  Operations/sec: %.2f", opsPerSec)
	t.Logf("  Errors: 0")
}

// BenchmarkStructCopyOverhead measures overhead of state copying
func BenchmarkStructCopyOverhead(b *testing.B) {
	state := CurrentState{
		Status: StatusSyncing,
		CurrentSync: &SyncRecord{
			ID:              "test-123",
			StartTime:       time.Now(),
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

	b.Run("value_copy", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			stateCopy := state
			_ = stateCopy
		}
	})

	b.Run("pointer_copy", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			statePtr := &state
			_ = statePtr
		}
	})
}

// BenchmarkHistorySliceGrowth measures memory impact of history growth
func BenchmarkHistorySliceGrowth(b *testing.B) {
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
		cardID := fmt.Sprintf("card-%d", i)
		_, err := m.StartSync(cardID, 100, 1024*1024)
		if err != nil {
			b.Fatal(err)
		}
		if err := m.FinishSync(true, nil); err != nil {
			b.Fatal(err)
		}
	}

	b.StopTimer()
	history := m.GetHistory()
	b.ReportMetric(float64(len(history)), "history_records")
}
