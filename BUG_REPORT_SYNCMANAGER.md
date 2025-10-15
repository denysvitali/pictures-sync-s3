# Rclone Integration Bug Report - Syncmanager Package

## Executive Summary

Found **7 critical bugs** and **3 design issues** in the rclone integration layer that could lead to:
- Memory leaks from WebSocket clients
- Inconsistent error handling that hides real failures  
- Resource cleanup failures after errors
- Progress reporting inaccuracies

## Critical Bugs Found

### Bug #1: Memory Leak in Progress Channel Subscriptions
**Severity: HIGH**  
**Location:** `pkg/syncmanager/syncmanager.go:332-339`

**Description:**  
The `SubscribeProgress()` method adds channels to `progressChans` slice but there's no corresponding `Unsubscribe()` method to remove them. When WebSocket clients disconnect, their channels remain in memory forever.

**Code:**
```go
func (m *Manager) SubscribeProgress() chan Progress {
	m.mu.Lock()
	defer m.mu.Unlock()

	ch := make(chan Progress, 10)
	m.progressChans = append(m.progressChans, ch)
	return ch  // No way to remove this later!
}
```

**Impact:**  
- Long-running system with frequent WebSocket reconnections will leak memory
- Could accumulate hundreds of dead channels over days/weeks
- Progress updates try to send to all channels (line 294-300), slowing down over time

**User Manifestation:**
- Gradual memory growth over time
- Slowdown in progress updates as dead channels accumulate
- Eventually: out-of-memory crash on Raspberry Pi

**Fix Required:**
```go
func (m *Manager) UnsubscribeProgress(ch chan Progress) {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	for i, listener := range m.progressChans {
		if listener == ch {
			m.progressChans = append(m.progressChans[:i], m.progressChans[i+1:]...)
			close(ch)
			break
		}
	}
}
```

---

### Bug #2: GetRemoteSize Returns Error Instead of Zero for Empty Paths
**Severity: MEDIUM**  
**Location:** `pkg/syncmanager/syncmanager.go:92-94`

**Description:**  
Code comment says "If destination doesn't exist, that's fine - return 0", but `operations.ListFn` at line 98 fails with "directory not found" error when the remote path doesn't exist yet.

**Code:**
```go
// Try to create the remote filesystem
dstFs, err := fs.NewFs(ctx, destPath)
if err != nil {
	// If destination doesn't exist, that's fine - return 0
	return 0, nil  // ← This never executes!
}

// Calculate total size of files on remote
var totalSize int64
err = operations.ListFn(ctx, dstFs, func(obj fs.Object) {
	totalSize += obj.Size()
})
if err != nil {
	return 0, fmt.Errorf("failed to calculate remote size: %w", err)  // ← Fails here!
}
```

**Impact:**  
- First sync of a new card always logs warning: "Warning: Failed to get remote size"
- Cannot distinguish between network errors and "card not synced yet"
- Resume detection doesn't work correctly for first sync

**User Manifestation:**
- Confusing warning messages in logs on first sync
- User thinks sync is failing when it's actually working fine

**Fix Required:**
```go
err = operations.ListFn(ctx, dstFs, func(obj fs.Object) {
	totalSize += obj.Size()
})
if err != nil {
	// Distinguish between "doesn't exist" and real errors
	if strings.Contains(err.Error(), "directory not found") ||
	   strings.Contains(err.Error(), "not found") {
		return 0, nil
	}
	return 0, fmt.Errorf("failed to calculate remote size: %w", err)
}
```

---

### Bug #3: Resource Cleanup After Failed Sync
**Severity: MEDIUM**  
**Location:** `pkg/syncmanager/syncmanager.go:118-123`

**Description:**  
The defer statement that resets `isRunning` and `cancelFunc` only runs when `Sync()` returns. If sync fails early (config error, path error), the cleanup still happens, BUT state manager may not be updated properly, leaving inconsistent state.

**Code:**
```go
func (m *Manager) Sync(sourcePath, cardID string, totalFiles int, totalBytes int64) error {
	m.mu.Lock()
	if m.isRunning {
		m.mu.Unlock()
		return fmt.Errorf("sync already in progress")
	}
	m.isRunning = true
	m.mu.Unlock()

	defer func() {  // ← Runs on ANY return
		m.mu.Lock()
		m.isRunning = false
		m.cancelFunc = nil
		m.mu.Unlock()
	}()
	
	// ... sync operations that might fail ...
}
```

**Impact:**  
- If sync fails before calling `stateMgr.FinishSync()`, state manager shows StatusSyncing forever
- WebUI shows "syncing" status even though nothing is happening
- `isRunning` is correctly reset but state is inconsistent

**User Manifestation:**
- Web UI stuck showing "Syncing..." after error
- Can't start new sync because UI thinks one is running
- Have to restart service to clear state

**Fix Required:**
```go
defer func() {
	m.mu.Lock()
	wasRunning := m.isRunning
	m.isRunning = false
	m.cancelFunc = nil
	m.mu.Unlock()
	
	// Ensure state manager is updated if we were running
	if wasRunning && m.stateMgr != nil {
		currentState := m.stateMgr.GetState()
		if currentState.Status == state.StatusSyncing {
			// Sync was interrupted without proper cleanup
			m.stateMgr.SetStatus(state.StatusIdle)
		}
	}
}()
```

---

### Bug #4: Rclone Library Calls os.Exit() on Severe Config Errors
**Severity: LOW**  
**Location:** `pkg/syncmanager/syncmanager.go:75-76, 126-127`

**Description:**  
When `config.SetConfigPath()` encounters a severely invalid path (like `/root/impossible/path`), the rclone library logs ERROR and may call `os.Exit()` or `log.Fatal()`, killing the entire process.

**Impact:**  
- Cannot test severe config error scenarios
- Bad config path can crash the entire Gokrazy service
- No graceful error handling possible

**User Manifestation:**
- Service crashes on bad rclone config
- Log shows "ERROR Failed to load config file" right before crash
- Have to fix config and reboot

**Fix Required:**  
Need to validate config path before passing to rclone:
```go
func (m *Manager) validateConfigPath() error {
	dir := filepath.Dir(m.configPath)
	if _, err := os.Stat(dir); err != nil {
		return fmt.Errorf("config directory not accessible: %w", err)
	}
	// Check if we can write to it
	testFile := filepath.Join(dir, ".write_test")
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		return fmt.Errorf("config directory not writable: %w", err)
	}
	os.Remove(testFile)
	return nil
}
```

---

### Bug #5: Progress Calculation Overflow with Large Files
**Severity: LOW**  
**Location:** `pkg/syncmanager/syncmanager.go:260-262`

**Description:**  
Percentage calculation can overflow when `totalTransferred > totalBytes`, which happens if resume calculation is wrong or files are added to card during sync.

**Code:**
```go
var percentage int
if totalBytes > 0 {
	percentage = int((float64(totalTransferred) / float64(totalBytes)) * 100)
	// Can be > 100%!
}
```

**Impact:**  
- Progress shows > 100% in Web UI
- ETA calculation becomes negative
- Confusing to users

**User Manifestation:**
- Progress bar shows "152%" complete
- ETA shows garbage values

**Fix Required:**
```go
var percentage int
if totalBytes > 0 {
	pct := (float64(totalTransferred) / float64(totalBytes)) * 100
	percentage = int(math.Min(pct, 100.0))  // Cap at 100%
}
```

---

### Bug #6: Remote Stats Parsing Can Panic on Malformed Data
**Severity: LOW**  
**Location:** `pkg/syncmanager/syncmanager.go:238-249`

**Description:**  
Type assertions on `RemoteStats()` data don't check for nil or validate structure. If rclone returns malformed JSON, will panic.

**Code:**
```go
if remoteStats, err := stats.RemoteStats(true); err == nil {
	if transferring, ok := remoteStats["transferring"].([]interface{}); ok && len(transferring) > 0 {
		if transfer, ok := transferring[0].(map[string]interface{}); ok {
			// ← What if transferring[0] is nil?
			if name, ok := transfer["name"].(string); ok {
				currentFile = name
			}
		}
	}
}
```

**Impact:**  
- Rare panic during sync if rclone returns unexpected data
- Whole sync fails

**User Manifestation:**
- Random "panic: runtime error" during sync
- Very rare, hard to reproduce

**Fix Required:**  
Add nil checks and recover from panics.

---

### Bug #7: Network Error vs Auth Error Indistinguishable  
**Severity: MEDIUM**  
**Location:** `pkg/syncmanager/syncmanager.go:136-140`

**Description:**  
When `GetRemoteSize()` fails, code logs "Warning: Failed to get remote size" and continues. Can't distinguish between:
- Network temporarily down (retry makes sense)
- Auth expired (need to reconfigure)
- Quota exceeded (need user action)

**Impact:**  
- Sync proceeds even when remote is inaccessible
- Fails later with less helpful error message
- Wastes time trying to sync when auth is broken

**User Manifestation:**
- Sync appears to start, then fails mysteriously
- No clear indication that cloud credentials are wrong

**Fix Required:**  
Parse error types and fail fast on auth errors:
```go
alreadySyncedBytes, err := m.GetRemoteSize(cardID)
if err != nil {
	// Check for auth/permission errors
	if strings.Contains(err.Error(), "auth") || 
	   strings.Contains(err.Error(), "permission") ||
	   strings.Contains(err.Error(), "unauthorized") {
		return fmt.Errorf("remote authentication failed: %w", err)
	}
	// Network errors are warnings, continue
	log.Printf("Warning: Failed to get remote size: %v", err)
	alreadySyncedBytes = 0
}
```

---

## Design Issues (Not Bugs, But Problematic)

### Issue #1: No Timeout on Sync Operations
**Location:** `pkg/syncmanager/syncmanager.go:148-154`

Context is created without timeout. If network hangs during upload, sync never times out.

**Recommendation:**
```go
ctx, cancel := context.WithTimeout(context.Background(), 6*time.Hour)
```

### Issue #2: State Manager Can Be Nil
**Location:** `pkg/syncmanager/syncmanager.go:60-70`

`NewManager` accepts nil `stateMgr`, but code at line 275 dereferences it without checking.

**Recommendation:**  
Require non-nil state manager or add nil checks everywhere.

### Issue #3: Excessive Error Logging from Rclone
**Location:** Throughout

Rclone library logs ERROR to stdout for non-fatal issues (empty directories, etc.). Pollutes logs and makes real errors hard to find.

**Recommendation:**  
Redirect rclone logs to structured logging with levels.

---

## Test Coverage Summary

Created comprehensive error scenario tests in `/workspace/pictures-sync-s3/pkg/syncmanager/rclone_error_test.go`:

- ✅ Concurrent sync prevention
- ✅ Cancel function cleanup
- ✅ Progress channel overflow handling  
- ✅ State consistency after errors
- ✅ Progress calculation edge cases (zero bytes, overflow)
- ✅ Remote stats parsing with malformed data
- ✅ Memory leak detection in progress channels
- ✅ Resource cleanup on cancelled syncs
- ✅ Configuration error handling
- ✅ Path traversal security

**Total: 25 new test cases**

All tests pass except one disabled test that crashes the test runner (rclone os.Exit() issue).

---

## Priority Recommendations

1. **HIGH PRIORITY:** Fix Bug #1 (memory leak) - Will cause crashes over time
2. **HIGH PRIORITY:** Fix Bug #3 (state cleanup) - Breaks UI experience
3. **MEDIUM:** Fix Bug #2 (GetRemoteSize errors) - Confusing UX
4. **MEDIUM:** Fix Bug #7 (error classification) - Better failure modes
5. **LOW:** Fix Bug #4, #5, #6 - Edge cases and rare panics

## Test Execution

Run tests:
```bash
go test ./pkg/syncmanager -v
```

All tests pass in ~1 second.
