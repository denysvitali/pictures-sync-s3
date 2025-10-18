package handlers

import (
	"log"
	"net/http"
)

// HandleStatus returns current system status
func (ctx *Context) HandleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Reload state from disk to get latest updates from pictures-sync service
	if err := ctx.StateMgr.Reload(); err != nil {
		log.Printf("Failed to reload state: %v", err)
	}

	status := ctx.StateMgr.GetState()
	JSONResponse(w, status)
}

// HandleHistory returns sync history
func (ctx *Context) HandleHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	history := ctx.StateMgr.GetHistory()
	JSONResponse(w, history)
}
