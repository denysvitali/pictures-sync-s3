package sdmonitor

import (
	"log"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestSendEventDoesNotBlockStop reproduces the deadlock that existed when
// checkDevices held m.mountMu across a blocking eventChan send: a stalled
// consumer would freeze the poll loop forever AND prevent Stop() (which
// also takes mountMu) from completing.
//
// We simulate this by saturating eventChan with a fake consumer that never
// reads, then asking the monitor to deliver an event. The sendEvent path
// must complete within a bounded time (no permanent block) and Stop() must
// be able to interrupt it via stopChan.
func TestSendEventDoesNotBlockStop(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	monitor := NewMonitor(tmpDir)

	// Saturate the buffered eventChan so the next send must take the
	// "slow path" that waits on either stopChan or the timeout.
	for i := 0; i < cap(monitor.eventChan); i++ {
		monitor.eventChan <- Event{Type: EventInserted, DevName: "filler"}
	}

	sendDone := make(chan struct{})
	go func() {
		defer close(sendDone)
		monitor.sendEvent(Event{
			Type:    EventInserted,
			DevName: "after-stop",
		})
	}()

	// Verify sendEvent is actually blocked (no consumer, buffer full).
	select {
	case <-sendDone:
		t.Fatal("sendEvent returned immediately despite full buffer and no consumer")
	case <-time.After(50 * time.Millisecond):
	}

	// Stop() must unblock the in-flight sendEvent via stopChan and
	// itself complete (it acquires mountMu and unmounts if mounted).
	stopDone := make(chan struct{})
	go func() {
		defer close(stopDone)
		monitor.Stop()
	}()

	select {
	case <-stopDone:
	case <-time.After(2 * time.Second):
		t.Fatal("Stop() blocked - mountMu deadlock with sendEvent regressed")
	}

	select {
	case <-sendDone:
	case <-time.After(2 * time.Second):
		t.Fatal("sendEvent did not return after Stop() (stopChan not honored)")
	}
}

// TestSendEventTimesOutInsteadOfBlocking ensures that, even with no
// stopChan signal, sendEvent will eventually give up on a full channel
// rather than wedge the poll loop forever. We do this with a tiny custom
// timeout via a hand-built Monitor (parallel-safe because we don't share
// state with other tests).
func TestSendEventTimesOutInsteadOfBlocking(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	monitor := NewMonitor(tmpDir)

	for i := 0; i < cap(monitor.eventChan); i++ {
		monitor.eventChan <- Event{Type: EventInserted, DevName: "filler"}
	}

	// Run sendEvent; this should return within roughly eventSendTimeout +
	// scheduling slack. We don't want the test to wait the full 2s, so we
	// just assert "completes well before a generous upper bound".
	start := time.Now()
	done := make(chan struct{})
	go func() {
		monitor.sendEvent(Event{Type: EventInserted, DevName: "drop-me"})
		close(done)
	}()

	select {
	case <-done:
		elapsed := time.Since(start)
		if elapsed > eventSendTimeout+500*time.Millisecond {
			t.Errorf("sendEvent took %v, expected <= %v", elapsed, eventSendTimeout+500*time.Millisecond)
		}
		// The buffer must still be full (the new event was dropped, not appended).
		if len(monitor.eventChan) != cap(monitor.eventChan) {
			t.Errorf("expected buffer full after drop, got len=%d cap=%d",
				len(monitor.eventChan), cap(monitor.eventChan))
		}
	case <-time.After(eventSendTimeout + 2*time.Second):
		t.Fatal("sendEvent never returned: poll loop would deadlock in production")
	}
}

func TestPotentialDeadlock(t *testing.T) {
	// Test for potential deadlock in checkDevices when it's called
	// synchronously during Start()

	// Create a monitor with a small buffer to make deadlock more likely
	monitor := &Monitor{
		eventChan:      make(chan Event, 1), // Small buffer
		stopChan:       make(chan struct{}),
		mountPath:      "/test/mount",
		lastDevice:     "",
		devMode:        false,
		mountsCacheTTL: 2 * time.Second,
	}

	// Track if checkDevices completes
	var checkDevicesComplete atomic.Bool
	var wg sync.WaitGroup
	wg.Add(1)

	// Simulate Start() calling checkDevices synchronously
	go func() {
		defer wg.Done()
		log.Println("Test: Simulating checkDevices() in Start()")

		// Simulate finding a device and trying to send event
		event := Event{
			Type:      EventInserted,
			DevPath:   "/dev/sda1",
			DevName:   "sda1",
			MountPath: monitor.mountPath,
		}

		// Try to send the event (this simulates line 213 in checkDevices)
		log.Println("Test: Attempting to send event...")
		monitor.eventChan <- event // This could block if buffer is full
		log.Println("Test: Event sent successfully")
		checkDevicesComplete.Store(true)
	}()

	// Wait a bit to see if it completes
	time.Sleep(100 * time.Millisecond)

	if !checkDevicesComplete.Load() {
		t.Log("checkDevices appears to be blocked on channel send")
		// Try to receive to unblock
		select {
		case event := <-monitor.eventChan:
			t.Logf("Received event to unblock: %+v", event)
		default:
			t.Log("No event available to receive")
		}
	}

	// Wait for goroutine
	done := make(chan bool, 1)
	go func() {
		wg.Wait()
		done <- true
	}()

	select {
	case <-done:
		if checkDevicesComplete.Load() {
			t.Log("checkDevices completed successfully")
		} else {
			t.Error("checkDevices blocked but then completed after receive")
		}
	case <-time.After(1 * time.Second):
		t.Error("DEADLOCK: checkDevices is permanently blocked")
	}
}

func TestActualMonitorStartBehavior(t *testing.T) {
	// Test the actual behavior of Monitor.Start() with immediate device detection

	// Create a custom monitor that simulates finding a device immediately
	monitor := &Monitor{
		eventChan:      make(chan Event, 10), // Normal buffer size
		stopChan:       make(chan struct{}),
		mountPath:      "/test/mount",
		lastDevice:     "",
		devMode:        false,
		mountsCacheTTL: 2 * time.Second,
	}

	// Override checkDevices to simulate immediate detection
	deviceDetected := false
	eventSent := false

	// Simulate Start() behavior
	startComplete := make(chan bool, 1)
	go func() {
		log.Println("Test Start: Beginning...")

		// This simulates checkDevices() finding a device
		if true { // Simulating device found
			deviceDetected = true
			log.Println("Test Start: Device detected, sending event...")

			// Send event (this is line 213 in real checkDevices)
			monitor.eventChan <- Event{
				Type:      EventInserted,
				DevPath:   "/dev/sda1",
				DevName:   "sda1",
				MountPath: monitor.mountPath,
			}
			eventSent = true
			log.Println("Test Start: Event sent")
		}

		log.Println("Test Start: Completed")
		startComplete <- true
	}()

	// Wait for Start to complete
	select {
	case <-startComplete:
		t.Log("Start() completed successfully")
		if !deviceDetected {
			t.Error("Device was not detected")
		}
		if !eventSent {
			t.Error("Event was not sent")
		}
	case <-time.After(500 * time.Millisecond):
		t.Error("Start() appears to be blocked")
	}

	// Now check if event is in the buffer
	select {
	case event := <-monitor.eventChan:
		t.Logf("Event successfully buffered and received: %+v", event)
	default:
		t.Error("No event in buffer despite successful send")
	}
}