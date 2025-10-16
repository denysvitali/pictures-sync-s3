package sdmonitor

import (
	"testing"
	"time"
)

func TestImmediateCardDetection(t *testing.T) {
	// This test simulates the scenario where an SD card is already inserted
	// when the monitor starts, reproducing the race condition

	monitor := &Monitor{
		eventChan:      make(chan Event, 10),
		stopChan:       make(chan struct{}),
		mountPath:      "/test/mount",
		lastDevice:     "", // No device initially
		devMode:        false,
		mountsCacheTTL: 2 * time.Second,
	}

	// Simulate finding a device and sending an event
	// This happens in checkDevices() called from Start()
	device := "/dev/sda1"

	// This simulates what happens when checkDevices() finds a new device
	t.Logf("Simulating device detection: %s", device)

	// Send the event (this is what happens in checkDevices)
	event := Event{
		Type:      EventInserted,
		DevPath:   device,
		DevName:   "sda1",
		MountPath: monitor.mountPath,
	}

	// Try to send event immediately (simulating what happens in Start())
	select {
	case monitor.eventChan <- event:
		t.Log("Event sent successfully")
	default:
		t.Error("Event channel blocked - this shouldn't happen with buffered channel")
	}

	// Now simulate the main loop trying to read the event
	// but it might have started AFTER the event was sent
	eventReceived := false

	// Try to receive the event with a timeout
	select {
	case receivedEvent := <-monitor.eventChan:
		t.Logf("Event received: %+v", receivedEvent)
		eventReceived = true
	case <-time.After(100 * time.Millisecond):
		t.Log("No event received within timeout")
	}

	if !eventReceived {
		t.Error("Event was sent but not received - possible race condition")
	}
}

func TestStartupRaceCondition(t *testing.T) {
	// This test demonstrates the actual race condition in the production code

	monitor := &Monitor{
		eventChan:      make(chan Event, 10),
		stopChan:       make(chan struct{}),
		mountPath:      "/test/mount",
		lastDevice:     "",
		devMode:        false,
		mountsCacheTTL: 2 * time.Second,
	}

	// Simulate the sequence that happens in production:
	// 1. Start() is called
	// 2. checkDevices() is called immediately (line 89 in Start())
	// 3. Device is found and event is sent to channel
	// 4. Start() returns
	// 5. Main loop calls Events() to get the channel
	// 6. Main loop starts listening

	// Step 1-3: Simulate immediate device detection in Start()
	go func() {
		// This simulates checkDevices() finding a device immediately
		event := Event{
			Type:      EventInserted,
			DevPath:   "/dev/sda1",
			DevName:   "sda1",
			MountPath: monitor.mountPath,
		}
		monitor.eventChan <- event
		t.Log("Event sent during simulated Start()")
	}()

	// Small delay to ensure the event is sent
	time.Sleep(10 * time.Millisecond)

	// Step 5-6: Main loop gets the channel and starts listening
	eventChan := monitor.Events()
	t.Log("Main loop obtained event channel")

	// The event was already sent, so with a buffered channel it should still be available
	select {
	case event := <-eventChan:
		t.Logf("SUCCESS: Event received from buffered channel: %+v", event)
	case <-time.After(100 * time.Millisecond):
		t.Error("FAILURE: Event lost - race condition confirmed!")
	}
}

func TestBufferedChannelPreventsLoss(t *testing.T) {
	// This test verifies that the buffered channel (size 10) prevents event loss

	monitor := &Monitor{
		eventChan:      make(chan Event, 10), // Buffered channel
		stopChan:       make(chan struct{}),
		mountPath:      "/test/mount",
		lastDevice:     "",
		devMode:        false,
		mountsCacheTTL: 2 * time.Second,
	}

	// Send event before anyone is listening
	event := Event{
		Type:      EventInserted,
		DevPath:   "/dev/sda1",
		DevName:   "sda1",
		MountPath: monitor.mountPath,
	}

	monitor.eventChan <- event
	t.Log("Event sent to buffered channel before listener exists")

	// Simulate delay before main loop starts listening
	time.Sleep(50 * time.Millisecond)

	// Now start listening (simulating main loop)
	select {
	case receivedEvent := <-monitor.eventChan:
		t.Logf("SUCCESS: Buffered channel preserved event: %+v", receivedEvent)
	case <-time.After(100 * time.Millisecond):
		t.Error("FAILURE: Event lost despite buffered channel")
	}
}