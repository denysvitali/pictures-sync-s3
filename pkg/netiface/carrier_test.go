package netiface

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHasCarrier(t *testing.T) {
	tmpDir := t.TempDir()
	oldRoot := CarrierRoot
	CarrierRoot = tmpDir
	t.Cleanup(func() {
		CarrierRoot = oldRoot
	})

	tests := []struct {
		name    string
		iface   string
		content *string
		want    bool
		wantErr bool
	}{
		{
			name:    "carrier present",
			iface:   "eth0",
			content: strPtr("1\n"),
			want:    true,
		},
		{
			name:    "carrier absent",
			iface:   "eth1",
			content: strPtr("0\n"),
			want:    false,
		},
		{
			name:  "interface missing",
			iface: "eth2",
			want:  false,
		},
		{
			name:    "unexpected value",
			iface:   "eth3",
			content: strPtr("unknown\n"),
			wantErr: true,
		},
		{
			name:  "empty interface",
			iface: "",
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.content != nil {
				dir := filepath.Join(tmpDir, tt.iface)
				if err := os.MkdirAll(dir, 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "carrier"), []byte(*tt.content), 0644); err != nil {
					t.Fatal(err)
				}
			}

			got, err := HasCarrier(tt.iface)
			if (err != nil) != tt.wantErr {
				t.Fatalf("HasCarrier() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("HasCarrier() = %v, want %v", got, tt.want)
			}
		})
	}
}

func strPtr(s string) *string {
	return &s
}
