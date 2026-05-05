package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/denysvitali/pictures-sync-s3/pkg/sdmonitor"
	"github.com/denysvitali/pictures-sync-s3/pkg/state"
)

const (
	// successStatusDuration is how long to keep the success status visible after manual sync completion.
	// This provides visual feedback to the user via LED patterns before returning to idle state.
	successStatusDuration = 5 * time.Second
)

// HandleDevices lists all available storage devices
func (ctx *Context) HandleDevices(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	devices, err := sdmonitor.ListAllStorageDevices()
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to list devices: %v", err), http.StatusInternalServerError)
		return
	}

	// Convert to state.DeviceInfo to match the state manager's type
	stateDevices := make([]state.DeviceInfo, len(devices))
	for i, d := range devices {
		stateDevices[i] = state.DeviceInfo{
			DevicePath:  d.DevicePath,
			DeviceName:  d.DeviceName,
			Size:        d.Size,
			SizeHuman:   d.SizeHuman,
			IsUSB:       d.IsUSB,
			IsMounted:   d.IsMounted,
			MountPath:   d.MountPath,
			HasDCIM:     d.HasDCIM,
			VolumeLabel: d.VolumeLabel,
		}
	}

	// Update state manager with available devices
	ctx.StateMgr.SetAvailableDevices(stateDevices)

	JSONResponse(w, devices)
}

// HandleDeviceSelect handles manual device selection
func (ctx *Context) HandleDeviceSelect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		DevicePath string `json:"device_path"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if req.DevicePath == "" {
		http.Error(w, "device_path is required", http.StatusBadRequest)
		return
	}

	log.Printf("Manual device selection: %s", req.DevicePath)

	// TODO: Trigger sync for the selected device
	// This needs to be integrated with the main pictures-sync service

	JSONResponse(w, map[string]string{
		"status":  "ok",
		"message": "Device selection received. Integration with sync service pending.",
	})
}

// HandleSyncStart starts a manual sync operation
func (ctx *Context) HandleSyncStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Check if sync is already running
	if ctx.SyncMgr.IsRunning() {
		http.Error(w, "Sync already in progress", http.StatusConflict)
		return
	}

	// Get current state to check if SD card is mounted
	currentState := ctx.StateMgr.GetState()
	if !currentState.SDCardMounted || currentState.SDCardPath == "" {
		http.Error(w, "No SD card mounted", http.StatusBadRequest)
		return
	}

	log.Printf("Manual sync requested for mounted SD card at: %s", currentState.SDCardPath)

	if _, err := os.Stat(currentState.SDCardPath); err != nil {
		http.Error(w, fmt.Sprintf("Mounted SD card path is not accessible: %v", err), http.StatusBadRequest)
		return
	}

	// Check for DCIM directory before claiming the sync has started.
	dcimPath := filepath.Join(currentState.SDCardPath, "DCIM")
	if !sdmonitor.HasDCIM(currentState.SDCardPath) {
		log.Printf("Manual sync rejected: no DCIM directory found at %s", dcimPath)
		http.Error(w, "No DCIM directory found on mounted SD card", http.StatusBadRequest)
		return
	}

	// Count photos before returning success so the UI sees immediate setup failures.
	totalFiles, totalBytes, err := sdmonitor.CountPhotos(currentState.SDCardPath)
	if err != nil {
		log.Printf("Manual sync rejected: error counting photos: %v", err)
		http.Error(w, fmt.Sprintf("Failed to count photos on mounted SD card: %v", err), http.StatusInternalServerError)
		return
	}

	if totalFiles == 0 {
		log.Printf("Manual sync rejected: no photos found under %s", dcimPath)
		http.Error(w, "No photos found on mounted SD card", http.StatusBadRequest)
		return
	}

	// Get card ID before returning success so read-only or inaccessible cards are reported clearly.
	cardID, _, err := sdmonitor.GetOrCreateCardID(currentState.SDCardPath, nil)
	if err != nil {
		log.Printf("Manual sync rejected: error getting card ID: %v", err)
		http.Error(w, fmt.Sprintf("Failed to get SD card ID: %v", err), http.StatusInternalServerError)
		return
	}

	log.Printf("Starting manual sync of %d files (%.2f MB) for card: %s",
		totalFiles, float64(totalBytes)/(1024*1024), cardID)

	if _, err := ctx.StateMgr.StartSync(cardID, int64(totalFiles), totalBytes); err != nil {
		log.Printf("Manual sync rejected: error starting sync record: %v", err)
		http.Error(w, fmt.Sprintf("Failed to start sync: %v", err), http.StatusConflict)
		return
	}

	// Trigger sync in a goroutine to avoid blocking the HTTP response
	go func() {
		// Perform sync
		err = ctx.SyncMgr.Sync(dcimPath, cardID, totalFiles, totalBytes)

		if err != nil {
			log.Printf("Manual sync failed: %v", err)
			ctx.StateMgr.FinishSync(false, err)
			ctx.StateMgr.SetStatus(state.StatusError)
		} else {
			log.Println("Manual sync completed successfully!")
			ctx.StateMgr.FinishSync(true, nil)
			ctx.StateMgr.SetStatus(state.StatusSuccess)

			// Keep success status for a few seconds, then go idle
			time.Sleep(successStatusDuration)
			ctx.StateMgr.SetStatus(state.StatusIdle)
		}
	}()

	JSONResponse(w, map[string]string{
		"status":  "ok",
		"message": "Sync started",
	})
}

// HandleSyncCancel cancels the current sync operation
func (ctx *Context) HandleSyncCancel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if !ctx.SyncMgr.IsRunning() {
		http.Error(w, "No sync in progress", http.StatusBadRequest)
		return
	}

	log.Println("Manual sync cancellation requested")

	if err := ctx.SyncMgr.Cancel(); err != nil {
		http.Error(w, fmt.Sprintf("Failed to cancel sync: %v", err), http.StatusInternalServerError)
		return
	}

	// Update state
	ctx.StateMgr.FinishSync(false, fmt.Errorf("cancelled by user"))
	ctx.StateMgr.SetStatus(state.StatusIdle)

	JSONResponse(w, map[string]string{
		"status":  "ok",
		"message": "Sync cancelled",
	})
}
