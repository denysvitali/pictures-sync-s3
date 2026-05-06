package sdmonitor

import (
	"context"
	"testing"
)

func TestIsSupportedDevicePath(t *testing.T) {
	tests := []struct {
		name       string
		devicePath string
		want       bool
	}{
		{name: "usb partition", devicePath: "/dev/sda1", want: true},
		{name: "second usb partition rejected", devicePath: "/dev/sda2", want: false},
		{name: "mmc card reader partition", devicePath: "/dev/mmcblk1p1", want: true},
		{name: "boot mmc partition rejected", devicePath: "/dev/mmcblk0p1", want: false},
		{name: "whole disk rejected", devicePath: "/dev/sda", want: false},
		{name: "path traversal rejected", devicePath: "/dev/../dev/sda1", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsSupportedDevicePath(tt.devicePath); got != tt.want {
				t.Fatalf("IsSupportedDevicePath(%q) = %v, want %v", tt.devicePath, got, tt.want)
			}
		})
	}
}

func TestFormatCurrentDeviceRejectsUnmountedCard(t *testing.T) {
	monitor := NewMonitor(t.TempDir())

	err := monitor.FormatCurrentDevice(context.Background(), "/dev/sda1")
	if err == nil || err.Error() != "no SD card mounted" {
		t.Fatalf("Expected no SD card mounted error, got %v", err)
	}
}

func TestFormatCurrentDeviceRejectsUnsupportedDevice(t *testing.T) {
	monitor := NewMonitor(t.TempDir())

	err := monitor.FormatCurrentDevice(context.Background(), "/dev/sda")
	if err == nil || err.Error() != "unsupported SD card device path: /dev/sda" {
		t.Fatalf("Expected unsupported device error, got %v", err)
	}
}
