package provisionap

import (
	"context"
	"encoding/binary"
	"fmt"
	"log"
	"net"
)

type dnsServer struct {
	ip net.IP
}

func newDNSServer(ip net.IP) *dnsServer {
	return &dnsServer{ip: ip.To4()}
}

func (s *dnsServer) Serve(ctx context.Context) error {
	addr, err := net.ResolveUDPAddr("udp4", net.JoinHostPort(s.ip.String(), "53"))
	if err != nil {
		return err
	}
	conn, err := net.ListenUDP("udp4", addr)
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
		n, remote, err := conn.ReadFromUDP(buf)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		response, err := s.response(buf[:n])
		if err != nil {
			log.Printf("provision-ap DNS: ignoring query: %v", err)
			continue
		}
		if _, err := conn.WriteToUDP(response, remote); err != nil {
			log.Printf("provision-ap DNS: failed to send response: %v", err)
		}
	}
}

func (s *dnsServer) response(query []byte) ([]byte, error) {
	if len(query) < 12 {
		return nil, fmt.Errorf("short query")
	}
	qdCount := binary.BigEndian.Uint16(query[4:6])
	if qdCount == 0 {
		return nil, fmt.Errorf("query has no questions")
	}

	questionEnd, qType, err := parseQuestion(query, 12)
	if err != nil {
		return nil, err
	}

	resp := make([]byte, 0, len(query)+32)
	resp = append(resp, query[:2]...)
	resp = append(resp, 0x81, 0x80)
	resp = append(resp, query[4:6]...)
	if qType == 1 {
		resp = append(resp, 0x00, 0x01)
	} else {
		resp = append(resp, 0x00, 0x00)
	}
	resp = append(resp, 0x00, 0x00, 0x00, 0x00)
	resp = append(resp, query[12:questionEnd]...)

	if qType != 1 {
		return resp, nil
	}

	resp = append(resp,
		0xc0, 0x0c, // compressed name pointer to first question
		0x00, 0x01, // A
		0x00, 0x01, // IN
		0x00, 0x00, 0x00, 0x3c, // 60s TTL
		0x00, 0x04,
	)
	resp = append(resp, s.ip.To4()...)
	return resp, nil
}

func parseQuestion(query []byte, offset int) (int, uint16, error) {
	for {
		if offset >= len(query) {
			return 0, 0, fmt.Errorf("question exceeds packet")
		}
		length := int(query[offset])
		offset++
		if length == 0 {
			break
		}
		if length&0xc0 != 0 {
			return 0, 0, fmt.Errorf("compressed question names are unsupported")
		}
		offset += length
	}
	if offset+4 > len(query) {
		return 0, 0, fmt.Errorf("question missing type/class")
	}
	qType := binary.BigEndian.Uint16(query[offset : offset+2])
	return offset + 4, qType, nil
}
