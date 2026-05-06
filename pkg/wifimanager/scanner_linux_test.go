package wifimanager

import (
	"testing"
	"time"

	"github.com/mdlayher/genetlink"
	"github.com/mdlayher/netlink"
	"github.com/mdlayher/netlink/nlenc"
	"github.com/mdlayher/wifi"
	"golang.org/x/sys/unix"
)

func TestEncodeTriggerScanDataUsesWildcardSSID(t *testing.T) {
	data, err := encodeTriggerScanData(&wifi.Interface{Index: 7})
	if err != nil {
		t.Fatalf("encodeTriggerScanData returned error: %v", err)
	}

	ad, err := netlink.NewAttributeDecoder(data)
	if err != nil {
		t.Fatalf("NewAttributeDecoder returned error: %v", err)
	}

	var (
		ifIndex   uint32
		scanSSIDs [][]byte
	)

	for ad.Next() {
		switch ad.Type() {
		case unix.NL80211_ATTR_IFINDEX:
			ifIndex = ad.Uint32()
		case unix.NL80211_ATTR_SCAN_SSIDS:
			if ad.TypeFlags()&netlink.Nested == 0 {
				t.Fatal("NL80211_ATTR_SCAN_SSIDS is not marked nested")
			}
			ad.Nested(func(nad *netlink.AttributeDecoder) error {
				for nad.Next() {
					scanSSIDs = append(scanSSIDs, nad.Bytes())
				}
				return nil
			})
		}
	}
	if err := ad.Err(); err != nil {
		t.Fatalf("decode trigger scan data: %v", err)
	}

	if ifIndex != 7 {
		t.Fatalf("ifindex = %d, want 7", ifIndex)
	}
	if len(scanSSIDs) != 1 {
		t.Fatalf("got %d scan SSID attributes, want 1", len(scanSSIDs))
	}
	if len(scanSSIDs[0]) != 0 {
		t.Fatalf("scan SSID attribute = %v, want zero-length wildcard", scanSSIDs[0])
	}
}

func TestParseScanInformationElements(t *testing.T) {
	ies := []byte{
		0, 4, 'T', 'e', 's', 't',
		48, 2, 0x01, 0x00,
	}

	ssid, encrypted := parseScanInformationElements(ies)
	if ssid != "Test" {
		t.Fatalf("SSID = %q, want Test", ssid)
	}
	if !encrypted {
		t.Fatal("encrypted = false, want true")
	}
}

func TestParseScanInformationElementsDetectsWPA(t *testing.T) {
	ies := []byte{
		0, 3, 'O', 'l', 'd',
		221, 4, 0x00, 0x50, 0xf2, 0x01,
	}

	ssid, encrypted := parseScanInformationElements(ies)
	if ssid != "Old" {
		t.Fatalf("SSID = %q, want Old", ssid)
	}
	if !encrypted {
		t.Fatal("encrypted = false, want true")
	}
}

func TestParseNL80211ScanMessagesUsesBeaconIEs(t *testing.T) {
	attrs, err := netlink.MarshalAttributes([]netlink.Attribute{
		{Type: unix.NL80211_BSS_BSSID, Data: []byte{0x00, 0x11, 0x22, 0x33, 0x44, 0x55}},
		{Type: unix.NL80211_BSS_FREQUENCY, Data: nlenc.Uint32Bytes(2412)},
		{Type: unix.NL80211_BSS_BEACON_INTERVAL, Data: nlenc.Uint16Bytes(100)},
		{Type: unix.NL80211_BSS_SEEN_MS_AGO, Data: nlenc.Uint32Bytes(1234)},
		{Type: unix.NL80211_BSS_SIGNAL_MBM, Data: nlenc.Int32Bytes(-4200)},
		{Type: unix.NL80211_BSS_CAPABILITY, Data: nlenc.Uint16Bytes(capabilityPrivacy)},
		{Type: unix.NL80211_BSS_INFORMATION_ELEMENTS, Data: []byte{0, 0}},
		{Type: unix.NL80211_BSS_BEACON_IES, Data: []byte{0, 6, 'B', 'e', 'a', 'c', 'o', 'n'}},
	})
	if err != nil {
		t.Fatalf("marshal BSS attrs: %v", err)
	}

	data, err := netlink.MarshalAttributes([]netlink.Attribute{
		{Type: unix.NL80211_ATTR_BSS, Data: attrs},
	})
	if err != nil {
		t.Fatalf("marshal message attrs: %v", err)
	}

	aps, err := parseNL80211ScanMessages([]genetlink.Message{{Data: data}})
	if err != nil {
		t.Fatalf("parseNL80211ScanMessages returned error: %v", err)
	}
	if len(aps) != 1 {
		t.Fatalf("got %d APs, want 1", len(aps))
	}

	ap := aps[0]
	if ap.SSID != "Beacon" {
		t.Fatalf("SSID = %q, want Beacon", ap.SSID)
	}
	if ap.Frequency != 2412 {
		t.Fatalf("Frequency = %d, want 2412", ap.Frequency)
	}
	if ap.Signal != -4200 {
		t.Fatalf("Signal = %d, want -4200", ap.Signal)
	}
	if ap.BeaconInterval != 102400*time.Microsecond {
		t.Fatalf("BeaconInterval = %v, want 102400us", ap.BeaconInterval)
	}
	if ap.LastSeen != 1234*time.Millisecond {
		t.Fatalf("LastSeen = %v, want 1234ms", ap.LastSeen)
	}
	if len(ap.RSN.PairwiseCiphers) == 0 {
		t.Fatal("expected privacy capability to mark AP encrypted")
	}
}
