package deviceinfo

import (
	"github.com/denysvitali/pictures-sync-s3/pkg/sdmonitor"
	"github.com/denysvitali/pictures-sync-s3/pkg/state"
)

// ToStateDevices converts detected storage devices into the shape persisted in
// CurrentState.
func ToStateDevices(devices []sdmonitor.DeviceInfo) []state.DeviceInfo {
	stateDevices := make([]state.DeviceInfo, len(devices))
	for i, d := range devices {
		stateDevices[i] = state.DeviceInfo{
			DevicePath:  d.DevicePath,
			DeviceName:  d.DeviceName,
			Size:        d.Size,
			SizeHuman:   d.SizeHuman,
			IsUSB:       d.IsUSB,
			IsMounted:   d.IsMounted,
			MountPath:   d.MountPath,
			HasDCIM:     d.HasDCIM,
			VolumeLabel: d.VolumeLabel,
			Partitions:  make([]state.PartitionInfo, len(d.Partitions)),
		}
		for j, p := range d.Partitions {
			stateDevices[i].Partitions[j] = state.PartitionInfo{
				DevicePath:  p.DevicePath,
				DeviceName:  p.DeviceName,
				Size:        p.Size,
				SizeHuman:   p.SizeHuman,
				FileSystem:  p.FileSystem,
				UUID:        p.UUID,
				VolumeLabel: p.VolumeLabel,
				IsMounted:   p.IsMounted,
				MountPath:   p.MountPath,
				HasDCIM:     p.HasDCIM,
			}
		}
	}
	return stateDevices
}
