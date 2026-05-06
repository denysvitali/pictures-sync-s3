package sdmonitor

import (
	"context"
	"reflect"
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

	err := monitor.FormatCurrentDevice(context.Background(), "/dev/sda1", "")
	if err == nil || err.Error() != "no SD card mounted" {
		t.Fatalf("Expected no SD card mounted error, got %v", err)
	}
}

func TestFormatCurrentDeviceRejectsUnsupportedDevice(t *testing.T) {
	monitor := NewMonitor(t.TempDir())

	err := monitor.FormatCurrentDevice(context.Background(), "/dev/sda", "")
	if err == nil || err.Error() != "unsupported SD card device path: /dev/sda" {
		t.Fatalf("Expected unsupported device error, got %v", err)
	}
}

func TestValidateVolumeLabel(t *testing.T) {
	tests := []struct {
		name    string
		label   string
		wantErr bool
	}{
		{name: "blank default", label: "", wantErr: false},
		{name: "simple label", label: "CAMERA_1", wantErr: false},
		{name: "spaces allowed", label: "SD CARD", wantErr: false},
		{name: "too long", label: "TOO-LONG-LABEL", wantErr: true},
		{name: "special characters rejected", label: "CAMERA!", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateVolumeLabel(tt.label)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateVolumeLabel(%q) error = %v, wantErr %v", tt.label, err, tt.wantErr)
			}
		})
	}
}

func TestFormatCurrentDeviceOmitsLabelByDefault(t *testing.T) {
	args := buildFormatArgs("/dev/sda1", "")
	if containsArg(args, "-n") {
		t.Fatalf("Expected no label args, got %v", args)
	}
	want := []string{"-F", "32", "/dev/sda1"}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("Expected args %v, got %v", want, args)
	}
}

func TestFormatCurrentDeviceIncludesProvidedLabel(t *testing.T) {
	args := buildFormatArgs("/dev/sda1", "CAMERA_1")

	want := []string{"-F", "32", "-n", "CAMERA_1", "/dev/sda1"}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("Expected args %v, got %v", want, args)
	}
}

func containsArg(args []string, want string) bool {
	for _, arg := range args {
		if arg == want {
			return true
		}
	}
	return false
}
