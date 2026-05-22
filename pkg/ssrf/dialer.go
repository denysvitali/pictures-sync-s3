package ssrf

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"
)

// Resolver is the subset of *net.Resolver used by SafeDial. It exists so
// tests can inject a controlled resolver without touching real DNS.
type Resolver interface {
	LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error)
}

// DialFunc matches the net.Dialer.DialContext signature so any custom
// underlying dialer (e.g. for unit tests with httptest.Server) can be
// plugged in.
type DialFunc func(ctx context.Context, network, address string) (net.Conn, error)

// SafeDialer wraps a net.Dialer with SSRF protection. It performs DNS
// resolution and IP validation in the same step, then connects directly to
// the validated IP — this prevents DNS-rebinding attacks where the resolver
// returns a public IP for validation and a private IP for the actual
// connection (a classic TOCTOU bypass).
//
// SafeDialer.DialContext is suitable for use as http.Transport.DialContext.
// Because the same dialer is reused across redirects by net/http, every
// redirect target is independently re-validated.
type SafeDialer struct {
	// Validator performs hostname/IP validation. Required.
	Validator *Validator
	// ClientIP is the upstream client identifier used for rate-limiting and
	// audit logging. May be empty (rate-limit bucket will be shared).
	ClientIP string
	// Resolver is used for hostname lookups. Defaults to net.DefaultResolver.
	Resolver Resolver
	// Timeout for each underlying dial. Defaults to 30s.
	Timeout time.Duration
	// KeepAlive for the underlying dialer. Defaults to 30s.
	KeepAlive time.Duration
	// DialFunc, if non-nil, is used to perform the actual TCP connection
	// to a validated IP. Defaults to a net.Dialer with the fields above.
	// Tests can substitute a function that redirects the dial to a
	// loopback httptest.Server while keeping production-grade validation.
	DialFunc DialFunc
}

// NewSafeDialer constructs a SafeDialer with sensible defaults.
func NewSafeDialer(v *Validator, clientIP string) *SafeDialer {
	return &SafeDialer{
		Validator: v,
		ClientIP:  clientIP,
		Resolver:  net.DefaultResolver,
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}
}

// DialContext resolves and validates address, then dials directly to a
// validated IP. The returned connection is established to the SAME IP that
// was validated, eliminating the resolve-once / connect-twice race.
//
// network must be "tcp", "tcp4" or "tcp6". address must be host:port (with
// host being either a literal IP or a hostname).
func (d *SafeDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	if d.Validator == nil {
		return nil, errors.New("ssrf: SafeDialer has nil Validator")
	}
	switch network {
	case "tcp", "tcp4", "tcp6":
	default:
		return nil, &ValidationError{
			Reason: fmt.Sprintf("network %q not allowed", network),
			Target: address,
		}
	}

	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, &ValidationError{
			Reason: fmt.Sprintf("invalid address: %v", err),
			Target: address,
		}
	}

	// Validate / resolve. If host is a literal IP, the validator handles
	// it directly; otherwise we resolve and validate every returned IP.
	var ips []net.IP
	if literal := net.ParseIP(host); literal != nil {
		ip, vErr := d.Validator.ValidateIP(host, d.ClientIP)
		if vErr != nil {
			return nil, vErr
		}
		ips = []net.IP{ip}
	} else {
		resolved, vErr := d.lookupAndValidate(ctx, host)
		if vErr != nil {
			return nil, vErr
		}
		ips = resolved
	}

	dialFn := d.DialFunc
	if dialFn == nil {
		nd := &net.Dialer{
			Timeout:   d.Timeout,
			KeepAlive: d.KeepAlive,
		}
		dialFn = nd.DialContext
	}

	// Connect directly to the validated IP (not by hostname) so the actual
	// destination cannot diverge from what we validated. Try each IP in
	// order until one succeeds; bubble up the last error.
	var lastErr error
	for _, ip := range ips {
		hostport := net.JoinHostPort(ip.String(), port)
		conn, cErr := dialFn(ctx, network, hostport)
		if cErr == nil {
			return conn, nil
		}
		lastErr = cErr
	}
	if lastErr == nil {
		// No IPs (should not happen — validator rejects this case).
		return nil, &ValidationError{
			Reason: "no validated IPs to dial",
			Target: address,
		}
	}
	return nil, lastErr
}

// lookupAndValidate mirrors Validator.ValidateHostname but uses the
// injectable Resolver so the same DNS reply is the one we validate AND
// connect to (closing the TOCTOU window). The hostname-format and
// dangerous-name checks are reused via the validator.
func (d *SafeDialer) lookupAndValidate(ctx context.Context, hostname string) ([]net.IP, error) {
	if !d.Validator.rateLimiter.Allow(d.ClientIP) {
		return nil, &ValidationError{
			Reason: "rate limit exceeded",
			Target: hostname,
		}
	}
	if err := validateHostnameFormat(hostname); err != nil {
		return nil, err
	}
	if err := checkDangerousHostnames(hostname); err != nil {
		return nil, err
	}

	resolver := d.Resolver
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	addrs, err := resolver.LookupIPAddr(ctx, hostname)
	if err != nil {
		return nil, &ValidationError{
			Reason: fmt.Sprintf("DNS resolution failed: %v", err),
			Target: hostname,
		}
	}
	if len(addrs) == 0 {
		return nil, &ValidationError{
			Reason: "no IP addresses resolved",
			Target: hostname,
		}
	}
	ips := make([]net.IP, 0, len(addrs))
	for _, a := range addrs {
		if err := validateIP(a.IP, hostname); err != nil {
			return nil, err
		}
		ips = append(ips, a.IP)
	}
	return ips, nil
}
