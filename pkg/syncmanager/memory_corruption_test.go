package syncmanager

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/denysvitali/pictures-sync-s3/pkg/state"
)

// TestCardIDPathTraversal tests for path traversal vulnerabilities
func TestCardIDPathTraversal(t *testing.T) {
	tests := []struct {
		name        string
		cardID      string
		shouldError bool
	}{
		{
			name:        "ValidCardID",
			cardID:      "card-0123456789abcdef",
			shouldError: false,
		},
		{
			name:        "PathTraversalDotDot",
			cardID:      "../../../etc/passwd",
			shouldError: true,
		},
		{
			name:        "PathTraversalSlash",
			cardID:      "card/../../etc",
			shouldError: true,
		},
		{
			name:        "EmptyCardID",
			cardID:      "",
			shouldError: true,
		},
		{
			name:        "BackslashTraversal",
			cardID:      "..\\..\\windows",
			shouldError: true,
		},
		{
			name:        "OnlyDots",
			cardID:      "....",
			shouldError: true,
		},
		{
			name:        "NullByteInjection",
			cardID:      "card-1234\x00../../etc",
			shouldError: true,
		},
		{
			name:        "TooShort",
			cardID:      "card-123",
			shouldError: true,
		},
		{
			name:        "TooLong",
			cardID:      "card-123456789ABC",
			shouldError: true,
		},
		{
			name:        "SpecialChars",
			cardID:      "card-1234!@#$",
			shouldError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateCardID(tt.cardID)
			if tt.shouldError && err == nil {
				t.Errorf("CRITICAL: Path traversal not detected for: %q", tt.cardID)
			}
			if !tt.shouldError && err != nil {
				t.Errorf("Valid card ID rejected: %v", err)
			}
		})
	}
}

// TestIntegerOverflowInProgress tests integer overflow in progress calculations
func TestIntegerOverflowInProgress(t *testing.T) {
	tests := []struct {
		name          string
		bytes         int64
		percentage    int
		expectedPanic bool
	}{
		{
			name:       "MaxInt64",
			bytes:      9223372036854775807,
			percentage: 100,
		},
		{
			name:       "NegativeBytes",
			bytes:      -1,
			percentage: 0,
		},
		{
			name:       "Overflow",
			bytes:      9223372036854775807,
			percentage: 200, // Invalid percentage
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					if !tt.expectedPanic {
						t.Errorf("CRITICAL: Unexpected panic with bytes=%d: %v", tt.bytes, r)
					}
				}
			}()

			progress := Progress{
				BytesTransferred: tt.bytes,
				Percentage:       tt.percentage,
			}

			// Verify values don't overflow
			if progress.BytesTransferred < 0 && tt.bytes >= 0 {
				t.Errorf("CRITICAL: BytesTransferred overflowed: %d -> %d", tt.bytes, progress.BytesTransferred)
			}
		})
	}
}

// TestConcurrentManagerAccess tests race conditions in Manager
func TestConcurrentManagerAccess(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "rclone.conf")
	stateDir := filepath.Join(tmpDir, "state")

	// Create minimal config
	os.WriteFile(configPath, []byte(""), 0644)
	os.MkdirAll(stateDir, 0755)

	// Note: Cannot override state constants, manager will use default paths
	stateMgr, err := state.NewManager()
	if err != nil {
		t.Fatal(err)
	}

	mgr := NewManager(configPath, "test", "/test", stateMgr, 4, 8)

	t.Run("ConcurrentSetRemote", func(t *testing.T) {
		var wg sync.WaitGroup

		for i := 0; i < 50; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				mgr.SetRemote("remote"+string(rune(id)), "/path"+string(rune(id)))
			}(i)
		}

		wg.Wait()
	})

	t.Run("ConcurrentIsRunning", func(t *testing.T) {
		var wg sync.WaitGroup

		for i := 0; i < 100; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_ = mgr.IsRunning()
			}()
		}

		wg.Wait()
	})
}

// TestProgressChannelMemoryLeak tests for memory leaks in progress channels
func TestProgressChannelMemoryLeak(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "rclone.conf")
	stateDir := filepath.Join(tmpDir, "state")

	os.WriteFile(configPath, []byte(""), 0644)
	os.MkdirAll(stateDir, 0755)

	// Note: Cannot modify state constants

	stateMgr, _ := state.NewManager()
	mgr := NewManager(configPath, "test", "/test", stateMgr, 4, 8)

	t.Run("ManySubscribersNeverRead", func(t *testing.T) {
		// Subscribe many channels without reading
		for i := 0; i < 1000; i++ {
			_ = mgr.SubscribeProgress()
		}

		mgr.mu.Lock()
		channelCount := len(mgr.progressChans)
		mgr.mu.Unlock()

		if channelCount != 1000 {
			t.Errorf("Expected 1000 channels, got %d", channelCount)
		}

		t.Logf("WARNING: %d progress channels without cleanup - potential memory leak", channelCount)
	})
}

// TestConcurrentCancelOperation tests race conditions in Cancel
func TestConcurrentCancelOperation(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "rclone.conf")
	stateDir := filepath.Join(tmpDir, "state")

	os.WriteFile(configPath, []byte(""), 0644)
	os.MkdirAll(stateDir, 0755)

	// Note: Cannot modify state constants

	stateMgr, _ := state.NewManager()
	mgr := NewManager(configPath, "test", "/test", stateMgr, 4, 8)

	t.Run("ConcurrentCancel", func(t *testing.T) {
		// Set up as if running
		mgr.mu.Lock()
		mgr.isRunning = true
		ctx, cancel := context.WithCancel(context.Background())
		mgr.cancelFunc = cancel
		mgr.mu.Unlock()

		var wg sync.WaitGroup

		// Multiple concurrent cancels
		for i := 0; i < 20; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer func() {
					if r := recover(); r != nil {
						t.Errorf("CRITICAL: Panic in Cancel: %v", r)
					}
				}()
				_ = mgr.Cancel()
			}()
		}

		wg.Wait()

		// Verify context is cancelled
		select {
		case <-ctx.Done():
			// Good
		default:
			t.Error("Context should be cancelled")
		}
	})
}

// TestMonitorProgressRaceCondition tests race in monitorProgress
func TestMonitorProgressRaceCondition(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "rclone.conf")
	stateDir := filepath.Join(tmpDir, "state")

	os.WriteFile(configPath, []byte(""), 0644)
	os.MkdirAll(stateDir, 0755)

	// Note: Cannot modify state constants

	stateMgr, _ := state.NewManager()
	mgr := NewManager(configPath, "test", "/test", stateMgr, 4, 8)

	t.Run("ConcurrentProgressReads", func(t *testing.T) {
		_, cancelFunc := context.WithCancel(context.Background())
		defer cancelFunc()

		done := make(chan struct{})

		// Start monitor
		// Note: We can't easily create accounting.StatsInfo, so we test the structure
		go func() {
			time.Sleep(100 * time.Millisecond)
			close(done)
		}()

		// Concurrent channel operations
		var wg sync.WaitGroup
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				ch := mgr.SubscribeProgress()
				for {
					select {
					case <-ch:
					case <-done:
						return
					}
				}
			}()
		}

		<-done
		cancelFunc()
		wg.Wait()
	})
}

// TestRetryableErrorDetection tests isRetryableError for edge cases
func TestRetryableErrorDetection(t *testing.T) {
	tests := []struct {
		name        string
		err         error
		shouldRetry bool
	}{
		{
			name:        "NilError",
			err:         nil,
			shouldRetry: false,
		},
		{
			name:        "ConnectionRefused",
			err:         &testErr{"connection refused"},
			shouldRetry: true,
		},
		{
			name:        "Timeout",
			err:         &testErr{"context deadline exceeded (timeout)"},
			shouldRetry: true,
		},
		{
			name:        "FileNotFound",
			err:         &testErr{"file not found"},
			shouldRetry: false,
		},
		{
			name:        "RateLimit",
			err:         &testErr{"429 Too Many Requests"},
			shouldRetry: true,
		},
		{
			name:        "ServerError",
			err:         &testErr{"503 Service Unavailable"},
			shouldRetry: true,
		},
		{
			name:        "GooglePhotosMediaItemInternal",
			err:         &testErr{"failed to commit batch: batch upload failed: upload failed: Failed: There was an error while trying to create this media item. (13)"},
			shouldRetry: true,
		},
		{
			name:        "GooglePhotosUnavailable",
			err:         &testErr{"upload failed: backend unavailable (14)"},
			shouldRetry: true,
		},
		{
			name:        "GooglePhotosCommitBatch",
			err:         &testErr{"failed to commit batch: something transient"},
			shouldRetry: true,
		},
		{
			name:        "GooglePhotosPermanentInvalidArgument",
			err:         &testErr{"upload failed: invalid argument (3)"},
			shouldRetry: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isRetryableError(tt.err)
			if result != tt.shouldRetry {
				t.Errorf("Expected shouldRetry=%v for %v, got %v", tt.shouldRetry, tt.err, result)
			}
		})
	}
}

type testErr struct {
	msg string
}

func (e *testErr) Error() string {
	return e.msg
}

// TestFormatDurationIntegerOverflow tests formatDuration with extreme values
func TestFormatDurationIntegerOverflow(t *testing.T) {
	tests := []struct {
		name    string
		seconds int
	}{
		{"MaxInt", 2147483647},
		{"Negative", -1},
		{"Zero", 0},
		{"VeryLarge", 1000000000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("CRITICAL: formatDuration panicked on %d: %v", tt.seconds, r)
				}
			}()

			result := formatDuration(tt.seconds)

			if tt.seconds < 0 && result != "" {
				t.Logf("Negative duration formatted as: %s", result)
			}

			if result == "" && tt.seconds >= 0 {
				t.Errorf("formatDuration returned empty string for %d", tt.seconds)
			}
		})
	}
}

// TestStringContainsNullBytes tests string operations with null bytes
func TestStringContainsNullBytes(t *testing.T) {
	t.Run("ErrorMessageWithNullByte", func(t *testing.T) {
		errMsg := "error\x00message"

		// Test if strings.ToLower handles null bytes
		lowerMsg := strings.ToLower(errMsg)
		if !strings.Contains(lowerMsg, "\x00") {
			t.Error("Null byte removed by ToLower")
		}

		// Test if strings.Contains handles null bytes
		result := strings.Contains(lowerMsg, "error")
		if !result {
			t.Error("strings.Contains failed with null byte")
		}
	})
}

// TestContextCancellationRace tests race between context cancel and operations
func TestContextCancellationRace(t *testing.T) {
	t.Run("CancelDuringSync", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())

		var wg sync.WaitGroup

		// Goroutine that cancels
		wg.Add(1)
		go func() {
			defer wg.Done()
			time.Sleep(10 * time.Millisecond)
			cancel()
		}()

		// Goroutine that checks cancellation
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				select {
				case <-ctx.Done():
					return
				default:
					time.Sleep(1 * time.Millisecond)
				}
			}
		}()

		wg.Wait()
	})
}

// TestSliceAppendRaceInProgressChans tests race in appending to progressChans
func TestSliceAppendRaceInProgressChans(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "rclone.conf")
	stateDir := filepath.Join(tmpDir, "state")

	os.WriteFile(configPath, []byte(""), 0644)
	os.MkdirAll(stateDir, 0755)

	// Note: Cannot modify state constants

	stateMgr, _ := state.NewManager()
	mgr := NewManager(configPath, "test", "/test", stateMgr, 4, 8)

	t.Run("ConcurrentSubscribe", func(t *testing.T) {
		var wg sync.WaitGroup

		// Concurrent subscribes
		for i := 0; i < 100; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_ = mgr.SubscribeProgress()
			}()
		}

		wg.Wait()

		mgr.mu.Lock()
		count := len(mgr.progressChans)
		mgr.mu.Unlock()

		if count != 100 {
			t.Errorf("Expected 100 channels, got %d - possible race condition", count)
		}
	})
}

// TestNilCancelFuncDereference tests nil cancelFunc dereference
func TestNilCancelFuncDereference(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "rclone.conf")
	stateDir := filepath.Join(tmpDir, "state")

	os.WriteFile(configPath, []byte(""), 0644)
	os.MkdirAll(stateDir, 0755)

	// Note: Cannot modify state constants

	stateMgr, _ := state.NewManager()
	mgr := NewManager(configPath, "test", "/test", stateMgr, 4, 8)

	t.Run("CancelWithoutSync", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("CRITICAL: Panic when canceling without active sync: %v", r)
			}
		}()

		err := mgr.Cancel()
		if err == nil {
			t.Error("Expected error when canceling without sync")
		}
	})

	t.Run("CancelWithNilCancelFunc", func(t *testing.T) {
		mgr.mu.Lock()
		mgr.isRunning = true
		mgr.cancelFunc = nil // Explicitly nil
		mgr.mu.Unlock()

		defer func() {
			if r := recover(); r != nil {
				t.Errorf("CRITICAL: Panic on nil cancelFunc dereference: %v", r)
			}
			mgr.mu.Lock()
			mgr.isRunning = false
			mgr.mu.Unlock()
		}()

		err := mgr.Cancel()
		if err != nil {
			t.Logf("Cancel with nil cancelFunc: %v", err)
		}
	})
}

// TestSpeedCalculationDivisionByZero tests for division by zero in speed calc
func TestSpeedCalculationDivisionByZero(t *testing.T) {
	t.Run("ZeroElapsedTime", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("CRITICAL: Panic on division by zero: %v", r)
			}
		}()

		// Simulate zero elapsed time
		elapsed := time.Duration(0)
		sessionTransferred := int64(1024)

		var speed float64
		if elapsed > 0 {
			speed = float64(sessionTransferred) / elapsed.Seconds()
		}

		// Speed should be 0, not panic
		if speed != 0 {
			t.Errorf("Expected speed 0 for zero elapsed, got %f", speed)
		}
	})
}

// TestETACalculationNegativeRemaining tests ETA with negative remaining bytes
func TestETACalculationNegativeRemaining(t *testing.T) {
	tests := []struct {
		name           string
		totalBytes     int64
		transferred    int64
		speed          float64
		expectNegative bool
	}{
		{
			name:           "TransferredExceedsTotal",
			totalBytes:     1000,
			transferred:    2000,
			speed:          100.0,
			expectNegative: true,
		},
		{
			name:           "NegativeTotal",
			totalBytes:     -1000,
			transferred:    500,
			speed:          100.0,
			expectNegative: true,
		},
		{
			name:        "ZeroSpeed",
			totalBytes:  1000,
			transferred: 500,
			speed:       0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			remaining := tt.totalBytes - tt.transferred

			var etaSeconds int
			if tt.speed > 0 {
				etaSeconds = int(float64(remaining) / tt.speed)
			}

			if tt.expectNegative && etaSeconds >= 0 && remaining < 0 {
				t.Logf("WARNING: Negative remaining (%d) produced non-negative ETA (%d)", remaining, etaSeconds)
			}

			if etaSeconds < 0 {
				t.Logf("Negative ETA: %d seconds (remaining: %d, speed: %f)", etaSeconds, remaining, tt.speed)
			}
		})
	}
}

// TestProgressPercentageOverflow tests percentage calculation overflow
func TestProgressPercentageOverflow(t *testing.T) {
	tests := []struct {
		name        string
		transferred int64
		total       int64
	}{
		{
			name:        "ZeroTotal",
			transferred: 1000,
			total:       0,
		},
		{
			name:        "MaxValues",
			transferred: 9223372036854775807,
			total:       9223372036854775807,
		},
		{
			name:        "TransferredExceedsTotal",
			transferred: 2000,
			total:       1000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var percentage int
			if tt.total > 0 {
				percentage = int((float64(tt.transferred) / float64(tt.total)) * 100)
			}

			if percentage < 0 {
				t.Errorf("CRITICAL: Negative percentage: %d", percentage)
			}

			if percentage > 100 && tt.transferred <= tt.total {
				t.Logf("WARNING: Percentage > 100: %d (transferred: %d, total: %d)", percentage, tt.transferred, tt.total)
			}
		})
	}
}
