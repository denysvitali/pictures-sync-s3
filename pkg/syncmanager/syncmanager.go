package syncmanager

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/denysvitali/pictures-sync-s3/pkg/state"
)

// Manager manages rclone sync operations
type Manager struct {
	configPath    string
	remoteName    string
	remotePath    string
	stateMgr      *state.Manager
	currentCmd    *exec.Cmd
	mu            sync.Mutex
	progressChans []chan Progress
}

// Progress represents sync progress
type Progress struct {
	BytesTransferred int64   `json:"bytes"`
	Percentage       int     `json:"percentage"`
	TransferredFiles int     `json:"transferred_files"`
	TotalFiles       int     `json:"total_files"`
	Speed            float64 `json:"speed"` // bytes per second
	ETA              int     `json:"eta"`   // seconds
}

// RcloneLog represents a log line from rclone JSON output
type RcloneLog struct {
	Level   string                 `json:"level"`
	Msg     string                 `json:"msg"`
	Source  string                 `json:"source"`
	Stats   *RcloneStats           `json:"stats,omitempty"`
	Object  string                 `json:"object,omitempty"`
	Size    int64                  `json:"size,omitempty"`
	Fs      map[string]interface{} `json:"fs,omitempty"`
	Time    string                 `json:"time"`
}

// RcloneStats represents rclone stats
type RcloneStats struct {
	Bytes             int64   `json:"bytes"`
	Checks            int     `json:"checks"`
	DeletedDirs       int     `json:"deletedDirs"`
	Deletes           int     `json:"deletes"`
	ElapsedTime       float64 `json:"elapsedTime"`
	Errors            int     `json:"errors"`
	ETA               int     `json:"eta"`
	FatalError        bool    `json:"fatalError"`
	Renames           int     `json:"renames"`
	RetryError        bool    `json:"retryError"`
	Speed             float64 `json:"speed"`
	TotalBytes        int64   `json:"totalBytes"`
	TotalChecks       int     `json:"totalChecks"`
	TotalTransfers    int     `json:"totalTransfers"`
	TransferTime      float64 `json:"transferTime"`
	Transfers         int     `json:"transfers"`
}

// NewManager creates a new sync manager
func NewManager(configPath, remoteName, remotePath string, stateMgr *state.Manager) *Manager {
	return &Manager{
		configPath:    configPath,
		remoteName:    remoteName,
		remotePath:    remotePath,
		stateMgr:      stateMgr,
		progressChans: make([]chan Progress, 0),
	}
}

// Sync synchronizes files from source to remote
func (m *Manager) Sync(sourcePath, cardID string, totalFiles int, totalBytes int64) error {
	m.mu.Lock()
	if m.currentCmd != nil {
		m.mu.Unlock()
		return fmt.Errorf("sync already in progress")
	}
	m.mu.Unlock()

	// Construct rclone command with card-specific folder
	// Result: remote:/photos/{cardID}/DCIM/
	destPath := filepath.Join(m.remoteName+":"+m.remotePath, cardID, "DCIM")

	args := []string{
		"sync",
		sourcePath,
		destPath,
		"--config", m.configPath,
		"--progress",
		"--stats", "2s",
		"--stats-one-line",
		"--use-json-log",
		"--log-level", "INFO",
		"-v",
	}

	log.Printf("Running rclone: rclone %s", strings.Join(args, " "))
	log.Printf("Syncing to card-specific folder: %s", destPath)

	cmd := exec.Command("rclone", args...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start rclone: %w", err)
	}

	m.mu.Lock()
	m.currentCmd = cmd
	m.mu.Unlock()

	// Process stdout (JSON logs)
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			m.processLogLine(line)
		}
	}()

	// Process stderr (regular logs)
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			line := scanner.Text()
			log.Printf("rclone: %s", line)
		}
	}()

	// Wait for completion
	err = cmd.Wait()

	m.mu.Lock()
	m.currentCmd = nil
	m.mu.Unlock()

	if err != nil {
		return fmt.Errorf("rclone failed: %w", err)
	}

	return nil
}

// Cancel cancels the current sync operation
func (m *Manager) Cancel() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.currentCmd == nil {
		return fmt.Errorf("no sync in progress")
	}

	if m.currentCmd.Process != nil {
		return m.currentCmd.Process.Kill()
	}

	return nil
}

// IsRunning returns true if a sync is currently running
func (m *Manager) IsRunning() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.currentCmd != nil
}

// SubscribeProgress returns a channel that receives progress updates
func (m *Manager) SubscribeProgress() chan Progress {
	m.mu.Lock()
	defer m.mu.Unlock()

	ch := make(chan Progress, 10)
	m.progressChans = append(m.progressChans, ch)
	return ch
}

// processLogLine processes a JSON log line from rclone
func (m *Manager) processLogLine(line string) {
	var logEntry RcloneLog
	if err := json.Unmarshal([]byte(line), &logEntry); err != nil {
		// Not JSON, just log it
		log.Printf("rclone: %s", line)
		return
	}

	// Log the message
	if logEntry.Msg != "" {
		log.Printf("rclone [%s]: %s", logEntry.Level, logEntry.Msg)
	}

	// Process stats
	if logEntry.Stats != nil {
		m.updateProgress(logEntry.Stats)
	}
}

// updateProgress updates progress from rclone stats
func (m *Manager) updateProgress(stats *RcloneStats) {
	// Update state manager
	m.stateMgr.UpdateSyncProgress(int64(stats.Transfers), stats.Bytes)

	// Calculate percentage
	var percentage int
	if stats.TotalBytes > 0 {
		percentage = int((float64(stats.Bytes) / float64(stats.TotalBytes)) * 100)
	}

	// Create progress update
	progress := Progress{
		BytesTransferred: stats.Bytes,
		Percentage:       percentage,
		TransferredFiles: stats.Transfers,
		TotalFiles:       stats.TotalTransfers,
		Speed:            stats.Speed,
		ETA:              stats.ETA,
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
}

// TestConnection tests the rclone configuration
func (m *Manager) TestConnection() error {
	cmd := exec.Command("rclone", "lsd", m.remoteName+":", "--config", m.configPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("connection test failed: %w\nOutput: %s", err, string(output))
	}
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
	cmd := exec.Command("rclone", "listremotes", "--config", m.configPath)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to list remotes: %w", err)
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	remotes := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			// Remove trailing colon
			remotes = append(remotes, strings.TrimSuffix(line, ":"))
		}
	}

	return remotes, nil
}
