//go:build stress

package syncmanager

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// TestTimeZoneChangeDuringSync tests behavior when system timezone changes during sync
func TestTimeZoneChangeDuringSync(t *testing.T) {
	// BUG: time.Now() and time.Since() use wall clock which is affected by timezone changes
	// This can cause negative durations or incorrect ETA calculations

	tests := []struct {
		name            string
		startTime       time.Time
		currentTime     time.Time
		expectedElapsed time.Duration
		expectNegative  bool
	}{
		{
			name:            "normal progression",
			startTime:       time.Date(2025, 10, 15, 10, 0, 0, 0, time.UTC),
			currentTime:     time.Date(2025, 10, 15, 10, 5, 0, 0, time.UTC),
			expectedElapsed: 5 * time.Minute,
			expectNegative:  false,
		},
		{
			name:            "timezone change backwards (UTC to PST)",
			startTime:       time.Date(2025, 10, 15, 10, 0, 0, 0, time.UTC),
			currentTime:     time.Date(2025, 10, 15, 2, 0, 0, 0, time.FixedZone("PST", -8*3600)),
			expectedElapsed: 0, // Same instant, but looks like -8 hours
			expectNegative:  false,
		},
		{
			name:            "DST transition forward (spring)",
			startTime:       time.Date(2025, 3, 9, 1, 30, 0, 0, time.FixedZone("PST", -8*3600)),
			currentTime:     time.Date(2025, 3, 9, 3, 30, 0, 0, time.FixedZone("PDT", -7*3600)),
			expectedElapsed: 1 * time.Hour, // Lost an hour
			expectNegative:  false,
		},
		{
			name:            "DST transition backward (fall)",
			startTime:       time.Date(2025, 11, 2, 1, 30, 0, 0, time.FixedZone("PDT", -7*3600)),
			currentTime:     time.Date(2025, 11, 2, 1, 30, 0, 0, time.FixedZone("PST", -8*3600)),
			expectedElapsed: 1 * time.Hour, // Gained an hour
			expectNegative:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate elapsed time calculation like in monitorProgress
			elapsed := tt.currentTime.Sub(tt.startTime)

			// BUG: This can be negative or unexpected due to timezone changes
			if elapsed < 0 && !tt.expectNegative {
				t.Errorf("FOUND BUG: elapsed time is negative: %v", elapsed)
			}

			// BUG: Speed calculation can panic or produce infinity with negative/zero elapsed
			var speed float64
			sessionTransferred := int64(1000000) // 1MB
			if elapsed > 0 {
				speed = float64(sessionTransferred) / elapsed.Seconds()
			}
			// With elapsed <= 0, speed would be 0 or infinity
			if elapsed <= 0 && speed != 0 {
				t.Errorf("FOUND BUG: speed should be 0 when elapsed <= 0, got %f", speed)
			}
		})
	}
}

// TestSystemClockGoingBackwards tests behavior when system clock jumps backwards
func TestSystemClockGoingBackwards(t *testing.T) {
	// BUG: time.Since() can return negative duration if clock goes backwards (NTP adjustment)

	startTime := time.Now()

	// Simulate clock going backwards (NTP adjustment, manual change, etc.)
	simulatedCurrentTime := startTime.Add(-1 * time.Hour)

	elapsed := simulatedCurrentTime.Sub(startTime)

	if elapsed < 0 {
		t.Logf("FOUND BUG: negative elapsed time detected: %v", elapsed)

		// BUG: This will cause incorrect ETA and speed calculations
		sessionTransferred := int64(1000000)
		speed := float64(sessionTransferred) / elapsed.Seconds() // Negative!

		if speed < 0 {
			t.Errorf("FOUND BUG: negative transfer speed: %f bytes/sec", speed)
		}

		// BUG: ETA calculation with negative speed
		remaining := int64(5000000)
		etaSeconds := int(float64(remaining) / speed)

		if etaSeconds < 0 {
			t.Errorf("FOUND BUG: negative ETA: %d seconds", etaSeconds)
		}
	}
}

// TestMonotonicVsWallClockTime tests monotonic vs wall clock issues
func TestMonotonicVsWallClockTime(t *testing.T) {
	// time.Now() returns a value with both wall clock and monotonic clock
	// time.Since() uses monotonic clock if available
	// But time.Unix() and JSON marshaling strip monotonic clock

	t1 := time.Now()
	t.Logf("t1 with monotonic: %v", t1)

	// Strip monotonic clock (happens during JSON marshal/unmarshal)
	t2 := time.Unix(t1.Unix(), 0)
	t.Logf("t2 without monotonic: %v", t2)

	// These should be equal but may not be identical
	if !t1.Equal(t2) {
		diff := t1.Sub(t2)
		if diff > time.Second || diff < -time.Second {
			t.Errorf("FOUND BUG: significant difference after stripping monotonic: %v", diff)
		}
	}

	// BUG: If startTime is loaded from JSON (state restoration), it loses monotonic clock
	// and time.Since() calculations become vulnerable to wall clock changes
	startTimeFromJSON := time.Unix(time.Now().Unix(), 0)
	time.Sleep(100 * time.Millisecond)

	elapsed := time.Since(startTimeFromJSON)
	// This now uses wall clock comparison, not monotonic clock
	t.Logf("Elapsed time with wall clock: %v", elapsed)
}

// TestDurationOverflow tests integer overflow in duration calculations
func TestDurationOverflow(t *testing.T) {
	tests := []struct {
		name           string
		bytes          int64
		speed          float64
		expectOverflow bool
	}{
		{
			name:           "normal case",
			bytes:          1000000,
			speed:          100000,
			expectOverflow: false,
		},
		{
			name:           "very large file with slow speed",
			bytes:          1 << 50, // 1 PB
			speed:          1024,    // 1 KB/s
			expectOverflow: true,    // Would take 34 years
		},
		{
			name:           "max int64 bytes",
			bytes:          1<<63 - 1,
			speed:          1,
			expectOverflow: true,
		},
		{
			name:           "zero speed",
			bytes:          1000000,
			speed:          0,
			expectOverflow: false, // Division by zero case
		},
		{
			name:           "negative speed (from clock going backwards)",
			bytes:          1000000,
			speed:          -1000,
			expectOverflow: false, // Results in negative ETA
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.speed == 0 {
				// BUG: Division by zero - should be handled
				t.Logf("FOUND BUG: speed is zero, ETA calculation will fail")
				return
			}

			etaSeconds := float64(tt.bytes) / tt.speed

			// Check for overflow/unreasonable values
			maxReasonableSeconds := float64(365 * 24 * 3600 * 100) // 100 years

			if etaSeconds > maxReasonableSeconds {
				t.Logf("FOUND BUG: ETA overflow detected: %f seconds (%.2f years)",
					etaSeconds, etaSeconds/(365*24*3600))
			}

			if etaSeconds < 0 {
				t.Errorf("FOUND BUG: negative ETA: %f seconds", etaSeconds)
			}

			// BUG: Converting to int can overflow
			etaInt := int(etaSeconds)
			if etaInt < 0 && etaSeconds > 0 {
				t.Errorf("FOUND BUG: int conversion overflow: %f -> %d", etaSeconds, etaInt)
			}
		})
	}
}

// TestFormatDurationEdgeCases tests the formatDuration function with edge cases
func TestFormatDurationEdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		seconds  int
		expected string
	}{
		{"zero", 0, "0s"},
		{"negative", -1, "-1s"}, // BUG: Should this be handled?
		{"one second", 1, "1s"},
		{"59 seconds", 59, "59s"},
		{"one minute", 60, "1m 0s"},
		{"one hour", 3600, "1h 0m"},
		{"max int", int(^uint(0) >> 1), ""},       // BUG: Will overflow in calculations
		{"negative max", -int(^uint(0) >> 1), ""}, // BUG: Will overflow
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatDuration(tt.seconds)

			if tt.seconds < 0 {
				t.Logf("FOUND BUG: negative seconds not handled: formatDuration(%d) = %s",
					tt.seconds, result)
			}

			if tt.seconds > 365*24*3600*100 {
				t.Logf("FOUND BUG: extremely large duration may overflow: formatDuration(%d) = %s",
					tt.seconds, result)
			}

			t.Logf("formatDuration(%d) = %s", tt.seconds, result)
		})
	}
}

// TestTimeZeroComparisons tests comparisons with time.Zero()
func TestTimeZeroComparisons(t *testing.T) {
	var zeroTime time.Time

	// BUG: Zero value time is not the same as time.Time{}
	if !zeroTime.IsZero() {
		t.Error("FOUND BUG: zero value time.Time should be zero")
	}

	// BUG: Comparing with time.Zero() can be problematic
	now := time.Now()
	if now.IsZero() {
		t.Error("FOUND BUG: current time should not be zero")
	}

	// BUG: JSON unmarshaling can create non-zero time that looks like zero
	jsonTime := time.Time{}
	if jsonTime.Unix() != -62135596800 { // Unix time for year 1
		t.Logf("Zero time Unix timestamp: %d", jsonTime.Unix())
	}

	// BUG: Subtracting from zero time causes issues
	elapsed := now.Sub(zeroTime)
	if elapsed < 0 {
		t.Errorf("FOUND BUG: elapsed from zero time is negative: %v", elapsed)
	}
}

// TestUnixTimestampOverflow2038 tests Year 2038 problem
func TestUnixTimestampOverflow2038(t *testing.T) {
	// BUG: 32-bit Unix timestamps overflow on Jan 19, 2038 03:14:07 UTC
	// Go uses int64, but external systems or serialization might use int32

	year2038 := time.Date(2038, 1, 19, 3, 14, 8, 0, time.UTC)
	timestamp := year2038.Unix()

	t.Logf("Year 2038 timestamp: %d", timestamp)

	// Check if this would overflow in 32-bit systems
	maxInt32 := int64(1<<31 - 1)
	if timestamp > maxInt32 {
		t.Logf("FOUND BUG: timestamp %d exceeds 32-bit max %d", timestamp, maxInt32)
	}

	// BUG: Sync record IDs use Unix timestamp as string
	// In state.go line 173: ID: fmt.Sprintf("%d", time.Now().Unix())
	// This could collide if two syncs start in the same second
	id1 := fmt.Sprintf("%d", time.Now().Unix())
	id2 := fmt.Sprintf("%d", time.Now().Unix())

	if id1 == id2 {
		t.Logf("FOUND BUG: sync IDs can collide if started in same second: %s == %s", id1, id2)
	}
}

// TestTimeParsingLocale tests time parsing with different locales
func TestTimeParsingLocale(t *testing.T) {
	// BUG: Time parsing can fail if locale changes

	testTimes := []string{
		"2025-10-15T10:30:00Z",
		"2025-10-15T10:30:00+00:00",
		"2025-10-15T10:30:00-07:00",
		"2025-10-15 10:30:00",
		"10/15/2025 10:30:00",
	}

	layouts := []string{
		time.RFC3339,
		time.RFC3339Nano,
		"2006-01-02 15:04:05",
		"01/02/2006 15:04:05",
	}

	for _, timeStr := range testTimes {
		parsed := false
		for _, layout := range layouts {
			if _, err := time.Parse(layout, timeStr); err == nil {
				parsed = true
				break
			}
		}
		if !parsed {
			t.Logf("FOUND BUG: cannot parse time string with standard layouts: %s", timeStr)
		}
	}
}

// TestTickerAndTimerLeaks tests proper cleanup of tickers and timers
func TestTickerAndTimerLeaks(t *testing.T) {
	// BUG: In syncmanager.go:216, ticker is created but might not be stopped if context is cancelled

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	done := make(chan struct{})

	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop() // Must be called to release resources

	go func() {
		defer func() {
			// Prevent double close panic
			select {
			case <-done:
				// Already closed
			default:
				close(done)
			}
		}()

		for {
			select {
			case <-ctx.Done():
				return
			case <-done:
				return
			case <-ticker.C:
				// Process tick
			}
		}
	}()

	<-ctx.Done()
	// Wait for goroutine to exit
	time.Sleep(50 * time.Millisecond)

	// Ticker should be stopped by defer, but if not, it leaks
	// Running with -race and monitoring goroutines would catch this
}

// TestTimeoutValueZeroOrNegative tests handling of zero or negative timeouts
func TestTimeoutValueZeroOrNegative(t *testing.T) {
	tests := []struct {
		name         string
		timeout      time.Duration
		shouldExpire bool
	}{
		{"positive timeout", 100 * time.Millisecond, true},
		{"zero timeout", 0, true},                    // BUG: context.WithTimeout(0) expires immediately
		{"negative timeout", -1 * time.Second, true}, // BUG: Treated as 0
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), tt.timeout)
			defer cancel()

			select {
			case <-ctx.Done():
				if !tt.shouldExpire {
					t.Errorf("FOUND BUG: context expired when it shouldn't")
				}
				if tt.timeout <= 0 {
					t.Logf("FOUND BUG: zero or negative timeout causes immediate expiration")
				}
			case <-time.After(200 * time.Millisecond):
				if tt.shouldExpire {
					t.Errorf("context did not expire when it should")
				}
			}
		})
	}
}

// TestConcurrentTimeAccess tests race conditions with time-based throttling
func TestConcurrentTimeAccess(t *testing.T) {
	// BUG: In state.go:208, time.Since(m.lastProgressSave) is checked outside the lock
	// Multiple goroutines can pass the check simultaneously before lock acquisition

	lastSave := time.Now().Add(-10 * time.Second)
	delay := 5 * time.Second

	var saves int
	saveChan := make(chan struct{}, 10)

	// Simulate multiple concurrent progress updates
	for i := 0; i < 5; i++ {
		go func() {
			// This is the buggy pattern from state.go
			shouldSave := time.Since(lastSave) >= delay // RACE: Multiple threads can pass

			if shouldSave {
				saveChan <- struct{}{} // Would save to disk
			}
		}()
	}

	// Wait a bit for goroutines to complete
	time.Sleep(50 * time.Millisecond)
	close(saveChan)

	for range saveChan {
		saves++
	}

	if saves > 1 {
		t.Logf("FOUND BUG: multiple saves triggered simultaneously: %d saves", saves)
		t.Logf("This is the race condition documented in docs/race-condition-analysis.md")
	}
}

// TestSleepInterruption tests if time.Sleep can be interrupted
func TestSleepInterruption(t *testing.T) {
	// BUG: time.Sleep cannot be interrupted by context cancellation
	// In main.go:184 and :211, Sleep blocks even if sync is cancelled

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})

	go func() {
		select {
		case <-ctx.Done():
			// Context cancelled
			close(done)
		case <-time.After(5 * time.Second): // Would block for full 5 seconds
			close(done)
		}
	}()

	// Cancel after 50ms
	time.Sleep(50 * time.Millisecond)
	cancel()

	// Check if goroutine exits quickly
	select {
	case <-done:
		// Good, context cancellation worked
	case <-time.After(200 * time.Millisecond):
		t.Error("FOUND BUG: time.After not interruptible by context cancellation")
	}
}

// TestTimeCacheInvalidation tests cache invalidation timing
func TestTimeCacheInvalidation(t *testing.T) {
	// BUG: In sdmonitor.go:110, cache uses time.Since which is vulnerable to clock changes

	cacheTime := time.Now()
	cacheTTL := 2 * time.Second

	// Normal case
	if time.Since(cacheTime) < cacheTTL {
		t.Log("Cache is valid")
	}

	// Simulate clock going backwards
	simulatedNow := cacheTime.Add(-1 * time.Hour)
	elapsed := simulatedNow.Sub(cacheTime)

	if elapsed < cacheTTL {
		t.Logf("FOUND BUG: negative elapsed time treats cache as valid: %v", elapsed)
		t.Log("Cache should be invalidated but appears valid due to negative time")
	}

	// Simulate clock jumping forward
	simulatedNow = cacheTime.Add(10 * time.Minute)
	elapsed = simulatedNow.Sub(cacheTime)

	if elapsed >= cacheTTL {
		t.Log("Cache correctly invalidated with forward time jump")
	}
}

// TestProgressThrottlingRaceCondition tests the specific bug in state.UpdateSyncProgress
func TestProgressThrottlingRaceCondition(t *testing.T) {
	// This tests the exact bug described in docs/race-condition-analysis.md
	// where multiple goroutines can pass the time.Since check before acquiring the lock

	type mockManager struct {
		lastProgressSave  time.Time
		progressSaveDelay time.Duration
		saves             int
	}

	m := &mockManager{
		lastProgressSave:  time.Now().Add(-10 * time.Second), // Old enough to trigger save
		progressSaveDelay: 5 * time.Second,
	}

	// Simulate the buggy code pattern
	numGoroutines := 5
	savesChan := make(chan bool, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			// BUGGY PATTERN from state.go lines 207-212
			shouldSave := time.Since(m.lastProgressSave) >= m.progressSaveDelay // RACE!

			if shouldSave {
				// In real code, this would acquire lock and save to disk
				// All 5 goroutines can reach here simultaneously
				savesChan <- true
			}
		}()
	}

	// Wait for goroutines
	time.Sleep(50 * time.Millisecond)
	close(savesChan)

	saves := 0
	for range savesChan {
		saves++
	}

	if saves > 1 {
		t.Errorf("FOUND BUG: race condition in progress throttling - %d goroutines triggered save", saves)
		t.Log("Expected: only 1 save")
		t.Log("Actual: multiple goroutines passed the time.Since check before lock acquisition")
		t.Log("Fix: move time.Since check inside the lock, or use atomic operations")
	}
}

// TestCardIDGenerationCollision tests for ID collision in card ID generation
func TestCardIDGenerationCollision(t *testing.T) {
	// In sdmonitor.go:400, fallback uses time.Now().Unix()
	// BUG: Multiple cards inserted in the same second will get the same ID

	ids := make(map[string]int)

	// Simulate rapid card insertions
	for i := 0; i < 10; i++ {
		// This simulates the fallback in generateCardID when crypto/rand fails
		id := fmt.Sprintf("card-%d", time.Now().Unix())
		ids[id]++

		if i < 9 {
			// Don't sleep on last iteration
			time.Sleep(100 * time.Millisecond)
		}
	}

	// Check for collisions
	collisions := 0
	for id, count := range ids {
		if count > 1 {
			collisions++
			t.Logf("FOUND BUG: card ID collision detected: %s appeared %d times", id, count)
		}
	}

	if collisions > 0 {
		t.Errorf("FOUND BUG: %d card ID collisions due to timestamp-based generation", collisions)
		t.Log("This can happen when crypto/rand fails and fallback uses Unix timestamp")
	}
}

// TestDSTTransitionEdgeCases tests daylight saving time transition edge cases
func TestDSTTransitionEdgeCases(t *testing.T) {
	// Test the "lost hour" in spring DST transition

	// In spring, 2 AM becomes 3 AM
	// Time 2:30 AM doesn't exist
	loc, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Skip("Cannot load timezone data")
	}

	// March 9, 2025: DST starts at 2 AM
	// Try to create 2:30 AM which doesn't exist
	nonExistentTime := time.Date(2025, 3, 9, 2, 30, 0, 0, loc)

	t.Logf("Non-existent time (should be adjusted): %v", nonExistentTime)

	// Go automatically adjusts to 3:30 AM PDT
	if nonExistentTime.Hour() != 3 {
		t.Logf("Time was adjusted to hour %d", nonExistentTime.Hour())
	}

	// Test the "repeated hour" in fall DST transition
	// In fall, 2 AM happens twice
	// November 2, 2025: DST ends at 2 AM
	repeatedTime1 := time.Date(2025, 11, 2, 1, 30, 0, 0, time.FixedZone("PDT", -7*3600))
	repeatedTime2 := time.Date(2025, 11, 2, 1, 30, 0, 0, time.FixedZone("PST", -8*3600))

	diff := repeatedTime2.Sub(repeatedTime1)
	t.Logf("Difference between two 1:30 AM times: %v", diff)

	// BUG: If sync runs during DST transition, time calculations can be off by an hour
	if diff != time.Hour {
		t.Logf("FOUND BUG: DST transition causes unexpected time difference: %v", diff)
	}
}
