package googlephotos

import (
	"fmt"
	"time"
)

const (
	circuitBreakerThreshold = 6
	circuitBreakerCooldown  = 2 * time.Minute
)

// circuitBreaker tracks consecutive failures and trips open when the threshold
// is exceeded. It is embedded in SyncManager and protected by sm.mu.
type circuitBreaker struct {
	failures int
	openAt   time.Time
}

func (sm *SyncManager) recordFailure() {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.cb.failures++
	if sm.cb.failures >= circuitBreakerThreshold {
		sm.cb.openAt = time.Now()
		if sm.progress != nil {
			sm.progress.DebugDetails = append(sm.progress.DebugDetails, "Google Photos upload circuit breaker opened after repeated failures")
		}
	}
}

func (sm *SyncManager) recordSuccess() {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.cb.failures = 0
	sm.cb.openAt = time.Time{}
}

func (sm *SyncManager) allowRequest() bool {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.cb.openAt.IsZero() || time.Since(sm.cb.openAt) > circuitBreakerCooldown
}

// circuitBreakerOpenError returns a descriptive error when the breaker is open.
func (sm *SyncManager) circuitBreakerOpenError() error {
	return fmt.Errorf("upload circuit breaker open; retry after %s", sm.cb.openAt.Add(circuitBreakerCooldown).Format(time.RFC3339))
}
