package sdmonitor

import (
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

const (
	// unmountSettleDelay is the wait time after unmounting to allow the kernel to complete cleanup.
	// This ensures all filesystem operations are flushed and the device is fully released before
	// proceeding with other operations.
	unmountSettleDelay = 100 * time.Millisecond
)

// mount mounts the device (initially read-write for card ID setup)
// This is NOT thread-safe - caller must hold m.mountMu
func (m *Monitor) mount(device string) error {
	log.Printf("Mount: Starting mount operation for device %s at %s", device, m.mountPath)

	// Check if already mounted using cached data
	mounts, err := m.getCachedMounts()
	if err == nil {
		if strings.Contains(mounts, device+" "+m.mountPath) {
			log.Printf("Mount: Device %s already mounted at %s", device, m.mountPath)
			return nil // Already mounted
		}
	}

	// Clean up stale mount point before attempting mount
	if err := m.cleanupMountPoint(); err != nil {
		log.Printf("Mount: Warning: Failed to cleanup mount point %s: %v", m.mountPath, err)
		// Continue anyway - we'll try to mount
	}

	// Mount read-write initially to allow writing card ID
	// We'll remount read-only after card ID is written
	fstypes := []string{"vfat", "exfat", "ext4", "ntfs"}

	var lastErr error
	for _, fstype := range fstypes {
		err := unix.Mount(device, m.mountPath, fstype, 0, "")
		if err == nil {
			log.Printf("Mount: Successfully mounted %s as %s (rw) at %s", device, fstype, m.mountPath)
			// Verify mount succeeded by checking if directory is accessible
			if !m.verifyMount() {
				log.Printf("Mount: ERROR: Mount verification failed for %s at %s", device, m.mountPath)
				m.forceUnmount() // Clean up failed mount
				return fmt.Errorf("mount verification failed: directory not accessible")
			}
			return nil
		}
		lastErr = err
		log.Printf("Mount: Failed to mount %s as %s: %v", device, fstype, err)
	}

	// Try auto-detect (empty fstype)
	err = unix.Mount(device, m.mountPath, "", 0, "")
	if err != nil {
		log.Printf("Mount: ERROR: All mount attempts failed for device %s. Last error: %v", device, lastErr)
		return fmt.Errorf("failed to mount device %s: %w", device, err)
	}

	log.Printf("Mount: Successfully mounted %s (auto-detected, rw) at %s", device, m.mountPath)

	// Verify mount succeeded
	if !m.verifyMount() {
		log.Printf("Mount: ERROR: Mount verification failed for %s at %s", device, m.mountPath)
		m.forceUnmount() // Clean up failed mount
		return fmt.Errorf("mount verification failed: directory not accessible")
	}

	return nil
}

// unmount unmounts the current device with proper error handling
// This is NOT thread-safe - caller must hold m.mountMu
func (m *Monitor) unmount() error {
	log.Printf("Unmount: Attempting to unmount %s", m.mountPath)

	// First attempt: normal unmount
	if err := unix.Unmount(m.mountPath, 0); err != nil {
		if errors.Is(err, unix.EINVAL) {
			// Not mounted, that's fine
			log.Printf("Unmount: %s is not mounted (EINVAL)", m.mountPath)
			return nil
		}

		if errors.Is(err, unix.EBUSY) {
			log.Printf("Unmount: Device busy at %s, attempting lazy unmount", m.mountPath)
			// Second attempt: lazy unmount (MNT_DETACH)
			// This detaches the filesystem from the hierarchy now, and cleans up
			// all references to it as soon as it's no longer busy
			if err := unix.Unmount(m.mountPath, unix.MNT_DETACH); err != nil {
				log.Printf("Unmount: ERROR: Lazy unmount failed for %s: %v", m.mountPath, err)
				return fmt.Errorf("lazy unmount failed: %w", err)
			}
			log.Printf("Unmount: Successfully performed lazy unmount of %s", m.mountPath)
			// Give the kernel a moment to complete the detach
			time.Sleep(unmountSettleDelay)
			return nil
		}

		log.Printf("Unmount: ERROR: Failed to unmount %s: %v", m.mountPath, err)
		return fmt.Errorf("unmount failed: %w", err)
	}

	log.Printf("Unmount: Successfully unmounted %s", m.mountPath)
	return nil
}

// forceUnmount performs an aggressive unmount with lazy detach
// This is NOT thread-safe - caller must hold m.mountMu
func (m *Monitor) forceUnmount() error {
	log.Printf("ForceUnmount: Forcing unmount of %s", m.mountPath)

	// Try lazy unmount immediately
	if err := unix.Unmount(m.mountPath, unix.MNT_DETACH); err != nil {
		if !errors.Is(err, unix.EINVAL) {
			log.Printf("ForceUnmount: ERROR: Failed to force unmount %s: %v", m.mountPath, err)
			return err
		}
		// EINVAL means not mounted, which is fine
		log.Printf("ForceUnmount: %s was not mounted", m.mountPath)
	} else {
		log.Printf("ForceUnmount: Successfully force unmounted %s", m.mountPath)
		time.Sleep(unmountSettleDelay)
	}

	return nil
}

// cleanupMountPoint ensures the mount point is clean before mounting
// This is NOT thread-safe - caller must hold m.mountMu
func (m *Monitor) cleanupMountPoint() error {
	log.Printf("CleanupMountPoint: Cleaning mount point %s", m.mountPath)

	// Force unmount anything that might be there
	if err := m.forceUnmount(); err != nil {
		log.Printf("CleanupMountPoint: Warning: Force unmount failed: %v", err)
	}

	// Ensure the directory exists and is accessible
	// #nosec G301 -- SD card mount point must be accessible by all processes on embedded device
	if err := os.MkdirAll(m.mountPath, 0755); err != nil {
		log.Printf("CleanupMountPoint: ERROR: Failed to create mount directory %s: %v", m.mountPath, err)
		return fmt.Errorf("failed to create mount directory: %w", err)
	}

	log.Printf("CleanupMountPoint: Mount point %s is ready", m.mountPath)
	return nil
}

// verifyMount checks if the mount was successful by testing directory access
func (m *Monitor) verifyMount() bool {
	log.Printf("VerifyMount: Verifying mount at %s", m.mountPath)

	// Try to read the directory
	entries, err := os.ReadDir(m.mountPath)
	if err != nil {
		log.Printf("VerifyMount: ERROR: Cannot read mount directory %s: %v", m.mountPath, err)
		return false
	}

	log.Printf("VerifyMount: Mount verified successfully (%d entries found)", len(entries))
	return true
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
