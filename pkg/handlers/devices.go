package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/denysvitali/pictures-sync-s3/pkg/daemoncontrol"
	"github.com/denysvitali/pictures-sync-s3/pkg/sdmonitor"
	"github.com/denysvitali/pictures-sync-s3/pkg/state"
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

	requester := ctx.ManualSync
	if requester == nil {
		requester = ManualSyncFunc(daemoncontrol.RequestManualSync)
	}

	requestCtx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	log.Println("Manual sync requested via WebUI; forwarding to pictures-sync daemon")
	if err := requester.RequestManualSync(requestCtx); err != nil {
		statusCode := http.StatusServiceUnavailable
		var commandErr *daemoncontrol.CommandError
		if errors.As(err, &commandErr) {
			switch commandErr.Code {
			case daemoncontrol.CodeNoSDCardMounted:
				statusCode = http.StatusBadRequest
			case daemoncontrol.CodeSyncAlreadyActive:
				statusCode = http.StatusConflict
			}
		}
		http.Error(w, err.Error(), statusCode)
		return
	}

	JSONResponse(w, map[string]string{
		"status":  "ok",
		"message": "Sync start requested",
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
