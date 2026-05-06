//go:build !stress

package state

import (
	"testing"
	"time"
)

// setupRaceTestManager creates a test manager without file persistence.
func setupRaceTestManager(t *testing.T) *Manager {
	t.Helper()

	return &Manager{
		currentState:      CurrentState{Status: StatusIdle},
		history:           make([]SyncRecord, 0),
		notifier:          newNotifier(),
		progressSaveDelay: 100 * time.Millisecond,
	}
}
