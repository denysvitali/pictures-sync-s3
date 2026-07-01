package deviceinfo

import (
	"reflect"
	"testing"

	"github.com/denysvitali/pictures-sync-s3/pkg/sdmonitor"
	"github.com/denysvitali/pictures-sync-s3/pkg/state"
)

func TestToStateDevices(t *testing.T) {
	devices := []sdmonitor.DeviceInfo{
		{
			DevicePath:  "/dev/sda",
			DeviceName:  "sda",
			Size:        128,
			SizeHuman:   "128 B",
			IsUSB:       true,
			IsMounted:   true,
			MountPath:   "/mnt/card",
			HasDCIM:     true,
			VolumeLabel: "CAMERA",
			Partitions: []sdmonitor.PartitionInfo{
				{
					DevicePath:  "/dev/sda1",
					DeviceName:  "sda1",
					Size:        64,
					SizeHuman:   "64 B",
					FileSystem:  "vfat",
					UUID:        "uuid",
					VolumeLabel: "CANON",
					IsMounted:   true,
					MountPath:   "/mnt/card",
					HasDCIM:     true,
				},
			},
		},
	}

	want := []state.DeviceInfo{
		{
			DevicePath:  "/dev/sda",
			DeviceName:  "sda",
			Size:        128,
			SizeHuman:   "128 B",
			IsUSB:       true,
			IsMounted:   true,
			MountPath:   "/mnt/card",
			HasDCIM:     true,
			VolumeLabel: "CAMERA",
			Partitions: []state.PartitionInfo{
				{
					DevicePath:  "/dev/sda1",
					DeviceName:  "sda1",
					Size:        64,
					SizeHuman:   "64 B",
					FileSystem:  "vfat",
					UUID:        "uuid",
					VolumeLabel: "CANON",
					IsMounted:   true,
					MountPath:   "/mnt/card",
					HasDCIM:     true,
				},
			},
		},
	}

	if got := ToStateDevices(devices); !reflect.DeepEqual(got, want) {
		t.Fatalf("ToStateDevices() = %#v, want %#v", got, want)
	}
}
