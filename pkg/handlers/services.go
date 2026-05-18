package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/denysvitali/pictures-sync-s3/pkg/httputil"
)

const (
	servicePicturesSync = "pictures-sync"
	serviceWebUI        = "webui"
)

var (
	findServicePIDs = findServiceProcessPIDs
	signalService   = func(pid int, sig syscall.Signal) error {
		return syscall.Kill(pid, sig)
	}
	currentPID            = os.Getpid
	scheduleServiceSignal = func(fn func()) {
		go func() {
			time.Sleep(500 * time.Millisecond)
			fn()
		}()
	}
)

type restartServicesRequest struct {
	Services []string `json:"services"`
}

type serviceRestartResult struct {
	Service string `json:"service"`
	Status  string `json:"status"`
	PIDs    []int  `json:"pids,omitempty"`
	Error   string `json:"error,omitempty"`
}

// HandleSystemServicesRestart restarts app-owned services only. On Gokrazy,
// terminating these supervised processes causes the supervisor to start them
// again without rebooting the whole device.
func (ctx *Context) HandleSystemServicesRestart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httputil.MethodNotAllowed(w)
		return
	}

	var req restartServicesRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		httputil.BadRequest(w, "Invalid JSON")
		return
	}

	services, err := normalizeRestartServices(req.Services)
	if err != nil {
		httputil.BadRequest(w, err.Error())
		return
	}

	results, restartWebUI, err := restartAppServices(services)
	if err != nil {
		httputil.JSON(w, http.StatusInternalServerError, map[string]any{
			"success": false,
			"error":   err.Error(),
			"results": results,
		})
		return
	}

	httputil.JSON(w, http.StatusAccepted, map[string]any{
		"success": true,
		"results": results,
	})
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}

	if restartWebUI {
		pid := currentPID()
		scheduleServiceSignal(func() {
			_ = signalService(pid, syscall.SIGTERM)
		})
	}
}

func normalizeRestartServices(requested []string) ([]string, error) {
	if len(requested) == 0 {
		return []string{servicePicturesSync, serviceWebUI}, nil
	}

	seen := make(map[string]bool, len(requested))
	services := make([]string, 0, len(requested))
	for _, raw := range requested {
		service := strings.ToLower(strings.TrimSpace(raw))
		if service == "app" || service == "all" {
			for _, appService := range []string{servicePicturesSync, serviceWebUI} {
				if !seen[appService] {
					seen[appService] = true
					services = append(services, appService)
				}
			}
			continue
		}
		switch service {
		case servicePicturesSync, serviceWebUI:
			if !seen[service] {
				seen[service] = true
				services = append(services, service)
			}
		default:
			return nil, fmt.Errorf("unsupported service: %s", raw)
		}
	}
	if len(services) == 0 {
		return nil, fmt.Errorf("at least one service is required")
	}
	return services, nil
}

func restartAppServices(services []string) ([]serviceRestartResult, bool, error) {
	results := make([]serviceRestartResult, 0, len(services))
	restartWebUI := false

	for _, service := range services {
		switch service {
		case servicePicturesSync:
			pids, err := findServicePIDs("/user/pictures-sync")
			if err != nil {
				results = append(results, serviceRestartResult{Service: service, Status: "error", Error: err.Error()})
				return results, false, err
			}
			if len(pids) == 0 {
				results = append(results, serviceRestartResult{Service: service, Status: "not_found"})
				continue
			}
			for _, pid := range pids {
				if err := signalService(pid, syscall.SIGTERM); err != nil {
					wrapped := fmt.Errorf("restart %s pid %d: %w", service, pid, err)
					results = append(results, serviceRestartResult{Service: service, Status: "error", PIDs: pids, Error: wrapped.Error()})
					return results, false, wrapped
				}
			}
			results = append(results, serviceRestartResult{Service: service, Status: "signaled", PIDs: pids})
		case serviceWebUI:
			restartWebUI = true
			results = append(results, serviceRestartResult{Service: service, Status: "scheduled", PIDs: []int{currentPID()}})
		}
	}

	return results, restartWebUI, nil
}

func findServiceProcessPIDs(exePath string) ([]int, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}

	pids := make([]int, 0, 1)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		exe, err := os.Readlink(filepath.Join("/proc", entry.Name(), "exe"))
		if err != nil {
			if os.IsNotExist(err) || os.IsPermission(err) {
				continue
			}
			continue
		}
		if strings.TrimSuffix(exe, " (deleted)") == exePath {
			pids = append(pids, pid)
		}
	}
	return pids, nil
}
