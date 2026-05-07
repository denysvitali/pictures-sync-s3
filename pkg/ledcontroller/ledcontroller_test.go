package ledcontroller

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/denysvitali/pictures-sync-s3/pkg/state"
)

// MockStateManager implements a minimal state manager for testing
// We'll wrap the controller's Start method instead of trying to mock state.Manager
type MockStateManager struct {
	mu        sync.RWMutex
	status    state.SyncStatus
	listeners []chan state.CurrentState
}

func NewMockStateManager() *MockStateManager {
	return &MockStateManager{
		status:    state.StatusIdle,
		listeners: make([]chan state.CurrentState, 0),
	}
}

func (m *MockStateManager) SetStatus(status state.SyncStatus) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.status = status
	currentState := state.CurrentState{Status: status}
	// Send under the lock so Unsubscribe cannot close a channel mid-send.
	// Sends are non-blocking (default case skips full channels), so
	// holding the lock here doesn't risk deadlock.
	for _, ch := range m.listeners {
		select {
		case ch <- currentState:
		default:
		}
	}
	return nil
}

func (m *MockStateManager) GetState() state.CurrentState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return state.CurrentState{Status: m.status}
}

func (m *MockStateManager) Subscribe() chan state.CurrentState {
	m.mu.Lock()
	defer m.mu.Unlock()
	ch := make(chan state.CurrentState, 10)
	m.listeners = append(m.listeners, ch)
	return ch
}

func (m *MockStateManager) Unsubscribe(ch chan state.CurrentState) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, listener := range m.listeners {
		if listener == ch {
			m.listeners = append(m.listeners[:i], m.listeners[i+1:]...)
			close(ch)
			break
		}
	}
}

// StartMocked starts the controller with a mock state manager
func (c *Controller) StartMocked(m *MockStateManager) error {
	if c.actLED == nil || !c.actLED.available {
		return nil
	}

	c.stateUpdates = m.Subscribe()

	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		var currentStatus state.SyncStatus
		for {
			select {
			case <-c.stopChan:
				return
			case newState := <-c.stateUpdates:
				if newState.Status != currentStatus {
					currentStatus = newState.Status
					c.handleStatusChange(currentStatus)
				}
			}
		}
	}()

	return nil
}

// TestLED implements the LED interface for testing
type TestLED struct {
	mu             sync.RWMutex
	brightness     int
	writeCount     int
	lastWrite      time.Time
	writeDurations []time.Duration
	failNextWrite  bool
	writeError     error
	writeHistory   []int // Track all brightness values written
}

func (t *TestLED) SetBrightness(value int) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.lastWrite = time.Now()
	t.writeCount++
	t.brightness = value
	t.writeHistory = append(t.writeHistory, value)

	if t.failNextWrite {
		t.failNextWrite = false
		if t.writeError != nil {
			return t.writeError
		}
		return fmt.Errorf("simulated write error")
	}

	return nil
}

func (t *TestLED) GetBrightness() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.brightness
}

func (t *TestLED) GetWriteCount() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.writeCount
}

func (t *TestLED) GetWriteHistory() []int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	history := make([]int, len(t.writeHistory))
	copy(history, t.writeHistory)
	return history
}

func (t *TestLED) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.brightness = 0
	t.writeCount = 0
	t.writeHistory = []int{}
}

// createTestController creates a controller with test LEDs
func createTestController() (*Controller, *TestLED) {
	testLED := &TestLED{
		writeHistory: make([]int, 0),
	}

	c := &Controller{
		actLED: &LED{
			name:           "TEST",
			brightnessPath: "/dev/null",
			available:      true,
		},
		stopChan: make(chan struct{}),
		wg:       sync.WaitGroup{},
	}

	return c, testLED
}

// createTempLEDPath creates temporary LED sysfs structure for testing
func createTempLEDPath(t *testing.T) (string, func()) {
	tmpDir, err := os.MkdirTemp("", "ledtest-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	ledPath := filepath.Join(tmpDir, "sys", "class", "leds", "TEST")
	if err := os.MkdirAll(ledPath, 0755); err != nil {
		t.Fatalf("Failed to create LED path: %v", err)
	}

	brightnessFile := filepath.Join(ledPath, "brightness")
	if err := os.WriteFile(brightnessFile, []byte("0"), 0644); err != nil {
		t.Fatalf("Failed to create brightness file: %v", err)
	}

	cleanup := func() {
		os.RemoveAll(tmpDir)
	}

	return brightnessFile, cleanup
}

// countGoroutines returns the number of goroutines
func countGoroutines() int {
	return runtime.NumGoroutine()
}

// waitForGoroutineDecrease waits for goroutines to decrease
func waitForGoroutineDecrease(initialCount int, timeout time.Duration) (int, bool) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		current := countGoroutines()
		if current < initialCount {
			return current, true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return countGoroutines(), false
}

// Test 1: Rapid state transitions should not leak goroutines
func TestRapidStateTransitions(t *testing.T) {
	mock := NewMockStateManager()
	c, _ := createTestController()

	initialGoroutines := countGoroutines()
	t.Logf("Initial goroutines: %d", initialGoroutines)

	err := c.StartMocked(mock)
	if err != nil {
		t.Fatalf("Failed to start controller: %v", err)
	}

	// Rapid fire state transitions
	states := []state.SyncStatus{
		state.StatusDetected,
		state.StatusSyncing,
		state.StatusSuccess,
		state.StatusIdle,
		state.StatusDetected,
		state.StatusSyncing,
		state.StatusError,
		state.StatusIdle,
	}

	for i := 0; i < 50; i++ {
		for _, status := range states {
			mock.SetStatus(status)
			time.Sleep(5 * time.Millisecond) // Very rapid transitions
		}
	}

	// Stop controller
	c.Stop()

	// Wait for goroutines to clean up
	time.Sleep(200 * time.Millisecond)
	runtime.GC()
	time.Sleep(100 * time.Millisecond)

	finalGoroutines := countGoroutines()
	t.Logf("Final goroutines: %d", finalGoroutines)

	// BUG FOUND: Goroutine leak expected here due to updatePattern creating
	// new goroutines without properly stopping old ones
	goroutineDiff := finalGoroutines - initialGoroutines
	if goroutineDiff > 5 {
		t.Errorf("GOROUTINE LEAK: Started with %d goroutines, ended with %d (diff: %d)",
			initialGoroutines, finalGoroutines, goroutineDiff)
	}
}

// Test 2: LED patterns not stopping when they should
func TestPatternNotStopping(t *testing.T) {
	mock := NewMockStateManager()

	brightnessFile, cleanup := createTempLEDPath(t)
	defer cleanup()

	c := &Controller{
		actLED: &LED{
			name:           "TEST",
			brightnessPath: brightnessFile,
			available:      true,
		},
		stopChan: make(chan struct{}),
		wg:       sync.WaitGroup{},
	}

	err := c.StartMocked(mock)
	if err != nil {
		t.Fatalf("Failed to start controller: %v", err)
	}

	// Start a blinking pattern
	mock.SetStatus(state.StatusSyncing)
	time.Sleep(300 * time.Millisecond)

	// Change to steady pattern. Allow enough time for the previous
	// blink goroutine to observe the status change and exit; on slow
	// CI runners 100 ms was racy.
	mock.SetStatus(state.StatusIdle)
	time.Sleep(500 * time.Millisecond)

	// Record initial brightness
	data, _ := os.ReadFile(brightnessFile)
	initialBrightness := string(data)

	// Wait and check if brightness changes (it shouldn't for steady pattern)
	time.Sleep(500 * time.Millisecond)
	data, _ = os.ReadFile(brightnessFile)
	finalBrightness := string(data)

	// BUG FOUND: Old pattern goroutines might still be running
	// causing unwanted brightness changes
	if initialBrightness != finalBrightness {
		t.Errorf("BUG: Pattern not stopped properly. Brightness changed from %s to %s",
			initialBrightness, finalBrightness)
	}

	c.Stop()
}

// Test 3: Multiple concurrent pattern changes
func TestConcurrentPatternChanges(t *testing.T) {
	mock := NewMockStateManager()

	c, _ := createTestController()
	err := c.StartMocked(mock)
	if err != nil {
		t.Fatalf("Failed to start controller: %v", err)
	}

	initialGoroutines := countGoroutines()

	// Hammer the controller with concurrent state changes
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			statuses := []state.SyncStatus{
				state.StatusIdle,
				state.StatusDetected,
				state.StatusSyncing,
				state.StatusSuccess,
				state.StatusError,
			}
			for j := 0; j < 20; j++ {
				mock.SetStatus(statuses[j%len(statuses)])
				time.Sleep(time.Duration(id+1) * time.Millisecond)
			}
		}(i)
	}

	wg.Wait()
	c.Stop()

	// Check for goroutine leaks
	time.Sleep(200 * time.Millisecond)
	runtime.GC()
	time.Sleep(100 * time.Millisecond)

	finalGoroutines := countGoroutines()
	goroutineDiff := finalGoroutines - initialGoroutines

	if goroutineDiff > 5 {
		t.Errorf("GOROUTINE LEAK: Concurrent changes leaked %d goroutines", goroutineDiff)
	}
}

// Test 4: Invalid LED paths or permissions
func TestInvalidLEDPaths(t *testing.T) {
	tests := []struct {
		name      string
		path      string
		available bool
		shouldErr bool
	}{
		{
			name:      "Non-existent path",
			path:      "/dev/null/nonexistent/led",
			available: true,
			shouldErr: true,
		},
		{
			name:      "No permission path",
			path:      "/root/led",
			available: true,
			shouldErr: true,
		},
		{
			name:      "Unavailable LED",
			path:      "/dev/null",
			available: false,
			shouldErr: false, // Should not error, just no-op
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			led := &LED{
				name:           "TEST",
				brightnessPath: tt.path,
				available:      tt.available,
			}

			err := led.SetBrightness(255)

			if tt.shouldErr && err == nil {
				t.Errorf("Expected error for path %s, got nil", tt.path)
			}

			if !tt.shouldErr && err != nil {
				t.Errorf("Unexpected error for path %s: %v", tt.path, err)
			}
		})
	}
}

// Test 5: State changes during active blink patterns
func TestStateChangeDuringBlink(t *testing.T) {
	mock := NewMockStateManager()

	brightnessFile, cleanup := createTempLEDPath(t)
	defer cleanup()

	c := &Controller{
		actLED: &LED{
			name:           "TEST",
			brightnessPath: brightnessFile,
			available:      true,
		},
		stopChan: make(chan struct{}),
		wg:       sync.WaitGroup{},
	}

	err := c.StartMocked(mock)
	if err != nil {
		t.Fatalf("Failed to start controller: %v", err)
	}

	// Start blinking
	mock.SetStatus(state.StatusSyncing)
	time.Sleep(150 * time.Millisecond) // Let it blink a bit

	// Change state mid-blink
	mock.SetStatus(state.StatusDetected)
	time.Sleep(50 * time.Millisecond)

	// Change again quickly
	mock.SetStatus(state.StatusIdle)
	time.Sleep(50 * time.Millisecond)

	// And again
	mock.SetStatus(state.StatusError)
	time.Sleep(150 * time.Millisecond)

	c.Stop()

	// Check if LED ended up in a consistent state
	data, _ := os.ReadFile(brightnessFile)
	brightness := string(data)

	// After Stop(), LED should be off (0)
	time.Sleep(50 * time.Millisecond)
	data, _ = os.ReadFile(brightnessFile)
	finalBrightness := string(data)

	t.Logf("Brightness before stop: %s, after stop: %s", brightness, finalBrightness)

	// BUG: Stop() might not properly wait for patterns to clean up
	if finalBrightness != "0" && finalBrightness != "0\n" {
		t.Errorf("BUG: LED not properly turned off after Stop(). Got brightness: %s", finalBrightness)
	}
}

// Test 6: Race conditions in goroutine management
func TestRaceConditionsInGoroutineManagement(t *testing.T) {
	// Run with -race flag to detect data races
	mock := NewMockStateManager()

	c, testLED := createTestController()

	// Override LED to track writes
	c.actLED = &LED{
		name:           "TEST",
		brightnessPath: "/dev/null",
		available:      true,
	}

	err := c.StartMocked(mock)
	if err != nil {
		t.Fatalf("Failed to start controller: %v", err)
	}

	// Create race conditions by accessing stopChan concurrently
	var wg sync.WaitGroup

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				status := state.StatusIdle
				if j%2 == 0 {
					status = state.StatusSyncing
				}
				mock.SetStatus(status)
				time.Sleep(time.Duration(id+1) * time.Millisecond)
			}
		}(i)
	}

	// Concurrently try to stop
	go func() {
		time.Sleep(50 * time.Millisecond)
		c.Stop()
	}()

	wg.Wait()
	time.Sleep(100 * time.Millisecond)

	// BUG: Race detector should catch issues with stopChan being closed
	// while patterns are still trying to use it
	t.Logf("Test completed without panic (check race detector output)")
	_ = testLED // Use testLED to avoid unused warning
}

// Test 7: Memory leaks from goroutines
func TestMemoryLeaksFromGoroutines(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping memory leak test in short mode")
	}

	mock := NewMockStateManager()

	initialGoroutines := countGoroutines()
	t.Logf("Starting goroutines: %d", initialGoroutines)

	// Create and destroy multiple controllers
	for iteration := 0; iteration < 10; iteration++ {
		c, _ := createTestController()

		err := c.StartMocked(mock)
		if err != nil {
			t.Fatalf("Failed to start controller: %v", err)
		}

		// Run through various states
		for i := 0; i < 20; i++ {
			mock.SetStatus(state.StatusSyncing)
			time.Sleep(10 * time.Millisecond)
			mock.SetStatus(state.StatusIdle)
			time.Sleep(10 * time.Millisecond)
		}

		c.Stop()
		time.Sleep(50 * time.Millisecond)
	}

	// Force garbage collection
	runtime.GC()
	time.Sleep(200 * time.Millisecond)
	runtime.GC()
	time.Sleep(200 * time.Millisecond)

	finalGoroutines := countGoroutines()
	t.Logf("Final goroutines: %d", finalGoroutines)

	goroutineDiff := finalGoroutines - initialGoroutines

	// BUG EXPECTED: Each controller iteration likely leaks goroutines
	// from runPattern calls that don't properly clean up
	if goroutineDiff > 10 {
		t.Errorf("SEVERE GOROUTINE LEAK: Leaked %d goroutines over 10 iterations", goroutineDiff)
	}
}

// Test 8: LED state after errors or panics
func TestLEDStateAfterErrors(t *testing.T) {
	mock := NewMockStateManager()

	// Create LED that will fail writes
	tmpDir, err := os.MkdirTemp("", "ledtest-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	brightnessFile := filepath.Join(tmpDir, "brightness")
	if err := os.WriteFile(brightnessFile, []byte("0"), 0644); err != nil {
		t.Fatalf("Failed to create brightness file: %v", err)
	}

	c := &Controller{
		actLED: &LED{
			name:           "TEST",
			brightnessPath: brightnessFile,
			available:      true,
		},
		stopChan: make(chan struct{}),
		wg:       sync.WaitGroup{},
	}

	err = c.StartMocked(mock)
	if err != nil {
		t.Fatalf("Failed to start controller: %v", err)
	}

	// Start a pattern
	mock.SetStatus(state.StatusSyncing)
	time.Sleep(100 * time.Millisecond)

	// Make the file unwritable
	os.Chmod(brightnessFile, 0444)

	// Try to change state (should error internally but not crash)
	mock.SetStatus(state.StatusSuccess)
	time.Sleep(100 * time.Millisecond)

	// BUG: Pattern goroutines might panic or hang on write errors
	// Controller should handle errors gracefully

	c.Stop()

	// Verify we can still interact with controller
	if c.actLED == nil {
		t.Error("BUG: Controller actLED became nil after errors")
	}
}

// Test 9: Cleanup on shutdown
func TestCleanupOnShutdown(t *testing.T) {
	mock := NewMockStateManager()

	brightnessFile, cleanup := createTempLEDPath(t)
	defer cleanup()

	c := &Controller{
		actLED: &LED{
			name:           "TEST",
			brightnessPath: brightnessFile,
			available:      true,
		},
		stopChan: make(chan struct{}),
		wg:       sync.WaitGroup{},
	}

	err := c.StartMocked(mock)
	if err != nil {
		t.Fatalf("Failed to start controller: %v", err)
	}

	// Start various patterns
	mock.SetStatus(state.StatusSyncing)
	time.Sleep(100 * time.Millisecond)

	mock.SetStatus(state.StatusDetected)
	time.Sleep(50 * time.Millisecond)

	initialGoroutines := countGoroutines()

	// Stop controller
	c.Stop()

	// Give goroutines time to exit
	time.Sleep(200 * time.Millisecond)

	// Check LED is off
	data, _ := os.ReadFile(brightnessFile)
	brightness := string(data)

	if brightness != "0" && brightness != "0\n" {
		t.Errorf("BUG: LED not turned off on shutdown. Brightness: %s", brightness)
	}

	// Check goroutines decreased
	finalGoroutines := countGoroutines()
	if finalGoroutines >= initialGoroutines {
		t.Errorf("BUG: Goroutines not cleaned up on shutdown. Before: %d, After: %d",
			initialGoroutines, finalGoroutines)
	}

	// Try to stop again (should not panic)
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("BUG: Stop() panicked on second call: %v", r)
		}
	}()
	c.Stop()
}

// Test 10: State manager subscription handling
func TestStateManagerSubscriptionHandling(t *testing.T) {
	mock := NewMockStateManager()

	c, _ := createTestController()

	err := c.StartMocked(mock)
	if err != nil {
		t.Fatalf("Failed to start controller: %v", err)
	}

	// Create additional subscriptions
	sub1 := mock.Subscribe()
	sub2 := mock.Subscribe()
	sub3 := mock.Subscribe()

	// Generate state changes
	go func() {
		for i := 0; i < 50; i++ {
			mock.SetStatus(state.StatusSyncing)
			time.Sleep(10 * time.Millisecond)
			mock.SetStatus(state.StatusIdle)
			time.Sleep(10 * time.Millisecond)
		}
	}()

	// Read from subscriptions at different rates
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		count := 0
		for range sub1 {
			count++
			if count > 20 {
				return
			}
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		count := 0
		for range sub2 {
			count++
			time.Sleep(50 * time.Millisecond) // Slow consumer
			if count > 10 {
				return
			}
		}
	}()

	// Don't read from sub3 at all (blocked consumer)

	time.Sleep(500 * time.Millisecond)

	c.Stop()

	// BUG: Blocked subscriptions might cause state manager to hang
	// or leak goroutines when notifying listeners

	// Clean up subscriptions
	mock.Unsubscribe(sub1)
	mock.Unsubscribe(sub2)
	mock.Unsubscribe(sub3)

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Subscription consumers did not exit after unsubscribe")
	}

	t.Log("Subscription handling test completed")
}

// Test 11: updatePattern stopChan race condition
func TestUpdatePatternStopChanRace(t *testing.T) {
	mock := NewMockStateManager()

	c, _ := createTestController()
	err := c.StartMocked(mock)
	if err != nil {
		t.Fatalf("Failed to start controller: %v", err)
	}

	// BUG ANALYSIS: updatePattern has a critical race condition:
	// 1. It sends to c.stopChan (line 135)
	// 2. Then creates a NEW stopChan (line 140)
	// 3. Starts goroutines that read from the NEW stopChan
	//
	// Problem: Old goroutines are still using the OLD stopChan,
	// but updatePattern sends to the OLD stopChan, then replaces it.
	// This means old goroutines might never receive the stop signal!

	initialGoroutines := countGoroutines()

	// Rapidly trigger updatePattern
	for i := 0; i < 100; i++ {
		if i%2 == 0 {
			mock.SetStatus(state.StatusSyncing)
		} else {
			mock.SetStatus(state.StatusIdle)
		}
		time.Sleep(5 * time.Millisecond)
	}

	c.Stop()
	time.Sleep(500 * time.Millisecond)
	runtime.GC()
	time.Sleep(200 * time.Millisecond)

	finalGoroutines := countGoroutines()
	leaked := finalGoroutines - initialGoroutines

	t.Logf("Goroutine count: initial=%d, final=%d, leaked=%d",
		initialGoroutines, finalGoroutines, leaked)

	if leaked > 10 {
		t.Errorf("CRITICAL BUG: updatePattern stopChan race leaked %d goroutines", leaked)
	}
}

// Test 12: runPattern with Repeat count edge cases
func TestRunPatternRepeatEdgeCases(t *testing.T) {
	brightnessFile, cleanup := createTempLEDPath(t)
	defer cleanup()

	led := &LED{
		name:           "TEST",
		brightnessPath: brightnessFile,
		available:      true,
	}

	tests := []struct {
		name    string
		pattern LEDPattern
		timeout time.Duration
	}{
		{
			name: "Zero repeat (infinite)",
			pattern: LEDPattern{
				OnDuration:  50 * time.Millisecond,
				OffDuration: 50 * time.Millisecond,
				Repeat:      0,
			},
			timeout: 250 * time.Millisecond,
		},
		{
			name: "One repeat",
			pattern: LEDPattern{
				OnDuration:  50 * time.Millisecond,
				OffDuration: 50 * time.Millisecond,
				Repeat:      1,
			},
			timeout: 200 * time.Millisecond,
		},
		{
			name: "Steady on (zero durations)",
			pattern: LEDPattern{
				OnDuration:  0,
				OffDuration: 0,
				Repeat:      0,
			},
			timeout: 100 * time.Millisecond,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Controller{
				actLED:   led,
				stopChan: make(chan struct{}),
			}

			patternStop := make(chan struct{})
			done := make(chan struct{})
			go func() {
				c.runPattern(led, tt.pattern, patternStop)
				close(done)
			}()

			// Wait for timeout
			time.Sleep(tt.timeout)

			// Stop the pattern
			close(patternStop)

			// Wait for goroutine to exit
			select {
			case <-done:
				// Success
			case <-time.After(500 * time.Millisecond):
				t.Errorf("BUG: runPattern goroutine did not exit after stopChan close")
			}
		})
	}
}

// Test 13: Double close of stopChan
func TestDoubleCloseStopChan(t *testing.T) {
	c, _ := createTestController()

	mock := NewMockStateManager()

	err := c.StartMocked(mock)
	if err != nil {
		t.Fatalf("Failed to start controller: %v", err)
	}

	// Trigger pattern changes
	mock.SetStatus(state.StatusSyncing)
	time.Sleep(50 * time.Millisecond)

	// Try to trigger double close
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("BUG: Panic from double close: %v", r)
		}
	}()

	c.Stop()
	c.Stop() // Should not panic
	c.Stop() // Should not panic
}

// Test 14: Pattern goroutine behavior on stopChan close vs send
func TestStopChanCloseVsSend(t *testing.T) {
	brightnessFile, cleanup := createTempLEDPath(t)
	defer cleanup()

	led := &LED{
		name:           "TEST",
		brightnessPath: brightnessFile,
		available:      true,
	}

	// BUG ANALYSIS: Stop() closes stopChan, but updatePattern tries to SEND
	// to stopChan. Sending to a closed channel panics!
	// This is a critical bug in updatePattern line 135:
	//   select { case c.stopChan <- struct{}{}: ... }

	c := &Controller{
		actLED:   led,
		stopChan: make(chan struct{}),
		wg:       sync.WaitGroup{},
	}

	mock := NewMockStateManager()

	err := c.StartMocked(mock)
	if err != nil {
		t.Fatalf("Failed to start controller: %v", err)
	}

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("CRITICAL BUG: Panic when sending to closed stopChan: %v", r)
		}
	}()

	// Start a pattern
	mock.SetStatus(state.StatusSyncing)
	time.Sleep(50 * time.Millisecond)

	// Stop (closes stopChan)
	c.Stop()
	time.Sleep(20 * time.Millisecond)

	// Try to change state (will call updatePattern which tries to send to closed stopChan)
	mock.SetStatus(state.StatusIdle)
	time.Sleep(50 * time.Millisecond)
}

// Test 15: Stress test with monitoring
func TestStressTestWithMonitoring(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress test in short mode")
	}

	mock := NewMockStateManager()

	c, _ := createTestController()

	initialGoroutines := countGoroutines()
	peakGoroutines := initialGoroutines
	var goroutineCount int32 = int32(initialGoroutines)

	// Monitor goroutines
	stopMonitor := make(chan struct{})
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stopMonitor:
				return
			case <-ticker.C:
				current := int32(countGoroutines())
				atomic.StoreInt32(&goroutineCount, current)
				if int(current) > peakGoroutines {
					peakGoroutines = int(current)
				}
			}
		}
	}()

	err := c.StartMocked(mock)
	if err != nil {
		t.Fatalf("Failed to start controller: %v", err)
	}

	// Stress test
	duration := 5 * time.Second
	deadline := time.Now().Add(duration)
	changeCount := 0

	states := []state.SyncStatus{
		state.StatusIdle,
		state.StatusDetected,
		state.StatusSyncing,
		state.StatusSuccess,
		state.StatusError,
	}

	for time.Now().Before(deadline) {
		mock.SetStatus(states[changeCount%len(states)])
		changeCount++
		time.Sleep(20 * time.Millisecond)
	}

	c.Stop()
	close(stopMonitor)

	// Cleanup wait
	time.Sleep(500 * time.Millisecond)
	runtime.GC()
	time.Sleep(200 * time.Millisecond)

	finalGoroutines := countGoroutines()

	t.Logf("Stress test results:")
	t.Logf("  State changes: %d", changeCount)
	t.Logf("  Initial goroutines: %d", initialGoroutines)
	t.Logf("  Peak goroutines: %d", peakGoroutines)
	t.Logf("  Final goroutines: %d", finalGoroutines)
	t.Logf("  Leaked goroutines: %d", finalGoroutines-initialGoroutines)

	if finalGoroutines-initialGoroutines > 20 {
		t.Errorf("SEVERE LEAK: %d goroutines leaked during stress test",
			finalGoroutines-initialGoroutines)
	}

	if peakGoroutines-initialGoroutines > 100 {
		t.Errorf("GOROUTINE EXPLOSION: Peak reached %d goroutines (%d above baseline)",
			peakGoroutines, peakGoroutines-initialGoroutines)
	}
}

// Benchmark: Pattern change performance
func BenchmarkPatternChange(b *testing.B) {
	mock := NewMockStateManager()
	c, _ := createTestController()
	err := c.StartMocked(mock)
	if err != nil {
		b.Fatalf("Failed to start controller: %v", err)
	}
	defer c.Stop()

	states := []state.SyncStatus{
		state.StatusIdle,
		state.StatusDetected,
		state.StatusSyncing,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mock.SetStatus(states[i%len(states)])
	}
}

// Benchmark: Goroutine creation overhead
func BenchmarkGoroutineCreation(b *testing.B) {
	mock := NewMockStateManager()
	c, _ := createTestController()
	err := c.StartMocked(mock)
	if err != nil {
		b.Fatalf("Failed to start controller: %v", err)
	}
	defer c.Stop()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mock.SetStatus(state.StatusSyncing)
		mock.SetStatus(state.StatusIdle)
	}
}
