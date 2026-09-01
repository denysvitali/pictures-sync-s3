package ota

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/denysvitali/pictures-sync-s3/pkg/perminit"
	"golang.org/x/sys/unix"
)

const trybootStatePath = "/proc/device-tree/chosen/bootloader/tryboot"

// commitTrybootDelay gives the updated userspace time to start its essential
// services before the candidate kernel becomes the normal boot target.
const commitTrybootDelay = 30 * time.Second

func (m *Manager) commitTrybootWhenHealthy() {
	state, err := os.ReadFile(trybootStatePath)
	if err != nil || !bytes.Contains(state, []byte{1}) {
		return
	}
	time.Sleep(commitTrybootDelay)

	if err := promoteTrybootConfig(); err != nil {
		log.Printf("OTA tryboot: candidate kernel was not promoted: %v", err)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	installer, ok := m.installer().(GokrazyInstaller)
	if !ok {
		return
	}
	baseURL := normalizeUpdateBaseURL(installer.BaseURL)
	if baseURL == "" {
		baseURL = DefaultUpdateURL
	}
	target, err := NewUpdateTarget(ctx, baseURL, installer.httpClient(baseURL))
	if err != nil {
		log.Printf("OTA tryboot: connect to updater for root promotion: %v", err)
		return
	}
	if err := target.Switch(ctx); err != nil {
		log.Printf("OTA tryboot: promote tested root partition: %v", err)
		return
	}
	log.Printf("OTA tryboot: candidate kernel and root partition promoted after health delay")
}

func promoteTrybootConfig() error {
	blockDev, err := perminit.BootBlockDevice()
	if err != nil {
		return err
	}
	bootDev := perminit.PartitionDevice(blockDev, 1)
	mountDir, err := os.MkdirTemp("", "pictures-sync-boot-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(mountDir)
	if err := unix.Mount(bootDev, mountDir, "vfat", 0, ""); err != nil {
		return fmt.Errorf("mount boot filesystem: %w", err)
	}
	defer unix.Unmount(mountDir, 0)

	trybootPath := filepath.Join(mountDir, "tryboot.txt")
	config, err := os.ReadFile(trybootPath)
	if err != nil {
		return fmt.Errorf("read tryboot config: %w", err)
	}
	if !bytes.Contains(config, []byte("kernel=vmlinuz")) {
		return fmt.Errorf("tryboot config does not select candidate kernel")
	}
	tmpPath := filepath.Join(mountDir, "config.promote")
	if err := os.WriteFile(tmpPath, config, 0o644); err != nil {
		return fmt.Errorf("write promoted config: %w", err)
	}
	f, err := os.OpenFile(tmpPath, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, filepath.Join(mountDir, "config.txt")); err != nil {
		return fmt.Errorf("promote tryboot config: %w", err)
	}
	unix.Sync()
	return nil
}
