package syncmanager

import (
	"context"
	"errors"
	"fmt"
	"log"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/denysvitali/pictures-sync-s3/pkg/metrics"
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

// GooglePhotosCardSummary describes one B2 source card for the Google Photos
// sync UI: how many photo/video files it holds, how many this app has already
// uploaded to Google Photos, and a few preview image paths (relative to the
// remote path) the UI can thumbnail.
type GooglePhotosCardSummary struct {
	Name             string   `json:"name"`
	TotalFiles       int      `json:"total_files"`
	TotalBytes       int64    `json:"total_bytes"`
	TransferredFiles int      `json:"transferred_files"`
	Preview          []string `json:"preview"`
}

// GetGooglePhotosCardSummary walks a single card's DCIM tree on the B2 remote to
// count photo/video files and gather up to 4 preview image paths, and reads the
// local upload ledger to report how many of those files have already been
// transferred to Google Photos. The B2 remote is the sync source, so this is
// the authoritative view of "what will be synced" and "how far along we are".
func (m *Manager) GetGooglePhotosCardSummary(ctx context.Context, cardName string) (GooglePhotosCardSummary, error) {
	cardName = strings.TrimSpace(cardName)
	if cardName == "" || strings.ContainsAny(cardName, "/\\") || strings.Contains(cardName, "..") || !strings.HasPrefix(cardName, "card-") {
		return GooglePhotosCardSummary{}, fmt.Errorf("invalid card name %q", cardName)
	}

	rcloneConfigMu.Lock()
	defer rcloneConfigMu.Unlock()
	if err := m.loadRcloneConfigLocked(); err != nil {
		return GooglePhotosCardSummary{}, err
	}

	srcPath := filepath.Join(m.remoteName+":"+m.remotePath, cardName, "DCIM")
	srcFs, err := fs.NewFs(ctx, srcPath)
	if err != nil {
		return GooglePhotosCardSummary{}, fmt.Errorf("failed to open card %s: %w", cardName, err)
	}

	preview := make([]string, 0, 4)
	count, totalBytes := m.gpSummaryWalk(ctx, srcFs, "", cardName, &preview)

	// Transferred count comes from the local upload ledger, whose keys are
	// "card-<id>/<path-relative-to-DCIM>" (see syncCardToGooglePhotos).
	transferred := 0
	state := loadGooglePhotosRcloneState()
	prefix := cardName + "/"
	for key := range state.Uploaded {
		if strings.HasPrefix(key, prefix) {
			transferred++
		}
	}
	if transferred > count {
		transferred = count
	}

	return GooglePhotosCardSummary{
		Name:             cardName,
		TotalFiles:       count,
		TotalBytes:       totalBytes,
		TransferredFiles: transferred,
		Preview:          preview,
	}, nil
}

// gpSummaryWalk recursively counts photo/video files under dir and appends up to
// 4 thumbnailable preview paths (relative to the remote path, including the card
// and DCIM prefix) into preview.
func (m *Manager) gpSummaryWalk(ctx context.Context, f fs.Fs, dir, cardName string, preview *[]string) (int, int64) {
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
			if isPhotoOrVideo(e.Remote()) {
				count++
				totalBytes += e.Size()
				if len(*preview) < 4 && isThumbnailablePhoto(e.Remote()) {
					*preview = append(*preview, path.Join(cardName, "DCIM", e.Remote()))
				}
			}
		case fs.Directory:
			c, b := m.gpSummaryWalk(ctx, f, e.Remote(), cardName, preview)
			count += c
			totalBytes += b
		}
	}
	return count, totalBytes
}

// isThumbnailablePhoto reports whether a file can be turned into a preview
// thumbnail by the server (formats the Go image stack decodes reliably).
func isThumbnailablePhoto(filename string) bool {
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".jpg", ".jpeg", ".png":
		return true
	}
	return false
}

// SyncCardsToGooglePhotos syncs all card directories from the B2 remote to
// Google Photos using rclone's googlephotos backend. It lists cards from the
// B2 remote, then syncs each card's DCIM folder to gphotos:card-{id}.
//
// When force is true the local upload-tracking state is ignored, so files
// recorded as already uploaded are re-uploaded. Use this to recover when the
// local state is stale (files marked uploaded but missing from Google Photos).
//
// cardFilter optionally restricts the sync to the named cards (album titles such
// as "card-00001"). When it is empty, every card on the remote is synced.
func (m *Manager) SyncCardsToGooglePhotos(ctx context.Context, force bool, cardFilter []string) error {
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

	// Restrict to the user-selected cards when a filter was provided. An empty
	// filter means "sync everything" (the default and first-run behaviour).
	if len(cardFilter) > 0 {
		allow := make(map[string]struct{}, len(cardFilter))
		for _, name := range cardFilter {
			allow[name] = struct{}{}
		}
		filtered := cards[:0]
		for _, card := range cards {
			if _, ok := allow[card.Name]; ok {
				filtered = append(filtered, card)
			}
		}
		cards = filtered
		log.Printf("Google Photos sync: restricted to %d selected card(s)", len(cards))
	}

	if len(cards) == 0 {
		log.Println("No cards found on remote for Google Photos sync")
		m.setGooglePhotosProgress(Progress{Status: "completed"})
		return nil
	}

	log.Printf("Starting Google Photos sync for %d card(s)", len(cards))

	// No global deadline: a full first-time backlog can be hundreds of GB
	// at Google Photos' per-stream throttle, easily exceeding any fixed
	// timeout. Hung backends are caught by rclone's per-call timeouts plus
	// our retry/backoff; user-initiated cancel still works via
	// googlePhotosCancel.
	syncCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	m.mu.Lock()
	m.googlePhotosCancel = cancel
	m.mu.Unlock()

	// Track overall progress.
	m.mu.Lock()
	m.startTime = time.Now()
	m.mu.Unlock()
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
		log.Printf("Google Photos sync: card %s has %d files (%s)", card.Name, count, utils.FormatBytes(bytes))
		totalFiles += count
		totalBytes += bytes
	}

	if totalFiles == 0 {
		log.Println("No files found to sync to Google Photos")
		m.setGooglePhotosProgress(Progress{Status: "completed"})
		return nil
	}

	log.Printf("Google Photos sync: %d files, %s total", totalFiles, utils.FormatBytes(totalBytes))

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

	var cardErrs []error
	cancelled := false
	processedFiles := 0
	processedBytes := int64(0)

	for i, card := range cards {
		if syncCtx.Err() != nil {
			cancelled = true
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

		cardFiles, cardBytes, err := m.syncCardToGooglePhotos(syncCtx, srcPath, dstPath, force)
		processedFiles += cardFiles
		processedBytes += cardBytes

		if err != nil {
			// A cancelled context stops the whole run; any other card failure is
			// isolated so the remaining cards still get a chance to sync. We
			// collect the errors and report them at the end.
			if errors.Is(err, context.Canceled) || syncCtx.Err() != nil {
				cancelled = true
				break
			}
			log.Printf("Warning: failed to sync card %s to Google Photos: %v", card.Name, err)
			cardErrs = append(cardErrs, fmt.Errorf("card %s: %w", card.Name, err))
		}
	}

	// Stop progress monitoring.
	close(done)
	<-monitorDone

	if cancelled {
		cardErrs = append(cardErrs, fmt.Errorf("Google Photos sync cancelled"))
	}

	if len(cardErrs) > 0 {
		err := errors.Join(cardErrs...)
		metrics.Inc("pictures_sync_error_total", map[string]string{"remote": "googlephotos", "reason": "unknown"})
		m.setGooglePhotosProgress(Progress{
			Status:           "error",
			TransferredFiles: processedFiles,
			TotalFiles:       totalFiles,
			BytesTransferred: processedBytes,
			Error:            err.Error(),
		})
		return err
	}

	metrics.Inc("pictures_sync_success_total", map[string]string{"remote": "googlephotos"})
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
func (m *Manager) syncCardToGooglePhotos(ctx context.Context, srcPath, dstPath string, force bool) (int, int64, error) {
	workers := m.googlePhotosTransferCount()

	// Align rclone config BEFORE creating dstFs: the googlephotos backend
	// reads --transfers at fs construction time to size its internal batcher
	// queue and pick the sync-mode batch size default. Setting it afterwards
	// (as we did previously) left batch_size derived from the default
	// --transfers of 4 — but our explicit batch_size override below makes
	// that moot. The Transfers/Checkers tweak stays for upload parallelism.
	ci := fs.GetConfig(ctx)
	ci.Transfers = workers
	if ci.Checkers < workers {
		ci.Checkers = workers
	}

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

	// Force the gphotos batcher to a fixed batch_size of 50 (the API max).
	// Without this, rclone's sync-mode batcher defaults batch_size to
	// --transfers, so workers=1 → batch length 1 → one batchCreate API call
	// per file. That serialised the whole sync at ~1 MB/s even on a 10 Gbps
	// link. With batch_size=50, the batcher's single commit goroutine
	// commits 50 items per round-trip while the upload phase stays
	// parallel, so there's still no concurrent batchCreate to race the
	// per-album server transaction and trigger 409 ABORTED.
	dstFsPath := injectRemoteOption(dstPath, "batch_size", "50")

	// Create destination filesystem with retry.
	var dstFs fs.Fs
	err = retry(ctx, func(attempt int) error {
		f, err := fs.NewFs(ctx, dstFsPath)
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

	// Flush any partial trailing batch before we return. atexit also handles
	// this, but we may have many cards in one run and shouldn't wait until
	// process exit to commit each card's final <50 files.
	defer func() {
		if sh, ok := dstFs.(fs.Shutdowner); ok {
			if err := sh.Shutdown(ctx); err != nil {
				log.Printf("Google Photos sync: shutdown returned %v", err)
			}
		}
	}()

	// Copy files into the album root. Rclone's googlephotos backend treats
	// subdirectories under album/<name> as part of the album title, so preserving
	// source paths like DJI_001/file.jpg would create album "<name>/DJI_001".
	// listObjects already filters to photo/video extensions during the walk.
	objects, beforeBytes := m.listObjects(ctx, srcFs)
	beforeCount := len(objects)
	log.Printf("Google Photos sync: %d files (%s) to upload", beforeCount, utils.FormatBytes(beforeBytes))

	// Load persistent upload state to avoid re-uploading files that Google Photos
	// may not list while processing (especially videos).
	uploadState := loadGooglePhotosRcloneState()
	albumName := path.Base(strings.Trim(dstPath, "/"))

	basenameCounts := googlePhotosBasenameCounts(objects)
	usedNames := make(map[string]int, len(objects))
	existingNames, listErr := listExistingObjectRemotes(ctx, dstFs)
	// Three outcomes, not two:
	//  1. Listing succeeded — trust it as the authoritative skip set.
	//  2. Listing failed with "directory not found" — the album does not exist
	//     on Google Photos, so provably ZERO files have been uploaded. Any local
	//     upload state for this album is therefore stale and must be ignored,
	//     otherwise every file is silently skipped and the album never gets
	//     created. (This is the bug that left whole cards unsynced: stale state
	//     recorded files as uploaded even though their album was gone.)
	//  3. Listing failed for any other (transient) reason — we can't confirm the
	//     album's contents, so fall back to the local upload state as the sole
	//     skip gate. Surface it loudly.
	listingOK := listErr == nil
	albumMissing := errors.Is(listErr, fs.ErrorDirNotFound)
	switch {
	case listingOK:
		log.Printf("Google Photos sync: album listing returned %d existing item(s)", len(existingNames))
	case albumMissing:
		log.Printf("Google Photos sync: album %s does not exist yet; local upload state is stale and will be ignored so every file re-uploads", albumName)
	default:
		log.Printf("Google Photos sync: WARNING album listing failed (%v); cannot verify uploads against the album, relying on local upload state only", listErr)
	}

	jobs := make([]googlePhotosCopyJob, 0, len(objects))
	stateSkipped := 0
	for _, obj := range objects {
		dstRemote := googlePhotosFlatRemote(obj.Remote(), basenameCounts, usedNames)
		if _, exists := existingNames[dstRemote]; exists {
			continue
		}
		// Skip if already uploaded per local state (path + size match), unless
		// a forced re-sync was requested or the album is provably absent.
		// Forcing ignores the local state so a user can recover from stale
		// tracking; a missing album means the state is definitively stale (the
		// files cannot exist if their album doesn't). Files genuinely already in
		// the album are still skipped above, and Google Photos de-duplicates
		// identical content, so re-uploading is safe.
		if !force && !albumMissing {
			stateKey := albumName + "/" + obj.Remote()
			if entry, ok := uploadState.Uploaded[stateKey]; ok && entry.Size == obj.Size() {
				stateSkipped++
				continue
			}
		}
		jobs = append(jobs, googlePhotosCopyJob{src: obj, dstRemote: dstRemote, albumName: albumName})
		existingNames[dstRemote] = struct{}{}
	}
	if force {
		log.Printf("Google Photos sync: force re-sync requested, ignoring local upload state for %s", albumName)
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
	var stateMu sync.Mutex
	var failMu sync.Mutex
	var failed int
	var lastFailErr error

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
					// A cancelled context aborts the whole card (user cancel or
					// shutdown). Any other failure is isolated to this one file:
					// log it, count it, and keep uploading the rest of the album
					// rather than tearing down the entire multi-card sync for a
					// single bad item. The file simply isn't recorded as
					// uploaded, so a later run retries it.
					if copyCtx.Err() != nil {
						return
					}
					log.Printf("Google Photos sync: giving up on %s after retries: %v", job.src.Remote(), err)
					failMu.Lock()
					failed++
					lastFailErr = err
					failMu.Unlock()
					continue
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

	// Context cancellation (user cancel / shutdown) takes precedence and is
	// reported as-is so the caller can stop the whole run.
	if err := ctx.Err(); err != nil {
		return 0, 0, err
	}
	if err := copyCtx.Err(); err != nil && err != context.Canceled {
		return 0, 0, err
	}

	// Per-file failures don't abort the card: the files that did upload are
	// recorded in state above and counted as transferred. Surface a summary
	// error so the card is reported as partially failed and a later run
	// retries the stragglers, but let the caller continue to the next card.
	if failed > 0 {
		log.Printf("Google Photos sync: %d of %d file(s) failed to upload to %s", failed, len(jobs), dstPath)
		return beforeCount - failed, beforeBytes, fmt.Errorf("%d of %d file(s) failed to upload to %s (last error: %w)", failed, len(jobs), dstPath, lastFailErr)
	}

	log.Printf("Google Photos sync: upload completed for %s", dstPath)

	return beforeCount, beforeBytes, nil
}

type googlePhotosCopyJob struct {
	src       fs.Object
	dstRemote string
	albumName string
}

// googlePhotosTransferCount returns the number of parallel upload workers to
// use for Google Photos transfers.
//
// The 409 ABORTED race is now avoided structurally: dstFsPath sets
// batch_size=50 and rclone's batcher has a single commit goroutine, so
// regardless of how many uploads run in parallel only one batchCreate is
// ever in flight against the album. That lets us honour the user-configured
// --transfers value from Settings. Google Photos throttles each upload
// connection to ~0.8 MB/s, so total throughput scales near-linearly with
// worker count until the pacer burst (100 req) or gigabit NIC is hit.
//
// Fallback to 8 when the setting is unset so existing installs without a
// configured value still benefit from the per-stream parallelism win,
// rather than collapsing back to 1 and the old 1 MB/s ceiling.
func (m *Manager) googlePhotosTransferCount() int {
	if m.transfers > 0 {
		return m.transfers
	}
	return 8
}

// injectRemoteOption rewrites an rclone remote spec like "gphotos:album/x"
// into "gphotos,key=value:album/x" so backend options can be set without
// editing rclone.conf. If the path doesn't have the expected "remote:path"
// shape it is returned unchanged.
func injectRemoteOption(remotePath, key, value string) string {
	colon := strings.Index(remotePath, ":")
	if colon <= 0 {
		return remotePath
	}
	return fmt.Sprintf("%s,%s=%s%s", remotePath[:colon], key, value, remotePath[colon:])
}

// listExistingObjectRemotes lists the objects already present in the
// destination album. The returned error is non-nil when the listing itself
// failed (e.g. the Google Photos API refusing to enumerate album contents).
// Callers must distinguish "listing succeeded and is empty" from "listing
// failed": treating a failed listing as an empty album silently hands all
// skip decisions to the local upload state, which can mask unsynced files.
func listExistingObjectRemotes(ctx context.Context, f fs.Fs) (map[string]struct{}, error) {
	remotes := make(map[string]struct{})
	entries, err := f.List(ctx, "")
	if err != nil {
		return remotes, err
	}
	for _, entry := range entries {
		if _, ok := entry.(fs.Object); ok {
			remote := entry.Remote()
			remotes[remote] = struct{}{}
			remotes[path.Base(strings.Trim(remote, "/"))] = struct{}{}
		}
	}
	return remotes, nil
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
