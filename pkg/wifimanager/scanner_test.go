package wifimanager

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/mdlayher/wifi"
)

type fakeScanClient struct {
	scanErr    error
	accessErr  error
	cachedAPs  []*wifi.BSS
	scannedAPs []*wifi.BSS
	calls      []string
}

func (c *fakeScanClient) AccessPoints(_ *wifi.Interface) ([]*wifi.BSS, error) {
	c.calls = append(c.calls, "access")
	if c.accessErr != nil {
		return nil, c.accessErr
	}
	if len(c.calls) > 0 && c.calls[0] == "scan" && c.scanErr == nil {
		return c.scannedAPs, nil
	}
	return c.cachedAPs, nil
}

func (c *fakeScanClient) Scan(_ context.Context, _ *wifi.Interface) error {
	c.calls = append(c.calls, "scan")
	return c.scanErr
}

func TestScanInterfaceTriggersScanBeforeReadingAccessPoints(t *testing.T) {
	oldDelay := scanWaitDelay
	oldReadAccessPoints := readAccessPoints
	oldTriggerScan := triggerScan
	scanWaitDelay = 0
	readAccessPoints = func(cl scanClient, intf *wifi.Interface) ([]*wifi.BSS, error) {
		return cl.AccessPoints(intf)
	}
	triggerScan = func(ctx context.Context, cl scanClient, intf *wifi.Interface) error {
		return cl.Scan(ctx, intf)
	}
	defer func() {
		scanWaitDelay = oldDelay
		readAccessPoints = oldReadAccessPoints
		triggerScan = oldTriggerScan
	}()

	client := &fakeScanClient{
		cachedAPs: []*wifi.BSS{
			{SSID: "Connected"},
		},
		scannedAPs: []*wifi.BSS{
			{SSID: "Connected"},
			{SSID: "Neighbor"},
		},
	}

	aps, err := scanInterface(client, &wifi.Interface{Name: "wlan0"})
	if err != nil {
		t.Fatalf("scanInterface returned error: %v", err)
	}

	if !reflect.DeepEqual(client.calls, []string{"scan", "access"}) {
		t.Fatalf("calls = %v, want [scan access]", client.calls)
	}
	if len(aps) != 2 {
		t.Fatalf("got %d APs, want 2", len(aps))
	}
	if aps[1].SSID != "Neighbor" {
		t.Fatalf("second AP SSID = %q, want Neighbor", aps[1].SSID)
	}
}

func TestScanInterfaceFallsBackToCachedAccessPoints(t *testing.T) {
	oldDelay := scanWaitDelay
	oldReadAccessPoints := readAccessPoints
	oldTriggerScan := triggerScan
	scanWaitDelay = 0
	readAccessPoints = func(cl scanClient, intf *wifi.Interface) ([]*wifi.BSS, error) {
		return cl.AccessPoints(intf)
	}
	triggerScan = func(ctx context.Context, cl scanClient, intf *wifi.Interface) error {
		return cl.Scan(ctx, intf)
	}
	defer func() {
		scanWaitDelay = oldDelay
		readAccessPoints = oldReadAccessPoints
		triggerScan = oldTriggerScan
	}()

	client := &fakeScanClient{
		scanErr: errors.New("scan busy"),
		cachedAPs: []*wifi.BSS{
			{SSID: "Connected"},
		},
	}

	aps, err := scanInterface(client, &wifi.Interface{Name: "wlan0"})
	if err != nil {
		t.Fatalf("scanInterface returned error: %v", err)
	}

	if !reflect.DeepEqual(client.calls, []string{"scan", "access"}) {
		t.Fatalf("calls = %v, want [scan access]", client.calls)
	}
	if len(aps) != 1 || aps[0].SSID != "Connected" {
		t.Fatalf("APs = %+v, want cached connected AP", aps)
	}
}

func TestProcessAccessPointsPrefers5GHzDuplicateSSID(t *testing.T) {
	networks := make(map[string]ScanResult)
	aps := []*wifi.BSS{
		{SSID: "Home", Frequency: 2412, Signal: -4000},
		{SSID: "Home", Frequency: 5180, Signal: -6500},
	}

	processed, _, skipped := processAccessPoints(aps, networks, true)
	if processed != 1 {
		t.Fatalf("processed = %d, want 1", processed)
	}
	if skipped != 1 {
		t.Fatalf("skipped = %d, want 1", skipped)
	}
	got := networks["Home"]
	if got.Frequency != 5180 {
		t.Fatalf("frequency = %d, want 5180", got.Frequency)
	}
}

func TestConvertSignalToDBM(t *testing.T) {
	tests := []struct {
		name          string
		signalMBm     int32
		signalPercent uint32
		want          int
	}{
		// mBm precision: prior implementation used int(mBm)/100 which truncates
		// toward zero, biasing negative dBm values toward stronger-than-reality.
		{name: "exact dBm negative", signalMBm: -6500, want: -65},
		{name: "rounds half away from zero negative", signalMBm: -7250, want: -73},
		{name: "rounds half away from zero negative just below half", signalMBm: -7249, want: -72},
		{name: "rounds half away from zero positive", signalMBm: 7250, want: 73},
		{name: "exact dBm positive", signalMBm: 4200, want: 42},

		// When mBm is 0 the driver did not populate it; previously the code
		// reported 0 dBm (an "excellent" signal) regardless of reality. Now
		// we map percent onto a plausible dBm range.
		{name: "no mBm, no percent reports 0", signalMBm: 0, signalPercent: 0, want: 0},
		{name: "no mBm, 100 percent maps to -40", signalMBm: 0, signalPercent: 100, want: -40},
		{name: "no mBm, 0 percent maps to -100", signalMBm: 0, signalPercent: 0, want: 0},
		{name: "no mBm, 50 percent maps to -70", signalMBm: 0, signalPercent: 50, want: -70},
		{name: "no mBm, percent clamps at 100", signalMBm: 0, signalPercent: 250, want: -40},
		{name: "no mBm, 1 percent maps near -100", signalMBm: 0, signalPercent: 1, want: -100},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := convertSignalToDBM(tc.signalMBm, tc.signalPercent)
			if got != tc.want {
				t.Fatalf("convertSignalToDBM(%d, %d) = %d, want %d",
					tc.signalMBm, tc.signalPercent, got, tc.want)
			}
		})
	}
}

func TestProcessAccessPointsKeepsFirstDuplicateSSIDWhen5GHzPreferenceDisabled(t *testing.T) {
	networks := make(map[string]ScanResult)
	aps := []*wifi.BSS{
		{SSID: "Home", Frequency: 2412, Signal: -4000},
		{SSID: "Home", Frequency: 5180, Signal: -6500},
	}

	processAccessPoints(aps, networks, false)
	got := networks["Home"]
	if got.Frequency != 2412 {
		t.Fatalf("frequency = %d, want 2412", got.Frequency)
	}
}
