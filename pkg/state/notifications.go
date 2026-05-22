package state

import (
	"sync"
)

// subscriberBufferSize is the per-subscriber channel buffer. Sends are
// non-blocking and the latest value is preferred, so this primarily affects
// how many bursty updates a slow consumer can absorb before drops occur.
const subscriberBufferSize = 32

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

// notifier manages the subscriber/listener pattern for state updates.
//
// Notification dispatch is performed by a single, long-lived broadcaster
// goroutine instead of spawning a new goroutine per update. Callers post the
// latest state into a single-slot "pending" buffer (under pendingMu); the
// broadcaster wakes via the pendingCh signal, snapshots the pending state,
// and fans out to all subscribers. If multiple updates arrive before the
// broadcaster runs, only the most recent value is kept — this coalesces
// bursts (e.g. rapid sync progress) while guaranteeing that the latest state
// is always delivered.
type notifier struct {
	mu        sync.RWMutex
	listeners []*subscriber

	pendingMu    sync.Mutex
	pending      CurrentState
	pendingValid bool

	pendingCh chan struct{}
	doneCh    chan struct{}

	stopOnce sync.Once
	stopped  chan struct{}
}

// newNotifier creates a new notifier and starts its broadcaster goroutine.
func newNotifier() *notifier {
	n := &notifier{
		listeners: make([]*subscriber, 0),
		pendingCh: make(chan struct{}, 1),
		doneCh:    make(chan struct{}),
		stopped:   make(chan struct{}),
	}
	go n.run()
	return n
}

// run is the broadcaster loop. It blocks until either a notification is
// signalled via pendingCh or the notifier is stopped. On wake it pulls the
// latest pending state (if any) and fans it out to all current subscribers.
func (n *notifier) run() {
	defer close(n.doneCh)
	for {
		select {
		case <-n.stopped:
			// Drain any final pending state before exiting so the last
			// update isn't lost on shutdown.
			if state, ok := n.takePending(); ok {
				n.broadcast(state)
			}
			return
		case <-n.pendingCh:
			state, ok := n.takePending()
			if !ok {
				continue
			}
			n.broadcast(state)
		}
	}
}

// takePending atomically consumes the latest pending state, if any.
func (n *notifier) takePending() (CurrentState, bool) {
	n.pendingMu.Lock()
	defer n.pendingMu.Unlock()
	if !n.pendingValid {
		return CurrentState{}, false
	}
	state := n.pending
	n.pending = CurrentState{}
	n.pendingValid = false
	return state, true
}

// broadcast fans the given state out to every current subscriber.
func (n *notifier) broadcast(state CurrentState) {
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

// subscribe adds a new listener and returns a channel for state updates. If
// the notifier has already been closed the returned channel is closed
// immediately so consumers ranging over it don't block forever.
func (n *notifier) subscribe() chan CurrentState {
	n.mu.Lock()
	defer n.mu.Unlock()

	sub := &subscriber{ch: make(chan CurrentState, subscriberBufferSize)}
	select {
	case <-n.stopped:
		sub.closed = true
		close(sub.ch)
		return sub.ch
	default:
	}
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

// notify posts the given state into the pending slot (keeping only the most
// recent value) and signals the broadcaster. It never blocks.
//
// This is the entry point for all internal dispatchers; both the async
// fast-path (notifyListenersAsync) and direct internal callers go through it.
func (n *notifier) notify(state CurrentState) {
	// If Close was already called, silently drop further updates. Posting
	// after Close cannot be delivered because the broadcaster has exited.
	select {
	case <-n.stopped:
		return
	default:
	}

	n.pendingMu.Lock()
	n.pending = state
	n.pendingValid = true
	n.pendingMu.Unlock()

	// Non-blocking signal. The buffered (size 1) pendingCh acts as an
	// edge-triggered wakeup: if a signal is already queued the broadcaster
	// will pick up our newly-stored pending value on its next iteration.
	select {
	case n.pendingCh <- struct{}{}:
	default:
	}
}

// close stops the broadcaster goroutine, waits for it to exit, and closes
// every subscriber channel so consumers blocked on a receive (or ranging over
// the channel) wake up instead of leaking. Subsequent calls to notify will be
// silently dropped. Safe to call multiple times.
func (n *notifier) close() {
	n.stopOnce.Do(func() {
		close(n.stopped)
	})
	<-n.doneCh

	// Take ownership of the listener list under the write lock so that any
	// concurrent subscribe/unsubscribe sees the post-Close state. Closing each
	// subscriber individually goes through subscriber.close, which is
	// idempotent and races safely with concurrent send() calls (send becomes a
	// no-op once closed is set).
	n.mu.Lock()
	subs := n.listeners
	n.listeners = nil
	n.mu.Unlock()

	for _, s := range subs {
		s.close()
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

// Close shuts down the manager's notifier broadcaster goroutine. Safe to call
// multiple times. After Close, further state mutations will not produce
// notifications.
func (m *Manager) Close() {
	if m.notifier != nil {
		m.notifier.close()
	}
}

// notifyListeners sends current state to all subscribers
func (m *Manager) notifyListeners() {
	m.mu.RLock()
	state := cloneState(m.currentState)
	m.mu.RUnlock()

	m.notifyListenersAsync(state)
}

// notifyListenersAsync posts a state update to be broadcast by the notifier's
// long-lived broadcaster goroutine. The call is non-blocking and coalescing:
// rapid bursts collapse to the latest state, preventing unbounded goroutine
// proliferation and event reordering.
func (m *Manager) notifyListenersAsync(state CurrentState) {
	m.notifier.notify(state)
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
