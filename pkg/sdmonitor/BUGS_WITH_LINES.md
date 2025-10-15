# Bugs Found in sdmonitor Package - Detailed Line Numbers

## Critical Bugs with Exact Locations

### 1. Null Bytes in Card ID Not Sanitized
**File:** `/workspace/pictures-sync-s3/pkg/sdmonitor/sdmonitor.go`
**Line:** 354
**Function:** `GetOrCreateCardID()`

```go
353:    if data, err := os.ReadFile(idPath); err == nil {
354:        cardID := strings.TrimSpace(string(data))  // ← BUG: Doesn't remove null bytes
355:        if cardID != "" {
```

**Evidence:** Test `TestCorruptedCardIDFile/NullBytes` shows that card ID "card-\x00\x00\x00corrupted" is accepted as valid.

---

### 2. Unlimited Memory Read from Card ID File
**File:** `/workspace/pictures-sync-s3/pkg/sdmonitor/sdmonitor.go`
**Line:** 353
**Function:** `GetOrCreateCardID()`

```go
353:    if data, err := os.ReadFile(idPath); err == nil {  // ← BUG: No size limit
354:        cardID := strings.TrimSpace(string(data))
```

**Evidence:** Test `TestCorruptedCardIDFile/ExtremelyLong` loads a 10MB file into memory. On constrained Raspberry Pi, this could exhaust available RAM.

---

### 3. Race Condition - No File Locking on Card ID Writes
**File:** `/workspace/pictures-sync-s3/pkg/sdmonitor/sdmonitor.go`
**Lines:** 374 (GetOrCreateCardID), 414 (CreateNewCardID)
**Functions:** `GetOrCreateCardID()`, `CreateNewCardID()`

```go
374:    if err := os.WriteFile(idPath, []byte(newID+"\n"), 0644); err != nil {  // ← BUG: No lock
```

```go
414:    if err := os.WriteFile(idPath, []byte(newID+"\n"), 0644); err != nil {  // ← BUG: No lock
```

**Evidence:** Test `TestCardIDFileCorruptionRace` with 10 concurrent calls generated 10 different card IDs:
```
map[card-1ca1aa1acd7a3b1a:1 card-684918eb26c9006b:1 card-7ecc8a366c5c000f:1 ...]
```

---

### 4. Symlinks Counted as Separate Photos
**File:** `/workspace/pictures-sync-s3/pkg/sdmonitor/sdmonitor.go`
**Lines:** 308-328
**Function:** `CountPhotos()`

```go
308:    err := filepath.WalkDir(dcimPath, func(path string, d fs.DirEntry, err error) error {
309:        if err != nil {
310:            return err
311:        }
312:        if d.IsDir() {
313:            return nil  // ← BUG: Doesn't check for symlinks before this
314:        }
315:
316:        // Check if it's an image or video file
317:        ext := strings.ToLower(filepath.Ext(path))
318:        switch ext {
319:        case ".jpg", ".jpeg", ".png", ".gif", ".raw", ".cr2", ".nef", ".arw",
320:            ".mp4", ".mov", ".avi", ".mkv":
321:            count++  // ← BUG: Counts symlinks to photos as separate files
```

**Evidence:** Test `TestCountPhotosSymlinks` showed 1 real photo + 1 symlink = 2 counted (expected 1).

---

### 5. Concurrent Stop() Calls Cause Panic
**File:** `/workspace/pictures-sync-s3/pkg/sdmonitor/sdmonitor.go`
**Line:** 78
**Function:** `Stop()`

```go
76: func (m *Monitor) Stop() {
77:     close(m.stopChan)  // ← BUG: Panics if called multiple times
78:     // Unmount if mounted
79:     if m.lastDevice != "" {
80:         m.unmount()
81:     }
82: }
```

**Evidence:** Test `TestConcurrentStop` resulted in:
```
panic: close of closed channel
goroutine 55 [running]:
github.com/denysvitali/pictures-sync-s3/pkg/sdmonitor.(*Monitor).Stop(...)
    /workspace/pictures-sync-s3/pkg/sdmonitor/sdmonitor.go:78
```

---

## Medium Severity Bugs

### 6. CountPhotos Doesn't Validate DCIM is Directory
**File:** `/workspace/pictures-sync-s3/pkg/sdmonitor/sdmonitor.go`
**Lines:** 302-304
**Function:** `CountPhotos()`

```go
301: func CountPhotos(mountPath string) (int, int64, error) {
302:     dcimPath := filepath.Join(mountPath, "DCIM")
303:     // ← BUG: No validation that DCIM is actually a directory
304:     var count int
```

**Evidence:** Test `TestNoDCIMFolder` shows error "Should return error when DCIM is not a directory" - the function returns a generic WalkDir error instead of clear message.

---

### 7. Only One Device Tracked
**File:** `/workspace/pictures-sync-s3/pkg/sdmonitor/sdmonitor.go`
**Line:** 44
**Struct:** `Monitor`

```go
39: type Monitor struct {
40:     eventChan    chan Event
41:     stopChan     chan struct{}
42:     mountPath    string
43:     lastDevice   string  // ← BUG: Only tracks single device
```

**Lines:** 128-130
```go
128:    device := m.findUSBStorageDevice()  // ← BUG: Returns only first device
129:
130:    if device != "" && device != m.lastDevice {
```

---

### 8. Event Channel Buffer Overflow Blocks Polling
**File:** `/workspace/pictures-sync-s3/pkg/sdmonitor/sdmonitor.go`
**Lines:** 55, 142
**Functions:** `NewMonitor()`, `checkDevices()`

```go
54:     return &Monitor{
55:         eventChan:      make(chan Event, 10),  // ← BUG: Buffer of 10 can overflow
```

```go
142:        m.eventChan <- Event{  // ← BUG: Blocking send, no timeout
143:            Type:      EventInserted,
```

**Evidence:** Test `TestEventChannelBlocking` demonstrated that after 10 events, further sends block.

---

### 9. Card Removal During checkDevices() Causes Issues
**File:** `/workspace/pictures-sync-s3/pkg/sdmonitor/sdmonitor.go`
**Lines:** 128-147
**Function:** `checkDevices()`

```go
128:    device := m.findUSBStorageDevice()
129:
130:    if device != "" && device != m.lastDevice {
131:        // New device detected
132:        log.Printf("SD card detected: %s (lastDevice was: %s)", device, m.lastDevice)
133:
134:        // Try to mount it
135:        if err := m.mount(device); err != nil {  // ← Device could be removed before mount
136:            log.Printf("Failed to mount device %s: %v", device, err)
137:            return
138:        }
```

Actually reviewing this more carefully, the code handles this correctly with early return. But there's a related issue:
- If mount fails, `lastDevice` is not updated
- Next poll (2 seconds later) will try to mount the same device again
- Could spam mount attempts if device is flaky

---

## Low Severity Issues

### 10. No Validation of Disk Size
**File:** `/workspace/pictures-sync-s3/pkg/sdmonitor/sdmonitor.go`
**Lines:** 522-524
**Function:** `getDeviceInfo()`

```go
520:    if sizeErr == nil && len(sizeData) > 0 {
521:        // Size is in 512-byte sectors
522:        var sectors int64
523:        fmt.Sscanf(strings.TrimSpace(string(sizeData)), "%d", &sectors)
524:        info.Size = sectors * 512  // ← BUG: No validation that sectors > 0
525:        info.SizeHuman = formatBytes(info.Size)
```

---

### 11. Volume Label Not Sanitized
**File:** `/workspace/pictures-sync-s3/pkg/sdmonitor/sdmonitor.go`
**Lines:** 556-559
**Function:** `getDeviceInfo()`

```go
555:    // Get volume label using blkid
556:    cmd := exec.Command("blkid", "-s", "LABEL", "-o", "value", devicePath)
557:    if output, err := cmd.Output(); err == nil && len(output) > 0 {
558:        info.VolumeLabel = strings.TrimSpace(string(output))  // ← BUG: No sanitization
559:    }
```

Potential issues with control characters, newlines, null bytes in label.

---

## Additional Context Issues

### 12. No Timeout on CountPhotos
**File:** `/workspace/pictures-sync-s3/pkg/sdmonitor/sdmonitor.go`
**Lines:** 302-331
**Function:** `CountPhotos()`

The entire function has no timeout or cancellation mechanism. A card with millions of photos could hang the system.

---

### 13. RemountReadOnly Failure Handling
**File:** `/workspace/pictures-sync-s3/pkg/sdmonitor/sdmonitor.go`
**Lines:** 359-362 (in GetOrCreateCardID), 384-387 (in GetOrCreateCardID)
**Function:** `GetOrCreateCardID()`

```go
359:            if err := monitor.RemountReadOnly(); err != nil {
360:                log.Printf("ERROR: Failed to remount read-only after reading card ID: %v", err)
361:                // This is critical - SD card remains read-write and could be corrupted
362:                return cardID, false, fmt.Errorf("failed to remount read-only: %w", err)
```

The error is returned but the caller in `cmd/pictures-sync/main.go:145-149` might not abort the sync properly.

---

### 14. Mount Cache Uses time.Since
**File:** `/workspace/pictures-sync-s3/pkg/sdmonitor/sdmonitor.go`
**Line:** 110
**Function:** `getCachedMounts()`

```go
108: func (m *Monitor) getCachedMounts() (string, error) {
109:     // Check if cache is still valid
110:     if time.Since(m.mountsCacheTime) < m.mountsCacheTTL {  // ← Uses wall clock
111:         return m.cachedMounts, nil
112:     }
```

Actually, this is fine in modern Go as time.Now() includes monotonic clock component.

---

## Test Results Summary

```
TestCorruptedCardIDFile/NullBytes           FAIL - Null bytes not sanitized
TestCorruptedCardIDFile/ExtremelyLong       PASS - But loads 10MB into memory
TestCardIDFileCorruptionRace                FAIL - Generated 10 different IDs
TestCountPhotosSymlinks                     FAIL - Expected 1, got 2
TestConcurrentStop                          PANIC - close of closed channel
TestNoDCIMFolder                            FAIL - Error message unclear
```

## Lines Requiring Changes (Summary)

1. **sdmonitor.go:354** - Add validation for card ID content
2. **sdmonitor.go:353** - Add size limit to ReadFile
3. **sdmonitor.go:374, 414** - Add file locking
4. **sdmonitor.go:308-321** - Check for symlinks before counting
5. **sdmonitor.go:78** - Use sync.Once for Stop()
6. **sdmonitor.go:302-304** - Validate DCIM is directory
7. **sdmonitor.go:44, 128** - Consider supporting multiple devices
8. **sdmonitor.go:142** - Use non-blocking send or larger buffer
9. **sdmonitor.go:524** - Validate sectors > 0
10. **sdmonitor.go:558** - Sanitize volume label
11. **sdmonitor.go:302-331** - Add context/timeout to CountPhotos

Total lines requiring changes: ~15 locations across the file.
