package state

import (
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// recordIDCounter disambiguates SyncRecord IDs created within the same
// nanosecond (e.g. tests starting many syncs concurrently).
var recordIDCounter uint64

// Manager manages persistent state for the photo sync system.
//
// Locking discipline:
//   - mu guards in-memory state (currentState, history, lastProgressSave).
//   - saveMu serializes disk persistence. It is ALWAYS acquired AFTER releasing
//     mu, never while mu is held, so the fsync inside atomicWrite cannot stall
//     readers/writers contending for mu. Concurrent mutators first commit to
//     memory under mu, then queue at saveMu in commit order, so on-disk writes
//     match the order of in-memory commits.
type Manager struct {
	mu                sync.RWMutex
	saveMu            sync.Mutex
	currentState      CurrentState
	history           []SyncRecord
	notifier          *notifier
	lastProgressSave  time.Time
	progressSaveDelay time.Duration // Throttle disk writes to reduce SD card wear
}

// NewManager creates a new state manager
func NewManager() (*Manager, error) {
	configurePathsFromEnv()

	m := &Manager{
		notifier:          newNotifier(),
		progressSaveDelay: 5 * time.Second, // Only save progress every 5 seconds to reduce SD wear
	}

	// Ensure directories exist
	if err := ensureDirectories(); err != nil {
		return nil, err
	}

	// Load existing state
	if err := m.load(); err != nil {
		// If loading fails, start with empty state
		m.currentState = CurrentState{Status: StatusIdle}
	}

	// Clear any stale in-progress sync from previous crash/restart
	if err := m.clearStaleSync(); err != nil {
		log.Printf("Warning: Failed to clear stale sync: %v", err)
	}

	return m, nil
}

// load reads state and history from disk. Before reading, any orphaned .tmp
// siblings from a power loss between AtomicWrite's write and rename steps are
// either promoted (if newer + valid JSON) or removed, so the on-disk view is
// consistent before the rest of startup runs.
func (m *Manager) load() error {
	if err := recoverOrphanedTempFiles(); err != nil {
		log.Printf("state recovery: completed with errors: %v", err)
	}
	if err := m.loadState(); err != nil {
		return err
	}
	return m.loadHistory()
}

// save persists current state to disk. It acquires an RLock to safely read
// currentState, so callers MUST NOT already hold m.mu (read or write).
// Internal callers that already hold the write lock should use saveLocked.
func (m *Manager) save() error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.saveState()
}

// saveLocked persists current state to disk. Caller MUST hold m.mu (read or
// write lock).
func (m *Manager) saveLocked() error {
	return m.saveState()
}

// clearStaleSync removes any in-progress sync from a previous crash/restart
func (m *Manager) clearStaleSync() error {
	if m.currentState.CurrentSync == nil {
		return nil
	}

	log.Printf("Clearing stale sync record from previous run: %s", m.currentState.CurrentSync.CardID)

	// Move it to history as failed
	m.currentState.CurrentSync.EndTime = time.Now()
	m.currentState.CurrentSync.Status = "error"
	m.currentState.CurrentSync.Error = "Service restarted during sync"
	m.history = append(m.history, *m.currentState.CurrentSync)
	m.currentState.LastSync = m.currentState.CurrentSync
	m.currentState.CurrentSync = nil
	m.currentState.Status = StatusIdle

	// Save cleaned state
	if err := m.saveLocked(); err != nil {
		return fmt.Errorf("failed to save cleaned state: %w", err)
	}
	if err := m.saveHistory(); err != nil {
		return fmt.Errorf("failed to save history: %w", err)
	}

	return nil
}

// cloneState returns a deep copy of CurrentState so callers cannot observe
// or accidentally mutate shared SyncRecord pointers / device slices that the
// manager continues to update under its lock.
func cloneState(s CurrentState) CurrentState {
	out := s
	if s.CurrentSync != nil {
		rec := *s.CurrentSync
		out.CurrentSync = &rec
	}
	if s.LastSync != nil {
		rec := *s.LastSync
		out.LastSync = &rec
	}
	if s.AvailableDevices != nil {
		devs := make([]DeviceInfo, len(s.AvailableDevices))
		for i, d := range s.AvailableDevices {
			cp := d
			if d.Partitions != nil {
				cp.Partitions = make([]PartitionInfo, len(d.Partitions))
				copy(cp.Partitions, d.Partitions)
			}
			devs[i] = cp
		}
		out.AvailableDevices = devs
	}
	return out
}

// GetState returns a deep copy of the current state
func (m *Manager) GetState() CurrentState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return cloneState(m.currentState)
}

// mutate runs fn under the write lock, snapshots the resulting state, releases
// the lock, then persists and broadcasts. Persistence happens OUTSIDE m.mu so
// the fsync in atomicWrite cannot stall concurrent readers. saveStateSnapshot
// serializes through m.saveMu so writers reach disk in commit order. A failing
// disk write is logged but not returned to the caller: the in-memory mutation
// has already succeeded, so the API contract is preserved.
func (m *Manager) mutate(fn func(*CurrentState)) error {
	m.mu.Lock()
	fn(&m.currentState)
	stateCopy := cloneState(m.currentState)
	m.mu.Unlock()

	if err := m.saveStateSnapshot(stateCopy); err != nil {
		log.Printf("state: failed to persist state after mutation: %v", err)
	}
	m.notifyListenersAsync(stateCopy)
	return nil
}

// SetStatus updates the current status
func (m *Manager) SetStatus(status SyncStatus) error {
	return m.mutate(func(s *CurrentState) {
		s.Status = status
		if status != StatusError {
			s.Error = ""
		}
	})
}

// SetError sets the error status with a message
func (m *Manager) SetError(errorMsg string) error {
	return m.mutate(func(s *CurrentState) {
		s.Status = StatusError
		s.Error = errorMsg
		if s.CurrentSync != nil {
			s.CurrentSync.Error = errorMsg
			s.CurrentSync.Status = "error"
		}
	})
}

// SetSDCard updates SD card mount status
func (m *Manager) SetSDCard(mounted bool, path string) error {
	return m.mutate(func(s *CurrentState) {
		s.SDCardMounted = mounted
		s.SDCardPath = path
		if !mounted {
			s.SDCardDevicePath = ""
			s.SDCardPhotoCount = 0
			s.SDCardPhotoBytes = 0
		}
	})
}

// SetSDCardDevice records the currently mounted SD-card device path.
func (m *Manager) SetSDCardDevice(devicePath string) error {
	return m.mutate(func(s *CurrentState) {
		s.SDCardDevicePath = devicePath
	})
}

// SetSDCardPhotoSummary records the photo count found on the currently mounted card.
func (m *Manager) SetSDCardPhotoSummary(count, bytes int64) error {
	return m.mutate(func(s *CurrentState) {
		s.SDCardPhotoCount = count
		s.SDCardPhotoBytes = bytes
	})
}

// SetAvailableDevices updates the list of available storage devices
func (m *Manager) SetAvailableDevices(devices []DeviceInfo) error {
	return m.mutate(func(s *CurrentState) {
		s.AvailableDevices = devices
	})
}

// SetNeedsDeviceSelect sets whether manual device selection is needed
func (m *Manager) SetNeedsDeviceSelect(needs bool) error {
	return m.mutate(func(s *CurrentState) {
		s.NeedsDeviceSelect = needs
	})
}

// newRecordID returns a collision-resistant identifier for a SyncRecord.
func newRecordID() string {
	return fmt.Sprintf("%d-%d", time.Now().UnixNano(), atomic.AddUint64(&recordIDCounter, 1))
}

// StartSync begins a new sync operation
func (m *Manager) StartSync(cardID string, totalFiles, totalBytes int64) (*SyncRecord, error) {
	m.mu.Lock()

	// Check if sync is already in progress
	if m.currentState.CurrentSync != nil {
		cardID := m.currentState.CurrentSync.CardID
		m.mu.Unlock()
		return nil, fmt.Errorf("sync already in progress for card %s", cardID)
	}

	record := &SyncRecord{
		ID:            newRecordID(),
		StartTime:     time.Now(),
		Status:        "syncing",
		ProgressPhase: "preparing",
		FilesTotal:    totalFiles,
		BytesTotal:    totalBytes,
		CardID:        cardID,
	}

	m.currentState.CurrentSync = record
	m.currentState.Status = StatusSyncing
	m.currentState.Error = ""

	stateCopy := cloneState(m.currentState)
	// Return a copy of the record so callers cannot mutate the record the
	// manager continues to update under its lock.
	recCopy := *record
	m.mu.Unlock()

	if err := m.saveStateSnapshot(stateCopy); err != nil {
		log.Printf("state: failed to persist state after StartSync: %v", err)
	}
	m.notifyListenersAsync(stateCopy)
	return &recCopy, nil
}

// UpdateSyncProgress updates the progress of current sync
// Saves to disk only periodically (throttled) to reduce SD card wear
func (m *Manager) UpdateSyncProgress(filesSynced, bytesSynced int64, currentFile string, currentFileSize int64, transferSpeed float64, eta string) error {
	m.mu.Lock()

	// Return error if no sync is in progress (fail-fast instead of silent failure)
	if m.currentState.CurrentSync == nil {
		m.mu.Unlock()
		return fmt.Errorf("no sync in progress")
	}

	// Update all fields while holding lock to prevent race conditions
	m.currentState.CurrentSync.FilesSynced = filesSynced
	m.currentState.CurrentSync.BytesSynced = bytesSynced
	m.currentState.CurrentSync.CurrentFile = currentFile
	m.currentState.CurrentSync.CurrentFileSize = currentFileSize
	m.currentState.CurrentSync.TransferSpeed = transferSpeed
	m.currentState.CurrentSync.ETA = eta
	m.currentState.CurrentSync.ProgressPhase = progressPhase(currentFile, transferSpeed)

	// Throttle disk writes - only save every progressSaveDelay seconds. We
	// decide whether to save and update lastProgressSave while holding m.mu
	// (so concurrent updaters see a consistent throttle window), but the
	// actual atomicWrite/fsync runs after the lock is released to keep
	// readers off the disk-write critical path.
	shouldSave := time.Since(m.lastProgressSave) >= m.progressSaveDelay
	if shouldSave {
		m.lastProgressSave = time.Now()
	}

	stateCopy := cloneState(m.currentState)
	m.mu.Unlock()

	if shouldSave {
		if err := m.saveStateSnapshot(stateCopy); err != nil {
			log.Printf("state: failed to persist sync progress: %v", err)
		}
	}
	m.notifyListenersAsync(stateCopy)
	return nil
}

// FinishSync completes the current sync operation
func (m *Manager) FinishSync(success bool, err error) error {
	m.mu.Lock()

	if m.currentState.CurrentSync == nil {
		m.mu.Unlock()
		return fmt.Errorf("no sync in progress")
	}

	m.currentState.CurrentSync.EndTime = time.Now()
	if success {
		m.currentState.CurrentSync.Status = "success"
		m.currentState.CurrentSync.ProgressPhase = "completed"
		m.currentState.CurrentSync.FilesSynced = m.currentState.CurrentSync.FilesTotal
		m.currentState.CurrentSync.BytesSynced = m.currentState.CurrentSync.BytesTotal
		m.currentState.CurrentSync.CurrentFile = ""
		m.currentState.CurrentSync.CurrentFileSize = 0
		m.currentState.CurrentSync.TransferSpeed = 0
		m.currentState.CurrentSync.ETA = ""
		m.currentState.Status = StatusSuccess
		m.currentState.Error = ""
	} else {
		m.currentState.CurrentSync.Status = "error"
		m.currentState.Status = StatusError
		if err != nil {
			m.currentState.CurrentSync.Error = err.Error()
			m.currentState.Error = err.Error()
		}
	}

	// Add to history
	m.history = append(m.history, *m.currentState.CurrentSync)
	m.currentState.LastSync = m.currentState.CurrentSync
	m.currentState.CurrentSync = nil

	// Snapshot both state and history under the lock, then release before
	// writing to disk. The snapshot of history is a fresh slice so subsequent
	// appends to m.history (e.g. from another FinishSync) cannot race with
	// the marshal step inside saveHistorySnapshot.
	stateCopy := cloneState(m.currentState)
	historyCopy := make([]SyncRecord, len(m.history))
	copy(historyCopy, m.history)
	m.mu.Unlock()

	if saveErr := m.saveStateSnapshot(stateCopy); saveErr != nil {
		log.Printf("state: failed to persist state after FinishSync: %v", saveErr)
	}
	if saveErr := m.saveHistorySnapshot(historyCopy); saveErr != nil {
		log.Printf("state: failed to persist history after FinishSync: %v", saveErr)
	}
	m.notifyListenersAsync(stateCopy)
	return nil
}

func progressPhase(currentFile string, transferSpeed float64) string {
	switch {
	case strings.HasPrefix(currentFile, "[Checking]"):
		return "checking"
	case transferSpeed > 0:
		return "uploading"
	default:
		return "preparing"
	}
}

// GetHistory returns the sync history
func (m *Manager) GetHistory() []SyncRecord {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Return a copy
	history := make([]SyncRecord, len(m.history))
	copy(history, m.history)
	return history
}

// FindLastSyncByCardID finds the most recent sync for a given card ID
func (m *Manager) FindLastSyncByCardID(cardID string) *SyncRecord {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Search history in reverse order (most recent first)
	for i := len(m.history) - 1; i >= 0; i-- {
		if m.history[i].CardID == cardID {
			// Return a copy
			record := m.history[i]
			return &record
		}
	}

	return nil
}

// Replace overwrites the in-memory current state with the provided snapshot
// and notifies subscribers. Unlike the other mutators it does NOT persist to
// disk — it's intended for processes (e.g. the web UI) that consume state
// pushed from another authoritative owner over IPC and need a way to feed
// the local cache.
func (m *Manager) Replace(snapshot CurrentState) {
	clone := cloneState(snapshot)
	m.mu.Lock()
	m.currentState = clone
	m.mu.Unlock()

	m.notifyListenersAsync(cloneState(clone))
}

// Reload reads the latest state from disk and notifies listeners
func (m *Manager) Reload() error {
	var newState CurrentState

	data, err := loadStateFile()
	if err != nil {
		return err
	}

	if data != nil {
		if err := unmarshalState(data, &newState); err != nil {
			return err
		}
	}

	// Update current state with lock
	m.mu.Lock()
	m.currentState = newState
	m.mu.Unlock()

	// Notify listeners synchronously after releasing the lock to avoid deadlock
	m.notifyListeners()

	return nil
}

// configurePathsFromEnv applies any PERM_DIR / PICTURES_SYNC_STATE_DIR
// overrides at manager construction time.
func configurePathsFromEnv() {
	perm := PermDir
	runtime := runtimeStateDir
	changed := false
	if os.Getenv("PERM_DIR") != "" {
		perm = getPermDir()
		changed = true
	}
	if os.Getenv("PICTURES_SYNC_STATE_DIR") != "" {
		runtime = getRuntimeStateDir()
		changed = true
	}
	if changed {
		configurePaths(perm, runtime)
	}
}
