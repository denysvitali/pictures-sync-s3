package state

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

func getPermDir() string {
	// Use local directory for development if /perm doesn't exist
	if _, err := os.Stat("/perm"); os.IsNotExist(err) {
		return "/tmp/pictures-sync"
	}
	return "/perm/pictures-sync"
}

var (
	PermDir    = getPermDir()
	MountDir   = filepath.Join(PermDir, "mounts/sdcard")
	ConfigFile = filepath.Join(PermDir, "rclone.conf")
	HistoryFile = filepath.Join(PermDir, "sync-history.json")
	StateFile  = filepath.Join(PermDir, "state.json")
)

// SyncStatus represents the current sync operation status
type SyncStatus string

const (
	StatusIdle     SyncStatus = "idle"
	StatusDetected SyncStatus = "detected"
	StatusSyncing  SyncStatus = "syncing"
	StatusSuccess  SyncStatus = "success"
	StatusError    SyncStatus = "error"
)

// SyncRecord represents a completed sync operation
type SyncRecord struct {
	ID              string    `json:"id"`
	StartTime       time.Time `json:"start_time"`
	EndTime         time.Time `json:"end_time"`
	Status          string    `json:"status"`
	FilesTotal      int64     `json:"files_total"`
	FilesSynced     int64     `json:"files_synced"`
	BytesTotal      int64     `json:"bytes_total"`
	BytesSynced     int64     `json:"bytes_synced"`
	Error           string    `json:"error,omitempty"`
	CardID          string    `json:"card_id"` // Unique ID from .pictures-sync-id file
	CurrentFile     string    `json:"current_file,omitempty"` // Current file being synced
	CurrentFileSize int64     `json:"current_file_size,omitempty"` // Size of current file
	TransferSpeed   float64   `json:"transfer_speed,omitempty"` // Bytes per second
	ETA             string    `json:"eta,omitempty"` // Estimated time remaining (formatted)
}

// DeviceInfo represents a detected storage device
type DeviceInfo struct {
	DevicePath  string `json:"device_path"`
	DeviceName  string `json:"device_name"`
	Size        int64  `json:"size"`
	SizeHuman   string `json:"size_human"`
	IsUSB       bool   `json:"is_usb"`
	IsMounted   bool   `json:"is_mounted"`
	MountPath   string `json:"mount_path,omitempty"`
	HasDCIM     bool   `json:"has_dcim"`
	VolumeLabel string `json:"volume_label,omitempty"`
}

// CurrentState represents the current system state
type CurrentState struct {
	Status            SyncStatus    `json:"status"`
	CurrentSync       *SyncRecord   `json:"current_sync,omitempty"`
	LastSync          *SyncRecord   `json:"last_sync,omitempty"`
	SDCardMounted     bool          `json:"sdcard_mounted"`
	SDCardPath        string        `json:"sdcard_path,omitempty"`
	AvailableDevices  []DeviceInfo  `json:"available_devices,omitempty"`
	NeedsDeviceSelect bool          `json:"needs_device_select"`
}

// Manager manages persistent state
type Manager struct {
	mu                sync.RWMutex
	currentState      CurrentState
	history           []SyncRecord
	listeners         []chan CurrentState
	lastProgressSave  time.Time
	progressSaveDelay time.Duration // Throttle disk writes to reduce SD card wear
}

// NewManager creates a new state manager
func NewManager() (*Manager, error) {
	m := &Manager{
		listeners:         make([]chan CurrentState, 0),
		progressSaveDelay: 5 * time.Second, // Only save progress every 5 seconds to reduce SD wear
	}

	// Ensure directories exist
	if err := os.MkdirAll(PermDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create perm directory: %w", err)
	}
	if err := os.MkdirAll(MountDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create mount directory: %w", err)
	}

	// Load existing state
	if err := m.load(); err != nil {
		// If loading fails, start with empty state
		m.currentState = CurrentState{Status: StatusIdle}
	}

	// Clear any stale in-progress sync from previous crash/restart
	if m.currentState.CurrentSync != nil {
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
			log.Printf("Warning: Failed to save cleaned state: %v", err)
		}
		if err := m.saveHistory(); err != nil {
			log.Printf("Warning: Failed to save history: %v", err)
		}
	}

	return m, nil
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
	// Create a copy of the current state to pass to notifyListeners
	stateCopy := m.currentState
	m.mu.Unlock()

	// Note: We call notifyListeners without holding the lock to avoid deadlock
	go m.notifyListenersAsync(stateCopy)
	log.Printf("SetStatus: SetStatus completed successfully")
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

	// Create a copy of the current state to pass to notifyListeners
	stateCopy := m.currentState
	m.mu.Unlock()

	// Note: We call notifyListenersAsync without holding the lock to avoid deadlock
	go m.notifyListenersAsync(stateCopy)
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

	// Create a copy of the current state to pass to notifyListeners
	stateCopy := m.currentState
	m.mu.Unlock()

	// Note: We call notifyListenersAsync without holding the lock to avoid deadlock
	go m.notifyListenersAsync(stateCopy)
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

	// Create a copy of the current state to pass to notifyListeners
	stateCopy := m.currentState
	m.mu.Unlock()

	// Note: We call notifyListenersAsync without holding the lock to avoid deadlock
	go m.notifyListenersAsync(stateCopy)
	return nil
}

// StartSync begins a new sync operation
func (m *Manager) StartSync(cardID string, totalFiles, totalBytes int64) (*SyncRecord, error) {
	// Check if sync is already in progress
	m.mu.Lock()
	if m.currentState.CurrentSync != nil {
		m.mu.Unlock()
		return nil, fmt.Errorf("sync already in progress for card %s", m.currentState.CurrentSync.CardID)
	}

	record := &SyncRecord{
		ID:          fmt.Sprintf("%d", time.Now().Unix()),
		StartTime:   time.Now(),
		Status:      "syncing",
		FilesTotal:  totalFiles,
		BytesTotal:  totalBytes,
		CardID:      cardID,
	}

	m.currentState.CurrentSync = record
	m.currentState.Status = StatusSyncing

	if err := m.save(); err != nil {
		m.mu.Unlock()
		return nil, err
	}

	// Create a copy of the current state to pass to notifyListeners
	stateCopy := m.currentState
	m.mu.Unlock()

	// Note: We call notifyListenersAsync without holding the lock to avoid deadlock
	go m.notifyListenersAsync(stateCopy)
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
		// Save to disk if throttle period has elapsed
		if err := m.save(); err != nil {
			m.mu.Unlock()
			return err
		}
	}

	// Create a copy of the current state to pass to notifyListeners
	stateCopy := m.currentState
	m.mu.Unlock()

	// Note: We call notifyListenersAsync without holding the lock to avoid deadlock
	go m.notifyListenersAsync(stateCopy)
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

	// Create a copy of the current state to pass to notifyListeners
	stateCopy := m.currentState
	m.mu.Unlock()

	// Note: We call notifyListenersAsync without holding the lock to avoid deadlock
	go m.notifyListenersAsync(stateCopy)
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

// Subscribe returns a channel that receives state updates
func (m *Manager) Subscribe() chan CurrentState {
	m.mu.Lock()
	defer m.mu.Unlock()

	ch := make(chan CurrentState, 10)
	m.listeners = append(m.listeners, ch)
	return ch
}

// Unsubscribe removes a channel from the listeners list and closes it
func (m *Manager) Unsubscribe(ch chan CurrentState) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Find and remove the channel
	for i, listener := range m.listeners {
		if listener == ch {
			// Remove from slice
			m.listeners = append(m.listeners[:i], m.listeners[i+1:]...)
			// Close the channel to signal the subscriber
			close(ch)
			break
		}
	}
}

// notifyListeners sends current state to all subscribers
func (m *Manager) notifyListeners() {
	m.mu.RLock()
	state := m.currentState
	m.mu.RUnlock()

	m.notifyListenersAsync(state)
}

// notifyListenersAsync sends a given state to all subscribers without acquiring locks
func (m *Manager) notifyListenersAsync(state CurrentState) {
	m.mu.RLock()
	// Deep copy the listeners slice to avoid race conditions
	listenersCopy := make([]chan CurrentState, len(m.listeners))
	copy(listenersCopy, m.listeners)
	m.mu.RUnlock()

	// Send to listeners without holding the lock
	for _, ch := range listenersCopy {
		// Use panic recovery to handle closed channels gracefully
		func(c chan CurrentState) {
			defer func() {
				if r := recover(); r != nil {
					// Channel was closed, log and continue
					log.Printf("Warning: Failed to notify listener (channel closed): %v", r)
				}
			}()
			select {
			case c <- state:
			default:
				// Skip if channel is full
			}
		}(ch)
	}
}

// save persists current state to disk
// IMPORTANT: Caller must hold at least a read lock (RLock or Lock)
func (m *Manager) save() error {
	// Marshal state (caller already holds lock)
	data, err := json.MarshalIndent(m.currentState, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal state: %w", err)
	}

	// Atomic write
	tmpFile := StateFile + ".tmp"
	if err := os.WriteFile(tmpFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write state file: %w", err)
	}
	if err := os.Rename(tmpFile, StateFile); err != nil {
		// Clean up temp file on rename failure
		os.Remove(tmpFile)
		return fmt.Errorf("failed to rename state file: %w", err)
	}

	return nil
}

// load reads state from disk
func (m *Manager) load() error {
	data, err := os.ReadFile(StateFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No state file yet
		}
		return fmt.Errorf("failed to read state file: %w", err)
	}

	if err := json.Unmarshal(data, &m.currentState); err != nil {
		return fmt.Errorf("failed to unmarshal state: %w", err)
	}

	// Load history
	return m.loadHistory()
}

// Reload reads the latest state from disk and notifies listeners
func (m *Manager) Reload() error {
	data, err := os.ReadFile(StateFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No state file yet
		}
		return fmt.Errorf("failed to read state file: %w", err)
	}

	var newState CurrentState
	if err := json.Unmarshal(data, &newState); err != nil {
		return fmt.Errorf("failed to unmarshal state: %w", err)
	}

	// Update current state with lock
	m.mu.Lock()
	m.currentState = newState
	m.mu.Unlock()

	// Notify listeners synchronously after releasing the lock to avoid deadlock
	m.notifyListeners()

	return nil
}

// saveHistory persists sync history to disk
// IMPORTANT: Caller must hold at least a read lock (RLock or Lock)
func (m *Manager) saveHistory() error {
	// Marshal history (caller already holds lock)
	data, err := json.MarshalIndent(m.history, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal history: %w", err)
	}

	tmpFile := HistoryFile + ".tmp"
	if err := os.WriteFile(tmpFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write history file: %w", err)
	}
	if err := os.Rename(tmpFile, HistoryFile); err != nil {
		// Clean up temp file on rename failure
		os.Remove(tmpFile)
		return fmt.Errorf("failed to rename history file: %w", err)
	}

	return nil
}

// loadHistory reads sync history from disk
func (m *Manager) loadHistory() error {
	data, err := os.ReadFile(HistoryFile)
	if err != nil {
		if os.IsNotExist(err) {
			m.history = make([]SyncRecord, 0)
			return nil
		}
		return fmt.Errorf("failed to read history file: %w", err)
	}

	if err := json.Unmarshal(data, &m.history); err != nil {
		return fmt.Errorf("failed to unmarshal history: %w", err)
	}

	return nil
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
