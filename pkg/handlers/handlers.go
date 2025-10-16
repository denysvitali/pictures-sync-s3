package handlers

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/denysvitali/pictures-sync-s3/pkg/settings"
	"github.com/denysvitali/pictures-sync-s3/pkg/ssrf"
	"github.com/denysvitali/pictures-sync-s3/pkg/state"
	"github.com/denysvitali/pictures-sync-s3/pkg/syncmanager"
	"github.com/denysvitali/pictures-sync-s3/pkg/wifimanager"
)

// Context holds dependencies for all handlers
type Context struct {
	StateMgr       *state.Manager
	SyncMgr        *syncmanager.Manager
	WiFiMgr        *wifimanager.Manager
	AppSettings    *settings.Settings
	SSRFValidator  *ssrf.Validator
}

// JSONResponse writes a JSON response
func JSONResponse(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("Failed to encode JSON response: %v", err)
	}
}
