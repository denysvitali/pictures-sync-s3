package provisionap

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultInterface       = "wlan0"
	defaultSSID            = "PhotoBackup-Setup"
	defaultPSK             = "photo-backup-setup"
	defaultAPIP            = "192.168.44.1"
	defaultNetmask         = "255.255.255.0"
	defaultDHCPStart       = "192.168.44.50"
	defaultDHCPEnd         = "192.168.44.150"
	defaultHostapdPath     = "/usr/bin/hostapd"
	defaultConfigDir       = "/tmp/provision-ap"
	defaultStartupWait     = 90 * time.Second
	defaultConfigPollDelay = 5 * time.Second
)

// Config contains all runtime settings for provisioning hotspot mode.
type Config struct {
	Interface        string
	SSID             string
	PSK              string
	APIP             net.IP
	Netmask          net.IPMask
	DHCPStart        net.IP
	DHCPEnd          net.IP
	HostapdPath      string
	ConfigDir        string
	ClientConfigPath string
	AppConfigPath    string
	StartupWait      time.Duration
	ConfigPollDelay  time.Duration
}

// ConfigFromEnv builds a Config from environment variables with safe defaults.
func ConfigFromEnv() (Config, error) {
	cfg := Config{
		Interface:        getenv("PROVISION_AP_INTERFACE", defaultInterface),
		SSID:             getenv("SETUP_WIFI_SSID", defaultSSID),
		PSK:              getenv("SETUP_WIFI_PSK", defaultPSK),
		HostapdPath:      getenv("HOSTAPD_PATH", defaultHostapdPath),
		ConfigDir:        getenv("PROVISION_AP_CONFIG_DIR", defaultConfigDir),
		ClientConfigPath: getenv("WIFI_CONFIG_PATH", "/perm/wifi.json"),
		AppConfigPath:    getenv("EXTRA_WIFI_CONFIG_PATH", "/perm/extra-wifi.json"),
		StartupWait:      getenvDuration("PROVISION_AP_STARTUP_WAIT", defaultStartupWait),
		ConfigPollDelay:  getenvDuration("PROVISION_AP_CONFIG_POLL", defaultConfigPollDelay),
	}

	apIP := net.ParseIP(getenv("PROVISION_AP_IP", defaultAPIP)).To4()
	if apIP == nil {
		return Config{}, fmt.Errorf("invalid PROVISION_AP_IP")
	}
	cfg.APIP = apIP

	netmask := net.ParseIP(getenv("PROVISION_AP_NETMASK", defaultNetmask)).To4()
	if netmask == nil {
		return Config{}, fmt.Errorf("invalid PROVISION_AP_NETMASK")
	}
	cfg.Netmask = net.IPMask(netmask)

	dhcpStart := net.ParseIP(getenv("PROVISION_AP_DHCP_START", defaultDHCPStart)).To4()
	if dhcpStart == nil {
		return Config{}, fmt.Errorf("invalid PROVISION_AP_DHCP_START")
	}
	cfg.DHCPStart = dhcpStart

	dhcpEnd := net.ParseIP(getenv("PROVISION_AP_DHCP_END", defaultDHCPEnd)).To4()
	if dhcpEnd == nil {
		return Config{}, fmt.Errorf("invalid PROVISION_AP_DHCP_END")
	}
	cfg.DHCPEnd = dhcpEnd

	return cfg, cfg.Validate()
}

func (c Config) Validate() error {
	if strings.TrimSpace(c.Interface) == "" {
		return fmt.Errorf("interface cannot be empty")
	}
	if err := validateSSID(c.SSID); err != nil {
		return err
	}
	if err := validatePSK(c.PSK); err != nil {
		return err
	}
	if c.APIP == nil || c.APIP.To4() == nil {
		return fmt.Errorf("AP IP must be an IPv4 address")
	}
	if len(c.Netmask) != net.IPv4len {
		return fmt.Errorf("netmask must be IPv4")
	}
	if c.DHCPStart == nil || c.DHCPStart.To4() == nil {
		return fmt.Errorf("DHCP start must be an IPv4 address")
	}
	if c.DHCPEnd == nil || c.DHCPEnd.To4() == nil {
		return fmt.Errorf("DHCP end must be an IPv4 address")
	}
	if ipToUint32(c.DHCPStart) > ipToUint32(c.DHCPEnd) {
		return fmt.Errorf("DHCP start must be before DHCP end")
	}
	if strings.TrimSpace(c.HostapdPath) == "" {
		return fmt.Errorf("hostapd path cannot be empty")
	}
	if strings.TrimSpace(c.ConfigDir) == "" {
		return fmt.Errorf("config dir cannot be empty")
	}
	if c.ConfigPollDelay <= 0 {
		return fmt.Errorf("config poll delay must be positive")
	}
	return nil
}

func getenv(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func getenvDuration(key string, fallback time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	if duration, err := time.ParseDuration(raw); err == nil {
		return duration
	}
	if seconds, err := strconv.Atoi(raw); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	return fallback
}

func validateSSID(ssid string) error {
	if ssid == "" {
		return fmt.Errorf("SSID cannot be empty")
	}
	if len(ssid) > 32 {
		return fmt.Errorf("SSID cannot exceed 32 bytes")
	}
	for i, r := range ssid {
		if r < 32 || r > 126 {
			return fmt.Errorf("SSID contains invalid character at position %d", i)
		}
	}
	return nil
}

func validatePSK(psk string) error {
	if len(psk) < 8 {
		return fmt.Errorf("PSK must be at least 8 characters")
	}
	if len(psk) > 63 {
		return fmt.Errorf("PSK cannot exceed 63 characters")
	}
	for i, r := range psk {
		if r < 32 || r > 126 {
			return fmt.Errorf("PSK contains invalid character at position %d", i)
		}
	}
	return nil
}

func ipToUint32(ip net.IP) uint32 {
	ip = ip.To4()
	return uint32(ip[0])<<24 | uint32(ip[1])<<16 | uint32(ip[2])<<8 | uint32(ip[3])
}
