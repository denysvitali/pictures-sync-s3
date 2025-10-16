package state

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// setupRaceTestManager creates a test manager without file persistence to avoid const issues
func setupRaceTestManager(t *testing.T) *Manager {
	mgr := &Manager{
		notifier:          newNotifier(),
		progressSaveDelay: 100 * time.Millisecond, // Shorter for tests
	}

	mgr.currentState = CurrentState{Status: StatusIdle}
	mgr.history = make([]SyncRecord, 0)

	return mgr
}

// TestConcurrentStartSync tests multiple syncs started simultaneously
func TestConcurrentStartSync(t *testing.T) {
	mgr := setupRaceTestManager(t)

	const numGoroutines = 50
	var wg sync.WaitGroup
	errors := make(chan error, numGoroutines)

	// Start multiple syncs concurrently
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			cardID := fmt.Sprintf("card-%d", id)

			// Lock is acquired here
			mgr.mu.Lock()
			record := &SyncRecord{
				ID:          fmt.Sprintf("%d", time.Now().UnixNano()),
				StartTime:   time.Now(),
				Status:      "syncing",
				FilesTotal:  int64(id * 100),
				BytesTotal:  int64(id * 1024 * 1024),
				CardID:      cardID,
			}
			mgr.currentState.CurrentSync = record
			mgr.currentState.Status = StatusSyncing
			mgr.mu.Unlock()

			// Notify without lock
			mgr.notifyListeners()
		}(i)
	}

	wg.Wait()
	close(errors)

	// Check for errors
	for err := range errors {
		t.Errorf("StartSync error: %v", err)
	}

	// Verify final state - should have the last sync that won the race
	state := mgr.GetState()
	if state.Status != StatusSyncing {
		t.Errorf("Expected status %v, got %v", StatusSyncing, state.Status)
	}
	if state.CurrentSync == nil {
		t.Error("Expected current sync to be set")
	}
}

// TestSyncCanceledWhileWritingState tests canceling sync while writing state
func TestSyncCanceledWhileWritingState(t *testing.T) {
	mgr := setupRaceTestManager(t)

	// Start a sync
	mgr.mu.Lock()
	record := &SyncRecord{
		ID:          fmt.Sprintf("%d", time.Now().Unix()),
		StartTime:   time.Now(),
		Status:      "syncing",
		FilesTotal:  100,
		BytesTotal:  1024 * 1024,
		CardID:      "test-card",
	}
	mgr.currentState.CurrentSync = record
	mgr.currentState.Status = StatusSyncing
	mgr.mu.Unlock()

	const numOperations = 100
	var wg sync.WaitGroup
	errors := make(chan error, numOperations*3)

	// Goroutine 1: Continuous progress updates
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < numOperations; i++ {
			mgr.mu.Lock()
			if mgr.currentState.CurrentSync != nil {
				mgr.currentState.CurrentSync.FilesSynced = int64(i)
				mgr.currentState.CurrentSync.BytesSynced = int64(i * 1024)
				mgr.currentState.CurrentSync.CurrentFile = fmt.Sprintf("file-%d.jpg", i)
			}
			mgr.mu.Unlock()
			mgr.notifyListeners()
			time.Sleep(time.Millisecond)
		}
	}()

	// Goroutine 2: Continuous state reads
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < numOperations; i++ {
			state := mgr.GetState()
			if state.Status != StatusSyncing && state.Status != StatusSuccess && state.Status != StatusError {
				errors <- fmt.Errorf("unexpected state: %v", state.Status)
			}
			time.Sleep(time.Millisecond)
		}
	}()

	// Goroutine 3: Finish sync in the middle
	wg.Add(1)
	go func() {
		defer wg.Done()
		time.Sleep(50 * time.Millisecond)

		mgr.mu.Lock()
		if mgr.currentState.CurrentSync == nil {
			mgr.mu.Unlock()
			errors <- fmt.Errorf("no sync in progress")
			return
		}

		mgr.currentState.CurrentSync.EndTime = time.Now()
		mgr.currentState.CurrentSync.Status = "success"
		mgr.currentState.Status = StatusSuccess

		mgr.history = append(mgr.history, *mgr.currentState.CurrentSync)
		mgr.currentState.LastSync = mgr.currentState.CurrentSync
		mgr.currentState.CurrentSync = nil
		mgr.mu.Unlock()

		mgr.notifyListeners()
	}()

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Errorf("Concurrent operation error: %v", err)
	}
}

// TestStateUpdatesFromMultipleGoroutines tests state updates from multiple goroutines
func TestStateUpdatesFromMultipleGoroutines(t *testing.T) {
	mgr := setupRaceTestManager(t)

	const numGoroutines = 20
	const opsPerGoroutine = 50
	var wg sync.WaitGroup
	errors := make(chan error, numGoroutines*opsPerGoroutine)

	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			for i := 0; i < opsPerGoroutine; i++ {
				// Random operations
				switch rand.Intn(5) {
				case 0:
					mgr.mu.Lock()
					mgr.currentState.Status = StatusIdle
					mgr.mu.Unlock()
					mgr.notifyListeners()
				case 1:
					mgr.mu.Lock()
					mgr.currentState.SDCardMounted = true
					mgr.currentState.SDCardPath = fmt.Sprintf("/path/%d", id)
					mgr.mu.Unlock()
					mgr.notifyListeners()
				case 2:
					mgr.mu.Lock()
					mgr.currentState.NeedsDeviceSelect = rand.Intn(2) == 0
					mgr.mu.Unlock()
					mgr.notifyListeners()
				case 3:
					_ = mgr.GetState()
				case 4:
					_ = mgr.GetHistory()
				}

				// Small delay to increase contention
				time.Sleep(time.Microsecond * time.Duration(rand.Intn(100)))
			}
		}(g)
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Errorf("Concurrent state update error: %v", err)
	}
}

// TestProgressUpdatesDuringStateTransitions tests progress updates during status changes
func TestProgressUpdatesDuringStateTransitions(t *testing.T) {
	mgr := setupRaceTestManager(t)

	var wg sync.WaitGroup
	errors := make(chan error, 300)
	stopProgress := make(chan struct{})

	// Start sync
	mgr.mu.Lock()
	record := &SyncRecord{
		ID:          fmt.Sprintf("%d", time.Now().Unix()),
		StartTime:   time.Now(),
		Status:      "syncing",
		FilesTotal:  1000,
		BytesTotal:  1024 * 1024 * 100,
		CardID:      "test-card",
	}
	mgr.currentState.CurrentSync = record
	mgr.currentState.Status = StatusSyncing
	mgr.mu.Unlock()

	// Goroutine 1: Rapid progress updates
	wg.Add(1)
	go func() {
		defer wg.Done()
		counter := int64(0)
		for {
			select {
			case <-stopProgress:
				return
			default:
				counter++
				mgr.mu.Lock()
				if mgr.currentState.CurrentSync != nil {
					mgr.currentState.CurrentSync.FilesSynced = counter
					mgr.currentState.CurrentSync.BytesSynced = counter * 1024
					mgr.currentState.CurrentSync.CurrentFile = fmt.Sprintf("file-%d.jpg", counter)
				}
				mgr.mu.Unlock()
				mgr.notifyListeners()
			}
		}
	}()

	// Goroutine 2: Rapid status changes
	wg.Add(1)
	go func() {
		defer wg.Done()
		statuses := []SyncStatus{StatusSyncing, StatusDetected, StatusSyncing}
		for i := 0; i < 100; i++ {
			mgr.mu.Lock()
			mgr.currentState.Status = statuses[i%len(statuses)]
			mgr.mu.Unlock()
			mgr.notifyListeners()
			time.Sleep(time.Millisecond)
		}
	}()

	// Goroutine 3: Rapid state reads
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			state := mgr.GetState()
			// Check for consistency
			if state.CurrentSync != nil {
				if state.CurrentSync.FilesSynced < 0 || state.CurrentSync.BytesSynced < 0 {
					errors <- fmt.Errorf("negative progress values detected")
				}
			}
			time.Sleep(time.Millisecond)
		}
	}()

	wg.Wait()
	close(stopProgress)
	close(errors)

	for err := range errors {
		t.Errorf("Progress/transition error: %v", err)
	}
}

// TestHistoryAccessDuringWrites tests reading history while writes are happening
func TestHistoryAccessDuringWrites(t *testing.T) {
	mgr := setupRaceTestManager(t)

	const numSyncs = 50
	var wg sync.WaitGroup
	errors := make(chan error, numSyncs*5)

	// Goroutine 1: Create and finish many syncs
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < numSyncs; i++ {
			cardID := fmt.Sprintf("card-%d", i)

			// Start sync
			mgr.mu.Lock()
			record := &SyncRecord{
				ID:          fmt.Sprintf("%d-%d", time.Now().Unix(), i),
				StartTime:   time.Now(),
				Status:      "syncing",
				FilesTotal:  int64(i * 10),
				BytesTotal:  int64(i * 1024),
				CardID:      cardID,
			}
			mgr.currentState.CurrentSync = record
			mgr.currentState.Status = StatusSyncing
			mgr.mu.Unlock()

			// Small delay
			time.Sleep(time.Millisecond * 5)

			// Finish sync
			mgr.mu.Lock()
			if mgr.currentState.CurrentSync != nil {
				mgr.currentState.CurrentSync.EndTime = time.Now()
				if i%2 == 0 {
					mgr.currentState.CurrentSync.Status = "success"
					mgr.currentState.Status = StatusSuccess
				} else {
					mgr.currentState.CurrentSync.Status = "error"
					mgr.currentState.Status = StatusError
				}
				mgr.history = append(mgr.history, *mgr.currentState.CurrentSync)
				mgr.currentState.LastSync = mgr.currentState.CurrentSync
				mgr.currentState.CurrentSync = nil
			}
			mgr.mu.Unlock()

			mgr.notifyListeners()
		}
	}()

	// Goroutine 2-4: Continuous history reads
	for g := 0; g < 3; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < numSyncs*2; i++ {
				history := mgr.GetHistory()
				// Verify history is consistent
				for _, record := range history {
					if record.ID == "" {
						errors <- fmt.Errorf("goroutine %d: empty record ID in history", id)
					}
					if record.CardID == "" {
						errors <- fmt.Errorf("goroutine %d: empty card ID in history", id)
					}
				}
				time.Sleep(time.Millisecond)
			}
		}(g)
	}

	// Goroutine 5: FindLastSyncByCardID calls
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < numSyncs*2; i++ {
			cardID := fmt.Sprintf("card-%d", rand.Intn(numSyncs))
			record := mgr.FindLastSyncByCardID(cardID)
			if record != nil && record.CardID != cardID {
				errors <- fmt.Errorf("FindLastSyncByCardID returned wrong card: expected %s, got %s", cardID, record.CardID)
			}
			time.Sleep(time.Millisecond)
		}
	}()

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Errorf("History access error: %v", err)
	}
}

// TestSubscriberNotificationRaces tests race conditions in subscriber notifications
func TestSubscriberNotificationRaces(t *testing.T) {
	mgr := setupRaceTestManager(t)

	const numSubscribers = 20
	const numUpdates = 100
	var wg sync.WaitGroup
	errors := make(chan error, numSubscribers*numUpdates)

	// Create multiple subscribers
	subscribers := make([]chan CurrentState, numSubscribers)
	for i := 0; i < numSubscribers; i++ {
		subscribers[i] = mgr.Subscribe()
	}

	// Start listeners
	for i, ch := range subscribers {
		wg.Add(1)
		go func(id int, stateChan chan CurrentState) {
			defer wg.Done()
			received := 0
			timeout := time.After(5 * time.Second)

			for {
				select {
				case state, ok := <-stateChan:
					if !ok {
						return
					}
					received++
					// Verify state is valid
					if state.Status == "" {
						errors <- fmt.Errorf("subscriber %d: received empty status", id)
					}

					// Stop after receiving enough updates
					if received >= numUpdates/2 {
						return
					}
				case <-timeout:
					return
				}
			}
		}(i, ch)
	}

	// Generate state updates
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < numUpdates; i++ {
			mgr.mu.Lock()
			mgr.currentState.Status = StatusSyncing
			mgr.mu.Unlock()
			mgr.notifyListeners()
			time.Sleep(time.Millisecond * 10)
		}
	}()

	// Dynamically add/remove subscribers during updates
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 10; i++ {
			time.Sleep(time.Millisecond * 50)

			// Add new subscriber
			newCh := mgr.Subscribe()

			// Remove it immediately
			go func(ch chan CurrentState) {
				time.Sleep(time.Millisecond * 10)
				mgr.Unsubscribe(ch)
			}(newCh)
		}
	}()

	wg.Wait()
	close(errors)

	// Clean up remaining subscribers
	for _, ch := range subscribers {
		mgr.Unsubscribe(ch)
	}

	for err := range errors {
		t.Errorf("Subscriber notification error: %v", err)
	}
}

// TestMultipleHTTPRequestsToWebUI simulates concurrent HTTP requests
func TestMultipleHTTPRequestsToWebUI(t *testing.T) {
	mgr := setupRaceTestManager(t)

	// Start a sync
	mgr.mu.Lock()
	record := &SyncRecord{
		ID:          fmt.Sprintf("%d", time.Now().Unix()),
		StartTime:   time.Now(),
		Status:      "syncing",
		FilesTotal:  1000,
		BytesTotal:  1024 * 1024 * 100,
		CardID:      "test-card",
	}
	mgr.currentState.CurrentSync = record
	mgr.currentState.Status = StatusSyncing
	mgr.mu.Unlock()

	const numRequests = 100
	var wg sync.WaitGroup
	errors := make(chan error, numRequests*3)

	// Simulate multiple types of HTTP requests
	for i := 0; i < numRequests; i++ {
		// GET /api/status
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			state := mgr.GetState()
			if state.Status == "" {
				errors <- fmt.Errorf("request %d: got empty status", id)
			}
		}(i)

		// GET /api/history
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			history := mgr.GetHistory()
			for _, record := range history {
				if record.ID == "" {
					errors <- fmt.Errorf("request %d: history has empty ID", id)
				}
			}
		}(i)

		// POST /api/settings (simulated by SetStatus)
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			statuses := []SyncStatus{StatusIdle, StatusSyncing, StatusDetected}
			mgr.mu.Lock()
			mgr.currentState.Status = statuses[id%len(statuses)]
			mgr.mu.Unlock()
			mgr.notifyListeners()
		}(i)
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Errorf("HTTP request simulation error: %v", err)
	}
}

// TestShutdownDuringActiveSync tests shutdown while sync is active
func TestShutdownDuringActiveSync(t *testing.T) {
	mgr := setupRaceTestManager(t)

	var wg sync.WaitGroup
	errors := make(chan error, 100)
	shutdown := make(chan struct{})

	// Start a sync
	mgr.mu.Lock()
	record := &SyncRecord{
		ID:          fmt.Sprintf("%d", time.Now().Unix()),
		StartTime:   time.Now(),
		Status:      "syncing",
		FilesTotal:  1000,
		BytesTotal:  1024 * 1024 * 100,
		CardID:      "test-card",
	}
	mgr.currentState.CurrentSync = record
	mgr.currentState.Status = StatusSyncing
	mgr.mu.Unlock()

	// Goroutine 1: Progress updates
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			select {
			case <-shutdown:
				return
			default:
				mgr.mu.Lock()
				if mgr.currentState.CurrentSync != nil {
					mgr.currentState.CurrentSync.FilesSynced = int64(i)
					mgr.currentState.CurrentSync.BytesSynced = int64(i * 1024)
				}
				mgr.mu.Unlock()
				mgr.notifyListeners()
				time.Sleep(time.Millisecond)
			}
		}
	}()

	// Goroutine 2: State reads
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			select {
			case <-shutdown:
				return
			default:
				_ = mgr.GetState()
				time.Sleep(time.Millisecond)
			}
		}
	}()

	// Goroutine 3: Subscribers
	wg.Add(1)
	go func() {
		defer wg.Done()
		ch := mgr.Subscribe()
		defer mgr.Unsubscribe(ch)

		for {
			select {
			case <-shutdown:
				return
			case _, ok := <-ch:
				if !ok {
					return
				}
			case <-time.After(100 * time.Millisecond):
				// Continue
			}
		}
	}()

	// Trigger shutdown after a short time
	time.Sleep(50 * time.Millisecond)
	close(shutdown)

	// Finish sync during shutdown
	mgr.mu.Lock()
	if mgr.currentState.CurrentSync != nil {
		mgr.currentState.CurrentSync.EndTime = time.Now()
		mgr.currentState.CurrentSync.Status = "error"
		mgr.currentState.CurrentSync.Error = "shutdown"
		mgr.currentState.Status = StatusError
		mgr.history = append(mgr.history, *mgr.currentState.CurrentSync)
		mgr.currentState.LastSync = mgr.currentState.CurrentSync
		mgr.currentState.CurrentSync = nil
	}
	mgr.mu.Unlock()

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Errorf("Shutdown error: %v", err)
	}

	// Verify final state is consistent
	state := mgr.GetState()
	if state.CurrentSync != nil {
		t.Error("CurrentSync should be nil after FinishSync")
	}
}

// TestDeadlockDetection tests for potential deadlocks
func TestDeadlockDetection(t *testing.T) {
	mgr := setupRaceTestManager(t)

	// Use a timeout to detect deadlocks
	done := make(chan struct{})

	go func() {
		defer close(done)

		var wg sync.WaitGroup
		const numGoroutines = 50

		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()

				for j := 0; j < 100; j++ {
					// Mix of operations that could deadlock
					switch j % 6 {
					case 0:
						mgr.GetState()
					case 1:
						mgr.GetHistory()
					case 2:
						mgr.mu.Lock()
						mgr.currentState.Status = StatusIdle
						mgr.mu.Unlock()
						mgr.notifyListeners()
					case 3:
						mgr.mu.Lock()
						record := &SyncRecord{
							ID:         fmt.Sprintf("%d-%d", id, j),
							StartTime:  time.Now(),
							Status:     "syncing",
							FilesTotal: 100,
							BytesTotal: 1024,
							CardID:     fmt.Sprintf("card-%d", id),
						}
						mgr.currentState.CurrentSync = record
						mgr.currentState.Status = StatusSyncing
						mgr.mu.Unlock()
					case 4:
						mgr.mu.Lock()
						if mgr.currentState.CurrentSync != nil {
							mgr.currentState.CurrentSync.FilesSynced = int64(j)
						}
						mgr.mu.Unlock()
					case 5:
						mgr.mu.Lock()
						if mgr.currentState.CurrentSync != nil {
							mgr.history = append(mgr.history, *mgr.currentState.CurrentSync)
							mgr.currentState.CurrentSync = nil
						}
						mgr.mu.Unlock()
					}
				}
			}(i)
		}

		wg.Wait()
	}()

	select {
	case <-done:
		// Success - no deadlock
	case <-time.After(10 * time.Second):
		t.Fatal("Deadlock detected - operations did not complete within timeout")
	}
}

// TestLostUpdates tests for lost update scenarios
func TestLostUpdates(t *testing.T) {
	mgr := setupRaceTestManager(t)

	const numUpdates = 1000
	var updateCounter int64
	var wg sync.WaitGroup

	// Start a sync
	mgr.mu.Lock()
	record := &SyncRecord{
		ID:          fmt.Sprintf("%d", time.Now().Unix()),
		StartTime:   time.Now(),
		Status:      "syncing",
		FilesTotal:  numUpdates,
		BytesTotal:  1024 * 1024 * 100,
		CardID:      "test-card",
	}
	mgr.currentState.CurrentSync = record
	mgr.currentState.Status = StatusSyncing
	mgr.mu.Unlock()

	// Multiple goroutines updating progress
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < numUpdates/10; j++ {
				counter := atomic.AddInt64(&updateCounter, 1)
				mgr.mu.Lock()
				if mgr.currentState.CurrentSync != nil {
					mgr.currentState.CurrentSync.FilesSynced = counter
					mgr.currentState.CurrentSync.BytesSynced = counter * 1024
					mgr.currentState.CurrentSync.CurrentFile = fmt.Sprintf("file-%d.jpg", counter)
				}
				mgr.mu.Unlock()
				mgr.notifyListeners()
			}
		}()
	}

	wg.Wait()

	// Verify final state
	state := mgr.GetState()
	if state.CurrentSync == nil {
		t.Fatal("CurrentSync is nil after updates")
	}

	// The final FilesSynced should match the counter
	if state.CurrentSync.FilesSynced != updateCounter {
		t.Errorf("Lost updates detected: expected %d, got %d", updateCounter, state.CurrentSync.FilesSynced)
	}
}

// TestSubscribeUnsubscribeRace tests race conditions in subscribe/unsubscribe
func TestSubscribeUnsubscribeRace(t *testing.T) {
	mgr := setupRaceTestManager(t)

	var wg sync.WaitGroup

	// Continuously subscribe and unsubscribe
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ch := mgr.Subscribe()

			// Try to read from channel
			go func() {
				for range ch {
					// Drain channel
				}
			}()

			// Unsubscribe after a short delay
			time.Sleep(time.Millisecond * time.Duration(rand.Intn(10)))
			mgr.Unsubscribe(ch)
		}()
	}

	// Generate updates while subscribing/unsubscribing
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			mgr.mu.Lock()
			mgr.currentState.Status = StatusIdle
			mgr.mu.Unlock()
			mgr.notifyListeners()
			time.Sleep(time.Millisecond)
		}
	}()

	wg.Wait()

	// Verify no goroutine leaks by checking listener count
	listenerCount := mgr.notifier.getListenerCount()

	if listenerCount > 0 {
		t.Errorf("Memory leak: %d listeners still registered after unsubscribe", listenerCount)
	}
}

// TestSaveCorruptionSimulation tests for potential data corruption during concurrent operations
func TestSaveCorruptionSimulation(t *testing.T) {
	mgr := setupRaceTestManager(t)

	// Create temporary file for testing
	tmpFile := filepath.Join(t.TempDir(), "state.json")

	const numOperations = 200
	var wg sync.WaitGroup

	// Multiple goroutines triggering state changes and simulated saves
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOperations/10; j++ {
				// Update state
				mgr.mu.Lock()
				mgr.currentState.SDCardMounted = j%2 == 0
				mgr.currentState.SDCardPath = fmt.Sprintf("/path/%d/%d", id, j)

				// Simulate save operation
				data, err := json.MarshalIndent(mgr.currentState, "", "  ")
				mgr.mu.Unlock()

				if err != nil {
					t.Errorf("Marshal error: %v", err)
					continue
				}

				// Write to temp file
				if err := os.WriteFile(tmpFile, data, 0644); err != nil {
					t.Errorf("Write error: %v", err)
				}

				time.Sleep(time.Microsecond * 100)
			}
		}(i)
	}

	wg.Wait()

	// Verify the saved state is valid JSON
	data, err := os.ReadFile(tmpFile)
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("Failed to read state file: %v", err)
	}

	if len(data) > 0 {
		var state CurrentState
		if err := json.Unmarshal(data, &state); err != nil {
			t.Errorf("State file corrupted: %v\nContent: %s", err, string(data))
		}
	}
}
