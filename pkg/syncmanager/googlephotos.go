package syncmanager

import (
	"context"
	"fmt"
	"log"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/denysvitali/pictures-sync-s3/pkg/state"
	"github.com/denysvitali/pictures-sync-s3/pkg/utils"
	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/accounting"
	"github.com/rclone/rclone/fs/operations"
)

var photoVideoExts = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true, ".gif": true,
	".heic": true, ".heif": true, ".webp": true, ".bmp": true,
	".tiff": true, ".tif": true, ".mp4": true, ".mov": true,
	".avi": true, ".mkv": true, ".wmv": true, ".flv": true,
	".m4v": true, ".3gp": true,
}

func isPhotoOrVideo(filename string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	return photoVideoExts[ext]
}

const googlePhotosRcloneStateFile = "google-photos-rclone-state.json"

type googlePhotosRcloneState struct {
	Uploaded map[string]googlePhotosUploadedFile `json:"uploaded"`
}

type googlePhotosUploadedFile struct {
	Size       int64     `json:"size"`
	UploadedAt time.Time `json:"uploaded_at"`
}

func loadGooglePhotosRcloneState() googlePhotosRcloneState {
	var s googlePhotosRcloneState
	defaultState := googlePhotosRcloneState{Uploaded: make(map[string]googlePhotosUploadedFile)}
	_ = utils.LoadJSON(filepath.Join(state.PermDir, googlePhotosRcloneStateFile), &s, defaultState)
	if s.Uploaded == nil {
		s.Uploaded = make(map[string]googlePhotosUploadedFile)
	}
	return s
}

func saveGooglePhotosRcloneState(s googlePhotosRcloneState) error {
	return utils.SaveJSON(filepath.Join(state.PermDir, googlePhotosRcloneStateFile), &s, 0600)
}

// ClearGooglePhotosAlbumState removes all upload tracking entries for a given
// album from the local rclone Google Photos state. This lets the next sync
// re-upload files after the album has been cleared on the remote.
func ClearGooglePhotosAlbumState(albumName string) error {
	s := loadGooglePhotosRcloneState()
	prefix := albumName + "/"
	changed := false
	for key := range s.Uploaded {
		if strings.HasPrefix(key, prefix) {
			delete(s.Uploaded, key)
			changed = true
		}
	}
	if changed {
		return saveGooglePhotosRcloneState(s)
	}
	return nil
}

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
			CurrentFile:      card.Name,
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

	workers := m.googlePhotosTransferCount()

	// Keep rclone's config aligned with the explicit worker pool below.
	ci := fs.GetConfig(ctx)
	ci.Transfers = workers
	if ci.Checkers < workers {
		ci.Checkers = workers
	}

	// Copy files into the album root. Rclone's googlephotos backend treats
	// subdirectories under album/<name> as part of the album title, so preserving
	// source paths like DJI_001/file.jpg would create album "<name>/DJI_001".
	// listObjects already filters to photo/video extensions during the walk.
	objects, beforeBytes := m.listObjects(ctx, srcFs)
	beforeCount := len(objects)
	log.Printf("Google Photos sync: %d files (%s) to upload", beforeCount, formatBytes(beforeBytes))

	// Load persistent upload state to avoid re-uploading files that Google Photos
	// may not list while processing (especially videos).
	uploadState := loadGooglePhotosRcloneState()
	albumName := path.Base(strings.Trim(dstPath, "/"))

	basenameCounts := googlePhotosBasenameCounts(objects)
	usedNames := make(map[string]int, len(objects))
	existingNames := listExistingObjectRemotes(ctx, dstFs)
	jobs := make([]googlePhotosCopyJob, 0, len(objects))
	stateSkipped := 0
	for _, obj := range objects {
		dstRemote := googlePhotosFlatRemote(obj.Remote(), basenameCounts, usedNames)
		if _, exists := existingNames[dstRemote]; exists {
			continue
		}
		// Skip if already uploaded per local state (path + size match).
		stateKey := albumName + "/" + obj.Remote()
		if entry, ok := uploadState.Uploaded[stateKey]; ok && entry.Size == obj.Size() {
			stateSkipped++
			continue
		}
		jobs = append(jobs, googlePhotosCopyJob{src: obj, dstRemote: dstRemote, albumName: albumName})
		existingNames[dstRemote] = struct{}{}
	}
	if stateSkipped > 0 {
		log.Printf("Google Photos sync: skipped %d file(s) already tracked in local state", stateSkipped)
	}
	if len(jobs) == 0 {
		log.Printf("Google Photos sync: all %d file(s) already exist in %s", beforeCount, dstPath)
		return beforeCount, beforeBytes, nil
	}
	if workers > len(jobs) {
		workers = len(jobs)
	}
	log.Printf("Google Photos sync: copying %d new file(s) to %s with %d parallel transfer(s)", len(jobs), dstPath, workers)

	copyCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	jobsCh := make(chan googlePhotosCopyJob)
	var wg sync.WaitGroup
	var errMu sync.Mutex
	var firstErr error
	var stateMu sync.Mutex

	recordErr := func(err error) {
		if err == nil {
			return
		}
		errMu.Lock()
		if firstErr == nil {
			firstErr = err
			cancel()
		}
		errMu.Unlock()
	}

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobsCh {
				if copyCtx.Err() != nil {
					return
				}
				err := retry(copyCtx, func(attempt int) error {
					_, copyErr := operations.Copy(copyCtx, dstFs, nil, job.dstRemote, job.src)
					return copyErr
				}, isRetryableError, fmt.Sprintf("Google Photos copy %s", job.src.Remote()))
				if err != nil {
					recordErr(err)
					return
				}
				// Mark as uploaded in local state.
				stateKey := job.albumName + "/" + job.src.Remote()
				stateMu.Lock()
				uploadState.Uploaded[stateKey] = googlePhotosUploadedFile{
					Size:       job.src.Size(),
					UploadedAt: time.Now(),
				}
				stateMu.Unlock()
			}
		}()
	}

sendJobs:
	for _, job := range jobs {
		select {
		case <-copyCtx.Done():
			break sendJobs
		case jobsCh <- job:
		}
	}
	close(jobsCh)
	wg.Wait()

	// Persist upload state so subsequent runs skip already-uploaded files.
	if err := saveGooglePhotosRcloneState(uploadState); err != nil {
		log.Printf("Google Photos sync: failed to save upload state: %v", err)
	}

	if firstErr != nil {
		return 0, 0, firstErr
	}
	if err := copyCtx.Err(); err != nil && err != context.Canceled {
		return 0, 0, err
	}
	if err := ctx.Err(); err != nil {
		return 0, 0, err
	}
	log.Printf("Google Photos sync: upload completed for %s", dstPath)

	return beforeCount, beforeBytes, nil
}

type googlePhotosCopyJob struct {
	src       fs.Object
	dstRemote string
	albumName string
}

// isRaspberryPi returns true when the binary is running on a Raspberry Pi.
// It reads /proc/cpuinfo and looks for the "Raspberry Pi" hardware identifier.
// The result is used to cap upload workers to a Pi-safe default.
func isRaspberryPi() bool {
	data, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		return false
	}
	return strings.Contains(string(data), "Raspberry Pi")
}

// googlePhotosTransferCount returns the number of parallel upload workers to
// use for Google Photos transfers.
//
// Defaults (no override set):
//   - Raspberry Pi detected: 2 workers (keeps the Pi responsive)
//   - All other platforms:    2 workers (conservative default)
//
// The caller can override via m.transfers (e.g. from settings or an env var).
// The hard cap is 4 to avoid hitting Google Photos rate limits.
func (m *Manager) googlePhotosTransferCount() int {
	m.mu.Lock()
	transfers := m.transfers
	m.mu.Unlock()

	const defaultWorkers = 2
	const maxWorkers = 4
	const piMaxWorkers = 2

	if transfers < 1 {
		transfers = defaultWorkers
	}
	if transfers > maxWorkers {
		transfers = maxWorkers
	}
	// On a Raspberry Pi, always cap to piMaxWorkers regardless of the override,
	// to avoid starving the OS under sustained I/O + network load.
	if isRaspberryPi() && transfers > piMaxWorkers {
		transfers = piMaxWorkers
	}
	return transfers
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

// countFilesInDir recursively counts photo/video files in a directory.
// It walks the directory tree incrementally — one rclone List call per
// directory — without materialising the full entry list into a slice,
// keeping peak memory proportional to directory depth rather than total
// file count (O(depth) instead of O(n)).
func (m *Manager) countFilesInDir(ctx context.Context, f fs.Fs, dir string) (int, int64) {
	entries, err := f.List(ctx, dir)
	if err != nil {
		return 0, 0
	}
	var count int
	var totalBytes int64
	for _, entry := range entries {
		select {
		case <-ctx.Done():
			return count, totalBytes
		default:
		}
		switch e := entry.(type) {
		case fs.Object:
			// Filter by extension during the walk — no secondary pass needed.
			if isPhotoOrVideo(e.Remote()) {
				count++
				totalBytes += e.Size()
			}
		case fs.Directory:
			subCount, subBytes := m.countFilesInDir(ctx, f, e.Remote())
			count += subCount
			totalBytes += subBytes
		}
	}
	return count, totalBytes
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
	var totalBytes int64
	for _, entry := range entries {
		select {
		case <-ctx.Done():
			return objects, totalBytes
		default:
		}
		switch e := entry.(type) {
		case fs.Object:
			// Filter by extension here so the caller never needs a second pass.
			if isPhotoOrVideo(e.Remote()) {
				objects = append(objects, e)
				totalBytes += e.Size()
			}
		case fs.Directory:
			subObjects, subBytes := m.listObjectsInDir(ctx, f, e.Remote())
			objects = append(objects, subObjects...)
			totalBytes += subBytes
		}
	}
	return objects, totalBytes
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
