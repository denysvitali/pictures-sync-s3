package sdmonitor

import (
	"log"
	"runtime"
	"testing"
	"time"
)

func TestExactProductionScenario(t *testing.T) {
	// This test reproduces the EXACT scenario from production
	// where the SD card is already inserted when the monitor starts

	log.SetFlags(log.LstdFlags | log.Lshortfile)

	// Step 1: Create the monitor (main.go line 76)
	mountPath := "/test/mount"
	monitor := NewMonitor(mountPath)

	// Verify the channel is created and buffered
	if monitor.eventChan == nil {
		t.Fatal("Event channel is nil!")
	}

	// Step 2: Simulate what Start() does
	t.Log("Simulating Start()...")

	// Start() would normally call checkDevices() synchronously
	// Let's simulate this with a goroutine to track blocking
	checkDevicesStarted := make(chan bool, 1)
	checkDevicesCompleted := make(chan bool, 1)

	go func() {
		checkDevicesStarted <- true
		t.Log("checkDevices: Starting...")

		// Simulate device detection (as in checkDevices lines 199-218)
		device := "/dev/sda1"

		// This is the sequence from checkDevices:
		// 1. Device is found (line 199)
		// 2. Device != lastDevice check passes (line 201)
		// 3. Log "SD card detected" (line 203)
		t.Logf("checkDevices: SD card detected: %s", device)

		// 4. Mount succeeds (lines 206-209)
		// We'll skip actual mounting in test
		t.Logf("checkDevices: Mounted %s", device)

		// 5. Update lastDevice (line 211)
		monitor.lastDevice = device

		// 6. Log "SD card inserted" (line 212)
		t.Logf("checkDevices: SD card inserted: %s, mounted at %s", device, mountPath)

		// 7. Send event to channel (line 213)
		t.Log("checkDevices: About to send event to channel...")

		// Check goroutine count before send
		goroutinesBefore := runtime.NumGoroutine()
		t.Logf("Goroutines before send: %d", goroutinesBefore)

		// THIS IS THE CRITICAL LINE - line 213 in checkDevices
		select {
		case monitor.eventChan <- Event{
			Type:      EventInserted,
			DevPath:   device,
			DevName:   "sda1",
			MountPath: mountPath,
		}:
			t.Log("checkDevices: Event sent successfully!")
			checkDevicesCompleted <- true
		case <-time.After(1 * time.Second):
			t.Error("checkDevices: BLOCKED on channel send!")
			checkDevicesCompleted <- false
		}
	}()

	// Wait for checkDevices to start
	<-checkDevicesStarted

	// Give it time to potentially block
	time.Sleep(100 * time.Millisecond)

	// Check if it completed
	select {
	case success := <-checkDevicesCompleted:
		if success {
			t.Log("✓ checkDevices completed successfully - no blocking")

			// Verify event is in buffer
			select {
			case event := <-monitor.eventChan:
				t.Logf("✓ Event is in buffer: %+v", event)
			default:
				t.Error("✗ No event in buffer!")
			}
		} else {
			t.Error("✗ checkDevices blocked on channel send")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("✗ Test timeout - checkDevices is completely blocked")
	}
}

func TestChannelCapacity(t *testing.T) {
	// Test that the channel actually has the expected capacity

	monitor := NewMonitor("/test")

	// The channel should have a buffer of 10
	expectedCapacity := 10

	// Fill the buffer
	for i := 0; i < expectedCapacity; i++ {
		select {
		case monitor.eventChan <- Event{Type: EventInserted, DevName: "test"}:
			t.Logf("Sent event %d", i+1)
		default:
			t.Errorf("Channel blocked at event %d, expected capacity %d", i+1, expectedCapacity)
			return
		}
	}

	// Now it should block
	select {
	case monitor.eventChan <- Event{Type: EventInserted, DevName: "overflow"}:
		t.Error("Channel accepted more events than expected capacity")
	default:
		t.Logf("✓ Channel correctly blocks after %d events", expectedCapacity)
	}

	// Drain the channel
	for i := 0; i < expectedCapacity; i++ {
		<-monitor.eventChan
	}

	// Should be empty now
	select {
	case <-monitor.eventChan:
		t.Error("Channel had more events than sent")
	default:
		t.Log("✓ Channel is empty after draining")
	}
}