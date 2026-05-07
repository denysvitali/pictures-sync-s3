package state

import (
	"sync"
)

// subscriber wraps a listener channel with coordination to safely close it
// without racing with concurrent senders. The channel is only ever closed by
// the unsubscribe path, while senders coordinate through s.mu and s.closed.
type subscriber struct {
	mu     sync.Mutex
	ch     chan CurrentState
	closed bool
}

func (s *subscriber) send(state CurrentState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	select {
	case s.ch <- state:
	default:
		// Skip if channel is full
	}
}

func (s *subscriber) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.closed = true
	close(s.ch)
}

// notifier manages the subscriber/listener pattern for state updates
type notifier struct {
	mu        sync.RWMutex
	listeners []*subscriber
}

// newNotifier creates a new notifier
func newNotifier() *notifier {
	return &notifier{
		listeners: make([]*subscriber, 0),
	}
}

// subscribe adds a new listener and returns a channel for state updates
func (n *notifier) subscribe() chan CurrentState {
	n.mu.Lock()
	defer n.mu.Unlock()

	sub := &subscriber{ch: make(chan CurrentState, 10)}
	n.listeners = append(n.listeners, sub)
	return sub.ch
}

// unsubscribe removes a listener channel and closes it
func (n *notifier) unsubscribe(ch chan CurrentState) {
	n.mu.Lock()
	defer n.mu.Unlock()

	for i, sub := range n.listeners {
		if sub.ch == ch {
			n.listeners = append(n.listeners[:i], n.listeners[i+1:]...)
			sub.close()
			break
		}
	}
}

// notify sends a state update to all subscribers
func (n *notifier) notify(state CurrentState) {
	n.mu.RLock()
	subs := make([]*subscriber, len(n.listeners))
	copy(subs, n.listeners)
	n.mu.RUnlock()

	for _, s := range subs {
		// Each subscriber clones the state independently so receivers cannot
		// observe pointer aliasing across listeners.
		s.send(cloneState(state))
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
	state := cloneState(m.currentState)
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

// getListeners returns a copy of the listener channels (for testing)
func (n *notifier) getListeners() []chan CurrentState {
	n.mu.RLock()
	defer n.mu.RUnlock()
	listeners := make([]chan CurrentState, len(n.listeners))
	for i, s := range n.listeners {
		listeners[i] = s.ch
	}
	return listeners
}

// addListener adds a listener directly (for testing)
func (n *notifier) addListener(ch chan CurrentState) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.listeners = append(n.listeners, &subscriber{ch: ch})
}
