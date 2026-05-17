package perminit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBootBlockDevice(t *testing.T) {
	tests := []struct {
		name    string
		cmdline string
		want    string
		wantErr bool
	}{
		{
			name:    "raspberry pi mmcblk0p2",
			cmdline: "console=tty1 root=/dev/mmcblk0p2 init=/gokrazy/init rootwait\n",
			want:    "/dev/mmcblk0",
		},
		{
			name:    "loopback test",
			cmdline: "root=/dev/loop0p3 quiet",
			want:    "/dev/loop0",
		},
		{
			name:    "nvme",
			cmdline: "root=/dev/nvme0n1p2",
			want:    "/dev/nvme0n1",
		},
		{
			name:    "sda (no trailing p)",
			cmdline: "root=/dev/sda2",
			want:    "/dev/sda",
		},
		{
			name:    "ubd0 (UML)",
			cmdline: "ubd0=/dev/loop0p2",
			want:    "/dev/loop0",
		},
		{
			name:    "unsupported partition number 4",
			cmdline: "root=/dev/mmcblk0p4",
			wantErr: true,
		},
		{
			name:    "no match",
			cmdline: "console=ttyS0",
			wantErr: true,
		},
		{
			name:    "PARTUUID with PARTNROFF",
			cmdline: "console=tty1 root=PARTUUID=60c24cc1-f3f9-427a-8199-89a6807c0001/PARTNROFF=1 init=/gokrazy/init rootwait",
			want:    "$DEV/mmcblk0",
		},
		{
			name:    "PARTUUID relative symlink to nvme",
			cmdline: "root=PARTUUID=8bc8f0e6-5655-4937-93cb-f2e2878b48a2",
			want:    "$DEV/nvme0n1",
		},
	}

	dir := t.TempDir()
	origCmdline := CmdlineFile
	origPartUUIDDir := PartUUIDDir
	origDeviceDir := DeviceDir
	deviceRoot := filepath.Join(dir, "dev")
	PartUUIDDir = filepath.Join(deviceRoot, "disk", "by-partuuid")
	DeviceDir = deviceRoot
	if err := os.MkdirAll(PartUUIDDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, device := range []string{"mmcblk0p1", "nvme0n1p2"} {
		if err := os.WriteFile(filepath.Join(deviceRoot, device), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink("../../mmcblk0p1", filepath.Join(PartUUIDDir, "60c24cc1-f3f9-427a-8199-89a6807c0001")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../../nvme0n1p2", filepath.Join(PartUUIDDir, "8bc8f0e6-5655-4937-93cb-f2e2878b48a2")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		CmdlineFile = origCmdline
		PartUUIDDir = origPartUUIDDir
		DeviceDir = origDeviceDir
	})

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			want := strings.ReplaceAll(tc.want, "$DEV", deviceRoot)
			path := filepath.Join(dir, "cmdline-"+tc.name)
			if err := os.WriteFile(path, []byte(tc.cmdline), 0o600); err != nil {
				t.Fatal(err)
			}
			CmdlineFile = path
			got, err := BootBlockDevice()
			if tc.wantErr {
				if err == nil {
					t.Fatalf("BootBlockDevice() = %q, want error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("BootBlockDevice() error: %v", err)
			}
			if got != want {
				t.Errorf("BootBlockDevice() = %q, want %q", got, want)
			}
		})
	}
}

func TestPartitionDevice(t *testing.T) {
	tests := []struct {
		blockDev string
		n        int
		want     string
	}{
		{"/dev/mmcblk0", 4, "/dev/mmcblk0p4"},
		{"/dev/mmcblk0p", 4, "/dev/mmcblk0p4"},
		{"/dev/loop0", 1, "/dev/loop0p1"},
		{"/dev/nvme0n1", 2, "/dev/nvme0n1p2"},
		{"/dev/sda", 4, "/dev/sda4"},
	}
	for _, tc := range tests {
		got := PartitionDevice(tc.blockDev, tc.n)
		if got != tc.want {
			t.Errorf("PartitionDevice(%q, %d) = %q, want %q", tc.blockDev, tc.n, got, tc.want)
		}
	}
}
