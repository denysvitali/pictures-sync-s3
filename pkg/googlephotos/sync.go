package googlephotos

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/denysvitali/pictures-sync-s3/pkg/syncmanager"
)

const (
	batchSize = 10 // Google Photos allows up to 50; smaller batches make album updates visible sooner.
)

// SyncManager orchestrates syncing photos from B2 to Google Photos
type SyncManager struct {
	client   *Client
	syncMgr  SyncManagerMinimal
	progress *SyncProgress
	mu       sync.RWMutex
	cancel   context.CancelFunc
	running  atomic.Bool
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
	file syncmanager.FileInfo
	item *NewMediaItem
	err  error
	ack  chan struct{}
}

func (r mediaUploadResult) done() {
	if r.ack != nil {
		close(r.ack)
	}
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
	return &SyncManager{
		client:  client,
		syncMgr: syncMgr,
	}
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
		return &SyncProgress{Status: "idle"}
	}
	// Return a copy
	p := *sm.progress
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
	sm.mu.Lock()
	sm.progress = &SyncProgress{
		Status:       "listing_cards",
		CurrentPhase: "Listing cards",
		StartedAt:    &startedAt,
		UpdatedAt:    &startedAt,
	}
	sm.mu.Unlock()

	// Get list of cards from B2
	cards, err := sm.syncMgr.ListCardIDs()
	if err != nil {
		sm.setError(err)
		return fmt.Errorf("failed to list cards: %w", err)
	}

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
	sm.progress.UpdatedAt = timePtr(time.Now())
	sm.mu.Unlock()

	log.Printf("[GooglePhotos] Sync complete: %d uploaded, %d skipped, %d failed", totalUploaded, totalSkipped, totalFailed)
	return nil
}

// syncCard syncs all photos from a single card to Google Photos
func (sm *SyncManager) syncCard(ctx context.Context, cardID, cardDirName string) (uploaded, skipped, failed int, err error) {
	// List all media in the card's DCIM directory before creating an album.
	// Camera cards commonly store files under DCIM/100CANON, DCIM/100MSDCF, etc.
	dcimPath := filepath.Join(cardDirName, "DCIM")
	mediaFiles, skipped, err := sm.listMediaFiles(ctx, dcimPath)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("failed to list files: %w", err)
	}

	if len(mediaFiles) == 0 {
		log.Printf("[GooglePhotos] No media files found in card %s", cardID)
		return 0, skipped, 0, nil
	}
	sortMediaForUpload(mediaFiles)

	cardBytes := totalFileSize(mediaFiles)
	sm.mu.Lock()
	sm.progress.CurrentCardFiles = len(mediaFiles)
	sm.progress.TotalFiles += len(mediaFiles)
	sm.progress.TotalBytes += cardBytes
	sm.progress.CurrentPhase = "Preparing album"
	sm.progress.UpdatedAt = timePtr(time.Now())
	sm.mu.Unlock()

	// Find or create album for this card only after there is something to upload.
	albumTitle := "Card " + cardID
	album, err := sm.client.FindAlbumByTitle(albumTitle)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("failed to find album: %w", err)
	}

	if album == nil {
		log.Printf("[GooglePhotos] Creating album: %s", albumTitle)
		album, err = sm.client.CreateAlbum(albumTitle)
		if err != nil {
			return 0, 0, 0, fmt.Errorf("failed to create album: %w", err)
		}
	}

	log.Printf("[GooglePhotos] Syncing %d files from card %s to album %s", len(mediaFiles), cardID, albumTitle)

	var batch []*NewMediaItem
	for result := range sm.uploadMediaItems(ctx, cardID, mediaFiles) {
		if result.err != nil {
			log.Printf("[GooglePhotos] Failed to upload %s: %v", result.file.Name, result.err)
			failed++
			sm.finishFileProgress(result.file.Size, uploaded, skipped, failed)
			result.done()
			continue
		}

		batch = append(batch, result.item)
		uploaded++
		sm.finishFileProgress(result.file.Size, uploaded, skipped, failed)
		sm.setBatchPending(len(batch))
		result.done()

		if ctx.Err() != nil {
			return uploaded, skipped, failed, ctx.Err()
		}

		if len(batch) < batchSize {
			continue
		}

		sm.setPhase("Adding to album")
		successCount, failCount, err := sm.createBatch(album.ID, batch)
		if err != nil {
			log.Printf("[GooglePhotos] Failed to create batch in album %s: %v", albumTitle, err)
		}
		log.Printf("[GooglePhotos] Created media items in album %s: success=%d failed=%d", albumTitle, successCount, failCount)
		uploaded -= failCount
		failed += failCount
		_ = successCount
		batch = batch[:0]
		sm.setCounts(uploaded, skipped, failed)
		sm.setBatchPending(0)

		// Small delay to avoid rate limiting
		time.Sleep(100 * time.Millisecond)
	}

	if ctx.Err() != nil {
		return uploaded, skipped, failed, ctx.Err()
	}

	// Create remaining items in batch
	if len(batch) > 0 {
		sm.setPhase("Adding to album")
		successCount, failCount, err := sm.createBatch(album.ID, batch)
		if err != nil {
			log.Printf("[GooglePhotos] Failed to create final batch in album %s: %v", albumTitle, err)
		}
		log.Printf("[GooglePhotos] Created final media items in album %s: success=%d failed=%d", albumTitle, successCount, failCount)
		uploaded -= failCount
		failed += failCount
		_ = successCount
		sm.setCounts(uploaded, skipped, failed)
		sm.setBatchPending(0)
	}

	return uploaded, skipped, failed, nil
}

func (sm *SyncManager) uploadMediaItems(ctx context.Context, cardID string, mediaFiles []syncmanager.FileInfo) <-chan mediaUploadResult {
	results := make(chan mediaUploadResult)
	go func() {
		defer close(results)
		for fileIndex, file := range mediaFiles {
			if ctx.Err() != nil {
				return
			}

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

			log.Printf("[GooglePhotos] Uploading file %d/%d from card %s: %s (%d bytes)", fileIndex+1, len(mediaFiles), cardID, file.Path, file.Size)

			item, err := sm.uploadMediaItem(file)
			result := mediaUploadResult{
				file: file,
				item: item,
				err:  err,
				ack:  make(chan struct{}),
			}
			select {
			case results <- result:
			case <-ctx.Done():
				return
			}

			select {
			case <-result.ack:
			case <-ctx.Done():
				return
			}
		}
	}()
	return results
}

func (sm *SyncManager) uploadMediaItem(file syncmanager.FileInfo) (*NewMediaItem, error) {
	fileData, fileSize, cleanup, err := sm.downloadFile(file)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", file.Path, err)
	}
	defer cleanup()

	sm.setUploadProgress(file.Name, file.Path, file.Size, 0)
	uploadReader := &progressReader{
		reader: fileData,
		onRead: func(uploadedBytes int64) {
			sm.setUploadProgress(file.Name, file.Path, file.Size, uploadedBytes)
		},
	}
	uploadToken, err := sm.client.UploadMediaReader(uploadReader, fileSize, file.Name)
	if err != nil {
		return nil, err
	}

	return &NewMediaItem{
		SimpleMediaItem: &SimpleMediaItem{
			UploadToken: uploadToken,
			FileName:    file.Name,
		},
	}, nil
}

func (sm *SyncManager) listMediaFiles(ctx context.Context, path string) ([]syncmanager.FileInfo, int, error) {
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
			nestedMediaFiles, nestedSkipped, err := sm.listMediaFiles(ctx, f.Path)
			skipped += nestedSkipped
			if err != nil {
				return nil, skipped, err
			}
			mediaFiles = append(mediaFiles, nestedMediaFiles...)
			continue
		}

		if IsPhotoOrVideo(f.Name) {
			mediaFiles = append(mediaFiles, f)
		} else if IsRAW(f.Name) {
			skipped++
		}
	}

	return mediaFiles, skipped, nil
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

// downloadFile downloads a file from B2 using the sync manager. Native Google
// Photos sync may process large videos, so use a temp file instead of buffering
// the whole media item in memory.
func (sm *SyncManager) downloadFile(file syncmanager.FileInfo) (*os.File, int64, func(), error) {
	tmp, err := os.CreateTemp("", "pictures-sync-gphotos-*"+filepath.Ext(file.Name))
	if err != nil {
		return nil, 0, nil, fmt.Errorf("failed to create temp file: %w", err)
	}

	cleanup := func() {
		name := tmp.Name()
		_ = tmp.Close()
		_ = os.Remove(name)
	}

	timeout := googlePhotosTransferTimeout(file.Size)
	if downloader, ok := sm.syncMgr.(syncManagerWithDownloadTimeout); ok {
		err = downloader.GetFileWithTimeout(file.Path, tmp, timeout)
	} else {
		err = sm.syncMgr.GetFile(file.Path, tmp)
	}
	if err != nil {
		cleanup()
		return nil, 0, nil, err
	}
	info, err := tmp.Stat()
	if err != nil {
		cleanup()
		return nil, 0, nil, fmt.Errorf("failed to stat temp file: %w", err)
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		cleanup()
		return nil, 0, nil, fmt.Errorf("failed to rewind temp file: %w", err)
	}
	return tmp, info.Size(), cleanup, nil
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

// createBatch creates media items in a batch and returns (successCount, failCount, error)
func (sm *SyncManager) createBatch(albumID string, items []*NewMediaItem) (int, int, error) {
	resp, err := sm.client.BatchCreateMediaItems(albumID, items)
	if err != nil {
		return 0, len(items), err
	}

	var successCount, failCount int
	for _, result := range resp.NewMediaItemResults {
		if result.Status != nil && result.Status.Code != 0 {
			log.Printf("[GooglePhotos] Item creation failed: %s (code %d)", result.Status.Message, result.Status.Code)
			failCount++
		} else {
			successCount++
		}
	}

	return successCount, failCount, nil
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
	if uploadedBytes > sm.progress.CurrentFileBytesUploaded {
		sm.progress.ProcessedBytes += uploadedBytes - sm.progress.CurrentFileBytesUploaded
	}
	sm.progress.CurrentFileBytesUploaded = uploadedBytes
	sm.progress.CurrentFilePercent = percent(uploadedBytes, size)
	sm.progress.CurrentPhase = "Uploading to Google Photos"
	sm.progress.UpdatedAt = timePtr(time.Now())
}

func (sm *SyncManager) finishFileProgress(size int64, uploaded, skipped, failed int) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if sm.progress == nil {
		return
	}
	if size > sm.progress.CurrentFileBytesUploaded {
		sm.progress.ProcessedBytes += size - sm.progress.CurrentFileBytesUploaded
	}
	sm.progress.ProcessedFiles++
	sm.progress.UploadedFiles = uploaded
	sm.progress.SkippedFiles = skipped
	sm.progress.FailedFiles = failed
	sm.progress.CurrentFileBytesUploaded = size
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
