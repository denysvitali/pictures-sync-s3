package syncmanager

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"time"

	"github.com/denysvitali/pictures-sync-s3/pkg/utils"
	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/accounting"
	rcsync "github.com/rclone/rclone/fs/sync"
)

// Sync synchronizes files from source to remote
func (m *Manager) Sync(sourcePath, cardID string, totalFiles int, totalBytes int64) error {
	// Sanitize cardID to prevent path traversal
	if err := validateCardID(cardID); err != nil {
		return fmt.Errorf("invalid card ID: %w", err)
	}

	m.mu.Lock()
	if m.isRunning {
		m.mu.Unlock()
		return fmt.Errorf("sync already in progress")
	}
	m.isRunning = true
	m.mu.Unlock()

	defer func() {
		m.mu.Lock()
		m.isRunning = false
		m.cancelFunc = nil
		m.mu.Unlock()
	}()

	// Load rclone config from custom path
	if err := m.loadRcloneConfig(); err != nil {
		return err
	}

	// Get size of files already synced to remote (for resume support)
	alreadySyncedBytes, err := m.GetRemoteSize(cardID)
	if err != nil {
		log.Printf("Warning: Failed to get remote size: %v", err)
		alreadySyncedBytes = 0
	}
	log.Printf("Already synced: %.2f MB of %.2f MB",
		float64(alreadySyncedBytes)/(1024*1024),
		float64(totalBytes)/(1024*1024))

	// Record start time for elapsed time calculation
	m.startTime = time.Now()

	// Create context with cancel
	ctx, cancel := context.WithCancel(context.Background())
	m.mu.Lock()
	m.cancelFunc = cancel
	m.mu.Unlock()

	defer cancel()

	// Construct destination path: remote:/photos/{cardID}/DCIM/
	destPath := filepath.Join(m.remoteName+":"+m.remotePath, cardID, "DCIM")

	log.Printf("Syncing from %s to %s", sourcePath, destPath)

	// Create source filesystem (local, doesn't need network)
	srcFs, err := fs.NewFs(ctx, sourcePath)
	if err != nil {
		return fmt.Errorf("failed to create source filesystem: %w", err)
	}

	// Set up config for progress tracking and parallel transfers
	ci := fs.GetConfig(ctx)
	ci.StatsOneLine = true
	ci.Progress = true
	ci.Transfers = m.transfers // Upload multiple files in parallel for better performance
	ci.Checkers = m.checkers    // Use multiple checkers to compare files in parallel

	// Create context-specific accounting stats (required for proper check/transfer tracking)
	// NewStats creates a new StatsInfo and embeds it in the context
	stats := accounting.NewStats(ctx)

	// Start progress monitoring
	done := make(chan struct{})
	go m.monitorProgress(ctx, stats, totalFiles, totalBytes, alreadySyncedBytes, done)

	// Perform sync operation with retry logic and exponential backoff
	// This now includes filesystem creation which may fail due to network issues
	log.Printf("Starting sync operation...")
	err = m.syncWithRetry(ctx, srcFs, destPath)

	// Stop progress monitoring
	close(done)

	if err != nil {
		return err
	}

	log.Printf("Sync completed successfully")

	// Upload JPG files to Google Photos if enabled
	if m.googlePhotosEnabled && m.googlePhotosRemoteName != "" {
		log.Printf("Starting Google Photos upload for JPG files...")
		if err := m.uploadToGooglePhotos(ctx, sourcePath, cardID); err != nil {
			log.Printf("Warning: Google Photos upload failed: %v", err)
			// Don't return error - main sync succeeded
		} else {
			log.Printf("Google Photos upload completed successfully")
		}
	}

	return nil
}

// syncWithRetry performs the sync operation with retry logic
func (m *Manager) syncWithRetry(ctx context.Context, srcFs fs.Fs, destPath string) error {
	const maxAttempts = 10
	const initialRetries = 3 // First 3 attempts use fixed delay
	var lastErr error
	backoff := 5 * time.Second

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		// Check if context was cancelled
		select {
		case <-ctx.Done():
			return fmt.Errorf("sync cancelled")
		default:
		}

		// Create destination filesystem (may fail due to network issues)
		dstFs, err := fs.NewFs(ctx, destPath)
		if err != nil {
			lastErr = fmt.Errorf("failed to create destination filesystem: %w", err)

			// Check if it's a network error and we should retry
			if attempt < maxAttempts && utils.IsRetryableNetworkError(lastErr) {
				// Calculate delay: fixed 5s for first 3 attempts, then exponential backoff
				var delay time.Duration
				if attempt <= initialRetries {
					delay = 5 * time.Second
					log.Printf("Destination filesystem creation attempt %d/%d failed with retryable error: %v. Retrying in %v...",
						attempt, maxAttempts, err, delay)
				} else {
					delay = backoff
					log.Printf("Destination filesystem creation attempt %d/%d failed with retryable error: %v. Retrying with exponential backoff in %v...",
						attempt, maxAttempts, err, delay)

					// Exponential backoff: 5s, 10s, 20s, 40s, 80s (capped at 120s)
					backoff *= 2
					if backoff > 120*time.Second {
						backoff = 120 * time.Second
					}
				}

				// Wait before retry (with context cancellation check)
				select {
				case <-time.After(delay):
					// Continue to next attempt
				case <-ctx.Done():
					return fmt.Errorf("sync cancelled during retry wait")
				}
				continue
			}
			// Non-retryable error or last attempt
			return lastErr
		}

		// Now try the actual sync
		err = rcsync.Sync(ctx, dstFs, srcFs, false)
		if err == nil {
			// Success!
			return nil
		}

		lastErr = err

		// Check if it's a network error and we should retry
		if attempt < maxAttempts && isRetryableError(err) {
			// Calculate delay: fixed 5s for first 3 attempts, then exponential backoff
			var delay time.Duration
			if attempt <= initialRetries {
				delay = 5 * time.Second
				log.Printf("Sync attempt %d/%d failed with retryable error: %v. Retrying in %v...",
					attempt, maxAttempts, err, delay)
			} else {
				delay = backoff
				log.Printf("Sync attempt %d/%d failed with retryable error: %v. Retrying with exponential backoff in %v...",
					attempt, maxAttempts, err, delay)

				// Exponential backoff: 5s, 10s, 20s, 40s, 80s (capped at 120s)
				backoff *= 2
				if backoff > 120*time.Second {
					backoff = 120 * time.Second
				}
			}

			// Wait before retry (with context cancellation check)
			select {
			case <-time.After(delay):
				// Continue to next attempt
			case <-ctx.Done():
				return fmt.Errorf("sync cancelled during retry wait")
			}
		} else {
			// Non-retryable error or last attempt
			break
		}
	}

	return fmt.Errorf("sync failed after %d attempts: %w", maxAttempts, lastErr)
}

// GetRemoteSize returns the total size of files already on the remote for a given card ID
func (m *Manager) GetRemoteSize(cardID string) (int64, error) {
	// Sanitize cardID to prevent path traversal
	if err := validateCardID(cardID); err != nil {
		return 0, fmt.Errorf("invalid card ID: %w", err)
	}

	// Load rclone config from custom path
	if err := m.loadRcloneConfig(); err != nil {
		return 0, err
	}

	// OPTIMIZATION: Skip remote size calculation - it's too slow with many files
	// The remote size was only used for resume support, but rclone handles that internally
	// Listing thousands of files just to get total size can take minutes with large backups
	// Instead, return 0 and let rclone handle sync efficiently with its own internal tracking

	// Note: This means we can't show "X MB already synced" in logs,
	// but it's a worthwhile tradeoff for performance (minutes → seconds)

	// Rclone's sync algorithm handles resume/incremental sync efficiently:
	// - Only transfers files that don't exist or have changed
	// - Uses checksums to detect changes
	// - Handles partial transfers automatically

	return 0, nil
}
