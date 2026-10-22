package wifiregion

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestNormalize(t *testing.T) {
	for in, want := range map[string]string{"de": "DE", " us ": "US"} {
		if got, err := Normalize(in); err != nil || got != want {
			t.Errorf("Normalize(%q) = %q, %v", in, got, err)
		}
	}
	for _, in := range []string{"", "D", "DEU", "D1", "XX", "é"} {
		if _, err := Normalize(in); err == nil {
			t.Errorf("Normalize(%q) should fail", in)
		}
	}
}

func TestReadWrite(t *testing.T) {
	File = filepath.Join(t.TempDir(), "sub", "wifi-region")
	if got := Read(); got != "" {
		t.Fatalf("Read() = %q before write", got)
	}
	if err := Write("it"); err != nil {
		t.Fatal(err)
	}
	if got := Read(); got != "IT" {
		t.Fatalf("Read() = %q", got)
	}
	if err := Write("nope"); err == nil {
		t.Fatal("expected error")
	}
}

func TestSetClientConfigRegion(t *testing.T) {
	cases := []struct {
		name, in string
		force    bool
		changed  bool
		region   string
	}{
		{"missing", `{"ssid":"a","psk":"b"}`, false, true, "DE"},
		{"world", `{"ssid":"a","region":"00"}`, false, true, "DE"},
		{"kept", `{"ssid":"a","region":"IT"}`, false, false, "IT"},
		{"forced", `{"ssid":"a","region":"IT"}`, true, true, "DE"},
		{"forced same", `{"ssid":"a","region":"DE"}`, true, false, "DE"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "wifi.json")
			if err := os.WriteFile(path, []byte(tc.in), 0o600); err != nil {
				t.Fatal(err)
			}
			changed, err := SetClientConfigRegion(path, "DE", tc.force)
			if err != nil || changed != tc.changed {
				t.Fatalf("changed=%v err=%v", changed, err)
			}
			var got map[string]any
			data, _ := os.ReadFile(path)
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatal(err)
			}
			if got["region"] != tc.region || got["ssid"] != "a" {
				t.Fatalf("got %v", got)
			}
		})
	}
	if changed, err := SetClientConfigRegion(filepath.Join(t.TempDir(), "none.json"), "DE", true); changed || err != nil {
		t.Fatalf("missing file: changed=%v err=%v", changed, err)
	}
}
