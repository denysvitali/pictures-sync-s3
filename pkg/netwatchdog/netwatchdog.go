// Package netwatchdog detects host connectivity issues across:
// 1) default-gateway reachability (ICMP),
// 2) internet DNS+ICMP reachability to common public hosts, and
// 3) Tailscale status.
package netwatchdog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"strings"
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

// TailscaleCheckFunc validates that Tailscale is usable/endpoints are reachable.
type TailscaleCheckFunc func(ctx context.Context) error

// Config controls the watchdog. Zero values are replaced with defaults in New.
type Config struct {
	Interface        string
	Interval         time.Duration
	PingTimeout      time.Duration
	FailureThreshold int
	LogPath          string
	MaxLogBytes      int64
	RebootOnFailure  bool
	SetupModeIP      net.IP

	Ping           PingFunc
	Reboot         RebootFunc
	Gateway        GatewayFunc
	TailscaleCheck TailscaleCheckFunc

	InternetTargets []string
}

const tailscaleBinary = "/user/tailscale"

var defaultInternetTargets = []string{
	"www.google.com",
	"www.apple.com",
	"www.microsoft.com",
}

// DefaultConfig returns the production defaults: 30s interval, 10 consecutive
// failures (~5 min) before reboot, persistent log under /perm.
func DefaultConfig() Config {
	return Config{
		Interface:        "",
		Interval:         30 * time.Second,
		PingTimeout:      3 * time.Second,
		FailureThreshold: 10,
		LogPath:          "/perm/logs/netwatchdog.log",
		MaxLogBytes:      1 << 20,
		RebootOnFailure:  true,
		SetupModeIP:      net.IPv4(192, 168, 44, 1),
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
	if cfg.TailscaleCheck == nil {
		cfg.TailscaleCheck = defaultTailscaleCheck
	}
	if len(cfg.InternetTargets) == 0 {
		cfg.InternetTargets = defaultInternetTargets
	}
	if cfg.SetupModeIP != nil {
		cfg.SetupModeIP = cfg.SetupModeIP.To4()
	}
	return &Watchdog{cfg: cfg, plog: newPersistLog(cfg.LogPath, cfg.MaxLogBytes)}
}

// Run blocks until ctx is cancelled. Safe to call once per Watchdog.
func (w *Watchdog) Run(ctx context.Context) {
	w.logf("netwatchdog: starting (iface=%s interval=%s threshold=%d reboot=%v)",
		interfaceLabel(w.cfg.Interface), w.cfg.Interval, w.cfg.FailureThreshold, w.cfg.RebootOnFailure)

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
	if interfaceHasIPv4(w.cfg.Interface, w.cfg.SetupModeIP) {
		return true, fmt.Sprintf("setup hotspot active on %s (%s)", interfaceLabel(w.cfg.Interface), w.cfg.SetupModeIP)
	}
	gw, err := w.cfg.Gateway(w.cfg.Interface)
	if err != nil {
		return false, fmt.Sprintf("no default gateway on %s: %v", interfaceLabel(w.cfg.Interface), err)
	}
	pingCtx, cancel := context.WithTimeout(ctx, w.cfg.PingTimeout)
	defer cancel()
	if err := w.cfg.Ping(pingCtx, gw.String()); err != nil {
		return false, fmt.Sprintf("gateway ping %s failed: %v", gw, err)
	}
	if err := w.checkInternetConnectivity(ctx); err != nil {
		return false, err.Error()
	}
	if err := w.cfg.TailscaleCheck(ctx); err != nil {
		return false, fmt.Sprintf("tailscale check failed: %v", err)
	}
	return true, fmt.Sprintf("gateway, internet, and tailscale checks passed")
}

func (w *Watchdog) checkInternetConnectivity(ctx context.Context) error {
	var errs []string
	for _, host := range w.cfg.InternetTargets {
		pingCtx, cancel := context.WithTimeout(ctx, w.cfg.PingTimeout)
		err := w.cfg.Ping(pingCtx, host)
		cancel()
		if err == nil {
			return nil
		}
		errs = append(errs, fmt.Sprintf("%s: %v", host, err))
	}
	return fmt.Errorf("internet check failed for all targets: %s", strings.Join(errs, "; "))
}

func interfaceHasIPv4(iface string, target net.IP) bool {
	target = target.To4()
	if target == nil {
		return false
	}
	iface = strings.TrimSpace(iface)
	if iface == "" {
		intfs, err := net.Interfaces()
		if err != nil {
			return false
		}
		for _, intf := range intfs {
			if interfaceHasIPv4OnInterface(intf.Name, target) {
				return true
			}
		}
		return false
	}
	return interfaceHasIPv4OnInterface(iface, target)
}

func interfaceLabel(iface string) string {
	if strings.TrimSpace(iface) == "" {
		return "auto"
	}
	return iface
}

func interfaceHasIPv4OnInterface(iface string, target net.IP) bool {
	intf, err := net.InterfaceByName(iface)
	if err != nil {
		return false
	}
	addrs, err := intf.Addrs()
	if err != nil {
		return false
	}
	for _, addr := range addrs {
		ipNet, ok := addr.(*net.IPNet)
		if !ok {
			continue
		}
		if ip := ipNet.IP.To4(); ip != nil && ip.Equal(target) {
			return true
		}
	}
	return false
}

type tailscaleStatus struct {
	BackendState string `json:"BackendState"`
	Self         struct {
		TailscaleIPs []string `json:"TailscaleIPs"`
	} `json:"Self"`
}

func defaultTailscaleCheck(ctx context.Context) error {
	cmdCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, tailscaleBinary, "status", "--json")
	data, err := cmd.CombinedOutput()
	if err != nil {
		if isCommandMissing(err) {
			return nil
		}
		return parseTailscaleStatusError(err, data)
	}
	return validateTailscaleJSON(data)
}

func isCommandMissing(err error) bool {
	var pathErr *exec.Error
	if errors.As(err, &pathErr) {
		return pathErr.Err == exec.ErrNotFound
	}
	if errors.Is(err, os.ErrNotExist) {
		return true
	}
	return false
}

func parseTailscaleStatusError(err error, output []byte) error {
	stderr := strings.TrimSpace(string(output))
	if stderr == "" {
		return fmt.Errorf("tailscale status command failed: %w", err)
	}
	return fmt.Errorf("tailscale status command failed: %v: %s", err, stderr)
}

func validateTailscaleJSON(data []byte) error {
	var status tailscaleStatus
	cleaned := trimBOM(data)
	if err := json.Unmarshal(cleaned, &status); err != nil {
		return fmt.Errorf("invalid tailscale status json: %w", err)
	}
	if status.BackendState != "Running" {
		return fmt.Errorf("tailscale backend state: %s", status.BackendState)
	}
	if len(status.Self.TailscaleIPs) == 0 {
		return errors.New("tailscale has no self IPs")
	}
	return nil
}

func trimBOM(data []byte) []byte {
	if len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF {
		return data[3:]
	}
	return data
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
