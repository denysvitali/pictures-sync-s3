package sdmonitor

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	// devModeCardCreationDelay is the wait time before creating a mock SD card in development mode.
	// This simulates the delay of physical card insertion for testing purposes.
	devModeCardCreationDelay = 3 * time.Second

	// eventSendTimeout bounds how long a poll-loop event delivery may wait
	// when the event channel is full. Beyond this we drop the event rather
	// than block the poller (and, transitively, Stop()).
	eventSendTimeout = 2 * time.Second

	// eventDebounceWindow suppresses duplicate notifications caused by noisy
	// device settle/flap behavior while still allowing insert->remove changes
	// to pass through immediately.
	eventDebounceWindow = 750 * time.Millisecond
)

// Monitor monitors for SD card insertion/removal
type Monitor struct {
	eventChan     chan Event
	stopChan      chan struct{}
	mountPath     string
	lastDevice    string
	ignoredDevice string       // Device to ignore until removal after a manual format attempt.
	devMode       bool         // Development mode for testing without hardware
	stopped       bool         // Tracks if monitor has been stopped
	mu            sync.RWMutex // Protects lastDevice and stopped
	mountMu       sync.Mutex   // Protects mount/unmount operations (prevents concurrent mounts)
	eventMu       sync.Mutex
	lastEvent     Event
	lastEventTime time.Time

	// Cache for /proc/mounts to reduce I/O
	mountsCacheMu   sync.Mutex // Protects cachedMounts and mountsCacheTime
	cachedMounts    string
	mountsCacheTime time.Time
	mountsCacheTTL  time.Duration
}

// NewMonitor creates a new SD card monitor
func NewMonitor(mountPath string) *Monitor {
	// Check if we're in development mode (no hardware devices available)
	// On Gokrazy (real hardware), /perm always exists. In local dev, it won't.
	devMode := false
	if _, err := os.Stat("/perm"); os.IsNotExist(err) {
		// /perm doesn't exist, we're in local development
		devMode = true
		log.Println("SD Monitor: /perm not found, enabling development mode")
	}

	// Allow explicit override via environment variable
	if os.Getenv("SDMONITOR_DEV_MODE") == "1" {
		devMode = true
		log.Println("SD Monitor: Development mode forced via SDMONITOR_DEV_MODE=1")
	} else if os.Getenv("SDMONITOR_DEV_MODE") == "0" {
		devMode = false
		log.Println("SD Monitor: Hardware mode forced via SDMONITOR_DEV_MODE=0")
	}

	return &Monitor{
		eventChan:      make(chan Event, 10),
		stopChan:       make(chan struct{}),
		mountPath:      mountPath,
		devMode:        devMode,
		mountsCacheTTL: 2 * time.Second, // Cache for same duration as polling interval
	}
}

// Start begins monitoring for SD card events
func (m *Monitor) Start() error {
	// Ensure mount directory exists
	// #nosec G301 -- SD card mount point must be accessible by all processes on embedded device
	if err := os.MkdirAll(m.mountPath, 0755); err != nil {
		return err
	}

	if m.devMode {
		log.Println("SD Monitor: Running in development mode (no hardware detection)")
		// In dev mode, create a mock DCIM structure for testing
		go m.createDevModeCard()
	} else {
		log.Println("SD Monitor: Running in hardware mode")
		// Check for already-inserted cards immediately on startup
		m.checkDevices()
		go m.pollDevices()
	}

	return nil
}

// Stop stops the monitor
func (m *Monitor) Stop() {
	m.mu.Lock()
	if m.stopped {
		m.mu.Unlock()
		return // Already stopped
	}
	m.stopped = true
	m.mu.Unlock()

	close(m.stopChan)

	// Unmount if mounted (use separate mutex for mount operations)
	m.mu.RLock()
	hasDevice := m.lastDevice != ""
	m.mu.RUnlock()

	if hasDevice {
		m.mountMu.Lock()
		if err := m.unmount(); err != nil {
			log.Printf("Stop: Warning: Failed to unmount during shutdown: %v", err)
		}
		m.mountMu.Unlock()
	}
}

// Events returns the event channel
func (m *Monitor) Events() <-chan Event {
	return m.eventChan
}

// IsCardMounted returns true if an SD card is currently mounted
func (m *Monitor) IsCardMounted() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.lastDevice != ""
}

// GetMountPath returns the current mount path
func (m *Monitor) GetMountPath() string {
	return m.mountPath
}

// GetCurrentDevice returns the currently mounted device
func (m *Monitor) GetCurrentDevice() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.lastDevice
}

// RedetectCurrentDevice clears any post-format ignore marker and checks for a card immediately.
func (m *Monitor) RedetectCurrentDevice() error {
	m.mu.Lock()
	m.ignoredDevice = ""
	m.mu.Unlock()

	m.mountsCacheMu.Lock()
	m.mountsCacheTime = time.Time{}
	m.mountsCacheMu.Unlock()

	m.checkDevices()

	if !m.IsCardMounted() {
		return fmt.Errorf("no SD card detected")
	}
	return nil
}

// pollDevices polls for USB storage devices
func (m *Monitor) pollDevices() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-m.stopChan:
			return
		case <-ticker.C:
			// Invalidate cache at each poll to get fresh data
			m.mountsCacheMu.Lock()
			m.mountsCacheTime = time.Time{}
			m.mountsCacheMu.Unlock()
			m.checkDevices()
		}
	}
}

// checkDevices checks for new or removed devices
func (m *Monitor) checkDevices() {
	if m.devMode {
		return // Skip hardware checking in dev mode
	}

	// Look for USB storage devices
	device := findStorageDevice(m.mountPath)

	m.mu.RLock()
	currentDevice := m.lastDevice
	ignoredDevice := m.ignoredDevice
	m.mu.RUnlock()

	if device == "" && ignoredDevice != "" {
		m.mu.Lock()
		m.ignoredDevice = ""
		m.mu.Unlock()
		ignoredDevice = ""
	}
	if device != "" && currentDevice == "" && device == ignoredDevice {
		return
	}
	if device != "" && ignoredDevice != "" && device != ignoredDevice {
		m.mu.Lock()
		m.ignoredDevice = ""
		m.mu.Unlock()
	}

	if device != "" && device != currentDevice {
		// New device detected
		log.Printf("SD card detected: %s (lastDevice was: %s)", device, currentDevice)

		// Acquire mount mutex to prevent concurrent mount operations.
		// IMPORTANT: do NOT hold mountMu across the eventChan send below.
		// Stop() also acquires mountMu, so if eventChan is full and the
		// consumer has stalled, holding mountMu here would deadlock Stop().
		m.mountMu.Lock()
		err := m.mount(device)
		if err != nil {
			m.mountMu.Unlock()
			log.Printf("Mount ERROR: Failed to mount device %s: %v", device, err)
			// DO NOT update lastDevice on mount failure - this is the critical bug fix
			// The device is NOT mounted, so we must not mark it as such
			log.Printf("Mount ERROR: Device %s will NOT be marked as mounted due to mount failure", device)
			return
		}

		// Mount succeeded - now it's safe to update state
		m.mu.Lock()
		m.lastDevice = device
		m.mu.Unlock()
		m.mountMu.Unlock()

		log.Printf("SD card inserted: %s, mounted at %s", filepath.Base(device), m.mountPath)
		m.sendEvent(Event{
			Type:      EventInserted,
			DevPath:   device,
			DevName:   filepath.Base(device),
			MountPath: m.mountPath,
		})
	} else if device == "" && currentDevice != "" {
		// Device removed
		log.Printf("SD card removed: %s", currentDevice)

		// Acquire mount mutex to prevent concurrent unmount operations
		m.mountMu.Lock()
		unmountErr := m.unmount()
		m.mountMu.Unlock()

		if unmountErr != nil {
			log.Printf("Unmount ERROR: Failed to unmount %s: %v (device will still be marked as removed)", currentDevice, unmountErr)
			// Continue anyway - the physical device is gone, so we should clear our state
		}

		m.mu.Lock()
		oldDevice := m.lastDevice
		m.lastDevice = ""
		m.mu.Unlock()

		m.sendEvent(Event{
			Type:    EventRemoved,
			DevPath: oldDevice,
			DevName: filepath.Base(oldDevice),
		})
	}
}

// sendEvent delivers an event to consumers without ever blocking the caller.
// We must not block here because checkDevices runs from the poll loop and
// Stop()/RedetectCurrentDevice() can be called concurrently with it. Blocking
// on a full buffered channel previously made the poller deadlock against
// Stop() once a downstream consumer stalled.
//
// If the buffer is full (slow/stalled consumer) we log and drop the event
// rather than wedge the monitor. Stop() also unblocks pending sends by
// closing stopChan and stops being a deadlock victim.
func (m *Monitor) sendEvent(ev Event) {
	if m.shouldDebounceEvent(ev) {
		log.Printf("sendEvent: debounced duplicate %v event for %s", ev.Type, ev.DevName)
		return
	}

	// Fast path: deliver immediately if buffer has room and we are not stopping.
	select {
	case <-m.stopChan:
		log.Printf("sendEvent: monitor stopped, dropping %v event for %s", ev.Type, ev.DevName)
		return
	case m.eventChan <- ev:
		return
	default:
	}

	// Slow path: wait briefly, but never block forever. Always remain
	// responsive to Stop().
	timer := time.NewTimer(eventSendTimeout)
	defer timer.Stop()

	select {
	case m.eventChan <- ev:
	case <-m.stopChan:
		log.Printf("sendEvent: monitor stopped while delivering %v event for %s", ev.Type, ev.DevName)
	case <-timer.C:
		log.Printf("sendEvent: WARNING - event channel full, dropping %v event for %s", ev.Type, ev.DevName)
	}
}

func (m *Monitor) shouldDebounceEvent(ev Event) bool {
	m.eventMu.Lock()
	defer m.eventMu.Unlock()

	now := time.Now()
	duplicate := m.lastEvent.Type == ev.Type &&
		m.lastEvent.DevPath == ev.DevPath &&
		m.lastEvent.MountPath == ev.MountPath &&
		now.Sub(m.lastEventTime) < eventDebounceWindow

	m.lastEvent = ev
	m.lastEventTime = now
	return duplicate
}

// getCachedMounts returns cached /proc/mounts content or refreshes if expired
func (m *Monitor) getCachedMounts() (string, error) {
	m.mountsCacheMu.Lock()
	defer m.mountsCacheMu.Unlock()

	// Check if cache is still valid
	if time.Since(m.mountsCacheTime) < m.mountsCacheTTL {
		return m.cachedMounts, nil
	}

	// Refresh cache
	data, err := os.ReadFile("/proc/mounts")
	if err != nil {
		return "", err
	}

	m.cachedMounts = string(data)
	m.mountsCacheTime = time.Now()
	return m.cachedMounts, nil
}

// isMountedElsewhere checks if device is already mounted somewhere else
func (m *Monitor) isMountedElsewhere(device string) bool {
	mounts, err := m.getCachedMounts()
	if err != nil {
		return false
	}

	// Use strings.Index for faster search instead of splitting all lines
	devicePrefix := device + " "
	idx := strings.Index(mounts, devicePrefix)
	if idx == -1 {
		return false
	}

	// Find the end of the line containing the device
	lineStart := strings.LastIndex(mounts[:idx], "\n") + 1
	lineEnd := strings.Index(mounts[idx:], "\n")
	if lineEnd == -1 {
		lineEnd = len(mounts)
	} else {
		lineEnd += idx
	}

	line := mounts[lineStart:lineEnd]
	fields := strings.Fields(line)
	if len(fields) >= 2 && fields[1] != m.mountPath {
		return true
	}

	return false
}

// createDevModeCard creates a mock SD card for development mode
func (m *Monitor) createDevModeCard() {
	// Wait a few seconds to simulate card insertion
	time.Sleep(devModeCardCreationDelay)

	log.Println("SD Monitor: Simulating SD card insertion in dev mode")

	// Create DCIM structure with some mock photos
	dcimPath := filepath.Join(m.mountPath, "DCIM", "100CANON")
	// #nosec G301 -- Mock SD card DCIM structure must be accessible by web UI
	if err := os.MkdirAll(dcimPath, 0755); err != nil {
		log.Printf("Failed to create mock DCIM directory: %v", err)
		return
	}

	// Create some mock photo files
	mockPhotos := []string{"IMG_001.JPG", "IMG_002.JPG", "IMG_003.JPG"}
	for _, photo := range mockPhotos {
		photoPath := filepath.Join(dcimPath, photo)
		// Create a small mock JPEG file (just placeholder content)
		content := []byte("Mock JPEG file content for " + photo)
		// #nosec G306 -- Mock photo files on SD card must be readable by web UI process
		if err := os.WriteFile(photoPath, content, 0644); err != nil {
			log.Printf("Failed to create mock photo %s: %v", photo, err)
		}
	}

	// Mark as mounted
	m.mu.Lock()
	m.lastDevice = "mock-sdcard"
	m.mu.Unlock()

	// Send insertion event
	event := Event{
		Type:      EventInserted,
		DevPath:   "mock-sdcard",
		DevName:   "mock-sdcard",
		MountPath: m.mountPath,
	}
	log.Printf("SD Monitor: Sending insertion event to channel: %+v", event)
	m.sendEvent(event)

	log.Printf("SD Monitor: Mock SD card created with %d photos at %s", len(mockPhotos), m.mountPath)
}
