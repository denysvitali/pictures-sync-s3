package sdmonitor

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/diskfs/go-diskfs"
	"github.com/diskfs/go-diskfs/filesystem"
	"github.com/diskfs/go-diskfs/partition/mbr"
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

func TestFormatDeviceRejectsUnsupportedDevicePath(t *testing.T) {
	err := formatDeviceExpectError(context.Background(), "/tmp/sdcard.img", "")
	if err == nil {
		t.Fatal("Expected error")
	}
	if !strings.Contains(err.Error(), "unsupported SD card device path") {
		t.Fatalf("Expected unsupported device path error, got %v", err)
	}
}

func TestFormatTargetForDevicePath(t *testing.T) {
	tests := []struct {
		name          string
		devicePath    string
		wantDisk      string
		wantPartition string
	}{
		{name: "usb card reader", devicePath: "/dev/sda1", wantDisk: "/dev/sda", wantPartition: "/dev/sda1"},
		{name: "mmc card reader", devicePath: "/dev/mmcblk1p1", wantDisk: "/dev/mmcblk1", wantPartition: "/dev/mmcblk1p1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := formatTargetForDevicePath(tt.devicePath)
			if err != nil {
				t.Fatalf("formatTargetForDevicePath() error = %v", err)
			}
			if got.diskPath != tt.wantDisk || got.partitionPath != tt.wantPartition {
				t.Fatalf("formatTargetForDevicePath() = %+v, want disk %q partition %q", got, tt.wantDisk, tt.wantPartition)
			}
		})
	}
}

func TestSinglePartitionTableUsesAvailableCardCapacity(t *testing.T) {
	const diskSize = 64 * 1024 * 1024

	table, err := singlePartitionTable(diskSize, mbr.Fat32LBA)
	if err != nil {
		t.Fatalf("singlePartitionTable() error = %v", err)
	}
	if len(table.Partitions) != 1 {
		t.Fatalf("partition count = %d, want 1", len(table.Partitions))
	}

	partition := table.Partitions[0]
	if partition.Index != 1 {
		t.Fatalf("partition index = %d, want 1", partition.Index)
	}
	if partition.Type != mbr.Fat32LBA {
		t.Fatalf("partition type = %v, want %v", partition.Type, mbr.Fat32LBA)
	}
	if partition.Start != partitionStartSector {
		t.Fatalf("partition start = %d, want %d", partition.Start, partitionStartSector)
	}

	wantSectors := uint32(diskSize/partitionSectorSize) - partitionStartSector
	if partition.Size != wantSectors {
		t.Fatalf("partition size = %d sectors, want %d", partition.Size, wantSectors)
	}
}

func TestRepartitionDeviceCreatesSinglePartition(t *testing.T) {
	imagePath := createFormatTestImage(t, 64*1024*1024)

	if err := repartitionDevice(context.Background(), imagePath, mbr.Fat32LBA); err != nil {
		t.Fatalf("repartitionDevice() error = %v", err)
	}

	disk, err := diskfs.Open(imagePath, diskfs.WithOpenMode(diskfs.ReadOnly), diskfs.WithSectorSize(diskfs.SectorSize512))
	if err != nil {
		t.Fatalf("Open partitioned image: %v", err)
	}
	defer disk.Backend.Close()

	table, err := disk.GetPartitionTable()
	if err != nil {
		t.Fatalf("GetPartitionTable() error = %v", err)
	}
	partitions := table.GetPartitions()
	if len(partitions) != 4 {
		t.Fatalf("partition table entries = %d, want 4", len(partitions))
	}
	if partitions[0].GetStart() != int64(partitionStartSector)*partitionSectorSize {
		t.Fatalf("partition start = %d, want %d", partitions[0].GetStart(), int64(partitionStartSector)*partitionSectorSize)
	}
	wantSize := (int64(64*1024*1024)/partitionSectorSize - int64(partitionStartSector)) * partitionSectorSize
	if partitions[0].GetSize() != wantSize {
		t.Fatalf("partition size = %d, want %d", partitions[0].GetSize(), wantSize)
	}
	for i, partition := range partitions[1:] {
		if partition.GetSize() != 0 {
			t.Fatalf("extra partition %d size = %d, want 0", i+2, partition.GetSize())
		}
	}
}

func TestPrepareDeviceForFormatWipesStaleSignatures(t *testing.T) {
	imagePath := createFormatTestImage(t, 16*1024*1024)
	f, err := os.OpenFile(imagePath, os.O_RDWR, 0)
	if err != nil {
		t.Fatalf("Open image: %v", err)
	}
	if _, err := f.WriteAt([]byte("EXFAT   "), 3); err != nil {
		_ = f.Close()
		t.Fatalf("Write start signature: %v", err)
	}
	if _, err := f.WriteAt([]byte("EXFATBACKUP"), 16*1024*1024-1024); err != nil {
		_ = f.Close()
		t.Fatalf("Write end signature: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("Close image: %v", err)
	}

	if _, err := prepareDeviceForFormat(context.Background(), imagePath); err != nil {
		t.Fatalf("prepareDeviceForFormat() error = %v", err)
	}

	data, err := os.ReadFile(imagePath)
	if err != nil {
		t.Fatalf("Read image: %v", err)
	}
	if strings.Contains(string(data[:4096]), "EXFAT") {
		t.Fatal("Start signature was not wiped")
	}
	if strings.Contains(string(data[len(data)-4096:]), "EXFATBACKUP") {
		t.Fatal("End signature was not wiped")
	}
}

func formatDeviceExpectError(ctx context.Context, devicePath, label string) error {
	_, err := formatDevice(ctx, devicePath, label)
	return err
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
