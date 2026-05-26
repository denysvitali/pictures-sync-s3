package syncmanager

import (
	"context"
	"fmt"
	"log"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/denysvitali/pictures-sync-s3/pkg/utils"
	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/accounting"
	"github.com/rclone/rclone/fs/operations"
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
	m.setGooglePhotosProgress(Progress{
		Status:      "syncing",
		CurrentFile: "Preparing Google Photos sync",
	})

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

	// Pre-flight: verify the Google Photos remote exists in rclone config.
	gpRemote := m.googlePhotosRemoteName
	if gpRemote == "" {
		gpRemote = "gphotos"
	}
	_, err := fs.NewFs(ctx, gpRemote+":")
	if err != nil {
		m.setGooglePhotosProgress(Progress{Status: "error", Error: fmt.Sprintf("Google Photos remote %q is not configured. Complete the OAuth connection first.", gpRemote)})
		return fmt.Errorf("google photos remote %q not configured: %w", gpRemote, err)
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

	// Create context with a generous overall timeout so a hung backend
	// cannot block the sync forever.
	syncCtx, cancel := context.WithTimeout(ctx, 2*time.Hour)
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
		m.setGooglePhotosProgress(Progress{
			Status:      "syncing",
			CurrentFile: fmt.Sprintf("Counting card %s", card.Name),
		})
		srcPath := filepath.Join(m.remoteName+":"+m.remotePath, card.Name, "DCIM")
		log.Printf("Google Photos sync: counting files for card %s", card.Name)
		var srcFs fs.Fs
		err := retry(syncCtx, func(attempt int) error {
			f, err := fs.NewFs(syncCtx, srcPath)
			if err != nil {
				return fmt.Errorf("failed to open source fs for card %s: %w", card.Name, err)
			}
			srcFs = f
			return nil
		}, utils.IsRetryableNetworkError, fmt.Sprintf("B2 fs for card %s", card.Name))
		if err != nil {
			log.Printf("Warning: failed to open source fs for card %s: %v", card.Name, err)
			continue
		}
		count, bytes := m.countFiles(syncCtx, srcFs)
		log.Printf("Google Photos sync: card %s has %d files (%s)", card.Name, count, formatBytes(bytes))
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
		dstPath := gpRemote + ":album/" + card.Name

		log.Printf("Google Photos sync: card %d/%d (%s) — %s → %s", i+1, len(cards), card.Name, srcPath, dstPath)
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
			Error:            lastErr.Error(),
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
	// Create source filesystem with retry.
	var srcFs fs.Fs
	err := retry(ctx, func(attempt int) error {
		f, err := fs.NewFs(ctx, srcPath)
		if err != nil {
			return fmt.Errorf("failed to create source filesystem: %w", err)
		}
		srcFs = f
		return nil
	}, utils.IsRetryableNetworkError, "Source filesystem creation")
	if err != nil {
		return 0, 0, err
	}
	log.Printf("Google Photos sync: source fs ready (%s)", srcPath)

	// Create destination filesystem with retry.
	var dstFs fs.Fs
	err = retry(ctx, func(attempt int) error {
		f, err := fs.NewFs(ctx, dstPath)
		if err != nil {
			return fmt.Errorf("failed to create destination filesystem: %w", err)
		}
		dstFs = f
		return nil
	}, utils.IsRetryableNetworkError, "Destination filesystem creation")
	if err != nil {
		return 0, 0, err
	}
	log.Printf("Google Photos sync: destination fs ready (%s)", dstPath)

	// Set up config with fewer parallel transfers for Google Photos rate limits.
	ci := fs.GetConfig(ctx)
	ci.Transfers = 1
	ci.Checkers = 2

	// Copy files into the album root. Rclone's googlephotos backend treats
	// subdirectories under album/<name> as part of the album title, so preserving
	// source paths like DJI_001/file.jpg would create album "<name>/DJI_001".
	objects, beforeBytes := m.listObjects(ctx, srcFs)
	beforeCount := len(objects)
	log.Printf("Google Photos sync: %d files (%s) to upload", beforeCount, formatBytes(beforeBytes))

	basenameCounts := googlePhotosBasenameCounts(objects)
	usedNames := make(map[string]int, len(objects))
	existingNames := listExistingObjectRemotes(ctx, dstFs)
	for _, obj := range objects {
		dstRemote := googlePhotosFlatRemote(obj.Remote(), basenameCounts, usedNames)
		if _, exists := existingNames[dstRemote]; exists {
			continue
		}
		err = retry(ctx, func(attempt int) error {
			_, copyErr := operations.Copy(ctx, dstFs, nil, dstRemote, obj)
			return copyErr
		}, isRetryableError, fmt.Sprintf("Google Photos copy %s", obj.Remote()))
		if err != nil {
			return 0, 0, err
		}
		existingNames[dstRemote] = struct{}{}
	}
	log.Printf("Google Photos sync: upload completed for %s", dstPath)

	return beforeCount, beforeBytes, nil
}

func listExistingObjectRemotes(ctx context.Context, f fs.Fs) map[string]struct{} {
	remotes := make(map[string]struct{})
	entries, err := f.List(ctx, "")
	if err != nil {
		return remotes
	}
	for _, entry := range entries {
		if _, ok := entry.(fs.Object); ok {
			remote := entry.Remote()
			remotes[remote] = struct{}{}
			remotes[path.Base(strings.Trim(remote, "/"))] = struct{}{}
		}
	}
	return remotes
}

func googlePhotosBasenameCounts(objects []fs.Object) map[string]int {
	counts := make(map[string]int, len(objects))
	for _, obj := range objects {
		counts[path.Base(strings.Trim(obj.Remote(), "/"))]++
	}
	return counts
}

func googlePhotosFlatRemote(srcRemote string, basenameCounts map[string]int, usedNames map[string]int) string {
	cleaned := strings.Trim(strings.TrimSpace(srcRemote), "/")
	name := path.Base(cleaned)
	if name == "." || name == "/" || name == "" {
		name = "photo"
	}
	if basenameCounts[name] > 1 {
		name = strings.ReplaceAll(cleaned, "/", "_")
	}
	if name == "." || name == "/" || name == "" {
		name = "photo"
	}
	if used := usedNames[name]; used > 0 {
		ext := path.Ext(name)
		base := strings.TrimSuffix(name, ext)
		name = fmt.Sprintf("%s_%d%s", base, used+1, ext)
	}
	usedNames[name]++
	return name
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

// countFiles counts files and total bytes recursively in a filesystem.
func (m *Manager) countFiles(ctx context.Context, f fs.Fs) (int, int64) {
	return m.countFilesInDir(ctx, f, "")
}

// countFilesInDir recursively counts files in a directory.
func (m *Manager) countFilesInDir(ctx context.Context, f fs.Fs, dir string) (int, int64) {
	entries, err := f.List(ctx, dir)
	if err != nil {
		return 0, 0
	}
	var count int
	var bytes int64
	for _, entry := range entries {
		select {
		case <-ctx.Done():
			return count, bytes
		default:
		}
		if _, ok := entry.(fs.Object); ok {
			count++
			bytes += entry.Size()
		} else if dirEntry, ok := entry.(fs.Directory); ok {
			subCount, subBytes := m.countFilesInDir(ctx, f, dirEntry.Remote())
			count += subCount
			bytes += subBytes
		}
	}
	return count, bytes
}

func (m *Manager) listObjects(ctx context.Context, f fs.Fs) ([]fs.Object, int64) {
	return m.listObjectsInDir(ctx, f, "")
}

func (m *Manager) listObjectsInDir(ctx context.Context, f fs.Fs, dir string) ([]fs.Object, int64) {
	entries, err := f.List(ctx, dir)
	if err != nil {
		return nil, 0
	}

	var objects []fs.Object
	var bytes int64
	for _, entry := range entries {
		select {
		case <-ctx.Done():
			return objects, bytes
		default:
		}
		if obj, ok := entry.(fs.Object); ok {
			objects = append(objects, obj)
			bytes += obj.Size()
		} else if dirEntry, ok := entry.(fs.Directory); ok {
			subObjects, subBytes := m.listObjectsInDir(ctx, f, dirEntry.Remote())
			objects = append(objects, subObjects...)
			bytes += subBytes
		}
	}
	return objects, bytes
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
