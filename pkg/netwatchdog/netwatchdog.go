// Package netwatchdog detects when the host has lost LAN connectivity (e.g.
// brcmfmac firmware wedge, AP de-auth without reassociation) and triggers a
// reboot to recover. Symptoms it catches: kernel still has a v4 lease, but the
// default gateway no longer answers ICMP — the device looks healthy locally
// while being unreachable from peers and Tailscale.
package netwatchdog

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"syscall"
	"time"

	probing "github.com/prometheus-community/pro-bing"
)

// PingFunc returns nil if the target is reachable.
type PingFunc func(ctx context.Context, target string) error

// RebootFunc triggers a system reboot.
type RebootFunc func() error

// GatewayFunc returns the IPv4 default gateway for the given interface.
type GatewayFunc func(iface string) (net.IP, error)

// Config controls the watchdog. Zero values are replaced with defaults in New.
type Config struct {
	Interface        string
	Interval         time.Duration
	PingTimeout      time.Duration
	FailureThreshold int
	LogPath          string
	MaxLogBytes      int64
	RebootOnFailure  bool

	Ping    PingFunc
	Reboot  RebootFunc
	Gateway GatewayFunc
}

// DefaultConfig returns the production defaults: 30s interval, 10 consecutive
// failures (~5 min) before reboot, persistent log under /perm.
func DefaultConfig() Config {
	return Config{
		Interface:        "wlan0",
		Interval:         30 * time.Second,
		PingTimeout:      3 * time.Second,
		FailureThreshold: 10,
		LogPath:          "/perm/logs/netwatchdog.log",
		MaxLogBytes:      1 << 20,
		RebootOnFailure:  true,
	}
}

type Watchdog struct {
	cfg  Config
	plog *persistLog
}

func New(cfg Config) *Watchdog {
	if cfg.Interval <= 0 {
		cfg.Interval = 30 * time.Second
	}
	if cfg.PingTimeout <= 0 {
		cfg.PingTimeout = 3 * time.Second
	}
	if cfg.FailureThreshold <= 0 {
		cfg.FailureThreshold = 10
	}
	if cfg.MaxLogBytes <= 0 {
		cfg.MaxLogBytes = 1 << 20
	}
	if cfg.Ping == nil {
		cfg.Ping = defaultPing
	}
	if cfg.Reboot == nil {
		cfg.Reboot = defaultReboot
	}
	if cfg.Gateway == nil {
		cfg.Gateway = DefaultGateway
	}
	return &Watchdog{cfg: cfg, plog: newPersistLog(cfg.LogPath, cfg.MaxLogBytes)}
}

// Run blocks until ctx is cancelled. Safe to call once per Watchdog.
func (w *Watchdog) Run(ctx context.Context) {
	w.logf("netwatchdog: starting (iface=%s interval=%s threshold=%d reboot=%v)",
		w.cfg.Interface, w.cfg.Interval, w.cfg.FailureThreshold, w.cfg.RebootOnFailure)

	fails := 0
	t := time.NewTimer(w.cfg.Interval)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}

		ok, msg := w.checkOnce(ctx)
		if ok {
			if fails > 0 {
				w.logf("netwatchdog: recovered after %d consecutive failures (%s)", fails, msg)
			}
			fails = 0
		} else {
			fails++
			w.logf("netwatchdog: failure %d/%d: %s", fails, w.cfg.FailureThreshold, msg)
			if fails >= w.cfg.FailureThreshold {
				w.logf("netwatchdog: threshold reached, reboot=%v", w.cfg.RebootOnFailure)
				if w.cfg.RebootOnFailure {
					if err := w.cfg.Reboot(); err != nil {
						w.logf("netwatchdog: reboot failed: %v", err)
					}
				}
				// Reset so we don't spin if reboot is disabled or fails.
				fails = 0
			}
		}
		t.Reset(w.cfg.Interval)
	}
}

func (w *Watchdog) checkOnce(ctx context.Context) (bool, string) {
	gw, err := w.cfg.Gateway(w.cfg.Interface)
	if err != nil {
		return false, fmt.Sprintf("no default gateway on %s: %v", w.cfg.Interface, err)
	}
	pingCtx, cancel := context.WithTimeout(ctx, w.cfg.PingTimeout)
	defer cancel()
	if err := w.cfg.Ping(pingCtx, gw.String()); err != nil {
		return false, fmt.Sprintf("ping %s: %v", gw, err)
	}
	return true, fmt.Sprintf("ping %s OK", gw)
}

func (w *Watchdog) logf(format string, args ...any) {
	log.Printf(format, args...)
	w.plog.Writef(format, args...)
}

func defaultPing(ctx context.Context, target string) error {
	p, err := probing.NewPinger(target)
	if err != nil {
		return err
	}
	p.Count = 1
	p.Timeout = 2 * time.Second
	p.SetPrivileged(true)
	if err := p.RunWithContext(ctx); err != nil {
		return err
	}
	if p.PacketsRecv == 0 {
		return errors.New("no reply")
	}
	return nil
}

func defaultReboot() error {
	syscall.Sync()
	return syscall.Reboot(syscall.LINUX_REBOOT_CMD_RESTART)
}
