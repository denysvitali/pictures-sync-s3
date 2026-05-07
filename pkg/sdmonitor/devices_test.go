package sdmonitor

import "testing"

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
