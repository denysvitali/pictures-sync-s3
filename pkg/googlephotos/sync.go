package googlephotos

import (
	"context"
	"fmt"
	"io"
	"log"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/denysvitali/pictures-sync-s3/pkg/syncmanager"
)

const (
	batchSize = 50 // Google Photos batch create limit
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
	if sm.cancel != nil {
		sm.cancel()
	}
}

// Sync performs a full sync from B2 to Google Photos
func (sm *SyncManager) Sync(ctx context.Context) error {
	if !sm.running.CompareAndSwap(false, true) {
		return fmt.Errorf("sync already in progress")
	}
	defer sm.running.Store(false)

	ctx, cancel := context.WithCancel(ctx)
	sm.cancel = cancel
	defer func() { sm.cancel = nil }()

	// Initialize progress
	sm.mu.Lock()
	sm.progress = &SyncProgress{
		Status: "listing_cards",
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

	sm.mu.Lock()
	sm.progress.TotalFiles += len(mediaFiles)
	sm.mu.Unlock()

	log.Printf("[GooglePhotos] Syncing %d files from card %s to album %s", len(mediaFiles), cardID, albumTitle)

	// Process files in batches
	var batch []*NewMediaItem
	for _, file := range mediaFiles {
		if ctx.Err() != nil {
			return uploaded, skipped, failed, ctx.Err()
		}

		sm.mu.Lock()
		sm.progress.CurrentFile = file.Name
		sm.progress.ProcessedFiles++
		sm.mu.Unlock()

		// Download file from B2
		fileData, err := sm.downloadFile(file.Path)
		if err != nil {
			log.Printf("[GooglePhotos] Failed to download %s: %v", file.Path, err)
			failed++
			continue
		}

		// Upload to Google Photos
		uploadToken, err := sm.client.UploadMedia(fileData, file.Name)
		if err != nil {
			log.Printf("[GooglePhotos] Failed to upload %s: %v", file.Name, err)
			failed++
			continue
		}

		batch = append(batch, &NewMediaItem{
			SimpleMediaItem: &SimpleMediaItem{
				UploadToken: uploadToken,
				FileName:    file.Name,
			},
		})

		uploaded++

		// Batch create when we reach the limit
		if len(batch) >= batchSize {
			successCount, failCount, err := sm.createBatch(album.ID, batch)
			if err != nil {
				log.Printf("[GooglePhotos] Failed to create batch in album %s: %v", albumTitle, err)
			}
			uploaded -= failCount
			failed += failCount
			_ = successCount
			batch = batch[:0]

			// Small delay to avoid rate limiting
			time.Sleep(100 * time.Millisecond)
		}
	}

	// Create remaining items in batch
	if len(batch) > 0 {
		successCount, failCount, err := sm.createBatch(album.ID, batch)
		if err != nil {
			log.Printf("[GooglePhotos] Failed to create final batch in album %s: %v", albumTitle, err)
		}
		uploaded -= failCount
		failed += failCount
		_ = successCount
	}

	return uploaded, skipped, failed, nil
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

// downloadFile downloads a file from B2 using the sync manager
func (sm *SyncManager) downloadFile(path string) ([]byte, error) {
	var dataBuf []byte
	w := &bytesWriter{data: &dataBuf}
	if err := sm.syncMgr.GetFile(path, w); err != nil {
		return nil, err
	}
	return dataBuf, nil
}

type bytesWriter struct {
	data *[]byte
}

func (w *bytesWriter) Write(p []byte) (int, error) {
	*w.data = append(*w.data, p...)
	return len(p), nil
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
	}
}

func (sm *SyncManager) setStatus(status string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if sm.progress != nil {
		sm.progress.Status = status
	}
}
