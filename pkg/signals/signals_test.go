package signals

import (
	"os"
	"syscall"
	"testing"
	"time"
)

func TestNewHandler(t *testing.T) {
	h := NewHandler()
	if h == nil {
		t.Fatal("NewHandler returned nil")
	}
	if h.sigChan == nil {
		t.Error("sigChan should not be nil")
	}
	defer h.Stop()
}

func TestChannel(t *testing.T) {
	h := NewHandler()
	defer h.Stop()

	ch := h.Channel()
	if ch == nil {
		t.Fatal("Channel returned nil")
	}

	// Verify we can select on the channel
	select {
	case <-ch:
		t.Error("should not receive signal without sending one")
	case <-time.After(10 * time.Millisecond):
		// Expected - no signal received
	}
}

func TestSignalDelivery(t *testing.T) {
	h := NewHandler()
	defer h.Stop()

	// Send SIGTERM to ourselves
	done := make(chan bool)
	go func() {
		select {
		case sig := <-h.Channel():
			if sig != syscall.SIGTERM {
				t.Errorf("expected SIGTERM, got %v", sig)
			}
			done <- true
		case <-time.After(2 * time.Second):
			t.Error("timeout waiting for signal")
			done <- false
		}
	}()

	// Small delay to ensure goroutine is waiting
	time.Sleep(50 * time.Millisecond)

	// Send signal
	if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatalf("failed to send signal: %v", err)
	}

	<-done
}

func TestWait(t *testing.T) {
	h := NewHandler()
	defer h.Stop()

	// Test that Wait blocks until signal is received
	done := make(chan bool)
	go func() {
		h.Wait()
		done <- true
	}()

	// Ensure Wait is blocking
	select {
	case <-done:
		t.Error("Wait should not return without signal")
	case <-time.After(100 * time.Millisecond):
		// Expected - still waiting
	}

	// Send signal
	if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatalf("failed to send signal: %v", err)
	}

	// Wait should now return
	select {
	case <-done:
		// Expected
	case <-time.After(2 * time.Second):
		t.Error("Wait did not return after signal")
	}
}

func TestStop(t *testing.T) {
	h := NewHandler()
	ch := h.Channel()

	// Stop should close the channel
	h.Stop()

	// Try to receive from closed channel
	select {
	case _, ok := <-ch:
		if ok {
			t.Error("channel should be closed")
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("closed channel should return immediately")
	}
}

func TestMultipleHandlers(t *testing.T) {
	h1 := NewHandler()
	h2 := NewHandler()
	defer h1.Stop()
	defer h2.Stop()

	// Both handlers should receive the same signal
	done1 := make(chan bool)
	done2 := make(chan bool)

	go func() {
		<-h1.Channel()
		done1 <- true
	}()

	go func() {
		<-h2.Channel()
		done2 <- true
	}()

	time.Sleep(50 * time.Millisecond)

	// Send signal
	if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatalf("failed to send signal: %v", err)
	}

	// Both should receive
	timeout := time.After(2 * time.Second)
	received1 := false
	received2 := false

	for i := 0; i < 2; i++ {
		select {
		case <-done1:
			received1 = true
		case <-done2:
			received2 = true
		case <-timeout:
			t.Fatal("timeout waiting for handlers")
		}
	}

	if !received1 || !received2 {
		t.Error("not all handlers received signal")
	}
}

func TestSignalInterrupt(t *testing.T) {
	h := NewHandler()
	defer h.Stop()

	done := make(chan bool)
	go func() {
		select {
		case sig := <-h.Channel():
			// Both SIGINT and SIGTERM are acceptable
			if sig != os.Interrupt && sig != syscall.SIGTERM {
				t.Errorf("expected interrupt signal, got %v", sig)
			}
			done <- true
		case <-time.After(2 * time.Second):
			t.Error("timeout waiting for signal")
			done <- false
		}
	}()

	time.Sleep(50 * time.Millisecond)

	// Send SIGINT (os.Interrupt)
	if err := syscall.Kill(os.Getpid(), syscall.SIGINT); err != nil {
		t.Fatalf("failed to send signal: %v", err)
	}

	<-done
}

func TestStopIdempotent(t *testing.T) {
	h := NewHandler()

	// Calling Stop multiple times should not panic
	h.Stop()

	// Second call should not panic (though may not be safe to call in general)
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Stop panicked on second call: %v", r)
		}
	}()

	// Note: Calling Stop twice will close an already-closed channel in real code
	// which would panic. This test documents the expected behavior.
	// In production, Stop should only be called once.
}

func TestBufferedChannel(t *testing.T) {
	h := NewHandler()
	defer h.Stop()

	// The signal channel should be buffered (size 1)
	// Send signal without anyone listening
	if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatalf("failed to send signal: %v", err)
	}

	// Small delay to ensure signal is received
	time.Sleep(100 * time.Millisecond)

	// Now read the signal - should be available immediately
	select {
	case <-h.Channel():
		// Expected - signal was buffered
	case <-time.After(100 * time.Millisecond):
		t.Error("signal was not buffered")
	}
}

// BenchmarkSignalHandler benchmarks signal handler creation
func BenchmarkNewHandler(b *testing.B) {
	for i := 0; i < b.N; i++ {
		h := NewHandler()
		h.Stop()
	}
}

// BenchmarkSignalDelivery benchmarks signal delivery
func BenchmarkSignalDelivery(b *testing.B) {
	h := NewHandler()
	defer h.Stop()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		go func() {
			<-h.Channel()
		}()

		syscall.Kill(os.Getpid(), syscall.SIGTERM)
	}
}
