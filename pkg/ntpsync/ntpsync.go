// Package ntpsync provides NTP time synchronization functionality.
// It queries multiple NTP servers with retry logic and exponential backoff to ensure
// accurate system time before photo sync operations begin.
package ntpsync

import (
	"context"
	"fmt"
	"log"
	"net"
	"time"

	"github.com/beevik/ntp"
	"golang.org/x/sys/unix"
)

const (
	defaultNTPTimeout       = 2 * time.Second
	systemClockSetThreshold = time.Second
	dnsFallbackTimeout      = 2 * time.Second
)

// earliestReasonableTime is the cutoff below which the system clock is
// considered uninitialised. Gokrazy boots at the Unix epoch until something
// (RTC, NTP, ...) sets the clock; anything before this date is treated as
// "needs sync".
var earliestReasonableTime = time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC)

var (
	servers = []string{
		"0.pool.ntp.org",
		"1.pool.ntp.org",
		"2.pool.ntp.org",
		"time.google.com",
	}
	// fallbackDNS is consulted when the system resolver fails (typical on
	// gokrazy when /etc/resolv.conf points at a stub on [::1]:53 with nothing
	// listening). These are well-known public resolvers.
	fallbackDNS = []string{
		"1.1.1.1:53",
		"8.8.8.8:53",
		"9.9.9.9:53",
	}
	now            = time.Now
	setSystemClock = setSystemClockLinux
)

// SyncResult describes a successful NTP synchronization attempt.
type SyncResult struct {
	Server        string
	SyncedTime    time.Time
	Offset        time.Duration
	SystemTimeSet bool
}

// IsClockSane reports whether the system clock is past a sensible epoch and
// therefore likely already synced (e.g. by a system-level NTP daemon or RTC).
// Callers can use this to skip redundant NTP queries.
func IsClockSane() bool {
	return now().After(earliestReasonableTime)
}

// SyncTime synchronizes system time with NTP servers.
// Returns error if sync fails after all configured servers.
func SyncTime() error {
	_, err := SyncTimeWithTimeout(defaultNTPTimeout)
	return err
}

// SyncTimeWithTimeout synchronizes system time using a bounded NTP query timeout.
func SyncTimeWithTimeout(timeout time.Duration) (*SyncResult, error) {
	var lastErr error
	for _, server := range servers {
		log.Printf("Attempting NTP sync with %s...", server)

		host, err := resolveHost(server, timeout)
		if err != nil {
			log.Printf("Failed to resolve %s: %v", server, err)
			lastErr = err
			continue
		}

		response, err := ntp.QueryWithOptions(host, ntp.QueryOptions{Timeout: timeout})
		if err != nil {
			log.Printf("Failed to sync with %s (%s): %v", server, host, err)
			lastErr = err
			continue
		}
		if err := response.Validate(); err != nil {
			log.Printf("Invalid NTP response from %s: %v", server, err)
			lastErr = err
			continue
		}

		offset := response.ClockOffset
		ntpTime := now().Add(offset)
		log.Printf("NTP sync successful with %s. Time offset: %v", server, offset)

		result := &SyncResult{
			Server:     server,
			SyncedTime: ntpTime,
			Offset:     offset,
		}

		if offset.Abs() > systemClockSetThreshold {
			log.Printf("Warning: System time differs from NTP by %v", offset)
			if err := SetSystemTime(ntpTime); err != nil {
				return nil, fmt.Errorf("set system time from NTP server %s: %w", server, err)
			}
			result.SystemTimeSet = true
			result.SyncedTime = now()
		}

		return result, nil
	}

	return nil, fmt.Errorf("failed to sync with any NTP server: %w", lastErr)
}

// resolveHost returns either the original host (if it is already an IP literal
// or the system resolver succeeds) or a public-DNS-resolved address. The result
// is in host:port form when a resolved IP is returned, or bare host otherwise
// (which lets the NTP library append its own default port).
func resolveHost(host string, timeout time.Duration) (string, error) {
	// If it already parses as an IP, no DNS needed.
	if net.ParseIP(host) != nil {
		return host, nil
	}

	// Try system resolver first.
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if addrs, err := net.DefaultResolver.LookupHost(ctx, host); err == nil && len(addrs) > 0 {
		return host, nil
	}

	// Fall back to public DNS. We return host:port so the NTP library connects
	// directly to the resolved address rather than re-running the failing
	// system resolver.
	for _, dns := range fallbackDNS {
		ip, err := lookupViaResolver(host, dns, timeout)
		if err != nil {
			log.Printf("DNS fallback %s failed for %s: %v", dns, host, err)
			continue
		}
		log.Printf("Resolved %s via fallback DNS %s -> %s", host, dns, ip)
		return net.JoinHostPort(ip, "123"), nil
	}

	return "", fmt.Errorf("hostname resolution failed for %s (system resolver and public DNS fallbacks)", host)
}

func lookupViaResolver(host, server string, timeout time.Duration) (string, error) {
	r := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			d := net.Dialer{Timeout: dnsFallbackTimeout}
			return d.DialContext(ctx, "udp", server)
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	addrs, err := r.LookupHost(ctx, host)
	if err != nil {
		return "", err
	}
	if len(addrs) == 0 {
		return "", fmt.Errorf("no addresses returned")
	}
	return addrs[0], nil
}

// SetSystemTime sets the system realtime clock to the supplied timestamp.
func SetSystemTime(t time.Time) error {
	if t.IsZero() {
		return fmt.Errorf("time must not be zero")
	}
	return setSystemClock(t)
}

func setSystemClockLinux(t time.Time) error {
	ts := unix.NsecToTimespec(t.UnixNano())
	return unix.ClockSettime(unix.CLOCK_REALTIME, &ts)
}

// EnsureTimeSync ensures time is synchronized before proceeding
// Retries with backoff if needed
func EnsureTimeSync(maxAttempts int) error {
	backoff := time.Second

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		_, err := SyncTimeWithTimeout(defaultNTPTimeout)
		if err == nil {
			return nil
		}

		if attempt < maxAttempts {
			log.Printf("NTP sync attempt %d/%d failed: %v. Retrying in %v...",
				attempt, maxAttempts, err, backoff)
			time.Sleep(backoff)

			// Exponential backoff: 1s, 2s, 4s, 8s, 16s, 32s, 64s (capped at 64s)
			backoff *= 2
			if backoff > 64*time.Second {
				backoff = 64 * time.Second
			}
		}
	}

	return fmt.Errorf("failed to sync time after %d attempts", maxAttempts)
}
