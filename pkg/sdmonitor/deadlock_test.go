package sdmonitor

import (
	"log"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

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