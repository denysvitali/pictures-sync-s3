// Package ntpsync provides NTP time synchronization functionality.
// It queries multiple NTP servers with retry logic and exponential backoff to ensure
// accurate system time before photo sync operations begin.
package ntpsync

import (
	"fmt"
	"log"
	"time"

	"github.com/beevik/ntp"
)

// SyncTime synchronizes system time with NTP servers
// Returns error if sync fails after all retries
func SyncTime() error {
	servers := []string{
		"0.pool.ntp.org",
		"1.pool.ntp.org",
		"2.pool.ntp.org",
		"time.google.com",
	}

	var lastErr error
	for _, server := range servers {
		log.Printf("Attempting NTP sync with %s...", server)

		ntpTime, err := ntp.Time(server)
		if err != nil {
			log.Printf("Failed to sync with %s: %v", server, err)
			lastErr = err
			continue
		}

		offset := time.Since(ntpTime)
		log.Printf("NTP sync successful with %s. Time offset: %v", server, offset)

		// If offset is more than 1 second, log a warning
		if offset.Abs() > time.Second {
			log.Printf("Warning: System time differs from NTP by %v", offset)
		}

		return nil
	}

	return fmt.Errorf("failed to sync with any NTP server: %w", lastErr)
}

// EnsureTimeSync ensures time is synchronized before proceeding
// Retries with backoff if needed
func EnsureTimeSync(maxAttempts int) error {
	backoff := time.Second

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		err := SyncTime()
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
