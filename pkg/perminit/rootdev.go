package perminit

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// CmdlineFile is the file used by BootBlockDevice to read the kernel command
// line. It is overridable for testing.
var CmdlineFile = "/proc/cmdline"

// PartUUIDDir is the directory used to resolve PARTUUID= root devices.
// It is overridable for testing.
var PartUUIDDir = "/dev/disk/by-partuuid"

// DeviceDir is the directory containing block devices. It is overridable for
// tests that resolve PARTUUID links in a temporary filesystem tree.
var DeviceDir = "/dev"

// rootRe matches a literal device path passed via root=.
//
// The same patterns gokrazy uses; see github.com/gokrazy/internal/rootdev.
var rootRe = regexp.MustCompile(`(?:^|\s)(?:root|ubd0)=(/dev/(?:mmcblk[01]p|sda|loop0p|nvme0n1p))([23])\b`)
var rootPartUUIDRe = regexp.MustCompile(`(?:^|\s)root=PARTUUID=([A-Fa-f0-9-]+)(?:/PARTNROFF=[+-]?\d+)?\b`)

// BootBlockDevice returns the file system path to the block device gokrazy
// booted from (e.g. /dev/mmcblk0).
func BootBlockDevice() (string, error) {
	b, err := os.ReadFile(CmdlineFile)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", CmdlineFile, err)
	}
	matches := rootRe.FindStringSubmatch(string(b))
	if len(matches) == 3 {
		return partitionParentDevice(matches[1]), nil
	}

	matches = rootPartUUIDRe.FindStringSubmatch(string(b))
	if len(matches) == 2 {
		dev, err := blockDeviceForPartUUID(matches[1])
		if err != nil {
			return "", err
		}
		return dev, nil
	}

	return "", fmt.Errorf("could not find supported root= entry in %s: %q", CmdlineFile, strings.TrimSpace(string(b)))
}

func blockDeviceForPartUUID(partUUID string) (string, error) {
	path := filepath.Join(PartUUIDDir, partUUID)
	target, err := os.Readlink(path)
	if err != nil {
		return "", fmt.Errorf("resolve PARTUUID %s: %w", partUUID, err)
	}
	if !filepath.IsAbs(target) {
		target = filepath.Clean(filepath.Join(filepath.Dir(path), target))
	}
	deviceDir := filepath.Clean(DeviceDir)
	if !strings.HasPrefix(target, deviceDir+string(os.PathSeparator)) {
		return "", fmt.Errorf("resolve PARTUUID %s: target %q is not under %s", partUUID, target, deviceDir)
	}
	return partitionParentDevice(target), nil
}

func partitionParentDevice(partition string) string {
	dir := filepath.Dir(partition)
	base := filepath.Base(partition)
	for _, prefix := range []string{"mmcblk", "loop", "nvme"} {
		if strings.HasPrefix(base, prefix) {
			if idx := strings.LastIndex(base, "p"); idx > len(prefix) {
				return filepath.Join(dir, base[:idx])
			}
		}
	}
	return filepath.Join(dir, strings.TrimRight(base, "0123456789"))
}

// PartitionDevice returns the device path for the given partition number on
// the given block device. E.g. PartitionDevice("/dev/mmcblk0", 4) ->
// "/dev/mmcblk0p4".
func PartitionDevice(blockDev string, n int) string {
	if (strings.HasPrefix(blockDev, "/dev/mmcblk") ||
		strings.HasPrefix(blockDev, "/dev/loop") ||
		strings.HasPrefix(blockDev, "/dev/nvme")) &&
		!strings.HasSuffix(blockDev, "p") {
		blockDev += "p"
	}
	return blockDev + strconv.Itoa(n)
}
