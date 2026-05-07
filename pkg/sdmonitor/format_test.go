package sdmonitor

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/diskfs/go-diskfs"
	"github.com/diskfs/go-diskfs/filesystem"
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

func TestFormatCurrentDeviceIgnoresDeviceUntilRemovalAfterAttempt(t *testing.T) {
	monitor := NewMonitor(t.TempDir())
	monitor.lastDevice = "/dev/sda1"

	monitor.ignoreDeviceUntilRemoval("/dev/sda1")

	if monitor.ignoredDevice != "/dev/sda1" {
		t.Fatalf("ignoredDevice = %q, want /dev/sda1", monitor.ignoredDevice)
	}
	if monitor.lastDevice != "" {
		t.Fatalf("lastDevice = %q, want cleared device", monitor.lastDevice)
	}
}

func TestRedetectCurrentDeviceClearsIgnoredDevice(t *testing.T) {
	monitor := NewMonitor(t.TempDir())
	monitor.ignoreDeviceUntilRemoval("/dev/sda1")

	err := monitor.RedetectCurrentDevice()
	if err == nil || err.Error() != "no SD card detected" {
		t.Fatalf("Expected no SD card detected error, got %v", err)
	}
	if monitor.ignoredDevice != "" {
		t.Fatalf("ignoredDevice = %q, want cleared device", monitor.ignoredDevice)
	}
}

func TestFormatFAT32DeviceCreatesFilesystem(t *testing.T) {
	imagePath := createFormatTestImage(t, 64*1024*1024)

	if err := formatFAT32Device(context.Background(), imagePath, "CAMERA_1"); err != nil {
		t.Fatalf("formatFAT32Device() error = %v", err)
	}

	disk, err := diskfs.Open(imagePath, diskfs.WithOpenMode(diskfs.ReadOnly), diskfs.WithSectorSize(diskfs.SectorSize512))
	if err != nil {
		t.Fatalf("Open formatted image: %v", err)
	}
	defer disk.Backend.Close()

	fs, err := disk.GetFilesystem(0)
	if err != nil {
		t.Fatalf("GetFilesystem() error = %v", err)
	}
	if fs.Type() != filesystem.TypeFat32 {
		t.Fatalf("filesystem type = %v, want %v", fs.Type(), filesystem.TypeFat32)
	}
	if fs.Label() != "CAMERA_1" {
		t.Fatalf("filesystem label = %q, want CAMERA_1", fs.Label())
	}
}

func TestFormatFAT32DeviceHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := formatFAT32Device(ctx, "/dev/sda1", "")
	if err == nil {
		t.Fatal("Expected error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Error does not wrap context.Canceled: %v", err)
	}
}

func createFormatTestImage(t *testing.T, size int64) string {
	t.Helper()

	imagePath := t.TempDir() + "/sdcard.img"
	f, err := os.Create(imagePath)
	if err != nil {
		t.Fatalf("Create test image: %v", err)
	}
	if err := f.Truncate(size); err != nil {
		_ = f.Close()
		t.Fatalf("Truncate test image: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("Close test image: %v", err)
	}

	return imagePath
}
