package sdmonitor

import (
	"fmt"
	"log"
	"strings"

	"golang.org/x/sys/unix"
)

// mount mounts the device (initially read-write for card ID setup)
func (m *Monitor) mount(device string) error {
	// Check if already mounted using cached data
	mounts, err := m.getCachedMounts()
	if err == nil {
		if strings.Contains(mounts, device+" "+m.mountPath) {
			return nil // Already mounted
		}
	}

	// Unmount if anything is currently mounted at our path
	if err := unix.Unmount(m.mountPath, 0); err != nil {
		// Only log if it's not "not mounted" error
		if err != unix.EINVAL {
			log.Printf("Warning: Failed to unmount existing mount at %s: %v", m.mountPath, err)
			// Try to continue anyway, as the mount might succeed
		}
	}

	// Mount read-write initially to allow writing card ID
	// We'll remount read-only after card ID is written
	fstypes := []string{"vfat", "exfat", "ext4", "ntfs"}

	for _, fstype := range fstypes {
		err := unix.Mount(device, m.mountPath, fstype, 0, "")
		if err == nil {
			log.Printf("Mounted %s as %s (rw) at %s", device, fstype, m.mountPath)
			return nil
		}
	}

	// Try auto-detect (empty fstype)
	err = unix.Mount(device, m.mountPath, "", 0, "")
	if err != nil {
		return fmt.Errorf("failed to mount device: %w", err)
	}

	log.Printf("Mounted %s (rw) at %s", device, m.mountPath)
	return nil
}

// unmount unmounts the current device
func (m *Monitor) unmount() error {
	if err := unix.Unmount(m.mountPath, 0); err != nil {
		log.Printf("Failed to unmount %s: %v", m.mountPath, err)
		return err
	}
	log.Printf("Unmounted %s", m.mountPath)
	return nil
}

// RemountReadOnly remounts the filesystem as read-only
func (m *Monitor) RemountReadOnly() error {
	// Remount with MS_REMOUNT | MS_RDONLY flags
	err := unix.Mount("", m.mountPath, "", unix.MS_REMOUNT|unix.MS_RDONLY, "")
	if err != nil {
		return fmt.Errorf("failed to remount read-only: %w", err)
	}
	log.Printf("Remounted %s as read-only", m.mountPath)
	return nil
}
