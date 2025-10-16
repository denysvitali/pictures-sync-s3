package ledcontroller

import "time"

// LEDPattern defines a blinking pattern
type LEDPattern struct {
	OnDuration  time.Duration
	OffDuration time.Duration
	Repeat      int // 0 means infinite
}

// Predefined LED patterns for different system states
var (
	// PatternSteady keeps the LED constantly on
	PatternSteady = LEDPattern{
		OnDuration:  0,
		OffDuration: 0,
		Repeat:      0,
	}

	// PatternSlowBlink provides a slow, steady blink (500ms on/off)
	// Used for: Idle state, SD card detected
	PatternSlowBlink = LEDPattern{
		OnDuration:  500 * time.Millisecond,
		OffDuration: 500 * time.Millisecond,
		Repeat:      0,
	}

	// PatternFastBlink provides a fast blink (125ms on/off)
	// Used for: Active syncing operations
	PatternFastBlink = LEDPattern{
		OnDuration:  125 * time.Millisecond,
		OffDuration: 125 * time.Millisecond,
		Repeat:      0,
	}

	// PatternQuickFlash provides a brief flash sequence (3 times)
	// Used for: Success indication
	PatternQuickFlash = LEDPattern{
		OnDuration:  100 * time.Millisecond,
		OffDuration: 100 * time.Millisecond,
		Repeat:      3,
	}

	// PatternOff turns the LED off
	PatternOff = LEDPattern{
		OnDuration:  0,
		OffDuration: 0,
		Repeat:      1,
	}
)
