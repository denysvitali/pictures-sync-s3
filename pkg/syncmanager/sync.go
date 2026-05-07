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
	go m.monitorProgress(ctx, stats, totalFiles, totalBytes, 0, done)

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

// retry runs op with the project's standard backoff policy: a fixed 5s delay
// for the first initialRetries attempts, then exponential backoff capped at
// 120s, up to maxAttempts total. The op is only retried when isRetryable
// returns true. Context cancellation aborts immediately.
func retry(ctx context.Context, op func(attempt int) error, isRetryable func(error) bool, label string) error {
	const maxAttempts = 10
	const initialRetries = 3
	backoff := 5 * time.Second
	var lastErr error

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		select {
		case <-ctx.Done():
			return fmt.Errorf("sync cancelled")
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
			delay = 5 * time.Second
			log.Printf("%s attempt %d/%d failed with retryable error: %v. Retrying in %v...",
				label, attempt, maxAttempts, err, delay)
		} else {
			delay = backoff
			log.Printf("%s attempt %d/%d failed with retryable error: %v. Retrying with exponential backoff in %v...",
				label, attempt, maxAttempts, err, delay)
			backoff *= 2
			if backoff > 120*time.Second {
				backoff = 120 * time.Second
			}
		}

		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return fmt.Errorf("sync cancelled during retry wait")
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
