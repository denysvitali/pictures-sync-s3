package sdmonitor

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unsafe"

	diskfs "github.com/diskfs/go-diskfs"
	"github.com/diskfs/go-diskfs/backend"
	"github.com/diskfs/go-diskfs/backend/file"
	"github.com/diskfs/go-diskfs/filesystem/fat32"
	"github.com/diskfs/go-diskfs/partition/mbr"
	"golang.org/x/sys/unix"
)

const formatTimeout = 2 * time.Minute
const sdxcExFATThreshold = 32 * 1024 * 1024 * 1024
const signatureWipeBytes int64 = 4 * 1024 * 1024
const partitionStartSector uint32 = 2048
const partitionSectorSize int64 = 512

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

// FormatCurrentDevice unmounts the current SD card, recreates one full-card partition, and formats it.
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
	m.mountsCacheMu.Lock()
	m.mountsCacheTime = time.Time{}
	m.mountsCacheMu.Unlock()
	log.Printf("SD card partition %s will be ignored until removal after format attempt", devicePath)

	filesystemType, err := formatDevice(formatCtx, devicePath, label)
	if err != nil {
		return err
	}

	log.Printf("Repartitioned SD card and formatted %s as %s", devicePath, filesystemType)
	return nil
}

func (m *Monitor) ignoreDeviceUntilRemoval(devicePath string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastDevice = ""
	m.ignoredDevice = devicePath
}

func formatFAT32Device(ctx context.Context, devicePath, label string) (err error) {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("format SD card: %w", err)
	}

	device, err := file.OpenFromPath(devicePath, false)
	if err != nil {
		return fmt.Errorf("format SD card: open device: %w", err)
	}
	defer func() {
		if closeErr := device.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("format SD card: close device: %w", closeErr)
		}
	}()

	size, err := storageSize(device)
	if err != nil {
		return fmt.Errorf("format SD card: determine device size: %w", err)
	}

	if _, err := fat32.Create(device, size, 0, int64(fat32.SectorSize512), label, false); err != nil {
		return fmt.Errorf("format SD card as FAT32: %w", err)
	}
	if err := syncStorage(device); err != nil {
		return fmt.Errorf("format SD card: sync device: %w", err)
	}

	return nil
}

func formatDevice(ctx context.Context, devicePath, label string) (string, error) {
	target, err := formatTargetForDevicePath(devicePath)
	if err != nil {
		return "", err
	}

	size, err := deviceSizeForFormat(ctx, target.diskPath)
	if err != nil {
		return "", err
	}

	if size > sdxcExFATThreshold {
		formatter, ok := findFormatter("mkfs.exfat", "mkexfatfs")
		if !ok {
			return "", fmt.Errorf("format SD card: exFAT formatter not found; cards larger than 32 GiB need exFAT for reliable camera/device compatibility")
		}
		if err := ensureNoMountedPartitions(target.diskPath); err != nil {
			return "", err
		}
		if err := repartitionDevice(ctx, target.diskPath, mbr.NTFS); err != nil {
			return "", err
		}
		if err := waitForPartitionDevice(ctx, target.partitionPath); err != nil {
			return "", err
		}
		if _, err := prepareDeviceForFormat(ctx, target.partitionPath); err != nil {
			return "", err
		}
		if err := runMkfsExFAT(ctx, formatter, label, target.partitionPath); err != nil {
			return "", fmt.Errorf("format SD card as exFAT: %w", err)
		}
		return "exFAT", nil
	}

	if err := ensureNoMountedPartitions(target.diskPath); err != nil {
		return "", err
	}
	if err := repartitionDevice(ctx, target.diskPath, mbr.Fat32LBA); err != nil {
		return "", err
	}
	if err := waitForPartitionDevice(ctx, target.partitionPath); err != nil {
		return "", err
	}
	if _, err := prepareDeviceForFormat(ctx, target.partitionPath); err != nil {
		return "", err
	}

	if formatter, ok := findFormatter("mkfs.vfat", "mkfs.fat"); ok {
		if err := runMkfs(ctx, formatter, fat32Args(label, target.partitionPath)); err != nil {
			return "", fmt.Errorf("format SD card as FAT32: %w", err)
		}
		return "FAT32", nil
	}

	if err := formatFAT32Device(ctx, target.partitionPath, label); err != nil {
		return "", err
	}
	return "FAT32", nil
}

type formatTarget struct {
	diskPath      string
	partitionPath string
}

func formatTargetForDevicePath(devicePath string) (formatTarget, error) {
	if !IsSupportedDevicePath(devicePath) {
		return formatTarget{}, fmt.Errorf("unsupported SD card device path: %s", devicePath)
	}

	baseName := filepath.Base(devicePath)
	diskName, partitionName := extractDiskAndPartitionName(baseName)
	if partitionName != baseName {
		return formatTarget{}, fmt.Errorf("unsupported SD card device path: %s", devicePath)
	}

	return formatTarget{
		diskPath:      filepath.Join(filepath.Dir(devicePath), diskName),
		partitionPath: filepath.Join(filepath.Dir(devicePath), firstPartitionName(diskName)),
	}, nil
}

func firstPartitionName(diskName string) string {
	if strings.HasPrefix(diskName, "mmcblk") || strings.HasPrefix(diskName, "nvme") {
		return diskName + "p1"
	}
	return diskName + "1"
}

func ensureNoMountedPartitions(diskPath string) error {
	mounts, err := os.ReadFile("/proc/mounts")
	if err != nil {
		return fmt.Errorf("format SD card: read mounts: %w", err)
	}

	diskName := filepath.Base(diskPath)
	for _, line := range strings.Split(string(mounts), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		deviceName := filepath.Base(fields[0])
		if isPartitionName(diskName, deviceName) {
			return fmt.Errorf("format SD card: partition %s is still mounted at %s", fields[0], fields[1])
		}
	}
	return nil
}

func repartitionDevice(ctx context.Context, diskPath string, partitionType mbr.Type) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("partition SD card: %w", err)
	}

	disk, err := diskfs.Open(diskPath, diskfs.WithOpenMode(diskfs.ReadWriteExclusive), diskfs.WithSectorSize(diskfs.SectorSize512))
	if err != nil {
		return fmt.Errorf("partition SD card: open disk: %w", err)
	}
	defer disk.Backend.Close()

	osFile, err := disk.Backend.Sys()
	if err != nil {
		return fmt.Errorf("partition SD card: access disk file: %w", err)
	}
	if err := wipeFilesystemSignatures(osFile, disk.Size); err != nil {
		return err
	}
	if err := osFile.Sync(); err != nil {
		return fmt.Errorf("partition SD card: sync wiped signatures: %w", err)
	}

	table, err := singlePartitionTable(disk.Size, partitionType)
	if err != nil {
		return err
	}
	if err := disk.Partition(table); err != nil {
		return fmt.Errorf("partition SD card: write partition table: %w", err)
	}
	if err := osFile.Sync(); err != nil {
		return fmt.Errorf("partition SD card: sync partition table: %w", err)
	}
	flushBlockDeviceBuffers(osFile)
	return nil
}

func singlePartitionTable(diskSize int64, partitionType mbr.Type) (*mbr.Table, error) {
	totalSectors := diskSize / partitionSectorSize
	if totalSectors <= int64(partitionStartSector) {
		return nil, fmt.Errorf("partition SD card: disk too small for aligned partition")
	}

	partitionSectors := totalSectors - int64(partitionStartSector)
	if partitionSectors > math.MaxUint32 {
		return nil, fmt.Errorf("partition SD card: disk too large for MBR partition table")
	}

	return &mbr.Table{
		LogicalSectorSize:  int(partitionSectorSize),
		PhysicalSectorSize: int(partitionSectorSize),
		Partitions: []*mbr.Partition{
			{
				Index: 1,
				Type:  partitionType,
				Start: partitionStartSector,
				Size:  uint32(partitionSectors),
			},
		},
	}, nil
}

func waitForPartitionDevice(ctx context.Context, partitionPath string) error {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()

	for {
		if _, err := os.Stat(partitionPath); err == nil {
			return nil
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("format SD card: check partition device: %w", err)
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("format SD card: wait for partition device: %w", ctx.Err())
		case <-deadline.C:
			return fmt.Errorf("format SD card: partition device %s did not appear", partitionPath)
		case <-ticker.C:
		}
	}
}

func deviceSizeForFormat(ctx context.Context, devicePath string) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, fmt.Errorf("format SD card: %w", err)
	}

	f, err := os.OpenFile(devicePath, os.O_RDONLY, 0)
	if err != nil {
		return 0, fmt.Errorf("format SD card: open device: %w", err)
	}
	defer f.Close()

	size, err := storageSizeFromOSFile(f)
	if err != nil {
		return 0, fmt.Errorf("format SD card: determine device size: %w", err)
	}
	return size, nil
}

func prepareDeviceForFormat(ctx context.Context, devicePath string) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, fmt.Errorf("format SD card: %w", err)
	}

	f, err := os.OpenFile(devicePath, os.O_RDWR|os.O_EXCL, 0)
	if err != nil {
		return 0, fmt.Errorf("format SD card: open device: %w", err)
	}
	defer f.Close()

	size, err := storageSizeFromOSFile(f)
	if err != nil {
		return 0, fmt.Errorf("format SD card: determine device size: %w", err)
	}
	if err := wipeFilesystemSignatures(f, size); err != nil {
		return 0, err
	}
	if err := f.Sync(); err != nil {
		return 0, fmt.Errorf("format SD card: sync wiped signatures: %w", err)
	}
	flushBlockDeviceBuffers(f)

	return size, nil
}

func wipeFilesystemSignatures(f *os.File, size int64) error {
	if size <= 0 {
		return fmt.Errorf("format SD card: invalid device size %d", size)
	}

	zeroLen := signatureWipeBytes
	if size < zeroLen {
		zeroLen = size
	}
	zeros := make([]byte, zeroLen)

	if _, err := f.WriteAt(zeros, 0); err != nil {
		return fmt.Errorf("format SD card: wipe start of device: %w", err)
	}

	endOffset := size - zeroLen
	if endOffset > 0 {
		if _, err := f.WriteAt(zeros, endOffset); err != nil {
			return fmt.Errorf("format SD card: wipe end of device: %w", err)
		}
	}

	return nil
}

func findFormatter(names ...string) (string, bool) {
	searchDirs := []string{"/usr/bin", "/usr/local/bin", "/bin", "/user"}

	for _, name := range names {
		path, err := exec.LookPath(name)
		if err == nil {
			return path, true
		}
		if filepath.IsAbs(name) {
			continue
		}
		for _, dir := range searchDirs {
			candidate := filepath.Join(dir, name)
			info, err := os.Stat(candidate)
			if err == nil && !info.IsDir() && info.Mode()&0111 != 0 {
				return candidate, true
			}
		}
	}
	return "", false
}

func exfatArgs(label, devicePath string) []string {
	args := []string{}
	if label != "" {
		args = append(args, "-n", label)
	}
	return append(args, devicePath)
}

func exfatArgsWithLongLabel(label, devicePath string) []string {
	args := []string{}
	if label != "" {
		args = append(args, "-L", label)
	}
	return append(args, devicePath)
}

func fat32Args(label, devicePath string) []string {
	args := []string{"-F", "32", "-I"}
	if label != "" {
		args = append(args, "-n", label)
	}
	return append(args, devicePath)
}

func runMkfsExFAT(ctx context.Context, formatter, label, devicePath string) error {
	err := runMkfs(ctx, formatter, exfatArgs(label, devicePath))
	if err == nil || label == "" {
		return err
	}

	errMsg := strings.ToLower(err.Error())
	if strings.Contains(errMsg, "invalid option") || strings.Contains(errMsg, "unknown option") {
		return runMkfs(ctx, formatter, exfatArgsWithLongLabel(label, devicePath))
	}
	return err
}

func runMkfs(ctx context.Context, formatter string, args []string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	// #nosec G204 -- formatter is discovered locally via PATH, and args are fixed except for a validated label/device path.
	cmd := exec.CommandContext(ctx, formatter, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		detail := strings.TrimSpace(string(output))
		if detail != "" {
			return fmt.Errorf("%w: %s", err, detail)
		}
		return err
	}
	return nil
}

func storageSize(device backend.Storage) (int64, error) {
	info, err := device.Stat()
	if err != nil {
		return 0, err
	}
	if info.Size() > 0 {
		return info.Size(), nil
	}

	osFile, err := device.Sys()
	if err != nil {
		return 0, err
	}

	return storageSizeFromOSFile(osFile)
}

func storageSizeFromOSFile(osFile *os.File) (int64, error) {
	info, err := osFile.Stat()
	if err != nil {
		return 0, err
	}
	if info.Size() > 0 {
		return info.Size(), nil
	}

	var size uint64
	// #nosec G103 -- BLKGETSIZE64 requires passing a pointer for the kernel to fill.
	if _, _, errno := unix.Syscall(
		unix.SYS_IOCTL,
		osFile.Fd(),
		uintptr(unix.BLKGETSIZE64),
		uintptr(unsafe.Pointer(&size)),
	); errno != 0 {
		return 0, errno
	}
	if size > uint64(math.MaxInt64) {
		return 0, errors.New("device size exceeds supported range")
	}

	return int64(size), nil
}

func syncStorage(device backend.Storage) error {
	osFile, err := device.Sys()
	if err != nil {
		return err
	}
	return osFile.Sync()
}

func flushBlockDeviceBuffers(osFile *os.File) {
	if _, _, errno := unix.Syscall(unix.SYS_IOCTL, osFile.Fd(), uintptr(unix.BLKFLSBUF), 0); errno != 0 {
		if errno == unix.ENOTTY || errno == unix.ENOTBLK {
			return
		}
		log.Printf("Warning: Failed to flush block device buffers after format preparation: %v", errno)
	}
}
