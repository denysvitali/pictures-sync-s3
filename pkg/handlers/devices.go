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

	requester := ctx.ManualSync
	if requester == nil {
		requester = DaemonControlFunc{
			ManualSync: daemoncontrol.RequestManualSyncWithPath,
			CancelSync: daemoncontrol.RequestCancelSync,
		}
	}

	requestCtx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	log.Printf("Manual device selection requested via WebUI; syncing device: %s", req.DevicePath)
	if err := requester.RequestManualSync(requestCtx, req.DevicePath); err != nil {
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
		"message": "Manual sync requested for selected device",
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
		requester = DaemonControlFunc{
			ManualSync: daemoncontrol.RequestManualSync,
			CancelSync: daemoncontrol.RequestCancelSync,
		}
	}

	requestCtx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	log.Println("Manual sync requested via WebUI; forwarding to pictures-sync daemon")
	if err := requester.RequestManualSync(requestCtx, ""); err != nil {
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

	requester := ctx.ManualSync
	if requester == nil {
		requester = DaemonControlFunc{
			ManualSync: daemoncontrol.RequestManualSync,
			CancelSync: daemoncontrol.RequestCancelSync,
		}
	}

	requestCtx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	log.Println("Manual sync cancellation requested via WebUI; forwarding to pictures-sync daemon")
	if err := requester.RequestCancelSync(requestCtx); err != nil {
		statusCode := http.StatusServiceUnavailable
		var commandErr *daemoncontrol.CommandError
		if errors.As(err, &commandErr) {
			switch commandErr.Code {
			case daemoncontrol.CodeSyncAlreadyActive:
				statusCode = http.StatusBadRequest
			}
		}
		http.Error(w, err.Error(), statusCode)
		return
	}

	JSONResponse(w, map[string]string{
		"status":  "ok",
		"message": "Sync cancelled",
	})
}
