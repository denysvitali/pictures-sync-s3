package ledcontroller

import "time"

// LEDPattern defines a blinking pattern.
//
// A pattern is built from "groups" of pulses. Each group emits BurstCount
// on/off pairs (each pulse using OnDuration and OffDuration), then waits for
// BurstPause before the next group. Repeat controls how many groups to emit
// (0 means run forever). When BurstCount is 0 or 1 and BurstPause is 0 the
// pattern behaves as a single on/off cycle, preserving the original
// single-pulse semantics.
type LEDPattern struct {
	OnDuration  time.Duration
	OffDuration time.Duration
	Repeat      int // 0 means infinite groups

	// BurstCount is the number of on/off pulses per group. 0 is treated as 1.
	BurstCount int
	// BurstPause is the additional pause after a group of pulses.
	BurstPause time.Duration
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

	// PatternError is two quick blinks followed by a long pause, repeating.
	// Used for: Mount failure, sync failure, and any other error state where
	// the operator needs a visually distinct LED cue (vs the steady/slow/fast
	// blinks used during normal operation).
	PatternError = LEDPattern{
		OnDuration:  120 * time.Millisecond,
		OffDuration: 180 * time.Millisecond,
		Repeat:      0,
		BurstCount:  2,
		BurstPause:  1 * time.Second,
	}

	// PatternOff turns the LED off
	PatternOff = LEDPattern{
		OnDuration:  0,
		OffDuration: 0,
		Repeat:      1,
	}
)
