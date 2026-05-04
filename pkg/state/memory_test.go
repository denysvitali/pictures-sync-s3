package state

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
	"testing"
	"time"
)

// setupTestManager creates a test manager (shared with state_test.go)
func setupTestManager(t testing.TB) *Manager {
	mgr := &Manager{
		currentState:      CurrentState{Status: StatusIdle},
		notifier:          newNotifier(),
		history:           make([]SyncRecord, 0),
		progressSaveDelay: 5 * time.Second,
		lastProgressSave:  time.Now(),
	}
	return mgr
}

// TestGoroutineLeaks verifies that goroutines are properly cleaned up
func TestGoroutineLeaks(t *testing.T) {
	// Force GC to clean up before baseline
	runtime.GC()
	debug.FreeOSMemory()
	time.Sleep(100 * time.Millisecond)

	baselineGoroutines := runtime.NumGoroutine()
	t.Logf("Baseline goroutines: %d", baselineGoroutines)

	// Create and destroy managers multiple times
	iterations := 50
	for i := 0; i < iterations; i++ {
		mgr := setupTestManager(t)

		// Subscribe and unsubscribe multiple times
		for j := 0; j < 10; j++ {
			ch := mgr.Subscribe()
			mgr.Unsubscribe(ch)
		}

		// Simulate state updates
		mgr.SetStatus(StatusSyncing)
		mgr.StartSync("test-card", 100, 1000000)
		mgr.FinishSync(true, nil)
	}

	// Force GC multiple times to clean up
	for i := 0; i < 5; i++ {
		runtime.GC()
		debug.FreeOSMemory()
		time.Sleep(100 * time.Millisecond)
	}

	finalGoroutines := runtime.NumGoroutine()
	t.Logf("Final goroutines: %d", finalGoroutines)

	// Allow some variance (typically 1-2 goroutines per test iteration may remain briefly)
	// But shouldn't grow unbounded
	leakedGoroutines := finalGoroutines - baselineGoroutines

	if leakedGoroutines > 10 {
		t.Errorf("Potential goroutine leak detected: baseline=%d, final=%d, leaked=%d",
			baselineGoroutines, finalGoroutines, leakedGoroutines)

		// Print stack traces to debug
		buf := make([]byte, 1<<20)
		stackLen := runtime.Stack(buf, true)
		t.Logf("Goroutine stack traces:\n%s", buf[:stackLen])
	} else {
		t.Logf("Goroutine leak test passed: leaked=%d (within acceptable range)", leakedGoroutines)
	}
}

// TestSubscriberChannelBufferExhaustion tests channel buffer limits
func TestSubscriberChannelBufferExhaustion(t *testing.T) {
	mgr := setupTestManager(t)

	// Subscribe but don't consume messages
	ch := mgr.Subscribe()
	defer mgr.Unsubscribe(ch)

	// Send more updates than the channel buffer can hold (buffer is 10)
	for i := 0; i < 100; i++ {
		err := mgr.SetStatus(StatusSyncing)
		if err != nil {
			t.Errorf("SetStatus failed at iteration %d: %v", i, err)
		}
	}

	// Verify state manager didn't block or crash
	currentState := mgr.GetState()
	if currentState.Status != StatusSyncing {
		t.Errorf("Expected status to be syncing, got %v", currentState.Status)
	}

	t.Log("Channel buffer exhaustion test passed - state manager continues working")
}

// TestFileDescriptorAccumulation checks for file descriptor leaks
func TestFileDescriptorAccumulation(t *testing.T) {
	// Get baseline FD count
	baselineFDs := countOpenFileDescriptors(t)
	t.Logf("Baseline file descriptors: %d", baselineFDs)

	// Perform many state save operations (using in-memory manager, no file I/O)
	mgr := setupTestManager(t)

	iterations := 500
	for i := 0; i < iterations; i++ {
		mgr.SetStatus(StatusIdle)
		mgr.SetStatus(StatusSyncing)
		mgr.StartSync(fmt.Sprintf("card-%d", i), 100, 1000000)
		mgr.UpdateSyncProgress(50, 500000, "test.jpg", 1024, 1000000, "5s")
		mgr.FinishSync(true, nil)
	}

	// Force GC
	runtime.GC()
	time.Sleep(100 * time.Millisecond)

	finalFDs := countOpenFileDescriptors(t)
	t.Logf("Final file descriptors: %d", finalFDs)

	fdLeak := finalFDs - baselineFDs
	if fdLeak > 10 {
		t.Errorf("File descriptor leak detected: baseline=%d, final=%d, leaked=%d",
			baselineFDs, finalFDs, fdLeak)
	} else {
		t.Logf("FD leak test passed: leaked=%d", fdLeak)
	}
}

// TestMemoryGrowthDuringLargeSync simulates large sync operations
func TestMemoryGrowthDuringLargeSync(t *testing.T) {
	runtime.GC()
	debug.FreeOSMemory()

	var m1 runtime.MemStats
	runtime.ReadMemStats(&m1)
	baselineAlloc := m1.Alloc
	t.Logf("Baseline memory allocation: %.2f MB", float64(baselineAlloc)/(1024*1024))

	mgr := setupTestManager(t)

	// Simulate large sync with many progress updates
	mgr.StartSync("test-card-large", 10000, 100*1024*1024*1024) // 100 GB

	for i := 0; i < 1000; i++ {
		fileName := fmt.Sprintf("IMG_%04d.jpg", i)
		mgr.UpdateSyncProgress(
			int64(i),
			int64(i*10*1024*1024), // 10 MB per file
			fileName,
			10*1024*1024,
			5*1024*1024, // 5 MB/s
			"2h 30m",
		)
	}

	mgr.FinishSync(true, nil)

	runtime.GC()
	debug.FreeOSMemory()

	var m2 runtime.MemStats
	runtime.ReadMemStats(&m2)
	finalAlloc := m2.Alloc
	t.Logf("Final memory allocation: %.2f MB", float64(finalAlloc)/(1024*1024))

	growth := float64(finalAlloc-baselineAlloc) / (1024 * 1024)
	t.Logf("Memory growth: %.2f MB", growth)

	// Should not grow more than 10 MB for this test
	if growth > 10 {
		t.Errorf("Excessive memory growth: %.2f MB (threshold: 10 MB)", growth)
	}
}

// TestStringConcatenationInLoops checks for inefficient string building
func TestStringConcatenationInLoops(t *testing.T) {
	runtime.GC()
	var m1 runtime.MemStats
	runtime.ReadMemStats(&m1)

	mgr := setupTestManager(t)

	// Create many sync records with string data
	for i := 0; i < 1000; i++ {
		cardID := fmt.Sprintf("card-%d", i)
		mgr.StartSync(cardID, 100, 1000000)

		// Simulate progress updates with varying file names
		for j := 0; j < 10; j++ {
			fileName := fmt.Sprintf("IMG_%04d_%s.jpg", j, strings.Repeat("a", 100))
			mgr.UpdateSyncProgress(int64(j), int64(j*10000), fileName, 10000, 1000, "1m")
		}

		mgr.FinishSync(true, nil)
	}

	runtime.GC()
	var m2 runtime.MemStats
	runtime.ReadMemStats(&m2)

	allocGrowth := float64(m2.TotalAlloc-m1.TotalAlloc) / (1024 * 1024)
	t.Logf("Total allocations during string operations: %.2f MB", allocGrowth)

	// Check allocation efficiency - should be reasonable for 1000 syncs
	if allocGrowth > 100 {
		t.Errorf("Excessive allocations from string operations: %.2f MB", allocGrowth)
	}
}

// TestLargeJSONMarshalingMemory tests memory usage with large history
func TestLargeJSONMarshalingMemory(t *testing.T) {
	runtime.GC()
	var m1 runtime.MemStats
	runtime.ReadMemStats(&m1)
	baselineHeap := m1.HeapAlloc

	mgr := setupTestManager(t)

	// Create large history (simulate 10,000 syncs = ~100MB JSON)
	numRecords := 10000
	for i := 0; i < numRecords; i++ {
		record := SyncRecord{
			ID:              fmt.Sprintf("sync-%d", i),
			StartTime:       time.Now(),
			EndTime:         time.Now().Add(time.Hour),
			Status:          "success",
			FilesTotal:      1000,
			FilesSynced:     1000,
			BytesTotal:      100 * 1024 * 1024, // 100 MB
			BytesSynced:     100 * 1024 * 1024,
			CardID:          fmt.Sprintf("card-%d", i%100),
			CurrentFile:     fmt.Sprintf("IMG_%d.jpg", i),
			CurrentFileSize: 10 * 1024 * 1024,
			TransferSpeed:   5 * 1024 * 1024,
			ETA:             "5m",
		}

		mgr.mu.Lock()
		mgr.history = append(mgr.history, record)
		mgr.mu.Unlock()
	}

	// Measure memory before marshaling
	runtime.GC()
	var m2 runtime.MemStats
	runtime.ReadMemStats(&m2)
	beforeMarshalHeap := m2.HeapAlloc

	// Marshal large history to JSON (simulates save without file I/O)
	data, err := json.MarshalIndent(mgr.history, "", "  ")
	if err != nil {
		t.Fatalf("Failed to marshal history: %v", err)
	}

	// Measure memory after marshaling
	runtime.GC()
	var m3 runtime.MemStats
	runtime.ReadMemStats(&m3)
	afterMarshalHeap := m3.HeapAlloc

	historySize := float64(beforeMarshalHeap-baselineHeap) / (1024 * 1024)
	marshalOverhead := float64(afterMarshalHeap-beforeMarshalHeap) / (1024 * 1024)
	dataSize := float64(len(data)) / (1024 * 1024)

	t.Logf("History memory usage: %.2f MB", historySize)
	t.Logf("JSON marshaling overhead: %.2f MB", marshalOverhead)
	t.Logf("JSON data size: %.2f MB", dataSize)
	t.Logf("Number of records: %d", numRecords)

	// Verify marshaling doesn't leak memory
	if marshalOverhead > 50 {
		t.Errorf("Excessive JSON marshaling overhead: %.2f MB", marshalOverhead)
	}

	// History should be at least as large as marshaled data (in-memory has some overhead)
	if historySize < dataSize*0.5 {
		t.Errorf("In-memory history (%.2f MB) much smaller than data (%.2f MB)", historySize, dataSize)
	}
}

// TestUnboundedSliceGrowth tests that slices don't grow without bounds
func TestUnboundedSliceGrowth(t *testing.T) {
	mgr := setupTestManager(t)

	// Add many listeners
	var listeners []chan CurrentState
	for i := 0; i < 1000; i++ {
		ch := mgr.Subscribe()
		listeners = append(listeners, ch)
	}

	initialLen := mgr.notifier.getListenerCount()
	initialCap := cap(mgr.notifier.getListeners())
	t.Logf("Initial listeners: len=%d, cap=%d", initialLen, initialCap)

	// Unsubscribe half
	for i := 0; i < 500; i++ {
		mgr.Unsubscribe(listeners[i])
	}

	afterUnsubLen := mgr.notifier.getListenerCount()
	afterUnsubCap := cap(mgr.notifier.getListeners())

	t.Logf("After unsubscribe: len=%d, cap=%d", afterUnsubLen, afterUnsubCap)

	// Length should decrease
	if afterUnsubLen >= initialLen {
		t.Errorf("Listeners slice not shrinking: before=%d, after=%d", initialLen, afterUnsubLen)
	}

	// Capacity should not grow excessively
	if afterUnsubCap > initialCap*2 {
		t.Errorf("Listeners slice capacity growing too much: initial=%d, final=%d", initialCap, afterUnsubCap)
	}

	// Clean up remaining listeners
	for i := 500; i < 1000; i++ {
		mgr.Unsubscribe(listeners[i])
	}
}

// TestMapMemoryNotReleased checks if map memory is properly released
func TestMapMemoryNotReleased(t *testing.T) {
	runtime.GC()
	var m1 runtime.MemStats
	runtime.ReadMemStats(&m1)

	mgr := setupTestManager(t)

	// Create many sync records (stored in slice, but could expose map issues)
	numSyncs := 10000
	for i := 0; i < numSyncs; i++ {
		cardID := fmt.Sprintf("card-%d", i)
		mgr.StartSync(cardID, 100, 1000000)
		mgr.FinishSync(true, nil)
	}

	runtime.GC()
	var m2 runtime.MemStats
	runtime.ReadMemStats(&m2)
	memAfterAdd := m2.HeapAlloc

	// Clear history
	mgr.mu.Lock()
	mgr.history = make([]SyncRecord, 0)
	mgr.mu.Unlock()

	runtime.GC()
	debug.FreeOSMemory()
	time.Sleep(100 * time.Millisecond)

	var m3 runtime.MemStats
	runtime.ReadMemStats(&m3)
	memAfterClear := m3.HeapAlloc

	growthMB := float64(memAfterAdd-m1.HeapAlloc) / (1024 * 1024)
	releasedMB := float64(memAfterAdd-memAfterClear) / (1024 * 1024)
	retainedMB := float64(memAfterClear-m1.HeapAlloc) / (1024 * 1024)

	t.Logf("Memory growth: %.2f MB", growthMB)
	t.Logf("Memory released: %.2f MB", releasedMB)
	t.Logf("Memory retained: %.2f MB", retainedMB)

	// Should release most of the memory
	releaseRate := releasedMB / growthMB
	if releaseRate < 0.5 {
		t.Errorf("Insufficient memory released: %.1f%% (expected >50%%)", releaseRate*100)
	}
}

// TestContextLeaks checks for context cancellation leaks
func TestContextLeaks(t *testing.T) {
	runtime.GC()
	baselineGoroutines := runtime.NumGoroutine()

	mgr := setupTestManager(t)

	// Simulate context-based operations
	for i := 0; i < 100; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)

		// Simulate work with context
		ch := mgr.Subscribe()

		go func(c context.Context, ch chan CurrentState) {
			select {
			case <-c.Done():
				return
			case <-ch:
				return
			}
		}(ctx, ch)

		// Cancel and cleanup
		cancel()
		mgr.Unsubscribe(ch)
	}

	// Wait for goroutines to exit
	time.Sleep(200 * time.Millisecond)
	runtime.GC()

	finalGoroutines := runtime.NumGoroutine()
	leaked := finalGoroutines - baselineGoroutines

	t.Logf("Context leak test: baseline=%d, final=%d, leaked=%d",
		baselineGoroutines, finalGoroutines, leaked)

	if leaked > 10 {
		t.Errorf("Context goroutine leak detected: leaked=%d", leaked)
	}
}

// TestConcurrentAccessMemorySafety tests thread-safety under load
func TestConcurrentAccessMemorySafety(t *testing.T) {
	mgr := setupTestManager(t)

	// Start concurrent operations
	var wg sync.WaitGroup
	errors := make(chan error, 100)

	// Single writer to avoid "sync already in progress" contention
	wg.Add(1)
	go func() {
		defer wg.Done()
		for j := 0; j < 500; j++ {
			if err := mgr.SetStatus(StatusSyncing); err != nil {
				errors <- fmt.Errorf("writer: SetStatus failed: %v", err)
				return
			}

			if _, err := mgr.StartSync(fmt.Sprintf("card-%d", j), 100, 1000000); err != nil {
				errors <- fmt.Errorf("writer: StartSync failed: %v", err)
				return
			}

			if err := mgr.UpdateSyncProgress(50, 500000, "test.jpg", 1024, 1000, "5s"); err != nil {
				errors <- fmt.Errorf("writer: UpdateSyncProgress failed: %v", err)
				return
			}

			if err := mgr.FinishSync(true, nil); err != nil {
				errors <- fmt.Errorf("writer: FinishSync failed: %v", err)
				return
			}
		}
	}()

	// Multiple concurrent readers
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_ = mgr.GetState()
				_ = mgr.GetHistory()
				_ = mgr.FindLastSyncByCardID(fmt.Sprintf("card-%d", id))
			}
		}(i)
	}

	// Subscribers
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			ch := mgr.Subscribe()
			defer mgr.Unsubscribe(ch)

			timeout := time.After(3 * time.Second)
			for {
				select {
				case <-ch:
					// Consume messages
				case <-timeout:
					return
				}
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// Check for errors
	errorCount := 0
	for err := range errors {
		t.Errorf("Concurrent access error: %v", err)
		errorCount++
	}

	if errorCount > 0 {
		t.Fatalf("Found %d errors during concurrent access", errorCount)
	}

	t.Log("Concurrent access memory safety test passed")
}

// TestNotifyListenersDeadlock checks for deadlock in notification
func TestNotifyListenersDeadlock(t *testing.T) {
	mgr := setupTestManager(t)

	// Create slow consumers
	var wg sync.WaitGroup
	receivedCounts := make([]int, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			ch := mgr.Subscribe()
			defer mgr.Unsubscribe(ch)

			// Consume messages for up to 1 second; notification may
			// drop messages when the buffer is full, so we just verify
			// the system doesn't deadlock.
			timeout := time.After(1 * time.Second)
			for {
				select {
				case <-ch:
					receivedCounts[id]++
					// Slow consumer
					time.Sleep(10 * time.Millisecond)
				case <-timeout:
					return
				}
			}
		}(i)
	}

	// Rapid state updates - should complete quickly without deadlock
	updateStart := time.Now()
	for i := 0; i < 50; i++ {
		mgr.SetStatus(StatusSyncing)
		mgr.SetStatus(StatusIdle)
	}
	updateElapsed := time.Since(updateStart)

	// If updates took more than 5 seconds, there may be a deadlock
	if updateElapsed > 5*time.Second {
		t.Errorf("State updates took too long (%v) - possible deadlock", updateElapsed)
	}

	wg.Wait()

	totalReceived := 0
	for _, count := range receivedCounts {
		totalReceived += count
	}
	t.Logf("Deadlock test passed: updates=%v, total messages received=%d", updateElapsed, totalReceived)
}

// Helper functions

func countOpenFileDescriptors(t *testing.T) int {
	// Linux-specific: count open file descriptors in /proc/self/fd
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		// Not on Linux or can't read /proc
		t.Logf("Cannot count FDs: %v", err)
		return 0
	}
	return len(entries)
}

// BenchmarkNotifyListenersScaling benchmarks notification performance
func BenchmarkNotifyListenersScaling(b *testing.B) {
	mgr := setupTestManager(b)

	// Test with varying numbers of subscribers
	for _, numListeners := range []int{1, 10, 100, 1000} {
		b.Run(fmt.Sprintf("listeners-%d", numListeners), func(b *testing.B) {
			// Subscribe
			channels := make([]chan CurrentState, numListeners)
			for i := 0; i < numListeners; i++ {
				channels[i] = mgr.Subscribe()
			}

			// Start consumers
			var wg sync.WaitGroup
			for i := 0; i < numListeners; i++ {
				wg.Add(1)
				go func(ch chan CurrentState) {
					defer wg.Done()
					for range ch {
						// Consume
					}
				}(channels[i])
			}

			b.ResetTimer()

			// Benchmark notifications
			for i := 0; i < b.N; i++ {
				mgr.SetStatus(StatusSyncing)
			}

			b.StopTimer()

			// Cleanup
			for _, ch := range channels {
				mgr.Unsubscribe(ch)
			}
			wg.Wait()
		})
	}
}

// BenchmarkJSONMarshalLargeHistory benchmarks JSON marshaling
func BenchmarkJSONMarshalLargeHistory(b *testing.B) {
	mgr := setupTestManager(b)

	// Create large history
	for i := 0; i < 1000; i++ {
		record := SyncRecord{
			ID:          fmt.Sprintf("sync-%d", i),
			StartTime:   time.Now(),
			EndTime:     time.Now().Add(time.Hour),
			Status:      "success",
			FilesTotal:  1000,
			FilesSynced: 1000,
			BytesTotal:  100 * 1024 * 1024,
			BytesSynced: 100 * 1024 * 1024,
			CardID:      fmt.Sprintf("card-%d", i),
		}
		mgr.mu.Lock()
		mgr.history = append(mgr.history, record)
		mgr.mu.Unlock()
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		data, err := json.MarshalIndent(mgr.history, "", "  ")
		if err != nil {
			b.Fatalf("Marshal failed: %v", err)
		}
		_ = data
	}

	b.ReportMetric(float64(len(mgr.history)), "records")
}

// BenchmarkMemoryAllocationRate measures allocation rate
func BenchmarkMemoryAllocationRate(b *testing.B) {
	mgr := setupTestManager(b)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		mgr.StartSync(fmt.Sprintf("card-%d", i), 100, 1000000)
		mgr.UpdateSyncProgress(50, 500000, "test.jpg", 1024, 1000, "5s")
		mgr.FinishSync(true, nil)
	}
}
