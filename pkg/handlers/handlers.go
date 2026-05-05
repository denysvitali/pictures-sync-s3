package handlers

import (
	"encoding/json"
	"io"
	"log"
	"net/http"

	"github.com/denysvitali/pictures-sync-s3/pkg/auth"
	"github.com/denysvitali/pictures-sync-s3/pkg/captiveportal"
	"github.com/denysvitali/pictures-sync-s3/pkg/ota"
	"github.com/denysvitali/pictures-sync-s3/pkg/settings"
	"github.com/denysvitali/pictures-sync-s3/pkg/ssrf"
	"github.com/denysvitali/pictures-sync-s3/pkg/state"
	"github.com/denysvitali/pictures-sync-s3/pkg/syncmanager"
	"github.com/denysvitali/pictures-sync-s3/pkg/version"
	"github.com/denysvitali/pictures-sync-s3/pkg/wifimanager"
)

// SyncManager describes the sync operations used by HTTP handlers.
type SyncManager interface {
	IsRunning() bool
	Cancel() error
	Sync(sourcePath, cardID string, totalFiles int, totalBytes int64) error
	SetRemote(remoteName, remotePath string)
	SetGooglePhotos(enabled bool, remoteName string)
	ListRemotes() ([]string, error)
	TestConnection() error
	ListCardIDs() ([]syncmanager.FileInfo, error)
	ListFiles(path string) ([]syncmanager.FileInfo, error)
	ListFilesPaginated(path string, page, pageSize int) (*syncmanager.FileListResult, error)
	GetFile(path string, w io.Writer) error
	GetPublicLink(path string) (string, error)
}

// Context holds dependencies for all handlers
type Context struct {
	StateMgr      *state.Manager
	SyncMgr       SyncManager
	WiFiMgr       wifimanager.WiFiManager
	AppSettings   *settings.Settings
	SSRFValidator *ssrf.Validator
	CaptivePortal *captiveportal.Authenticator
	OTAMgr        *ota.Manager
	PasswordMgr   *auth.PasswordManager
}

func (ctx *Context) VersionInfo() version.Info {
	return version.Get()
}

// JSONResponse writes a JSON response
func JSONResponse(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("Failed to encode JSON response: %v", err)
	}
}
