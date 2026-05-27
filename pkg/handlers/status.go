package handlers

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/denysvitali/pictures-sync-s3/pkg/httputil"
	"github.com/denysvitali/pictures-sync-s3/pkg/systeminfo"
)

// HandleStatus returns current system status
func (ctx *Context) HandleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	requestCtx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	status, err := ctx.daemonClient().RequestStatus(requestCtx)
	if err != nil {
		log.Printf("Failed to get daemon status: %v", err)
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}

	runtimeInfo := systeminfo.Snapshot()
	status.Runtime = &runtimeInfo
	httputil.JSON(w, http.StatusOK, status)
}

// HandleHistory returns sync history
func (ctx *Context) HandleHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	requestCtx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	history, err := ctx.daemonClient().RequestHistory(requestCtx)
	if err != nil {
		log.Printf("Failed to get daemon history: %v", err)
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}

	httputil.JSON(w, http.StatusOK, history)
}
