package handlers

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"

	"github.com/denysvitali/pictures-sync-s3/pkg/auth"
	"github.com/denysvitali/pictures-sync-s3/pkg/captiveportal"
	"github.com/denysvitali/pictures-sync-s3/pkg/daemoncontrol"
	"github.com/denysvitali/pictures-sync-s3/pkg/ota"
	"github.com/denysvitali/pictures-sync-s3/pkg/sdcardbrowser"
	"github.com/denysvitali/pictures-sync-s3/pkg/sdmonitor"
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

type ManualSyncRequester interface {
	RequestManualSync(context.Context, string) error
	RequestCancelSync(context.Context) error
	RequestFormatSDCard(context.Context, string, string) error
	RequestRedetectSDCard(context.Context) error
}

type DaemonClient interface {
	ManualSyncRequester
	RequestStatus(context.Context) (state.CurrentState, error)
	RequestHistory(context.Context) ([]state.SyncRecord, error)
	RequestDevices(context.Context) ([]sdmonitor.DeviceInfo, error)
	RequestSDCardFiles(context.Context, string) (*sdcardbrowser.FileList, error)
	RequestSDCardPreview(context.Context, string) (*sdcardbrowser.Preview, error)
	RequestSDCardThumbnail(context.Context, string) (*sdcardbrowser.Preview, error)
}

type DaemonControlFunc struct {
	ManualSync     func(context.Context, string) error
	CancelSync     func(context.Context) error
	FormatSDCard   func(context.Context, string, string) error
	RedetectSDCard func(context.Context) error
}

func (f DaemonControlFunc) RequestManualSync(ctx context.Context, devicePath string) error {
	return f.ManualSync(ctx, devicePath)
}

func (f DaemonControlFunc) RequestCancelSync(ctx context.Context) error {
	return f.CancelSync(ctx)
}

func (f DaemonControlFunc) RequestFormatSDCard(ctx context.Context, devicePath, label string) error {
	if f.FormatSDCard != nil {
		return f.FormatSDCard(ctx, devicePath, label)
	}
	return &daemoncontrol.CommandError{
		Code:    daemoncontrol.CodeUnavailable,
		Message: "format SD card command is not available",
	}
}

func (f DaemonControlFunc) RequestRedetectSDCard(ctx context.Context) error {
	if f.RedetectSDCard != nil {
		return f.RedetectSDCard(ctx)
	}
	return &daemoncontrol.CommandError{
		Code:    daemoncontrol.CodeUnavailable,
		Message: "redetect SD card command is not available",
	}
}

type DaemonControlClient struct{}

func (DaemonControlClient) RequestManualSync(ctx context.Context, devicePath string) error {
	if devicePath != "" {
		return daemoncontrol.RequestManualSyncWithPath(ctx, devicePath)
	}
	return daemoncontrol.RequestManualSync(ctx)
}

func (DaemonControlClient) RequestCancelSync(ctx context.Context) error {
	return daemoncontrol.RequestCancelSync(ctx)
}

func (DaemonControlClient) RequestFormatSDCard(ctx context.Context, devicePath, label string) error {
	return daemoncontrol.RequestFormatSDCard(ctx, devicePath, label)
}

func (DaemonControlClient) RequestRedetectSDCard(ctx context.Context) error {
	return daemoncontrol.RequestRedetectSDCard(ctx)
}

func (DaemonControlClient) RequestStatus(ctx context.Context) (state.CurrentState, error) {
	return daemoncontrol.RequestStatus(ctx)
}

func (DaemonControlClient) RequestHistory(ctx context.Context) ([]state.SyncRecord, error) {
	return daemoncontrol.RequestHistory(ctx)
}

func (DaemonControlClient) RequestDevices(ctx context.Context) ([]sdmonitor.DeviceInfo, error) {
	return daemoncontrol.RequestDevices(ctx)
}

func (DaemonControlClient) RequestSDCardFiles(ctx context.Context, path string) (*sdcardbrowser.FileList, error) {
	return daemoncontrol.RequestSDCardFiles(ctx, path)
}

func (DaemonControlClient) RequestSDCardPreview(ctx context.Context, path string) (*sdcardbrowser.Preview, error) {
	return daemoncontrol.RequestSDCardPreview(ctx, path)
}

func (DaemonControlClient) RequestSDCardThumbnail(ctx context.Context, path string) (*sdcardbrowser.Preview, error) {
	return daemoncontrol.RequestSDCardThumbnail(ctx, path)
}

// Context holds dependencies for all handlers
type Context struct {
	StateMgr      *state.Manager
	SyncMgr       SyncManager
	ManualSync    ManualSyncRequester
	Daemon        DaemonClient
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

func (ctx *Context) daemonClient() DaemonClient {
	if ctx.Daemon != nil {
		return ctx.Daemon
	}
	return DaemonControlClient{}
}

func (ctx *Context) manualSyncClient() ManualSyncRequester {
	if ctx.ManualSync != nil {
		return ctx.ManualSync
	}
	return ctx.daemonClient()
}

// JSONResponse writes a JSON response
func JSONResponse(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("Failed to encode JSON response: %v", err)
	}
}
