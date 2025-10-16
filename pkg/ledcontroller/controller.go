package ledcontroller

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/denysvitali/pictures-sync-s3/pkg/state"
)

const (
	sysfsLEDPath = "/sys/class/leds"
)

// LED represents a single LED device
type LED struct {
	name         string
	brightnessPath string
	available    bool
	mu           sync.Mutex // Protects LED operations
}

// LEDInterface allows for dependency injection and testing
type LEDInterface interface {
	SetBrightness(value int) error
	IsAvailable() bool
}

// Controller manages LED indicators and responds to system state changes
type Controller struct {
	actLED    *LED
	pwrLED    *LED
	stateMgr  *state.Manager

	// Goroutine management
	stopChan       chan struct{}
	patternStopMu  sync.Mutex
	patternStop    chan struct{}
	stateUpdates   chan state.CurrentState
	wg             sync.WaitGroup

	// State tracking
	currentPattern LEDPattern
	currentPatternMu sync.RWMutex
}

// NewController creates a new LED controller and discovers available LEDs
func NewController() (*Controller, error) {
	c := &Controller{
		stopChan: make(chan struct{}),
	}

	// Try to find ACT LED (green, primary status indicator)
	c.actLED = discoverLED("ACT", []string{
		filepath.Join(sysfsLEDPath, "ACT", "brightness"),
		filepath.Join(sysfsLEDPath, "led0", "brightness"),
		filepath.Join(sysfsLEDPath, "mmc0::", "brightness"),
	})

	// Try to find PWR LED (red, power/error indicator)
	c.pwrLED = discoverLED("PWR", []string{
		filepath.Join(sysfsLEDPath, "PWR", "brightness"),
		filepath.Join(sysfsLEDPath, "led1", "brightness"),
	})

	return c, nil
}

// discoverLED attempts to find an LED at one of the given paths
func discoverLED(name string, paths []string) *LED {
	for _, path := range paths {
		if _, err := os.Stat(path); err == nil {
			return &LED{
				name:         name,
				brightnessPath: path,
				available:    true,
			}
		}
	}
	return nil
}

// Start begins monitoring state changes and controlling LEDs
func (c *Controller) Start(stateMgr *state.Manager) error {
	if c.actLED == nil || !c.actLED.available {
		// No LEDs available, return early without error
		return nil
	}

	c.stateMgr = stateMgr
	c.stateUpdates = stateMgr.Subscribe()

	c.wg.Add(1)
	go c.monitorStateChanges()

	return nil
}

// monitorStateChanges watches for state updates and updates LED patterns
func (c *Controller) monitorStateChanges() {
	defer c.wg.Done()

	var currentStatus state.SyncStatus
	for {
		select {
		case <-c.stopChan:
			return
		case newState := <-c.stateUpdates:
			if newState.Status != currentStatus {
				currentStatus = newState.Status
				c.handleStatusChange(currentStatus)
			}
		}
	}
}

// handleStatusChange updates the LED pattern based on the new status
func (c *Controller) handleStatusChange(status state.SyncStatus) {
	// Stop any running pattern before starting a new one
	c.stopCurrentPattern()

	switch status {
	case state.StatusIdle:
		c.startPattern(c.actLED, PatternSteady)
	case state.StatusDetected:
		c.startPattern(c.actLED, PatternSlowBlink)
	case state.StatusSyncing:
		c.startPattern(c.actLED, PatternFastBlink)
	case state.StatusSuccess:
		c.startSuccessSequence()
	case state.StatusError:
		c.startErrorPattern()
	}
}

// startSuccessSequence plays a quick flash sequence followed by steady on
func (c *Controller) startSuccessSequence() {
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		c.runPatternOnce(c.actLED, PatternQuickFlash)

		// Check if we should continue after flash sequence
		select {
		case <-c.stopChan:
			return
		default:
			c.setLEDState(c.actLED, true)
		}
	}()
}

// startErrorPattern blinks the PWR LED if available, otherwise ACT LED
func (c *Controller) startErrorPattern() {
	if c.pwrLED != nil && c.pwrLED.available {
		c.startPattern(c.pwrLED, PatternSlowBlink)
	} else {
		c.startPattern(c.actLED, PatternSlowBlink)
	}
}

// startPattern starts a new LED pattern in a separate goroutine
func (c *Controller) startPattern(led *LED, pattern LEDPattern) {
	if led == nil || !led.available {
		return
	}

	// Create new stop channel for this pattern
	c.patternStopMu.Lock()
	c.patternStop = make(chan struct{})
	stopChan := c.patternStop
	c.patternStopMu.Unlock()

	c.currentPatternMu.Lock()
	c.currentPattern = pattern
	c.currentPatternMu.Unlock()

	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		c.runPattern(led, pattern, stopChan)
	}()
}

// stopCurrentPattern stops any currently running LED pattern
func (c *Controller) stopCurrentPattern() {
	c.patternStopMu.Lock()
	if c.patternStop != nil {
		close(c.patternStop)
		c.patternStop = nil
	}
	c.patternStopMu.Unlock()

	// Brief wait to allow pattern goroutine to clean up
	time.Sleep(10 * time.Millisecond)
}

// runPattern executes a LED pattern until stopped
func (c *Controller) runPattern(led *LED, pattern LEDPattern, stopChan chan struct{}) {
	// Handle steady-on pattern (no blinking)
	if pattern.OnDuration == 0 && pattern.OffDuration == 0 {
		c.setLEDState(led, true)
		<-stopChan // Wait for stop signal
		return
	}

	count := 0
	for {
		// Check if we should stop
		select {
		case <-stopChan:
			c.setLEDState(led, false)
			return
		case <-c.stopChan:
			c.setLEDState(led, false)
			return
		default:
		}

		// Check repeat limit
		if pattern.Repeat > 0 && count >= pattern.Repeat {
			c.setLEDState(led, false)
			return
		}

		// LED on phase
		c.setLEDState(led, true)
		if !c.sleepInterruptible(pattern.OnDuration, stopChan) {
			c.setLEDState(led, false)
			return
		}

		// LED off phase
		c.setLEDState(led, false)
		if !c.sleepInterruptible(pattern.OffDuration, stopChan) {
			return
		}

		count++
	}
}

// runPatternOnce executes a pattern exactly once without a stop channel
func (c *Controller) runPatternOnce(led *LED, pattern LEDPattern) {
	if led == nil || !led.available {
		return
	}

	for i := 0; i < pattern.Repeat; i++ {
		select {
		case <-c.stopChan:
			return
		default:
		}

		c.setLEDState(led, true)
		time.Sleep(pattern.OnDuration)
		c.setLEDState(led, false)
		time.Sleep(pattern.OffDuration)
	}
}

// sleepInterruptible sleeps for the given duration but can be interrupted
// Returns true if sleep completed normally, false if interrupted
func (c *Controller) sleepInterruptible(duration time.Duration, stopChan chan struct{}) bool {
	select {
	case <-time.After(duration):
		return true
	case <-stopChan:
		return false
	case <-c.stopChan:
		return false
	}
}

// setLEDState is a helper to turn an LED on or off
func (c *Controller) setLEDState(led *LED, on bool) {
	if led == nil || !led.available {
		return
	}

	brightness := 0
	if on {
		brightness = 255
	}

	led.SetBrightness(brightness)
}

// Stop gracefully shuts down the LED controller
func (c *Controller) Stop() {
	// Close main stop channel (safe to close multiple times with select guard)
	select {
	case <-c.stopChan:
		// Already closed
		return
	default:
		close(c.stopChan)
	}

	// Stop current pattern
	c.stopCurrentPattern()

	// Unsubscribe from state updates to prevent memory leak
	if c.stateMgr != nil && c.stateUpdates != nil {
		c.stateMgr.Unsubscribe(c.stateUpdates)
	}

	// Wait for all goroutines to finish
	c.wg.Wait()

	// Turn off all LEDs
	c.turnOffAllLEDs()
}

// turnOffAllLEDs turns off all available LEDs
func (c *Controller) turnOffAllLEDs() {
	if c.actLED != nil && c.actLED.available {
		c.setLEDState(c.actLED, false)
	}
	if c.pwrLED != nil && c.pwrLED.available {
		c.setLEDState(c.pwrLED, false)
	}
}

// SetBrightness sets the LED brightness (0-255)
// This method is thread-safe
func (l *LED) SetBrightness(value int) error {
	if !l.available {
		return nil
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	data := []byte(fmt.Sprintf("%d", value))
	return os.WriteFile(l.brightnessPath, data, 0644)
}

// IsAvailable returns whether the LED is available for use
func (l *LED) IsAvailable() bool {
	return l.available
}
