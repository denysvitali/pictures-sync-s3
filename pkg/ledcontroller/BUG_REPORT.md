# LED Controller State Management - Bug Report

**Date:** 2025-10-15
**Package:** `pkg/ledcontroller`
**Test File:** `pkg/ledcontroller/ledcontroller_test.go`
**Severity:** CRITICAL

## Executive Summary

Comprehensive testing of the LED controller state management has revealed **7 critical bugs** that cause:
- Severe goroutine leaks (100+ goroutines leaked in rapid state transitions)
- Data races accessing `stopChan` without synchronization
- Double close panics on `stopChan`
- Resource exhaustion under normal operation
- Incorrect LED states that could mislead users

## Critical Bugs Found

### BUG #1: Massive Goroutine Leak in `updatePattern()` ⚠️ CRITICAL
**File:** `ledcontroller.go:132-163`
**Severity:** CRITICAL
**Impact:** Resource Exhaustion, System Instability

#### Description
The `updatePattern()` method creates new goroutines for LED patterns but **fails to properly stop old goroutines**. The method:

1. Tries to send to `stopChan` (line 135)
2. Creates a **NEW** `stopChan` (line 140)
3. Starts new goroutines reading from the NEW `stopChan`

**Problem:** Old goroutines are still reading from the OLD `stopChan`, which has been replaced. These goroutines never receive the stop signal and **leak forever**.

#### Evidence
```
Test: TestRapidStateTransitions
Initial goroutines: 2
Final goroutines: 110
Leaked: 108 goroutines (5400% increase!)

Test: TestConcurrentPatternChanges
Leaked: 67 goroutines

Test: TestStressTestWithMonitoring
Initial: 4 goroutines
Peak: 156 goroutines (3900% increase!)
Final: 29 goroutines leaked
```

#### Code Analysis
```go
// ledcontroller.go:132-140
func (c *Controller) updatePattern(status state.SyncStatus) {
	// Stop current pattern
	select {
	case c.stopChan <- struct{}{}:  // ← Sends to OLD channel
	default:
	}

	// Start new pattern
	c.stopChan = make(chan struct{})  // ← Creates NEW channel, orphaning old goroutines!
```

#### Recommended Fix
Use a mutex to synchronize channel replacement and ensure old goroutines are stopped:

```go
func (c *Controller) updatePattern(status state.SyncStatus) {
	c.mu.Lock()

	// Close old stopChan to signal all goroutines
	if c.stopChan != nil {
		close(c.stopChan)
	}

	// Create new stopChan
	c.stopChan = make(chan struct{})
	currentChan := c.stopChan
	c.mu.Unlock()

	// Wait briefly for old goroutines to exit
	time.Sleep(10 * time.Millisecond)

	// Start new pattern with the new channel
	switch status {
		// ... pattern logic using currentChan
	}
}
```

---

### BUG #2: Data Race on `stopChan` Field ⚠️ CRITICAL
**File:** `ledcontroller.go:140, 180`
**Severity:** CRITICAL
**Impact:** Race Condition, Undefined Behavior, Potential Crashes

#### Description
Multiple goroutines access `c.stopChan` concurrently without synchronization:
- `updatePattern()` WRITES to `c.stopChan` (line 140)
- `runPattern()` READS from `c.stopChan` (line 180)
- `Stop()` READS from `c.stopChan` (line 124)

#### Evidence
```
WARNING: DATA RACE
Write at 0x00c00010c658 by goroutine 9:
  (*Controller).updatePattern()
      ledcontroller.go:140

Previous read at 0x00c00010c658 by goroutine 10:
  (*Controller).runPattern()
      ledcontroller.go:180
```

This race was detected in **every single test** that involved state transitions.

#### Recommended Fix
Protect `stopChan` with a mutex:

```go
type Controller struct {
	mu              sync.RWMutex
	actLED          *LED
	pwrLED          *LED
	stopChan        chan struct{}
	currentPattern  LEDPattern
}

func (c *Controller) getStopChan() chan struct{} {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.stopChan
}
```

---

### BUG #3: Double Close of `stopChan` Causes Panic ⚠️ CRITICAL
**File:** `ledcontroller.go:124`
**Severity:** CRITICAL
**Impact:** Service Crash, LED Control Failure

#### Description
The `Stop()` method calls `close(c.stopChan)` without checking if the channel is already closed. If `Stop()` is called twice, the service **panics**.

#### Evidence
```
Test: TestCleanupOnShutdown
BUG: Stop() panicked on second call: close of closed channel

Test: TestDoubleCloseStopChan
BUG: Panic from double close: close of closed channel
```

#### Recommended Fix
```go
func (c *Controller) Stop() {
	c.mu.Lock()
	if c.stopChan != nil {
		close(c.stopChan)
		c.stopChan = nil
	}
	c.mu.Unlock()

	// Turn off LEDs
	if c.actLED != nil && c.actLED.available {
		c.actLED.SetBrightness(0)
	}
}
```

---

### BUG #4: Send to Closed Channel Can Panic ⚠️ HIGH
**File:** `ledcontroller.go:135`
**Severity:** HIGH
**Impact:** Service Crash

#### Description
`updatePattern()` tries to send to `stopChan` using `select`. If `Stop()` has already closed the channel, this can panic.

```go
select {
case c.stopChan <- struct{}{}:  // ← PANICS if stopChan is closed!
default:
}
```

The `default` case prevents blocking, but doesn't prevent the panic from sending to a closed channel.

#### Evidence
Race detector shows concurrent access between `updatePattern()` sending and `Stop()` closing.

#### Recommended Fix
Check if channel is closed before sending, or use a different signaling mechanism.

---

### BUG #5: Goroutines Not Cleaned Up on Stop() ⚠️ HIGH
**File:** `ledcontroller.go:123-129`
**Severity:** HIGH
**Impact:** Resource Leak, Memory Leak

#### Description
The `Stop()` method closes `stopChan` but doesn't wait for goroutines to exit. Pattern goroutines may still be running and holding resources.

#### Evidence
```
Test: TestCleanupOnShutdown
Goroutines before stop: 12
Goroutines after stop: 15
BUG: Goroutines not cleaned up on shutdown
```

#### Recommended Fix
```go
func (c *Controller) Stop() {
	c.mu.Lock()
	if c.stopChan != nil {
		close(c.stopChan)
		c.stopChan = nil
	}
	c.mu.Unlock()

	// Wait for goroutines to exit
	time.Sleep(100 * time.Millisecond)

	// Turn off LEDs
	if c.actLED != nil && c.actLED.available {
		c.actLED.SetBrightness(0)
	}
}
```

---

### BUG #6: Race in MockStateManager (Test Infrastructure) ⚠️ MEDIUM
**File:** `ledcontroller_test.go:42, 70`
**Severity:** MEDIUM (Test code only)
**Impact:** Flaky Tests

#### Description
The mock state manager has races between `SetStatus()` sending to channels and `Unsubscribe()` closing them.

#### Evidence
```
WARNING: DATA RACE
Write at 0x00c0001420f0 by goroutine 1109:
  (*MockStateManager).Unsubscribe()
      ledcontroller_test.go:70

Previous read at 0x00c0001420f0 by goroutine 1111:
  (*MockStateManager).SetStatus()
      ledcontroller_test.go:42
```

#### Recommended Fix
Add synchronization to prevent closing channels while sending.

---

### BUG #7: Pattern Goroutines Don't Check for Closed Channel ⚠️ MEDIUM
**File:** `ledcontroller.go:179-201`
**Severity:** MEDIUM
**Impact:** Delayed Shutdown, Resource Leaks

#### Description
`runPattern()` checks `stopChan` in a `select` statement, but if the channel is closed, the goroutine will immediately receive the zero value and exit the select. However, the pattern sleep times mean goroutines can continue running for up to 500ms after stop.

```go
select {
case <-c.stopChan:
	return
default:
}

// Turn on
led.SetBrightness(255)
time.Sleep(pattern.OnDuration)  // ← Can be up to 500ms!

// Turn off
led.SetBrightness(0)
time.Sleep(pattern.OffDuration)
```

#### Recommended Fix
Check stopChan more frequently:

```go
func (c *Controller) runPattern(led *LED, pattern LEDPattern) {
	// ... existing code ...

	for {
		// Check before ON phase
		select {
		case <-c.stopChan:
			led.SetBrightness(0)
			return
		default:
		}

		led.SetBrightness(255)

		// Check during ON phase
		select {
		case <-c.stopChan:
			led.SetBrightness(0)
			return
		case <-time.After(pattern.OnDuration):
		}

		// Similar for OFF phase
	}
}
```

---

## Resource Leak Analysis

### Goroutine Leak Rate
- **Rapid state transitions:** 108 goroutines leaked in 2.5 seconds (43 leaks/second)
- **Concurrent changes:** 67 goroutines leaked in 0.5 seconds (134 leaks/second)
- **Stress test:** Peak of 156 goroutines (38x baseline), 25 remained after stop

### Memory Impact
Each leaked goroutine holds:
- Stack space: ~2-8 KB per goroutine
- Channel references
- LED object references
- Pattern state

**Estimated leak rate:** 100-400 KB/second under high state transition load

### Production Impact
On a Raspberry Pi 4 with limited resources:
- 1000 leaked goroutines = 2-8 MB of leaked memory
- At 40 leaks/second, reaches 1000 goroutines in 25 seconds
- LED controller becomes unusable within minutes of deployment
- **System may become unresponsive or crash**

---

## Test Coverage

The following test scenarios were implemented:

✅ **Test 1:** Rapid state transitions (50 iterations, 8 states) - **FAILED**
✅ **Test 2:** LED patterns not stopping when they should - **PASSED**
✅ **Test 3:** Multiple concurrent pattern changes (10 threads) - **FAILED**
✅ **Test 4:** Invalid LED paths or permissions - **PASSED**
✅ **Test 5:** State changes during active blink patterns - **PASSED**
✅ **Test 6:** Race conditions in goroutine management - **PASSED** (but race detector found issues)
✅ **Test 7:** Memory leaks from goroutines (10 iterations) - **FAILED**
✅ **Test 8:** LED state after errors or panics - **PASSED**
✅ **Test 9:** Cleanup on shutdown - **FAILED** (double close panic)
✅ **Test 10:** State manager subscription handling - **FAILED** (race in mock)
✅ **Test 11:** updatePattern stopChan race condition - **PASSED** (but documented issue)
✅ **Test 12:** runPattern with Repeat count edge cases - **PASSED**
✅ **Test 13:** Double close of stopChan - **FAILED** (panic)
✅ **Test 14:** stopChan close vs send - **FAILED** (race)
✅ **Test 15:** Stress test with monitoring (5 seconds, 244 changes) - **FAILED**

**Test Results:** 8/15 tests failed, all failures related to goroutine/channel management

---

## Recommended Actions

### Immediate (P0 - Critical)
1. **Add mutex synchronization** to `stopChan` access
2. **Fix goroutine leak** in `updatePattern()` - stop old goroutines before creating new ones
3. **Fix double close panic** in `Stop()` method
4. **Add goroutine tracking** for debugging

### Short Term (P1 - High)
5. **Implement graceful shutdown** - wait for goroutines to exit
6. **Add recovery handlers** to prevent panic crashes
7. **Improve channel signaling** - use context.Context instead of chan struct{}

### Long Term (P2 - Medium)
8. **Refactor LED controller** to use a single pattern goroutine that receives pattern changes via channel
9. **Add metrics/monitoring** for goroutine count
10. **Implement circuit breaker** to prevent resource exhaustion

---

## Alternative Architecture Proposal

Instead of creating new goroutines for each pattern change, use a single goroutine:

```go
type Controller struct {
	mu              sync.RWMutex
	actLED          *LED
	pwrLED          *LED
	patternChan     chan LEDPattern
	stopChan        chan struct{}
}

func (c *Controller) Start(stateMgr *state.Manager) error {
	c.patternChan = make(chan LEDPattern, 10)
	c.stopChan = make(chan struct{})

	stateUpdates := stateMgr.Subscribe()

	// Single goroutine for state monitoring
	go c.monitorState(stateUpdates)

	// Single goroutine for LED control
	go c.runLED()

	return nil
}

func (c *Controller) runLED() {
	for {
		select {
		case <-c.stopChan:
			return
		case pattern := <-c.patternChan:
			c.executePattern(pattern)
		}
	}
}

func (c *Controller) updatePattern(status state.SyncStatus) {
	pattern := c.getPatternForStatus(status)
	select {
	case c.patternChan <- pattern:
	default:
		// Pattern channel full, drop old pattern
	}
}
```

**Benefits:**
- No goroutine leaks (fixed number of goroutines)
- No channel replacement races
- Simpler synchronization
- Lower resource usage

---

## Test Artifacts

- **Test file:** `/workspace/pictures-sync-s3/pkg/ledcontroller/ledcontroller_test.go`
- **Full test output:** `/tmp/led_full_test_output.txt`
- **Test execution:** `go test -v -race ./pkg/ledcontroller/...`

---

## Conclusion

The LED controller has **critical bugs** that make it unsuitable for production use:

1. **Severe goroutine leaks** (100+ goroutines in normal operation)
2. **Multiple data races** that can cause crashes
3. **Double close panics** that crash the service
4. **No graceful shutdown** leading to resource leaks

**Recommendation:** **DO NOT DEPLOY** current implementation. Implement fixes for Bugs #1-#5 before production use.

**Estimated fix time:** 4-8 hours for critical bugs, 1-2 days for full refactoring to proposed architecture.
