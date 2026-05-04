package provisionap

import (
	"context"
	"encoding/binary"
	"fmt"
	"log"
	"net"
	"sync"
	"syscall"
	"time"
)

const (
	dhcpDiscover byte = 1
	dhcpOffer    byte = 2
	dhcpRequest  byte = 3
	dhcpAck      byte = 5
)

type dhcpServer struct {
	cfg    Config
	leases map[string]net.IP
	mu     sync.Mutex
}

func newDHCPServer(cfg Config) *dhcpServer {
	return &dhcpServer{
		cfg:    cfg,
		leases: make(map[string]net.IP),
	}
}

func (s *dhcpServer) Serve(ctx context.Context) error {
	conn, err := listenUDPWithBroadcast(ctx, s.cfg.Interface, ":67")
	if err != nil {
		return err
	}
	defer conn.Close()

	go func() {
		<-ctx.Done()
		conn.Close()
	}()

	buf := make([]byte, 1500)
	for {
		n, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		response, err := s.response(buf[:n])
		if err != nil {
			log.Printf("provision-ap DHCP: ignoring packet: %v", err)
			continue
		}
		if response == nil {
			continue
		}
		if _, err := conn.WriteToUDP(response, &net.UDPAddr{IP: net.IPv4bcast, Port: 68}); err != nil {
			log.Printf("provision-ap DHCP: failed to send response: %v", err)
		}
	}
}

func (s *dhcpServer) response(packet []byte) ([]byte, error) {
	if len(packet) < 240 {
		return nil, fmt.Errorf("short packet")
	}
	if packet[0] != 1 {
		return nil, nil
	}

	options := parseDHCPOptions(packet[240:])
	msgType := firstOptionByte(options[53])
	if msgType != dhcpDiscover && msgType != dhcpRequest {
		return nil, nil
	}

	mac := net.HardwareAddr(packet[28:34])
	lease := s.leaseFor(mac)
	replyType := dhcpOffer
	if msgType == dhcpRequest {
		replyType = dhcpAck
	}

	reply := make([]byte, 240)
	copy(reply, packet[:240])
	reply[0] = 2
	copy(reply[16:20], lease.To4())
	copy(reply[20:24], s.cfg.APIP.To4())
	copy(reply[236:240], []byte{99, 130, 83, 99})

	opts := reply[:240]
	opts = appendDHCPOption(opts, 53, []byte{replyType})
	opts = appendDHCPOption(opts, 54, s.cfg.APIP.To4())
	opts = appendDHCPOption(opts, 51, uint32Bytes(uint32((12*time.Hour)/time.Second)))
	opts = appendDHCPOption(opts, 1, net.IP(s.cfg.Netmask).To4())
	opts = appendDHCPOption(opts, 3, s.cfg.APIP.To4())
	opts = appendDHCPOption(opts, 6, s.cfg.APIP.To4())
	opts = appendDHCPOption(opts, 255, nil)
	return opts, nil
}

func (s *dhcpServer) leaseFor(mac net.HardwareAddr) net.IP {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := mac.String()
	if lease := s.leases[key]; lease != nil {
		return lease
	}

	start := ipToUint32(s.cfg.DHCPStart)
	end := ipToUint32(s.cfg.DHCPEnd)
	leaseCount := len(s.leases)
	if leaseCount < 0 {
		leaseCount = 0
	}
	// #nosec G115 -- lease count from in-memory map is bounded by available memory
	offset := uint32(leaseCount)
	if start+offset > end {
		offset = 0
	}
	lease := uint32ToIP(start + offset)
	s.leases[key] = lease
	return lease
}

func parseDHCPOptions(raw []byte) map[byte][]byte {
	options := make(map[byte][]byte)
	for i := 0; i < len(raw); {
		code := raw[i]
		i++
		if code == 0 {
			continue
		}
		if code == 255 {
			break
		}
		if i >= len(raw) {
			break
		}
		length := int(raw[i])
		i++
		if i+length > len(raw) {
			break
		}
		options[code] = raw[i : i+length]
		i += length
	}
	return options
}

func firstOptionByte(value []byte) byte {
	if len(value) == 0 {
		return 0
	}
	return value[0]
}

func appendDHCPOption(packet []byte, code byte, value []byte) []byte {
	packet = append(packet, code)
	if code == 255 {
		return packet
	}
	// DHCP option length is a single byte; truncate if exceeding 255.
	if len(value) > 255 {
		value = value[:255]
	}
	// #nosec G115 -- len(value) is bounded to [0,255] above
	packet = append(packet, byte(len(value)))
	return append(packet, value...)
}

func uint32Bytes(value uint32) []byte {
	buf := make([]byte, 4)
	binary.BigEndian.PutUint32(buf, value)
	return buf
}

func uint32ToIP(value uint32) net.IP {
	// Each shifted value is masked to [0,255] for safe byte conversion.
	return net.IPv4(
		byte((value>>24)&0xFF),
		byte((value>>16)&0xFF),
		byte((value>>8)&0xFF),
		byte(value&0xFF),
	)
}

func listenUDPWithBroadcast(ctx context.Context, interfaceName, addr string) (*net.UDPConn, error) {
	var lc net.ListenConfig
	lc.Control = func(network, address string, c syscall.RawConn) error {
		var ctrlErr error
		if err := c.Control(func(fd uintptr) {
			if err := syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1); err != nil {
				ctrlErr = err
				return
			}
			if err := syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_BROADCAST, 1); err != nil {
				ctrlErr = err
				return
			}
			if interfaceName != "" {
				ctrlErr = syscall.SetsockoptString(int(fd), syscall.SOL_SOCKET, syscall.SO_BINDTODEVICE, interfaceName)
			}
		}); err != nil {
			return err
		}
		return ctrlErr
	}

	packetConn, err := lc.ListenPacket(ctx, "udp4", addr)
	if err != nil {
		return nil, err
	}
	udpConn, ok := packetConn.(*net.UDPConn)
	if !ok {
		packetConn.Close()
		return nil, fmt.Errorf("listener is not UDP")
	}
	return udpConn, nil
}
