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

	"golang.org/x/sys/unix"
)

const (
	defaultInterface = "wlan0"
	defaultTimeout   = 15 * time.Second
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
