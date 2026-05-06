package sdmonitor

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unsafe"

	"github.com/diskfs/go-diskfs/backend"
	"github.com/diskfs/go-diskfs/backend/file"
	"github.com/diskfs/go-diskfs/filesystem/fat32"
	"golang.org/x/sys/unix"
)

const formatTimeout = 2 * time.Minute

var validVolumeLabelPattern = regexp.MustCompile(`^[A-Za-z0-9 _-]{1,11}$`)

// IsSupportedDevicePath returns true for partition paths the monitor is allowed to manage.
func IsSupportedDevicePath(devicePath string) bool {
	cleanPath := filepath.Clean(devicePath)
	if cleanPath != devicePath {
		return false
	}

	for _, pattern := range []string{sdDevicePattern, mmcDevicePattern} {
		matched, err := filepath.Match(pattern, devicePath)
		if err == nil && matched {
			return true
		}
	}

	return false
}

// ValidateVolumeLabel validates an optional FAT volume label.
func ValidateVolumeLabel(label string) error {
	if label == "" {
		return nil
	}
	if !validVolumeLabelPattern.MatchString(label) {
		return fmt.Errorf("label must be 1-11 characters and contain only letters, numbers, spaces, underscores, or hyphens")
	}
	return nil
}

// FormatCurrentDevice unmounts and formats the currently mounted SD-card partition as FAT32.
func (m *Monitor) FormatCurrentDevice(ctx context.Context, devicePath, label string) error {
	if !IsSupportedDevicePath(devicePath) {
		return fmt.Errorf("unsupported SD card device path: %s", devicePath)
	}
	label = strings.TrimSpace(label)
	if err := ValidateVolumeLabel(label); err != nil {
		return err
	}

	m.mu.RLock()
	currentDevice := m.lastDevice
	m.mu.RUnlock()
	if currentDevice == "" {
		return fmt.Errorf("no SD card mounted")
	}
	if currentDevice != devicePath {
		return fmt.Errorf("selected device is not mounted")
	}

	formatCtx, cancel := context.WithTimeout(ctx, formatTimeout)
	defer cancel()

	m.mountMu.Lock()
	defer m.mountMu.Unlock()

	log.Printf("Formatting SD card partition %s", devicePath)
	if err := m.unmount(); err != nil {
		return fmt.Errorf("unmount SD card before format: %w", err)
	}
	m.ignoreDeviceUntilRemoval(devicePath)
	m.mountsCacheTime = time.Time{}
	log.Printf("SD card partition %s will be ignored until removal after format attempt", devicePath)

	if err := formatFAT32Device(formatCtx, devicePath, label); err != nil {
		return err
	}

	log.Printf("Formatted SD card partition %s as FAT32", devicePath)
	return nil
}

func (m *Monitor) ignoreDeviceUntilRemoval(devicePath string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastDevice = ""
	m.ignoredDevice = devicePath
}

func formatFAT32Device(ctx context.Context, devicePath, label string) (err error) {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("format SD card: %w", err)
	}

	device, err := file.OpenFromPath(devicePath, false)
	if err != nil {
		return fmt.Errorf("format SD card: open device: %w", err)
	}
	defer func() {
		if closeErr := device.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("format SD card: close device: %w", closeErr)
		}
	}()

	size, err := storageSize(device)
	if err != nil {
		return fmt.Errorf("format SD card: determine device size: %w", err)
	}

	if _, err := fat32.Create(device, size, 0, int64(fat32.SectorSize512), label, false); err != nil {
		return fmt.Errorf("format SD card as FAT32: %w", err)
	}
	if err := syncStorage(device); err != nil {
		return fmt.Errorf("format SD card: sync device: %w", err)
	}

	return nil
}

func storageSize(device backend.Storage) (int64, error) {
	info, err := device.Stat()
	if err != nil {
		return 0, err
	}
	if info.Size() > 0 {
		return info.Size(), nil
	}

	osFile, err := device.Sys()
	if err != nil {
		return 0, err
	}

	var size uint64
	// #nosec G103 -- BLKGETSIZE64 requires passing a pointer for the kernel to fill.
	if _, _, errno := unix.Syscall(
		unix.SYS_IOCTL,
		osFile.Fd(),
		uintptr(unix.BLKGETSIZE64),
		uintptr(unsafe.Pointer(&size)),
	); errno != 0 {
		return 0, errno
	}
	if size > uint64(math.MaxInt64) {
		return 0, errors.New("device size exceeds supported range")
	}

	return int64(size), nil
}

func syncStorage(device backend.Storage) error {
	osFile, err := device.Sys()
	if err != nil {
		return err
	}
	return osFile.Sync()
}
