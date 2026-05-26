package syncmanager

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/denysvitali/pictures-sync-s3/pkg/state"
	"github.com/rclone/rclone/fs/config"
	"github.com/rclone/rclone/fs/config/configfile"

	// Import storage backends - these register themselves with rclone
	_ "github.com/rclone/rclone/backend/azureblob" // Azure Blob Storage
	_ "github.com/rclone/rclone/backend/b2"        // Backblaze B2
	_ "github.com/rclone/rclone/backend/drive"     // Google Drive (for Google Photos)
	_ "github.com/rclone/rclone/backend/local"     // Local filesystem (useful for testing)
	_ "github.com/rclone/rclone/backend/s3"        // Amazon S3, Wasabi, etc.
)

// Manager manages rclone sync operations
type Manager struct {
	configPath             string
	remoteName             string
	remotePath             string
	stateMgr               *state.Manager
	transfers              int    // Number of parallel transfers
	checkers               int    // Number of parallel checkers
	googlePhotosEnabled    bool   // Enable Google Photos upload
	googlePhotosRemoteName string // Google Photos remote name
	mu                     sync.Mutex
	progressChans          []*progressSubscriber
	cancelFunc             context.CancelFunc
	isRunning              bool
	startTime              time.Time
	// Google Photos sync state (separate from main B2 sync)
	googlePhotosRunning  bool
	googlePhotosCancel   context.CancelFunc
	googlePhotosProgress Progress
}

// Progress represents sync progress
type Progress struct {
	BytesTransferred int64   `json:"bytes"`
	Percentage       int     `json:"percentage"`
	TransferredFiles int     `json:"transferred_files"`
	TotalFiles       int     `json:"total_files"`
	Speed            float64 `json:"speed"`                       // bytes per second
	ETA              int     `json:"eta"`                         // seconds
	CurrentFile      string  `json:"current_file,omitempty"`      // Current file being transferred
	CurrentFileSize  int64   `json:"current_file_size,omitempty"` // Size of current file
	Status           string  `json:"status,omitempty"`            // sync status: syncing, completed, error, cancelled
}

// NewManager creates a new sync manager
func NewManager(configPath, remoteName, remotePath string, stateMgr *state.Manager, transfers, checkers int) *Manager {
	return &Manager{
		configPath:    configPath,
		remoteName:    remoteName,
		remotePath:    remotePath,
		stateMgr:      stateMgr,
		transfers:     transfers,
		checkers:      checkers,
		progressChans: make([]*progressSubscriber, 0),
	}
}

// GetConfig returns the current rclone config path
func (m *Manager) GetConfig() string {
	return m.configPath
}

// SetRemote updates the remote name and path
func (m *Manager) SetRemote(remoteName, remotePath string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.remoteName = remoteName
	m.remotePath = remotePath
}

// ApplySettings updates sync runtime settings from persisted application
// settings. It is used by the daemon before starting a sync so changes made by
// the separate web UI process are picked up without a restart.
func (m *Manager) ApplySettings(remoteName, remotePath string, transfers, checkers int, googlePhotosEnabled bool, googlePhotosRemoteName string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.remoteName = remoteName
	m.remotePath = remotePath
	m.transfers = transfers
	m.checkers = checkers
	m.googlePhotosEnabled = googlePhotosEnabled
	m.googlePhotosRemoteName = googlePhotosRemoteName
}

// SetGooglePhotos updates the Google Photos settings
func (m *Manager) SetGooglePhotos(enabled bool, remoteName string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.googlePhotosEnabled = enabled
	m.googlePhotosRemoteName = remoteName
}

// Cancel cancels the current sync operation
func (m *Manager) Cancel() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.isRunning {
		return fmt.Errorf("no sync in progress")
	}

	if m.cancelFunc != nil {
		m.cancelFunc()
	}

	return nil
}

// IsRunning returns true if a sync is currently running
func (m *Manager) IsRunning() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.isRunning
}

// progressSubscriber wraps a progress channel with coordination so that
// UnsubscribeProgress can close the channel without racing concurrent
// broadcastProgress senders.
type progressSubscriber struct {
	mu     sync.Mutex
	ch     chan Progress
	closed bool
}

func (s *progressSubscriber) send(p Progress) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	select {
	case s.ch <- p:
	default:
		// Skip if channel is full
	}
}

func (s *progressSubscriber) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.closed = true
	close(s.ch)
}

// SubscribeProgress returns a channel that receives progress updates
func (m *Manager) SubscribeProgress() chan Progress {
	m.mu.Lock()
	defer m.mu.Unlock()

	sub := &progressSubscriber{ch: make(chan Progress, 10)}
	m.progressChans = append(m.progressChans, sub)
	return sub.ch
}

// UnsubscribeProgress removes a channel from progress updates and closes it
func (m *Manager) UnsubscribeProgress(ch chan Progress) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i, sub := range m.progressChans {
		if sub.ch == ch {
			m.progressChans = append(m.progressChans[:i], m.progressChans[i+1:]...)
			sub.close()
			break
		}
	}
}

// rcloneConfigMu serializes access to rclone's package-level configuration
// globals (SetConfigPath, Install, Data) which are not safe for concurrent
// mutation. All call sites that touch the global rclone config — both the
// initial load and any subsequent reads of the parsed config (storage.Load,
// storage.GetSectionList, fs.NewFs, etc.) — must hold this lock for the
// entire duration of the operation, otherwise another goroutine can swap
// the global config out from under them and the race detector will fire.
var rcloneConfigMu sync.Mutex

// LockRcloneConfig acquires the package-level rclone config mutex. Callers
// must pair this with UnlockRcloneConfig (typically via defer) and hold the
// lock for the full duration of any rclone config / fs operation.
func LockRcloneConfig()   { rcloneConfigMu.Lock() }
func UnlockRcloneConfig() { rcloneConfigMu.Unlock() }

// loadRcloneConfig loads the rclone configuration from the custom path.
// It serializes against other rclone-config mutations via rcloneConfigMu.
func (m *Manager) loadRcloneConfig() error {
	rcloneConfigMu.Lock()
	defer rcloneConfigMu.Unlock()
	return m.loadRcloneConfigLocked()
}

// loadRcloneConfigLocked performs the actual rclone config load. Caller MUST
// hold rcloneConfigMu.
func (m *Manager) loadRcloneConfigLocked() error {
	if err := config.SetConfigPath(m.configPath); err != nil {
		return fmt.Errorf("failed to set config path: %w", err)
	}
	configfile.Install()
	storage := &configfile.Storage{}
	if err := storage.Load(); err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	return nil
}
