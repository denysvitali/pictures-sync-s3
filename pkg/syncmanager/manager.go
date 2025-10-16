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
	progressChans          []chan Progress
	cancelFunc             context.CancelFunc
	isRunning              bool
	startTime              time.Time
}

// Progress represents sync progress
type Progress struct {
	BytesTransferred int64   `json:"bytes"`
	Percentage       int     `json:"percentage"`
	TransferredFiles int     `json:"transferred_files"`
	TotalFiles       int     `json:"total_files"`
	Speed            float64 `json:"speed"` // bytes per second
	ETA              int     `json:"eta"`   // seconds
	CurrentFile      string  `json:"current_file,omitempty"`      // Current file being transferred
	CurrentFileSize  int64   `json:"current_file_size,omitempty"` // Size of current file
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
		progressChans: make([]chan Progress, 0),
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

// SubscribeProgress returns a channel that receives progress updates
func (m *Manager) SubscribeProgress() chan Progress {
	m.mu.Lock()
	defer m.mu.Unlock()

	ch := make(chan Progress, 10)
	m.progressChans = append(m.progressChans, ch)
	return ch
}

// UnsubscribeProgress removes a channel from progress updates and closes it
func (m *Manager) UnsubscribeProgress(ch chan Progress) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Find and remove the channel
	for i, progressChan := range m.progressChans {
		if progressChan == ch {
			// Remove from slice
			m.progressChans = append(m.progressChans[:i], m.progressChans[i+1:]...)
			// Close the channel to signal the subscriber
			close(ch)
			break
		}
	}
}

// loadRcloneConfig loads the rclone configuration from the custom path
func (m *Manager) loadRcloneConfig() error {
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
