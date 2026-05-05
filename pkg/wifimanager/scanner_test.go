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
	scanWaitDelay = 0
	defer func() { scanWaitDelay = oldDelay }()

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
	scanWaitDelay = 0
	defer func() { scanWaitDelay = oldDelay }()

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
