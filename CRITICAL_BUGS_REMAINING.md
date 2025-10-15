# Critical Bugs Remaining - Priority Fix List

**Last Updated**: 2025-10-15
**Status**: 🔴 HIGH PRIORITY - Immediate action required

## Summary

This document lists all CRITICAL severity bugs that remain unfixed after the initial bug fix pass. These bugs can cause data loss, system crashes, or security breaches and must be addressed before production deployment.

---

## ✅ FIXED CRITICAL BUGS (Completed)

1. **99% Data Loss - History Corruption** ✅
   - Fixed: Added RLock in saveHistory()
   - Commit: 221d0fd

2. **WebSocket Authentication Bypass (CVSS 9.8)** ✅
   - Fixed: Added Basic Auth check before upgrade
   - Commit: 221d0fd

3. **Credential Exposure in /api/config** ✅
   - Fixed: Removed config content from API response
   - Commit: 221d0fd

4. **Concurrent StartSync Race** ✅
   - Fixed: Added CurrentSync != nil check
   - Commit: 221d0fd

5. **Path Traversal (CVSS 9.1)** ✅
   - Fixed: Added validateCardID() function
   - Commit: 221d0fd

6. **LED Controller Goroutine Leak** ✅
   - Fixed: Proper cleanup before new patterns
   - Commit: 221d0fd

7. **Channel Panic in notifyListeners** ✅
   - Fixed: Added panic recovery
   - Commit: 221d0fd

8. **CSRF Protection (CVSS 9.8)** ✅
   - Fixed: Implemented CSRF token system
   - Commit: a0ec9a7

9. **UpdateSyncProgress Nil Pointer** ✅
   - Fixed: Fail-fast error return
   - Commit: a0ec9a7

---

## 🔴 CRITICAL BUGS REMAINING

### Concurrency Issues (4 bugs)

#### BUG-C1: Listener Slice Modification During Iteration
**Severity**: CRITICAL
**Location**: `pkg/state/state.go:325-350`
**Test**: `TestSubscribeDuringNotification`

**Description**: Subscribe() and Unsubscribe() can modify the listeners slice while notifyListeners() is iterating, causing:
- Index out of bounds panic
- Skipped notifications
- Double notifications

**Fix Required**:
```go
// Use sync.Map or separate mutex for listener management
type Manager struct {
    listenersMu sync.RWMutex
    listeners   []chan CurrentState
    // ...
}

func (m *Manager) Subscribe() chan CurrentState {
    m.listenersMu.Lock()
    defer m.listenersMu.Unlock()
    ch := make(chan CurrentState, 10)
    m.listeners = append(m.listeners, ch)
    return ch
}
```

---

#### BUG-C2: FindLastSyncByCardID Slice Reallocation Race
**Severity**: CRITICAL
**Location**: `pkg/state/state.go:274-285`
**Test**: `TestHistoryAppendDuringSearch`

**Description**: FindLastSyncByCardID iterates history slice without lock while FinishSync appends to it, causing:
- Slice reallocation during iteration
- Panic: runtime error: slice bounds out of range
- Returned pointer to wrong sync record

**Fix Required**:
```go
func (m *Manager) FindLastSyncByCardID(cardID string) *SyncRecord {
    m.mu.RLock()
    defer m.mu.RUnlock()

    // Iterate backwards to find most recent
    for i := len(m.history) - 1; i >= 0; i-- {
        if m.history[i].CardID == cardID {
            // Return copy, not pointer to slice element
            record := m.history[i]
            return &record
        }
    }
    return nil
}
```

---

#### BUG-C3: Unsynchronized lastProgressSave Access
**Severity**: CRITICAL (race detector)
**Location**: `pkg/state/state.go:220-223`

**Status**: ⚠️ PARTIALLY FIXED - Need to verify with race detector

The lastProgressSave field was moved inside the lock in the recent fix, but we need to verify with:
```bash
go test -race ./pkg/state -run TestSaveThrottlingRaceCondition
```

If race still detected, move lastProgressSave access completely inside mutex.

---

#### BUG-C4: Unsubscribe During notifyListeners Race
**Severity**: CRITICAL
**Location**: `pkg/state/state.go:308-350`
**Test**: `TestUnsubscribeDuringNotification`

**Description**: Unsubscribe() closes a channel that notifyListeners() is about to send to, causing:
- Send on closed channel panic
- Service crash

**Fix Required**:
Use a generation counter or mark channels as closed instead of immediately closing them.

---

### SD Card / Mount Issues (3 bugs)

#### BUG-C5: Mount Fails But Device Marked as Mounted
**Severity**: CRITICAL
**Location**: `pkg/sdmonitor/sdmonitor.go:135-147`
**Test**: `TestMountFailureButDeviceMarkedAsMounted`

**Description**: When mount() fails, the code still sets lastDevice and sends EventInserted, causing sync to run on empty directory.

**Fix Required**:
```go
if err := m.mount(device); err != nil {
    log.Printf("Failed to mount device %s: %v", device, err)
    return  // Don't set lastDevice or send event
}

m.lastDevice = device  // Only after successful mount
m.eventChan <- Event{/*...*/}
```

---

#### BUG-C6: Unmount During Active File Operations
**Severity**: CRITICAL
**Location**: `pkg/sdmonitor/sdmonitor.go:281-289`
**Test**: `TestUnmountDuringActiveFileOperations`

**Description**: unmount() doesn't use MNT_FORCE or MNT_DETACH, causing EBUSY errors when rclone is reading files. Sync continues on unmounted filesystem.

**Fix Required**:
```go
func (m *Monitor) unmount() error {
    // Try graceful unmount first
    if err := unix.Unmount(m.mountPath, 0); err != nil {
        // Force unmount if busy
        log.Printf("Unmount busy, forcing: %s", m.mountPath)
        if err := unix.Unmount(m.mountPath, unix.MNT_FORCE); err != nil {
            log.Printf("Force unmount failed: %v", err)
            return err
        }
    }
    return nil
}
```

---

#### BUG-C7: Full SD Card Returns Success on ID Write Failure
**Severity**: HIGH → CRITICAL (causes duplicate uploads)
**Location**: `pkg/sdmonitor/sdmonitor.go` (GetOrCreateCardID)
**Test**: `TestFullSDCardNoSpaceForID`

**Description**: When SD card is full, writing .pictures-sync-id fails but function returns success with empty/partial ID. Next insertion generates new ID → duplicate uploads.

**Fix Required**:
```go
func GetOrCreateCardID(...) (string, bool, error) {
    // Check disk space before writing
    var stat unix.Statfs_t
    if err := unix.Statfs(mountPath, &stat); err == nil {
        availableBytes := stat.Bavail * uint64(stat.Bsize)
        if availableBytes < 4096 {  // Need at least 4KB
            return "", false, fmt.Errorf("SD card full, cannot write card ID")
        }
    }

    // Write ID
    if err := os.WriteFile(idPath, []byte(cardID), 0644); err != nil {
        return "", false, fmt.Errorf("failed to write card ID: %w", err)
    }

    // Verify write succeeded
    written, err := os.ReadFile(idPath)
    if err != nil || string(written) != cardID {
        return "", false, fmt.Errorf("card ID write verification failed")
    }

    return cardID, true, nil
}
```

---

### HTTP API Security (3 bugs)

#### BUG-C8: No Rate Limiting (CVSS 7.5)
**Severity**: CRITICAL
**Location**: All API endpoints
**Test**: `TestHTTPAPIRateLimiting`

**Description**: No rate limiting on any endpoint allows:
- Unlimited brute force attacks on password
- DoS via API flooding
- Tested: 100+ requests in <1 second

**Fix Required**:
Implement token bucket rate limiter (example code available in HTTP_API_SECURITY_VULNERABILITIES.md lines 1200-1250)

---

#### BUG-C9: Server-Side Request Forgery (CVSS 9.1)
**Severity**: CRITICAL
**Location**: Multiple endpoints (DNS lookup, ping, config)
**Test**: `TestSSRFVulnerabilities`

**Description**: No validation of target addresses allows:
- AWS/GCP metadata theft: http://169.254.169.254/latest/meta-data/
- Internal network scanning
- Localhost service access

**Fix Required**:
```go
func isPrivateOrMetadata(host string) bool {
    ip := net.ParseIP(host)
    if ip == nil {
        // Resolve hostname
        ips, _ := net.LookupIP(host)
        if len(ips) == 0 {
            return false
        }
        ip = ips[0]
    }

    // Block metadata services
    if ip.String() == "169.254.169.254" {
        return true
    }

    // Block private ranges
    return ip.IsLoopback() || ip.IsPrivate()
}
```

---

#### BUG-C10: Path Traversal in File Operations
**Severity**: CRITICAL
**Location**: handleThumbnail, handleSDCardFiles
**Test**: `TestPathTraversalInFileOperations`

**Description**: Multiple path traversal bypasses tested:
- `../../../etc/passwd` works
- URL encoding bypasses
- Double-slash bypasses

**Fix Required**:
Use filepath.Clean() and verify result is within allowed directory.

---

### Error Handling (2 bugs)

#### BUG-C11: No Panic Recovery in Listener Goroutines
**Severity**: CRITICAL
**Location**: `pkg/state/state.go:325-350` (notifyListeners)
**Test**: `TestPanicInSubscriberCallback`

**Status**: ✅ PARTIALLY FIXED (panic recovery added in a0ec9a7)

Verify with test that malicious subscribers can't crash service.

---

#### BUG-C12: Channel Leaks from Subscribe()
**Severity**: CRITICAL (memory leak → OOM)
**Location**: `pkg/state/state.go:298-305`
**Test**: `TestSubscribeWithoutUnsubscribe`

**Description**: No limit on subscribers. Test shows:
- 1000 subscriptions = 10MB memory
- 10000 subscriptions = 100MB memory
- Eventually causes OOM

**Fix Required**:
```go
const maxSubscribers = 100

func (m *Manager) Subscribe() (chan CurrentState, error) {
    m.mu.Lock()
    defer m.mu.Unlock()

    if len(m.listeners) >= maxSubscribers {
        return nil, fmt.Errorf("maximum subscribers reached")
    }

    ch := make(chan CurrentState, 10)
    m.listeners = append(m.listeners, ch)
    return ch, nil
}
```

---

## Priority Timeline

### Week 1 (Immediate - Days 1-3)
1. BUG-C8: Rate limiting (prevents brute force)
2. BUG-C9: SSRF protection (prevents credential theft)
3. BUG-C5: Mount failure handling (prevents silent failures)

### Week 1 (Days 4-7)
4. BUG-C1: Listener slice races (prevents crashes)
5. BUG-C6: Force unmount (prevents I/O errors)
6. BUG-C10: Path traversal (prevents file access)

### Week 2
7. BUG-C2: History search races (prevents panics)
8. BUG-C4: Unsubscribe races (prevents panics)
9. BUG-C7: Full SD card handling (prevents duplicates)
10. BUG-C12: Subscriber limits (prevents OOM)

### Week 3 (Verification)
11. BUG-C3: Race detector verification
12. BUG-C11: Panic recovery verification
13. Comprehensive testing with race detector
14. Load testing with multiple SD cards

---

## Testing Commands

```bash
# Run all CRITICAL bug tests
go test -v ./pkg/state -run "TestHistoryAppend|TestSubscribeDuring|TestUnsubscribeDuring"
go test -v ./pkg/sdmonitor -run "TestMountFailure|TestUnmountDuring|TestFullSDCard"
go test -v ./cmd/webui -run "TestHTTPAPIRateLimiting|TestSSRF|TestPathTraversal"

# Race detector (CRITICAL)
go test -race ./...

# Memory leak detection
go test -v ./pkg/state -run TestSubscribeWithoutUnsubscribe -memprofile=mem.prof
```

---

## Metrics

- **Total CRITICAL Bugs**: 21
- **Fixed**: 9 (43%)
- **Remaining**: 12 (57%)
- **Estimated Fix Time**: 3-4 weeks
- **Production Ready**: ❌ NOT READY

---

## Risk Assessment

**Current State**: 🔴 **HIGH RISK**

Without these fixes:
- **Data Loss**: Possible (history corruption, nil pointers)
- **Security Breach**: Likely (SSRF, path traversal, no rate limit)
- **Service Crash**: Frequent (panics, race conditions)
- **Data Corruption**: Possible (mount failures, force unmount)

**Recommendation**: Complete Week 1 fixes before ANY production deployment.
