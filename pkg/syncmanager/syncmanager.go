package syncmanager

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"path/filepath"
	"regexp"
	"sort"
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

// validateCardID checks if a card ID is safe to use in paths
func validateCardID(cardID string) error {
	if cardID == "" {
		return fmt.Errorf("card ID cannot be empty")
	}

	// Check for path traversal attempts
	if strings.Contains(cardID, "..") || strings.Contains(cardID, "/") || strings.Contains(cardID, "\\") {
		return fmt.Errorf("card ID contains invalid characters")
	}

	// Ensure card ID matches expected format (card-XXXXXXXX)
	validCardID := regexp.MustCompile(`^card-[a-zA-Z0-9]{8}$`)
	if !validCardID.MatchString(cardID) {
		return fmt.Errorf("card ID format invalid, expected: card-XXXXXXXX")
	}

	return nil
}

// isRetryableError determines if an error is worth retrying
func isRetryableError(err error) bool {
	if err == nil {
		return false
	}

	errStr := strings.ToLower(err.Error())

	// Network-related errors
	if strings.Contains(errStr, "connection refused") ||
		strings.Contains(errStr, "connection reset") ||
		strings.Contains(errStr, "timeout") ||
		strings.Contains(errStr, "temporary failure") ||
		strings.Contains(errStr, "no such host") ||
		strings.Contains(errStr, "network is unreachable") ||
		strings.Contains(errStr, "broken pipe") {
		return true
	}

	// Rate limiting errors
	if strings.Contains(errStr, "rate limit") ||
		strings.Contains(errStr, "too many requests") ||
		strings.Contains(errStr, "429") {
		return true
	}

	// Temporary server errors
	if strings.Contains(errStr, "503") ||
		strings.Contains(errStr, "502") ||
		strings.Contains(errStr, "500") {
		return true
	}

	return false
}

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

// GetRemoteSize returns the total size of files already on the remote for a given card ID
func (m *Manager) GetRemoteSize(cardID string) (int64, error) {
	// Sanitize cardID to prevent path traversal
	if err := validateCardID(cardID); err != nil {
		return 0, fmt.Errorf("invalid card ID: %w", err)
	}

	// Load rclone config from custom path
	if err := config.SetConfigPath(m.configPath); err != nil {
		return 0, fmt.Errorf("failed to set config path: %w", err)
	}
	configfile.Install()
	storage := &configfile.Storage{}
	if err := storage.Load(); err != nil {
		log.Printf("Warning: failed to load config file: %v", err)
	}

	// OPTIMIZATION: Skip remote size calculation - it's too slow with many files
	// The remote size was only used for resume support, but rclone handles that internally
	// Listing thousands of files just to get total size can take minutes with large backups
	// Instead, return 0 and let rclone handle sync efficiently with its own internal tracking

	// Note: This means we can't show "X MB already synced" in logs,
	// but it's a worthwhile tradeoff for performance (minutes → seconds)

	// Rclone's sync algorithm handles resume/incremental sync efficiently:
	// - Only transfers files that don't exist or have changed
	// - Uses checksums to detect changes
	// - Handles partial transfers automatically

	return 0, nil
}

// Sync synchronizes files from source to remote
func (m *Manager) Sync(sourcePath, cardID string, totalFiles int, totalBytes int64) error {
	// Sanitize cardID to prevent path traversal
	if err := validateCardID(cardID); err != nil {
		return fmt.Errorf("invalid card ID: %w", err)
	}

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

	// Get size of files already synced to remote (for resume support)
	alreadySyncedBytes, err := m.GetRemoteSize(cardID)
	if err != nil {
		log.Printf("Warning: Failed to get remote size: %v", err)
		alreadySyncedBytes = 0
	}
	log.Printf("Already synced: %.2f MB of %.2f MB",
		float64(alreadySyncedBytes)/(1024*1024),
		float64(totalBytes)/(1024*1024))

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
	go m.monitorProgress(ctx, stats, totalFiles, totalBytes, alreadySyncedBytes, done)

	// Perform sync operation with retry logic
	log.Printf("Starting sync operation...")
	const maxRetries = 3
	var lastErr error

	for attempt := 1; attempt <= maxRetries; attempt++ {
		// Check if context was cancelled
		select {
		case <-ctx.Done():
			close(done)
			return fmt.Errorf("sync cancelled")
		default:
		}

		err = rcsync.Sync(ctx, dstFs, srcFs, false)
		if err == nil {
			// Success!
			break
		}

		lastErr = err

		// Check if it's a network error and we should retry
		if attempt < maxRetries && isRetryableError(err) {
			log.Printf("Sync attempt %d failed with retryable error: %v. Retrying in 5 seconds...", attempt, err)

			// Wait before retry (with context cancellation check)
			select {
			case <-time.After(5 * time.Second):
				// Continue to next attempt
			case <-ctx.Done():
				close(done)
				return fmt.Errorf("sync cancelled during retry wait")
			}
		} else {
			// Non-retryable error or last attempt
			break
		}
	}

	// Stop progress monitoring
	close(done)

	if lastErr != nil {
		return fmt.Errorf("sync failed after %d attempts: %w", maxRetries, lastErr)
	}

	log.Printf("Sync completed successfully")

	// Upload JPG files to Google Photos if enabled
	if m.googlePhotosEnabled && m.googlePhotosRemoteName != "" {
		log.Printf("Starting Google Photos upload for JPG files...")
		if err := m.uploadToGooglePhotos(ctx, sourcePath, cardID); err != nil {
			log.Printf("Warning: Google Photos upload failed: %v", err)
			// Don't return error - main sync succeeded
		} else {
			log.Printf("Google Photos upload completed successfully")
		}
	}

	return nil
}

// monitorProgress monitors sync progress and updates state
func (m *Manager) monitorProgress(ctx context.Context, stats *accounting.StatsInfo, totalFiles int, totalBytes int64, alreadySyncedBytes int64, done chan struct{}) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-done:
			return
		case <-ticker.C:
			// Get current stats (bytes transferred in this session only)
			sessionTransferred := stats.GetBytes()
			transfers := int(stats.GetTransfers())

			// Calculate total bytes including what was already synced
			totalTransferred := alreadySyncedBytes + sessionTransferred

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

			// Calculate speed from elapsed time and bytes transferred in this session
			elapsed := time.Since(m.startTime)
			var speed float64
			if elapsed > 0 {
				speed = float64(sessionTransferred) / elapsed.Seconds()
			}

			// Calculate percentage based on total progress (including already synced)
			var percentage int
			if totalBytes > 0 {
				percentage = int((float64(totalTransferred) / float64(totalBytes)) * 100)
			}

			// Calculate ETA and format it (based on remaining bytes and current speed)
			var etaSeconds int
			var etaStr string
			if speed > 0 {
				remaining := totalBytes - totalTransferred
				etaSeconds = int(float64(remaining) / speed)
				etaStr = formatDuration(etaSeconds)
			}

			// Update state manager with current file info including speed and ETA
			// Note: We report totalTransferred (including already synced) for accurate percentage
			m.stateMgr.UpdateSyncProgress(int64(transfers), totalTransferred, currentFile, currentFileSize, speed, etaStr)

			// Create progress update
			progress := Progress{
				BytesTransferred: totalTransferred, // Use total including already synced
				Percentage:       percentage,
				TransferredFiles: transfers,
				TotalFiles:       totalFiles,
				Speed:            speed,
				ETA:              etaSeconds,
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

// SetGooglePhotos updates the Google Photos settings
func (m *Manager) SetGooglePhotos(enabled bool, remoteName string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.googlePhotosEnabled = enabled
	m.googlePhotosRemoteName = remoteName
}

// formatDuration formats seconds into a human-readable duration string
func formatDuration(seconds int) string {
	if seconds < 60 {
		return fmt.Sprintf("%ds", seconds)
	}
	minutes := seconds / 60
	if minutes < 60 {
		return fmt.Sprintf("%dm %ds", minutes, seconds%60)
	}
	hours := minutes / 60
	return fmt.Sprintf("%dh %dm", hours, minutes%60)
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

// FileInfo represents a file or directory on the remote
type FileInfo struct {
	Name    string    `json:"name"`
	Path    string    `json:"path"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"mod_time"`
	IsDir   bool      `json:"is_dir"`
}

// FileListResult represents paginated file listing result
type FileListResult struct {
	Files      []FileInfo `json:"files"`
	Path       string     `json:"path"`
	Total      int        `json:"total"`
	Page       int        `json:"page"`
	PageSize   int        `json:"page_size"`
	TotalPages int        `json:"total_pages"`
	HasMore    bool       `json:"has_more"`
}

// ListCardIDs lists all card IDs (card-* directories) in the photos folder
func (m *Manager) ListCardIDs() ([]FileInfo, error) {
	// Load rclone config
	if err := config.SetConfigPath(m.configPath); err != nil {
		return nil, fmt.Errorf("failed to set config path: %w", err)
	}
	configfile.Install()
	storage := &configfile.Storage{}
	if err := storage.Load(); err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	ctx := context.Background()

	// List root photos directory
	fullPath := m.remoteName + ":" + m.remotePath
	fsys, err := fs.NewFs(ctx, fullPath)
	if err != nil {
		return nil, fmt.Errorf("failed to access remote path: %w", err)
	}

	entries, err := fsys.List(ctx, "")
	if err != nil {
		return nil, fmt.Errorf("failed to list entries: %w", err)
	}

	var cardDirs []FileInfo
	for _, entry := range entries {
		if dir, ok := entry.(fs.Directory); ok {
			name := dir.Remote()
			// Only include card-* directories
			if strings.HasPrefix(name, "card-") {
				cardDirs = append(cardDirs, FileInfo{
					Name:    name,
					Path:    name,
					Size:    0,
					ModTime: dir.ModTime(ctx),
					IsDir:   true,
				})
			}
		}
	}

	// Sort by modification time (most recent first)
	sort.Slice(cardDirs, func(i, j int) bool {
		return cardDirs[i].ModTime.After(cardDirs[j].ModTime)
	})

	return cardDirs, nil
}

// ListFilesPaginated lists files with pagination support
func (m *Manager) ListFilesPaginated(path string, page, pageSize int) (*FileListResult, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 1000 {
		pageSize = 100 // Default page size
	}

	// Get all files first (we'll optimize this later with streaming)
	allFiles, err := m.ListFiles(path)
	if err != nil {
		return nil, err
	}

	total := len(allFiles)
	totalPages := (total + pageSize - 1) / pageSize

	// Calculate slice bounds for pagination
	start := (page - 1) * pageSize
	end := start + pageSize

	if start >= total {
		// Page beyond available data
		return &FileListResult{
			Files:      []FileInfo{},
			Path:       path,
			Total:      total,
			Page:       page,
			PageSize:   pageSize,
			TotalPages: totalPages,
			HasMore:    false,
		}, nil
	}

	if end > total {
		end = total
	}

	return &FileListResult{
		Files:      allFiles[start:end],
		Path:       path,
		Total:      total,
		Page:       page,
		PageSize:   pageSize,
		TotalPages: totalPages,
		HasMore:    page < totalPages,
	}, nil
}

// ListFiles lists files and directories at the given path on the remote
func (m *Manager) ListFiles(path string) ([]FileInfo, error) {
	// Load rclone config from custom path
	if err := config.SetConfigPath(m.configPath); err != nil {
		return nil, fmt.Errorf("failed to set config path: %w", err)
	}
	configfile.Install()
	storage := &configfile.Storage{}
	if err := storage.Load(); err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	ctx := context.Background()

	// Construct full remote path
	var fullPath string
	if path == "" || path == "/" {
		fullPath = m.remoteName + ":" + m.remotePath
	} else {
		fullPath = m.remoteName + ":" + filepath.Join(m.remotePath, path)
	}

	// Create remote filesystem
	fsys, err := fs.NewFs(ctx, fullPath)
	if err != nil {
		return nil, fmt.Errorf("failed to access remote path: %w", err)
	}

	var files []FileInfo

	// Use List to get both files and directories at current level
	entries, err := fsys.List(ctx, "")
	if err != nil {
		return nil, fmt.Errorf("failed to list entries: %w", err)
	}

	for _, entry := range entries {
		switch item := entry.(type) {
		case fs.Directory:
			files = append(files, FileInfo{
				Name:    item.Remote(),
				Path:    item.Remote(),
				Size:    0,
				ModTime: item.ModTime(ctx),
				IsDir:   true,
			})
		case fs.Object:
			files = append(files, FileInfo{
				Name:    item.Remote(),
				Path:    item.Remote(),
				Size:    item.Size(),
				ModTime: item.ModTime(ctx),
				IsDir:   false,
			})
		}
	}

	return files, nil
}

// GetFile retrieves a file from the remote and writes it to the provided writer
func (m *Manager) GetFile(path string, w io.Writer) error {
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

	// Split path into directory and filename
	dir := filepath.Dir(path)
	filename := filepath.Base(path)

	// Construct the directory path on remote
	var remoteDirPath string
	if dir == "." || dir == "/" {
		// File is at root of remotePath
		remoteDirPath = m.remoteName + ":" + m.remotePath
	} else {
		// File is in a subdirectory
		remoteDirPath = m.remoteName + ":" + filepath.Join(m.remotePath, dir)
	}

	// Create filesystem for the directory containing the file
	fsys, err := fs.NewFs(ctx, remoteDirPath)
	if err != nil {
		return fmt.Errorf("failed to access remote directory %s: %w", remoteDirPath, err)
	}

	// Get the file object
	obj, err := fsys.NewObject(ctx, filename)
	if err != nil {
		return fmt.Errorf("failed to get file object %s in %s: %w", filename, remoteDirPath, err)
	}

	// Open the file for reading
	rc, err := obj.Open(ctx)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer rc.Close()

	// Copy the file content to the writer
	_, err = io.Copy(w, rc)
	if err != nil {
		return fmt.Errorf("failed to copy file content: %w", err)
	}

	return nil
}

// uploadToGooglePhotos uploads only JPG files to Google Photos
func (m *Manager) uploadToGooglePhotos(ctx context.Context, sourcePath, cardID string) error {
	// Load rclone config from custom path
	if err := config.SetConfigPath(m.configPath); err != nil {
		return fmt.Errorf("failed to set config path: %w", err)
	}
	configfile.Install()
	storage := &configfile.Storage{}
	if err := storage.Load(); err != nil {
		log.Printf("Warning: failed to load config file: %v", err)
	}

	// Create source filesystem
	srcFs, err := fs.NewFs(ctx, sourcePath)
	if err != nil {
		return fmt.Errorf("failed to create source filesystem: %w", err)
	}

	// Destination path for Google Photos: remote:/cardID/
	destPath := m.googlePhotosRemoteName + ":" + cardID

	// Create destination filesystem
	dstFs, err := fs.NewFs(ctx, destPath)
	if err != nil {
		return fmt.Errorf("failed to create Google Photos destination filesystem: %w", err)
	}

	// Set up config with fewer parallel transfers for Google Photos
	ci := fs.GetConfig(ctx)
	ci.Transfers = 1 // Google Photos API is rate-limited, use single transfer
	ci.Checkers = 2

	log.Printf("Uploading JPG files to Google Photos at %s", destPath)

	// List all files in source
	var jpgFiles []fs.Object
	err = operations.ListFn(ctx, srcFs, func(obj fs.Object) {
		name := strings.ToLower(obj.Remote())
		if strings.HasSuffix(name, ".jpg") || strings.HasSuffix(name, ".jpeg") {
			jpgFiles = append(jpgFiles, obj)
		}
	})
	if err != nil {
		return fmt.Errorf("failed to list source files: %w", err)
	}

	if len(jpgFiles) == 0 {
		log.Printf("No JPG files found to upload to Google Photos")
		return nil
	}

	log.Printf("Found %d JPG files to upload to Google Photos", len(jpgFiles))

	// Copy each JPG file individually
	for i, obj := range jpgFiles {
		log.Printf("Uploading JPG %d/%d to Google Photos: %s", i+1, len(jpgFiles), obj.Remote())

		_, err := operations.Copy(ctx, dstFs, nil, obj.Remote(), obj)
		if err != nil {
			log.Printf("Warning: failed to upload %s to Google Photos: %v", obj.Remote(), err)
			// Continue with other files
		}
	}

	return nil
}
