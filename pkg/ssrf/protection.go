package ssrf

import (
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
	"time"
)

// Validator provides SSRF protection by validating hostnames and IP addresses
type Validator struct {
	// Rate limiting per client IP
	rateLimiter *RateLimiter
}

// RateLimiter implements token bucket rate limiting for network operations
type RateLimiter struct {
	mu       sync.Mutex
	buckets  map[string]*bucket
	maxRate  int           // max operations per window
	window   time.Duration // time window for rate limiting
	cleanupT *time.Ticker  // cleanup ticker
}

type bucket struct {
	count      int
	lastAccess time.Time
}

// ValidationError represents an SSRF validation error
type ValidationError struct {
	Reason string
	Target string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("SSRF protection: %s (target: %s)", e.Reason, e.Target)
}

// NewValidator creates a new SSRF validator with rate limiting
func NewValidator(maxRate int, window time.Duration) *Validator {
	limiter := &RateLimiter{
		buckets: make(map[string]*bucket),
		maxRate: maxRate,
		window:  window,
	}

	// Start cleanup goroutine to remove old buckets
	limiter.cleanupT = time.NewTicker(window)
	go limiter.cleanup()

	return &Validator{
		rateLimiter: limiter,
	}
}

// ValidateHostname validates a hostname for SSRF attacks
// Returns the validated hostname and any resolved IPs, or an error
func (v *Validator) ValidateHostname(hostname, clientIP string) ([]net.IP, error) {
	// Log all network diagnostic requests for security audit
	log.Printf("[SSRF] Network diagnostic request from %s for target: %s", clientIP, hostname)

	// Check rate limit
	if !v.rateLimiter.Allow(clientIP) {
		log.Printf("[SSRF] Rate limit exceeded for client %s", clientIP)
		return nil, &ValidationError{
			Reason: "rate limit exceeded",
			Target: hostname,
		}
	}

	// Validate hostname format
	if err := validateHostnameFormat(hostname); err != nil {
		return nil, err
	}

	// Check against dangerous hostnames
	if err := checkDangerousHostnames(hostname); err != nil {
		return nil, err
	}

	// Resolve hostname to IPs
	ips, err := net.LookupIP(hostname)
	if err != nil {
		return nil, &ValidationError{
			Reason: fmt.Sprintf("DNS resolution failed: %v", err),
			Target: hostname,
		}
	}

	if len(ips) == 0 {
		return nil, &ValidationError{
			Reason: "no IP addresses resolved",
			Target: hostname,
		}
	}

	// Validate all resolved IPs
	for _, ip := range ips {
		if err := validateIP(ip, hostname); err != nil {
			return nil, err
		}
	}

	log.Printf("[SSRF] Validation successful for %s from client %s, resolved to %d IPs", hostname, clientIP, len(ips))
	return ips, nil
}

// ValidateIP validates an IP address string for SSRF attacks
func (v *Validator) ValidateIP(ipStr, clientIP string) (net.IP, error) {
	log.Printf("[SSRF] Direct IP request from %s for target: %s", clientIP, ipStr)

	// Check rate limit
	if !v.rateLimiter.Allow(clientIP) {
		log.Printf("[SSRF] Rate limit exceeded for client %s", clientIP)
		return nil, &ValidationError{
			Reason: "rate limit exceeded",
			Target: ipStr,
		}
	}

	ip := net.ParseIP(ipStr)
	if ip == nil {
		return nil, &ValidationError{
			Reason: "invalid IP address format",
			Target: ipStr,
		}
	}

	if err := validateIP(ip, ipStr); err != nil {
		return nil, err
	}

	log.Printf("[SSRF] IP validation successful for %s from client %s", ipStr, clientIP)
	return ip, nil
}

// validateHostnameFormat checks basic hostname format validity
func validateHostnameFormat(hostname string) error {
	if hostname == "" {
		return &ValidationError{
			Reason: "empty hostname",
			Target: hostname,
		}
	}

	if len(hostname) > 253 {
		return &ValidationError{
			Reason: "hostname too long",
			Target: hostname,
		}
	}

	// Check for suspicious patterns
	hostname = strings.ToLower(strings.TrimSpace(hostname))

	// Block URLs (should be hostname only)
	if strings.Contains(hostname, "://") {
		return &ValidationError{
			Reason: "URL scheme not allowed, hostname only",
			Target: hostname,
		}
	}

	// Block @ symbol (credentials in URL)
	if strings.Contains(hostname, "@") {
		return &ValidationError{
			Reason: "credentials in hostname not allowed",
			Target: hostname,
		}
	}

	return nil
}

// checkDangerousHostnames blocks known dangerous hostnames
func checkDangerousHostnames(hostname string) error {
	hostname = strings.ToLower(strings.TrimSpace(hostname))
	// Strip a single trailing dot (fully-qualified DNS form) so that
	// "localhost." and "metadata.google.internal." are treated the
	// same as their non-rooted counterparts.
	hostname = strings.TrimSuffix(hostname, ".")

	// Block localhost variants
	localhostVariants := []string{
		"localhost",
		"localhost.localdomain",
		"ip6-localhost",
		"ip6-loopback",
	}
	for _, variant := range localhostVariants {
		if hostname == variant {
			return &ValidationError{
				Reason: "localhost access not allowed",
				Target: hostname,
			}
		}
	}

	// Block cloud metadata endpoints
	metadataEndpoints := []string{
		"metadata",
		"metadata.google.internal",
		"169.254.169.254",
		"metadata.azure.com",
		"metadata.packet.net",
		"metadata.platformequinix.com",
		"169.254.169.123", // DigitalOcean
	}
	for _, endpoint := range metadataEndpoints {
		if hostname == endpoint || strings.HasSuffix(hostname, "."+endpoint) {
			return &ValidationError{
				Reason: "cloud metadata endpoint access not allowed",
				Target: hostname,
			}
		}
	}

	// Block internal domain patterns
	internalDomains := []string{
		".internal",
		".local",
		".localhost",
		".lan",
	}
	for _, domain := range internalDomains {
		if strings.HasSuffix(hostname, domain) {
			return &ValidationError{
				Reason: "internal domain access not allowed",
				Target: hostname,
			}
		}
	}

	return nil
}

// validateIP checks if an IP address is safe to access
func validateIP(ip net.IP, target string) error {
	// Block loopback addresses (127.0.0.0/8, ::1)
	if ip.IsLoopback() {
		return &ValidationError{
			Reason: "loopback address not allowed",
			Target: target,
		}
	}

	// Block private/internal IP ranges
	if isPrivateIP(ip) {
		return &ValidationError{
			Reason: "private IP address not allowed",
			Target: target,
		}
	}

	// Block link-local addresses (169.254.0.0/16, fe80::/10)
	if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return &ValidationError{
			Reason: "link-local address not allowed (includes cloud metadata)",
			Target: target,
		}
	}

	// Block multicast (224.0.0.0/4, ff00::/8)
	if ip.IsMulticast() {
		return &ValidationError{
			Reason: "multicast address not allowed",
			Target: target,
		}
	}

	// Block unspecified (0.0.0.0, ::)
	if ip.IsUnspecified() {
		return &ValidationError{
			Reason: "unspecified address not allowed",
			Target: target,
		}
	}

	// Block the entire "this network" range 0.0.0.0/8 — many Linux
	// kernels route any 0.0.0.0/8 destination to localhost, so an
	// attacker who can pick an arbitrary IP (e.g. 0.1.2.3) can reach
	// loopback services even though IsUnspecified() only flags 0.0.0.0.
	if ip4 := ip.To4(); ip4 != nil && ip4[0] == 0 {
		return &ValidationError{
			Reason: "0.0.0.0/8 (this-network) address not allowed",
			Target: target,
		}
	}

	// Additional check for AWS metadata service
	if ip.String() == "169.254.169.254" {
		return &ValidationError{
			Reason: "AWS metadata service not allowed",
			Target: target,
		}
	}

	return nil
}

// isPrivateIP checks if an IP is in a private range
func isPrivateIP(ip net.IP) bool {
	// Private IPv4 ranges
	privateIPv4Ranges := []string{
		"10.0.0.0/8",     // Private network
		"172.16.0.0/12",  // Private network
		"192.168.0.0/16", // Private network
		"100.64.0.0/10",  // Shared address space (Carrier-grade NAT)
		"198.18.0.0/15",  // Benchmark testing
		"192.0.0.0/24",   // IETF Protocol Assignments
		"192.0.2.0/24",   // TEST-NET-1
		"198.51.100.0/24", // TEST-NET-2
		"203.0.113.0/24", // TEST-NET-3
		"240.0.0.0/4",    // Reserved
	}

	// Private IPv6 ranges
	privateIPv6Ranges := []string{
		"fc00::/7",   // Unique local addresses
		"fe80::/10",  // Link-local
		"::1/128",    // Loopback
		"ff00::/8",   // Multicast
		"2001:db8::/32", // Documentation
	}

	// Check IPv4
	if ip.To4() != nil {
		for _, cidr := range privateIPv4Ranges {
			_, ipNet, err := net.ParseCIDR(cidr)
			if err != nil {
				continue
			}
			if ipNet.Contains(ip) {
				return true
			}
		}
	} else {
		// Check IPv6
		for _, cidr := range privateIPv6Ranges {
			_, ipNet, err := net.ParseCIDR(cidr)
			if err != nil {
				continue
			}
			if ipNet.Contains(ip) {
				return true
			}
		}
	}

	return false
}

// RateLimiter methods

// Allow checks if the client is allowed to make a request
func (r *RateLimiter) Allow(clientIP string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	b, exists := r.buckets[clientIP]

	if !exists {
		r.buckets[clientIP] = &bucket{
			count:      1,
			lastAccess: now,
		}
		return true
	}

	// Reset bucket if window has passed
	if now.Sub(b.lastAccess) >= r.window {
		b.count = 1
		b.lastAccess = now
		return true
	}

	// Check if limit exceeded
	if b.count >= r.maxRate {
		return false
	}

	// Increment counter
	b.count++
	b.lastAccess = now
	return true
}

// cleanup removes old buckets periodically
func (r *RateLimiter) cleanup() {
	for range r.cleanupT.C {
		r.mu.Lock()
		now := time.Now()
		for ip, b := range r.buckets {
			if now.Sub(b.lastAccess) > r.window*2 {
				delete(r.buckets, ip)
			}
		}
		r.mu.Unlock()
	}
}

// Stop stops the rate limiter cleanup goroutine
func (r *RateLimiter) Stop() {
	if r.cleanupT != nil {
		r.cleanupT.Stop()
	}
}

// Stop stops the validator
func (v *Validator) Stop() {
	if v.rateLimiter != nil {
		v.rateLimiter.Stop()
	}
}
