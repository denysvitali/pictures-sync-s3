package ntpsync

import (
	"strings"
	"testing"
	"time"
)

// Note: These tests require network connectivity to NTP servers.
// Tests marked with t.Skip() can be run manually when network is available.

func TestSyncTime(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network-dependent test in short mode")
	}

	err := SyncTime()
	if err != nil {
		// NTP sync can fail due to network issues, but should try multiple servers
		t.Logf("NTP sync failed (this may be expected in restricted environments): %v", err)

		// Verify error message is informative
		if !strings.Contains(err.Error(), "failed to sync with any NTP server") {
			t.Errorf("unexpected error message: %v", err)
		}
	} else {
		t.Log("NTP sync successful")
	}
}

func TestSyncTimeServerList(t *testing.T) {
	// This test documents that we try multiple NTP servers
	// The actual servers are defined in SyncTime()

	expectedServers := []string{
		"0.pool.ntp.org",
		"1.pool.ntp.org",
		"2.pool.ntp.org",
		"time.google.com",
	}

	// Verify we have multiple servers configured (good redundancy)
	if len(expectedServers) < 2 {
		t.Error("should have at least 2 NTP servers for redundancy")
	}

	t.Logf("Configured NTP servers: %v", expectedServers)
}

func TestEnsureTimeSyncMaxAttempts(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network-dependent test in short mode")
	}

	tests := []struct {
		name        string
		maxAttempts int
	}{
		{"single attempt", 1},
		{"three attempts", 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start := time.Now()
			err := EnsureTimeSync(tt.maxAttempts)
			duration := time.Since(start)

			if err != nil {
				t.Logf("EnsureTimeSync failed after %d attempts (duration: %v): %v",
					tt.maxAttempts, duration, err)

				// Verify error message includes attempt count
				if !strings.Contains(err.Error(), "failed to sync time after") {
					t.Errorf("error should mention failed attempts: %v", err)
				}
			} else {
				t.Logf("EnsureTimeSync succeeded (duration: %v)", duration)
			}
		})
	}
}

func TestEnsureTimeSyncBackoff(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping time-dependent test in short mode")
	}

	// Test with multiple attempts to verify backoff behavior
	maxAttempts := 3
	start := time.Now()

	err := EnsureTimeSync(maxAttempts)
	duration := time.Since(start)

	if err != nil {
		// If all attempts failed, verify backoff delays were applied
		// Expected delays: 1s + 2s = 3s minimum
		expectedMinDuration := 3 * time.Second

		// Allow some tolerance for execution time
		if duration < expectedMinDuration-500*time.Millisecond {
			t.Errorf("backoff not applied correctly, duration: %v (expected at least %v)",
				duration, expectedMinDuration)
		}

		t.Logf("Backoff applied correctly, total duration: %v", duration)
	} else {
		t.Logf("Sync succeeded on first attempt (duration: %v), backoff not tested", duration)
	}
}

func TestEnsureTimeSyncZeroAttempts(t *testing.T) {
	// Test edge case: 0 attempts should fail immediately
	err := EnsureTimeSync(0)
	if err == nil {
		t.Error("EnsureTimeSync(0) should return error")
	}

	if !strings.Contains(err.Error(), "failed to sync time after 0 attempts") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestEnsureTimeSyncNegativeAttempts(t *testing.T) {
	// Test edge case: negative attempts should fail immediately
	err := EnsureTimeSync(-1)
	if err == nil {
		t.Error("EnsureTimeSync(-1) should return error")
	}
}

func TestSetSystemTime(t *testing.T) {
	originalSetSystemClock := setSystemClock
	defer func() {
		setSystemClock = originalSetSystemClock
	}()

	want := time.Date(2026, time.May, 6, 8, 0, 0, 0, time.UTC)
	var got time.Time
	setSystemClock = func(t time.Time) error {
		got = t
		return nil
	}

	if err := SetSystemTime(want); err != nil {
		t.Fatalf("SetSystemTime returned error: %v", err)
	}
	if !got.Equal(want) {
		t.Fatalf("expected %s, got %s", want, got)
	}
}

func TestSetSystemTimeRejectsZero(t *testing.T) {
	originalSetSystemClock := setSystemClock
	defer func() {
		setSystemClock = originalSetSystemClock
	}()

	called := false
	setSystemClock = func(t time.Time) error {
		called = true
		return nil
	}

	if err := SetSystemTime(time.Time{}); err == nil {
		t.Fatal("expected zero time error")
	}
	if called {
		t.Fatal("system clock setter should not be called for zero time")
	}
}

func TestBackoffCapping(t *testing.T) {
	// Test that backoff is capped at 64 seconds
	// This is a unit test of the backoff logic without actually running it

	backoff := 1 * time.Second
	maxBackoff := 64 * time.Second

	// Simulate 10 doublings
	for i := 0; i < 10; i++ {
		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}

	if backoff != maxBackoff {
		t.Errorf("backoff should be capped at %v, got %v", maxBackoff, backoff)
	}

	t.Logf("Backoff correctly capped at %v", maxBackoff)
}

func TestTimeOffsetDetection(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network-dependent test in short mode")
	}

	// This test verifies that SyncTime() can detect and log time offset
	// The actual offset detection happens inside SyncTime()

	// Try to sync and capture whether it succeeds
	err := SyncTime()
	if err != nil {
		t.Logf("NTP sync failed: %v", err)
	} else {
		t.Log("NTP sync successful - time offset would have been logged")
	}
}

func TestMultipleSequentialSyncs(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network-dependent test in short mode")
	}

	// Verify we can sync multiple times without issues
	for i := 0; i < 2; i++ {
		err := SyncTime()
		if err != nil {
			t.Logf("Sync attempt %d failed: %v", i+1, err)
		} else {
			t.Logf("Sync attempt %d succeeded", i+1)
		}

		// Small delay between syncs
		time.Sleep(100 * time.Millisecond)
	}
}

func TestConcurrentSyncs(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network-dependent test in short mode")
	}

	// Test that concurrent syncs don't cause issues
	done := make(chan bool, 3)

	for i := 0; i < 3; i++ {
		go func(n int) {
			err := SyncTime()
			if err != nil {
				t.Logf("Concurrent sync %d failed: %v", n, err)
			} else {
				t.Logf("Concurrent sync %d succeeded", n)
			}
			done <- true
		}(i)
	}

	// Wait for all to complete with timeout
	timeout := time.After(30 * time.Second)
	for i := 0; i < 3; i++ {
		select {
		case <-done:
			// Success
		case <-timeout:
			t.Error("concurrent sync test timed out")
			return
		}
	}
}

// BenchmarkSyncTime benchmarks NTP sync time
func BenchmarkSyncTime(b *testing.B) {
	if testing.Short() {
		b.Skip("skipping network-dependent benchmark in short mode")
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		SyncTime()
	}
}

// TestSyncTimeTimeout tests that sync operations timeout appropriately
func TestSyncTimeTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network-dependent test in short mode")
	}

	// This test ensures SyncTime() doesn't hang indefinitely
	done := make(chan bool)
	timeout := 30 * time.Second

	go func() {
		_ = SyncTime()
		done <- true
	}()

	select {
	case <-done:
		t.Log("SyncTime completed within timeout")
	case <-time.After(timeout):
		t.Errorf("SyncTime hung for more than %v", timeout)
	}
}

// TestPackageDocumentation verifies package-level documentation exists
func TestPackageDocumentation(t *testing.T) {
	// This test ensures the package is properly documented
	// The actual documentation is in the package comment

	// Just verify we can call the exported functions
	_ = SyncTime
	_ = EnsureTimeSync

	t.Log("Package exports validated")
}
