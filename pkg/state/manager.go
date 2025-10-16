package state

import (
	"fmt"
	"log"
	"sync"
	"time"
)

// Manager manages persistent state for the photo sync system
type Manager struct {
	mu                sync.RWMutex
	currentState      CurrentState
	history           []SyncRecord
	notifier          *notifier
	lastProgressSave  time.Time
	progressSaveDelay time.Duration // Throttle disk writes to reduce SD card wear
}

// NewManager creates a new state manager
func NewManager() (*Manager, error) {
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

// load reads state and history from disk
func (m *Manager) load() error {
	if err := m.loadState(); err != nil {
		return err
	}
	return m.loadHistory()
}

// save persists current state to disk (caller must hold lock)
func (m *Manager) save() error {
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
	if err := m.save(); err != nil {
		return fmt.Errorf("failed to save cleaned state: %w", err)
	}
	if err := m.saveHistory(); err != nil {
		return fmt.Errorf("failed to save history: %w", err)
	}

	return nil
}

// GetState returns the current state
func (m *Manager) GetState() CurrentState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.currentState
}

// SetStatus updates the current status
func (m *Manager) SetStatus(status SyncStatus) error {
	log.Printf("SetStatus: Attempting to set status to %s", status)
	m.mu.Lock()
	log.Printf("SetStatus: Lock acquired, setting status to %s", status)

	m.currentState.Status = status

	log.Printf("SetStatus: Calling save()")
	if err := m.save(); err != nil {
		log.Printf("SetStatus: Save failed: %v", err)
		m.mu.Unlock()
		return err
	}
	log.Printf("SetStatus: Save completed")

	log.Printf("SetStatus: Calling notifyListeners()")
	stateCopy := m.currentState
	m.mu.Unlock()

	m.notifyListenersAsync(stateCopy)
	log.Printf("SetStatus: SetStatus completed successfully")
	return nil
}

// SetError sets the error status with a message
func (m *Manager) SetError(errorMsg string) error {
	m.mu.Lock()
	m.currentState.Status = StatusError
	if m.currentState.CurrentSync != nil {
		m.currentState.CurrentSync.Error = errorMsg
		m.currentState.CurrentSync.Status = "error"
	}

	if err := m.save(); err != nil {
		m.mu.Unlock()
		return err
	}

	stateCopy := m.currentState
	m.mu.Unlock()

	m.notifyListenersAsync(stateCopy)
	return nil
}

// SetSDCard updates SD card mount status
func (m *Manager) SetSDCard(mounted bool, path string) error {
	m.mu.Lock()
	m.currentState.SDCardMounted = mounted
	m.currentState.SDCardPath = path

	if err := m.save(); err != nil {
		m.mu.Unlock()
		return err
	}

	stateCopy := m.currentState
	m.mu.Unlock()

	m.notifyListenersAsync(stateCopy)
	return nil
}

// SetAvailableDevices updates the list of available storage devices
func (m *Manager) SetAvailableDevices(devices []DeviceInfo) error {
	m.mu.Lock()
	m.currentState.AvailableDevices = devices

	if err := m.save(); err != nil {
		m.mu.Unlock()
		return err
	}

	stateCopy := m.currentState
	m.mu.Unlock()

	m.notifyListenersAsync(stateCopy)
	return nil
}

// SetNeedsDeviceSelect sets whether manual device selection is needed
func (m *Manager) SetNeedsDeviceSelect(needs bool) error {
	m.mu.Lock()
	m.currentState.NeedsDeviceSelect = needs

	if err := m.save(); err != nil {
		m.mu.Unlock()
		return err
	}

	stateCopy := m.currentState
	m.mu.Unlock()

	m.notifyListenersAsync(stateCopy)
	return nil
}

// StartSync begins a new sync operation
func (m *Manager) StartSync(cardID string, totalFiles, totalBytes int64) (*SyncRecord, error) {
	m.mu.Lock()

	// Check if sync is already in progress
	if m.currentState.CurrentSync != nil {
		m.mu.Unlock()
		return nil, fmt.Errorf("sync already in progress for card %s", m.currentState.CurrentSync.CardID)
	}

	record := &SyncRecord{
		ID:         fmt.Sprintf("%d", time.Now().Unix()),
		StartTime:  time.Now(),
		Status:     "syncing",
		FilesTotal: totalFiles,
		BytesTotal: totalBytes,
		CardID:     cardID,
	}

	m.currentState.CurrentSync = record
	m.currentState.Status = StatusSyncing

	if err := m.save(); err != nil {
		m.mu.Unlock()
		return nil, err
	}

	stateCopy := m.currentState
	m.mu.Unlock()

	m.notifyListenersAsync(stateCopy)
	return record, nil
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

	// Throttle disk writes - only save every progressSaveDelay seconds
	shouldSave := time.Since(m.lastProgressSave) >= m.progressSaveDelay
	if shouldSave {
		m.lastProgressSave = time.Now()
		if err := m.save(); err != nil {
			m.mu.Unlock()
			return err
		}
	}

	stateCopy := m.currentState
	m.mu.Unlock()

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
		m.currentState.Status = StatusSuccess
	} else {
		m.currentState.CurrentSync.Status = "error"
		m.currentState.Status = StatusError
		if err != nil {
			m.currentState.CurrentSync.Error = err.Error()
		}
	}

	// Add to history
	m.history = append(m.history, *m.currentState.CurrentSync)
	m.currentState.LastSync = m.currentState.CurrentSync
	m.currentState.CurrentSync = nil

	// Save state and history
	if saveErr := m.save(); saveErr != nil {
		m.mu.Unlock()
		return saveErr
	}
	if saveErr := m.saveHistory(); saveErr != nil {
		m.mu.Unlock()
		return saveErr
	}

	stateCopy := m.currentState
	m.mu.Unlock()

	m.notifyListenersAsync(stateCopy)
	return nil
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
