package sdmonitor

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/denysvitali/pictures-sync-s3/pkg/utils"
)

// PartitionInfo contains information about one partition on a storage device.
type PartitionInfo struct {
	DevicePath  string `json:"device_path"`
	DeviceName  string `json:"device_name"`
	Size        int64  `json:"size"`
	SizeHuman   string `json:"size_human"`
	FileSystem  string `json:"file_system,omitempty"`
	UUID        string `json:"uuid,omitempty"`
	VolumeLabel string `json:"volume_label,omitempty"`
	IsMounted   bool   `json:"is_mounted"`
	MountPath   string `json:"mount_path,omitempty"`
	HasDCIM     bool   `json:"has_dcim"`
}

// DeviceInfo contains information about a storage device
type DeviceInfo struct {
	DevicePath  string          `json:"device_path"`
	DeviceName  string          `json:"device_name"`
	Size        int64           `json:"size"`
	SizeHuman   string          `json:"size_human"`
	IsUSB       bool            `json:"is_usb"`
	IsMounted   bool            `json:"is_mounted"`
	MountPath   string          `json:"mount_path,omitempty"`
	HasDCIM     bool            `json:"has_dcim"`
	VolumeLabel string          `json:"volume_label,omitempty"`
	Partitions  []PartitionInfo `json:"partitions,omitempty"`
}

// Device enumeration patterns
var (
	// sdDevicePattern matches USB card readers (e.g., /dev/sda1)
	sdDevicePattern = "/dev/sd[a-z]1"
	// mmcDevicePattern matches MMC card readers, excluding mmcblk0 (Pi's boot SD)
	mmcDevicePattern = "/dev/mmcblk[1-9]p1"
)

// findStorageDevice finds available USB storage devices that are not mounted
// at a location other than ourMountPoint. ourMountPoint should be the
// monitor's configured mount path; pass "" to treat any mount as "elsewhere".
func findStorageDevice(ourMountPoint string) string {
	// Check /dev/sd* devices (USB card readers on Raspberry Pi)
	if dev := findDeviceByPattern(sdDevicePattern, ourMountPoint); dev != "" {
		return dev
	}

	// Also check /dev/mmcblk* for SD card readers (but exclude mmcblk0 which is the Pi's boot SD)
	if dev := findDeviceByPattern(mmcDevicePattern, ourMountPoint); dev != "" {
		return dev
	}

	return ""
}

// findDeviceByPattern searches for devices matching a glob pattern
func findDeviceByPattern(pattern, ourMountPoint string) string {
	matches, err := filepath.Glob(pattern)
	if err != nil {
		log.Printf("Error globbing %s: %v", pattern, err)
		return ""
	}

	// Return first available device that isn't mounted elsewhere
	for _, dev := range matches {
		if !isDeviceMountedElsewhere(dev, ourMountPoint) {
			return dev
		}
		log.Printf("Device %s is mounted elsewhere, skipping", dev)
	}

	return ""
}

// isDeviceMountedElsewhere checks if device is mounted somewhere other than our mount point.
// ourMountPoint is compared against the mount path in /proc/mounts to determine "elsewhere".
// If ourMountPoint is empty, any existing mount is treated as elsewhere.
func isDeviceMountedElsewhere(device, ourMountPoint string) bool {
	data, err := os.ReadFile("/proc/mounts")
	if err != nil {
		return false
	}

	// Check if device appears in mounts at a location other than our mount point
	lines := strings.Split(string(data), "\n")

	for _, line := range lines {
		if strings.HasPrefix(line, device+" ") {
			// Device is mounted, check where
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				mountPoint := parts[1]
				// If it's mounted at our location, it's not "elsewhere"
				if ourMountPoint != "" && mountPoint == ourMountPoint {
					return false
				}
				// Mounted somewhere else
				return true
			}
		}
	}

	// Not mounted at all
	return false
}

// ListAllStorageDevices returns information about all detected storage devices
func ListAllStorageDevices() ([]DeviceInfo, error) {
	devices := []DeviceInfo{}

	// Check /dev/sd* devices
	if err := collectDevicesByPattern(sdDevicePattern, &devices); err != nil {
		return nil, err
	}

	// Check /dev/mmcblk* (excluding mmcblk0 which is usually the Pi's boot SD)
	collectDevicesByPattern(mmcDevicePattern, &devices) // Ignore error for mmcblk*

	return devices, nil
}

// collectDevicesByPattern collects device info for devices matching a pattern
func collectDevicesByPattern(pattern string, devices *[]DeviceInfo) error {
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return fmt.Errorf("error globbing %s: %w", pattern, err)
	}

	for _, dev := range matches {
		info, err := getDeviceInfo(dev)
		if err != nil {
			log.Printf("Warning: failed to get info for %s: %v", dev, err)
			continue
		}
		*devices = append(*devices, info)
	}

	return nil
}

// getDeviceInfo retrieves detailed information about a storage device
func getDeviceInfo(devicePath string) (DeviceInfo, error) {
	info := DeviceInfo{
		DevicePath: devicePath,
		DeviceName: filepath.Base(devicePath),
	}

	// Get device size from sysfs
	size, err := getDeviceSize(devicePath)
	if err != nil {
		log.Printf("Warning: Could not read size for %s: %v", devicePath, err)
	} else {
		info.Size = size
		info.SizeHuman = formatBytes(size)
	}

	// Check if USB device
	diskName := extractDiskName(info.DeviceName)
	sysPath := filepath.Join("/sys/block", diskName, "device")
	if _, err := os.Stat(sysPath); err == nil {
		info.IsUSB = isUSBDevice(sysPath)
	}

	// Check mount status
	checkMountStatus(&info)

	// Get volume label
	info.VolumeLabel = getVolumeLabel(devicePath)
	info.Partitions = listPartitions(diskName)

	return info, nil
}

// getDeviceSize retrieves the size of a device from sysfs
func getDeviceSize(devicePath string) (int64, error) {
	baseName := filepath.Base(devicePath)
	diskName, partitionName := extractDiskAndPartitionName(baseName)

	// Try multiple sysfs paths for partition size
	sizePaths := []string{
		filepath.Join("/sys/class/block", partitionName, "size"),
		filepath.Join("/sys/block", diskName, partitionName, "size"),
	}

	for _, sizePath := range sizePaths {
		// #nosec G304 -- sizePath is constructed from sysfs device paths
		sizeData, err := os.ReadFile(sizePath)
		if err == nil && len(sizeData) > 0 {
			// Size is in 512-byte sectors
			var sectors int64
			fmt.Sscanf(strings.TrimSpace(string(sizeData)), "%d", &sectors)
			return sectors * 512, nil
		}
	}

	return 0, fmt.Errorf("could not read device size from sysfs")
}

// extractDiskName extracts the disk name from a device name
// For /dev/sda1 -> sda
// For /dev/mmcblk0p1 -> mmcblk0
func extractDiskName(baseName string) string {
	if strings.Contains(baseName, "mmcblk") {
		// MMC device: mmcblk0p1 -> mmcblk0
		parts := strings.Split(baseName, "p")
		if len(parts) >= 2 {
			return parts[0]
		}
	}
	// SD device: sda1 -> sda
	return strings.TrimRight(baseName, "0123456789")
}

// extractDiskAndPartitionName extracts both disk and partition names
func extractDiskAndPartitionName(baseName string) (disk, partition string) {
	if strings.Contains(baseName, "mmcblk") {
		// MMC device: mmcblk0p1 -> disk=mmcblk0, partition=mmcblk0p1
		parts := strings.Split(baseName, "p")
		if len(parts) >= 2 {
			return parts[0], baseName
		}
	}
	// SD device: sda1 -> disk=sda, partition=sda1
	disk = strings.TrimRight(baseName, "0123456789")
	return disk, baseName
}

// isUSBDevice checks if a device is connected via USB by walking sysfs tree
func isUSBDevice(sysPath string) bool {
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
		if isUSBInUevent(currentPath) {
			return true
		}

		// Move up to parent directory
		currentPath = filepath.Dir(currentPath)
	}

	return false
}

// isUSBDeviceHelper is an alias for isUSBDevice for backward compatibility with tests
func isUSBDeviceHelper(sysPath string) bool {
	return isUSBDevice(sysPath)
}

// isUSBInUevent checks if uevent file contains USB indicators
func isUSBInUevent(path string) bool {
	ueventPath := filepath.Join(path, "uevent")
	// #nosec G304 -- ueventPath is constructed from sysfs device paths
	data, err := os.ReadFile(ueventPath)
	if err != nil {
		return false
	}

	ueventStr := string(data)
	return strings.Contains(ueventStr, "DEVTYPE=usb") ||
		strings.Contains(ueventStr, "usb") ||
		strings.Contains(ueventStr, "USB")
}

// checkMountStatus checks if a device is mounted and updates DeviceInfo
func checkMountStatus(info *DeviceInfo) {
	mounted, mountPath := mountStatus(info.DevicePath)
	info.IsMounted = mounted
	info.MountPath = mountPath
	if mounted {
		info.HasDCIM = HasDCIM(mountPath)
	}
}

func mountStatus(devicePath string) (bool, string) {
	data, err := os.ReadFile("/proc/mounts")
	if err != nil {
		return false, ""
	}

	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == devicePath {
			return true, fields[1]
		}
	}

	return false, ""
}

func listPartitions(diskName string) []PartitionInfo {
	entries, err := os.ReadDir(filepath.Join("/sys/block", diskName))
	if err != nil {
		return nil
	}

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if isPartitionName(diskName, entry.Name()) {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)

	partitions := make([]PartitionInfo, 0, len(names))
	for _, name := range names {
		partitions = append(partitions, getPartitionInfo(name))
	}
	return partitions
}

func isPartitionName(diskName, name string) bool {
	var suffix string
	if strings.HasPrefix(diskName, "mmcblk") || strings.HasPrefix(diskName, "nvme") {
		prefix := diskName + "p"
		if !strings.HasPrefix(name, prefix) {
			return false
		}
		suffix = strings.TrimPrefix(name, prefix)
	} else {
		if !strings.HasPrefix(name, diskName) {
			return false
		}
		suffix = strings.TrimPrefix(name, diskName)
	}
	if suffix == "" {
		return false
	}
	for _, r := range suffix {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func getPartitionInfo(partitionName string) PartitionInfo {
	devicePath := filepath.Join("/dev", partitionName)
	info := PartitionInfo{
		DevicePath:  devicePath,
		DeviceName:  partitionName,
		FileSystem:  getBlkIDValue(devicePath, "TYPE"),
		UUID:        getBlkIDValue(devicePath, "UUID"),
		VolumeLabel: getVolumeLabel(devicePath),
	}

	if size, err := getDeviceSize(devicePath); err == nil {
		info.Size = size
		info.SizeHuman = formatBytes(size)
	}

	info.IsMounted, info.MountPath = mountStatus(devicePath)
	if info.IsMounted {
		info.HasDCIM = HasDCIM(info.MountPath)
	}

	return info
}

// GetVolumeID attempts to get a unique ID for the SD card
func GetVolumeID(device string) string {
	if uuid := getBlkIDValue(device, "UUID"); uuid != "" {
		return uuid
	}

	// Fallback to device name
	return filepath.Base(device)
}

// getVolumeLabel gets the volume label using blkid
func getVolumeLabel(devicePath string) string {
	return getBlkIDValue(devicePath, "LABEL")
}

func getBlkIDValue(devicePath, field string) string {
	// #nosec G204 -- devicePath is a /dev block device controlled by the system
	cmd := exec.Command("blkid", "-s", field, "-o", "value", devicePath)
	if output, err := cmd.Output(); err == nil && len(output) > 0 {
		return strings.TrimSpace(string(output))
	}
	return ""
}

// formatBytes formats bytes to human-readable string
// Uses the utils package for consistent formatting across the codebase
func formatBytes(bytes int64) string {
	return utils.FormatBytes(bytes)
}
