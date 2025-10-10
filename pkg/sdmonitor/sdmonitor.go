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

	"golang.org/x/sys/unix"
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

	// Check for already-inserted cards immediately on startup
	m.checkDevices()

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
	// Check /dev/sd* devices (USB card readers on Raspberry Pi)
	// On a dedicated appliance, /dev/sda* is typically the USB SD reader
	matches, err := filepath.Glob("/dev/sd[a-z]1")
	if err != nil {
		log.Printf("Error globbing /dev/sd*: %v", err)
		return ""
	}

	// Return first available /dev/sd* device that isn't mounted elsewhere
	for _, dev := range matches {
		if !m.isMountedElsewhere(dev) {
			return dev
		}
	}

	// Also check /dev/mmcblk* for SD card readers (but exclude mmcblk0 which is the Pi's boot SD)
	matches, err = filepath.Glob("/dev/mmcblk[1-9]p1")
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

// mount mounts the device (initially read-write for card ID setup)
func (m *Monitor) mount(device string) error {
	// Check if already mounted
	data, err := os.ReadFile("/proc/mounts")
	if err == nil {
		if strings.Contains(string(data), device+" "+m.mountPath) {
			return nil // Already mounted
		}
	}

	// Unmount if anything is currently mounted at our path
	unix.Unmount(m.mountPath, 0)

	// Mount read-write initially to allow writing card ID
	// We'll remount read-only after card ID is written
	fstypes := []string{"vfat", "exfat", "ext4", "ntfs"}

	for _, fstype := range fstypes {
		err := unix.Mount(device, m.mountPath, fstype, 0, "")
		if err == nil {
			log.Printf("Mounted %s as %s (rw) at %s", device, fstype, m.mountPath)
			return nil
		}
	}

	// Try auto-detect (empty fstype)
	err = unix.Mount(device, m.mountPath, "", 0, "")
	if err != nil {
		return fmt.Errorf("failed to mount device: %w", err)
	}

	log.Printf("Mounted %s (rw) at %s", device, m.mountPath)
	return nil
}

// RemountReadOnly remounts the filesystem as read-only
func (m *Monitor) RemountReadOnly() error {
	// Remount with MS_REMOUNT | MS_RDONLY flags
	err := unix.Mount("", m.mountPath, "", unix.MS_REMOUNT|unix.MS_RDONLY, "")
	if err != nil {
		return fmt.Errorf("failed to remount read-only: %w", err)
	}
	log.Printf("Remounted %s as read-only", m.mountPath)
	return nil
}

// unmount unmounts the current device
func (m *Monitor) unmount() error {
	if err := unix.Unmount(m.mountPath, 0); err != nil {
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
// The monitor parameter is optional - if provided, it will remount read-only after writing
func GetOrCreateCardID(mountPath string, monitor *Monitor) (string, bool, error) {
	idPath := filepath.Join(mountPath, CardIDFile)

	// Try to read existing ID
	if data, err := os.ReadFile(idPath); err == nil {
		cardID := strings.TrimSpace(string(data))
		if cardID != "" {
			log.Printf("Found existing card ID: %s", cardID)
			// Remount read-only now that we've read the ID
			if monitor != nil {
				if err := monitor.RemountReadOnly(); err != nil {
					log.Printf("Warning: Failed to remount read-only: %v", err)
				}
			}
			return cardID, false, nil
		}
	}

	// Generate new ID
	newID := generateCardID()
	log.Printf("Generated new card ID: %s", newID)

	// Write to card (filesystem should be read-write at this point)
	if err := os.WriteFile(idPath, []byte(newID+"\n"), 0644); err != nil {
		log.Printf("Warning: Could not write card ID to %s: %v", idPath, err)
		// Continue anyway - we'll use this ID for this session
	} else {
		log.Printf("Successfully wrote card ID to %s", idPath)
	}

	// Remount read-only now that we've written the ID
	if monitor != nil {
		if err := monitor.RemountReadOnly(); err != nil {
			log.Printf("Warning: Failed to remount read-only: %v", err)
		}
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
// The monitor parameter is optional - if provided, it will remount read-only after writing
func CreateNewCardID(mountPath string, monitor *Monitor) (string, error) {
	newID := generateCardID()
	idPath := filepath.Join(mountPath, CardIDFile)

	log.Printf("Creating new card ID: %s", newID)

	// Write to card (filesystem should be read-write at this point)
	if err := os.WriteFile(idPath, []byte(newID+"\n"), 0644); err != nil {
		log.Printf("Warning: Could not write card ID to %s: %v", idPath, err)
		// Continue anyway
	} else {
		log.Printf("Successfully wrote new card ID to %s", idPath)
	}

	// Remount read-only now that we've written the ID
	if monitor != nil {
		if err := monitor.RemountReadOnly(); err != nil {
			log.Printf("Warning: Failed to remount read-only: %v", err)
		}
	}

	return newID, nil
}

// DeviceInfo contains information about a storage device
type DeviceInfo struct {
	DevicePath  string `json:"device_path"`
	DeviceName  string `json:"device_name"`
	Size        int64  `json:"size"`
	SizeHuman   string `json:"size_human"`
	IsUSB       bool   `json:"is_usb"`
	IsMounted   bool   `json:"is_mounted"`
	MountPath   string `json:"mount_path,omitempty"`
	HasDCIM     bool   `json:"has_dcim"`
	VolumeLabel string `json:"volume_label,omitempty"`
}

// ListAllStorageDevices returns information about all detected storage devices
func ListAllStorageDevices() ([]DeviceInfo, error) {
	devices := []DeviceInfo{}

	// Check /dev/sd* devices
	matches, err := filepath.Glob("/dev/sd[a-z]1")
	if err != nil {
		return nil, fmt.Errorf("error globbing /dev/sd*: %w", err)
	}

	for _, dev := range matches {
		info, err := getDeviceInfo(dev)
		if err != nil {
			log.Printf("Warning: failed to get info for %s: %v", dev, err)
			continue
		}
		devices = append(devices, info)
	}

	// Check /dev/mmcblk* (excluding mmcblk0 which is usually the Pi's boot SD)
	matches, err = filepath.Glob("/dev/mmcblk[1-9]p1")
	if err == nil {
		for _, dev := range matches {
			info, err := getDeviceInfo(dev)
			if err != nil {
				log.Printf("Warning: failed to get info for %s: %v", dev, err)
				continue
			}
			devices = append(devices, info)
		}
	}

	return devices, nil
}

// getDeviceInfo retrieves detailed information about a storage device
func getDeviceInfo(devicePath string) (DeviceInfo, error) {
	info := DeviceInfo{
		DevicePath: devicePath,
		DeviceName: filepath.Base(devicePath),
	}

	// Get device size
	baseName := filepath.Base(devicePath)

	// Extract disk name and partition name
	// For /dev/sda1 -> disk=sda, partition=sda1
	// For /dev/mmcblk0p1 -> disk=mmcblk0, partition=mmcblk0p1
	var diskName, partitionName string
	if strings.Contains(baseName, "mmcblk") {
		// MMC device: mmcblk0p1 -> disk=mmcblk0, partition=mmcblk0p1
		parts := strings.Split(baseName, "p")
		if len(parts) >= 2 {
			diskName = parts[0]
			partitionName = baseName
		}
	} else {
		// SD device: sda1 -> disk=sda, partition=sda1
		diskName = strings.TrimRight(baseName, "0123456789")
		partitionName = baseName
	}

	// Try multiple sysfs paths for partition size
	var sizeData []byte
	var sizeErr error

	// Path 1: /sys/class/block/{partition}/size
	sizePath := filepath.Join("/sys/class/block", partitionName, "size")
	sizeData, sizeErr = os.ReadFile(sizePath)

	if sizeErr != nil {
		// Path 2: /sys/block/{disk}/{partition}/size
		sizePath = filepath.Join("/sys/block", diskName, partitionName, "size")
		sizeData, sizeErr = os.ReadFile(sizePath)
	}

	if sizeErr == nil && len(sizeData) > 0 {
		// Size is in 512-byte sectors
		var sectors int64
		fmt.Sscanf(strings.TrimSpace(string(sizeData)), "%d", &sectors)
		info.Size = sectors * 512
		info.SizeHuman = formatBytes(info.Size)
	} else {
		log.Printf("Warning: Could not read size for %s: %v", devicePath, sizeErr)
	}

	// Check if USB device by walking up the device tree
	sysPath := filepath.Join("/sys/block", diskName, "device")
	if _, err := os.Stat(sysPath); err == nil {
		info.IsUSB = isUSBDeviceHelper(sysPath)
	}

	// Check mount status
	data, err := os.ReadFile("/proc/mounts")
	if err == nil {
		lines := strings.Split(string(data), "\n")
		for _, line := range lines {
			if strings.HasPrefix(line, devicePath) {
				fields := strings.Fields(line)
				if len(fields) >= 2 {
					info.IsMounted = true
					info.MountPath = fields[1]

					// Check for DCIM if mounted
					info.HasDCIM = HasDCIM(info.MountPath)
				}
				break
			}
		}
	}

	// Get volume label using blkid
	cmd := exec.Command("blkid", "-s", "LABEL", "-o", "value", devicePath)
	if output, err := cmd.Output(); err == nil && len(output) > 0 {
		info.VolumeLabel = strings.TrimSpace(string(output))
	}

	return info, nil
}

// isUSBDeviceHelper is a standalone helper for checking USB devices (used by getDeviceInfo)
func isUSBDeviceHelper(sysPath string) bool {
	// Walk up the device tree looking for "usb" subsystem
	currentPath := sysPath
	for i := 0; i < 10; i++ { // Limit depth to prevent infinite loops
		// Check if we've reached the root
		if currentPath == "/" || currentPath == "/sys" {
			return false
		}

		// Check the subsystem symlink
		subsystemPath := filepath.Join(currentPath, "subsystem")
		if target, err := os.Readlink(subsystemPath); err == nil {
			if strings.Contains(target, "usb") {
				return true
			}
		}

		// Check uevent for USB indicators
		ueventPath := filepath.Join(currentPath, "uevent")
		if data, err := os.ReadFile(ueventPath); err == nil {
			ueventStr := string(data)
			if strings.Contains(ueventStr, "DEVTYPE=usb") ||
				strings.Contains(ueventStr, "usb") ||
				strings.Contains(ueventStr, "USB") {
				return true
			}
		}

		// Move up to parent directory
		currentPath = filepath.Dir(currentPath)
	}

	return false
}

// formatBytes formats bytes to human-readable string
func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}
