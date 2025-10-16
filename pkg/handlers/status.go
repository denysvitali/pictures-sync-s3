package handlers

import (
	"log"
	"net/http"

	"github.com/denysvitali/pictures-sync-s3/pkg/auth"
)

// HandleCSRFToken returns the CSRF token
func HandleCSRFToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	JSONResponse(w, map[string]string{
		"csrf_token": auth.GetCSRFToken(),
	})
}

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
