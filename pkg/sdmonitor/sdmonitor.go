package sdmonitor

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Event represents an SD card event
type Event struct {
	Type     EventType
	DevPath  string
	DevName  string
	MountPath string
}

// EventType represents the type of SD card event
type EventType int

const (
	EventInserted EventType = iota
	EventRemoved
)

const (
	// CardIDFile is the name of the file storing the card's unique ID
	CardIDFile = ".pictures-sync-id"
)

// Monitor monitors for SD card insertion/removal
type Monitor struct {
	eventChan  chan Event
	stopChan   chan struct{}
	mountPath  string
	lastDevice string
}

// NewMonitor creates a new SD card monitor
func NewMonitor(mountPath string) *Monitor {
	return &Monitor{
		eventChan: make(chan Event, 10),
		stopChan:  make(chan struct{}),
		mountPath: mountPath,
	}
}

// Start begins monitoring for SD card events
func (m *Monitor) Start() error {
	// Ensure mount directory exists
	if err := os.MkdirAll(m.mountPath, 0755); err != nil {
		return fmt.Errorf("failed to create mount directory: %w", err)
	}

	go m.pollDevices()
	return nil
}

// Stop stops the monitor
func (m *Monitor) Stop() {
	close(m.stopChan)
	// Unmount if mounted
	if m.lastDevice != "" {
		m.unmount()
	}
}

// Events returns the event channel
func (m *Monitor) Events() <-chan Event {
	return m.eventChan
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
			m.checkDevices()
		}
	}
}

// checkDevices checks for new or removed devices
func (m *Monitor) checkDevices() {
	// Look for USB storage devices
	device := m.findUSBStorageDevice()

	if device != "" && device != m.lastDevice {
		// New device detected
		log.Printf("SD card detected: %s", device)

		// Try to mount it
		if err := m.mount(device); err != nil {
			log.Printf("Failed to mount device %s: %v", device, err)
			return
		}

		m.lastDevice = device
		m.eventChan <- Event{
			Type:      EventInserted,
			DevPath:   device,
			DevName:   filepath.Base(device),
			MountPath: m.mountPath,
		}
	} else if device == "" && m.lastDevice != "" {
		// Device removed
		log.Printf("SD card removed: %s", m.lastDevice)

		m.unmount()
		oldDevice := m.lastDevice
		m.lastDevice = ""

		m.eventChan <- Event{
			Type:    EventRemoved,
			DevPath: oldDevice,
			DevName: filepath.Base(oldDevice),
		}
	}
}

// findUSBStorageDevice finds USB storage devices
func (m *Monitor) findUSBStorageDevice() string {
	// Check /dev/sd* devices
	matches, err := filepath.Glob("/dev/sd[a-z]1")
	if err != nil {
		return ""
	}

	// Filter for USB devices by checking if they're currently mounted
	// or by checking /sys/block/*/device/
	for _, dev := range matches {
		baseName := filepath.Base(dev)
		diskName := strings.TrimRight(baseName, "0123456789")

		// Check if this is a USB device
		sysPath := filepath.Join("/sys/block", diskName, "device")
		if _, err := os.Stat(sysPath); err == nil {
			// Check if it's USB by looking for uevent
			ueventPath := filepath.Join(sysPath, "uevent")
			if data, err := os.ReadFile(ueventPath); err == nil {
				if strings.Contains(string(data), "usb") || strings.Contains(string(data), "USB") {
					// Check if already mounted elsewhere
					if !m.isMountedElsewhere(dev) {
						return dev
					}
				}
			}
		}
	}

	// Also check /dev/mmcblk* for SD card readers
	matches, err = filepath.Glob("/dev/mmcblk[0-9]p1")
	if err == nil {
		for _, dev := range matches {
			if !m.isMountedElsewhere(dev) {
				return dev
			}
		}
	}

	return ""
}

// isMountedElsewhere checks if device is already mounted somewhere else
func (m *Monitor) isMountedElsewhere(device string) bool {
	data, err := os.ReadFile("/proc/mounts")
	if err != nil {
		return false
	}

	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, device) {
			// Check if mounted to our mount path
			fields := strings.Fields(line)
			if len(fields) >= 2 && fields[1] != m.mountPath {
				return true
			}
		}
	}

	return false
}

// mount mounts the device
func (m *Monitor) mount(device string) error {
	// Check if already mounted
	data, err := os.ReadFile("/proc/mounts")
	if err == nil {
		if strings.Contains(string(data), device+" "+m.mountPath) {
			return nil // Already mounted
		}
	}

	// Unmount if anything is currently mounted at our path
	exec.Command("umount", m.mountPath).Run()

	// Try to mount with various filesystem types
	fstypes := []string{"vfat", "exfat", "ext4", "ntfs"}

	for _, fstype := range fstypes {
		cmd := exec.Command("mount", "-t", fstype, "-o", "ro", device, m.mountPath)
		if err := cmd.Run(); err == nil {
			log.Printf("Mounted %s as %s at %s", device, fstype, m.mountPath)
			return nil
		}
	}

	// Try auto-detect
	cmd := exec.Command("mount", "-o", "ro", device, m.mountPath)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to mount device: %w", err)
	}

	log.Printf("Mounted %s at %s", device, m.mountPath)
	return nil
}

// unmount unmounts the current device
func (m *Monitor) unmount() error {
	cmd := exec.Command("umount", m.mountPath)
	if err := cmd.Run(); err != nil {
		log.Printf("Failed to unmount %s: %v", m.mountPath, err)
		return err
	}
	log.Printf("Unmounted %s", m.mountPath)
	return nil
}

// HasDCIM checks if the mounted SD card has a DCIM directory
func HasDCIM(mountPath string) bool {
	dcimPath := filepath.Join(mountPath, "DCIM")
	info, err := os.Stat(dcimPath)
	if err != nil {
		return false
	}
	return info.IsDir()
}

// CountPhotos counts photo files in DCIM directory
func CountPhotos(mountPath string) (int, int64, error) {
	dcimPath := filepath.Join(mountPath, "DCIM")

	var count int
	var totalSize int64

	err := filepath.WalkDir(dcimPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}

		// Check if it's an image or video file
		ext := strings.ToLower(filepath.Ext(path))
		switch ext {
		case ".jpg", ".jpeg", ".png", ".gif", ".raw", ".cr2", ".nef", ".arw",
			".mp4", ".mov", ".avi", ".mkv":
			count++
			info, err := d.Info()
			if err == nil {
				totalSize += info.Size()
			}
		}
		return nil
	})

	return count, totalSize, err
}

// GetVolumeID attempts to get a unique ID for the SD card
func GetVolumeID(device string) string {
	// Try to read volume ID from blkid
	cmd := exec.Command("blkid", "-s", "UUID", "-o", "value", device)
	output, err := cmd.Output()
	if err == nil && len(output) > 0 {
		return strings.TrimSpace(string(output))
	}

	// Fallback to device name
	return filepath.Base(device)
}

// GetOrCreateCardID reads or creates a unique ID for the SD card
// Returns: (cardID, isNewCard, error)
func GetOrCreateCardID(mountPath string) (string, bool, error) {
	idPath := filepath.Join(mountPath, CardIDFile)

	// Try to read existing ID
	if data, err := os.ReadFile(idPath); err == nil {
		cardID := strings.TrimSpace(string(data))
		if cardID != "" {
			log.Printf("Found existing card ID: %s", cardID)
			return cardID, false, nil
		}
	}

	// Generate new ID
	newID := generateCardID()
	log.Printf("Generated new card ID: %s", newID)

	// Try to write to card
	// Note: This will fail if card is mounted read-only, which is OK
	if err := os.WriteFile(idPath, []byte(newID+"\n"), 0644); err != nil {
		log.Printf("Warning: Could not write card ID to %s: %v (card may be read-only)", idPath, err)
		// Continue anyway - we'll use this ID for this session
	}

	return newID, true, nil
}

// generateCardID generates a unique card ID
func generateCardID() string {
	// Generate 8 random bytes
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		// Fallback to timestamp-based ID if crypto/rand fails
		return fmt.Sprintf("card-%d", time.Now().Unix())
	}
	return fmt.Sprintf("card-%s", hex.EncodeToString(b))
}

// CreateNewCardID forces creation of a new card ID (for reformatted cards)
func CreateNewCardID(mountPath string) (string, error) {
	newID := generateCardID()
	idPath := filepath.Join(mountPath, CardIDFile)

	log.Printf("Creating new card ID: %s", newID)

	// Try to write to card
	if err := os.WriteFile(idPath, []byte(newID+"\n"), 0644); err != nil {
		log.Printf("Warning: Could not write card ID to %s: %v", idPath, err)
		// Continue anyway
	}

	return newID, nil
}
