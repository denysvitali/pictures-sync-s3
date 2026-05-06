// Package ntpsync provides NTP time synchronization functionality.
// It queries multiple NTP servers with retry logic and exponential backoff to ensure
// accurate system time before photo sync operations begin.
package ntpsync

import (
	"fmt"
	"log"
	"time"

	"github.com/beevik/ntp"
	"golang.org/x/sys/unix"
)

const (
	defaultNTPTimeout       = 2 * time.Second
	systemClockSetThreshold = time.Second
)

var (
	servers = []string{
		"0.pool.ntp.org",
		"1.pool.ntp.org",
		"2.pool.ntp.org",
		"time.google.com",
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

		response, err := ntp.QueryWithOptions(server, ntp.QueryOptions{Timeout: timeout})
		if err != nil {
			log.Printf("Failed to sync with %s: %v", server, err)
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
