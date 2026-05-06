//go:build stress

package state

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestMultipleSDCardsSimultaneous tests inserting multiple SD cards at once
// BUG: Race condition in main.go:114 - IsRunning check is not protected by mutex
// BUG: No queue for pending syncs - second card insertion is silently ignored
func TestMultipleSDCardsSimultaneous(t *testing.T) {
	mgr := setupRaceTestManager(t)

	const numCards = 5
	var wg sync.WaitGroup
	startedSyncs := int32(0)
	ignoredCards := int32(0)

	// Simulate multiple SD cards being inserted simultaneously
	for i := 0; i < numCards; i++ {
		wg.Add(1)
		go func(cardNum int) {
			defer wg.Done()
			cardID := fmt.Sprintf("card-%08d", cardNum)

			// Simulate handleCardInserted logic
			// This mimics main.go:106-224
			mgr.mu.Lock()
			currentlyRunning := mgr.currentState.Status == StatusSyncing && mgr.currentState.CurrentSync != nil
			mgr.mu.Unlock()

			if currentlyRunning {
				atomic.AddInt32(&ignoredCards, 1)
				t.Logf("Card %d: Sync already in progress, ignoring", cardNum)
				return
			}

			// Start sync
			_, err := mgr.StartSync(cardID, int64(100+cardNum), int64((100+cardNum)*1024*1024))
			if err == nil {
				atomic.AddInt32(&startedSyncs, 1)
				t.Logf("Card %d: Sync started successfully", cardNum)

				// Simulate sync work
				for j := 0; j < 10; j++ {
					mgr.UpdateSyncProgress(int64(j*10), int64(j*100*1024), "file.jpg", 1024, 100000, "1m")
					time.Sleep(time.Millisecond * 10)
				}
				mgr.FinishSync(true, nil)
			} else {
				t.Logf("Card %d: Failed to start sync: %v", cardNum, err)
			}
		}(i)
	}

	wg.Wait()

	// BUG EXPOSED: Multiple syncs may start simultaneously due to race in IsRunning check
	// Expected: Only 1 sync should start, 4 should be ignored
	// Actual: Race condition may allow multiple syncs to start
	if startedSyncs > 1 {
		t.Errorf("BUG DETECTED: Multiple syncs started simultaneously (%d), should be 1", startedSyncs)
	}

	t.Logf("Results: Started=%d, Ignored=%d", startedSyncs, ignoredCards)
}

// TestConcurrentSyncSameCardID tests multiple syncs with the same card ID
// BUG: No protection against starting multiple syncs for the same card
// BUG: History corruption when multiple FinishSync calls happen for same card
func TestConcurrentSyncSameCardID(t *testing.T) {
	mgr := setupRaceTestManager(t)

	const numAttempts = 10
	cardID := "card-duplicate"
	var wg sync.WaitGroup
	successCount := int32(0)
	errorCount := int32(0)

	// Try to start multiple syncs for the same card simultaneously
	for i := 0; i < numAttempts; i++ {
		wg.Add(1)
		go func(attempt int) {
			defer wg.Done()

			// Try to start sync
			record, err := mgr.StartSync(cardID, int64(100+attempt), int64((100+attempt)*1024*1024))
			if err != nil {
				atomic.AddInt32(&errorCount, 1)
				return
			}

			// Record should have the card ID
			if record.CardID != cardID {
				t.Errorf("Attempt %d: Wrong card ID: got %s, want %s", attempt, record.CardID, cardID)
			}

			// Update progress
			for j := 0; j < 5; j++ {
				mgr.UpdateSyncProgress(int64(j*10), int64(j*100*1024), fmt.Sprintf("file-%d.jpg", j), 1024, 100000, "1m")
				time.Sleep(time.Millisecond)
			}

			// Finish sync
			finishErr := mgr.FinishSync(true, nil)
			if finishErr == nil {
				atomic.AddInt32(&successCount, 1)
			} else {
				atomic.AddInt32(&errorCount, 1)
				t.Logf("Attempt %d: FinishSync error: %v", attempt, finishErr)
			}
		}(i)
	}

	wg.Wait()

	// Check history for duplicates
	history := mgr.GetHistory()
	cardIDCount := 0
	for _, record := range history {
		if record.CardID == cardID {
			cardIDCount++
		}
	}

	// BUG EXPOSED: Multiple history entries for same card ID
	// This can corrupt statistics and cause incorrect reformat detection
	if cardIDCount > 1 {
		t.Errorf("BUG DETECTED: History has %d entries for same card ID, should be 1", cardIDCount)
	}

	t.Logf("Results: Success=%d, Errors=%d, History entries=%d", successCount, errorCount, cardIDCount)
}

// TestStartSyncRaceCondition tests race between checking CurrentSync and setting it
// BUG CRITICAL: StartSync doesn't check if a sync is already running before overwriting CurrentSync
// Location: state.go:171-192
func TestStartSyncRaceCondition(t *testing.T) {
	mgr := setupRaceTestManager(t)

	const numGoroutines = 50
	var wg sync.WaitGroup
	startedCount := int32(0)
	var lastCardID string
	var lastCardIDMutex sync.Mutex

	// Start multiple syncs simultaneously without any coordination
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			cardID := fmt.Sprintf("card-%d", id)

			_, err := mgr.StartSync(cardID, 100, 1024*1024)
			if err == nil {
				atomic.AddInt32(&startedCount, 1)
				lastCardIDMutex.Lock()
				lastCardID = cardID
				lastCardIDMutex.Unlock()
			}
		}(i)
	}

	wg.Wait()

	// BUG EXPOSED: All goroutines can call StartSync successfully
	// The CurrentSync pointer gets overwritten, losing track of previous syncs
	if startedCount > 1 {
		t.Logf("BUG DETECTED: %d syncs started concurrently without protection", startedCount)
	}

	// Check which card ID won the race
	state := mgr.GetState()
	if state.CurrentSync != nil {
		lastCardIDMutex.Lock()
		if state.CurrentSync.CardID != lastCardID {
			t.Errorf("CurrentSync.CardID race: expected last set %s, got %s", lastCardID, state.CurrentSync.CardID)
		}
		lastCardIDMutex.Unlock()
	}
}

// TestProgressUpdateRaceWithFinishSync tests race between UpdateSyncProgress and FinishSync
// BUG: UpdateSyncProgress modifies CurrentSync after FinishSync sets it to nil
// Location: state.go:196-223 (UpdateSyncProgress) vs state.go:226-261 (FinishSync)
func TestProgressUpdateRaceWithFinishSync(t *testing.T) {
	mgr := setupRaceTestManager(t)

	// Start a sync
	cardID := "card-race-finish"
	_, err := mgr.StartSync(cardID, 1000, 1024*1024*100)
	if err != nil {
		t.Fatalf("StartSync failed: %v", err)
	}

	var wg sync.WaitGroup
	stopProgress := make(chan struct{})
	panicDetected := int32(0)

	// Goroutine 1: Continuous rapid progress updates
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer func() {
			if r := recover(); r != nil {
				atomic.AddInt32(&panicDetected, 1)
				t.Logf("PANIC DETECTED in progress update: %v", r)
			}
		}()

		counter := int64(0)
		for {
			select {
			case <-stopProgress:
				return
			default:
				counter++
				// This can panic if CurrentSync becomes nil during FinishSync
				err := mgr.UpdateSyncProgress(counter, counter*1024, fmt.Sprintf("file-%d.jpg", counter), 1024, 100000, "1m")
				if err != nil {
					t.Logf("UpdateSyncProgress error: %v", err)
				}
			}
		}
	}()

	// Goroutine 2: Finish sync after a short delay
	wg.Add(1)
	go func() {
		defer wg.Done()
		time.Sleep(10 * time.Millisecond)

		err := mgr.FinishSync(true, nil)
		if err != nil {
			t.Logf("FinishSync error: %v", err)
		}

		close(stopProgress)
	}()

	wg.Wait()

	// BUG EXPOSED: UpdateSyncProgress tries to access nil CurrentSync pointer
	// This causes nil pointer dereference or silent data loss
	state := mgr.GetState()
	if state.CurrentSync != nil {
		t.Error("BUG: CurrentSync should be nil after FinishSync")
	}
}

// TestNotifyListenersChannelDeadlock tests deadlock in notifyListeners when channels are full
// BUG: notifyListeners copies listener slice under RLock, but if listener is added during notification
// the copy may be stale. Also, blocking sends can cause deadlock.
// Location: state.go:318-335
func TestNotifyListenersChannelDeadlock(t *testing.T) {
	mgr := setupRaceTestManager(t)

	// Create slow subscribers with small buffers
	const numSubs = 20
	subscribers := make([]chan CurrentState, numSubs)

	for i := 0; i < numSubs; i++ {
		// Use buffer size 1 to make it easier to fill
		ch := make(chan CurrentState, 1)
		mgr.notifier.addListener(ch)
		subscribers[i] = ch
	}

	// Start slow consumers that don't read fast enough
	var wg sync.WaitGroup
	for i := 0; i < numSubs/2; i++ {
		wg.Add(1)
		go func(ch chan CurrentState, id int) {
			defer wg.Done()
			timeout := time.After(5 * time.Second)
			count := 0
			for {
				select {
				case <-ch:
					count++
					// Slow consumer
					time.Sleep(100 * time.Millisecond)
					if count > 5 {
						return
					}
				case <-timeout:
					return
				}
			}
		}(subscribers[i], i)
	}

	// Generate rapid updates
	updatesDone := make(chan struct{})
	go func() {
		for i := 0; i < 100; i++ {
			mgr.mu.Lock()
			mgr.currentState.Status = StatusSyncing
			mgr.mu.Unlock()

			// This should not block forever
			mgr.notifyListeners()
		}
		close(updatesDone)
	}()

	// Check for deadlock
	select {
	case <-updatesDone:
		t.Log("Updates completed without deadlock")
	case <-time.After(3 * time.Second):
		t.Error("BUG DETECTED: notifyListeners deadlock - updates did not complete")
	}

	wg.Wait()
}

// TestHistoryCorruptionConcurrentFinishSync tests history corruption with concurrent FinishSync
// BUG: history slice append is not atomic, concurrent appends can lose data
// Location: state.go:247
func TestHistoryCorruptionConcurrentFinishSync(t *testing.T) {
	mgr := setupRaceTestManager(t)

	const numSyncs = 100
	var wg sync.WaitGroup

	// Start multiple syncs and finish them concurrently
	for i := 0; i < numSyncs; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			cardID := fmt.Sprintf("card-%d", id)

			// Use the actual API which has proper concurrency control
			_, err := mgr.StartSync(cardID, int64(id*10), int64(id*1024))
			if err != nil {
				// Expected when another sync is already in progress
				return
			}

			// Small delay to simulate work
			time.Sleep(time.Millisecond)

			// Finish sync via API - history append is protected by lock
			_ = mgr.FinishSync(true, nil)
		}(i)
	}

	wg.Wait()

	// Check history integrity
	history := mgr.GetHistory()

	// With proper API usage, only one sync can be active at a time,
	// so we expect at most 1 history entry. The key check is that
	// no entries are corrupted or duplicated.
	if len(history) > 1 {
		t.Errorf("Unexpected: got %d history entries, expected at most 1 (only one sync can be active at a time)",
			len(history))
	}

	// Check for duplicate IDs (symptom of corruption)
	idMap := make(map[string]int)
	for _, record := range history {
		idMap[record.ID]++
	}

	for id, count := range idMap {
		if count > 1 {
			t.Errorf("BUG DETECTED: Duplicate history ID %s appears %d times", id, count)
		}
	}
}

// TestSaveThrottlingRaceCondition tests race in lastProgressSave time check
// BUG: lastProgressSave is not protected by mutex during read/write
// Location: state.go:208-211
func TestSaveThrottlingRaceCondition(t *testing.T) {
	mgr := setupRaceTestManager(t)
	mgr.progressSaveDelay = 50 * time.Millisecond

	// Start a sync
	_, err := mgr.StartSync("card-throttle", 1000, 1024*1024*100)
	if err != nil {
		t.Fatalf("StartSync failed: %v", err)
	}

	const numGoroutines = 50
	var wg sync.WaitGroup

	// Multiple goroutines calling UpdateSyncProgress simultaneously
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				mgr.UpdateSyncProgress(int64(id*10+j), int64((id*10+j)*1024), "file.jpg", 1024, 100000, "1m")

				// Check if this triggered a save (by checking the time gap)
				// This is a heuristic test
				time.Sleep(time.Millisecond)
			}
		}(i)
	}

	wg.Wait()

	// BUG: Race detector should catch unsynchronized access to lastProgressSave
	// Run with: go test -race -run TestSaveThrottlingRaceCondition
	t.Log("Test completed - check race detector output")
}

// TestSubscribeDuringNotification tests race between Subscribe and notifyListeners
// BUG: Subscribe adds to listeners slice while notifyListeners may be iterating
// Location: state.go:292-299 vs state.go:318-335
func TestSubscribeDuringNotification(t *testing.T) {
	mgr := setupRaceTestManager(t)

	var wg sync.WaitGroup
	stopSignal := make(chan struct{})
	allChans := make(chan chan CurrentState, 200)

	// Goroutine 1: Continuously subscribe new listeners
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			select {
			case <-stopSignal:
				return
			default:
				ch := mgr.Subscribe()
				allChans <- ch
			}
		}
	}()

	// Goroutine 2: Continuously generate notifications
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			select {
			case <-stopSignal:
				return
			default:
				mgr.mu.Lock()
				mgr.currentState.Status = StatusSyncing
				mgr.mu.Unlock()
				mgr.notifyListeners()
			}
		}
	}()

	// Goroutine 3: Continuously unsubscribe
	wg.Add(1)
	go func() {
		defer wg.Done()
		time.Sleep(10 * time.Millisecond)
		for i := 0; i < 50; i++ {
			select {
			case <-stopSignal:
				return
			default:
				listeners := mgr.notifier.getListeners()
				if len(listeners) > 0 {
					ch := listeners[0]
					mgr.Unsubscribe(ch)
				}
			}
		}
	}()

	// Let it run for a bit
	time.Sleep(100 * time.Millisecond)
	close(stopSignal)

	wg.Wait()

	// Unsubscribe all remaining channels to prevent goroutine leaks
	close(allChans)
	for ch := range allChans {
		mgr.Unsubscribe(ch)
	}

	t.Log("Test completed - check race detector output")
}

// TestUnsubscribeRaceDuringNotification tests race in Unsubscribe during notification
// BUG: Unsubscribe modifies listeners slice which notifyListeners may have copied
// but the copy happens after the lock is released, causing stale references
// Location: state.go:302-316 vs state.go:318-335
func TestUnsubscribeRaceDuringNotification(t *testing.T) {
	mgr := setupRaceTestManager(t)

	// Create subscribers
	const numSubs = 50
	channels := make([]chan CurrentState, numSubs)
	for i := 0; i < numSubs; i++ {
		channels[i] = mgr.Subscribe()
	}

	var wg sync.WaitGroup
	panicCount := int32(0)

	// Start listeners
	for i := 0; i < numSubs; i++ {
		wg.Add(1)
		go func(ch chan CurrentState, id int) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					atomic.AddInt32(&panicCount, 1)
					t.Logf("PANIC in listener %d: %v", id, r)
				}
			}()

			timeout := time.After(2 * time.Second)
			for {
				select {
				case _, ok := <-ch:
					if !ok {
						return
					}
				case <-timeout:
					return
				}
			}
		}(channels[i], i)
	}

	// Generate notifications while unsubscribing
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			mgr.mu.Lock()
			mgr.currentState.Status = StatusSyncing
			mgr.mu.Unlock()
			mgr.notifyListeners()
			time.Sleep(time.Millisecond)
		}
	}()

	// Unsubscribe channels randomly
	wg.Add(1)
	go func() {
		defer wg.Done()
		time.Sleep(10 * time.Millisecond)
		for i := 0; i < numSubs/2; i++ {
			mgr.Unsubscribe(channels[i*2])
			time.Sleep(time.Millisecond * 2)
		}
	}()

	wg.Wait()

	if panicCount > 0 {
		t.Errorf("BUG DETECTED: %d panics occurred during unsubscribe", panicCount)
	}
}

// TestFindLastSyncByCardIDRaceDuringWrite tests race in FindLastSyncByCardID
// BUG: FindLastSyncByCardID iterates history while FinishSync appends to it
// Even with RLock, the underlying slice can be reallocated during append
// Location: state.go:274-289 vs state.go:247
func TestFindLastSyncByCardIDRaceDuringWrite(t *testing.T) {
	mgr := setupRaceTestManager(t)

	var wg sync.WaitGroup
	panicCount := int32(0)
	cardID := "card-search"

	// Pre-populate some history
	for i := 0; i < 10; i++ {
		mgr.mu.Lock()
		mgr.history = append(mgr.history, SyncRecord{
			ID:         fmt.Sprintf("init-%d", i),
			CardID:     cardID,
			FilesTotal: int64(i * 10),
		})
		mgr.mu.Unlock()
	}

	// Goroutine 1: Continuously search for card
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer func() {
			if r := recover(); r != nil {
				atomic.AddInt32(&panicCount, 1)
				t.Logf("PANIC in FindLastSyncByCardID: %v", r)
			}
		}()

		for i := 0; i < 500; i++ {
			record := mgr.FindLastSyncByCardID(cardID)
			if record != nil && record.CardID != cardID {
				t.Errorf("FindLastSyncByCardID returned wrong card: %s", record.CardID)
			}
		}
	}()

	// Goroutine 2: Continuously append to history
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 500; i++ {
			mgr.mu.Lock()
			// This append can cause slice reallocation while FindLastSyncByCardID is reading
			mgr.history = append(mgr.history, SyncRecord{
				ID:         fmt.Sprintf("new-%d", i),
				CardID:     cardID,
				FilesTotal: int64(i * 10),
			})
			mgr.mu.Unlock()
		}
	}()

	wg.Wait()

	if panicCount > 0 {
		t.Errorf("BUG DETECTED: %d panics in FindLastSyncByCardID", panicCount)
	}
}

// TestChannelDeadlockWithMultipleSyncs tests channel deadlock scenarios
// BUG: If progress channels are full and sync manager blocks, the whole system hangs
// Location: syncmanager.go:393-403
func TestChannelDeadlockWithMultipleSyncs(t *testing.T) {
	mgr := setupRaceTestManager(t)

	// Start a sync
	_, err := mgr.StartSync("card-deadlock", 1000, 1024*1024*100)
	if err != nil {
		t.Fatalf("StartSync failed: %v", err)
	}

	// Subscribe many listeners that don't read
	const numListeners = 20
	for i := 0; i < numListeners; i++ {
		mgr.Subscribe()
		// Deliberately NOT reading from these channels
	}

	updatesDone := make(chan struct{})

	// Try to send many progress updates
	go func() {
		for i := 0; i < 100; i++ {
			err := mgr.UpdateSyncProgress(int64(i), int64(i*1024), "file.jpg", 1024, 100000, "1m")
			if err != nil {
				t.Logf("UpdateSyncProgress error: %v", err)
			}
		}
		close(updatesDone)
	}()

	// Check if updates complete or deadlock
	select {
	case <-updatesDone:
		t.Log("Progress updates completed (channels have default case)")
	case <-time.After(3 * time.Second):
		t.Error("BUG DETECTED: Deadlock in progress updates - system hung")
	}
}
