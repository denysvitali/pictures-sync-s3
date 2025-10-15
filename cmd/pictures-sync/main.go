package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

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
	if err := monitor.Start(); err != nil {
		log.Fatalf("Failed to start SD card monitor: %v", err)
	}
	defer monitor.Stop()
	log.Println("SD card monitor started")

	// Set initial status
	stateMgr.SetStatus(state.StatusIdle)

	// Handle signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// Main event loop
	log.Println("Ready - waiting for SD card insertion...")

	for {
		select {
		case <-sigChan:
			log.Println("Received shutdown signal, exiting...")
			return

		case event := <-monitor.Events():
			switch event.Type {
			case sdmonitor.EventInserted:
				handleCardInserted(event, monitor, stateMgr, syncMgr, appSettings)
			case sdmonitor.EventRemoved:
				handleCardRemoved(event, stateMgr, syncMgr)
			}
		}
	}
}

func handleCardInserted(event sdmonitor.Event, monitor *sdmonitor.Monitor, stateMgr *state.Manager, syncMgr *syncmanager.Manager, appSettings *settings.Settings) {
	log.Printf("SD card inserted: %s, mounted at %s", event.DevName, event.MountPath)

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
		// Check for DCIM directory
		if !sdmonitor.HasDCIM(event.MountPath) {
			log.Println("No DCIM directory found on SD card, ignoring")
			stateMgr.SetStatus(state.StatusIdle)
			return
		}

		// Count photos
		totalFiles, totalBytes, err := sdmonitor.CountPhotos(event.MountPath)
		if err != nil {
			log.Printf("Error counting photos: %v", err)
			stateMgr.SetStatus(state.StatusIdle)
			return
		}

		log.Printf("Found %d photos (%.2f MB)", totalFiles, float64(totalBytes)/(1024*1024))

		if totalFiles == 0 {
			log.Println("No photos found on SD card")
			stateMgr.SetStatus(state.StatusIdle)
			return
		}

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
			stateMgr.SetStatus(state.StatusError)
			return
		}

		if isNewCard {
			log.Printf("New card detected: %s", cardID)
		} else {
			log.Printf("Known card detected: %s", cardID)

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
					} else {
						cardID = newCardID
						log.Printf("New card ID created: %s", cardID)
					}
				}
			}
		}

		// Wait a moment before starting sync
		time.Sleep(2 * time.Second)

		// Start sync
		log.Printf("Starting sync of %d files (%.2f MB) to card folder: %s",
			totalFiles, float64(totalBytes)/(1024*1024), cardID)

		_, err = stateMgr.StartSync(cardID, int64(totalFiles), totalBytes)
		if err != nil {
			log.Printf("Error starting sync: %v", err)
			stateMgr.SetStatus(state.StatusError)
			return
		}

		// Perform sync
		dcimPath := filepath.Join(event.MountPath, "DCIM")
		err = syncMgr.Sync(dcimPath, cardID, totalFiles, totalBytes)

		if err != nil {
			log.Printf("Sync failed: %v", err)
			stateMgr.FinishSync(false, err)
			stateMgr.SetStatus(state.StatusError)
		} else {
			log.Println("Sync completed successfully!")
			stateMgr.FinishSync(true, nil)
			stateMgr.SetStatus(state.StatusSuccess)

			// Keep success status for a few seconds, then go idle
			time.Sleep(5 * time.Second)
			stateMgr.SetStatus(state.StatusIdle)
		}
	}()
}

func handleCardRemoved(event sdmonitor.Event, stateMgr *state.Manager, syncMgr *syncmanager.Manager) {
	log.Printf("SD card removed: %s", event.DevName)
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
