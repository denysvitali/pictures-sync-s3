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

	// Hold rcloneConfigMu for the entire sync. rclone's package-level config
	// (config path, parsed config data) is mutated by SetConfigPath/Install
	// and read by fs.NewFs / operations during the sync, so we must serialize
	// against any concurrent rclone-config consumers (ListRemotes, ListFiles,
	// TestConnection, etc.) to avoid data races on those globals.
	rcloneConfigMu.Lock()
	defer rcloneConfigMu.Unlock()

	// Load rclone config from custom path
	if err := m.loadRcloneConfigLocked(); err != nil {
		return err
	}

	// Record start time for elapsed time calculation
	m.startTime = time.Now()

	// Create context with cancel
	ctx, cancel := context.WithCancel(context.Background())
	ctx = accounting.WithStatsGroup(ctx, fmt.Sprintf("pictures-sync-%d", time.Now().UnixNano()))
	stats := accounting.Stats(ctx)

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
	ci.Checkers = m.checkers   // Use multiple checkers to compare files in parallel

	// Start progress monitoring. We don't track "already synced bytes" — rclone
	// handles incremental/resume sync internally via checksums.
	done := make(chan struct{})
	monitorDone := make(chan struct{})
	go func() {
		defer close(monitorDone)
		m.monitorProgress(ctx, stats, totalFiles, totalBytes, 0, done)
	}()

	// Perform sync operation with retry logic and exponential backoff
	// This now includes filesystem creation which may fail due to network issues
	log.Printf("Starting sync operation...")
	err = m.syncWithRetry(ctx, srcFs, destPath)

	// Stop progress monitoring and wait for it to fully exit before returning.
	// Without this wait, the goroutine can still be executing broadcastProgress /
	// stateMgr.UpdateSyncProgress when Sync() returns, racing with the caller's
	// post-sync state writes (and with subscribers tearing down their channels).
	close(done)
	<-monitorDone

	if err != nil {
		return err
	}

	log.Printf("Sync completed successfully")

	// Upload JPG files to Google Photos if enabled
	if m.googlePhotosEnabled && m.googlePhotosRemoteName != "" {
		log.Printf("Starting Google Photos upload for JPG files...")
		if err := m.uploadToGooglePhotosLocked(ctx, sourcePath, cardID); err != nil {
			log.Printf("Warning: Google Photos upload failed: %v", err)
			// Don't return error - main sync succeeded
		} else {
			log.Printf("Google Photos upload completed successfully")
		}
	}

	return nil
}

// retry runs op with the project's standard backoff policy: a fixed 5s delay
// for the first initialRetries attempts, then exponential backoff capped at
// 120s, up to maxAttempts total. The op is only retried when isRetryable
// returns true. Context cancellation aborts immediately and the returned
// error wraps ctx.Err() so callers can use errors.Is(err, context.Canceled)
// (or context.DeadlineExceeded) to detect the cause.
func retry(ctx context.Context, op func(attempt int) error, isRetryable func(error) bool, label string) error {
	const maxAttempts = 10
	const initialRetries = 3
	const initialDelay = 5 * time.Second
	const maxBackoff = 120 * time.Second
	// backoff is the delay used for the FIRST exponential attempt
	// (attempt = initialRetries+1). It is doubled AFTER each exponential use,
	// so the sequence after the initial fixed-delay phase is 10s, 20s, 40s,
	// 80s, 120s, 120s, ... Starting at 10s here (rather than 5s) ensures the
	// first "exponential" delay is actually larger than the initial fixed
	// delay; otherwise attempts 1..4 would all wait the same 5s.
	backoff := 2 * initialDelay
	var lastErr error

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		select {
		case <-ctx.Done():
			return fmt.Errorf("sync cancelled: %w", ctx.Err())
		default:
		}

		err := op(attempt)
		if err == nil {
			return nil
		}
		lastErr = err

		if attempt >= maxAttempts || !isRetryable(err) {
			return lastErr
		}

		var delay time.Duration
		if attempt <= initialRetries {
			delay = initialDelay
			log.Printf("%s attempt %d/%d failed with retryable error: %v. Retrying in %v...",
				label, attempt, maxAttempts, err, delay)
		} else {
			delay = backoff
			log.Printf("%s attempt %d/%d failed with retryable error: %v. Retrying with exponential backoff in %v...",
				label, attempt, maxAttempts, err, delay)
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
		}

		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return fmt.Errorf("sync cancelled during retry wait: %w", ctx.Err())
		}
	}

	return fmt.Errorf("%s failed after %d attempts: %w", label, maxAttempts, lastErr)
}

// syncWithRetry performs the sync operation with retry logic
func (m *Manager) syncWithRetry(ctx context.Context, srcFs fs.Fs, destPath string) error {
	// Step 1: create destination filesystem (network errors retry).
	var dstFs fs.Fs
	err := retry(ctx, func(attempt int) error {
		f, err := fs.NewFs(ctx, destPath)
		if err != nil {
			return fmt.Errorf("failed to create destination filesystem: %w", err)
		}
		dstFs = f
		return nil
	}, utils.IsRetryableNetworkError, "Destination filesystem creation")
	if err != nil {
		return err
	}

	// Step 2: run the actual sync (network errors retry).
	return retry(ctx, func(attempt int) error {
		return rcsync.Sync(ctx, dstFs, srcFs, false)
	}, isRetryableError, "Sync")
}
