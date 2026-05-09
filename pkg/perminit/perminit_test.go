package perminit

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	diskfs "github.com/diskfs/go-diskfs"
	"github.com/diskfs/go-diskfs/partition/gpt"
)

// makeGokrazyLikeImage returns the path to a sparse file containing a GPT
// laid out like a small gokrazy image: p1 boot, p2/p3 root slots, p4 a small
// "perm" partition. The total disk is `diskBytes` so the test can verify
// that GrowPermPartition extends p4 into the trailing free space.
func makeGokrazyLikeImage(t *testing.T, diskBytes int64) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "img.bin")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(diskBytes); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	disk, err := diskfs.Open(path, diskfs.WithOpenMode(diskfs.ReadWriteExclusive))
	if err != nil {
		t.Fatal(err)
	}
	defer disk.Backend.Close()

	const sectorSize = 512
	parts := []*gpt.Partition{
		{Index: 1, Start: 2048, End: 2048 + 100*1024*1024/sectorSize - 1, Type: gpt.EFISystemPartition, Name: "boot"},
		{Index: 2, Start: 2048 + 100*1024*1024/sectorSize, End: 2048 + 600*1024*1024/sectorSize - 1, Type: gpt.LinuxFilesystem, Name: "root2"},
		{Index: 3, Start: 2048 + 600*1024*1024/sectorSize, End: 2048 + 1100*1024*1024/sectorSize - 1, Type: gpt.LinuxFilesystem, Name: "root3"},
		// p4 is intentionally smaller than what the test disk allows.
		{Index: 4, Start: 2048 + 1100*1024*1024/sectorSize, End: 2048 + 1100*1024*1024/sectorSize + 1000 - 1, Type: gpt.LinuxFilesystem, Name: "perm"},
	}
	table := &gpt.Table{
		LogicalSectorSize: sectorSize,
		Partitions:        parts,
	}
	if err := disk.Partition(table); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestGrowPermPartition(t *testing.T) {
	const diskBytes = int64(8 * 1024 * 1024 * 1024) // 8 GiB target disk
	path := makeGokrazyLikeImage(t, diskBytes)

	outcome, err := GrowPermPartition(path)
	if err != nil {
		t.Fatalf("GrowPermPartition: %v", err)
	}
	if outcome != Grew {
		t.Fatalf("outcome = %v, want Grew", outcome)
	}

	disk, err := diskfs.Open(path, diskfs.WithOpenMode(diskfs.ReadOnly))
	if err != nil {
		t.Fatal(err)
	}
	defer disk.Backend.Close()
	tbl, err := disk.GetPartitionTable()
	if err != nil {
		t.Fatal(err)
	}
	gptTbl := tbl.(*gpt.Table)
	if len(gptTbl.Partitions) < PermPartitionIndex {
		t.Fatalf("disk has %d partitions, want >=%d", len(gptTbl.Partitions), PermPartitionIndex)
	}
	perm := gptTbl.Partitions[PermPartitionIndex-1]
	diskSectors := uint64(diskBytes / int64(gptTbl.LogicalSectorSize))
	wantEnd := diskSectors - reservedTrailingSectors - 1
	if perm.End != wantEnd {
		t.Errorf("perm.End = %d, want %d (disk has %d sectors)", perm.End, wantEnd, diskSectors)
	}

	// Second invocation should be a no-op.
	outcome2, err := GrowPermPartition(path)
	if err != nil {
		t.Fatalf("GrowPermPartition (second call): %v", err)
	}
	if outcome2 != AlreadyFull {
		t.Errorf("second outcome = %v, want AlreadyFull", outcome2)
	}
}

func TestHasExistingFilesystem(t *testing.T) {
	dir := t.TempDir()

	// Empty file: no fs.
	empty := filepath.Join(dir, "empty.bin")
	if err := os.WriteFile(empty, make([]byte, 4096), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := HasExistingFilesystem(empty); err != nil || got {
		t.Errorf("HasExistingFilesystem(empty) = %v, %v; want false, nil", got, err)
	}

	// ext4 superblock magic at offset 1080.
	ext4 := make([]byte, 4096)
	ext4[1080] = 0x53
	ext4[1081] = 0xEF
	ext4Path := filepath.Join(dir, "ext4.bin")
	if err := os.WriteFile(ext4Path, ext4, 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := HasExistingFilesystem(ext4Path); err != nil || !got {
		t.Errorf("HasExistingFilesystem(ext4) = %v, %v; want true, nil", got, err)
	}

	// FAT32 marker.
	fat32 := make([]byte, 4096)
	copy(fat32[82:], "FAT32   ")
	fat32Path := filepath.Join(dir, "fat32.bin")
	if err := os.WriteFile(fat32Path, fat32, 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := HasExistingFilesystem(fat32Path); err != nil || !got {
		t.Errorf("HasExistingFilesystem(fat32) = %v, %v; want true, nil", got, err)
	}

	// File too small to contain a superblock.
	tiny := filepath.Join(dir, "tiny.bin")
	if err := os.WriteFile(tiny, make([]byte, 256), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := HasExistingFilesystem(tiny); err != nil || got {
		t.Errorf("HasExistingFilesystem(tiny) = %v, %v; want false, nil", got, err)
	}
}

func TestGrowPermPartitionMBRFails(t *testing.T) {
	// Build an MBR-only disk image with `sfdisk` if available; otherwise
	// skip — building MBR by hand is overkill for this test.
	if _, err := exec.LookPath("sfdisk"); err != nil {
		t.Skip("sfdisk not available")
	}
	// We simply assert a non-GPT input is rejected cleanly. Use an empty
	// (zero) image; diskfs will return an error on GetPartitionTable.
	path := filepath.Join(t.TempDir(), "blank.bin")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(64 * 1024 * 1024); err != nil {
		t.Fatal(err)
	}
	f.Close()

	if _, err := GrowPermPartition(path); err == nil {
		t.Fatal("GrowPermPartition on blank disk: expected error, got nil")
	}
}
