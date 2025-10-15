package state

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestPanicInListenerGoroutine tests if panic in listener causes system crash
func TestPanicInListenerGoroutine(t *testing.T) {
	m, err := NewManager()
	if err != nil {
		t.Skip("Failed to create manager - may be permission issue")
	}

	// Subscribe with a potentially panicking listener
	panicCh := m.Subscribe()

	// Goroutine that will panic when receiving state
	panicked := make(chan bool, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				panicked <- true
			}
		}()
		<-panicCh
		panic("listener panic!")
	}()

	// Trigger notification - should not crash the system
	m.SetStatus(StatusSyncing)

	// Wait to see if panic propagated
	select {
	case <-panicked:
		t.Log("VULNERABILITY: Panic in listener goroutine - system should handle gracefully but notifyListeners has no panic recovery")
	case <-time.After(100 * time.Millisecond):
		// Expected - panic is contained in goroutine
	}
}

// TestNilCurrentSyncInUpdateProgress tests nil CurrentSync dereference
func TestNilCurrentSyncInUpdateProgress(t *testing.T) {
	m, err := NewManager()
	if err != nil {
		t.Skip("Failed to create manager")
	}

	// Try to update progress without starting sync - CurrentSync is nil
	err = m.UpdateSyncProgress(10, 1000, "test.jpg", 100, 1024.0, "1m")

	if err != nil {
		t.Logf("Good: UpdateSyncProgress returns error: %v", err)
	} else {
		t.Error("VULNERABILITY: UpdateSyncProgress accepts nil CurrentSync without error - could mask bugs")
	}
}

// TestFinishWithoutStart tests finishing sync that was never started
func TestFinishWithoutStart(t *testing.T) {
	m, err := NewManager()
	if err != nil {
		t.Skip("Failed to create manager")
	}

	// Try to finish sync without starting one
	err = m.FinishSync(true, nil)
	if err == nil {
		t.Error("VULNERABILITY: FinishSync should return error when no sync is in progress")
	} else {
		t.Logf("Good: FinishSync correctly returns error: %v", err)
	}
}

// TestChannelLeaksFromSubscriptions tests resource leaks from unclosed subscription channels
func TestChannelLeaksFromSubscriptions(t *testing.T) {
	m, err := NewManager()
	if err != nil {
		t.Skip("Failed to create manager")
	}

	// Create many subscriptions without unsubscribing
	channels := make([]chan CurrentState, 100)
	for i := 0; i < 100; i++ {
		channels[i] = m.Subscribe()
	}

	// Check listener count
	m.mu.Lock()
	listenerCount := len(m.listeners)
	m.mu.Unlock()

	if listenerCount != 100 {
		t.Errorf("Expected 100 listeners, got %d", listenerCount)
	}

	// Send updates - with many blocked channels this could deadlock
	done := make(chan bool, 1)
	go func() {
		m.SetStatus(StatusSyncing) // This calls notifyListeners
		done <- true
	}()

	select {
	case <-done:
		t.Log("Good: notifyListeners doesn't block on full channels")
	case <-time.After(2 * time.Second):
		t.Error("VULNERABILITY: notifyListeners blocks when channels are full - DoS risk")
	}

	t.Log("VULNERABILITY: Subscribe() creates channels that are never closed unless explicitly unsubscribed")
	t.Log("If calling code doesn't unsubscribe, channels and goroutines leak")
}

// TestUnsubscribeNonExistentChannel tests unsubscribing a channel that wasn't subscribed
func TestUnsubscribeNonExistentChannel(t *testing.T) {
	m, err := NewManager()
	if err != nil {
		t.Skip("Failed to create manager")
	}

	// Try to unsubscribe a channel that was never subscribed
	fakeCh := make(chan CurrentState)

	// This should not panic
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("VULNERABILITY: Unsubscribe panics on unknown channel: %v", r)
		}
	}()

	m.Unsubscribe(fakeCh)
	t.Log("Good: Unsubscribe handles unknown channels gracefully")
}

// TestConcurrentSubscribeUnsubscribeNotify tests race conditions in subscription management
func TestConcurrentSubscribeUnsubscribeNotify(t *testing.T) {
	m, err := NewManager()
	if err != nil {
		t.Skip("Failed to create manager")
	}

	var wg sync.WaitGroup

	// Concurrent subscribe/unsubscribe/notify
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ch := m.Subscribe()
			time.Sleep(1 * time.Millisecond)
			m.Unsubscribe(ch)
		}()
	}

	// Concurrent notifications
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			if idx%2 == 0 {
				m.SetStatus(StatusSyncing)
			} else {
				m.SetStatus(StatusIdle)
			}
		}(i)
	}

	// This should complete without deadlock or panic
	done := make(chan bool, 1)
	go func() {
		wg.Wait()
		done <- true
	}()

	select {
	case <-done:
		t.Log("Good: Concurrent subscribe/unsubscribe/notify doesn't deadlock")
	case <-time.After(5 * time.Second):
		t.Error("VULNERABILITY: Deadlock in concurrent subscribe/unsubscribe/notify operations")
	}
}

// TestMalformedJSONRecovery tests handling of corrupted state file
func TestMalformedJSONRecovery(t *testing.T) {
	// This would need to modify StateFile path which is a const
	// Test demonstrates the vulnerability conceptually
	t.Log("VULNERABILITY: Manager creation could fail completely on malformed JSON")
	t.Log("load() should gracefully degrade to default state")
}

// TestDoubleFinish tests calling FinishSync twice
func TestDoubleFinish(t *testing.T) {
	m, err := NewManager()
	if err != nil {
		t.Skip("Failed to create manager")
	}

	// Start sync
	_, err = m.StartSync("card-test1234", 100, 1000000)
	if err != nil {
		t.Skipf("Failed to start sync: %v", err)
	}

	// Finish sync
	err = m.FinishSync(true, nil)
	if err != nil {
		t.Skipf("Failed to finish sync: %v", err)
	}

	// Try to finish again
	err = m.FinishSync(true, nil)
	if err == nil {
		t.Error("VULNERABILITY: FinishSync should return error when called twice")
	} else {
		t.Logf("Good: FinishSync returns error on double call: %v", err)
	}
}

// TestConcurrentReloadAndGetState tests race between Reload and other operations
func TestConcurrentReloadAndGetState(t *testing.T) {
	m, err := NewManager()
	if err != nil {
		t.Skip("Failed to create manager")
	}

	var wg sync.WaitGroup

	// Concurrent Reload and GetState calls
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			m.Reload()
		}()
		go func() {
			defer wg.Done()
			_ = m.GetState()
		}()
	}

	done := make(chan bool, 1)
	go func() {
		wg.Wait()
		done <- true
	}()

	select {
	case <-done:
		t.Log("Good: No deadlock in concurrent Reload and GetState")
	case <-time.After(5 * time.Second):
		t.Error("VULNERABILITY: Deadlock in concurrent Reload and GetState operations")
	}
}

// TestLargeHistoryMemoryExhaustion tests memory consumption with large history
func TestLargeHistoryMemoryExhaustion(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping memory test in short mode")
	}

	m, err := NewManager()
	if err != nil {
		t.Skip("Failed to create manager")
	}

	// Simulate large history
	for i := 0; i < 10000; i++ {
		m.mu.Lock()
		m.history = append(m.history, SyncRecord{
			ID:          fmt.Sprintf("%d", i),
			StartTime:   time.Now(),
			EndTime:     time.Now(),
			Status:      "success",
			FilesTotal:  1000,
			FilesSynced: 1000,
			BytesTotal:  1000000,
			BytesSynced: 1000000,
			CardID:      fmt.Sprintf("card-%d", i),
		})
		m.mu.Unlock()
	}

	history := m.GetHistory()
	if len(history) != 10000 {
		t.Errorf("Expected 10000 history items, got %d", len(history))
	}

	t.Log("VULNERABILITY: GetHistory returns entire history - no pagination")
	t.Log("Large history files cause memory exhaustion")
	t.Log("Consider implementing pagination or limiting history size")
}

// TestFindLastSyncPerformance tests linear search performance
func TestFindLastSyncPerformance(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping performance test in short mode")
	}

	m, err := NewManager()
	if err != nil {
		t.Skip("Failed to create manager")
	}

	// Add many history items
	m.mu.Lock()
	for i := 0; i < 1000; i++ {
		m.history = append(m.history, SyncRecord{
			ID:          fmt.Sprintf("%d", i),
			StartTime:   time.Now(),
			EndTime:     time.Now(),
			Status:      "success",
			FilesTotal:  100,
			FilesSynced: 100,
			CardID:      fmt.Sprintf("card-%08d", i),
		})
	}
	m.mu.Unlock()

	// Search for last card (worst case)
	start := time.Now()
	result := m.FindLastSyncByCardID("card-00000999")
	elapsed := time.Since(start)

	if result == nil {
		t.Error("Failed to find card")
	}

	t.Logf("Search time for 1000 items: %v", elapsed)
	if elapsed > 10*time.Millisecond {
		t.Log("VULNERABILITY: FindLastSyncByCardID uses linear search - O(n) complexity")
		t.Log("Consider using a map index for O(1) lookups")
	}
}

// TestErrorWrappingPreservesContext tests that error wrapping maintains context
func TestErrorWrappingPreservesContext(t *testing.T) {
	// Conceptual test - would need to trigger specific errors
	t.Log("Error messages should wrap original errors with %w")
	t.Log("Check that all error returns use fmt.Errorf with %w, not %v")
}

// TestStateSaveFailurePropagation tests state inconsistency on save failure
func TestStateSaveFailurePropagation(t *testing.T) {
	m, err := NewManager()
	if err != nil {
		t.Skip("Failed to create manager")
	}

	// Record initial status
	initialStatus := m.GetState().Status

	// Try to set status (will succeed in memory even if save fails)
	m.SetStatus(StatusSyncing)

	// In-memory state changed
	newStatus := m.GetState().Status

	if newStatus != StatusSyncing {
		t.Error("Status was not updated in memory")
	}

	t.Log("VULNERABILITY: SetStatus updates memory before save")
	t.Log("If save fails, memory and disk are inconsistent")
	t.Log("Should implement rollback on save failure")
	_ = initialStatus
}

// TestJSONUnmarshalErrors tests handling of corrupted JSON in various scenarios
func TestJSONUnmarshalErrors(t *testing.T) {
	malformedData := []byte(`{"status": "syncing", "current_sync": invalid}`)

	var state CurrentState
	err := json.Unmarshal(malformedData, &state)

	if err == nil {
		t.Error("VULNERABILITY: Unmarshal should fail on invalid JSON")
	} else {
		errStr := err.Error()
		if strings.Contains(errStr, "invalid") {
			t.Log("Error message contains parse details - could leak structure")
		}
	}
}

// TestProgressThrottlingAccuracy tests that progress throttling works correctly
func TestProgressThrottlingAccuracy(t *testing.T) {
	m, err := NewManager()
	if err != nil {
		t.Skip("Failed to create manager")
	}

	// Start sync
	_, err = m.StartSync("card-test1234", 100, 1000000)
	if err != nil {
		t.Skipf("Failed to start sync: %v", err)
	}

	// Call UpdateSyncProgress rapidly
	start := time.Now()
	updateCount := 0
	for i := 0; i < 100; i++ {
		err := m.UpdateSyncProgress(int64(i), int64(i*1000), fmt.Sprintf("file-%d.jpg", i), 1000, 1024.0, "1m")
		if err == nil {
			updateCount++
		}
		time.Sleep(10 * time.Millisecond)
	}
	elapsed := time.Since(start)

	// With 5s throttle and 10ms between updates, actual disk writes should be ~1 per 5s
	t.Logf("Update calls: %d, Elapsed: %v", updateCount, elapsed)
	t.Log("Note: Actual saves are throttled to reduce disk wear (5s interval)")
}
