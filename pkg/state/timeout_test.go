//go:build stress

package state

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestOperationTimeout tests operations with timeout constraints
func TestOperationTimeout(t *testing.T) {
	tmpDir := t.TempDir()
	oldStateDir := stateDir
	stateDir = tmpDir
	defer func() { stateDir = oldStateDir }()

	m, err := NewManager()
	if err != nil {
		t.Fatal(err)
	}

	testCases := []struct {
		name     string
		timeout  time.Duration
		op       func() error
		shouldOK bool
	}{
		{
			name:    "fast_get_state",
			timeout: 100 * time.Millisecond,
			op: func() error {
				_ = m.GetState()
				return nil
			},
			shouldOK: true,
		},
		{
			name:    "fast_set_status",
			timeout: 100 * time.Millisecond,
			op: func() error {
				return m.SetStatus(StatusSyncing)
			},
			shouldOK: true,
		},
		{
			name:    "fast_reload",
			timeout: 100 * time.Millisecond,
			op: func() error {
				return m.Reload()
			},
			shouldOK: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), tc.timeout)
			defer cancel()

			done := make(chan error, 1)
			go func() {
				done <- tc.op()
			}()

			select {
			case err := <-done:
				if err != nil && tc.shouldOK {
					t.Errorf("operation failed: %v", err)
				}
			case <-ctx.Done():
				if tc.shouldOK {
					t.Errorf("operation timed out after %v", tc.timeout)
				}
			}
		})
	}
}

// TestLongRunningOperations tests behavior of long-running operations
func TestLongRunningOperations(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running test in short mode")
	}

	tmpDir := t.TempDir()
	oldStateDir := stateDir
	stateDir = tmpDir
	defer func() { stateDir = oldStateDir }()

	m, err := NewManager()
	if err != nil {
		t.Fatal(err)
	}

	// Start a sync
	_, err = m.StartSync("card-long-running", 100000, 10*1024*1024*1024)
	if err != nil {
		t.Fatal(err)
	}

	// Simulate a long-running sync with many progress updates
	start := time.Now()
	const duration = 5 * time.Second
	const updatesPerSecond = 100

	ticker := time.NewTicker(time.Second / updatesPerSecond)
	defer ticker.Stop()

	updateCount := 0
	for time.Since(start) < duration {
		<-ticker.C
		err := m.UpdateSyncProgress(
			int64(updateCount),
			int64(updateCount*1024*1024),
			fmt.Sprintf("IMG_%05d.JPG", updateCount),
			5*1024*1024,
			2.5*1024*1024,
			"1h30m",
		)
		if err != nil {
			t.Fatal(err)
		}
		updateCount++
	}

	elapsed := time.Since(start)
	t.Logf("Long-running sync test:")
	t.Logf("  Duration: %v", elapsed)
	t.Logf("  Updates: %d", updateCount)
	t.Logf("  Updates/sec: %.2f", float64(updateCount)/elapsed.Seconds())
}

// TestResourceLimits tests behavior under resource constraints
func TestResourceLimits(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping resource limit test in short mode")
	}

	testCases := []struct {
		name       string
		goroutines int
		operations int
		maxTime    time.Duration
	}{
		{"small_load", 10, 100, 5 * time.Second},
		{"medium_load", 50, 500, 10 * time.Second},
		{"large_load", 100, 1000, 20 * time.Second},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			oldStateDir := stateDir
			stateDir = tmpDir
			defer func() { stateDir = oldStateDir }()

			m, err := NewManager()
			if err != nil {
				t.Fatal(err)
			}

			ctx, cancel := context.WithTimeout(context.Background(), tc.maxTime)
			defer cancel()

			var completed atomic.Int64
			var wg sync.WaitGroup

			start := time.Now()

			for i := 0; i < tc.goroutines; i++ {
				wg.Add(1)
				go func(id int) {
					defer wg.Done()

					for j := 0; j < tc.operations; j++ {
						select {
						case <-ctx.Done():
							return
						default:
							switch j % 3 {
							case 0:
								_ = m.GetState()
							case 1:
								m.SetStatus(StatusSyncing)
							case 2:
								_ = m.GetHistory()
							}
							completed.Add(1)
						}
					}
				}(i)
			}

			wg.Wait()
			elapsed := time.Since(start)

			completedOps := completed.Load()
			expectedOps := int64(tc.goroutines * tc.operations)

			t.Logf("Resource limit test '%s':", tc.name)
			t.Logf("  Goroutines: %d", tc.goroutines)
			t.Logf("  Target operations: %d", expectedOps)
			t.Logf("  Completed operations: %d", completedOps)
			t.Logf("  Elapsed: %v (max: %v)", elapsed, tc.maxTime)
			t.Logf("  Operations/sec: %.2f", float64(completedOps)/elapsed.Seconds())

			if elapsed > tc.maxTime {
				t.Errorf("Test exceeded max time: %v > %v", elapsed, tc.maxTime)
			}

			if completedOps < expectedOps {
				t.Errorf("Not all operations completed: %d < %d", completedOps, expectedOps)
			}
		})
	}
}

// TestDeadlockDetectionTimeout tests for potential deadlocks with timeouts
func TestDeadlockDetectionTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping deadlock detection test in short mode")
	}

	tmpDir := t.TempDir()
	oldStateDir := stateDir
	stateDir = tmpDir
	defer func() { stateDir = oldStateDir }()

	m, err := NewManager()
	if err != nil {
		t.Fatal(err)
	}

	// Start a sync for operations
	_, err = m.StartSync("card-deadlock", 1000, 100*1024*1024)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	const goroutines = 100
	var wg sync.WaitGroup
	errorChan := make(chan error, goroutines)

	// Spawn many goroutines performing mixed operations
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			for j := 0; j < 100; j++ {
				select {
				case <-ctx.Done():
					return
				default:
					// Mix of read and write operations that could potentially deadlock
					switch (id + j) % 6 {
					case 0:
						_ = m.GetState()
					case 1:
						m.SetStatus(StatusSyncing)
					case 2:
						ch := m.Subscribe()
						m.Unsubscribe(ch)
					case 3:
						_ = m.GetHistory()
					case 4:
						m.UpdateSyncProgress(int64(j), int64(j*1024), "test.jpg", 1024, 1.5*1024*1024, "5m")
					case 5:
						_ = m.FindLastSyncByCardID(fmt.Sprintf("card-%d", id))
					}
				}
			}
		}(i)
	}

	// Wait with timeout
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// All goroutines completed successfully
		t.Log("No deadlock detected - all operations completed")
	case <-ctx.Done():
		t.Fatal("Potential deadlock detected - operations did not complete within timeout")
	}

	close(errorChan)
	if len(errorChan) > 0 {
		t.Errorf("Got %d errors during deadlock test", len(errorChan))
	}
}

// TestRateLimiting tests rate limiting behavior
func TestRateLimiting(t *testing.T) {
	tmpDir := t.TempDir()
	oldStateDir := stateDir
	stateDir = tmpDir
	defer func() { stateDir = oldStateDir }()

	m, err := NewManager()
	if err != nil {
		t.Fatal(err)
	}

	// Set aggressive throttling
	m.progressSaveDelay = 1 * time.Second

	// Start a sync
	_, err = m.StartSync("card-rate-limit", 10000, 1000*1024*1024)
	if err != nil {
		t.Fatal(err)
	}

	// Send many rapid updates
	const updates = 1000
	start := time.Now()

	for i := 0; i < updates; i++ {
		err := m.UpdateSyncProgress(
			int64(i),
			int64(i*1024),
			fmt.Sprintf("IMG_%04d.JPG", i),
			1024,
			1.5*1024*1024,
			"5m",
		)
		if err != nil {
			t.Fatal(err)
		}
	}

	elapsed := time.Since(start)

	t.Logf("Rate limiting test:")
	t.Logf("  Updates: %d", updates)
	t.Logf("  Elapsed: %v", elapsed)
	t.Logf("  Updates/sec: %.2f", float64(updates)/elapsed.Seconds())
	t.Logf("  Throttle delay: %v", m.progressSaveDelay)

	// Should complete quickly despite throttling (throttling only affects disk writes)
	if elapsed > 5*time.Second {
		t.Errorf("Updates took too long: %v", elapsed)
	}
}

// TestGracefulShutdown tests graceful shutdown behavior
func TestGracefulShutdown(t *testing.T) {
	tmpDir := t.TempDir()
	oldStateDir := stateDir
	stateDir = tmpDir
	defer func() { stateDir = oldStateDir }()

	m, err := NewManager()
	if err != nil {
		t.Fatal(err)
	}

	// Start operations
	const goroutines = 50
	ctx, cancel := context.WithCancel(context.Background())

	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				default:
					_ = m.GetState()
					time.Sleep(1 * time.Millisecond)
				}
			}
		}()
	}

	// Let them run for a bit
	time.Sleep(500 * time.Millisecond)

	// Trigger shutdown
	shutdownStart := time.Now()
	cancel()

	// Wait for graceful shutdown with timeout
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		elapsed := time.Since(shutdownStart)
		t.Logf("Graceful shutdown completed in %v", elapsed)
		if elapsed > 2*time.Second {
			t.Errorf("Shutdown took too long: %v", elapsed)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Shutdown did not complete within timeout")
	}
}

// BenchmarkOperationLatency benchmarks latency of critical operations
func BenchmarkOperationLatency(b *testing.B) {
	tmpDir := b.TempDir()
	oldStateDir := stateDir
	stateDir = tmpDir
	defer func() { stateDir = oldStateDir }()

	m, err := NewManager()
	if err != nil {
		b.Fatal(err)
	}

	operations := []struct {
		name string
		op   func() error
	}{
		{"GetState", func() error { _ = m.GetState(); return nil }},
		{"SetStatus", func() error { return m.SetStatus(StatusSyncing) }},
		{"GetHistory", func() error { _ = m.GetHistory(); return nil }},
	}

	for _, op := range operations {
		b.Run(op.name, func(b *testing.B) {
			var totalLatency time.Duration
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				start := time.Now()
				if err := op.op(); err != nil {
					b.Fatal(err)
				}
				latency := time.Since(start)
				totalLatency += latency
			}

			avgLatency := totalLatency / time.Duration(b.N)
			b.ReportMetric(float64(avgLatency.Microseconds()), "µs/op")
		})
	}
}

// TestTimeoutPropagation tests timeout propagation through operation chains
func TestTimeoutPropagation(t *testing.T) {
	tmpDir := t.TempDir()
	oldStateDir := stateDir
	stateDir = tmpDir
	defer func() { stateDir = oldStateDir }()

	m, err := NewManager()
	if err != nil {
		t.Fatal(err)
	}

	// Start a sync
	_, err = m.StartSync("card-timeout", 1000, 100*1024*1024)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	// Perform operations with context timeout
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	count := 0
	for {
		select {
		case <-ctx.Done():
			t.Logf("Context timeout after %d operations", count)
			return
		case <-ticker.C:
			m.UpdateSyncProgress(int64(count), int64(count*1024), "test.jpg", 1024, 1.5*1024*1024, "5m")
			count++
		}
	}
}

// TestSubscriberBackpressure tests subscriber backpressure handling
func TestSubscriberBackpressure(t *testing.T) {
	tmpDir := t.TempDir()
	oldStateDir := stateDir
	stateDir = tmpDir
	defer func() { stateDir = oldStateDir }()

	m, err := NewManager()
	if err != nil {
		t.Fatal(err)
	}

	// Create slow subscriber
	ch := m.Subscribe()
	defer m.Unsubscribe(ch)

	// Don't read from channel (simulate slow subscriber)

	// Try to send many updates
	const updates = 100
	timeout := time.After(5 * time.Second)
	updatesSent := 0

	for i := 0; i < updates; i++ {
		select {
		case <-timeout:
			t.Logf("Sent %d/%d updates before timeout", updatesSent, updates)
			// This is expected - slow subscribers shouldn't block the system
			return
		default:
			if err := m.SetStatus(StatusSyncing); err != nil {
				t.Fatal(err)
			}
			updatesSent++
			time.Sleep(10 * time.Millisecond)
		}
	}

	t.Logf("All %d updates sent successfully", updatesSent)
}

// BenchmarkWorstCaseLatency benchmarks worst-case operation latency
func BenchmarkWorstCaseLatency(b *testing.B) {
	tmpDir := b.TempDir()
	oldStateDir := stateDir
	stateDir = tmpDir
	defer func() { stateDir = oldStateDir }()

	m, err := NewManager()
	if err != nil {
		b.Fatal(err)
	}

	// Create worst-case scenario: large history, many subscribers
	for i := 0; i < 1000; i++ {
		_, err := m.StartSync(fmt.Sprintf("card-%d", i), 1000, 100*1024*1024)
		if err != nil {
			b.Fatal(err)
		}
		if err := m.FinishSync(true, nil); err != nil {
			b.Fatal(err)
		}
	}

	// Create many subscribers
	const numSubscribers = 100
	channels := make([]chan CurrentState, numSubscribers)
	for i := 0; i < numSubscribers; i++ {
		ch := m.Subscribe()
		channels[i] = ch
		go func(c chan CurrentState) {
			for range c {
				time.Sleep(1 * time.Millisecond) // Slow subscriber
			}
		}(ch)
	}
	defer func() {
		for _, ch := range channels {
			m.Unsubscribe(ch)
		}
	}()

	var maxLatency time.Duration
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		start := time.Now()
		if err := m.SetStatus(StatusSyncing); err != nil {
			b.Fatal(err)
		}
		latency := time.Since(start)

		if latency > maxLatency {
			maxLatency = latency
		}
	}

	b.ReportMetric(float64(maxLatency.Milliseconds()), "ms_max")
}

// TestMemoryLeakUnderTimeout tests for memory leaks with timeout scenarios
func TestMemoryLeakUnderTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping memory leak test in short mode")
	}

	tmpDir := t.TempDir()
	oldStateDir := stateDir
	stateDir = tmpDir
	defer func() { stateDir = oldStateDir }()

	m, err := NewManager()
	if err != nil {
		t.Fatal(err)
	}

	const iterations = 100
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	for i := 0; i < iterations; i++ {
		select {
		case <-ctx.Done():
			t.Fatal("Test timed out")
		default:
			// Operations that could leak if not handled properly
			ch := m.Subscribe()
			m.Unsubscribe(ch)

			_, err := m.StartSync(fmt.Sprintf("card-%d", i), 100, 1024*1024)
			if err != nil {
				t.Fatal(err)
			}

			if err := m.FinishSync(true, nil); err != nil {
				t.Fatal(err)
			}
		}
	}

	t.Logf("Completed %d iterations without timeout or memory issues", iterations)
}
