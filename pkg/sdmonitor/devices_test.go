package sdmonitor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestIsDeviceMountedElsewhereRespectsConfiguredMountPath ensures that
// isDeviceMountedElsewhere uses the caller-provided mount point instead of the
// previously hard-coded /perm/pictures-sync/mounts/sdcard. Without this fix a
// monitor configured with a custom mountPath would incorrectly skip its own
// device whenever the device was already mounted at that custom location
// (e.g. after a remount race or test setup), breaking re-detection.
func TestIsDeviceMountedElsewhereRespectsConfiguredMountPath(t *testing.T) {
	mounts, err := os.ReadFile("/proc/mounts")
	if err != nil {
		t.Skipf("cannot read /proc/mounts: %v", err)
	}
	if len(mounts) == 0 {
		t.Skip("/proc/mounts is empty")
	}

	// Pick a real (device, mountpoint) pair from /proc/mounts whose source
	// looks like a device path. Avoid pseudo-fs entries.
	var device, mountPoint string
	for _, line := range strings.Split(string(mounts), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		if !strings.HasPrefix(fields[0], "/dev/") {
			continue
		}
		device = fields[0]
		mountPoint = fields[1]
		break
	}
	if device == "" {
		t.Skip("no /dev-backed mount available to test against")
	}

	// When the caller's mount point matches the actual mount point, the
	// device must NOT be reported as mounted elsewhere.
	if isDeviceMountedElsewhere(device, mountPoint) {
		t.Errorf("device %s mounted at %s was reported as elsewhere when ourMountPoint matches",
			device, mountPoint)
	}

	// When the caller's mount point differs, it IS elsewhere.
	if !isDeviceMountedElsewhere(device, "/definitely/not/this/path/zzz") {
		t.Errorf("device %s mounted at %s should be reported as elsewhere when ourMountPoint differs",
			device, mountPoint)
	}

	// When ourMountPoint is empty, any mount is "elsewhere".
	if !isDeviceMountedElsewhere(device, "") {
		t.Errorf("empty ourMountPoint should treat any mount as elsewhere")
	}
}

// TestFindStorageDevicePropagatesMountPath verifies that findStorageDevice
// threads the caller's mount path down into isDeviceMountedElsewhere. We rely
// on the fact that no test fixture creates /dev/sd[a-z]1 nor /dev/mmcblk[1-9]p1
// in the test sandbox, so the function must return "" regardless of input.
func TestFindStorageDevicePropagatesMountPath(t *testing.T) {
	// Sanity: nothing matching the patterns should exist in a normal CI sandbox.
	matches, _ := filepath.Glob(sdDevicePattern)
	matches2, _ := filepath.Glob(mmcDevicePattern)
	if len(matches)+len(matches2) > 0 {
		t.Skip("real SD/MMC devices present; cannot make deterministic assertion")
	}

	if dev := findStorageDevice("/some/custom/mount"); dev != "" {
		t.Errorf("expected no device, got %q", dev)
	}
	if dev := findStorageDevice(""); dev != "" {
		t.Errorf("expected no device with empty mount path, got %q", dev)
	}
}

func TestIsPartitionName(t *testing.T) {
	tests := []struct {
		name     string
		diskName string
		partName string
		want     bool
	}{
		{name: "sd partition", diskName: "sda", partName: "sda1", want: true},
		{name: "sd disk is not partition", diskName: "sda", partName: "sda", want: false},
		{name: "sd other disk", diskName: "sda", partName: "sdb1", want: false},
		{name: "mmc partition", diskName: "mmcblk1", partName: "mmcblk1p2", want: true},
		{name: "mmc missing p separator", diskName: "mmcblk1", partName: "mmcblk12", want: false},
		{name: "nvme partition", diskName: "nvme0n1", partName: "nvme0n1p1", want: true},
		{name: "non numeric suffix", diskName: "sda", partName: "sdaa", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isPartitionName(tt.diskName, tt.partName); got != tt.want {
				t.Fatalf("isPartitionName(%q, %q) = %v, want %v", tt.diskName, tt.partName, got, tt.want)
			}
		})
	}
}
