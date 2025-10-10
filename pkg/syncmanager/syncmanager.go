package syncmanager

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/denysvitali/pictures-sync-s3/pkg/state"
	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/accounting"
	"github.com/rclone/rclone/fs/config"
	"github.com/rclone/rclone/fs/config/configfile"
	"github.com/rclone/rclone/fs/operations"
	rcsync "github.com/rclone/rclone/fs/sync"

	// Import storage backends - these register themselves with rclone
	_ "github.com/rclone/rclone/backend/b2"        // Backblaze B2
	_ "github.com/rclone/rclone/backend/s3"        // Amazon S3, Wasabi, etc.
	_ "github.com/rclone/rclone/backend/drive"     // Google Drive (for Google Photos)
	_ "github.com/rclone/rclone/backend/azureblob" // Azure Blob Storage
	_ "github.com/rclone/rclone/backend/local"     // Local filesystem (useful for testing)
)

// Manager manages rclone sync operations
type Manager struct {
	configPath    string
	remoteName    string
	remotePath    string
	stateMgr      *state.Manager
	transfers     int // Number of parallel transfers
	checkers      int // Number of parallel checkers
	mu            sync.Mutex
	progressChans []chan Progress
	cancelFunc    context.CancelFunc
	isRunning     bool
	startTime     time.Time
}

// Progress represents sync progress
type Progress struct {
	BytesTransferred int64   `json:"bytes"`
	Percentage       int     `json:"percentage"`
	TransferredFiles int     `json:"transferred_files"`
	TotalFiles       int     `json:"total_files"`
	Speed            float64 `json:"speed"` // bytes per second
	ETA              int     `json:"eta"`   // seconds
	CurrentFile      string  `json:"current_file,omitempty"` // Current file being transferred
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

// Sync synchronizes files from source to remote
func (m *Manager) Sync(sourcePath, cardID string, totalFiles int, totalBytes int64) error {
	m.mu.Lock()
	if m.isRunning {
		m.mu.Unlock()
		return fmt.Errorf("sync already in progress")
	}
	m.isRunning = true
	m.mu.Unlock()

	defer func() {
		m.mu.Lock()
		m.isRunning = false
		m.cancelFunc = nil
		m.mu.Unlock()
	}()

	// Load rclone config from custom path
	if err := config.SetConfigPath(m.configPath); err != nil {
		return fmt.Errorf("failed to set config path: %w", err)
	}
	configfile.Install()
	storage := &configfile.Storage{}
	if err := storage.Load(); err != nil {
		log.Printf("Warning: failed to load config file: %v", err)
	}

	// Record start time for elapsed time calculation
	m.startTime = time.Now()

	// Create context with cancel
	ctx, cancel := context.WithCancel(context.Background())
	m.mu.Lock()
	m.cancelFunc = cancel
	m.mu.Unlock()

	defer cancel()

	// Construct destination path: remote:/photos/{cardID}/DCIM/
	destPath := filepath.Join(m.remoteName+":"+m.remotePath, cardID, "DCIM")

	log.Printf("Syncing from %s to %s", sourcePath, destPath)

	// Create source filesystem
	srcFs, err := fs.NewFs(ctx, sourcePath)
	if err != nil {
		return fmt.Errorf("failed to create source filesystem: %w", err)
	}

	// Create destination filesystem
	dstFs, err := fs.NewFs(ctx, destPath)
	if err != nil {
		return fmt.Errorf("failed to create destination filesystem: %w", err)
	}

	// Set up config for progress tracking and parallel transfers
	ci := fs.GetConfig(ctx)
	ci.StatsOneLine = true
	ci.Progress = true
	ci.Transfers = m.transfers  // Upload multiple files in parallel for better performance
	ci.Checkers = m.checkers    // Use multiple checkers to compare files in parallel

	// Create accounting stats
	stats := accounting.GlobalStats()

	// Start progress monitoring
	done := make(chan struct{})
	go m.monitorProgress(ctx, stats, totalFiles, totalBytes, done)

	// Perform sync operation
	log.Printf("Starting sync operation...")
	err = rcsync.Sync(ctx, dstFs, srcFs, false)

	// Stop progress monitoring
	close(done)

	if err != nil {
		return fmt.Errorf("sync failed: %w", err)
	}

	log.Printf("Sync completed successfully")
	return nil
}

// monitorProgress monitors sync progress and updates state
func (m *Manager) monitorProgress(ctx context.Context, stats *accounting.StatsInfo, totalFiles int, totalBytes int64, done chan struct{}) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-done:
			return
		case <-ticker.C:
			// Get current stats
			transferred := stats.GetBytes()
			transfers := int(stats.GetTransfers())

			// Get current file being transferred from RemoteStats
			var currentFile string
			var currentFileSize int64

			// Try to get remote stats which includes current transfers
			if remoteStats, err := stats.RemoteStats(true); err == nil {
				if transferring, ok := remoteStats["transferring"].([]interface{}); ok && len(transferring) > 0 {
					if transfer, ok := transferring[0].(map[string]interface{}); ok {
						if name, ok := transfer["name"].(string); ok {
							currentFile = name
						}
						if size, ok := transfer["size"].(int64); ok {
							currentFileSize = size
						}
					}
				}
			}

			// Calculate speed from elapsed time and bytes
			elapsed := time.Since(m.startTime)
			var speed float64
			if elapsed > 0 {
				speed = float64(transferred) / elapsed.Seconds()
			}

			// Calculate percentage
			var percentage int
			if totalBytes > 0 {
				percentage = int((float64(transferred) / float64(totalBytes)) * 100)
			}

			// Calculate ETA
			var eta int
			if speed > 0 {
				remaining := totalBytes - transferred
				eta = int(float64(remaining) / speed)
			}

			// Update state manager with current file info
			m.stateMgr.UpdateSyncProgress(int64(transfers), transferred, currentFile, currentFileSize)

			// Create progress update
			progress := Progress{
				BytesTransferred: transferred,
				Percentage:       percentage,
				TransferredFiles: transfers,
				TotalFiles:       totalFiles,
				Speed:            speed,
				ETA:              eta,
				CurrentFile:      currentFile,
				CurrentFileSize:  currentFileSize,
			}

			// Send to subscribers
			m.mu.Lock()
			chans := m.progressChans
			m.mu.Unlock()

			for _, ch := range chans {
				select {
				case ch <- progress:
				default:
					// Skip if channel is full
				}
			}

			log.Printf("Progress: %d/%d files, %d%%, %.2f MB/s",
				transfers, totalFiles, percentage, speed/(1024*1024))
		}
	}
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

// TestConnection tests the rclone configuration
func (m *Manager) TestConnection() error {
	// Load rclone config from custom path
	if err := config.SetConfigPath(m.configPath); err != nil {
		return fmt.Errorf("failed to set config path: %w", err)
	}
	configfile.Install()
	storage := &configfile.Storage{}
	if err := storage.Load(); err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	ctx := context.Background()

	// Try to create the remote filesystem
	remotePath := m.remoteName + ":"
	fsys, err := fs.NewFs(ctx, remotePath)
	if err != nil {
		return fmt.Errorf("connection test failed: %w", err)
	}

	// Try to list the root directory (using a buffer to capture output)
	var buf bytes.Buffer
	if err := operations.List(ctx, fsys, &buf); err != nil {
		return fmt.Errorf("failed to list remote: %w", err)
	}

	log.Printf("Connection test successful")
	return nil
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

// ListRemotes lists configured remotes
func (m *Manager) ListRemotes() ([]string, error) {
	// Load rclone config from custom path
	if err := config.SetConfigPath(m.configPath); err != nil {
		return nil, fmt.Errorf("failed to set config path: %w", err)
	}
	configfile.Install()
	storage := &configfile.Storage{}
	if err := storage.Load(); err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	// Get all configured remotes from the config file
	sections := storage.GetSectionList()

	remotes := make([]string, 0, len(sections))
	for _, section := range sections {
		section = strings.TrimSpace(section)
		if section != "" {
			remotes = append(remotes, section)
		}
	}

	return remotes, nil
}
