package state

import (
	"encoding/json"
	"fmt"
	"log"
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

// persistedFiles returns the list of files whose .tmp siblings should be
// considered by boot-time recovery. Each entry pairs the canonical path with
// a JSON-validator: if the orphaned .tmp does not parse into the expected
// shape it is discarded rather than promoted to the canonical path.
func persistedFiles() []persistedFile {
	return []persistedFile{
		{path: StateFile, validate: validateStateJSON},
		{path: HistoryFile, validate: validateHistoryJSON},
	}
}

type persistedFile struct {
	path     string
	validate func([]byte) error
}

func validateStateJSON(data []byte) error {
	var s CurrentState
	return json.Unmarshal(data, &s)
}

func validateHistoryJSON(data []byte) error {
	var h []SyncRecord
	return json.Unmarshal(data, &h)
}

// recoverOrphanedTempFiles inspects each persistence path for a leftover
// ".tmp" sibling from a power loss between write and rename. If the .tmp is
// newer than the main file AND parses cleanly, it is renamed into place to
// recover the most recent write. Otherwise it is removed. Every action is
// logged. Returns the first error encountered, but always attempts every
// path so a problem with one file does not block recovery of the others.
func recoverOrphanedTempFiles() error {
	var firstErr error
	for _, pf := range persistedFiles() {
		if err := recoverOrphanedTempFile(pf); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func recoverOrphanedTempFile(pf persistedFile) error {
	tmpPath := pf.path + ".tmp"
	tmpInfo, err := os.Stat(tmpPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		log.Printf("state recovery: stat %q failed: %v", tmpPath, err)
		return nil
	}

	// #nosec G304 -- tmpPath is derived from a controlled application path.
	data, readErr := os.ReadFile(tmpPath)
	parseErr := readErr
	if parseErr == nil {
		parseErr = pf.validate(data)
	}

	mainInfo, statErr := os.Stat(pf.path)
	mainExists := statErr == nil
	if statErr != nil && !os.IsNotExist(statErr) {
		log.Printf("state recovery: stat %q failed: %v", pf.path, statErr)
		return nil
	}

	tmpNewer := !mainExists || tmpInfo.ModTime().After(mainInfo.ModTime())

	if parseErr == nil && tmpNewer {
		if err := os.Rename(tmpPath, pf.path); err != nil {
			log.Printf("state recovery: failed to promote %q to %q: %v", tmpPath, pf.path, err)
			return err
		}
		log.Printf("state recovery: promoted orphaned %q to %q (newer write recovered after power loss)", tmpPath, pf.path)
		return nil
	}

	reason := "older than main file"
	if parseErr != nil {
		reason = fmt.Sprintf("invalid JSON: %v", parseErr)
	}
	if err := os.Remove(tmpPath); err != nil {
		log.Printf("state recovery: failed to remove orphaned %q (%s): %v", tmpPath, reason, err)
		return err
	}
	log.Printf("state recovery: removed orphaned %q (%s)", tmpPath, reason)
	return nil
}

// saveState persists current state to disk
// IMPORTANT: Caller must hold at least a read lock (RLock or Lock)
func (m *Manager) saveState() error {
	s := m.currentState
	if s.SchemaVersion == 0 {
		s.SchemaVersion = CurrentStateSchemaVersion
	}
	data, err := utils.MarshalJSONIndent(s)
	if err != nil {
		return err
	}

	return utils.AtomicWrite(StateFile, data, 0644)
}

// saveStateSnapshot persists a pre-captured CurrentState snapshot to disk.
// It does NOT read m.currentState, so callers must (and do) invoke it WITHOUT
// holding m.mu. Concurrent calls serialize through m.saveMu so the ordering of
// on-disk writes follows the order callers entered saveStateSnapshot, which in
// turn follows the in-memory commit order because each caller captured its
// snapshot under m.mu before contending for saveMu.
func (m *Manager) saveStateSnapshot(s CurrentState) error {
	if s.SchemaVersion == 0 {
		s.SchemaVersion = CurrentStateSchemaVersion
	}
	data, err := utils.MarshalJSONIndent(s)
	if err != nil {
		return err
	}

	m.saveMu.Lock()
	defer m.saveMu.Unlock()
	return utils.AtomicWrite(StateFile, data, 0644)
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

	return utils.AtomicWrite(HistoryFile, data, 0644)
}

// saveHistorySnapshot persists a pre-captured history slice to disk. Like
// saveStateSnapshot it does not touch m.history and serializes through
// m.saveMu so concurrent writers cannot interleave their on-disk writes.
func (m *Manager) saveHistorySnapshot(h []SyncRecord) error {
	data, err := utils.MarshalJSONIndent(h)
	if err != nil {
		return err
	}

	m.saveMu.Lock()
	defer m.saveMu.Unlock()
	return utils.AtomicWrite(HistoryFile, data, 0644)
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
