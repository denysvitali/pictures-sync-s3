package state

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/denysvitali/pictures-sync-s3/pkg/utils"
)

// File paths for persistent storage
var (
	stateDir    = getPermDir()
	PermDir     = stateDir
	MountDir    = filepath.Join(PermDir, "mounts/sdcard")
	ConfigFile  = filepath.Join(PermDir, "rclone.conf")
	HistoryFile = filepath.Join(PermDir, "sync-history.json")
	StateFile   = filepath.Join(PermDir, "state.json")
)

// GetStateDir returns the current state directory (for testing)
func GetStateDir() string {
	return stateDir
}

// SetStateDir sets the state directory and updates all file paths (for testing)
func SetStateDir(dir string) {
	stateDir = dir
	PermDir = dir
	MountDir = filepath.Join(PermDir, "mounts/sdcard")
	ConfigFile = filepath.Join(PermDir, "rclone.conf")
	HistoryFile = filepath.Join(PermDir, "sync-history.json")
	StateFile = filepath.Join(PermDir, "state.json")
}

// getPermDir returns the appropriate permanent directory path
// Uses /perm for production (Gokrazy), falls back to /tmp for development
func getPermDir() string {
	if _, err := os.Stat("/perm"); os.IsNotExist(err) {
		return "/tmp/pictures-sync"
	}
	return "/perm/pictures-sync"
}

// ensureDirectories creates necessary directories if they don't exist
func ensureDirectories() error {
	if err := os.MkdirAll(PermDir, 0755); err != nil {
		return fmt.Errorf("failed to create perm directory: %w", err)
	}
	if err := os.MkdirAll(MountDir, 0755); err != nil {
		return fmt.Errorf("failed to create mount directory: %w", err)
	}
	return nil
}

// atomicWrite performs an atomic file write by writing to a temp file
// and then renaming it to the target path
// Uses the utils package for consistent atomic writes across the codebase
func atomicWrite(path string, data []byte, perm os.FileMode) error {
	return utils.AtomicWrite(path, data, perm)
}

// saveState persists current state to disk
// IMPORTANT: Caller must hold at least a read lock (RLock or Lock)
func (m *Manager) saveState() error {
	data, err := utils.MarshalJSONIndent(m.currentState)
	if err != nil {
		return err
	}

	return atomicWrite(StateFile, data, 0644)
}

// loadState reads state from disk
func (m *Manager) loadState() error {
	return utils.LoadJSON(StateFile, &m.currentState, nil)
}

// saveHistory persists sync history to disk
// IMPORTANT: Caller must hold at least a read lock (RLock or Lock)
func (m *Manager) saveHistory() error {
	data, err := utils.MarshalJSONIndent(m.history)
	if err != nil {
		return err
	}

	return atomicWrite(HistoryFile, data, 0644)
}

// loadHistory reads sync history from disk
func (m *Manager) loadHistory() error {
	// Initialize empty history slice
	m.history = make([]SyncRecord, 0)

	// Load history from file (if it exists)
	return utils.LoadJSON(HistoryFile, &m.history, m.history)
}

// EnsureRcloneConfig checks if rclone config exists
func EnsureRcloneConfig() (bool, error) {
	_, err := os.Stat(ConfigFile)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// GetRcloneConfigPath returns the path to rclone config
func GetRcloneConfigPath() string {
	return ConfigFile
}

// loadStateFile reads the state file and returns the raw data
func loadStateFile() ([]byte, error) {
	data, err := os.ReadFile(StateFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // No state file yet
		}
		return nil, fmt.Errorf("failed to read state file: %w", err)
	}
	return data, nil
}

// unmarshalState unmarshals JSON data into a CurrentState
func unmarshalState(data []byte, state *CurrentState) error {
	return utils.UnmarshalJSON(data, state)
}

// marshalState marshals CurrentState to JSON (for testing)
func marshalState(state *CurrentState) ([]byte, error) {
	return utils.MarshalJSONIndent(state)
}
