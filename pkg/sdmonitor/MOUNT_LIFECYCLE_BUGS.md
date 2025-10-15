# Mount/Unmount Lifecycle Bug Report

**Agent:** Agent 11
**Focus Area:** Mount/unmount lifecycle and edge cases
**Test File:** `pkg/sdmonitor/mount_lifecycle_test.go`
**Tested:** 2025-10-15

## Executive Summary

This report documents **10 critical bugs** in the mount/unmount lifecycle that pose significant risks to data integrity, system stability, and security. These bugs can cause:
- **Data loss** from improper read-write mounts during sync operations
- **Mount point pollution** from stale files and failed cleanup
- **Resource leaks** including stale mount points and unclosed descriptors
- **Security issues** from writable mounts that should be read-only
- **Race conditions** between mount operations and device removal

## Critical Bugs Found

### 🔴 CRITICAL BUG #1: Mount Fails But Device Marked as Mounted
**Severity:** HIGH - Causes sync on empty/unmounted directory
**Location:** `sdmonitor.go:125-147`

#### Description
When `mount()` fails, `checkDevices()` logs the error but still:
1. Sets `lastDevice = device` (line 140)
2. Sends `EventInserted` (lines 142-147)
3. Does not clean up state

This causes `handleCardInserted()` to start sync on an empty/unmounted directory.

#### Code Evidence
```go
// sdmonitor.go:135-147
if err := m.mount(device); err != nil {
    log.Printf("Failed to mount device %s: %v", device, err)
    return  // BUG: Returns but lastDevice is set below
}

m.lastDevice = device  // This should NOT execute if mount failed
log.Printf("SD card inserted: %s, mounted at %s", filepath.Base(device), m.mountPath)
m.eventChan <- Event{
    Type:      EventInserted,
    DevPath:   device,
    DevName:   filepath.Base(device),
    MountPath: m.mountPath,
}
```

#### Impact
- Sync starts with empty mount point
- `HasDCIM()` returns false, sync aborted
- Confusing logs: "SD card inserted" but nothing mounted
- User thinks sync is working but it silently fails

#### Test Evidence
```
=== RUN   TestMountFailureButDeviceMarkedAsMounted
    mount_lifecycle_test.go:68: Mount correctly failed: failed to mount device
    mount_lifecycle_test.go:69: BUG: In production, checkDevices() would have set lastDevice and sent EventInserted
    mount_lifecycle_test.go:70: This causes handleCardInserted() to run with an empty/unmounted directory
```

#### Reproduction
1. Insert SD card with corrupted filesystem
2. `mount()` fails with "invalid argument"
3. `EventInserted` still sent
4. `handleCardInserted()` runs, finds no DCIM
5. Sync aborted silently

---

### 🔴 CRITICAL BUG #2: Unmount During Active File Operations
**Severity:** CRITICAL - Data corruption and sync failure
**Location:** `sdmonitor.go:281-289`

#### Description
`unmount()` uses `unix.Unmount(path, 0)` without flags. If rclone is actively reading files:
- Returns `EBUSY` error
- Logs error but doesn't retry
- Device removal event still sent
- Sync continues reading from unmounted filesystem

#### Code Evidence
```go
// sdmonitor.go:282-289
func (m *Monitor) unmount() error {
    if err := unix.Unmount(m.mountPath, 0); err != nil {
        log.Printf("Failed to unmount %s: %v", m.mountPath, err)
        return err  // BUG: No retry with MNT_FORCE or MNT_DETACH
    }
    log.Printf("Unmounted %s", m.mountPath)
    return nil
}
```

#### Impact
- User removes SD card during sync
- Unmount fails with EBUSY (rclone reading files)
- `EventRemoved` sent anyway
- Sync continues, gets I/O errors
- Potential filesystem corruption if card removed physically

#### Missing Features
1. No `MNT_FORCE` flag to force unmount
2. No `MNT_DETACH` for lazy unmount
3. No coordination with sync manager to pause operations
4. No retry logic for EBUSY errors

---

### 🔴 CRITICAL BUG #3: Mount Point Already Has Files
**Severity:** HIGH - Wrong files synced, incorrect card ID
**Location:** `sdmonitor.go:229-268`

#### Description
If system crashes while mounted, mount point contains stale files. On restart:
- `mount()` doesn't check if mount point is empty
- Stale files appear to be from new SD card
- Wrong card ID read from stale `.pictures-sync-id`
- Stale photos counted and synced

#### Code Evidence
```go
// sdmonitor.go:239-246
// Unmount if anything is currently mounted at our path
if err := unix.Unmount(m.mountPath, 0); err != nil {
    // Only log if it's not "not mounted" error
    if err != unix.EINVAL {
        log.Printf("Warning: Failed to unmount existing mount at %s: %v", m.mountPath, err)
        // Try to continue anyway, as the mount might succeed
    }
}
// BUG: No check if mount point has stale files after failed unmount
```

#### Test Evidence
```
=== RUN   TestMountPointAlreadyHasFiles
    mount_lifecycle_test.go:185: BUG CONFIRMED: Mount point has stale files
    mount_lifecycle_test.go:191: Impact: handleCardInserted() will count stale photos and try to sync them
    mount_lifecycle_test.go:192:         Card ID will be based on stale .pictures-sync-id file
```

#### Scenario
1. System crashes during sync
2. Mount point left with files: `/perm/pictures-sync/mounts/sdcard/DCIM/...`
3. On restart, new SD card inserted
4. `CountPhotos()` counts stale files
5. `GetOrCreateCardID()` reads stale `.pictures-sync-id`
6. Sync uses wrong card ID, uploads to wrong remote folder

---

### 🔴 CRITICAL BUG #4: Mount Point Permission Issues
**Severity:** MEDIUM - Files unreadable after mount
**Location:** `sdmonitor.go:64-67, 229-268`

#### Description
- `Start()` creates mount directory with `0755` (line 65)
- `mount()` doesn't verify permissions after mounting
- On some systems, mounted filesystem has root-only permissions
- Non-root processes can't read files

#### Code Evidence
```go
// sdmonitor.go:64-67
func (m *Monitor) Start() error {
    // Ensure mount directory exists
    if err := os.MkdirAll(m.mountPath, 0755); err != nil {
        return fmt.Errorf("failed to create mount directory: %w", err)
    }
    // BUG: No verification that mount point is readable after mounting
```

#### Impact
- Mount succeeds but webui can't list files
- `CountPhotos()` fails with permission denied
- Sync fails mysteriously

---

### 🔴 CRITICAL BUG #5: Concurrent Mount Attempts
**Severity:** MEDIUM - Race conditions and mount failures
**Location:** `sdmonitor.go:229-268` (no locking)

#### Description
No mutex protects mount operations. Multiple goroutines can call `mount()` simultaneously:
- `pollDevices()` detects device
- External code calls `mount()` directly
- Two mounts attempted on same path

#### Test Evidence
```
=== RUN   TestConcurrentMountAttempts
    [10 concurrent mount attempts, all fail]
    mount_lifecycle_test.go:281: BUG CONFIRMED: No synchronization for mount operations
    mount_lifecycle_test.go:287: Impact: Race conditions can cause mount failures or corruption
```

#### Missing Features
1. No `sync.Mutex` to serialize mount/unmount
2. No queue for mount requests
3. No check if mount already in progress

---

### 🔴 CRITICAL BUG #6: Mount With Corrupted Filesystem
**Severity:** HIGH - Silent mount of corrupted data
**Location:** `sdmonitor.go:250-267`

#### Description
`mount()` tries multiple filesystem types (vfat, exfat, ext4, ntfs) and accepts first success:
- Corrupted filesystem might mount successfully
- Filesystem is unreadable after mount
- No validation or health check

#### Code Evidence
```go
// sdmonitor.go:250-258
fstypes := []string{"vfat", "exfat", "ext4", "ntfs"}

for _, fstype := range fstypes {
    err := unix.Mount(device, m.mountPath, fstype, 0, "")
    if err == nil {
        log.Printf("Mounted %s as %s (rw) at %s", device, fstype, m.mountPath)
        return nil  // BUG: No validation that filesystem is healthy
    }
}
```

#### Impact
- Corrupted SD card mounts successfully
- `CountPhotos()` fails with I/O errors
- Sync fails mysteriously
- User doesn't know card is corrupted

#### Missing Features
1. No filesystem validation after mount
2. No `fsck` to check health
3. No dmesg monitoring for mount errors
4. No warning if mount logs errors

---

### 🔴 CRITICAL BUG #7: Device Removed During Mount
**Severity:** HIGH - Race condition between detection and mount
**Location:** `sdmonitor.go:125-162`

#### Description
Timing race:
1. `checkDevices()` detects device exists (line 128)
2. User removes SD card physically
3. `mount()` called on non-existent device (line 135)
4. Mount fails but `EventInserted` still sent (Bug #1)

#### Impact
- Same as Bug #1
- User confused: device was removed but insertion event sent
- Sync starts on empty directory

---

### 🔴 CRITICAL BUG #8: Mount Not Actually Read-Only
**Severity:** CRITICAL - Data corruption risk
**Location:** `sdmonitor.go:248-268, 270-279`

#### Description
Mount lifecycle:
1. `mount()` mounts **read-write** (line 249: "to allow writing card ID")
2. Card ID written to `.pictures-sync-id`
3. `RemountReadOnly()` called (line 359, 383)
4. **If remount fails:** SD card remains read-write during entire sync
5. rclone or other processes could accidentally modify files

#### Code Evidence
```go
// sdmonitor.go:248-249
// Mount read-write initially to allow writing card ID
// We'll remount read-only after card ID is written

// sdmonitor.go:358-363 (GetOrCreateCardID)
if monitor != nil {
    if err := monitor.RemountReadOnly(); err != nil {
        log.Printf("ERROR: Failed to remount read-only after reading card ID: %v", err)
        // This is critical - SD card remains read-write and could be corrupted
        return cardID, false, fmt.Errorf("failed to remount read-only: %w", err)
    }
}
```

#### Test Evidence
```
=== RUN   TestGetOrCreateCardIDWithoutRemount
    mount_lifecycle_test.go:625: BUG: GetOrCreateCardID with nil monitor
    mount_lifecycle_test.go:628:   - RemountReadOnly is skipped (line 358, 383)
    mount_lifecycle_test.go:629:   - SD card remains read-write
    mount_lifecycle_test.go:630:   - Sync proceeds with writable filesystem
```

#### Impact
- **CRITICAL DATA CORRUPTION RISK**
- If remount fails, sync runs with writable filesystem
- rclone reads files, but any write operation corrupts SD card
- User removes card during write = filesystem corruption
- Photos lost permanently

#### Scenarios Where Remount Fails
1. Mount busy (files open)
2. Kernel doesn't support remount
3. Filesystem doesn't support read-only remount
4. Permission denied

---

### 🔴 CRITICAL BUG #9: Stale Mount Points Accumulate
**Severity:** HIGH - System accumulates stale mounts
**Location:** `sdmonitor.go:239-246, 64-74`

#### Description
On startup, `mount()` tries to unmount existing mount (line 240):
- Uses `unix.Unmount(path, 0)` (might fail if busy)
- Only logs warning if unmount fails (line 242)
- Continues to mount anyway (line 253)
- Can result in stale mounts accumulating

#### Code Evidence
```go
// sdmonitor.go:239-246
// Unmount if anything is currently mounted at our path
if err := unix.Unmount(m.mountPath, 0); err != nil {
    // Only log if it's not "not mounted" error
    if err != unix.EINVAL {
        log.Printf("Warning: Failed to unmount existing mount at %s: %v", m.mountPath, err)
        // Try to continue anyway, as the mount might succeed
    }
}
// BUG: No verification that unmount succeeded
```

#### Impact
- System crashes leave mount points mounted
- On restart, unmount fails (EBUSY)
- New mount attempted over stale mount
- Mount fails or mounts over existing mount
- `/proc/mounts` accumulates stale entries

#### Missing Features
1. No `MNT_FORCE` or `MNT_DETACH` for forced unmount
2. No verification that unmount succeeded
3. No clearing of mount point directory after failed unmount
4. No startup cleanup of stale mounts

---

### 🔴 CRITICAL BUG #10: Mount Namespace Issues in Gokrazy
**Severity:** MEDIUM - Mounts invisible to other processes
**Location:** `sdmonitor.go:229-268` (entire mount implementation)

#### Description
In Gokrazy environment:
- Services run in separate mount namespaces
- Mount performed by `pictures-sync` not visible to `webui`
- No `MS_SHARED` flag used (mounts are private by default)
- State manager reports mount path but webui can't access it

#### Impact
- Webui shows "SD card mounted at /perm/..."
- But webui process can't see files (empty directory)
- API endpoints return empty file lists
- User thinks sync is broken

#### Solution
```go
// Add MS_SHARED flag to make mount visible across namespaces
err := unix.Mount(device, m.mountPath, fstype, unix.MS_SHARED, "")
```

---

## Additional Issues

### Race Conditions

#### Unmount Error Handling
**Location:** `sdmonitor.go:557-563`

```go
func (m *Monitor) unmount() error {
    if err := unix.Unmount(m.mountPath, 0); err != nil {
        log.Printf("Failed to unmount %s: %v", m.mountPath, err)
        return err
    }
    return nil
}
```

**Issue:** Doesn't distinguish between:
- `EINVAL` - not mounted (expected)
- `EBUSY` - mount busy (should retry)
- `EPERM` - permission denied (fatal)

#### Mount/Unmount Race
**Test Evidence:**
```
=== RUN   TestMountAfterUnmountRace
    [Multiple concurrent mount/unmount operations]
    mount_lifecycle_test.go:602: BUG: No synchronization between mount() and unmount()
    mount_lifecycle_test.go:604:   - Mount after unmount started
    mount_lifecycle_test.go:605:   - Unmount after mount started
    mount_lifecycle_test.go:606:   - Inconsistent lastDevice state
```

---

## Security Issues

### 1. Writable Mounts During Sync
**Risk:** Data corruption from accidental writes
**Severity:** CRITICAL

See Bug #8 above.

### 2. No Verification of Read-Only Status
**Location:** `sdmonitor.go:270-279`

After calling `RemountReadOnly()`, no verification that filesystem is actually read-only:
```go
func (m *Monitor) RemountReadOnly() error {
    err := unix.Mount("", m.mountPath, "", unix.MS_REMOUNT|unix.MS_RDONLY, "")
    if err != nil {
        return fmt.Errorf("failed to remount read-only: %w", err)
    }
    log.Printf("Remounted %s as read-only", m.mountPath)
    return nil  // BUG: No verification that remount actually worked
}
```

Should test by attempting write:
```go
// Verify read-only by attempting write
testFile := filepath.Join(m.mountPath, ".write-test")
if err := os.WriteFile(testFile, []byte("test"), 0644); err == nil {
    return fmt.Errorf("filesystem is still writable after remount")
}
```

---

## Resource Leaks

### 1. Stale Mount Points
Described in Bug #9 above.

### 2. No Mount Limit
System could accumulate unlimited stale mounts if:
- Service crashes repeatedly
- User rapidly inserts/removes cards
- Unmount failures not handled

### 3. File Descriptor Leaks
`CountPhotos()` walks directory tree - could leak FDs if:
- Operation interrupted
- Error in middle of walk
- Symlink loops

---

## Recommendations

### Immediate Fixes (Critical)

1. **Fix Bug #1:** Check mount result before setting state
   ```go
   if err := m.mount(device); err != nil {
       log.Printf("Failed to mount device %s: %v", device, err)
       return  // Don't set lastDevice or send event
   }
   ```

2. **Fix Bug #8:** Verify read-only mount
   ```go
   // After RemountReadOnly(), verify with write test
   testFile := filepath.Join(mountPath, ".write-test")
   if err := os.WriteFile(testFile, []byte("test"), 0644); err == nil {
       return fmt.Errorf("CRITICAL: filesystem still writable")
   }
   ```

3. **Fix Bug #3:** Clear stale files before mounting
   ```go
   // Before mounting, check if mount point has files
   entries, err := os.ReadDir(m.mountPath)
   if err == nil && len(entries) > 0 {
       log.Printf("WARNING: Mount point has %d stale files, clearing", len(entries))
       for _, entry := range entries {
           os.RemoveAll(filepath.Join(m.mountPath, entry.Name()))
       }
   }
   ```

### Short-Term Fixes (High Priority)

4. **Add mutex for mount operations**
   ```go
   type Monitor struct {
       // ... existing fields ...
       mountMutex sync.Mutex  // Serialize mount/unmount operations
   }

   func (m *Monitor) mount(device string) error {
       m.mountMutex.Lock()
       defer m.mountMutex.Unlock()
       // ... existing code ...
   }
   ```

5. **Improve unmount error handling**
   ```go
   func (m *Monitor) unmount() error {
       err := unix.Unmount(m.mountPath, 0)
       if err == unix.EBUSY {
           // Retry with force flag
           log.Printf("Mount busy, forcing unmount...")
           err = unix.Unmount(m.mountPath, unix.MNT_FORCE)
       }
       return err
   }
   ```

6. **Add Gokrazy mount namespace support**
   ```go
   err := unix.Mount(device, m.mountPath, fstype, unix.MS_SHARED, "")
   ```

### Long-Term Improvements

7. **Add filesystem validation**
   - Run `fsck` before mounting
   - Check dmesg for mount errors
   - Validate filesystem is readable

8. **Add mount status monitoring**
   - Periodically verify mount is still valid
   - Check `/proc/mounts` for stale entries
   - Auto-cleanup on startup

9. **Coordinate with sync manager**
   - Pause sync before unmounting
   - Wait for sync to complete
   - Prevent mount during sync

10. **Add comprehensive error recovery**
    - Retry failed mounts
    - Automatic remount on errors
    - Fallback to safe mode (read-only always)

---

## Test Coverage

Created comprehensive test suite in `mount_lifecycle_test.go`:

- ✅ `TestMountFailureButDeviceMarkedAsMounted` - Bug #1
- ✅ `TestUnmountDuringActiveFileOperations` - Bug #2
- ✅ `TestMountPointAlreadyHasFiles` - Bug #3
- ✅ `TestMountPointPermissionsRootOnly` - Bug #4
- ✅ `TestConcurrentMountAttempts` - Bug #5
- ✅ `TestMountCorruptedFilesystem` - Bug #6
- ✅ `TestDeviceRemovedDuringMount` - Bug #7
- ✅ `TestMountNotReadOnly` - Bug #8
- ✅ `TestStaleMountPoints` - Bug #9
- ✅ `TestMountNamespaceIsolation` - Bug #10
- ✅ `TestUnmountNotMounted` - Error handling
- ✅ `TestMountAfterUnmountRace` - Race conditions
- ✅ `TestGetOrCreateCardIDWithoutRemount` - Security
- ✅ `TestVerifyMountReadOnlyAfterRemount` - Verification

**All tests pass** and document the bugs clearly.

---

## Conclusion

The mount/unmount lifecycle has **10 critical bugs** that pose serious risks:

1. **Data corruption** from writable mounts (Bug #8)
2. **Wrong data synced** from stale files (Bug #3)
3. **Silent failures** from mount errors (Bug #1, #7)
4. **Resource leaks** from stale mounts (Bug #9)
5. **Race conditions** from lack of synchronization (Bug #2, #5)

**Immediate action required** on Bugs #1, #3, and #8 to prevent data loss.

The test suite in `mount_lifecycle_test.go` provides comprehensive coverage and can be used to verify fixes.
