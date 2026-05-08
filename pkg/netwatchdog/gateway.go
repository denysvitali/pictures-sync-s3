package netwatchdog

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
)

// DefaultGateway returns the IPv4 default gateway for iface (or any interface
// if iface is empty), parsed from /proc/net/route.
func DefaultGateway(iface string) (net.IP, error) {
	f, err := os.Open("/proc/net/route")
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return parseGateway(f, iface)
}

func parseGateway(r io.Reader, iface string) (net.IP, error) {
	s := bufio.NewScanner(r)
	if !s.Scan() {
		return nil, fmt.Errorf("empty /proc/net/route")
	}
	for s.Scan() {
		fields := strings.Fields(s.Text())
		if len(fields) < 8 {
			continue
		}
		if iface != "" && fields[0] != iface {
			continue
		}
		// Default route has destination 0.0.0.0
		if fields[1] != "00000000" {
			continue
		}
		raw, err := hex.DecodeString(fields[2])
		if err != nil || len(raw) != 4 {
			continue
		}
		// /proc/net/route stores addresses in little-endian host byte order
		// (raw[0] is the least significant byte of the IPv4 integer).
		ip := net.IPv4(raw[3], raw[2], raw[1], raw[0])
		if ip.Equal(net.IPv4zero) {
			continue
		}
		return ip, nil
	}
	if err := s.Err(); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("no default gateway for interface %q", iface)
}
