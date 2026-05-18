package provisionap

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/denysvitali/pictures-sync-s3/pkg/netwatchdog"
	"github.com/mdlayher/wifi"
)

// Manager runs the provisioning hotspot fallback.
type Manager struct {
	Config     Config
	Runner     ProcessRunner
	Configurer InterfaceConfigurer
	Connected  func(Config) bool
	Reboot     func() error
}

// NewManager creates a Manager with production dependencies.
func NewManager(cfg Config) *Manager {
	return &Manager{
		Config:     cfg,
		Runner:     execRunner{},
		Configurer: linuxInterfaceConfigurer{},
		Connected:  hasUsableConnectivity,
		Reboot:     rebootSystem,
	}
}

// Run blocks until the hotspot is no longer needed or the context is canceled.
func (m *Manager) Run(ctx context.Context) error {
	if err := m.Config.Validate(); err != nil {
		return err
	}
	if m.Runner == nil {
		m.Runner = execRunner{}
	}
	if m.Configurer == nil {
		m.Configurer = linuxInterfaceConfigurer{}
	}
	if m.Connected == nil {
		m.Connected = hasUsableConnectivity
	}
	if m.Reboot == nil {
		m.Reboot = rebootSystem
	}

	initialSnapshot, err := readWiFiConfigSnapshot(m.Config.ClientConfigPath, m.Config.AppConfigPath)
	if err != nil {
		log.Printf("provision-ap: Wi-Fi config check warning: %v", err)
	}
	initialHasConfig := initialSnapshot.HasNetworks

	if initialHasConfig {
		log.Printf("provision-ap: Wi-Fi config exists; waiting %s for client connection", m.Config.StartupWait)
		if waitForConnection(ctx, m.Config, m.Connected) {
			log.Printf("provision-ap: %s has usable client connectivity; monitoring for loss", m.Config.Interface)
			if waitForConnectionLoss(ctx, m.Config, m.Connected) {
				log.Printf("provision-ap: Wi-Fi client connectivity was lost; starting fallback hotspot")
			} else {
				return nil
			}
		} else {
			log.Printf("provision-ap: Wi-Fi client did not become reachable; starting fallback hotspot")
		}
	} else {
		log.Printf("provision-ap: no Wi-Fi config found; starting setup hotspot")
	}

	if err := disableClientWiFiForAP(m.Config); err != nil {
		log.Printf("provision-ap: client Wi-Fi disable warning: %v", err)
	}
	if snapshot, err := readWiFiConfigSnapshot(m.Config.ClientConfigPath, m.Config.AppConfigPath); err == nil {
		initialSnapshot = snapshot
	}

	if err := waitForInterface(ctx, m.Config.Interface, m.Config.InterfaceWait); err != nil {
		return fmt.Errorf("wait for AP interface: %w", err)
	}
	if err := m.Configurer.Configure(m.Config.Interface, m.Config.APIP, m.Config.Netmask); err != nil {
		return fmt.Errorf("configure AP interface: %w", err)
	}

	hostapdConfig, err := writeHostapdConfig(m.Config)
	if err != nil {
		return fmt.Errorf("write hostapd config: %w", err)
	}

	hostapd, err := m.Runner.Start(ctx, m.Config.HostapdPath, hostapdConfig)
	if err != nil {
		return fmt.Errorf("start hostapd: %w", err)
	}
	defer hostapd.Stop()

	serverCtx, cancelServers := context.WithCancel(ctx)
	defer cancelServers()

	errCh := make(chan error, 3)
	var wg sync.WaitGroup
	startServer := func(name string, serve func(context.Context) error) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := serve(serverCtx); err != nil && !errors.Is(err, net.ErrClosed) {
				errCh <- fmt.Errorf("%s: %w", name, err)
			}
		}()
	}

	startServer("dhcp", newDHCPServer(m.Config).Serve)
	startServer("dns", newDNSServer(m.Config.APIP).Serve)
	go func() {
		errCh <- hostapd.Wait()
	}()

	ticker := time.NewTicker(m.Config.ConfigPollDelay)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			cancelServers()
			wg.Wait()
			return nil
		case err := <-errCh:
			cancelServers()
			wg.Wait()
			if err != nil {
				return err
			}
			return nil
		case <-ticker.C:
			snapshot, err := readWiFiConfigSnapshot(m.Config.ClientConfigPath, m.Config.AppConfigPath)
			if err != nil {
				log.Printf("provision-ap: Wi-Fi config check warning: %v", err)
				continue
			}
			if shouldRebootForWiFiConfigChange(initialSnapshot, snapshot) {
				log.Printf("provision-ap: Wi-Fi config changed; rebooting into client mode")
				cancelServers()
				wg.Wait()
				return m.Reboot()
			}
		}
	}
}

func waitForConnection(ctx context.Context, cfg Config, connected func(Config) bool) bool {
	if cfg.StartupWait <= 0 {
		return connected(cfg)
	}
	deadline := time.NewTimer(cfg.StartupWait)
	defer deadline.Stop()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		if connected(cfg) {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-deadline.C:
			return connected(cfg)
		case <-ticker.C:
		}
	}
}

func waitForConnectionLoss(ctx context.Context, cfg Config, connected func(Config) bool) bool {
	ticker := time.NewTicker(cfg.ConfigPollDelay)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return false
		case <-ticker.C:
			if !connected(cfg) {
				return true
			}
		}
	}
}

func disableClientWiFiForAP(cfg Config) error {
	var errs []error
	if err := moveClientWiFiConfigAside(cfg.ClientConfigPath); err != nil {
		errs = append(errs, err)
	}
	if err := terminateClientWiFiProcesses(); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func moveClientWiFiConfigAside(path string) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	backup := path + ".provision-ap-disabled"
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat client Wi-Fi config: %w", err)
	}
	_ = os.Remove(backup)
	if err := os.Rename(path, backup); err != nil {
		return fmt.Errorf("move client Wi-Fi config aside: %w", err)
	}
	log.Printf("provision-ap: moved %s to %s while setup hotspot is active", path, backup)
	return nil
}

func terminateClientWiFiProcesses() error {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return fmt.Errorf("read /proc: %w", err)
	}
	var errs []error
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid == os.Getpid() {
			continue
		}
		exe, err := os.Readlink(filepath.Join("/proc", entry.Name(), "exe"))
		if err != nil {
			continue
		}
		if exe != "/user/wifi" && filepath.Base(exe) != "wifi" {
			continue
		}
		process, err := os.FindProcess(pid)
		if err != nil {
			errs = append(errs, fmt.Errorf("find wifi process %d: %w", pid, err))
			continue
		}
		if err := process.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
			errs = append(errs, fmt.Errorf("terminate wifi process %d: %w", pid, err))
			continue
		}
		log.Printf("provision-ap: terminated client Wi-Fi process pid=%d", pid)
	}
	return errors.Join(errs...)
}

func waitForInterface(ctx context.Context, name string, timeout time.Duration) error {
	if timeout < 0 {
		timeout = 0
	}
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()

	for {
		if _, err := net.InterfaceByName(name); err == nil {
			return nil
		}
		if timeout == 0 {
			return fmt.Errorf("%s is not available", name)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("%s did not appear within %s", name, timeout)
		case <-ticker.C:
		}
	}
}

func hasUsableConnectivity(cfg Config) bool {
	// The provisioning hotspot exists to recover Wi-Fi, so the readiness check
	// is scoped to cfg.Interface (wlan0). Allowing any interface here lets a
	// USB gadget / host-bridge link or stale Ethernet lease mask a dead Wi-Fi
	// client and suppress the fallback hotspot.
	intf, err := net.InterfaceByName(cfg.Interface)
	if err != nil {
		return false
	}
	if intf.Flags&net.FlagUp == 0 || intf.Flags&net.FlagLoopback != 0 {
		return false
	}
	if _, err := netwatchdog.DefaultGateway(cfg.Interface); err != nil {
		return false
	}
	if !interfaceHasUsableIPv4(*intf, cfg) {
		return false
	}
	if !interfaceHasAssociatedStation(cfg.Interface) {
		return false
	}
	return true
}

func interfaceHasUsableIPv4(intf net.Interface, cfg Config) bool {
	addrs, err := intf.Addrs()
	if err != nil {
		return false
	}
	for _, addr := range addrs {
		ipNet, ok := addr.(*net.IPNet)
		if !ok {
			continue
		}
		ip := ipNet.IP.To4()
		if ip == nil || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
			continue
		}
		if intf.Name == cfg.Interface && cfg.APIP != nil && ip.Equal(cfg.APIP) {
			continue
		}
		return true
	}
	return false
}

func interfaceHasAssociatedStation(name string) bool {
	cl, err := wifi.New()
	if err != nil {
		log.Printf("provision-ap: Wi-Fi association check unavailable: %v", err)
		return false
	}
	defer cl.Close()

	intfs, err := cl.Interfaces()
	if err != nil {
		log.Printf("provision-ap: Wi-Fi interface list unavailable: %v", err)
		return false
	}
	for _, intf := range intfs {
		if intf.Name != name {
			continue
		}
		stationInfos, err := cl.StationInfo(intf)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				log.Printf("provision-ap: Wi-Fi station info unavailable for %s: %v", name, err)
			}
			return false
		}
		for _, sta := range stationInfos {
			if !bytes.Equal(sta.HardwareAddr, net.HardwareAddr{}) {
				return true
			}
		}
		return false
	}
	return false
}

// Cleanup removes generated runtime files. It is intentionally separate from
// Run so tests and operators can call it without affecting /perm.
func (m *Manager) Cleanup() error {
	if m.Config.ConfigDir == "" || m.Config.ConfigDir == "/" {
		return nil
	}
	return os.RemoveAll(filepath.Clean(m.Config.ConfigDir))
}
