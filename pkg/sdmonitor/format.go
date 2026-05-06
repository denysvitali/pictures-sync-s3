package sdmonitor

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const formatTimeout = 2 * time.Minute

var formatCommandContext = exec.CommandContext

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

// FormatCurrentDevice unmounts and formats the currently mounted SD-card partition as FAT32.
func (m *Monitor) FormatCurrentDevice(ctx context.Context, devicePath string) error {
	if !IsSupportedDevicePath(devicePath) {
		return fmt.Errorf("unsupported SD card device path: %s", devicePath)
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
	m.mu.Lock()
	m.lastDevice = ""
	m.mu.Unlock()
	m.mountsCacheTime = time.Time{}

	args := []string{"-F", "32", "-n", "PICTURES", devicePath}
	// #nosec G204 -- devicePath is restricted to monitored SD-card partition patterns.
	cmd := formatCommandContext(formatCtx, "mkfs.vfat", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail != "" {
			return fmt.Errorf("format SD card: %w: %s", err, detail)
		}
		return fmt.Errorf("format SD card: %w", err)
	}

	log.Printf("Formatted SD card partition %s as FAT32", devicePath)
	return nil
}
