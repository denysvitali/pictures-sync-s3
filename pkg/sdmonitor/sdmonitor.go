// Package sdmonitor detects and manages SD card insertion/removal events.
// It polls for block devices (/dev/sd*, /dev/mmcblk*), mounts cards read-only to prevent
// corruption, and manages unique card IDs stored on each card. Includes development mode
// for testing without physical hardware.
//
// Package organization:
//   - monitor.go: Core Monitor struct and polling logic
//   - mount.go: Mount/unmount operations
//   - cardid.go: Card ID management and generation
//   - devices.go: Device detection and sysfs helpers
//   - filesystem.go: DCIM detection and photo counting
package sdmonitor

// Event represents an SD card event
type Event struct {
	Type      EventType
	DevPath   string
	DevName   string
	MountPath string
}

// EventType represents the type of SD card event
type EventType int

const (
	EventInserted EventType = iota
	EventRemoved
)
