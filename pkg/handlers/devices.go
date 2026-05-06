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
	"github.com/denysvitali/pictures-sync-s3/pkg/state"
)

// HandleDevices lists all available storage devices
func (ctx *Context) HandleDevices(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	requestCtx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	devices, err := ctx.daemonClient().RequestDevices(requestCtx)
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

	requester := ctx.manualSyncClient()

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

// HandleDeviceFormat formats the selected mounted SD card after explicit confirmation.
func (ctx *Context) HandleDeviceFormat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		DevicePath   string `json:"device_path"`
		Confirmation string `json:"confirmation"`
		Label        string `json:"label"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	if req.DevicePath == "" {
		http.Error(w, "device_path is required", http.StatusBadRequest)
		return
	}
	if req.Confirmation != "FORMAT" {
		http.Error(w, "confirmation must be FORMAT", http.StatusBadRequest)
		return
	}

	requester := ctx.manualSyncClient()
	requestCtx, cancel := context.WithTimeout(r.Context(), 2*time.Minute+5*time.Second)
	defer cancel()

	log.Printf("SD card format requested via WebUI for device: %s", req.DevicePath)
	if err := requester.RequestFormatSDCard(requestCtx, req.DevicePath, req.Label); err != nil {
		statusCode := http.StatusServiceUnavailable
		var commandErr *daemoncontrol.CommandError
		if errors.As(err, &commandErr) {
			switch commandErr.Code {
			case daemoncontrol.CodeNoSDCardMounted, daemoncontrol.CodeInvalidDevice:
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
		"message": "SD card formatted",
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

	requester := ctx.manualSyncClient()

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

	requester := ctx.manualSyncClient()

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
