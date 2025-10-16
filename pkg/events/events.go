// Package events provides a publish-subscribe event system for system-wide notifications.
// It supports event distribution for SD card operations, sync progress, and system status changes.
// Events are delivered asynchronously to all subscribers via buffered channels.
package events

import (
	"sync"
	"time"
)

// EventType represents different types of events that can occur
type EventType string

const (
	// SD Card Events
	EventSDCardInserted EventType = "sd_card_inserted"
	EventSDCardRemoved  EventType = "sd_card_removed"
	EventSDCardMounted  EventType = "sd_card_mounted"

	// Detection Events
	EventDetectingPhotos EventType = "detecting_photos"
	EventPhotosDetected EventType = "photos_detected"
	EventNoPhotosFound  EventType = "no_photos_found"

	// Card ID Events
	EventCardIDFound    EventType = "card_id_found"
	EventCardIDCreated  EventType = "card_id_created"
	EventReformatDetected EventType = "reformat_detected"

	// Sync Events
	EventSyncStarting   EventType = "sync_starting"
	EventSyncStarted    EventType = "sync_started"
	EventSyncProgress   EventType = "sync_progress"
	EventSyncCompleted  EventType = "sync_completed"
	EventSyncFailed     EventType = "sync_failed"
	EventSyncCanceled   EventType = "sync_canceled"

	// System Events
	EventStatusChanged  EventType = "status_changed"
	EventError         EventType = "error"
	EventInfo          EventType = "info"
)

// Event represents a system event
type Event struct {
	Type      EventType              `json:"type"`
	Timestamp time.Time              `json:"timestamp"`
	Message   string                 `json:"message"`
	Data      map[string]interface{} `json:"data,omitempty"`
}

// Manager manages event distribution
type Manager struct {
	mu        sync.RWMutex
	listeners []chan Event
}

// NewManager creates a new event manager
func NewManager() *Manager {
	return &Manager{
		listeners: make([]chan Event, 0),
	}
}

// Subscribe returns a channel that receives all events
func (m *Manager) Subscribe() chan Event {
	m.mu.Lock()
	defer m.mu.Unlock()

	ch := make(chan Event, 100) // Buffer to prevent blocking
	m.listeners = append(m.listeners, ch)
	return ch
}

// Unsubscribe removes a channel from the listeners list and closes it
func (m *Manager) Unsubscribe(ch chan Event) {
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

// Emit sends an event to all subscribers
func (m *Manager) Emit(eventType EventType, message string, data map[string]interface{}) {
	event := Event{
		Type:      eventType,
		Timestamp: time.Now(),
		Message:   message,
		Data:      data,
	}

	m.mu.RLock()
	// Create a copy of listeners to avoid race conditions
	listenersCopy := make([]chan Event, len(m.listeners))
	copy(listenersCopy, m.listeners)
	m.mu.RUnlock()

	// Send to all listeners without holding the lock
	for _, ch := range listenersCopy {
		select {
		case ch <- event:
		default:
			// Skip if channel is full (prevents blocking)
		}
	}
}

// EmitSDCardInserted emits an SD card insertion event
func (m *Manager) EmitSDCardInserted(deviceName, mountPath string) {
	m.Emit(EventSDCardInserted, "SD card inserted", map[string]interface{}{
		"device_name": deviceName,
		"mount_path":  mountPath,
	})
}

// EmitSDCardRemoved emits an SD card removal event
func (m *Manager) EmitSDCardRemoved(deviceName string) {
	m.Emit(EventSDCardRemoved, "SD card removed", map[string]interface{}{
		"device_name": deviceName,
	})
}

// EmitPhotosDetected emits a photos detected event
func (m *Manager) EmitPhotosDetected(count int, totalBytes int64) {
	m.Emit(EventPhotosDetected, "Photos detected on SD card", map[string]interface{}{
		"photo_count":  count,
		"total_bytes":  totalBytes,
		"total_mb":     float64(totalBytes) / (1024 * 1024),
	})
}

// EmitCardIDFound emits a card ID found event
func (m *Manager) EmitCardIDFound(cardID string) {
	m.Emit(EventCardIDFound, "Found existing card ID", map[string]interface{}{
		"card_id": cardID,
	})
}

// EmitCardIDCreated emits a card ID creation event
func (m *Manager) EmitCardIDCreated(cardID string) {
	m.Emit(EventCardIDCreated, "Created new card ID", map[string]interface{}{
		"card_id": cardID,
	})
}

// EmitReformatDetected emits a reformat detection event
func (m *Manager) EmitReformatDetected(cardID, newCardID string, percentageOfLast float64) {
	m.Emit(EventReformatDetected, "Card reformat detected", map[string]interface{}{
		"old_card_id":       cardID,
		"new_card_id":       newCardID,
		"percentage_of_last": percentageOfLast,
	})
}

// EmitSyncStarted emits a sync started event
func (m *Manager) EmitSyncStarted(cardID string, totalFiles int, totalBytes int64) {
	m.Emit(EventSyncStarted, "Sync operation started", map[string]interface{}{
		"card_id":     cardID,
		"total_files": totalFiles,
		"total_bytes": totalBytes,
	})
}

// EmitSyncProgress emits a sync progress event
func (m *Manager) EmitSyncProgress(filesSynced, bytesSynced int64, currentFile string, transferSpeed float64, eta string) {
	m.Emit(EventSyncProgress, "Sync progress update", map[string]interface{}{
		"files_synced":   filesSynced,
		"bytes_synced":   bytesSynced,
		"current_file":   currentFile,
		"transfer_speed": transferSpeed,
		"eta":           eta,
	})
}

// EmitSyncCompleted emits a sync completion event
func (m *Manager) EmitSyncCompleted(cardID string, totalFiles int64, totalBytes int64, duration time.Duration) {
	m.Emit(EventSyncCompleted, "Sync completed successfully", map[string]interface{}{
		"card_id":     cardID,
		"total_files": totalFiles,
		"total_bytes": totalBytes,
		"duration":    duration.String(),
	})
}

// EmitSyncFailed emits a sync failure event
func (m *Manager) EmitSyncFailed(cardID string, err error) {
	m.Emit(EventSyncFailed, "Sync operation failed", map[string]interface{}{
		"card_id": cardID,
		"error":   err.Error(),
	})
}

// EmitStatusChanged emits a status change event
func (m *Manager) EmitStatusChanged(oldStatus, newStatus string) {
	m.Emit(EventStatusChanged, "System status changed", map[string]interface{}{
		"old_status": oldStatus,
		"new_status": newStatus,
	})
}

// EmitError emits an error event
func (m *Manager) EmitError(message string, err error) {
	data := map[string]interface{}{}
	if err != nil {
		data["error"] = err.Error()
	}
	m.Emit(EventError, message, data)
}

// EmitInfo emits an info event
func (m *Manager) EmitInfo(message string, data map[string]interface{}) {
	m.Emit(EventInfo, message, data)
}