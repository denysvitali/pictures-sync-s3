# SD Card Edge Cases - Comprehensive Bug Report

This document catalogs all bugs discovered through comprehensive edge case testing of SD card handling in the pictures-sync-s3 project.

## Test Coverage

The test suite `sdcard_edge_cases_comprehensive_test.go` covers:

1. ✅ Card removal during sync operation
2. ✅ Card corruption and read errors
3. ✅ Full SD card (no space for .pictures-sync-id file)
4. ✅ Card with special characters in filenames
5. ✅ Card with deeply nested directory structures
6. ✅ Card with symlinks and special files
7. ✅ Card format detection (FAT32, exFAT, NTFS, ext4)
8. ✅ Read-only cards and write-protected cards
9. ✅ Card with millions of tiny files
10. ✅ Card hot-swapping (rapid insert/remove)

## Critical Severity Bugs (Immediate Action Required)

### BUG-001: Race Condition in Card Detection and Mounting
**Location:** `pkg/sdmonitor/sdmonitor.go:125-148` (checkDevices method)

**Scenario:**
1. Device detected at line 128
2. Card physically removed before mount() at line 135
3. mount() fails and logs error at line 136-137
4. Function returns without updating lastDevice
5. No event sent, but device remains untracked

**Impact:**
- Silent mount failures
- Device state becomes inconsistent
- Next poll cycle may retry mount repeatedly
- No error notification to user

**Proof of Concept:**
```go
// See TestCardRemovalDuringMount
```

**Fix:**
1. Verify mount success before updating lastDevice
2. Add backoff mechanism for failed mount attempts
3. Consider sending EventError for mount failures

---

### BUG-002: No Device Validation During Sync Preparation
**Location:** `cmd/pictures-sync/main.go:106-224` (handleCardInserted)

**Scenario:**
1. Card A inserted, EventInserted sent (line 142)
2. handleCardInserted runs in goroutine (line 120)
3. User swaps Card A for Card B before HasDCIM check
4. All operations (HasDCIM, CountPhotos, GetOrCreateCardID) read Card B
5. Sync proceeds with Card B's files but Card A's event metadata

**Impact:**
- Photos from Card B uploaded to Card A's remote folder
- Data loss and severe user confusion
- No detection of card swap

**Proof of Concept:**
```go
// See TestCardSwapDuringSyncPrepare
```

**Fix:**
1. Store device serial number or UUID at event time
2. Validate device identity before each operation
3. Re-check mount path matches original device
4. Abort sync if device mismatch detected

---

### BUG-003: Card Removal During Photo Count
**Location:** `pkg/sdmonitor/sdmonitor.go:302-331` (CountPhotos)

**Scenario:**
1. CountPhotos called in goroutine (main.go:129)
2. Card removed while WalkDir is traversing
3. WalkDir returns error mid-operation
4. Error propagated but count may be partial
5. Sync may proceed with stale data

**Impact:**
- Incorrect file counts reported
- Progress percentage miscalculated
- User sees "success" but files missing

**Proof of Concept:**
```go
// See TestCardRemovalDuringPhotoCount
```

**Fix:**
1. Add context.Context parameter to CountPhotos
2. Cancel context on card removal event
3. Check context.Done() in WalkDir callback
4. Return context.Canceled error for clear handling

---

## High Severity Bugs (Fix Soon)

### BUG-004: Full SD Card Cannot Write ID
**Location:** `pkg/sdmonitor/sdmonitor.go:374-377` (GetOrCreateCardID)

**Scenario:**
1. SD card is full (0 bytes free)
2. GetOrCreateCardID tries to write .pictures-sync-id
3. WriteFile fails with "no space left on device"
4. Error returned, but generated ID already created
5. Next insertion generates different ID

**Impact:**
- Card gets new ID on every insertion
- Photos uploaded to different remote folders each time
- Duplicate uploads, wasted bandwidth
- User confusion about card identity

**Proof of Concept:**
```go
// See TestFullSDCardNoSpaceForID
// See TestMinimalSpaceForCardID
```

**Fix:**
1. Check available space with syscall.Statfs before write
2. Require minimum 1KB free space
3. If insufficient space, try cleanup:
   - Delete temporary files
   - Suggest deleting old photos
4. If still no space, generate ephemeral ID with warning

---

### BUG-005: Write-Protected Cards Cannot Sync
**Location:** `pkg/sdmonitor/sdmonitor.go:248-268` (mount method)

**Scenario:**
1. Card inserted with write-protect switch enabled
2. mount() attempts read-write mount (line 253)
3. Mount succeeds but filesystem enforces read-only
4. GetOrCreateCardID tries to write ID file
5. Write fails with EROFS (read-only filesystem)
6. Sync aborted entirely

**Impact:**
- Write-protected cards cannot be backed up
- No indication to user about write-protection
- Card gets new ID every insertion (can't persist)
- User must remember to disable write-protect

**Proof of Concept:**
```go
// See TestWriteProtectedCard
// See TestReadOnlyFilesystem
```

**Fix:**
1. Detect write-protection: check statfs ST_RDONLY flag
2. If protected and ID exists: read and use existing ID
3. If protected and no ID: generate ephemeral session ID
4. Display clear WebUI message about write-protection
5. Allow sync to proceed with warning

---

### BUG-006: Event Channel Overflow Blocks Polling
**Location:** `pkg/sdmonitor/sdmonitor.go:55,142` (event channel buffer)

**Scenario:**
1. Event channel has buffer of 10 (line 55)
2. Slow consumer (handleCardInserted takes >20s)
3. Rapid device changes (unstable USB connection)
4. Buffer fills with 10 pending events
5. checkDevices tries to send 11th event (line 142)
6. Send blocks because channel is full
7. pollDevices goroutine stalls

**Impact:**
- Device removal not detected
- System appears hung
- No new cards detected until old events processed
- Critical failure in main monitoring loop

**Proof of Concept:**
```go
// See TestEventChannelBackpressure
// See TestRapidInsertRemoveCycles
```

**Fix:**
1. Use non-blocking send with select/default
2. Drop oldest event if buffer full (with log warning)
3. Increase buffer size to 100
4. Add event coalescing (remove duplicate consecutive events)

---

### BUG-007: RemountReadOnly Failure Leaves Card Writable
**Location:** `pkg/sdmonitor/sdmonitor.go:361-363,385-387` (GetOrCreateCardID)

**Scenario:**
1. Card mounted read-write for ID write
2. ID written successfully
3. RemountReadOnly called
4. Remount fails (e.g., device disconnected)
5. Error returned but card remains mounted read-write
6. Sync proceeds with writable card

**Impact:**
- Camera could write to card during sync
- Data corruption if camera and rclone write simultaneously
- Photos could be lost or corrupted
- Filesystem integrity at risk

**Proof of Concept:**
```go
// See TestReadOnlyRemountAfterCardIDWrite
```

**Fix:**
1. If RemountReadOnly fails, immediately unmount card
2. Abort sync operation
3. Send error event with clear message
4. Do not allow sync to proceed on read-write card

---

### BUG-008: No Timeout for Large Directory Counts
**Location:** `pkg/sdmonitor/sdmonitor.go:302-331` (CountPhotos)

**Scenario:**
1. SD card has 500,000 files (4K burst mode photography)
2. CountPhotos called, walks entire tree
3. WalkDir takes 5+ minutes
4. No timeout, no progress reporting
5. User sees system hang with no feedback

**Impact:**
- System appears frozen for minutes
- No way to cancel operation
- No progress indication
- User may force-reboot device

**Proof of Concept:**
```go
// See TestMillionsOfTinyFiles
```

**Fix:**
1. Add context.Context with timeout (5 minutes)
2. Check context.Done() in WalkDir callback every 100 files
3. Add progress callback: `func(count, bytes int64)`
4. Update WebUI with counting progress
5. Allow user to cancel count operation

---

## Medium Severity Bugs (Should Fix)

### BUG-009: WalkDir Error Stops Entire Count
**Location:** `pkg/sdmonitor/sdmonitor.go:308-328` (CountPhotos)

**Scenario:**
1. DCIM has 10 subdirectories with 1000 photos each
2. One subdirectory has permission denied
3. WalkDir returns error at first failure
4. Function returns with error, count = 0
5. 9000 accessible photos not counted

**Impact:**
- Single corrupt/inaccessible directory prevents all counting
- Sync aborted even though most photos accessible
- No partial results returned

**Proof of Concept:**
```go
// See TestCorruptedFilesystem
// See TestWalkDirErrors
```

**Fix:**
1. Continue walking on errors, collect error list
2. Return partial count + error slice
3. Log warnings for inaccessible directories
4. Only fail if DCIM root is inaccessible

---

### BUG-010: No Retry for Transient I/O Errors
**Location:** `pkg/sdmonitor/sdmonitor.go:322-324` (CountPhotos size calculation)

**Scenario:**
1. USB connection momentarily unstable
2. d.Info() returns temporary I/O error
3. Error propagated, entire count fails
4. Retry would have succeeded

**Impact:**
- Transient errors cause permanent failures
- User must reinsert card to retry
- Poor user experience

**Fix:**
1. Add retry logic with exponential backoff
2. Retry up to 3 times on transient errors
3. Only fail if error persists

---

### BUG-011: Concurrent Stop() Calls Panic
**Location:** `pkg/sdmonitor/sdmonitor.go:78` (Stop method)

**Scenario:**
1. Multiple goroutines call monitor.Stop()
2. First call closes stopChan
3. Second call tries to close already-closed channel
4. Panic: "close of closed channel"

**Impact:**
- Application crashes on shutdown
- Ungraceful termination
- May lose sync progress

**Proof of Concept:**
```go
// See TestConcurrentStopCalls
```

**Fix:**
```go
type Monitor struct {
    // ...
    stopOnce sync.Once
}

func (m *Monitor) Stop() {
    m.stopOnce.Do(func() {
        close(m.stopChan)
        if m.lastDevice != "" {
            m.unmount()
        }
    })
}
```

---

### BUG-012: Null Bytes Not Validated in Card ID
**Location:** `pkg/sdmonitor/sdmonitor.go:354` (GetOrCreateCardID)

**Scenario:**
1. Corrupted card ID file contains null bytes
2. ReadFile reads: "card-test\x00\x00corrupt"
3. TrimSpace doesn't remove null bytes
4. Card ID contains null bytes: "card-test\x00\x00corrupt"
5. Used in filepath.Join -> string truncated at first null

**Impact:**
- Path traversal potential
- Incorrect remote paths
- Files uploaded to wrong locations

**Proof of Concept:**
```go
// See TestFilenamesWithNullBytes
```

**Fix:**
```go
cardID := strings.TrimSpace(string(data))
if strings.ContainsRune(cardID, 0) {
    log.Printf("WARNING: Card ID contains null bytes, generating new ID")
    // Generate new ID
}
```

---

### BUG-013: No Filesystem Health Check
**Location:** `pkg/sdmonitor/sdmonitor.go:250-268` (mount method)

**Scenario:**
1. Card has corrupted FAT table
2. Mount succeeds but file sizes wrong
3. File reports 10TB size instead of 10MB
4. TotalBytes calculation wildly incorrect
5. Progress percentage meaningless

**Impact:**
- Incorrect progress reporting
- User confusion
- Possible sync failures

**Fix:**
1. Validate file sizes are reasonable (<10GB per photo)
2. Check filesystem with fsck before operations
3. Warn user if filesystem appears corrupt

---

### BUG-014: No Debouncing for Device Changes
**Location:** `pkg/sdmonitor/sdmonitor.go:91-105` (pollDevices)

**Scenario:**
1. Unstable USB connection
2. Device appears/disappears rapidly (electrical noise)
3. Each change triggers event
4. 100+ events generated in 10 seconds
5. System overwhelmed processing events

**Impact:**
- CPU spikes
- Event handler called repeatedly
- Sync starts and stops rapidly
- User experience degraded

**Proof of Concept:**
```go
// See TestRapidInsertRemoveCycles
```

**Fix:**
1. Add debounce timer (1 second)
2. Only send event if state stable for 1 second
3. Prevents flapping from triggering actions

---

## Low Severity Bugs (Nice to Have)

### BUG-015: No Maximum Depth Limit
**Location:** `pkg/sdmonitor/sdmonitor.go:308` (CountPhotos)

**Scenario:**
- Malicious or corrupted filesystem with 1000+ nested directories
- WalkDir exhausts stack
- Potential DoS

**Fix:** Add depth limit of 50 levels

---

### BUG-016: Missing Filesystem Types
**Location:** `pkg/sdmonitor/sdmonitor.go:250` (mount method)

**Missing:**
- F2FS (modern SD cards)
- BTRFS
- XFS
- HFS+/APFS (macOS cards)
- UDF (some cameras)

**Fix:** Add more types to try list, or use blkid for detection

---

### BUG-017: No Special File Type Filtering
**Location:** `pkg/sdmonitor/sdmonitor.go:313` (CountPhotos)

**Issue:**
- Doesn't filter FIFOs, sockets, block devices
- Could attempt to read special files

**Fix:** Add check: `d.Type().IsRegular()`

---

### BUG-018: No Path Length Validation
**Location:** Various locations

**Issue:**
- No validation of PATH_MAX (typically 4096)
- Long paths silently fail on some systems

**Fix:** Check path length before operations

---

## Statistics

- **Total Bugs Found:** 18
- **Critical Severity:** 3
- **High Severity:** 5
- **Medium Severity:** 6
- **Low Severity:** 4

## Test Execution

To run the comprehensive test suite:

```bash
# Run all edge case tests
go test -v ./pkg/sdmonitor -run TestCard

# Run specific category
go test -v ./pkg/sdmonitor -run TestCardRemoval

# Run with race detector
go test -race ./pkg/sdmonitor

# Generate coverage report
go test -coverprofile=coverage.out ./pkg/sdmonitor
go tool cover -html=coverage.out
```

## Recommended Fix Priority

1. **Immediate (This Week):**
   - BUG-002: Device validation in handleCardInserted
   - BUG-003: Add context.Context to CountPhotos
   - BUG-006: Fix event channel overflow

2. **Soon (This Month):**
   - BUG-001: Fix mount error handling
   - BUG-004: Handle full SD cards
   - BUG-005: Support write-protected cards
   - BUG-007: Fix RemountReadOnly failure handling
   - BUG-008: Add timeout to CountPhotos

3. **Eventually (Next Quarter):**
   - All medium severity bugs
   - Low severity bugs as time permits

## Additional Testing Recommendations

1. **Hardware Testing:**
   - Test with actual SD cards (various sizes: 8GB to 512GB)
   - Test with various filesystem types (FAT32, exFAT, ext4)
   - Test with write-protected cards
   - Test with corrupted filesystems

2. **Stress Testing:**
   - Cards with 100K+ files
   - Rapid insert/remove cycles (100 times)
   - Concurrent multi-card readers

3. **Integration Testing:**
   - Full sync workflow with edge cases
   - Error recovery scenarios
   - User notification testing

---

**Generated:** 2025-10-15
**Author:** Comprehensive Edge Case Test Suite
**Version:** 1.0
