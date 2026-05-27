package googlephotos

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand"
	"mime"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/denysvitali/pictures-sync-s3/pkg/syncmanager"
)

const (
	defaultBatchSize        = 10 // Google Photos allows up to 50; smaller batches make album updates visible sooner.
	defaultUploadWorkers    = 3
	maxUploadWorkers        = 16
	prefetchQueueMultiplier = 2
	progressUpdateInterval  = 500 * time.Millisecond
	progressUpdateBytes     = 2 * 1024 * 1024
	googlePhotosAPITimeout  = 30 * time.Second
	maxRetryAttempts = 4
)

// SyncManager orchestrates syncing photos from B2 to Google Photos
type SyncManager struct {
	client           *Client
	syncMgr          SyncManagerMinimal
	store            *stateStore
	progress         *SyncProgress
	mu               sync.RWMutex
	options          syncOptions
	metrics          syncMetrics
	cb               circuitBreaker
	cancel           context.CancelFunc
	running          atomic.Bool
	dynamicBatchSize atomic.Int32 // persists batch-size tuning across cards within a run
}

type syncOptions struct {
	skipDuplicates    bool
	parallelMetadata  bool
	uploadWorkerFloor int
	batchSize         int
}

// SyncManagerMinimal is the minimal interface needed from syncmanager for Google Photos sync
type SyncManagerMinimal interface {
	ListCardIDs() ([]syncmanager.FileInfo, error)
	ListFiles(path string) ([]syncmanager.FileInfo, error)
	GetFile(path string, w io.Writer) error
}

type syncManagerWithDownloadTimeout interface {
	GetFileWithTimeout(path string, w io.Writer, timeout time.Duration) error
}

type mediaUploadResult struct {
	index    int
	file     syncmanager.FileInfo
	item     *NewMediaItem
	checksum string
	skipped  bool
	err      error
}

type indexedMediaFile struct {
	index int
	file  syncmanager.FileInfo
}

type progressReader struct {
	reader    io.Reader
	onRead    func(int64)
	readBytes int64
}

func (r *progressReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	if n > 0 {
		r.readBytes += int64(n)
		r.onRead(r.readBytes)
	}
	return n, err
}

// NewSyncManager creates a new sync manager for B2 to Google Photos
func NewSyncManager(client *Client, syncMgr SyncManagerMinimal) *SyncManager {
	store := newStateStore()
	if client != nil && client.tokenStore != nil && client.tokenStore.filePath != defaultTokenFile {
		store = newStateStoreAt(filepath.Join(filepath.Dir(client.tokenStore.filePath), googlePhotosStateFileName))
	}
	return &SyncManager{
		client:  client,
		syncMgr: syncMgr,
		store:   store,
		options: syncOptions{
			skipDuplicates:    true,
			parallelMetadata:  !envBool("GOOGLE_PHOTOS_NO_PARALLEL_METADATA"),
			uploadWorkerFloor: defaultUploadWorkers,
			batchSize:         defaultBatchSize,
		},
	}
}

// SetSkipDuplicates controls hash-based duplicate short-circuiting for future runs.
func (sm *SyncManager) SetSkipDuplicates(enabled bool) {
	sm.mu.Lock()
	sm.options.skipDuplicates = enabled
	if sm.progress != nil {
		sm.progress.SkipDuplicates = enabled
	}
	sm.mu.Unlock()
}

// IsRunning returns true if a sync is currently in progress
func (sm *SyncManager) IsRunning() bool {
	return sm.running.Load()
}

// Progress returns the current sync progress
func (sm *SyncManager) Progress() *SyncProgress {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	if sm.progress == nil {
		history, last := sm.store.summaries()
		return &SyncProgress{Status: "idle", SkipDuplicates: sm.options.skipDuplicates, History: history, LastSuccessfulSync: last}
	}
	// Return a copy
	p := *sm.progress
	p.History, p.LastSuccessfulSync = sm.store.summaries()
	return &p
}

// Cancel cancels the current sync operation
func (sm *SyncManager) Cancel() {
	sm.mu.RLock()
	cancel := sm.cancel
	sm.mu.RUnlock()
	if cancel != nil {
		cancel()
	}
}

// Sync performs a full sync from B2 to Google Photos
func (sm *SyncManager) Sync(ctx context.Context) error {
	if !sm.running.CompareAndSwap(false, true) {
		return fmt.Errorf("sync already in progress")
	}
	defer sm.running.Store(false)

	ctx, cancel := context.WithCancel(ctx)
	sm.mu.Lock()
	sm.cancel = cancel
	sm.mu.Unlock()
	defer func() {
		sm.mu.Lock()
		sm.cancel = nil
		sm.mu.Unlock()
	}()

	// Initialize progress
	startedAt := time.Now()
	sm.metrics = syncMetrics{startedAt: startedAt}
	history, lastSuccess := sm.store.summaries()
	sm.mu.Lock()
	sm.progress = &SyncProgress{
		Status:             "listing_cards",
		CurrentPhase:       "Discovering cards",
		StartedAt:          &startedAt,
		UpdatedAt:          &startedAt,
		SortDescription:    "Files are sorted by modification time before upload",
		SkipDuplicates:     sm.options.skipDuplicates,
		StageTimeline:      initialStages(startedAt),
		History:            history,
		LastSuccessfulSync: lastSuccess,
		Warning:            "Native Google Photos uploads create Card <id> albums; rclone Google Photos remotes may use different album/path behavior.",
	}
	sm.mu.Unlock()

	// Get list of cards from B2
	cards, err := sm.syncMgr.ListCardIDs()
	if err != nil {
		sm.setError(err)
		return fmt.Errorf("failed to list cards: %w", err)
	}

	// Seed dynamic batch size from configured baseline; tuneBatchSize will adapt
	// it upward across cards within this run.
	sm.dynamicBatchSize.Store(int32(sm.options.batchSize))

	// Bulk-preload albums: a single pagination through the user's albums fills
	// the per-card cache so each syncCard skips a per-card ListAlbums round-trip
	// chain. Best-effort — failures fall back to the per-card lookup.
	sm.preloadAlbumCache(ctx, cards)

	sm.completeStage("discover")
	sm.mu.Lock()
	sm.progress.TotalCards = len(cards)
	sm.progress.Status = "syncing"
	sm.progress.CurrentPhase = "Preparing cards"
	sm.progress.UpdatedAt = timePtr(time.Now())
	sm.mu.Unlock()

	log.Printf("[GooglePhotos] Starting sync of %d cards to Google Photos", len(cards))

	var totalUploaded, totalSkipped, totalFailed int
	var cardErrors []CardError

	for i, card := range cards {
		// Check for cancellation
		if ctx.Err() != nil {
			sm.setStatus("cancelled")
			sm.recordCancelledSummary(startedAt, totalUploaded, totalSkipped, totalFailed)
			return ctx.Err()
		}

		cardID := card.Name
		sm.mu.Lock()
		sm.progress.CurrentCard = i + 1
		sm.progress.CurrentCardID = cardID
		sm.progress.CurrentFile = ""
		sm.progress.CurrentFilePath = ""
		sm.progress.CurrentFileSize = 0
		sm.progress.CurrentFileIndex = 0
		sm.progress.CurrentFileBytesUploaded = 0
		sm.progress.CurrentFilePercent = 0
		sm.progress.CurrentCardFiles = 0
		sm.progress.BatchPendingFiles = 0
		sm.progress.CurrentPhase = "Listing files"
		sm.progress.UpdatedAt = timePtr(time.Now())
		sm.mu.Unlock()

		// Extract actual card ID from "card-{id}" directory name
		actualCardID := strings.TrimPrefix(cardID, "card-")
		if actualCardID == "" {
			actualCardID = cardID
		}

		uploaded, skipped, failed, err := sm.syncCard(ctx, actualCardID, cardID)
		totalUploaded += uploaded
		totalSkipped += skipped
		totalFailed += failed

		if err != nil {
			log.Printf("[GooglePhotos] Error syncing card %s: %v", cardID, err)
			cardErrors = append(cardErrors, CardError{
				CardID: actualCardID,
				Error:  err.Error(),
			})
			// Continue with other cards
		}
	}

	completedAt := time.Now()
	duration := completedAt.Sub(startedAt).Round(time.Second).String()
	sm.mu.Lock()
	sm.progress.UploadedFiles = totalUploaded
	sm.progress.SkippedFiles = totalSkipped
	sm.progress.FailedFiles = totalFailed
	sm.progress.CardErrors = cardErrors
	if len(cardErrors) > 0 {
		sm.progress.Status = "error"
		if len(cardErrors) == 1 {
			sm.progress.Error = fmt.Sprintf("Failed to sync card %s: %s", cardErrors[0].CardID, cardErrors[0].Error)
		} else {
			sm.progress.Error = fmt.Sprintf("Failed to sync %d cards", len(cardErrors))
		}
	} else {
		sm.progress.Status = "completed"
		sm.progress.Error = ""
	}
	sm.progress.CurrentFile = ""
	sm.progress.CurrentFilePath = ""
	sm.progress.CurrentFileSize = 0
	sm.progress.CurrentFileBytesUploaded = 0
	sm.progress.CurrentFilePercent = 0
	sm.progress.CurrentPhase = ""
	sm.progress.BatchPendingFiles = 0
	sm.progress.UpdatedAt = timePtr(completedAt)
	sm.mu.Unlock()
	status := "completed"
	if len(cardErrors) > 0 {
		status = "error"
	}
	summary := SyncRunSummary{
		StartedAt:      startedAt,
		CompletedAt:    completedAt,
		Duration:       duration,
		Status:         status,
		UploadedFiles:  totalUploaded,
		SkippedFiles:   totalSkipped,
		FailedFiles:    totalFailed,
		ProcessedBytes: sm.Progress().ProcessedBytes,
	}
	if status == "error" {
		summary.Error = sm.Progress().Error
	}
	sm.store.addSummary(summary)
	sm.store.flush()

	log.Printf("[GooglePhotos] Sync complete: %d uploaded, %d skipped, %d failed", totalUploaded, totalSkipped, totalFailed)
	return nil
}

// preloadAlbumCache populates the album-ID cache for every card-* album in one
// pagination, so individual syncCard calls don't each pay a full ListAlbums.
func (sm *SyncManager) preloadAlbumCache(ctx context.Context, cards []syncmanager.FileInfo) {
	if sm.client == nil || len(cards) == 0 {
		return
	}
	needed := make(map[string]string, len(cards))
	for _, card := range cards {
		cardID := strings.TrimPrefix(card.Name, "card-")
		if cardID == "" {
			cardID = card.Name
		}
		if _, ok := sm.store.albumID(cardID); ok {
			continue
		}
		needed["card-"+cardID] = cardID
	}
	if len(needed) == 0 {
		return
	}
	listCtx, cancel := context.WithTimeout(ctx, googlePhotosAPITimeout*2)
	defer cancel()
	albums, err := sm.client.ListAlbumsContext(listCtx)
	if err != nil {
		log.Printf("[GooglePhotos] Album preload failed (will fall back per card): %v", err)
		return
	}
	for _, album := range albums {
		cardID, ok := needed[album.Title]
		if !ok {
			continue
		}
		sm.store.putAlbum(cardID, album.Title, album.ID)
	}
}

// syncCard syncs all photos from a single card to Google Photos
func (sm *SyncManager) syncCard(ctx context.Context, cardID, cardDirName string) (uploaded, skipped, failed int, err error) {
	// Always cancel the worker pipeline on return so any blocked goroutines
	// unwind even if we exit through an error path.
	ctx, cancelWorkers := context.WithCancel(ctx)
	defer cancelWorkers()

	// List all media in the card's DCIM directory before creating an album.
	// Camera cards commonly store files under DCIM/100CANON, DCIM/100MSDCF, etc.
	dcimPath := filepath.Join(cardDirName, "DCIM")
	sm.startStage("discover")
	mediaFiles, skipped, err := sm.listMediaFiles(ctx, dcimPath)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("failed to list files: %w", err)
	}

	if len(mediaFiles) == 0 {
		log.Printf("[GooglePhotos] No media files found in card %s", cardID)
		return 0, skipped, 0, nil
	}
	sortMediaForUpload(mediaFiles)
	log.Printf("[GooglePhotos] Card %s: %d media files sorted by modification time", cardID, len(mediaFiles))

	cardBytes := totalFileSize(mediaFiles)
	sm.mu.Lock()
	sm.progress.CurrentCardFiles = len(mediaFiles)
	sm.progress.TotalFiles += len(mediaFiles)
	sm.progress.TotalBytes += cardBytes
	sm.progress.CurrentPhase = "Preparing album"
	sm.progress.UpdatedAt = timePtr(time.Now())
	sm.mu.Unlock()
	sm.setCardProgress(cardID, 0, len(mediaFiles), uploaded, skipped, failed, "Preparing album", 0)

	// Find or create album for this card only after there is something to upload.
	albumTitle := "card-" + cardID
	var album *Album
	if cachedID, ok := sm.store.albumID(cardID); ok {
		album = &Album{ID: cachedID, Title: albumTitle}
	} else {
		albumCtx, cancel := context.WithTimeout(ctx, googlePhotosAPITimeout)
		album, err = sm.client.FindAlbumByTitleContext(albumCtx, albumTitle)
		cancel()
		if err != nil {
			return 0, 0, 0, classifyGooglePhotosError(fmt.Errorf("failed to find album: %w", err))
		}
	}

	if album == nil {
		log.Printf("[GooglePhotos] Creating album: %s", albumTitle)
		albumCtx, cancel := context.WithTimeout(ctx, googlePhotosAPITimeout)
		album, err = sm.client.CreateAlbumContext(albumCtx, albumTitle)
		cancel()
		if err != nil {
			return 0, 0, 0, classifyGooglePhotosError(fmt.Errorf("failed to create album: %w", err))
		}
	}
	sm.store.putAlbum(cardID, albumTitle, album.ID)

	log.Printf("[GooglePhotos] Syncing %d files from card %s to album %s", len(mediaFiles), cardID, albumTitle)

	dynamicBatchSize := int(sm.dynamicBatchSize.Load())
	if dynamicBatchSize <= 0 {
		dynamicBatchSize = sm.options.batchSize
	}
	batch := make([]*NewMediaItem, 0, dynamicBatchSize)
	batchFiles := make([]syncmanager.FileInfo, 0, dynamicBatchSize)
	pendingResults := make(map[int]mediaUploadResult)
	nextResult := 0
	sm.startStage("upload")
	for result := range sm.uploadMediaItems(ctx, cardID, mediaFiles) {
		pendingResults[result.index] = result

		for {
			orderedResult, ok := pendingResults[nextResult]
			if !ok {
				break
			}
			delete(pendingResults, nextResult)
			nextResult++

			if orderedResult.skipped {
				skipped++
				sm.finishFileProgress(orderedResult.file, uploaded, skipped, failed)
				sm.incrementDuplicateSkipped()
				sm.setCardProgress(cardID, nextResult, len(mediaFiles), uploaded, skipped, failed, "Skipped duplicate", len(mediaFiles)-nextResult)
				continue
			}

			if orderedResult.err != nil {
				log.Printf("[GooglePhotos] Failed to upload %s: %v", orderedResult.file.Name, orderedResult.err)
				failed++
				sm.finishFileProgress(orderedResult.file, uploaded, skipped, failed)
				sm.setCardProgress(cardID, nextResult, len(mediaFiles), uploaded, skipped, failed, "Upload failed", len(mediaFiles)-nextResult)
				continue
			}

			batch = append(batch, orderedResult.item)
			batchFiles = append(batchFiles, orderedResult.file)
			sm.finishFileProgress(orderedResult.file, uploaded, skipped, failed)
			sm.setBatchPending(len(batch))
			sm.setCardProgress(cardID, nextResult, len(mediaFiles), uploaded, skipped, failed, "Queued for album", len(mediaFiles)-nextResult)

			if ctx.Err() != nil {
				return uploaded, skipped, failed, ctx.Err()
			}

			if len(batch) < dynamicBatchSize {
				continue
			}

			sm.setPhase("Adding to album")
			sm.startStage("batch_create")
			batchResult, err := sm.createBatch(ctx, album.ID, batch)
			if err != nil {
				log.Printf("[GooglePhotos] Failed to create batch in album %s: %v", albumTitle, err)
			}
			log.Printf("[GooglePhotos] Created media items in album %s: success=%d failed=%d", albumTitle, batchResult.successCount, batchResult.failCount)
			uploaded += batchResult.successCount
			failed += batchResult.failCount
			if len(batchResult.successes) > 0 {
				for i, file := range batchFiles {
					if i >= len(batchResult.successes) || !batchResult.successes[i] {
						continue
					}
					sm.store.markBatchDone(file.Path)
				}
			}
			dynamicBatchSize = tuneBatchSize(dynamicBatchSize, batchResult.successCount, batchResult.failCount, err)
			sm.dynamicBatchSize.Store(int32(dynamicBatchSize))
			batch = batch[:0]
			batchFiles = batchFiles[:0]
			sm.setCounts(uploaded, skipped, failed)
			sm.setBatchPending(0)
			// The 429-aware retry + circuit breaker already handle rate-limit
			// backoff; the unconditional post-batch sleep here was cargo-cult and
			// wasted ~10% of the album-add path's wall time.
		}
	}

	if ctx.Err() != nil {
		return uploaded, skipped, failed, ctx.Err()
	}

	// Create remaining items in batch
	if len(batch) > 0 {
		sm.setPhase("Adding to album")
		sm.startStage("batch_create")
		batchResult, err := sm.createBatch(ctx, album.ID, batch)
		if err != nil {
			log.Printf("[GooglePhotos] Failed to create final batch in album %s: %v", albumTitle, err)
		}
		log.Printf("[GooglePhotos] Created final media items in album %s: success=%d failed=%d", albumTitle, batchResult.successCount, batchResult.failCount)
		uploaded += batchResult.successCount
		failed += batchResult.failCount
		if len(batchResult.successes) > 0 {
			for i, file := range batchFiles {
				if i >= len(batchResult.successes) || !batchResult.successes[i] {
					continue
				}
				sm.store.markBatchDone(file.Path)
			}
		}
		sm.setCounts(uploaded, skipped, failed)
		sm.setBatchPending(0)
	}
	sm.completeStage("batch_create")
	sm.startStage("finalization")
	sm.completeStage("finalization")

	return uploaded, skipped, failed, nil
}

func (sm *SyncManager) uploadMediaItems(ctx context.Context, cardID string, mediaFiles []syncmanager.FileInfo) <-chan mediaUploadResult {
	workerCount := sm.adaptiveUploadWorkers(len(mediaFiles))
	if len(mediaFiles) < workerCount {
		workerCount = len(mediaFiles)
	}
	if workerCount < 1 {
		workerCount = 1
	}
	queueSize := workerCount * prefetchQueueMultiplier
	if queueSize > len(mediaFiles) {
		queueSize = len(mediaFiles)
	}
	if queueSize < 1 {
		queueSize = 1
	}
	results := make(chan mediaUploadResult, queueSize)
	jobs := make(chan indexedMediaFile, queueSize)
	sm.updateBackendMetrics(queueSize, 0)

	go func() {
		for fileIndex, file := range mediaFiles {
			job := indexedMediaFile{index: fileIndex, file: file}
			select {
			case jobs <- job:
			case <-ctx.Done():
				close(jobs)
				return
			}
		}
		close(jobs)
	}()

	var wg sync.WaitGroup
	wg.Add(workerCount)
	for range workerCount {
		go func() {
			defer wg.Done()
			for job := range jobs {
				if ctx.Err() != nil {
					return
				}

				file := job.file
				sm.startFileProgress(job.index, len(mediaFiles), cardID, file)

				item, checksum, skipped, err := sm.uploadMediaItem(ctx, file)
				result := mediaUploadResult{
					index:    job.index,
					file:     file,
					item:     item,
					checksum: checksum,
					skipped:  skipped,
					err:      err,
				}
				select {
				case results <- result:
				case <-ctx.Done():
					return
				}
			}
		}()
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	return results
}

func (sm *SyncManager) startFileProgress(fileIndex, totalFiles int, cardID string, file syncmanager.FileInfo) {
	sm.startStage("download/copy")
	sm.mu.Lock()
	sm.progress.CurrentFile = file.Name
	sm.progress.CurrentFilePath = file.Path
	sm.progress.CurrentFileSize = file.Size
	sm.progress.CurrentFileIndex = fileIndex + 1
	sm.progress.CurrentFileBytesUploaded = 0
	sm.progress.CurrentFilePercent = 0
	sm.progress.CurrentPhase = "Downloading"
	sm.progress.UpdatedAt = timePtr(time.Now())
	sm.mu.Unlock()

	log.Printf("[GooglePhotos] Uploading file %d/%d from card %s: %s (%d bytes)", fileIndex+1, totalFiles, cardID, file.Path, file.Size)
}

func (sm *SyncManager) uploadMediaItem(ctx context.Context, file syncmanager.FileInfo) (*NewMediaItem, string, bool, error) {
	if sm.options.skipDuplicates {
		if meta, ok := sm.store.fileMeta(file.Path); ok && meta.Uploaded && meta.Checksum != "" && meta.Size == file.Size && meta.ModTime.Equal(file.ModTime) {
			log.Printf("[GooglePhotos] Skipping duplicate already uploaded file: %s", file.Path)
			return nil, meta.Checksum, true, nil
		}
	}
	if !sm.allowRequest() {
		return nil, "", false, sm.circuitBreakerOpenError()
	}

	fileData, fileSize, checksum, cleanup, err := sm.downloadFile(file)
	if err != nil {
		return nil, "", false, fmt.Errorf("download %s: %w", file.Path, err)
	}
	defer cleanup()

	mimeType := mime.TypeByExtension(filepath.Ext(file.Name))
	sm.store.putFileMeta(cachedFileMetadata{
		Path:     file.Path,
		Name:     file.Name,
		Size:     file.Size,
		ModTime:  file.ModTime,
		Checksum: checksum,
		Mime:     mimeType,
	})
	if sm.options.skipDuplicates && sm.store.uploadedChecksum(checksum) {
		log.Printf("[GooglePhotos] Skipping hash duplicate before upload: %s", file.Path)
		return nil, checksum, true, nil
	}

	if cached, ok := sm.store.uploadToken(file.Path); ok && cached.Checksum == checksum && cached.Token != "" && !cached.BatchDone {
		return &NewMediaItem{
			SimpleMediaItem: &SimpleMediaItem{
				UploadToken: cached.Token,
				FileName:    file.Name,
			},
		}, checksum, false, nil
	}

	sm.setUploadProgress(file.Name, file.Path, file.Size, 0)
	progress := newCoalescedProgress(func(uploadedBytes int64) {
		sm.setUploadProgress(file.Name, file.Path, file.Size, uploadedBytes)
	})
	uploadReader := &progressReader{
		reader: fileData,
		onRead: progress.update,
	}
	started := time.Now()
	uploadToken, err := retry(ctx, maxRetryAttempts, func(attempt int) (string, error) {
		if attempt > 0 {
			if _, seekErr := fileData.Seek(0, io.SeekStart); seekErr != nil {
				return "", seekErr
			}
			uploadReader.readBytes = 0
		}
		uploadCtx, cancel := context.WithTimeout(ctx, googlePhotosTransferTimeout(fileSize))
		defer cancel()
		return sm.client.UploadMediaReaderContext(uploadCtx, uploadReader, fileSize, file.Name)
	}, sm.setRetryStatus)
	progress.flush()
	sm.recordUploadLatency(time.Since(started), fileSize)
	if err != nil {
		sm.recordFailure()
		return nil, checksum, false, classifyGooglePhotosError(err)
	}
	sm.recordSuccess()
	sm.store.putUploadToken(file.Path, cachedUploadToken{
		Token:     uploadToken,
		FileName:  file.Name,
		Checksum:  checksum,
		CreatedAt: time.Now(),
	})

	return &NewMediaItem{
		SimpleMediaItem: &SimpleMediaItem{
			UploadToken: uploadToken,
			FileName:    file.Name,
		},
	}, checksum, false, nil
}

func (sm *SyncManager) listMediaFiles(ctx context.Context, path string) ([]syncmanager.FileInfo, int, error) {
	if sm.options.parallelMetadata {
		files, skipped, err := sm.listMediaFilesParallel(ctx, path)
		sm.store.flush()
		return files, skipped, err
	}
	files, skipped, err := sm.listMediaFilesRecursive(ctx, path)
	sm.store.flush()
	return files, skipped, err
}

func (sm *SyncManager) listMediaFilesRecursive(ctx context.Context, path string) ([]syncmanager.FileInfo, int, error) {
	if ctx.Err() != nil {
		return nil, 0, ctx.Err()
	}

	files, err := sm.syncMgr.ListFiles(path)
	if err != nil {
		return nil, 0, err
	}

	var mediaFiles []syncmanager.FileInfo
	var skipped int
	for _, f := range files {
		if ctx.Err() != nil {
			return nil, skipped, ctx.Err()
		}

		if f.IsDir {
			nestedMediaFiles, nestedSkipped, err := sm.listMediaFilesRecursive(ctx, f.Path)
			skipped += nestedSkipped
			if err != nil {
				return nil, skipped, err
			}
			mediaFiles = append(mediaFiles, nestedMediaFiles...)
			continue
		}

		if IsPhotoOrVideo(f.Name) {
			sm.cacheListedFileMetadata(f)
			mediaFiles = append(mediaFiles, f)
		} else if IsRAW(f.Name) {
			skipped++
		}
	}

	return mediaFiles, skipped, nil
}

func (sm *SyncManager) listMediaFilesParallel(ctx context.Context, root string) ([]syncmanager.FileInfo, int, error) {
	parallelism := runtime.GOMAXPROCS(0)
	if parallelism < 2 {
		parallelism = 2
	}
	if parallelism > 8 {
		parallelism = 8
	}
	sem := make(chan struct{}, parallelism)
	results := make(chan []syncmanager.FileInfo, parallelism)
	var errOnce sync.Once
	var firstErr error
	walkCtx, cancelWalk := context.WithCancel(ctx)
	defer cancelWalk()
	var wg sync.WaitGroup
	var skipped atomic.Int64

	var walk func(string)
	spawn := func(dir string) {
		// Acquire the semaphore in the parent goroutine so we don't pile up
		// goroutines waiting on the sem on wide trees.
		select {
		case sem <- struct{}{}:
		case <-walkCtx.Done():
			return
		}
		wg.Add(1)
		go walk(dir)
	}
	walk = func(dir string) {
		defer wg.Done()
		defer func() { <-sem }()
		files, err := sm.syncMgr.ListFiles(dir)
		if err != nil {
			errOnce.Do(func() {
				firstErr = err
				cancelWalk()
			})
			return
		}

		localMedia := make([]syncmanager.FileInfo, 0, len(files))
		for _, f := range files {
			if walkCtx.Err() != nil {
				return
			}
			if f.IsDir {
				spawn(f.Path)
				continue
			}
			if IsPhotoOrVideo(f.Name) {
				sm.cacheListedFileMetadata(f)
				localMedia = append(localMedia, f)
			} else if IsRAW(f.Name) {
				skipped.Add(1)
			}
		}
		if len(localMedia) > 0 {
			select {
			case results <- localMedia:
			case <-walkCtx.Done():
			}
		}
	}

	spawn(root)
	go func() {
		wg.Wait()
		close(results)
	}()

	var mediaFiles []syncmanager.FileInfo
	for chunk := range results {
		mediaFiles = append(mediaFiles, chunk...)
	}
	if firstErr != nil {
		return nil, int(skipped.Load()), firstErr
	}
	if err := ctx.Err(); err != nil && !errors.Is(err, context.Canceled) {
		return nil, int(skipped.Load()), err
	}
	return mediaFiles, int(skipped.Load()), nil
}

func sortMediaForUpload(files []syncmanager.FileInfo) {
	sort.SliceStable(files, func(i, j int) bool {
		iModTime := files[i].ModTime
		jModTime := files[j].ModTime
		if !iModTime.IsZero() && !jModTime.IsZero() && !iModTime.Equal(jModTime) {
			return iModTime.Before(jModTime)
		}
		if iModTime.IsZero() != jModTime.IsZero() {
			return !iModTime.IsZero()
		}
		if files[i].Path != files[j].Path {
			return files[i].Path < files[j].Path
		}
		return files[i].Name < files[j].Name
	})
}

func totalFileSize(files []syncmanager.FileInfo) int64 {
	var total int64
	for _, file := range files {
		if file.Size > 0 {
			total += file.Size
		}
	}
	return total
}

func (sm *SyncManager) cacheListedFileMetadata(file syncmanager.FileInfo) {
	meta := cachedFileMetadata{
		Path:    file.Path,
		Name:    file.Name,
		Size:    file.Size,
		ModTime: file.ModTime,
		Mime:    mime.TypeByExtension(filepath.Ext(file.Name)),
	}
	if existing, ok := sm.store.fileMeta(file.Path); ok && existing.Size == file.Size && existing.ModTime.Equal(file.ModTime) {
		meta.Checksum = existing.Checksum
		meta.Uploaded = existing.Uploaded
		meta.UploadedAt = existing.UploadedAt
	}
	sm.store.rememberFileMeta(meta)
}

// downloadFile downloads a file from B2 using the sync manager. Native Google
// Photos sync may process large videos, so use a temp file instead of buffering
// the whole media item in memory. The SHA-256 checksum is computed inline via
// io.MultiWriter to avoid a second full read of the file before upload.
func (sm *SyncManager) downloadFile(file syncmanager.FileInfo) (*os.File, int64, string, func(), error) {
	tmp, err := os.CreateTemp("", "pictures-sync-gphotos-*"+filepath.Ext(file.Name))
	if err != nil {
		return nil, 0, "", nil, fmt.Errorf("failed to create temp file: %w", err)
	}

	cleanup := func() {
		name := tmp.Name()
		_ = tmp.Close()
		_ = os.Remove(name)
	}

	hash := sha256.New()
	sink := io.MultiWriter(tmp, hash)

	timeout := googlePhotosTransferTimeout(file.Size)
	if downloader, ok := sm.syncMgr.(syncManagerWithDownloadTimeout); ok {
		err = downloader.GetFileWithTimeout(file.Path, sink, timeout)
	} else {
		err = sm.syncMgr.GetFile(file.Path, sink)
	}
	if err != nil {
		cleanup()
		return nil, 0, "", nil, err
	}
	info, err := tmp.Stat()
	if err != nil {
		cleanup()
		return nil, 0, "", nil, fmt.Errorf("failed to stat temp file: %w", err)
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		cleanup()
		return nil, 0, "", nil, fmt.Errorf("failed to rewind temp file: %w", err)
	}
	return tmp, info.Size(), hex.EncodeToString(hash.Sum(nil)), cleanup, nil
}

func googlePhotosTransferTimeout(size int64) time.Duration {
	const (
		minTimeout       = 5 * time.Minute
		maxTimeout       = 2 * time.Hour
		assumedBytesPerS = 512 * 1024
	)
	if size <= 0 {
		return minTimeout
	}
	timeout := time.Duration(size/assumedBytesPerS) * time.Second
	if timeout < minTimeout {
		return minTimeout
	}
	if timeout > maxTimeout {
		return maxTimeout
	}
	return timeout
}

type batchCreateResult struct {
	successCount int
	failCount    int
	successes    []bool
}

// createBatch creates media items in a batch and returns per-item success state.
func (sm *SyncManager) createBatch(ctx context.Context, albumID string, items []*NewMediaItem) (batchCreateResult, error) {
	result := batchCreateResult{
		successes: make([]bool, len(items)),
	}
	if !sm.allowRequest() {
		result.failCount = len(items)
		return result, sm.circuitBreakerOpenError()
	}
	started := time.Now()
	resp, err := retry(ctx, maxRetryAttempts, func(int) (*BatchCreateResponse, error) {
		apiCtx, cancel := context.WithTimeout(ctx, googlePhotosAPITimeout)
		defer cancel()
		return sm.client.BatchCreateMediaItemsContext(apiCtx, albumID, items)
	}, sm.setRetryStatus)
	sm.recordAPILatency(time.Since(started))
	if err != nil {
		sm.recordFailure()
		result.failCount = len(items)
		return result, err
	}
	sm.recordSuccess()

	for i, itemResult := range resp.NewMediaItemResults {
		if i >= len(items) {
			break
		}
		if itemResult.Status != nil && itemResult.Status.Code != 0 {
			log.Printf("[GooglePhotos] Item creation failed: %s (code %d)", itemResult.Status.Message, itemResult.Status.Code)
			result.failCount++
		} else {
			result.successes[i] = true
			result.successCount++
		}
	}
	if missing := len(items) - len(resp.NewMediaItemResults); missing > 0 {
		log.Printf("[GooglePhotos] Batch create returned %d/%d item results; treating %d missing results as failed", len(resp.NewMediaItemResults), len(items), missing)
		result.failCount += missing
	}

	return result, nil
}

func (sm *SyncManager) setError(err error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if sm.progress != nil {
		sm.progress.Status = "error"
		sm.progress.Error = err.Error()
		sm.progress.UpdatedAt = timePtr(time.Now())
	}
}

func (sm *SyncManager) setStatus(status string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if sm.progress != nil {
		sm.progress.Status = status
		sm.progress.UpdatedAt = timePtr(time.Now())
	}
}

func (sm *SyncManager) setPhase(phase string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if sm.progress != nil {
		sm.progress.CurrentPhase = phase
		sm.progress.UpdatedAt = timePtr(time.Now())
	}
}

func (sm *SyncManager) setUploadProgress(name, path string, size, uploadedBytes int64) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if sm.progress == nil {
		return
	}
	sm.progress.CurrentFile = name
	sm.progress.CurrentFilePath = path
	sm.progress.CurrentFileSize = size
	sm.progress.CurrentFileBytesUploaded = uploadedBytes
	sm.progress.CurrentFilePercent = percent(uploadedBytes, size)
	sm.progress.CurrentPhase = "Uploading to Google Photos"
	sm.progress.UpdatedAt = timePtr(time.Now())
}

func (sm *SyncManager) finishFileProgress(file syncmanager.FileInfo, uploaded, skipped, failed int) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if sm.progress == nil {
		return
	}
	if file.Size > 0 {
		sm.progress.ProcessedBytes += file.Size
	}
	sm.progress.ProcessedFiles++
	sm.progress.CurrentFile = file.Name
	sm.progress.CurrentFilePath = file.Path
	sm.progress.CurrentFileSize = file.Size
	sm.progress.UploadedFiles = uploaded
	sm.progress.SkippedFiles = skipped
	sm.progress.FailedFiles = failed
	sm.progress.CurrentFileBytesUploaded = file.Size
	sm.progress.CurrentFilePercent = 100
	sm.progress.CurrentPhase = "Queued for album"
	sm.progress.UpdatedAt = timePtr(time.Now())
}

func (sm *SyncManager) setCounts(uploaded, skipped, failed int) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if sm.progress != nil {
		sm.progress.UploadedFiles = uploaded
		sm.progress.SkippedFiles = skipped
		sm.progress.FailedFiles = failed
		sm.progress.UpdatedAt = timePtr(time.Now())
	}
}

func (sm *SyncManager) setBatchPending(pending int) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if sm.progress != nil {
		sm.progress.BatchPendingFiles = pending
		sm.progress.UpdatedAt = timePtr(time.Now())
	}
}

func (sm *SyncManager) adaptiveUploadWorkers(totalFiles int) int {
	workers := sm.options.uploadWorkerFloor
	if totalFiles > 25 {
		workers = runtime.GOMAXPROCS(0) + 1
	}
	if workers < 1 {
		workers = 1
	}
	if workers > maxUploadWorkers {
		workers = maxUploadWorkers
	}
	if totalFiles > 0 && workers > totalFiles {
		workers = totalFiles
	}
	sm.mu.Lock()
	if sm.progress != nil {
		sm.progress.BackendMetrics.UploadWorkers = workers
		sm.progress.BackendMetrics.BatchSize = sm.options.batchSize
	}
	sm.mu.Unlock()
	return workers
}

func (sm *SyncManager) setCardProgress(cardID string, processed, total, uploaded, skipped, failed int, phase string, queueDepth int) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if sm.progress == nil {
		return
	}
	card := CardProgress{
		CardID:       cardID,
		Position:     sm.progress.CurrentCard,
		TotalFiles:   total,
		Processed:    processed,
		Uploaded:     uploaded,
		Skipped:      skipped,
		Failed:       failed,
		QueueDepth:   queueDepth,
		CurrentPhase: phase,
	}
	if len(sm.progress.CardProgress) == 0 {
		sm.progress.CardProgress = []CardProgress{card}
	} else {
		sm.progress.CardProgress[len(sm.progress.CardProgress)-1] = card
	}
	sm.progress.UpdatedAt = timePtr(time.Now())
}

func (sm *SyncManager) incrementDuplicateSkipped() {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if sm.progress != nil {
		sm.progress.DuplicatesSkipped++
	}
}

func initialStages(now time.Time) []StageProgress {
	return []StageProgress{
		{Name: "discover", Status: "active", StartedAt: &now},
		{Name: "download/copy", Status: "pending"},
		{Name: "upload", Status: "pending"},
		{Name: "batch_create", Status: "pending"},
		{Name: "finalization", Status: "pending"},
	}
}

func (sm *SyncManager) startStage(name string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if sm.progress == nil {
		return
	}
	now := time.Now()
	for i := range sm.progress.StageTimeline {
		if sm.progress.StageTimeline[i].Name == name && sm.progress.StageTimeline[i].Status == "pending" {
			sm.progress.StageTimeline[i].Status = "active"
			sm.progress.StageTimeline[i].StartedAt = &now
		}
	}
}

func (sm *SyncManager) completeStage(name string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if sm.progress == nil {
		return
	}
	now := time.Now()
	for i := range sm.progress.StageTimeline {
		if sm.progress.StageTimeline[i].Name == name && sm.progress.StageTimeline[i].Status != "completed" {
			sm.progress.StageTimeline[i].Status = "completed"
			sm.progress.StageTimeline[i].CompletedAt = &now
		}
	}
}

func (sm *SyncManager) setRetryStatus(count int, next time.Time, reason string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if sm.progress == nil {
		return
	}
	if count == 0 {
		sm.progress.RetryStatus = nil
		return
	}
	sm.progress.RetryStatus = &RetryStatus{Count: count, NextRetryAt: &next, Reason: classifyGooglePhotosErrorMessage(reason).Error()}
	sm.progress.UpdatedAt = timePtr(time.Now())
}

func (sm *SyncManager) recordCancelledSummary(startedAt time.Time, uploaded, skipped, failed int) {
	now := time.Now()
	progress := sm.Progress()
	sm.mu.Lock()
	if sm.progress != nil {
		sm.progress.CancellationSummary = fmt.Sprintf("Cancelled after %d uploaded, %d skipped, %d failed", uploaded, skipped, failed)
	}
	sm.mu.Unlock()
	sm.store.addSummary(SyncRunSummary{
		StartedAt:      startedAt,
		CompletedAt:    now,
		Duration:       now.Sub(startedAt).Round(time.Second).String(),
		Status:         "cancelled",
		UploadedFiles:  uploaded,
		SkippedFiles:   skipped,
		FailedFiles:    failed,
		ProcessedBytes: progress.ProcessedBytes,
	})
}

type coalescedProgress struct {
	onUpdate func(int64)
	lastTime time.Time
	lastByte int64
	pending  int64
}

func newCoalescedProgress(onUpdate func(int64)) *coalescedProgress {
	return &coalescedProgress{onUpdate: onUpdate, lastTime: time.Now()}
}

func (p *coalescedProgress) update(uploaded int64) {
	p.pending = uploaded
	if uploaded-p.lastByte >= progressUpdateBytes || time.Since(p.lastTime) >= progressUpdateInterval {
		p.flush()
	}
}

func (p *coalescedProgress) flush() {
	if p.pending == p.lastByte {
		return
	}
	p.onUpdate(p.pending)
	p.lastByte = p.pending
	p.lastTime = time.Now()
}

func retry[T any](ctx context.Context, attempts int, fn func(int) (T, error), onRetry func(int, time.Time, string)) (T, error) {
	var zero T
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		value, err := fn(attempt)
		if err == nil {
			onRetry(0, time.Time{}, "")
			return value, nil
		}
		lastErr = err
		if attempt == attempts-1 || !isTransientGooglePhotosError(err) {
			break
		}
		delay := time.Duration(1<<attempt) * 500 * time.Millisecond
		delay += time.Duration(rand.Int63n(int64(250 * time.Millisecond)))
		next := time.Now().Add(delay)
		onRetry(attempt+1, next, err.Error())
		timer := time.NewTimer(delay)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return zero, ctx.Err()
		}
	}
	return zero, lastErr
}

func isTransientGooglePhotosError(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "timeout") ||
		strings.Contains(msg, "temporary") ||
		strings.Contains(msg, "429") ||
		strings.Contains(msg, "500") ||
		strings.Contains(msg, "502") ||
		strings.Contains(msg, "503") ||
		strings.Contains(msg, "504")
}

func classifyGooglePhotosError(err error) error {
	if err == nil {
		return nil
	}
	lower := strings.ToLower(err.Error())
	switch {
	case strings.Contains(lower, "401"), strings.Contains(lower, "403"), strings.Contains(lower, "token"), strings.Contains(lower, "auth"):
		return fmt.Errorf("Google Photos authorization failed; reconnect your account: %w", err)
	case strings.Contains(lower, "quota"), strings.Contains(lower, "429"):
		return fmt.Errorf("Google Photos quota or rate limit reached; retry later: %w", err)
	case strings.Contains(lower, "timeout"), strings.Contains(lower, "connection"), strings.Contains(lower, "network"):
		return fmt.Errorf("network problem while contacting Google Photos: %w", err)
	case strings.Contains(lower, "400"), strings.Contains(lower, "bad request"):
		return fmt.Errorf("Google Photos rejected a media item or request payload: %w", err)
	default:
		return err
	}
}

func classifyGooglePhotosErrorMessage(message string) error {
	lower := strings.ToLower(message)
	switch {
	case strings.Contains(lower, "401"), strings.Contains(lower, "403"), strings.Contains(lower, "token"), strings.Contains(lower, "auth"):
		return fmt.Errorf("Google Photos authorization failed; reconnect your account: %s", message)
	case strings.Contains(lower, "quota"), strings.Contains(lower, "429"):
		return fmt.Errorf("Google Photos quota or rate limit reached; retry later: %s", message)
	case strings.Contains(lower, "timeout"), strings.Contains(lower, "connection"), strings.Contains(lower, "network"):
		return fmt.Errorf("network problem while contacting Google Photos: %s", message)
	case strings.Contains(lower, "400"), strings.Contains(lower, "bad request"):
		return fmt.Errorf("Google Photos rejected a media item or request payload: %s", message)
	default:
		return errors.New(message)
	}
}

func tuneBatchSize(current, successCount, failCount int, err error) int {
	if current <= 0 {
		current = defaultBatchSize
	}
	if err != nil || failCount > 0 {
		current /= 2
		if current < 2 {
			return 2
		}
		return current
	}
	if successCount >= current && current < 50 {
		current += 5
		if current > 50 {
			return 50
		}
	}
	return current
}

func envBool(name string) bool {
	value := strings.TrimSpace(strings.ToLower(os.Getenv(name)))
	return value == "1" || value == "true" || value == "yes"
}

func percent(part, total int64) int {
	if total <= 0 || part <= 0 {
		return 0
	}
	p := int((part * 100) / total)
	if p > 100 {
		return 100
	}
	return p
}

func timePtr(t time.Time) *time.Time {
	return &t
}
