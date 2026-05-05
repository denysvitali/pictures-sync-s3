package handlers

import "testing"

func TestParseRootPartition(t *testing.T) {
	tests := []struct {
		name string
		root string
		want int
	}{
		{
			name: "mmc partition",
			root: "/dev/mmcblk0p2",
			want: 2,
		},
		{
			name: "mbr partuuid",
			root: "PARTUUID=2e18c40c-03",
			want: 3,
		},
		{
			name: "gpt partuuid partnroff root 2",
			root: "PARTUUID=9f1b0c66-e0f3-4d7e-a45f-4c1d6d6b69f8/PARTNROFF=1",
			want: 2,
		},
		{
			name: "gpt partuuid partnroff root 3",
			root: "PARTUUID=9f1b0c66-e0f3-4d7e-a45f-4c1d6d6b69f8/PARTNROFF=2",
			want: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseRootPartition(tt.root); got != tt.want {
				t.Fatalf("parseRootPartition(%q) = %d, want %d", tt.root, got, tt.want)
			}
		})
	}
}

func TestIsKnownInstalledVersion(t *testing.T) {
	tests := []struct {
		version string
		want    bool
	}{
		{"master-bf99d1223bf685669c008457d5515d9453f06835", true},
		{"v1.2.3", true},
		{"dev", false},
		{"", false},
		{"v0.0.0-00010101000000-000000000000", false},
	}

	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			if got := isKnownInstalledVersion(tt.version); got != tt.want {
				t.Fatalf("isKnownInstalledVersion(%q) = %t, want %t", tt.version, got, tt.want)
			}
		})
	}
}
