package main

import (
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/denysvitali/pictures-sync-s3/pkg/netiface"
	"github.com/mdlayher/genetlink"
	"github.com/mdlayher/netlink"
	"golang.org/x/sys/unix"
)

const (
	defaultInterface       = "wlan0"
	defaultTimeout         = 15 * time.Second
	defaultCountry         = "US"
	defaultEthernetIface   = "eth0"
	defaultEthernetTimeout = 10 * time.Second
	defaultWiFiCommand     = "/user/wifi"
)

type kernelModule struct {
	name     string
	optional bool
}

var moduleOrder = []kernelModule{
	{name: "brcmutil"},
	{name: "brcmfmac"},
	{name: "brcmfmac-wcc", optional: true},
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	iface := getenv("WIFI_INIT_INTERFACE", defaultInterface)
	timeout := getenvDuration("WIFI_INIT_TIMEOUT", defaultTimeout)
	country := strings.ToUpper(getenv("WIFI_COUNTRY", defaultCountry))
	ethernetFirst := getenvBool("WIFI_INIT_ETHERNET_FIRST", true)
	ethernetIface := getenv("WIFI_INIT_ETHERNET_INTERFACE", defaultEthernetIface)
	ethernetTimeout := getenvDuration("WIFI_INIT_ETHERNET_TIMEOUT", defaultEthernetTimeout)
	wifiCommand := getenv("WIFI_INIT_WIFI_COMMAND", defaultWiFiCommand)

	if ethernetFirst {
		log.Printf("wifi-init: waiting up to %s for Ethernet carrier on %s", ethernetTimeout, ethernetIface)
		if waitForEthernetCarrier(ethernetIface, ethernetTimeout) {
			log.Printf("wifi-init: Ethernet carrier detected on %s; leaving Wi-Fi disabled", ethernetIface)
			return
		}
		log.Printf("wifi-init: no Ethernet carrier detected on %s; enabling Wi-Fi", ethernetIface)
	}

	for _, module := range moduleOrder {
		if err := loadModule(module.name); err != nil {
			if module.optional {
				log.Printf("wifi-init: skipping optional module %s: %v", module.name, err)
				continue
			}
			log.Fatalf("load %s: %v", module.name, err)
		}
	}

	if err := waitForInterface(iface, timeout); err != nil {
		log.Fatalf("wait for %s: %v", iface, err)
	}
	log.Printf("wifi-init: %s is available", iface)

	if err := setRegulatoryDomain(country); err != nil {
		log.Printf("wifi-init: failed to set WiFi country %s: %v", country, err)
	} else {
		log.Printf("wifi-init: set WiFi country to %s", country)
	}

	if err := disablePowerSave(iface); err != nil {
		log.Printf("wifi-init: failed to disable power save on %s: %v", iface, err)
	} else {
		log.Printf("wifi-init: disabled power save on %s", iface)
	}

	if strings.TrimSpace(wifiCommand) == "" {
		log.Printf("wifi-init: WIFI_INIT_WIFI_COMMAND is empty; not starting Wi-Fi client")
		return
	}
	log.Printf("wifi-init: starting Wi-Fi client: %s", wifiCommand)
	if err := unix.Exec(wifiCommand, []string{filepath.Base(wifiCommand)}, os.Environ()); err != nil {
		log.Fatalf("start Wi-Fi client %s: %v", wifiCommand, err)
	}
}

func waitForEthernetCarrier(name string, timeout time.Duration) bool {
	if timeout < 0 {
		timeout = 0
	}
	deadline := time.Now().Add(timeout)
	for {
		carrier, err := netiface.HasCarrier(name)
		if err != nil {
			log.Printf("wifi-init: failed to read Ethernet carrier for %s: %v", name, err)
			return false
		}
		if carrier {
			return true
		}
		if timeout == 0 || time.Now().After(deadline) {
			return false
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// disablePowerSave turns off WiFi power management on the given interface via
// nl80211. brcmfmac (Pi WiFi) defaults to power save on, which causes the
// radio to silently stop acking after idle periods — the kernel keeps the IP
// lease so everything looks healthy locally, but the device becomes
// unreachable from the LAN until reassociation.
func disablePowerSave(iface string) error {
	ifi, err := net.InterfaceByName(iface)
	if err != nil {
		return fmt.Errorf("lookup %s: %w", iface, err)
	}

	conn, err := genetlink.Dial(nil)
	if err != nil {
		return fmt.Errorf("genetlink dial: %w", err)
	}
	defer conn.Close()

	family, err := conn.GetFamily(unix.NL80211_GENL_NAME)
	if err != nil {
		return fmt.Errorf("get nl80211 family: %w", err)
	}

	ae := netlink.NewAttributeEncoder()
	ae.Uint32(unix.NL80211_ATTR_IFINDEX, uint32(ifi.Index))
	ae.Uint32(unix.NL80211_ATTR_PS_STATE, unix.NL80211_PS_DISABLED)
	data, err := ae.Encode()
	if err != nil {
		return fmt.Errorf("encode attrs: %w", err)
	}

	_, err = conn.Execute(genetlink.Message{
		Header: genetlink.Header{
			Command: unix.NL80211_CMD_SET_POWER_SAVE,
			Version: family.Version,
		},
		Data: data,
	}, family.ID, netlink.Request|netlink.Acknowledge)
	if err != nil {
		return fmt.Errorf("set power save: %w", err)
	}
	return nil
}

func setRegulatoryDomain(country string) error {
	if len(country) != 2 {
		return fmt.Errorf("country must be a two-letter ISO 3166-1 alpha-2 code")
	}
	for _, r := range country {
		if r < 'A' || r > 'Z' {
			return fmt.Errorf("country must contain only uppercase ASCII letters")
		}
	}

	conn, err := genetlink.Dial(nil)
	if err != nil {
		return fmt.Errorf("genetlink dial: %w", err)
	}
	defer conn.Close()

	family, err := conn.GetFamily(unix.NL80211_GENL_NAME)
	if err != nil {
		return fmt.Errorf("get nl80211 family: %w", err)
	}

	ae := netlink.NewAttributeEncoder()
	ae.String(unix.NL80211_ATTR_REG_ALPHA2, country)
	data, err := ae.Encode()
	if err != nil {
		return fmt.Errorf("encode attrs: %w", err)
	}

	_, err = conn.Execute(genetlink.Message{
		Header: genetlink.Header{
			Command: unix.NL80211_CMD_REQ_SET_REG,
			Version: family.Version,
		},
		Data: data,
	}, family.ID, netlink.Request|netlink.Acknowledge)
	if err != nil {
		return fmt.Errorf("set regulatory domain: %w", err)
	}
	return nil
}

func loadModule(name string) error {
	path, err := findModule(name)
	if err != nil {
		return err
	}

	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	defer unix.Close(fd)

	err = unix.FinitModule(fd, "", 0)
	if err == nil || errors.Is(err, unix.EEXIST) {
		log.Printf("wifi-init: loaded %s from %s", name, path)
		return nil
	}
	return err
}

func findModule(name string) (string, error) {
	release, err := kernelRelease()
	if err != nil {
		return "", err
	}

	root := filepath.Join("/lib/modules", release)
	var matches []string
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		base := filepath.Base(path)
		if base == name+".ko" || strings.HasPrefix(base, name+".ko.") {
			matches = append(matches, path)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("module %q not found under %s", name, root)
	}
	return matches[0], nil
}

func kernelRelease() (string, error) {
	var uts unix.Utsname
	if err := unix.Uname(&uts); err != nil {
		return "", err
	}
	return charsToString(uts.Release[:]), nil
}

func charsToString(chars []byte) string {
	var b strings.Builder
	for _, c := range chars {
		if c == 0 {
			break
		}
		b.WriteByte(c)
	}
	return b.String()
}

func waitForInterface(name string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if _, err := net.InterfaceByName(name); err == nil {
			return nil
		}
		if timeout <= 0 || time.Now().After(deadline) {
			return fmt.Errorf("interface did not appear within %s", timeout)
		}
		time.Sleep(250 * time.Millisecond)
	}
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
	duration, err := time.ParseDuration(raw)
	if err != nil {
		return fallback
	}
	return duration
}

func getenvBool(key string, fallback bool) bool {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	switch strings.ToLower(raw) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}
