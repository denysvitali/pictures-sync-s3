package ledcontroller

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/denysvitali/pictures-sync-s3/pkg/state"
)

const (
	sysfsLEDPath = "/sys/class/leds"
)

// LEDPattern defines a blinking pattern
type LEDPattern struct {
	OnDuration  time.Duration
	OffDuration time.Duration
	Repeat      int // 0 means infinite
}

var (
	PatternSteady     = LEDPattern{OnDuration: 0, OffDuration: 0, Repeat: 0}     // Always on
	PatternSlowBlink  = LEDPattern{OnDuration: 500 * time.Millisecond, OffDuration: 500 * time.Millisecond, Repeat: 0}
	PatternFastBlink  = LEDPattern{OnDuration: 125 * time.Millisecond, OffDuration: 125 * time.Millisecond, Repeat: 0}
	PatternQuickFlash = LEDPattern{OnDuration: 100 * time.Millisecond, OffDuration: 100 * time.Millisecond, Repeat: 3}
)

// Controller manages LED indicators
type Controller struct {
	actLED      *LED
	pwrLED      *LED
	currentPattern LEDPattern
	stopChan    chan struct{}
}

// LED represents a single LED
type LED struct {
	name         string
	brightnessPath string
	available    bool
}

// NewController creates a new LED controller
func NewController() (*Controller, error) {
	c := &Controller{
		stopChan: make(chan struct{}),
	}

	// Try to find ACT LED (green, primary status indicator)
	actPaths := []string{
		filepath.Join(sysfsLEDPath, "ACT", "brightness"),
		filepath.Join(sysfsLEDPath, "led0", "brightness"),
		filepath.Join(sysfsLEDPath, "mmc0::", "brightness"),
	}

	for _, path := range actPaths {
		if _, err := os.Stat(path); err == nil {
			c.actLED = &LED{
				name:         "ACT",
				brightnessPath: path,
				available:    true,
			}
			break
		}
	}

	// Try to find PWR LED (red, power/error indicator)
	pwrPaths := []string{
		filepath.Join(sysfsLEDPath, "PWR", "brightness"),
		filepath.Join(sysfsLEDPath, "led1", "brightness"),
	}

	for _, path := range pwrPaths {
		if _, err := os.Stat(path); err == nil {
			c.pwrLED = &LED{
				name:         "PWR",
				brightnessPath: path,
				available:    true,
			}
			break
		}
	}

	if c.actLED == nil {
		// No LEDs available, but don't fail - just won't show status
		return c, nil
	}

	return c, nil
}

// Start begins monitoring state and controlling LEDs
func (c *Controller) Start(stateMgr *state.Manager) error {
	if c.actLED == nil || !c.actLED.available {
		// No LEDs available, return early
		return nil
	}

	// Subscribe to state changes
	stateUpdates := stateMgr.Subscribe()

	go func() {
		var currentStatus state.SyncStatus
		for {
			select {
			case <-c.stopChan:
				return
			case state := <-stateUpdates:
				if state.Status != currentStatus {
					currentStatus = state.Status
					c.updatePattern(currentStatus)
				}
			}
		}
	}()

	return nil
}

// Stop stops the LED controller
func (c *Controller) Stop() {
	close(c.stopChan)
	// Turn off LEDs
	if c.actLED != nil && c.actLED.available {
		c.actLED.SetBrightness(0)
	}
}

// updatePattern updates the LED pattern based on status
func (c *Controller) updatePattern(status state.SyncStatus) {
	// Stop current pattern by sending signal on existing channel
	if c.stopChan != nil {
		select {
		case c.stopChan <- struct{}{}:
		default:
		}
		// Wait a bit for goroutine to stop
		time.Sleep(10 * time.Millisecond)
	}

	// Create new stop channel for the new pattern
	c.stopChan = make(chan struct{})

	switch status {
	case state.StatusIdle:
		go c.runPattern(c.actLED, PatternSteady)
	case state.StatusDetected:
		go c.runPattern(c.actLED, PatternSlowBlink)
	case state.StatusSyncing:
		go c.runPattern(c.actLED, PatternFastBlink)
	case state.StatusSuccess:
		// Quick flash 3 times, then steady
		go func() {
			c.runPattern(c.actLED, PatternQuickFlash)
			c.runPattern(c.actLED, PatternSteady)
		}()
	case state.StatusError:
		// Blink PWR LED if available, otherwise ACT LED
		if c.pwrLED != nil && c.pwrLED.available {
			go c.runPattern(c.pwrLED, PatternSlowBlink)
		} else {
			go c.runPattern(c.actLED, PatternSlowBlink)
		}
	}
}

// runPattern executes a LED pattern
func (c *Controller) runPattern(led *LED, pattern LEDPattern) {
	if led == nil || !led.available {
		return
	}

	// Steady on pattern
	if pattern.OnDuration == 0 && pattern.OffDuration == 0 {
		led.SetBrightness(255)
		return
	}

	count := 0
	for {
		select {
		case <-c.stopChan:
			return
		default:
		}

		// Check repeat count
		if pattern.Repeat > 0 && count >= pattern.Repeat {
			led.SetBrightness(0)
			return
		}

		// Turn on
		led.SetBrightness(255)
		time.Sleep(pattern.OnDuration)

		// Turn off
		led.SetBrightness(0)
		time.Sleep(pattern.OffDuration)

		count++
	}
}

// SetBrightness sets the LED brightness (0-255)
func (l *LED) SetBrightness(value int) error {
	if !l.available {
		return nil
	}

	data := []byte(fmt.Sprintf("%d", value))
	return os.WriteFile(l.brightnessPath, data, 0644)
}
