package events

import (
	"errors"
	"sync"
	"testing"
	"time"
)

// TestNewManager tests creation of event manager
func TestNewManager(t *testing.T) {
	mgr := NewManager()
	if mgr == nil {
		t.Fatal("NewManager returned nil")
	}
	if mgr.listeners == nil {
		t.Error("listeners slice should be initialized")
	}
	if len(mgr.listeners) != 0 {
		t.Errorf("expected 0 listeners, got %d", len(mgr.listeners))
	}
}

// TestSubscribe tests event subscription
func TestSubscribe(t *testing.T) {
	mgr := NewManager()

	ch := mgr.Subscribe()
	if ch == nil {
		t.Fatal("Subscribe returned nil channel")
	}

	mgr.mu.RLock()
	if len(mgr.listeners) != 1 {
		t.Errorf("expected 1 listener, got %d", len(mgr.listeners))
	}
	mgr.mu.RUnlock()
}

// TestMultipleSubscribers tests multiple subscribers
func TestMultipleSubscribers(t *testing.T) {
	mgr := NewManager()

	ch1 := mgr.Subscribe()
	ch2 := mgr.Subscribe()
	ch3 := mgr.Subscribe()

	if ch1 == nil || ch2 == nil || ch3 == nil {
		t.Fatal("Subscribe returned nil channel")
	}

	mgr.mu.RLock()
	if len(mgr.listeners) != 3 {
		t.Errorf("expected 3 listeners, got %d", len(mgr.listeners))
	}
	mgr.mu.RUnlock()
}

// TestUnsubscribe tests unsubscribing from events
func TestUnsubscribe(t *testing.T) {
	mgr := NewManager()

	ch := mgr.Subscribe()

	mgr.mu.RLock()
	initialCount := len(mgr.listeners)
	mgr.mu.RUnlock()

	mgr.Unsubscribe(ch)

	mgr.mu.RLock()
	finalCount := len(mgr.listeners)
	mgr.mu.RUnlock()

	if finalCount != initialCount-1 {
		t.Errorf("expected %d listeners after unsubscribe, got %d", initialCount-1, finalCount)
	}

	// Channel should be closed
	_, ok := <-ch
	if ok {
		t.Error("channel should be closed after unsubscribe")
	}
}

// TestEmit tests basic event emission
func TestEmit(t *testing.T) {
	mgr := NewManager()
	ch := mgr.Subscribe()

	testData := map[string]interface{}{
		"key": "value",
	}

	mgr.Emit(EventInfo, "test message", testData)

	select {
	case event := <-ch:
		if event.Type != EventInfo {
			t.Errorf("expected event type %s, got %s", EventInfo, event.Type)
		}
		if event.Message != "test message" {
			t.Errorf("expected message 'test message', got '%s'", event.Message)
		}
		if event.Data["key"] != "value" {
			t.Errorf("expected data key 'value', got '%v'", event.Data["key"])
		}
		if event.Timestamp.IsZero() {
			t.Error("timestamp should not be zero")
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for event")
	}
}

// TestEmitMultipleSubscribers tests event delivery to multiple subscribers
func TestEmitMultipleSubscribers(t *testing.T) {
	mgr := NewManager()

	ch1 := mgr.Subscribe()
	ch2 := mgr.Subscribe()
	ch3 := mgr.Subscribe()

	mgr.Emit(EventInfo, "broadcast", nil)

	// All subscribers should receive the event
	var wg sync.WaitGroup
	wg.Add(3)

	checkEvent := func(ch chan Event, name string) {
		defer wg.Done()
		select {
		case event := <-ch:
			if event.Message != "broadcast" {
				t.Errorf("%s: expected message 'broadcast', got '%s'", name, event.Message)
			}
		case <-time.After(1 * time.Second):
			t.Errorf("%s: timeout waiting for event", name)
		}
	}

	go checkEvent(ch1, "subscriber1")
	go checkEvent(ch2, "subscriber2")
	go checkEvent(ch3, "subscriber3")

	wg.Wait()
}

// TestEmitAfterUnsubscribe tests that unsubscribed channels don't receive events
func TestEmitAfterUnsubscribe(t *testing.T) {
	mgr := NewManager()

	ch1 := mgr.Subscribe()
	ch2 := mgr.Subscribe()

	// Unsubscribe first channel
	mgr.Unsubscribe(ch1)

	// Emit event
	mgr.Emit(EventInfo, "test", nil)

	// ch2 should receive event
	select {
	case <-ch2:
		// Expected
	case <-time.After(1 * time.Second):
		t.Error("ch2 should receive event")
	}

	// ch1 should be closed and not receive event
	select {
	case _, ok := <-ch1:
		if ok {
			t.Error("ch1 should be closed")
		}
	case <-time.After(100 * time.Millisecond):
		// Expected - closed channel returns immediately
	}
}

// TestConcurrentEmit tests concurrent event emission
func TestConcurrentEmit(t *testing.T) {
	mgr := NewManager()
	ch := mgr.Subscribe()

	const numEvents = 100
	var wg sync.WaitGroup

	// Emit events concurrently
	for i := 0; i < numEvents; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			mgr.Emit(EventInfo, "concurrent", map[string]interface{}{
				"num": n,
			})
		}(i)
	}

	// Collect events
	received := 0
	done := make(chan struct{})

	go func() {
		for range ch {
			received++
			if received >= numEvents {
				close(done)
				return
			}
		}
	}()

	wg.Wait()

	select {
	case <-done:
		if received != numEvents {
			t.Errorf("expected %d events, got %d", numEvents, received)
		}
	case <-time.After(5 * time.Second):
		t.Errorf("timeout: received only %d/%d events", received, numEvents)
	}
}

// TestConcurrentSubscribeUnsubscribe tests concurrent subscribe/unsubscribe operations
func TestConcurrentSubscribeUnsubscribe(t *testing.T) {
	mgr := NewManager()

	var wg sync.WaitGroup
	const numOperations = 50

	// Concurrent subscribes
	channels := make([]chan Event, numOperations)
	for i := 0; i < numOperations; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			channels[idx] = mgr.Subscribe()
		}(i)
	}
	wg.Wait()

	// Concurrent unsubscribes
	for i := 0; i < numOperations; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			if channels[idx] != nil {
				mgr.Unsubscribe(channels[idx])
			}
		}(i)
	}
	wg.Wait()

	mgr.mu.RLock()
	if len(mgr.listeners) != 0 {
		t.Errorf("expected 0 listeners after unsubscribe all, got %d", len(mgr.listeners))
	}
	mgr.mu.RUnlock()
}

// TestBufferOverflow tests behavior when channel buffer is full
func TestBufferOverflow(t *testing.T) {
	mgr := NewManager()
	ch := mgr.Subscribe()

	// Fill the buffer (channel has buffer of 100)
	for i := 0; i < 150; i++ {
		mgr.Emit(EventInfo, "overflow test", nil)
	}

	// Should not block even though buffer is full
	// Events that don't fit in buffer are dropped
	time.Sleep(100 * time.Millisecond)

	// Drain channel
	count := 0
	timeout := time.After(1 * time.Second)
	for {
		select {
		case <-ch:
			count++
		case <-timeout:
			// Buffer was full, so we should have received up to 100 events
			if count > 100 {
				t.Errorf("received more events than buffer size: %d", count)
			}
			return
		default:
			// No more events
			return
		}
	}
}

// TestEmitSDCardInserted tests SD card insertion event
func TestEmitSDCardInserted(t *testing.T) {
	mgr := NewManager()
	ch := mgr.Subscribe()

	mgr.EmitSDCardInserted("sdX", "/mnt/sdcard")

	select {
	case event := <-ch:
		if event.Type != EventSDCardInserted {
			t.Errorf("expected event type %s, got %s", EventSDCardInserted, event.Type)
		}
		if event.Data["device_name"] != "sdX" {
			t.Errorf("expected device_name 'sdX', got %v", event.Data["device_name"])
		}
		if event.Data["mount_path"] != "/mnt/sdcard" {
			t.Errorf("expected mount_path '/mnt/sdcard', got %v", event.Data["mount_path"])
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for event")
	}
}

// TestEmitSDCardRemoved tests SD card removal event
func TestEmitSDCardRemoved(t *testing.T) {
	mgr := NewManager()
	ch := mgr.Subscribe()

	mgr.EmitSDCardRemoved("sdX")

	select {
	case event := <-ch:
		if event.Type != EventSDCardRemoved {
			t.Errorf("expected event type %s, got %s", EventSDCardRemoved, event.Type)
		}
		if event.Data["device_name"] != "sdX" {
			t.Errorf("expected device_name 'sdX', got %v", event.Data["device_name"])
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for event")
	}
}

// TestEmitPhotosDetected tests photos detected event
func TestEmitPhotosDetected(t *testing.T) {
	mgr := NewManager()
	ch := mgr.Subscribe()

	mgr.EmitPhotosDetected(42, 1024*1024*500)

	select {
	case event := <-ch:
		if event.Type != EventPhotosDetected {
			t.Errorf("expected event type %s, got %s", EventPhotosDetected, event.Type)
		}
		if event.Data["photo_count"] != 42 {
			t.Errorf("expected photo_count 42, got %v", event.Data["photo_count"])
		}
		totalMB := event.Data["total_mb"].(float64)
		if totalMB < 499 || totalMB > 501 {
			t.Errorf("expected total_mb ~500, got %v", totalMB)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for event")
	}
}

// TestEmitCardIDFound tests card ID found event
func TestEmitCardIDFound(t *testing.T) {
	mgr := NewManager()
	ch := mgr.Subscribe()

	mgr.EmitCardIDFound("card-12345")

	select {
	case event := <-ch:
		if event.Type != EventCardIDFound {
			t.Errorf("expected event type %s, got %s", EventCardIDFound, event.Type)
		}
		if event.Data["card_id"] != "card-12345" {
			t.Errorf("expected card_id 'card-12345', got %v", event.Data["card_id"])
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for event")
	}
}

// TestEmitCardIDCreated tests card ID creation event
func TestEmitCardIDCreated(t *testing.T) {
	mgr := NewManager()
	ch := mgr.Subscribe()

	mgr.EmitCardIDCreated("card-new")

	select {
	case event := <-ch:
		if event.Type != EventCardIDCreated {
			t.Errorf("expected event type %s, got %s", EventCardIDCreated, event.Type)
		}
		if event.Data["card_id"] != "card-new" {
			t.Errorf("expected card_id 'card-new', got %v", event.Data["card_id"])
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for event")
	}
}

// TestEmitReformatDetected tests reformat detection event
func TestEmitReformatDetected(t *testing.T) {
	mgr := NewManager()
	ch := mgr.Subscribe()

	mgr.EmitReformatDetected("card-old", "card-new", 25.5)

	select {
	case event := <-ch:
		if event.Type != EventReformatDetected {
			t.Errorf("expected event type %s, got %s", EventReformatDetected, event.Type)
		}
		if event.Data["old_card_id"] != "card-old" {
			t.Errorf("expected old_card_id 'card-old', got %v", event.Data["old_card_id"])
		}
		if event.Data["new_card_id"] != "card-new" {
			t.Errorf("expected new_card_id 'card-new', got %v", event.Data["new_card_id"])
		}
		if event.Data["percentage_of_last"] != 25.5 {
			t.Errorf("expected percentage 25.5, got %v", event.Data["percentage_of_last"])
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for event")
	}
}

// TestEmitSyncStarted tests sync started event
func TestEmitSyncStarted(t *testing.T) {
	mgr := NewManager()
	ch := mgr.Subscribe()

	mgr.EmitSyncStarted("card-123", 100, 1024*1024*500)

	select {
	case event := <-ch:
		if event.Type != EventSyncStarted {
			t.Errorf("expected event type %s, got %s", EventSyncStarted, event.Type)
		}
		if event.Data["card_id"] != "card-123" {
			t.Errorf("expected card_id 'card-123', got %v", event.Data["card_id"])
		}
		if event.Data["total_files"] != 100 {
			t.Errorf("expected total_files 100, got %v", event.Data["total_files"])
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for event")
	}
}

// TestEmitSyncProgress tests sync progress event
func TestEmitSyncProgress(t *testing.T) {
	mgr := NewManager()
	ch := mgr.Subscribe()

	mgr.EmitSyncProgress(50, 1024*1024*250, "photo.jpg", 5.5, "2m30s")

	select {
	case event := <-ch:
		if event.Type != EventSyncProgress {
			t.Errorf("expected event type %s, got %s", EventSyncProgress, event.Type)
		}
		if event.Data["files_synced"] != int64(50) {
			t.Errorf("expected files_synced 50, got %v", event.Data["files_synced"])
		}
		if event.Data["current_file"] != "photo.jpg" {
			t.Errorf("expected current_file 'photo.jpg', got %v", event.Data["current_file"])
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for event")
	}
}

// TestEmitSyncCompleted tests sync completion event
func TestEmitSyncCompleted(t *testing.T) {
	mgr := NewManager()
	ch := mgr.Subscribe()

	duration := 5 * time.Minute
	mgr.EmitSyncCompleted("card-123", 100, 1024*1024*500, duration)

	select {
	case event := <-ch:
		if event.Type != EventSyncCompleted {
			t.Errorf("expected event type %s, got %s", EventSyncCompleted, event.Type)
		}
		if event.Data["card_id"] != "card-123" {
			t.Errorf("expected card_id 'card-123', got %v", event.Data["card_id"])
		}
		if event.Data["duration"] != duration.String() {
			t.Errorf("expected duration %s, got %v", duration.String(), event.Data["duration"])
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for event")
	}
}

// TestEmitSyncFailed tests sync failure event
func TestEmitSyncFailed(t *testing.T) {
	mgr := NewManager()
	ch := mgr.Subscribe()

	testErr := errors.New("network timeout")
	mgr.EmitSyncFailed("card-123", testErr)

	select {
	case event := <-ch:
		if event.Type != EventSyncFailed {
			t.Errorf("expected event type %s, got %s", EventSyncFailed, event.Type)
		}
		if event.Data["card_id"] != "card-123" {
			t.Errorf("expected card_id 'card-123', got %v", event.Data["card_id"])
		}
		if event.Data["error"] != "network timeout" {
			t.Errorf("expected error 'network timeout', got %v", event.Data["error"])
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for event")
	}
}

// TestEmitStatusChanged tests status change event
func TestEmitStatusChanged(t *testing.T) {
	mgr := NewManager()
	ch := mgr.Subscribe()

	mgr.EmitStatusChanged("idle", "syncing")

	select {
	case event := <-ch:
		if event.Type != EventStatusChanged {
			t.Errorf("expected event type %s, got %s", EventStatusChanged, event.Type)
		}
		if event.Data["old_status"] != "idle" {
			t.Errorf("expected old_status 'idle', got %v", event.Data["old_status"])
		}
		if event.Data["new_status"] != "syncing" {
			t.Errorf("expected new_status 'syncing', got %v", event.Data["new_status"])
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for event")
	}
}

// TestEmitError tests error event
func TestEmitError(t *testing.T) {
	mgr := NewManager()
	ch := mgr.Subscribe()

	testErr := errors.New("test error")
	mgr.EmitError("something went wrong", testErr)

	select {
	case event := <-ch:
		if event.Type != EventError {
			t.Errorf("expected event type %s, got %s", EventError, event.Type)
		}
		if event.Message != "something went wrong" {
			t.Errorf("expected message 'something went wrong', got %s", event.Message)
		}
		if event.Data["error"] != "test error" {
			t.Errorf("expected error 'test error', got %v", event.Data["error"])
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for event")
	}
}

// TestEmitErrorNil tests error event with nil error
func TestEmitErrorNil(t *testing.T) {
	mgr := NewManager()
	ch := mgr.Subscribe()

	mgr.EmitError("error without details", nil)

	select {
	case event := <-ch:
		if event.Type != EventError {
			t.Errorf("expected event type %s, got %s", EventError, event.Type)
		}
		if _, hasError := event.Data["error"]; hasError {
			t.Error("should not have error key when err is nil")
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for event")
	}
}

// TestEmitInfo tests info event
func TestEmitInfo(t *testing.T) {
	mgr := NewManager()
	ch := mgr.Subscribe()

	data := map[string]interface{}{
		"key1": "value1",
		"key2": 42,
	}
	mgr.EmitInfo("info message", data)

	select {
	case event := <-ch:
		if event.Type != EventInfo {
			t.Errorf("expected event type %s, got %s", EventInfo, event.Type)
		}
		if event.Message != "info message" {
			t.Errorf("expected message 'info message', got %s", event.Message)
		}
		if event.Data["key1"] != "value1" {
			t.Errorf("expected key1 'value1', got %v", event.Data["key1"])
		}
		if event.Data["key2"] != 42 {
			t.Errorf("expected key2 42, got %v", event.Data["key2"])
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for event")
	}
}

// BenchmarkEmit benchmarks event emission
func BenchmarkEmit(b *testing.B) {
	mgr := NewManager()
	ch := mgr.Subscribe()

	// Drain events in background
	go func() {
		for range ch {
		}
	}()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mgr.Emit(EventInfo, "benchmark", nil)
	}
}

// BenchmarkEmitMultipleSubscribers benchmarks with multiple subscribers
func BenchmarkEmitMultipleSubscribers(b *testing.B) {
	mgr := NewManager()

	// Create 10 subscribers
	for i := 0; i < 10; i++ {
		ch := mgr.Subscribe()
		go func() {
			for range ch {
			}
		}()
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mgr.Emit(EventInfo, "benchmark", nil)
	}
}
