package version

import "testing"

func TestIsUsableBuildVersion(t *testing.T) {
	tests := []struct {
		version string
		want    bool
	}{
		{"master-bf99d1223bf685669c008457d5515d9453f06835", true},
		{"v1.2.3", true},
		{"", false},
		{"(devel)", false},
		{"v0.0.0-00010101000000-000000000000", false},
	}

	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			if got := isUsableBuildVersion(tt.version); got != tt.want {
				t.Fatalf("isUsableBuildVersion(%q) = %t, want %t", tt.version, got, tt.want)
			}
		})
	}
}
