package sdmonitor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const formatTimeout = 2 * time.Minute

var formatCommandContext = exec.CommandContext
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

	args := buildFormatArgs(devicePath, label)
	// #nosec G204 -- devicePath is restricted to monitored SD-card partition patterns.
	cmd := formatCommandContext(formatCtx, "mkfs.vfat", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return formatCommandError(err, stderr.String())
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

func buildFormatArgs(devicePath, label string) []string {
	args := []string{"-F", "32"}
	if label != "" {
		args = append(args, "-n", label)
	}
	args = append(args, devicePath)
	return args
}

func formatCommandError(err error, stderr string) error {
	var execErr *exec.Error
	if errors.As(err, &execErr) && errors.Is(execErr.Err, exec.ErrNotFound) {
		return fmt.Errorf("format SD card: mkfs.vfat not found in PATH; include github.com/gokrazy/mkfs in the gokrazy image: %w", err)
	}

	detail := strings.TrimSpace(stderr)
	if detail != "" {
		return fmt.Errorf("format SD card: %w: %s", err, detail)
	}
	return fmt.Errorf("format SD card: %w", err)
}
