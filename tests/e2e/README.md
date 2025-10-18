# End-to-End Test Suite

Comprehensive end-to-end tests for the Photo Backup Station that validate the entire system from SD card detection through sync completion.

## Test Categories

### 1. Sync Workflow Tests (`sync_workflow_test.go`)

Tests complete photo sync workflows from end to end:

- **TestE2EFullSyncWorkflow**: Complete workflow from SD card insertion to sync completion
  - Simulates card insertion
  - Tracks state transitions (detected → syncing → success)
  - Verifies sync history is recorded
  - Validates event emission sequence

- **TestE2EReformatDetection**: Card reformat detection and handling
  - Syncs card with 100 files
  - Simulates reformatting (reducing to 20 files)
  - Verifies new card ID is created when below threshold

- **TestE2ECardRemovalDuringSync**: Handling card removal during active sync
  - Starts sync operation
  - Removes card mid-sync
  - Verifies graceful cancellation and error recording

- **TestE2EMultipleCardsSequential**: Sequential syncing of multiple cards
  - Tests 3+ different cards in sequence
  - Verifies each sync completes successfully
  - Validates history tracks all cards

- **TestE2EEmptyCard**: Handling cards with no photos
  - Verifies EventNoPhotosFound is emitted
  - Ensures no sync history is created
  - System returns to idle state

### 2. Service Integration Tests (`service_integration_test.go`)

Tests integration between webui and pictures-sync services:

- **TestE2EServiceIntegration**: Full integration between daemon and WebUI
  - Starts both services
  - Triggers SD card detection
  - Verifies WebUI reflects daemon state
  - Tests API endpoints during sync

- **TestE2EWebUIStateReflection**: WebUI accurately reflects daemon state
  - Tests various state transitions
  - Verifies API returns correct data
  - Validates real-time updates

- **TestE2EConcurrentAPIRequests**: Thread safety of API under load
  - 20 concurrent clients
  - 10 requests per client
  - Verifies no errors or corruption

- **TestE2ESettingsPersistence**: Settings persist across service restarts
  - Modifies settings
  - Restarts service
  - Verifies settings retained

- **TestE2EDaemonStartupSequence**: Daemon initialization
  - Tests startup sequence
  - Verifies clean shutdown
  - Validates initialization events

- **TestE2ECardDetectionLatency**: Measures SD card detection speed
  - Times from mount to detection event
  - Ensures detection under 5 seconds

### 3. Hardware Mock Tests (`hardware_mock_test.go`)

Tests mock hardware scenarios for SD card operations:

- **TestE2EHardwareMockSDCardCycle**: Full insertion/removal cycle
  - Insert → Remove → Re-insert
  - Verifies all events are detected

- **TestE2EHardwareMockMultipleDevices**: Multiple SD cards simultaneously
  - Creates 3+ mock cards
  - Mounts with staggered timing
  - Verifies all are detected

- **TestE2EHardwareMockRapidInsertionRemoval**: Rapid card swapping
  - 5 rapid insert/remove cycles
  - Tests system stability
  - Verifies event handling

- **TestE2EHardwareMockCardWithDifferentFormats**: Various filesystem layouts
  - Standard DCIM layout
  - Multiple DCIM subdirectories
  - No DCIM directory
  - Empty DCIM
  - Mixed photo and non-photo files

- **TestE2EHardwareMockCardIDPersistence**: Card ID file persistence
  - First read creates ID
  - Second read retrieves same ID
  - Verifies file creation

- **TestE2EHardwareMockLargeCard**: Performance with 1000+ files
  - Tests mount and counting speed
  - Ensures reasonable performance

- **TestE2EHardwareMockCorruptedCard**: Corrupted filesystem handling
  - DCIM as file instead of directory
  - Verifies graceful error handling

- **TestE2EHardwareMockReadOnlyMount**: Read-only mounted cards
  - Tests read operations work
  - Verifies card ID creation handling

### 4. WebSocket E2E Tests (`websocket_e2e_test.go`)

Tests WebSocket communication end-to-end:

- **TestE2EWebSocketRealTimeUpdates**: Real-time state updates via WebSocket
  - Connects WebSocket client
  - Triggers state changes
  - Verifies updates received in real-time

- **TestE2EWebSocketMultipleClients**: Multiple concurrent WebSocket clients
  - 10 simultaneous connections
  - Broadcast to all clients
  - Verifies all receive updates

- **TestE2EWebSocketReconnection**: Client reconnection handling
  - Connect, disconnect, reconnect
  - Verifies state is sent to new connections

- **TestE2EWebSocketEventStream**: Event streaming
  - Tests various event types
  - Verifies events flow through WebSocket

- **TestE2EWebSocketSyncProgressUpdates**: Real-time sync progress
  - Simulates sync with progress updates
  - Tracks progress values
  - Verifies monotonic increase

- **TestE2EWebSocketTokenValidation**: WebSocket security
  - Tests connection without token (should fail)
  - Tests invalid token (should fail)
  - Tests valid token (should succeed)
  - Tests token reuse (should fail)

- **TestE2EWebSocketConcurrentUpdates**: Concurrent state updates
  - 50 concurrent state changes
  - Verifies no message loss
  - Tests thread safety

- **TestE2EWebSocketLongRunningConnection**: Connection stability
  - 30-second long-running connection
  - Periodic updates throughout
  - Verifies no errors or disconnects

- **TestE2EWebSocketIntegrationWithSync**: WebSocket during real sync
  - Performs actual sync operation
  - Tracks lifecycle via WebSocket
  - Verifies complete state sequence

### 5. Error Recovery Tests (`error_recovery_test.go`)

Tests system recovery from various error scenarios:

- **TestE2ERecoveryFromSyncFailure**: Recovery after sync failure
  - Triggers sync failure
  - Verifies error state
  - Tests recovery with valid sync

- **TestE2ERecoveryFromCardRemovalDuringSync**: Mid-sync card removal
  - Removes card during active sync
  - Verifies cancellation
  - Tests new card after interruption

- **TestE2ERecoveryFromCorruptedState**: Corrupted state file recovery
  - Creates invalid JSON in state file
  - Verifies graceful initialization
  - Tests normal operations resume

- **TestE2ERecoveryFromDiskFull**: Disk full scenario handling
  - Simulates "no space left on device"
  - Verifies error recording
  - Tests recovery for next sync

- **TestE2ERecoveryFromNetworkFailure**: Network error recovery
  - Tests various network errors
  - Verifies error recording
  - Ensures system remains operational

- **TestE2ERecoveryFromRapidCardSwapping**: Rapid card swap handling
  - 5 rapid insert/remove cycles
  - Verifies system stability
  - Tests normal operation after

- **TestE2ERecoveryFromConcurrentOperations**: Concurrent operation handling
  - 10 concurrent state changes
  - Verifies no panics or corruption
  - Tests thread safety

- **TestE2ERecoveryFromPermissionErrors**: Permission error handling
  - Creates read-only filesystem
  - Verifies graceful error handling
  - Tests recovery after fix

- **TestE2ERecoveryAfterPowerFailure**: Simulated power failure
  - Starts sync
  - Drops state manager (simulates crash)
  - Verifies state recovery
  - Tests resume operations

### 6. Configuration Persistence Tests (`config_persistence_test.go`)

Tests configuration and state persistence across restarts:

- **TestE2ESettingsPersistence**: Settings persist across restarts
  - Modifies all settings
  - Restarts (new settings instance)
  - Verifies all values retained

- **TestE2EHistoryPersistence**: Sync history persistence
  - Performs 4 syncs
  - Restarts state manager
  - Verifies all history intact

- **TestE2ERcloneConfigPersistence**: Rclone config file persistence
  - Creates rclone configuration
  - Verifies file permissions
  - Tests reading after restart

- **TestE2EWiFiConfigPersistence**: WiFi network persistence
  - Adds 3 WiFi networks
  - Restarts WiFi manager
  - Verifies all networks retained

- **TestE2EStatePersistenceAcrossMultipleRestarts**: Multiple restart cycles
  - Performs 5 restart cycles
  - Modifies state in each cycle
  - Verifies consistency throughout

- **TestE2EConfigurationMigration**: Config format migration
  - Creates old format config
  - Loads with new code
  - Verifies migration and defaults

- **TestE2EAtomicWrites**: Atomic state writes
  - Performs 100 rapid state changes
  - Verifies no corruption
  - Checks no temp files left behind

- **TestE2EConfigBackupAndRestore**: Backup/restore functionality
  - Creates config backup
  - Modifies config
  - Restores from backup
  - Verifies restoration

- **TestE2EStateConsistencyAfterCrash**: State consistency after crash
  - Starts sync
  - Simulates crash (no cleanup)
  - Verifies state recovery
  - Tests resume operations

## Running Tests

### Run all E2E tests:
```bash
cd tests/e2e
go test -v
```

### Run specific test category:
```bash
go test -v -run TestE2EWebSocket  # All WebSocket tests
go test -v -run TestE2ERecovery   # All recovery tests
go test -v -run TestE2EHardware   # All hardware mock tests
```

### Run single test:
```bash
go test -v -run TestE2EFullSyncWorkflow
```

### Run with integration tests (requires more setup):
```bash
E2E_INTEGRATION=1 go test -v
```

### Run excluding long tests:
```bash
go test -v -short
```

### Run with race detection:
```bash
go test -v -race
```

### Run with coverage:
```bash
go test -v -coverprofile=coverage.out
go tool cover -html=coverage.out
```

## Test Environment

Tests create isolated temporary environments with:

- Temporary base directory for all test data
- Mock SD card mount points
- Mock rclone configuration
- Isolated state and settings files
- Automatic cleanup on test completion

### TestEnvironment Structure:
```go
type TestEnvironment struct {
    BaseDir          string  // Base temporary directory
    MountDir         string  // Mock SD card mount directory
    RcloneConfigPath string  // Path to mock rclone config
    MockCards        []*MockCard
}
```

### MockCard Structure:
```go
type MockCard struct {
    CardID    string  // Unique card identifier
    DevName   string  // Device name (e.g., "mock-card-001")
    MountPath string  // Full path to mount point
    NumFiles  int     // Number of mock photo files
}
```

## Test Helpers

### setupTestEnvironment(t *testing.T)
Creates a complete test environment with temporary directories and mock configurations.

### setupIntegrationEnvironment(t *testing.T)
Creates an integration test environment with full service stack (state manager, sync manager, settings).

### CreateMockSDCard(cardID string, numFiles int)
Creates a mock SD card with specified number of photo files.

### MountMockCard(card *MockCard)
Creates filesystem structure for a mock card (DCIM directory, photos, card ID file).

### UnmountMockCard(card *MockCard)
Removes mock card filesystem.

### ReformatMockCard(card *MockCard, newFileCount int)
Simulates reformatting by removing files and card ID.

### waitForSyncCompletion(t, stateMgr, timeout)
Waits for a sync to complete (success, error, or idle) with timeout.

## Test Coverage

The E2E test suite covers:

- ✅ Complete sync workflows from detection to completion
- ✅ Service integration between daemon and WebUI
- ✅ WebSocket real-time communication
- ✅ SD card detection and monitoring
- ✅ Card ID creation and persistence
- ✅ Reformat detection
- ✅ Card removal during sync
- ✅ Multiple card handling
- ✅ Various filesystem layouts
- ✅ Error scenarios and recovery
- ✅ Configuration persistence
- ✅ State persistence across restarts
- ✅ Concurrent operations
- ✅ Security (WebSocket tokens, permissions)
- ✅ Performance (large cards, rapid operations)
- ✅ Crash recovery

## Integration with CI/CD

To integrate with CI/CD pipelines:

```yaml
# Example GitHub Actions
- name: Run E2E Tests
  run: |
    cd tests/e2e
    go test -v -race -coverprofile=coverage.out

- name: Run Integration Tests
  run: |
    cd tests/e2e
    E2E_INTEGRATION=1 go test -v -timeout 10m
```

## Test Data Cleanup

All tests automatically clean up test data via:
- `defer testEnv.Cleanup()` in each test
- Temporary directories are removed
- No persistent state between test runs

## Debugging Tests

### Enable verbose logging:
```bash
go test -v -run TestName 2>&1 | tee test.log
```

### Check test artifacts:
Some tests may leave debug info in `/tmp/e2e-test-*` directories if cleanup fails.

### Use debugger:
```bash
dlv test -- -test.run TestE2EFullSyncWorkflow
```

## Known Limitations

1. **Hardware Tests**: Mock hardware tests simulate SD card operations but don't test actual hardware detection
2. **Network Tests**: Network error tests use simulated errors, not real network conditions
3. **Timing**: Some tests use sleep() for synchronization; actual timing may vary
4. **Rclone**: Tests use mock rclone configs; actual rclone execution is not tested in all cases

## Future Enhancements

- [ ] Add performance benchmarks
- [ ] Add stress tests with many concurrent cards
- [ ] Add tests for actual rclone execution
- [ ] Add tests for LED controller
- [ ] Add tests for captive portal
- [ ] Add visual regression tests for web UI
- [ ] Add API contract tests
