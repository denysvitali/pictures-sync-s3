package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/denysvitali/pictures-sync-s3/pkg/events"
	"github.com/denysvitali/pictures-sync-s3/pkg/ledcontroller"
	"github.com/denysvitali/pictures-sync-s3/pkg/sdmonitor"
	"github.com/denysvitali/pictures-sync-s3/pkg/settings"
	"github.com/denysvitali/pictures-sync-s3/pkg/state"
	"github.com/denysvitali/pictures-sync-s3/pkg/syncmanager"
)

func main() {
	// Enable caller reporting in logs (file:line)
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	log.Println("Photo Backup Station - Starting...")

	// Wait for gokrazy's NTP daemon to sync time
	// The gokrazy NTP daemon runs automatically and syncs time on boot
	// We just need to wait a moment for it to complete initial sync
	log.Println("Waiting for time synchronization (gokrazy NTP daemon)...")
	time.Sleep(5 * time.Second)
	log.Println("Time sync wait complete. Current time:", time.Now())

	// Initialize event manager
	eventMgr := events.NewManager()

	// Initialize state manager
	stateMgr, err := state.NewManager()
	if err != nil {
		log.Fatalf("Failed to create state manager: %v", err)
	}

	// Load settings
	appSettings, err := settings.Load()
	if err != nil {
		log.Fatalf("Failed to load settings: %v", err)
	}
	log.Printf("Loaded settings: remote=%s:%s", appSettings.GetRemoteName(), appSettings.GetRemotePath())

	// Check if rclone is configured
	hasConfig, err := state.EnsureRcloneConfig()
	if err != nil {
		log.Fatalf("Failed to check rclone config: %v", err)
	}
	if !hasConfig {
		log.Println("Warning: rclone not configured yet. Please configure via web UI.")
	}

	// Initialize sync manager
	syncMgr := syncmanager.NewManager(
		state.GetRcloneConfigPath(),
		appSettings.GetRemoteName(),
		appSettings.GetRemotePath(),
		stateMgr,
		appSettings.GetTransfers(),
		appSettings.GetCheckers(),
	)
	// Update Google Photos settings
	syncMgr.SetGooglePhotos(appSettings.GetGooglePhotosEnabled(), appSettings.GetGooglePhotosRemoteName())

	// Initialize LED controller
	ledCtrl, err := ledcontroller.NewController()
	if err != nil {
		log.Printf("Warning: Failed to initialize LED controller: %v", err)
	} else {
		if err := ledCtrl.Start(stateMgr); err != nil {
			log.Printf("Warning: Failed to start LED controller: %v", err)
		}
		defer ledCtrl.Stop()
		log.Println("LED controller started")
	}

	// Initialize SD card monitor
	monitor := sdmonitor.NewMonitor(state.MountDir)

	// Get event channel BEFORE starting monitor to avoid missing events
	eventChan := monitor.Events()
	log.Printf("Event channel obtained: %p", eventChan)

	// Now start the monitor - any events sent during Start() will be buffered
	if err := monitor.Start(); err != nil {
		log.Fatalf("Failed to start SD card monitor: %v", err)
	}
	defer monitor.Stop()
	log.Println("SD card monitor started")

	// Set initial status
	log.Println("Setting initial status to idle...")
	stateMgr.SetStatus(state.StatusIdle)

	// Note: No need to manually check for already-mounted cards here.
	// The SD monitor's Start() method calls checkDevices() which will detect
	// any already-mounted cards and send an insertion event automatically.
	// This prevents duplicate events and race conditions.

	// Handle signals
	log.Println("Setting up signal handling...")
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// Main event loop
	log.Println("Ready - waiting for SD card insertion...")
	log.Println("Entering main event loop...")

	for {
		select {
		case <-sigChan:
			log.Println("Received shutdown signal, exiting...")
			return

		case event := <-eventChan:
			log.Printf("Received event from monitor: %+v", event)
			switch event.Type {
			case sdmonitor.EventInserted:
				log.Printf("Processing card insertion event: %s at %s", event.DevName, event.MountPath)
				handleCardInserted(event, monitor, stateMgr, syncMgr, appSettings, eventMgr)
			case sdmonitor.EventRemoved:
				log.Printf("Processing card removal event: %s", event.DevName)
				handleCardRemoved(event, stateMgr, syncMgr, eventMgr)
			}
		}
	}
}

func handleCardInserted(event sdmonitor.Event, monitor *sdmonitor.Monitor, stateMgr *state.Manager, syncMgr *syncmanager.Manager, appSettings *settings.Settings, eventMgr *events.Manager) {
	log.Printf("SD card inserted: %s, mounted at %s", event.DevName, event.MountPath)

	// Emit SD card inserted event
	eventMgr.EmitSDCardInserted(event.DevName, event.MountPath)

	// Update state
	stateMgr.SetSDCard(true, event.MountPath)
	stateMgr.SetStatus(state.StatusDetected)

	// Check if a sync is already running
	if syncMgr.IsRunning() {
		log.Println("Sync already in progress, ignoring new card insertion")
		return
	}

	// Run the sync operation in a goroutine to avoid blocking the event loop
	go func() {
		// Emit detecting photos event
		eventMgr.Emit(events.EventDetectingPhotos, "Scanning SD card for photos", map[string]interface{}{
			"mount_path": event.MountPath,
		})

		// Check for DCIM directory
		if !sdmonitor.HasDCIM(event.MountPath) {
			log.Printf("No DCIM directory found on SD card at %s", event.MountPath)

			// List contents of the root directory to see what's actually there
			entries, err := os.ReadDir(event.MountPath)
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
			eventMgr.EmitError("No DCIM directory found on SD card", nil)
			stateMgr.SetStatus(state.StatusIdle)
			return
		}

		// Count photos
		totalFiles, totalBytes, err := sdmonitor.CountPhotos(event.MountPath)
		if err != nil {
			log.Printf("Error counting photos: %v", err)
			eventMgr.EmitError("Failed to count photos on SD card", err)
			stateMgr.SetStatus(state.StatusIdle)
			return
		}

		log.Printf("Found %d photos (%.2f MB)", totalFiles, float64(totalBytes)/(1024*1024))

		if totalFiles == 0 {
			log.Println("No photos found on SD card")
			eventMgr.Emit(events.EventNoPhotosFound, "No photos found on SD card", nil)
			stateMgr.SetStatus(state.StatusIdle)
			return
		}

		// Emit photos detected event
		eventMgr.EmitPhotosDetected(totalFiles, totalBytes)

		// Handle empty cards (zero files) with user-friendly message
		if totalFiles == 0 {
			log.Println("Note: Card has no photos yet")
			// Set at least 1 file to avoid division by zero in progress bar
			totalFiles = 1
			totalBytes = 1
		}

		// Get or create card ID (pass monitor so it can remount read-only after writing)
		cardID, isNewCard, err := sdmonitor.GetOrCreateCardID(event.MountPath, monitor)
		if err != nil {
			log.Printf("Error getting card ID: %v", err)
			eventMgr.EmitError("Failed to get or create card ID", err)
			stateMgr.SetStatus(state.StatusError)
			return
		}

		if isNewCard {
			log.Printf("New card detected: %s", cardID)
			eventMgr.EmitCardIDCreated(cardID)
		} else {
			log.Printf("Known card detected: %s", cardID)
			eventMgr.EmitCardIDFound(cardID)

			// Check if card was reformatted
			lastSync := stateMgr.FindLastSyncByCardID(cardID)
			if lastSync != nil && lastSync.FilesTotal > 0 {
				// Calculate percentage of files compared to last sync
				// Note: totalFiles has been adjusted to at least 1 above if it was 0
				percentageOfLast := float64(totalFiles) / float64(lastSync.FilesTotal)
				threshold := appSettings.GetReformatThreshold()

				log.Printf("File count comparison: current=%d, last=%d, ratio=%.2f, threshold=%.2f",
					totalFiles, lastSync.FilesTotal, percentageOfLast, threshold)

				// If significantly fewer files, assume card was reformatted
				if percentageOfLast < threshold {
					log.Printf("Card appears to have been reformatted (%.1f%% of previous files)", percentageOfLast*100)
					log.Println("Creating new card ID for reformatted card")

					newCardID, err := sdmonitor.CreateNewCardID(event.MountPath, monitor)
					if err != nil {
						log.Printf("Warning: Failed to create new card ID: %v", err)
						eventMgr.EmitError("Failed to create new card ID for reformatted card", err)
					} else {
						oldCardID := cardID
						cardID = newCardID
						log.Printf("New card ID created: %s", cardID)
						eventMgr.EmitReformatDetected(oldCardID, newCardID, percentageOfLast)
					}
				}
			}
		}

		// Wait a moment before starting sync
		time.Sleep(2 * time.Second)

		// Start sync
		log.Printf("Starting sync of %d files (%.2f MB) to card folder: %s",
			totalFiles, float64(totalBytes)/(1024*1024), cardID)

		eventMgr.Emit(events.EventSyncStarting, "Preparing to start sync operation", map[string]interface{}{
			"card_id":     cardID,
			"total_files": totalFiles,
			"total_bytes": totalBytes,
		})

		_, err = stateMgr.StartSync(cardID, int64(totalFiles), totalBytes)
		if err != nil {
			log.Printf("Error starting sync: %v", err)
			eventMgr.EmitError("Failed to start sync operation", err)
			stateMgr.SetStatus(state.StatusError)
			return
		}

		// Emit sync started event
		eventMgr.EmitSyncStarted(cardID, totalFiles, totalBytes)

		// Perform sync
		dcimPath := filepath.Join(event.MountPath, "DCIM")
		err = syncMgr.Sync(dcimPath, cardID, totalFiles, totalBytes)

		if err != nil {
			log.Printf("Sync failed: %v", err)
			eventMgr.EmitSyncFailed(cardID, err)
			stateMgr.FinishSync(false, err)
			stateMgr.SetStatus(state.StatusError)
		} else {
			log.Println("Sync completed successfully!")
			eventMgr.EmitSyncCompleted(cardID, int64(totalFiles), totalBytes, time.Since(time.Now()))
			stateMgr.FinishSync(true, nil)
			stateMgr.SetStatus(state.StatusSuccess)

			// Keep success status for a few seconds, then go idle
			time.Sleep(5 * time.Second)
			stateMgr.SetStatus(state.StatusIdle)
		}
	}()
}

func handleCardRemoved(event sdmonitor.Event, stateMgr *state.Manager, syncMgr *syncmanager.Manager, eventMgr *events.Manager) {
	log.Printf("SD card removed: %s", event.DevName)
	eventMgr.EmitSDCardRemoved(event.DevName)
	stateMgr.SetSDCard(false, "")

	// If we were syncing, cancel and mark as error
	if syncMgr.IsRunning() {
		log.Println("Sync interrupted by card removal, cancelling...")
		if err := syncMgr.Cancel(); err != nil {
			log.Printf("Failed to cancel sync: %v", err)
		}
		stateMgr.FinishSync(false, fmt.Errorf("card removed during sync"))
	}

	stateMgr.SetStatus(state.StatusIdle)
}
