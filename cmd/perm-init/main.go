// Command perm-init runs once per boot. If /perm is not mounted as its own
// filesystem, it makes one step of progress (grow GPT partition 4 OR run
// mke2fs on it) and reboots, so that the next boot brings /perm one step
// closer to a usable state.
//
// Worst-case path on a freshly-flashed image:
//
//  1. Boot 1: partition 4 only spans the image (e.g. ~944 MiB of a 32 GiB
//     card) and has no filesystem. perm-init rewrites the GPT so partition 4
//     extends to the end of the disk, then reboots.
//  2. Boot 2: kernel re-reads the GPT and exposes the new partition size.
//     perm-init runs mke2fs and reboots.
//  3. Boot 3: gokrazy mounts /perm read-write; perm-init exits without
//     doing anything.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"

	"github.com/gokrazy/gokapi"
	"github.com/gokrazy/gokapi/ondeviceapi"

	"github.com/denysvitali/pictures-sync-s3/pkg/perminit"
)

const mke2fsPath = "/usr/local/bin/mke2fs"

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	if err := run(); err != nil {
		log.Printf("perm-init: %v", err)
	}
	// Exit 125 tells the gokrazy supervisor to treat this as a one-shot
	// service and not restart it.
	os.Exit(125)
}

func run() error {
	mounted, err := perminit.IsPermMounted()
	if err != nil {
		return fmt.Errorf("check /perm mount: %w", err)
	}
	if mounted {
		log.Println("/perm is mounted, nothing to do")
		return nil
	}

	blockDev, err := perminit.BootBlockDevice()
	if err != nil {
		return fmt.Errorf("locate boot block device: %w", err)
	}
	permDev := perminit.PartitionDevice(blockDev, perminit.PermPartitionIndex)
	log.Printf("/perm not mounted; block device=%s perm partition=%s", blockDev, permDev)

	hasFS, err := perminit.HasExistingFilesystem(permDev)
	if err != nil {
		return fmt.Errorf("probe %s for existing filesystem: %w", permDev, err)
	}

	outcome, err := perminit.GrowPermPartition(blockDev)
	if err != nil {
		return fmt.Errorf("grow perm partition: %w", err)
	}

	if outcome == perminit.Grew {
		log.Println("partition 4 grown to end of disk; rebooting so the kernel re-reads the GPT")
		return reboot()
	}

	if hasFS {
		// A filesystem is present but gokrazy did not mount it — could be a
		// transient fsck issue or an unsupported fstype. Refuse to wipe;
		// require manual recovery via breakglass.
		log.Printf("partition 4 already covers the disk and has an existing filesystem on %s; refusing to reformat to preserve data", permDev)
		return nil
	}

	log.Printf("partition 4 already covers the disk and has no filesystem; creating ext4 on %s", permDev)
	cmd := exec.Command(mke2fsPath, "-F", "-t", "ext4", "-L", "perm", permDev)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w", cmd.String(), err)
	}

	log.Println("filesystem created; rebooting to mount /perm")
	return reboot()
}

func reboot() error {
	cfg, err := gokapi.ConnectOnDevice()
	if err != nil {
		return fmt.Errorf("gokapi connect: %w", err)
	}
	cl := ondeviceapi.NewAPIClient(cfg)
	if _, err := cl.UpdateApi.Reboot(context.Background(), &ondeviceapi.UpdateApiRebootOpts{}); err != nil {
		return fmt.Errorf("reboot: %w", err)
	}
	return nil
}
