package cardhandler

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/denysvitali/pictures-sync-s3/pkg/events"
	"github.com/denysvitali/pictures-sync-s3/pkg/sdmonitor"
	"github.com/denysvitali/pictures-sync-s3/pkg/settings"
	"github.com/denysvitali/pictures-sync-s3/pkg/state"
)

// isCancellationError reports whether err originates from a user cancellation
// of the running sync (context.Canceled, or the wrapped "sync cancelled"
// errors produced by the retry helper in pkg/syncmanager).
func isCancellationError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "sync cancelled") || strings.Contains(msg, "cancelled by user")
}

const (
	// preSyncDelay is the wait time before starting sync operations after card detection.
	// This delay allows the filesystem to fully stabilize and ensures all files are accessible
	// before the sync begins.
	preSyncDelay = 2 * time.Second

	// successStatusDuration is how long to keep the success status visible after sync completion.
	// This provides visual feedback to the user via LED patterns before returning to idle state.
	successStatusDuration = 5 * time.Second
)

// Handler manages SD card insertion and removal events
type Handler struct {
	monitor       *sdmonitor.Monitor
	stateMgr      *state.Manager
	syncMgr       syncManager
	settings      *settings.Settings
	eventMgr      *events.Manager
	syncStartMu   sync.Mutex // Protects syncStarting and activeSyncKey
	syncStarting  bool       // True while the daemon-owned sync workflow is active
	activeSyncKey string     // Device currently being prepared or synced
}

type syncManager interface {
	IsRunning() bool
	Cancel() error
	Sync(sourcePath, cardID string, totalFiles int, totalBytes int64) error
	ApplySettings(remoteName, remotePath string, transfers, checkers int, googlePhotosEnabled bool, googlePhotosRemoteName string)
}

// NewHandler creates a new card handler
func NewHandler(
	monitor *sdmonitor.Monitor,
	stateMgr *state.Manager,
	syncMgr syncManager,
	settings *settings.Settings,
	eventMgr *events.Manager,
) *Handler {
	return &Handler{
		monitor:  monitor,
		stateMgr: stateMgr,
		syncMgr:  syncMgr,
		settings: settings,
		eventMgr: eventMgr,
	}
}

// HandleInserted processes an SD card insertion event
func (h *Handler) HandleInserted(event sdmonitor.Event) {
	log.Printf("SD card inserted: %s, mounted at %s", event.DevName, event.MountPath)

	// Emit SD card inserted event
	h.eventMgr.EmitSDCardInserted(event.DevName, event.MountPath)

	// Update state
	h.stateMgr.SetSDCard(true, event.MountPath)
	h.stateMgr.SetSDCardDevice(event.DevPath)
	h.stateMgr.SetStatus(state.StatusDetected)

	// Atomic check-and-set to prevent race condition:
	// Multiple cards could be inserted rapidly, but only one sync should proceed
	h.syncStartMu.Lock()
	defer h.syncStartMu.Unlock()

	if h.syncMgr.IsRunning() || h.syncStarting {
		if h.activeSyncKey == event.DevPath {
			log.Printf("Sync workflow already active for %s, ignoring duplicate insertion", event.DevPath)
		} else {
			log.Println("Sync already in progress or starting, ignoring new card insertion")
		}
		return
	}

	// Mark that we're starting a sync - this prevents other threads from also starting
	h.syncStarting = true
	h.activeSyncKey = event.DevPath

	// Launch the sync operation in a goroutine
	// The goroutine will clear syncStarting flag when sync actually starts or fails
	go h.processSyncOperation(event)
}

// HandleManualSync starts the daemon-owned sync workflow for the selected card.
// If devicePath is empty, it uses the currently mounted card.
func (h *Handler) HandleManualSync(devicePath string) error {
	if !h.monitor.IsCardMounted() {
		return fmt.Errorf("no SD card mounted")
	}

	mountPath := h.monitor.GetMountPath()
	device := h.monitor.GetCurrentDevice()
	if device == "" {
		return fmt.Errorf("no SD card mounted")
	}
	if devicePath != "" && devicePath != device {
		return fmt.Errorf("selected device is not mounted")
	}

	h.syncStartMu.Lock()
	defer h.syncStartMu.Unlock()

	if h.syncMgr.IsRunning() || h.syncStarting {
		return fmt.Errorf("sync already in progress or starting")
	}

	log.Printf("Manual sync accepted by daemon for mounted SD card at: %s", mountPath)

	h.stateMgr.SetSDCard(true, mountPath)
	h.stateMgr.SetSDCardDevice(device)
	h.stateMgr.SetStatus(state.StatusDetected)
	h.syncStarting = true
	h.activeSyncKey = device

	go h.processSyncOperation(sdmonitor.Event{
		Type:      sdmonitor.EventInserted,
		DevPath:   device,
		DevName:   filepath.Base(device),
		MountPath: mountPath,
	})

	return nil
}

// HandleRemoved processes an SD card removal event
func (h *Handler) HandleRemoved(event sdmonitor.Event) {
	log.Printf("SD card removed: %s", event.DevName)
	h.eventMgr.EmitSDCardRemoved(event.DevName)
	h.stateMgr.SetSDCard(false, "")

	h.syncStartMu.Lock()
	startingForRemovedDevice := h.syncStarting && (h.activeSyncKey == "" || h.activeSyncKey == event.DevPath)
	h.syncStartMu.Unlock()
	if startingForRemovedDevice && !h.syncMgr.IsRunning() {
		log.Println("Sync preparation interrupted by card removal")
	}

	// If we were syncing, cancel and mark as error
	if h.syncMgr.IsRunning() {
		log.Println("Sync interrupted by card removal, cancelling...")
		if err := h.syncMgr.Cancel(); err != nil {
			log.Printf("Failed to cancel sync: %v", err)
		}
		h.stateMgr.FinishSync(false, fmt.Errorf("card removed during sync"))
	}

	h.stateMgr.SetStatus(state.StatusIdle)
}

// processSyncOperation handles the complete sync workflow for an inserted card
func (h *Handler) processSyncOperation(event sdmonitor.Event) {
	defer func() {
		h.syncStartMu.Lock()
		if h.activeSyncKey == "" || h.activeSyncKey == event.DevPath {
			h.syncStarting = false
			h.activeSyncKey = ""
		}
		h.syncStartMu.Unlock()
	}()

	// Emit detecting photos event
	h.eventMgr.Emit(events.EventDetectingPhotos, "Scanning SD card for photos", map[string]interface{}{
		"mount_path": event.MountPath,
	})

	// Check for DCIM directory
	if !sdmonitor.HasDCIM(event.MountPath) {
		h.stateMgr.SetSDCardPhotoSummary(0, 0)
		h.handleNoDCIM(event.MountPath)
		return
	}

	if err := h.refreshSettings(); err != nil {
		log.Printf("Error loading latest settings: %v", err)
		h.eventMgr.EmitError("Failed to load sync settings", err)
		h.stateMgr.SetStatus(state.StatusError)
		return
	}

	// Count photos
	totalFiles, totalBytes, err := sdmonitor.CountPhotos(event.MountPath)
	if err != nil {
		log.Printf("Error counting photos: %v", err)
		h.eventMgr.EmitError("Failed to count photos on SD card", err)
		h.stateMgr.SetStatus(state.StatusIdle)
		return
	}

	log.Printf("Found %d photos (%.2f MB)", totalFiles, float64(totalBytes)/(1024*1024))
	h.stateMgr.SetSDCardPhotoSummary(int64(totalFiles), totalBytes)

	if totalFiles == 0 {
		log.Println("No photos found on SD card")
		h.eventMgr.Emit(events.EventNoPhotosFound, "No photos found on SD card", nil)
		h.stateMgr.SetStatus(state.StatusIdle)
		return
	}

	// Emit photos detected event
	h.eventMgr.EmitPhotosDetected(totalFiles, totalBytes)

	// Get or create card ID and check for reformat
	cardID, err := h.getCardID(event.MountPath, totalFiles)
	if err != nil {
		log.Printf("Error getting card ID: %v", err)
		h.eventMgr.EmitError("Failed to get or create card ID", err)
		h.stateMgr.SetStatus(state.StatusError)
		return
	}

	// Check if rclone is configured before attempting sync
	hasConfig, err := state.EnsureRcloneConfig()
	if err != nil {
		log.Printf("Error checking rclone config: %v", err)
		h.eventMgr.EmitError("Failed to check rclone configuration", err)
		h.stateMgr.SetStatus(state.StatusError)
		return
	}
	if !hasConfig {
		log.Println("Rclone not configured - cannot sync. Please configure via web UI.")
		h.eventMgr.EmitError("Rclone not configured. Please configure storage via the Settings page.", nil)
		h.stateMgr.SetStatus(state.StatusError)
		return
	}

	// Flip status to Syncing immediately so the UI reflects the user's action
	// without waiting for the pre-sync filesystem-stabilization delay below.
	// StartSync (invoked from performSync) will subsequently set Syncing again
	// once the SyncRecord is created; this early transition is purely for UX.
	h.stateMgr.SetStatus(state.StatusSyncing)

	// Wait a moment before starting sync to let the filesystem fully stabilize.
	time.Sleep(preSyncDelay)

	// Verify card is still mounted before starting sync
	if !h.monitor.IsCardMounted() {
		log.Println("SD card was removed during preparation, cancelling sync")
		h.eventMgr.EmitError("SD card removed before sync could start", nil)
		h.stateMgr.SetStatus(state.StatusIdle)
		return
	}

	// Perform the sync
	h.performSync(event.MountPath, cardID, totalFiles, totalBytes)
}

func (h *Handler) refreshSettings() error {
	latest, err := settings.Load()
	if err != nil {
		return err
	}

	h.settings = latest
	h.syncMgr.ApplySettings(
		latest.GetRemoteName(),
		latest.GetRemotePath(),
		latest.GetTransfers(),
		latest.GetCheckers(),
		latest.GetGooglePhotosEnabled(),
		latest.GetGooglePhotosRemoteName(),
	)
	return nil
}

// handleNoDCIM logs details about a card without a DCIM directory
func (h *Handler) handleNoDCIM(mountPath string) {
	log.Printf("No DCIM directory found on SD card at %s", mountPath)

	// List contents of the root directory to see what's actually there
	entries, err := os.ReadDir(mountPath)
	if err != nil {
		log.Printf("Failed to read SD card contents: %v", err)
	} else {
		log.Printf("SD card contents (%d items):", len(entries))
		for i, entry := range entries {
			if i >= 10 { // Limit output to first 10 items
				log.Printf("  ... and %d more items", len(entries)-10)
				break
			}
			if entry.IsDir() {
				log.Printf("  [DIR]  %s", entry.Name())
			} else {
				info, _ := entry.Info()
				log.Printf("  [FILE] %s (%d bytes)", entry.Name(), info.Size())
			}
		}
	}

	log.Println("Ignoring SD card (no DCIM directory)")
	h.eventMgr.EmitError("No DCIM directory found on SD card", nil)
	h.stateMgr.SetStatus(state.StatusIdle)
}

// getCardID retrieves or creates a card ID, handling reformat detection
func (h *Handler) getCardID(mountPath string, currentFiles int) (string, error) {
	// Get or create card ID (pass monitor so it can remount read-only after writing)
	cardID, isNewCard, err := sdmonitor.GetOrCreateCardID(mountPath, h.monitor)
	if err != nil {
		return "", fmt.Errorf("failed to get or create card ID: %w", err)
	}

	if isNewCard {
		log.Printf("New card detected: %s", cardID)
		h.eventMgr.EmitCardIDCreated(cardID)
		return cardID, nil
	}

	log.Printf("Known card detected: %s", cardID)
	h.eventMgr.EmitCardIDFound(cardID)

	// Check if card was reformatted
	lastSync := h.stateMgr.FindLastSyncByCardID(cardID)
	if lastSync != nil && lastSync.FilesTotal > 0 {
		// Calculate percentage of files compared to last sync
		percentageOfLast := float64(currentFiles) / float64(lastSync.FilesTotal)
		threshold := h.settings.GetReformatThreshold()

		log.Printf("File count comparison: current=%d, last=%d, ratio=%.2f, threshold=%.2f",
			currentFiles, lastSync.FilesTotal, percentageOfLast, threshold)

		// If significantly fewer files, assume card was reformatted
		if percentageOfLast < threshold {
			return h.handleReformat(mountPath, cardID, percentageOfLast)
		}
	}

	return cardID, nil
}

// handleReformat creates a new card ID when a reformat is detected
func (h *Handler) handleReformat(mountPath string, oldCardID string, percentageOfLast float64) (string, error) {
	log.Printf("Card appears to have been reformatted (%.1f%% of previous files)", percentageOfLast*100)
	log.Println("Creating new card ID for reformatted card")

	newCardID, err := sdmonitor.CreateNewCardID(mountPath, h.monitor)
	if err != nil {
		h.eventMgr.EmitError("Failed to create new card ID for reformatted card", err)
		return "", fmt.Errorf("failed to create new card ID: %w", err)
	}

	log.Printf("New card ID created: %s", newCardID)
	h.eventMgr.EmitReformatDetected(oldCardID, newCardID, percentageOfLast)
	return newCardID, nil
}

// performSync executes the sync operation and updates state accordingly
func (h *Handler) performSync(mountPath, cardID string, totalFiles int, totalBytes int64) {
	log.Printf("Starting sync of %d files (%.2f MB) to card folder: %s",
		totalFiles, float64(totalBytes)/(1024*1024), cardID)

	h.eventMgr.Emit(events.EventSyncStarting, "Preparing to start sync operation", map[string]interface{}{
		"card_id":     cardID,
		"total_files": totalFiles,
		"total_bytes": totalBytes,
	})

	_, err := h.stateMgr.StartSync(cardID, int64(totalFiles), totalBytes)
	if err != nil {
		log.Printf("Error starting sync: %v", err)
		h.eventMgr.EmitError("Failed to start sync operation", err)
		h.stateMgr.SetStatus(state.StatusError)
		return
	}

	// Emit sync started event
	h.eventMgr.EmitSyncStarted(cardID, totalFiles, totalBytes)

	// Perform sync
	startTime := time.Now()
	dcimPath := filepath.Join(mountPath, "DCIM")
	err = h.syncMgr.Sync(dcimPath, cardID, totalFiles, totalBytes)

	if err != nil {
		if isCancellationError(err) {
			log.Printf("Sync cancelled by user")
			h.eventMgr.EmitSyncFailed(cardID, fmt.Errorf("cancelled by user"))
			h.stateMgr.FinishSync(false, fmt.Errorf("cancelled by user"))
			h.stateMgr.SetStatus(state.StatusIdle)
		} else {
			log.Printf("Sync failed: %v", err)
			h.eventMgr.EmitSyncFailed(cardID, err)
			h.stateMgr.FinishSync(false, err)
			h.stateMgr.SetStatus(state.StatusError)
		}
	} else {
		duration := time.Since(startTime)
		log.Println("Sync completed successfully!")
		h.eventMgr.EmitSyncCompleted(cardID, int64(totalFiles), totalBytes, duration)
		h.stateMgr.FinishSync(true, nil)
		h.stateMgr.SetStatus(state.StatusSuccess)

		// Keep success status for a few seconds, then go idle
		time.Sleep(successStatusDuration)
		h.stateMgr.SetStatus(state.StatusIdle)
	}
}
