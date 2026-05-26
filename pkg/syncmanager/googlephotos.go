package syncmanager

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"time"

	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/accounting"
	rcsync "github.com/rclone/rclone/fs/sync"
)

// SyncCardsToGooglePhotos syncs all card directories from the B2 remote to
// Google Photos using rclone's googlephotos backend. It lists cards from the
// B2 remote, then syncs each card's DCIM folder to gphotos:card-{id}.
func (m *Manager) SyncCardsToGooglePhotos(ctx context.Context) error {
	m.mu.Lock()
	if m.googlePhotosRunning {
		m.mu.Unlock()
		return fmt.Errorf("Google Photos sync already in progress")
	}
	m.googlePhotosRunning = true
	m.mu.Unlock()

	defer func() {
		m.mu.Lock()
		m.googlePhotosRunning = false
		m.googlePhotosCancel = nil
		m.mu.Unlock()
	}()

	// Hold rcloneConfigMu for the entire sync.
	rcloneConfigMu.Lock()
	defer rcloneConfigMu.Unlock()

	if err := m.loadRcloneConfigLocked(); err != nil {
		return err
	}

	// List cards from the B2 remote.
	cards, err := m.listCardIDsLocked(ctx)
	if err != nil {
		return fmt.Errorf("failed to list cards: %w", err)
	}

	if len(cards) == 0 {
		log.Println("No cards found on remote for Google Photos sync")
		m.setGooglePhotosProgress(Progress{Status: "completed"})
		return nil
	}

	log.Printf("Starting Google Photos sync for %d card(s)", len(cards))

	// Create cancellable context.
	syncCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	m.mu.Lock()
	m.googlePhotosCancel = cancel
	m.mu.Unlock()

	// Track overall progress.
	m.startTime = time.Now()
	totalFiles := 0
	totalBytes := int64(0)

	// Count total files and bytes across all cards first.
	for _, card := range cards {
		srcPath := filepath.Join(m.remoteName+":"+m.remotePath, card.Name, "DCIM")
		srcFs, err := fs.NewFs(syncCtx, srcPath)
		if err != nil {
			log.Printf("Warning: failed to open source fs for card %s: %v", card.Name, err)
			continue
		}
		count, bytes := m.countFiles(syncCtx, srcFs)
		totalFiles += count
		totalBytes += bytes
	}

	if totalFiles == 0 {
		log.Println("No files found to sync to Google Photos")
		m.setGooglePhotosProgress(Progress{Status: "completed"})
		return nil
	}

	log.Printf("Google Photos sync: %d files, %s total", totalFiles, formatBytes(totalBytes))

	// Set up stats group for progress tracking.
	syncCtx = accounting.WithStatsGroup(syncCtx, fmt.Sprintf("gphotos-sync-%d", time.Now().UnixNano()))
	stats := accounting.Stats(syncCtx)

	// Start progress monitoring.
	done := make(chan struct{})
	monitorDone := make(chan struct{})
	go func() {
		defer close(monitorDone)
		m.monitorGooglePhotosProgress(syncCtx, stats, totalFiles, totalBytes, done)
	}()

	var lastErr error
	processedFiles := 0
	processedBytes := int64(0)

	for i, card := range cards {
		select {
		case <-syncCtx.Done():
			lastErr = fmt.Errorf("Google Photos sync cancelled")
			break
		default:
		}

		if lastErr != nil {
			break
		}

		srcPath := filepath.Join(m.remoteName+":"+m.remotePath, card.Name, "DCIM")
		dstPath := m.googlePhotosRemoteName + ":" + card.Name

		log.Printf("Google Photos sync: card %d/%d (%s)", i+1, len(cards), card.Name)
		m.setGooglePhotosProgress(Progress{
			Status:           "syncing",
			CurrentFile:      fmt.Sprintf("Card %s", card.Name),
			TransferredFiles: processedFiles,
			TotalFiles:       totalFiles,
			BytesTransferred: processedBytes,
		})

		cardFiles, cardBytes, err := m.syncCardToGooglePhotos(syncCtx, srcPath, dstPath)
		processedFiles += cardFiles
		processedBytes += cardBytes

		if err != nil {
			log.Printf("Warning: failed to sync card %s to Google Photos: %v", card.Name, err)
			lastErr = err
		}
	}

	// Stop progress monitoring.
	close(done)
	<-monitorDone

	if lastErr != nil {
		m.setGooglePhotosProgress(Progress{
			Status:           "error",
			TransferredFiles: processedFiles,
			TotalFiles:       totalFiles,
			BytesTransferred: processedBytes,
		})
		return lastErr
	}

	m.setGooglePhotosProgress(Progress{
		Status:           "completed",
		TransferredFiles: processedFiles,
		TotalFiles:       totalFiles,
		BytesTransferred: processedBytes,
	})
	log.Println("Google Photos sync completed successfully")
	return nil
}

// syncCardToGooglePhotos syncs a single card's DCIM folder to Google Photos.
// Returns the number of files transferred and bytes transferred for this card.
func (m *Manager) syncCardToGooglePhotos(ctx context.Context, srcPath, dstPath string) (int, int64, error) {
	srcFs, err := fs.NewFs(ctx, srcPath)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to create source filesystem: %w", err)
	}

	dstFs, err := fs.NewFs(ctx, dstPath)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to create destination filesystem: %w", err)
	}

	// Set up config with fewer parallel transfers for Google Photos rate limits.
	ci := fs.GetConfig(ctx)
	ci.Transfers = 1
	ci.Checkers = 2

	// Count files before sync.
	beforeCount, beforeBytes := m.countFiles(ctx, srcFs)

	if err := rcsync.Sync(ctx, dstFs, srcFs, false); err != nil {
		return 0, 0, err
	}

	return beforeCount, beforeBytes, nil
}

// listCardIDsLocked lists card directories from the B2 remote.
// Caller MUST hold rcloneConfigMu.
func (m *Manager) listCardIDsLocked(ctx context.Context) ([]FileInfo, error) {
	remoteFs, err := fs.NewFs(ctx, m.remoteName+":"+m.remotePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open remote filesystem: %w", err)
	}

	entries, err := remoteFs.List(ctx, "")
	if err != nil {
		return nil, err
	}

	var cards []FileInfo
	for _, entry := range entries {
		if dir, ok := entry.(fs.Directory); ok {
			name := dir.Remote()
			if strings.HasPrefix(name, "card-") {
				cards = append(cards, FileInfo{
					Name:  name,
					IsDir: true,
				})
			}
		}
	}

	return cards, nil
}

// countFiles counts files and total bytes in a filesystem.
func (m *Manager) countFiles(ctx context.Context, f fs.Fs) (int, int64) {
	entries, _ := f.List(ctx, "")
	var count int
	var bytes int64
	for _, entry := range entries {
		if _, ok := entry.(fs.Object); ok {
			count++
			bytes += entry.Size()
		}
	}
	return count, bytes
}

// monitorGooglePhotosProgress monitors Google Photos sync progress.
func (m *Manager) monitorGooglePhotosProgress(ctx context.Context, stats *accounting.StatsInfo, totalFiles int, totalBytes int64, done chan struct{}) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-done:
			return
		case <-ticker.C:
			progress := m.calculateProgress(stats, totalFiles, totalBytes, 0)
			m.setGooglePhotosProgress(progress)
		}
	}
}

// setGooglePhotosProgress updates the Google Photos progress atomically.
func (m *Manager) setGooglePhotosProgress(p Progress) {
	m.mu.Lock()
	m.googlePhotosProgress = p
	m.mu.Unlock()
}

// GetGooglePhotosProgress returns the current Google Photos sync progress.
func (m *Manager) GetGooglePhotosProgress() Progress {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.googlePhotosProgress
}

// IsGooglePhotosRunning returns true if a Google Photos sync is in progress.
func (m *Manager) IsGooglePhotosRunning() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.googlePhotosRunning
}

// CancelGooglePhotos cancels the current Google Photos sync operation.
func (m *Manager) CancelGooglePhotos() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.googlePhotosRunning {
		return fmt.Errorf("no Google Photos sync in progress")
	}

	if m.googlePhotosCancel != nil {
		m.googlePhotosCancel()
	}

	return nil
}

// formatBytes returns a human-readable byte count.
func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	units := []string{"KB", "MB", "GB", "TB", "PB"}
	return fmt.Sprintf("%.1f %s", float64(b)/float64(div), units[exp])
}
