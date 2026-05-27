package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	probing "github.com/prometheus-community/pro-bing"

	"github.com/denysvitali/pictures-sync-s3/pkg/middleware"
)

// HandleNetworkDNS returns the DNS configuration
func (ctx *Context) HandleNetworkDNS(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Read /etc/resolv.conf
	resolvConf, err := os.ReadFile("/etc/resolv.conf")
	if err != nil {
		log.Printf("Failed to read /etc/resolv.conf: %v", err)
		resolvConf = []byte("Unable to read /etc/resolv.conf")
	}

	JSONResponse(w, map[string]string{
		"resolv_conf": string(resolvConf),
	})
}

// HandleNetworkInterfaces returns network interface information
func (ctx *Context) HandleNetworkInterfaces(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get network interfaces using Go's net package
	interfaces, err := net.Interfaces()
	if err != nil {
		log.Printf("Failed to get network interfaces: %v", err)
		JSONResponse(w, map[string]any{
			"interfaces": []map[string]any{},
			"error":      err.Error(),
		})
		return
	}

	var result []map[string]any
	for _, iface := range interfaces {
		ifaceInfo := make(map[string]any)
		ifaceInfo["name"] = iface.Name
		ifaceInfo["mac"] = iface.HardwareAddr.String()
		ifaceInfo["mtu"] = iface.MTU

		// Parse flags
		flags := []string{}
		if iface.Flags&net.FlagUp != 0 {
			flags = append(flags, "up")
		}
		if iface.Flags&net.FlagBroadcast != 0 {
			flags = append(flags, "broadcast")
		}
		if iface.Flags&net.FlagLoopback != 0 {
			flags = append(flags, "loopback")
		}
		if iface.Flags&net.FlagPointToPoint != 0 {
			flags = append(flags, "pointtopoint")
		}
		if iface.Flags&net.FlagMulticast != 0 {
			flags = append(flags, "multicast")
		}
		ifaceInfo["flags"] = flags

		// Get addresses
		addrs, err := iface.Addrs()
		if err == nil {
			var addresses []map[string]string
			for _, addr := range addrs {
				addrInfo := make(map[string]string)
				ipNet, ok := addr.(*net.IPNet)
				if ok {
					if ipNet.IP.To4() != nil {
						addrInfo["family"] = "inet"
						addrInfo["address"] = ipNet.String()
					} else {
						addrInfo["family"] = "inet6"
						addrInfo["address"] = ipNet.String()
					}
					addresses = append(addresses, addrInfo)
				}
			}
			ifaceInfo["addresses"] = addresses
		}

		result = append(result, ifaceInfo)
	}

	JSONResponse(w, map[string]any{
		"interfaces": result,
	})
}

// HandleDNSLookup performs DNS lookup with SSRF protection
func (ctx *Context) HandleDNSLookup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Hostname string `json:"hostname"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if req.Hostname == "" {
		http.Error(w, "hostname is required", http.StatusBadRequest)
		return
	}

	// SSRF Protection: Validate hostname before resolving
	clientIP := middleware.GetClientIP(r)
	if ctx.SSRFValidator == nil {
		log.Printf("[SSRF] Warning: SSRFValidator not initialized, blocking request")
		http.Error(w, "Service temporarily unavailable", http.StatusServiceUnavailable)
		return
	}

	ips, err := ctx.SSRFValidator.ValidateHostname(req.Hostname, clientIP)
	if err != nil {
		log.Printf("[SSRF] DNS lookup blocked for %s from %s: %v", req.Hostname, clientIP, err)
		JSONResponse(w, map[string]any{
			"error": fmt.Sprintf("Request blocked: %v", err),
		})
		return
	}

	// Convert IPs to strings
	var addresses []string
	for _, ip := range ips {
		addresses = append(addresses, ip.String())
	}

	// Build raw output
	var rawOutput strings.Builder
	rawOutput.WriteString(fmt.Sprintf("Name: %s\n", req.Hostname))
	for _, ip := range ips {
		if ip.To4() != nil {
			rawOutput.WriteString(fmt.Sprintf("Address: %s (IPv4)\n", ip.String()))
		} else {
			rawOutput.WriteString(fmt.Sprintf("Address: %s (IPv6)\n", ip.String()))
		}
	}

	log.Printf("[SSRF] DNS lookup allowed for %s from %s, resolved %d addresses", req.Hostname, clientIP, len(addresses))

	JSONResponse(w, map[string]any{
		"addresses":  addresses,
		"raw_output": rawOutput.String(),
	})
}

// HandlePing performs ICMP ping test with SSRF protection
func (ctx *Context) HandlePing(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Hostname string `json:"hostname"`
		Count    int    `json:"count"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if req.Hostname == "" {
		http.Error(w, "hostname is required", http.StatusBadRequest)
		return
	}

	if req.Count <= 0 {
		req.Count = 4
	}
	if req.Count > 10 {
		req.Count = 10
	}

	// SSRF Protection: Validate hostname and resolve safely
	clientIP := middleware.GetClientIP(r)
	if ctx.SSRFValidator == nil {
		log.Printf("[SSRF] Warning: SSRFValidator not initialized, blocking request")
		http.Error(w, "Service temporarily unavailable", http.StatusServiceUnavailable)
		return
	}

	ips, err := ctx.SSRFValidator.ValidateHostname(req.Hostname, clientIP)
	if err != nil {
		log.Printf("[SSRF] Ping blocked for %s from %s: %v", req.Hostname, clientIP, err)
		JSONResponse(w, map[string]any{
			"error": fmt.Sprintf("Request blocked: %v", err),
		})
		return
	}

	if len(ips) == 0 {
		JSONResponse(w, map[string]any{
			"error": "No IP addresses found for hostname",
		})
		return
	}

	// Use first IPv4 address
	var targetIP net.IP
	for _, ip := range ips {
		if ip.To4() != nil {
			targetIP = ip
			break
		}
	}
	if targetIP == nil {
		targetIP = ips[0] // Fallback to first IP (might be IPv6)
	}

	log.Printf("[SSRF] Ping allowed for %s (%s) from %s", req.Hostname, targetIP.String(), clientIP)

	// Perform ping
	result := performICMPPing(req.Hostname, targetIP.String(), req.Count)
	JSONResponse(w, result)
}

// performICMPPing executes ICMP ping using pro-bing
func performICMPPing(hostname, ipAddr string, count int) map[string]any {
	var output strings.Builder

	output.WriteString(fmt.Sprintf("PING %s (%s) 56 bytes of data\n\n", hostname, ipAddr))

	pinger, err := probing.NewPinger(ipAddr)
	if err != nil {
		return map[string]any{
			"error": fmt.Sprintf("Failed to create pinger: %v", err),
		}
	}

	// Set ping count and timeout
	pinger.Count = count
	pinger.Timeout = 3 * time.Second
	pinger.Interval = 1 * time.Second

	// Set to privileged mode (requires root/CAP_NET_RAW)
	pinger.SetPrivileged(true)

	// Track each response
	var rtts []time.Duration
	pinger.OnRecv = func(pkt *probing.Packet) {
		rtts = append(rtts, pkt.Rtt)
		output.WriteString(fmt.Sprintf("%d: Reply from %s: seq=%d time=%v\n",
			len(rtts), pkt.IPAddr, pkt.Seq, pkt.Rtt))
	}

	// Run the pinger
	err = pinger.Run()
	if err != nil {
		return map[string]any{
			"error": fmt.Sprintf("Ping failed: %v", err),
		}
	}

	// Get statistics
	stats := pinger.Statistics()

	// Calculate statistics
	output.WriteString(fmt.Sprintf("\n--- %s ping statistics ---\n", hostname))
	output.WriteString(fmt.Sprintf("%d packets transmitted, %d received, %.1f%% packet loss\n",
		stats.PacketsSent, stats.PacketsRecv, stats.PacketLoss))

	if stats.PacketsRecv > 0 {
		output.WriteString(fmt.Sprintf("rtt min/avg/max/stddev = %v/%v/%v/%v\n",
			stats.MinRtt, stats.AvgRtt, stats.MaxRtt, stats.StdDevRtt))
	}

	summary := fmt.Sprintf("%d packets transmitted, %d received, %.1f%% packet loss",
		stats.PacketsSent, stats.PacketsRecv, stats.PacketLoss)

	return map[string]any{
		"output":  output.String(),
		"summary": summary,
	}
}

// HandleNetworkDiagnostics runs full network diagnostics
// Note: This uses hardcoded safe IPs (8.8.8.8, 1.1.1.1) and known-safe domains
func (ctx *Context) HandleNetworkDiagnostics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	result := make(map[string]any)

	// Test DNS servers (ICMP ping) - these are hardcoded safe public IPs
	result["dns_google"] = testICMPPing("8.8.8.8", 2*time.Second)
	result["dns_cloudflare"] = testICMPPing("1.1.1.1", 2*time.Second)

	// Test internet connectivity via DNS resolution and ping
	// Using SSRF protection for these lookups as well
	clientIP := middleware.GetClientIP(r)
	if ctx.SSRFValidator != nil {
		// Validate google.com
		ips, err := ctx.SSRFValidator.ValidateHostname("google.com", clientIP)
		if err == nil && len(ips) > 0 {
			result["internet_google"] = testICMPPing(ips[0].String(), 3*time.Second)
		} else {
			result["internet_google"] = false
			if err != nil {
				log.Printf("[SSRF] Network diagnostics: google.com blocked: %v", err)
			}
		}

		// Validate cloudflare.com
		ips, err = ctx.SSRFValidator.ValidateHostname("cloudflare.com", clientIP)
		if err == nil && len(ips) > 0 {
			result["internet_cloudflare"] = testICMPPing(ips[0].String(), 3*time.Second)
		} else {
			result["internet_cloudflare"] = false
			if err != nil {
				log.Printf("[SSRF] Network diagnostics: cloudflare.com blocked: %v", err)
			}
		}
	} else {
		result["internet_google"] = false
		result["internet_cloudflare"] = false
		log.Printf("[SSRF] Warning: SSRFValidator not initialized for network diagnostics")
	}

	// Get default gateway from routes
	gateway := getDefaultGatewayFromRoutes()
	result["gateway"] = gateway

	// Note: We allow pinging the gateway even if it's a private IP,
	// since it's determined from the local routing table and is a legitimate use case
	if gateway != "" {
		result["gateway_reachable"] = testICMPPing(gateway, 2*time.Second)
	}

	// Read routing table from /proc/net/route
	routes := readRoutingTable()
	result["routes"] = routes

	JSONResponse(w, result)
}

// Helper functions for network debugging

// testICMPPing tests if a host responds to ICMP ping
func testICMPPing(ipAddr string, timeout time.Duration) bool {
	pinger, err := probing.NewPinger(ipAddr)
	if err != nil {
		return false
	}

	pinger.Count = 1
	pinger.Timeout = timeout
	pinger.SetPrivileged(true)

	err = pinger.Run()
	if err != nil {
		return false
	}

	stats := pinger.Statistics()
	return stats.PacketsRecv > 0
}

// getDefaultGatewayFromRoutes reads the default gateway from /proc/net/route
func getDefaultGatewayFromRoutes() string {
	// Read /proc/net/route
	data, err := os.ReadFile("/proc/net/route")
	if err != nil {
		log.Printf("Failed to read /proc/net/route: %v", err)
		return ""
	}

	lines := strings.Split(string(data), "\n")
	for i, line := range lines {
		if i == 0 {
			continue // Skip header
		}
		fields := strings.Fields(line)
		if len(fields) >= 3 {
			// Check if destination is 00000000 (default route)
			if fields[1] == "00000000" {
				// Gateway is in field 2, in hex format (little-endian)
				gatewayHex := fields[2]
				if len(gatewayHex) == 8 {
					// Convert hex to IP (little-endian)
					ip := fmt.Sprintf("%d.%d.%d.%d",
						hexToInt(gatewayHex[6:8]),
						hexToInt(gatewayHex[4:6]),
						hexToInt(gatewayHex[2:4]),
						hexToInt(gatewayHex[0:2]))
					return ip
				}
			}
		}
	}
	return ""
}

// hexToInt converts hex string to int
func hexToInt(hex string) int {
	var val int
	fmt.Sscanf(hex, "%x", &val)
	return val
}

// readRoutingTable reads and formats the routing table
func readRoutingTable() string {
	data, err := os.ReadFile("/proc/net/route")
	if err != nil {
		return fmt.Sprintf("Failed to read routing table: %v", err)
	}

	lines := strings.Split(string(data), "\n")
	var output strings.Builder
	output.WriteString(fmt.Sprintf("%-15s %-15s %-15s %-7s\n", "Destination", "Gateway", "Genmask", "Iface"))
	output.WriteString(strings.Repeat("-", 60) + "\n")

	for i, line := range lines {
		if i == 0 || line == "" {
			continue // Skip header and empty lines
		}
		fields := strings.Fields(line)
		if len(fields) >= 8 {
			iface := fields[0]
			dest := hexToIP(fields[1])
			gateway := hexToIP(fields[2])
			mask := hexToIP(fields[7])

			output.WriteString(fmt.Sprintf("%-15s %-15s %-15s %-7s\n", dest, gateway, mask, iface))
		}
	}

	return output.String()
}

// hexToIP converts hex IP (little-endian) to dotted notation
func hexToIP(hex string) string {
	if len(hex) != 8 {
		return "0.0.0.0"
	}
	return fmt.Sprintf("%d.%d.%d.%d",
		hexToInt(hex[6:8]),
		hexToInt(hex[4:6]),
		hexToInt(hex[2:4]),
		hexToInt(hex[0:2]))
}
