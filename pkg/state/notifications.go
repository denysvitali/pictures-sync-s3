package state

import (
	"log"
	"sync"
)

// notifier manages the subscriber/listener pattern for state updates
type notifier struct {
	mu        sync.RWMutex
	listeners []chan CurrentState
}

// newNotifier creates a new notifier
func newNotifier() *notifier {
	return &notifier{
		listeners: make([]chan CurrentState, 0),
	}
}

// subscribe adds a new listener and returns a channel for state updates
func (n *notifier) subscribe() chan CurrentState {
	n.mu.Lock()
	defer n.mu.Unlock()

	ch := make(chan CurrentState, 10)
	n.listeners = append(n.listeners, ch)
	return ch
}

// unsubscribe removes a listener channel and closes it
func (n *notifier) unsubscribe(ch chan CurrentState) {
	n.mu.Lock()
	defer n.mu.Unlock()

	// Find and remove the channel
	for i, listener := range n.listeners {
		if listener == ch {
			// Remove from slice
			n.listeners = append(n.listeners[:i], n.listeners[i+1:]...)
			// Close the channel to signal the subscriber
			close(ch)
			break
		}
	}
}

// notify sends a state update to all subscribers
func (n *notifier) notify(state CurrentState) {
	n.mu.RLock()
	// Deep copy the listeners slice to avoid race conditions
	listenersCopy := make([]chan CurrentState, len(n.listeners))
	copy(listenersCopy, n.listeners)
	n.mu.RUnlock()

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

// Subscribe returns a channel that receives state updates
func (m *Manager) Subscribe() chan CurrentState {
	return m.notifier.subscribe()
}

// Unsubscribe removes a channel from the listeners list and closes it
func (m *Manager) Unsubscribe(ch chan CurrentState) {
	m.notifier.unsubscribe(ch)
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
	go m.notifier.notify(state)
}

// getListenerCount returns the number of active listeners (for testing)
func (n *notifier) getListenerCount() int {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return len(n.listeners)
}

// getListeners returns a copy of the listeners slice (for testing)
func (n *notifier) getListeners() []chan CurrentState {
	n.mu.RLock()
	defer n.mu.RUnlock()
	listeners := make([]chan CurrentState, len(n.listeners))
	copy(listeners, n.listeners)
	return listeners
}

// addListener adds a listener directly (for testing)
func (n *notifier) addListener(ch chan CurrentState) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.listeners = append(n.listeners, ch)
}
