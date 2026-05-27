package netwatchdog

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestParseGateway(t *testing.T) {
	const sample = `Iface	Destination	Gateway 	Flags	RefCnt	Use	Metric	Mask		MTU	Window	IRTT
wlan0	00000000	0101A8C0	0003	0	0	600	00000000	0	0	0
wlan0	0000A8C0	00000000	0001	0	0	600	00FFFFFF	0	0	0
`
	ip, err := parseGateway(strings.NewReader(sample), "wlan0")
	if err != nil {
		t.Fatalf("parseGateway: %v", err)
	}
	if got, want := ip.String(), "192.168.1.1"; got != want {
		t.Fatalf("gateway = %s, want %s", got, want)
	}
}

func TestParseGatewayFiltersByInterface(t *testing.T) {
	const sample = `Iface	Destination	Gateway 	Flags	RefCnt	Use	Metric	Mask		MTU	Window	IRTT
eth0	00000000	0102A8C0	0003	0	0	100	00000000	0	0	0
wlan0	00000000	0101A8C0	0003	0	0	600	00000000	0	0	0
`
	ip, err := parseGateway(strings.NewReader(sample), "wlan0")
	if err != nil {
		t.Fatalf("parseGateway: %v", err)
	}
	if got, want := ip.String(), "192.168.1.1"; got != want {
		t.Fatalf("gateway = %s, want %s", got, want)
	}
}

func TestParseGatewayMissing(t *testing.T) {
	const sample = `Iface	Destination	Gateway 	Flags	RefCnt	Use	Metric	Mask		MTU	Window	IRTT
wlan0	0000A8C0	00000000	0001	0	0	600	00FFFFFF	0	0	0
`
	if _, err := parseGateway(strings.NewReader(sample), "wlan0"); err == nil {
		t.Fatalf("expected error for missing default route")
	}
}

func TestWatchdogReboots(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "netwatchdog.log")

	var pings, reboots atomic.Int32
	cfg := Config{
		Interface:        "test0",
		Interval:         5 * time.Millisecond,
		PingTimeout:      time.Second,
		FailureThreshold: 3,
		LogPath:          logPath,
		MaxLogBytes:      1 << 20,
		RebootOnFailure:  true,
		InternetTargets:  []string{"www.example.com"},
		TailscaleCheck:   func(context.Context) error { return nil },
		Gateway: func(string) (net.IP, error) {
			return net.IPv4(192, 168, 1, 1), nil
		},
		Ping: func(context.Context, string) error {
			pings.Add(1)
			return errors.New("unreachable")
		},
		Reboot: func() error {
			reboots.Add(1)
			return nil
		},
	}

	w := New(cfg)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { w.Run(ctx); close(done) }()

	deadline := time.After(2 * time.Second)
	for reboots.Load() == 0 {
		select {
		case <-deadline:
			cancel()
			t.Fatalf("reboot was not triggered (pings=%d)", pings.Load())
		case <-time.After(5 * time.Millisecond):
		}
	}
	cancel()
	<-done

	if got := pings.Load(); got < 3 {
		t.Fatalf("expected at least 3 pings before reboot, got %d", got)
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if !strings.Contains(string(data), "threshold reached") {
		t.Fatalf("log missing threshold message: %s", data)
	}
}

func TestWatchdogRecoveryResetsCounter(t *testing.T) {
	dir := t.TempDir()
	var attempt atomic.Int32
	var reboots atomic.Int32

	cfg := Config{
		Interface:        "test0",
		Interval:         5 * time.Millisecond,
		PingTimeout:      time.Second,
		FailureThreshold: 3,
		LogPath:          filepath.Join(dir, "netwatchdog.log"),
		MaxLogBytes:      1 << 20,
		RebootOnFailure:  true,
		InternetTargets:  []string{"www.example.com"},
		TailscaleCheck:   func(context.Context) error { return nil },
		Gateway: func(string) (net.IP, error) {
			return net.IPv4(192, 168, 1, 1), nil
		},
		Ping: func(context.Context, string) error {
			n := attempt.Add(1)
			// Pattern: fail, fail, succeed, repeat — never reach threshold of 3.
			if n%3 == 0 {
				return nil
			}
			return errors.New("unreachable")
		},
		Reboot: func() error {
			reboots.Add(1)
			return nil
		},
	}

	w := New(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	w.Run(ctx)

	if reboots.Load() != 0 {
		t.Fatalf("expected no reboot when failures don't reach threshold consecutively, got %d", reboots.Load())
	}
	if attempt.Load() < 5 {
		t.Fatalf("expected several ping attempts, got %d", attempt.Load())
	}
}

func TestCheckOnceTreatsSetupHotspotAsHealthy(t *testing.T) {
	var gatewayCalls atomic.Int32
	cfg := Config{
		Interface:      "lo",
		SetupModeIP:    net.IPv4(127, 0, 0, 1),
		TailscaleCheck: func(context.Context) error { return nil },
		Gateway: func(string) (net.IP, error) {
			gatewayCalls.Add(1)
			return nil, errors.New("gateway should not be checked")
		},
		Ping: func(context.Context, string) error {
			t.Fatal("ping should not be called while setup hotspot is active")
			return nil
		},
	}

	ok, msg := New(cfg).checkOnce(context.Background())
	if !ok {
		t.Fatalf("checkOnce() ok = false, msg = %q", msg)
	}
	if !strings.Contains(msg, "setup hotspot active") {
		t.Fatalf("checkOnce() msg = %q, want setup hotspot message", msg)
	}
	if gatewayCalls.Load() != 0 {
		t.Fatalf("gateway was called %d times, want 0", gatewayCalls.Load())
	}
}

func TestCheckOnceTreatsSetupHotspotAsHealthyAcrossAnyInterface(t *testing.T) {
	var gatewayCalls atomic.Int32
	cfg := Config{
		Interface:      "",
		SetupModeIP:    net.IPv4(127, 0, 0, 1),
		TailscaleCheck: func(context.Context) error { return nil },
		Gateway: func(string) (net.IP, error) {
			gatewayCalls.Add(1)
			return nil, errors.New("gateway should not be checked")
		},
		Ping: func(context.Context, string) error {
			t.Fatal("ping should not be called while setup hotspot is active")
			return nil
		},
	}

	ok, msg := New(cfg).checkOnce(context.Background())
	if !ok {
		t.Fatalf("checkOnce() ok = false, msg = %q", msg)
	}
	if !strings.Contains(msg, "setup hotspot active") {
		t.Fatalf("checkOnce() msg = %q, want setup hotspot message", msg)
	}
	if gatewayCalls.Load() != 0 {
		t.Fatalf("gateway was called %d times, want 0", gatewayCalls.Load())
	}
}

func TestPersistLogRotates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "log")
	pl := newPersistLog(path, 64)
	for i := 0; i < 20; i++ {
		pl.Writef("line-%d-with-padding-to-make-this-long", i)
	}
	if _, err := os.Stat(path + ".1"); err != nil {
		t.Fatalf("expected rotated file: %v", err)
	}
}

func TestCheckOnceFailsWhenInternetAndTailscaleMissing(t *testing.T) {
	var pingCalls atomic.Int32
	cfg := Config{
		Interface:   "lo",
		SetupModeIP: nil,
		Gateway: func(string) (net.IP, error) {
			return net.IPv4(192, 168, 1, 1), nil
		},
		InternetTargets: []string{"bad.google.com"},
		Ping: func(context.Context, string) error {
			pingCalls.Add(1)
			return errors.New("unreachable")
		},
		TailscaleCheck: func(context.Context) error {
			return errors.New("tailscale not ready")
		},
	}
	_, msg := New(cfg).checkOnce(context.Background())
	if !strings.Contains(msg, "internet check failed") {
		t.Fatalf("checkOnce() msg = %q, want internet check failure first", msg)
	}
	if pingCalls.Load() == 0 {
		t.Fatalf("expected ping checks to run")
	}
}

func TestCheckInternetConnectivityFallsBackToNextHost(t *testing.T) {
	var seen []string
	cfg := Config{
		Interface: "lo",
		Gateway: func(string) (net.IP, error) {
			return net.IPv4(127, 0, 0, 1), nil
		},
		InternetTargets: []string{"bad.example.com", "also-bad.example.com", "www.example.com"},
		Ping: func(_ context.Context, host string) error {
			seen = append(seen, host)
			if len(seen) == 3 {
				return nil
			}
			return errors.New("unreachable")
		},
		TailscaleCheck: func(context.Context) error { return nil },
	}

	if err := New(cfg).checkInternetConnectivity(context.Background()); err != nil {
		t.Fatalf("expected internet connectivity to succeed via fallback host, got %v", err)
	}
	if len(seen) != 3 {
		t.Fatalf("expected 3 ping attempts (fallback path), got %d", len(seen))
	}
}
