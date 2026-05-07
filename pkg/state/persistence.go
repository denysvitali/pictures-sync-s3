package state

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/denysvitali/pictures-sync-s3/pkg/utils"
)

// File paths for persistent and runtime storage.
// StateFile is volatile runtime state shared by the daemon and web UI; keep it
// off persistent flash because sync progress is updated frequently.
var (
	stateDir        = getPermDir()
	runtimeStateDir = getRuntimeStateDir()
	PermDir         = stateDir
	MountDir        = filepath.Join(PermDir, "mounts/sdcard")
	ConfigFile      = filepath.Join(PermDir, "rclone.conf")
	HistoryFile     = filepath.Join(PermDir, "sync-history.json")
	StateFile       = filepath.Join(runtimeStateDir, "state.json")
)

// configurePaths is the single source of truth for derived paths. Both the
// public test hook (SetStateDir) and the env-var-driven init path go through
// here so we don't drift between them.
func configurePaths(perm, runtime string) {
	stateDir = perm
	runtimeStateDir = runtime
	PermDir = perm
	MountDir = filepath.Join(PermDir, "mounts/sdcard")
	ConfigFile = filepath.Join(PermDir, "rclone.conf")
	HistoryFile = filepath.Join(PermDir, "sync-history.json")
	StateFile = filepath.Join(runtimeStateDir, "state.json")
}

// GetStateDir returns the current persistent state directory (for testing)
func GetStateDir() string {
	return stateDir
}

// SetStateDir sets persistent and runtime state directories to the same path
// for tests.
func SetStateDir(dir string) {
	configurePaths(dir, dir)
}

// getPermDir returns the appropriate permanent directory path
// Uses /perm for production (Gokrazy), falls back to /tmp for development
func getPermDir() string {
	if baseDir := os.Getenv("PERM_DIR"); baseDir != "" {
		return filepath.Join(baseDir, "pictures-sync")
	}
	if _, err := os.Stat("/perm"); os.IsNotExist(err) {
		return "/tmp/pictures-sync"
	}
	return "/perm/pictures-sync"
}

func getRuntimeStateDir() string {
	if baseDir := os.Getenv("PICTURES_SYNC_STATE_DIR"); baseDir != "" {
		return filepath.Join(baseDir, "pictures-sync")
	}
	return filepath.Join(os.TempDir(), "pictures-sync")
}

// ensureDirectories creates necessary directories if they don't exist
func ensureDirectories() error {
	if err := os.MkdirAll(PermDir, 0750); err != nil {
		return fmt.Errorf("failed to create perm directory: %w", err)
	}
	if err := os.MkdirAll(runtimeStateDir, 0750); err != nil {
		return fmt.Errorf("failed to create runtime state directory: %w", err)
	}
	// #nosec G301 -- SD card mount point must be accessible by web UI process
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
	// #nosec G304 -- StateFile is a controlled application path under runtime state.
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
