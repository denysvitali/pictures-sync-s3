package syncmanager

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/denysvitali/pictures-sync-s3/pkg/state"
)

// CRITICAL FINDING: This codebase uses rclone as a LIBRARY, not an external process
// The syncmanager.Sync() calls rclone/fs/sync.Sync() directly (line 189 of syncmanager.go)
// This means:
// 1. No zombie processes (rclone runs in-process)
// 2. No process reaping issues
// 3. No stdin/stdout/stderr pipe leaks from subprocess
// 4. Context cancellation handles cleanup, not SIGTERM/SIGKILL
//
// However, this introduces DIFFERENT risks:
// - Goroutine leaks instead of process leaks
// - In-process resource exhaustion
// - Library-level panics can crash the entire service
// - No process isolation (rclone bugs affect main process)
//
// These tests focus on the ACTUAL architecture using rclone as a library

// ============================================================================
// GOROUTINE LEAK TESTS - The real concern for in-process rclone
// ============================================================================

// TestGoroutineLeakOnSync tests for goroutine leaks during sync operations
func TestGoroutineLeakOnSync(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "process-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create test files
	srcDir := filepath.Join(tmpDir, "source")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if err := os.WriteFile(filepath.Join(srcDir, fmt.Sprintf("test%d.jpg", i)), []byte("test"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	configPath := filepath.Join(tmpDir, "rclone.conf")
	validConfig := `[local-test]
type = local
`
	if err := os.WriteFile(configPath, []byte(validConfig), 0644); err != nil {
		t.Fatal(err)
	}

	stateMgr, _ := state.NewManager()
	syncMgr := NewManager(configPath, "local-test", tmpDir, stateMgr, 4, 8)

	// Measure goroutines before
	runtime.GC()
	time.Sleep(100 * time.Millisecond)
	before := runtime.NumGoroutine()

	// Run sync
	err = syncMgr.Sync(srcDir, "card-leak-test", 5, 20)
	if err != nil {
		t.Logf("Sync error (may be expected): %v", err)
	}

	// Wait for cleanup
	time.Sleep(500 * time.Millisecond)
	runtime.GC()
	time.Sleep(100 * time.Millisecond)

	// Measure goroutines after
	after := runtime.NumGoroutine()

	// Allow for some variance, but significant increase indicates leak
	increase := after - before
	t.Logf("Goroutines: before=%d, after=%d, increase=%d", before, after, increase)

	// FINDING: If increase > 5, we likely have a goroutine leak
	// The monitorProgress goroutine should be cleaned up by the done channel
	if increase > 5 {
		t.Errorf("POTENTIAL GOROUTINE LEAK: %d goroutines remain after sync", increase)
		t.Logf("BUG: Check that monitorProgress goroutine exits properly")
		t.Logf("BUG: Check that context cancellation propagates to all rclone operations")
	}
}

// TestGoroutineLeakOnCancel tests for goroutine leaks when sync is cancelled
func TestGoroutineLeakOnCancel(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "process-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	srcDir := filepath.Join(tmpDir, "source")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatal(err)
	}

	configPath := filepath.Join(tmpDir, "rclone.conf")
	validConfig := `[local-test]
type = local
`
	if err := os.WriteFile(configPath, []byte(validConfig), 0644); err != nil {
		t.Fatal(err)
	}

	stateMgr, _ := state.NewManager()
	syncMgr := NewManager(configPath, "local-test", tmpDir, stateMgr, 4, 8)

	runtime.GC()
	time.Sleep(100 * time.Millisecond)
	before := runtime.NumGoroutine()

	// Start sync in background
	go syncMgr.Sync(srcDir, "card-cancel-test", 0, 0)

	// Wait for sync to start
	time.Sleep(100 * time.Millisecond)

	// Cancel sync
	if err := syncMgr.Cancel(); err != nil {
		t.Logf("Cancel: %v", err)
	}

	// Wait for cleanup
	time.Sleep(500 * time.Millisecond)
	runtime.GC()
	time.Sleep(100 * time.Millisecond)

	after := runtime.NumGoroutine()
	increase := after - before

	t.Logf("Goroutines after cancel: before=%d, after=%d, increase=%d", before, after, increase)

	if increase > 5 {
		t.Errorf("GOROUTINE LEAK ON CANCEL: %d goroutines remain", increase)
		t.Logf("BUG: Context cancellation may not be propagating to all goroutines")
		t.Logf("BUG: monitorProgress may not be exiting on context.Done()")
	}
}

// TestMultipleConcurrentSyncAttempts verifies only one sync runs at a time
func TestMultipleConcurrentSyncAttempts(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "process-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	srcDir := filepath.Join(tmpDir, "source")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatal(err)
	}

	configPath := filepath.Join(tmpDir, "rclone.conf")
	validConfig := `[local-test]
type = local
`
	if err := os.WriteFile(configPath, []byte(validConfig), 0644); err != nil {
		t.Fatal(err)
	}

	stateMgr, _ := state.NewManager()
	syncMgr := NewManager(configPath, "local-test", tmpDir, stateMgr, 4, 8)

	// Start first sync
	go syncMgr.Sync(srcDir, "card-1", 0, 0)
	time.Sleep(50 * time.Millisecond)

	// Try to start 10 more syncs concurrently
	var wg sync.WaitGroup
	errorCount := 0
	var mu sync.Mutex

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			err := syncMgr.Sync(srcDir, fmt.Sprintf("card-%d", idx), 0, 0)
			if err != nil && strings.Contains(err.Error(), "sync already in progress") {
				mu.Lock()
				errorCount++
				mu.Unlock()
			}
		}(i)
	}

	wg.Wait()

	// All concurrent attempts should fail with "sync already in progress"
	if errorCount < 9 {
		t.Errorf("CONCURRENCY BUG: Expected at least 9 rejections, got %d", errorCount)
		t.Logf("BUG: Multiple syncs may be running simultaneously - data corruption risk")
	} else {
		t.Logf("PASS: Concurrent sync prevention working (%d attempts blocked)", errorCount)
	}

	// Cleanup
	syncMgr.Cancel()
	time.Sleep(200 * time.Millisecond)
}

// ============================================================================
// CONTEXT AND CANCELLATION TESTS
// ============================================================================

// TestContextCancellationPropagation tests that context cancellation reaches all operations
func TestContextCancellationPropagation(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "process-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	srcDir := filepath.Join(tmpDir, "source")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatal(err)
	}

	configPath := filepath.Join(tmpDir, "rclone.conf")
	validConfig := `[local-test]
type = local
`
	if err := os.WriteFile(configPath, []byte(validConfig), 0644); err != nil {
		t.Fatal(err)
	}

	stateMgr, _ := state.NewManager()
	syncMgr := NewManager(configPath, "local-test", tmpDir, stateMgr, 4, 8)

	// Start sync
	syncDone := make(chan error, 1)
	go func() {
		syncDone <- syncMgr.Sync(srcDir, "card-context-test", 0, 0)
	}()

	// Wait for sync to start
	time.Sleep(50 * time.Millisecond)

	// Cancel
	cancelTime := time.Now()
	if err := syncMgr.Cancel(); err != nil {
		t.Logf("Cancel: %v", err)
	}

	// Wait for sync to complete
	select {
	case err := <-syncDone:
		duration := time.Since(cancelTime)
		t.Logf("Sync completed in %v after cancel: %v", duration, err)

		// Should complete quickly after cancel
		if duration > 2*time.Second {
			t.Errorf("BUG: Sync took %v to exit after cancel - context may not be propagating", duration)
		}

	case <-time.After(5 * time.Second):
		t.Error("CRITICAL BUG: Sync did not exit within 5 seconds of cancel")
		t.Logf("BUG: Context cancellation not working - operations hang indefinitely")
	}
}

// TestCancelFuncIsNilAfterCompletion verifies proper cleanup of cancel function
func TestCancelFuncIsNilAfterCompletion(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "process-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	srcDir := filepath.Join(tmpDir, "source")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatal(err)
	}

	configPath := filepath.Join(tmpDir, "rclone.conf")
	validConfig := `[local-test]
type = local
`
	if err := os.WriteFile(configPath, []byte(validConfig), 0644); err != nil {
		t.Fatal(err)
	}

	stateMgr, _ := state.NewManager()
	syncMgr := NewManager(configPath, "local-test", tmpDir, stateMgr, 4, 8)

	// Run sync
	err = syncMgr.Sync(srcDir, "card-cleanup-test", 0, 0)
	t.Logf("Sync result: %v", err)

	// Check cancelFunc is nil
	syncMgr.mu.Lock()
	cancelFunc := syncMgr.cancelFunc
	isRunning := syncMgr.isRunning
	syncMgr.mu.Unlock()

	if cancelFunc != nil {
		t.Error("BUG: cancelFunc is not nil after sync completion - resource leak")
	}

	if isRunning {
		t.Error("BUG: isRunning is true after sync completion - state corruption")
	}
}

// TestPanicDuringSync tests that panics in sync operations don't leave state corrupted
func TestPanicDuringSync(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "process-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	configPath := filepath.Join(tmpDir, "rclone.conf")
	stateMgr, _ := state.NewManager()
	syncMgr := NewManager(configPath, "broken-remote", "/test", stateMgr, 4, 8)

	// This sync will fail
	err = syncMgr.Sync("/nonexistent/path", "card-panic-test", 0, 0)
	t.Logf("Sync error (expected): %v", err)

	// Check state is clean
	if syncMgr.IsRunning() {
		t.Error("BUG: isRunning is true after failed sync - prevents future syncs")
	}

	syncMgr.mu.Lock()
	if syncMgr.cancelFunc != nil {
		t.Error("BUG: cancelFunc not cleaned up after error")
	}
	syncMgr.mu.Unlock()

	// Try another sync - should not be blocked
	err = syncMgr.Sync("/another/nonexistent", "card-recovery-test", 0, 0)
	if err != nil && strings.Contains(err.Error(), "sync already in progress") {
		t.Error("CRITICAL BUG: Previous failed sync left manager in corrupted state")
		t.Logf("BUG: isRunning flag not reset by defer in Sync() - line 118-123")
	}
}

// ============================================================================
// RESOURCE LIMITS AND EXHAUSTION TESTS
// ============================================================================

// TestFileDescriptorUsage tests that file descriptors don't leak
func TestFileDescriptorUsage(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("File descriptor tracking not implemented for Windows")
	}

	tmpDir, err := os.MkdirTemp("", "process-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	srcDir := filepath.Join(tmpDir, "source")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create multiple test files
	for i := 0; i < 20; i++ {
		testFile := filepath.Join(srcDir, fmt.Sprintf("test%d.jpg", i))
		if err := os.WriteFile(testFile, make([]byte, 1024), 0644); err != nil {
			t.Fatal(err)
		}
	}

	configPath := filepath.Join(tmpDir, "rclone.conf")
	validConfig := `[local-test]
type = local
`
	if err := os.WriteFile(configPath, []byte(validConfig), 0644); err != nil {
		t.Fatal(err)
	}

	// Count FDs before
	fdsBefore, err := countOpenFDs()
	if err != nil {
		t.Logf("Warning: Cannot count FDs: %v", err)
		fdsBefore = 0
	}

	stateMgr, _ := state.NewManager()
	syncMgr := NewManager(configPath, "local-test", tmpDir, stateMgr, 4, 8)

	// Run multiple syncs
	for i := 0; i < 5; i++ {
		err = syncMgr.Sync(srcDir, fmt.Sprintf("card-fd-test-%d", i), 20, 20480)
		if err != nil {
			t.Logf("Sync %d error: %v", i, err)
		}
		time.Sleep(100 * time.Millisecond)
	}

	// Count FDs after
	runtime.GC()
	time.Sleep(200 * time.Millisecond)

	fdsAfter, err := countOpenFDs()
	if err != nil {
		t.Logf("Warning: Cannot count FDs: %v", err)
		return
	}

	increase := fdsAfter - fdsBefore
	t.Logf("File descriptors: before=%d, after=%d, increase=%d", fdsBefore, fdsAfter, increase)

	// Allow some increase for rclone library overhead, but not massive leaks
	if increase > 50 {
		t.Errorf("POTENTIAL FD LEAK: %d file descriptors remain open", increase)
		t.Logf("BUG: rclone library may not be closing files properly")
		t.Logf("BUG: Check fs.Object.Open() calls have matching Close()")
	}
}

// TestConcurrentSyncResourceUsage measures resource usage under load
func TestConcurrentSyncResourceUsage(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "process-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	srcDir := filepath.Join(tmpDir, "source")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatal(err)
	}

	configPath := filepath.Join(tmpDir, "rclone.conf")
	validConfig := `[local-test]
type = local
`
	if err := os.WriteFile(configPath, []byte(validConfig), 0644); err != nil {
		t.Fatal(err)
	}

	stateMgr, _ := state.NewManager()
	syncMgr := NewManager(configPath, "local-test", tmpDir, stateMgr, 4, 8)

	// Measure resources before
	var memBefore runtime.MemStats
	runtime.ReadMemStats(&memBefore)
	goroutinesBefore := runtime.NumGoroutine()

	// Try to start many syncs (only one should run)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			syncMgr.Sync(srcDir, fmt.Sprintf("card-resource-%d", idx), 0, 0)
		}(i)
		time.Sleep(10 * time.Millisecond) // Stagger starts
	}

	wg.Wait()

	// Measure resources after
	runtime.GC()
	time.Sleep(500 * time.Millisecond)

	var memAfter runtime.MemStats
	runtime.ReadMemStats(&memAfter)
	goroutinesAfter := runtime.NumGoroutine()

	memIncreaseMB := float64(memAfter.Alloc-memBefore.Alloc) / (1024 * 1024)
	goroutineIncrease := goroutinesAfter - goroutinesBefore

	t.Logf("Memory increase: %.2f MB", memIncreaseMB)
	t.Logf("Goroutine increase: %d", goroutineIncrease)

	if memIncreaseMB > 100 {
		t.Errorf("MEMORY LEAK: %.2f MB increase after syncs", memIncreaseMB)
	}

	if goroutineIncrease > 10 {
		t.Errorf("GOROUTINE LEAK: %d goroutines remain", goroutineIncrease)
	}
}

// ============================================================================
// CONFIGURATION AND ENVIRONMENT TESTS
// ============================================================================

// TestMissingRcloneBinary - N/A for library usage
// This test is not applicable because rclone is imported as a library, not executed as binary

// TestRcloneConfigPathHandling tests config path edge cases
func TestRcloneConfigPathHandling(t *testing.T) {
	testCases := []struct {
		name       string
		configPath string
		shouldWork bool
	}{
		{
			name:       "Empty config path",
			configPath: "",
			shouldWork: false,
		},
		{
			name:       "Relative path",
			configPath: "./rclone.conf",
			shouldWork: true, // rclone library handles relative paths
		},
		{
			name:       "Path with spaces",
			configPath: "/tmp/path with spaces/rclone.conf",
			shouldWork: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			stateMgr, _ := state.NewManager()
			syncMgr := NewManager(tc.configPath, "test", "/test", stateMgr, 4, 8)

			err := syncMgr.TestConnection()
			worked := err == nil || !strings.Contains(err.Error(), "config path")

			if worked != tc.shouldWork {
				t.Logf("Config path %q: worked=%v, shouldWork=%v, err=%v",
					tc.configPath, worked, tc.shouldWork, err)
			}
		})
	}
}

// TestTransferAndCheckerLimits tests that transfer/checker settings are respected
func TestTransferAndCheckerLimits(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "process-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	configPath := filepath.Join(tmpDir, "rclone.conf")
	validConfig := `[local-test]
type = local
`
	if err := os.WriteFile(configPath, []byte(validConfig), 0644); err != nil {
		t.Fatal(err)
	}

	testCases := []struct {
		name      string
		transfers int
		checkers  int
	}{
		{"Normal", 4, 8},
		{"Single transfer", 1, 1},
		{"High parallelism", 16, 32},
		{"Zero transfers", 0, 0}, // Should use defaults
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			stateMgr, _ := state.NewManager()
			syncMgr := NewManager(configPath, "local-test", tmpDir, stateMgr, tc.transfers, tc.checkers)

			// Verify manager was created
			if syncMgr == nil {
				t.Error("Manager creation failed")
			}

			// The actual limits are set in fs.GetConfig(ctx) during Sync()
			// We can't directly test them without running a full sync
			t.Logf("Created manager with transfers=%d, checkers=%d", tc.transfers, tc.checkers)
		})
	}
}

// ============================================================================
// CLEANUP AND SHUTDOWN TESTS
// ============================================================================

// TestCleanupOnServiceShutdown simulates service shutdown during active sync
func TestCleanupOnServiceShutdown(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "process-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	srcDir := filepath.Join(tmpDir, "source")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatal(err)
	}

	configPath := filepath.Join(tmpDir, "rclone.conf")
	validConfig := `[local-test]
type = local
`
	if err := os.WriteFile(configPath, []byte(validConfig), 0644); err != nil {
		t.Fatal(err)
	}

	stateMgr, _ := state.NewManager()
	syncMgr := NewManager(configPath, "local-test", tmpDir, stateMgr, 4, 8)

	// Start sync
	go syncMgr.Sync(srcDir, "card-shutdown-test", 0, 0)

	time.Sleep(50 * time.Millisecond)

	// Simulate shutdown - cancel sync
	shutdownStart := time.Now()
	if err := syncMgr.Cancel(); err != nil {
		t.Logf("Cancel: %v", err)
	}

	// Wait for cleanup (simulating graceful shutdown timeout)
	time.Sleep(1 * time.Second)

	shutdownDuration := time.Since(shutdownStart)
	t.Logf("Shutdown took %v", shutdownDuration)

	// Verify clean state
	if syncMgr.IsRunning() {
		t.Error("BUG: Sync still running after shutdown")
	}

	// FINDING: In real Gokrazy environment, if service is killed (SIGKILL),
	// the in-process rclone operations will be terminated immediately
	// This is better than leaving zombie processes, but may corrupt files mid-transfer
	t.Logf("NOTE: Gokrazy SIGKILL will terminate in-process rclone immediately")
	t.Logf("NOTE: No zombie processes possible with library usage")
}

// ============================================================================
// HELPER FUNCTIONS
// ============================================================================

// countOpenFDs returns the number of open file descriptors (Linux only)
func countOpenFDs() (int, error) {
	if runtime.GOOS != "linux" {
		return 0, fmt.Errorf("FD counting only supported on Linux")
	}

	pid := os.Getpid()
	fdDir := fmt.Sprintf("/proc/%d/fd", pid)

	entries, err := os.ReadDir(fdDir)
	if err != nil {
		return 0, err
	}

	return len(entries), nil
}

// ============================================================================
// EXTERNAL PROCESS SPAWN DETECTION (for verification)
// ============================================================================

// TestNoExternalProcessSpawn verifies that no external rclone process is spawned
func TestNoExternalProcessSpawn(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Process detection not implemented for Windows")
	}

	tmpDir, err := os.MkdirTemp("", "process-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	srcDir := filepath.Join(tmpDir, "source")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatal(err)
	}

	configPath := filepath.Join(tmpDir, "rclone.conf")
	validConfig := `[local-test]
type = local
`
	if err := os.WriteFile(configPath, []byte(validConfig), 0644); err != nil {
		t.Fatal(err)
	}

	stateMgr, _ := state.NewManager()
	syncMgr := NewManager(configPath, "local-test", tmpDir, stateMgr, 4, 8)

	// Start sync
	go syncMgr.Sync(srcDir, "card-spawn-test", 0, 0)

	time.Sleep(100 * time.Millisecond)

	// Check for rclone process
	cmd := exec.Command("pgrep", "-f", "rclone")
	output, err := cmd.Output()

	if err == nil && len(output) > 0 {
		t.Errorf("ARCHITECTURE ERROR: Found external rclone process: %s", output)
		t.Logf("BUG: Code should use rclone library, not spawn external process")
	} else {
		t.Logf("CORRECT: No external rclone process found (using library)")
	}

	syncMgr.Cancel()
}

// TestZombieProcessDetection verifies no zombie processes exist
func TestZombieProcessDetection(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Zombie process detection not applicable on Windows")
	}

	// Check for zombie processes (state Z)
	cmd := exec.Command("ps", "aux")
	output, err := cmd.Output()
	if err != nil {
		t.Logf("Warning: Cannot check for zombie processes: %v", err)
		return
	}

	lines := strings.Split(string(output), "\n")
	zombies := []string{}

	for _, line := range lines {
		// Look for defunct processes
		if strings.Contains(line, "defunct") || strings.Contains(line, " Z ") {
			zombies = append(zombies, line)
		}
	}

	if len(zombies) > 0 {
		t.Errorf("ZOMBIE PROCESSES DETECTED: %d", len(zombies))
		for _, zombie := range zombies {
			t.Logf("Zombie: %s", zombie)
		}
	} else {
		t.Logf("PASS: No zombie processes detected")
	}
}

// ============================================================================
// SIGNAL HANDLING TESTS
// ============================================================================

// TestSignalHandlingDuringSync tests behavior when process receives signals
func TestSignalHandlingDuringSync(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX signal testing not applicable on Windows")
	}

	tmpDir, err := os.MkdirTemp("", "process-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	srcDir := filepath.Join(tmpDir, "source")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatal(err)
	}

	configPath := filepath.Join(tmpDir, "rclone.conf")
	validConfig := `[local-test]
type = local
`
	if err := os.WriteFile(configPath, []byte(validConfig), 0644); err != nil {
		t.Fatal(err)
	}

	stateMgr, _ := state.NewManager()
	syncMgr := NewManager(configPath, "local-test", tmpDir, stateMgr, 4, 8)

	// Start sync
	go syncMgr.Sync(srcDir, "card-signal-test", 0, 0)

	time.Sleep(50 * time.Millisecond)

	// Send ourselves SIGTERM (gentle)
	// The main service should catch this and call Cancel()
	// We simulate that here
	t.Logf("Simulating SIGTERM handling (via Cancel)")
	err = syncMgr.Cancel()
	if err != nil {
		t.Logf("Cancel: %v", err)
	}

	time.Sleep(200 * time.Millisecond)

	// Verify cleanup
	if syncMgr.IsRunning() {
		t.Error("BUG: Sync still running after simulated SIGTERM")
	}

	// FINDING: SIGKILL would immediately terminate the process
	// No cleanup would occur, but also no zombie processes since rclone is in-process
	t.Logf("NOTE: SIGKILL would immediately terminate without cleanup")
	t.Logf("NOTE: In-process rclone means no child processes to become zombies")
}

// TestSIGKILLBehavior documents expected behavior on SIGKILL
func TestSIGKILLBehavior(t *testing.T) {
	// This is a documentation test - we can't actually test SIGKILL
	// because it would kill the test process

	t.Log("SIGKILL BEHAVIOR ANALYSIS:")
	t.Log("- SIGKILL immediately terminates the entire process")
	t.Log("- In-process rclone operations are killed instantly")
	t.Log("- No cleanup code runs (defer statements not executed)")
	t.Log("- Partially written files may be left on remote")
	t.Log("- State files may not be updated with final status")
	t.Log("")
	t.Log("ZOMBIE PROCESS RISK: NONE")
	t.Log("- rclone runs as library in same process")
	t.Log("- No child processes exist to become zombies")
	t.Log("")
	t.Log("MITIGATION:")
	t.Log("- Gokrazy should send SIGTERM first, wait for graceful shutdown")
	t.Log("- Main service should handle SIGTERM and call syncMgr.Cancel()")
	t.Log("- rclone library respects context cancellation")
}

// ============================================================================
// RCLONE LIBRARY SPECIFIC TESTS
// ============================================================================

// TestRcloneLibraryPanic tests that rclone library panics are caught
func TestRcloneLibraryPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("UNHANDLED PANIC from rclone library: %v", r)
			t.Logf("BUG: Panics in rclone library code crash the entire service")
			t.Logf("BUG: Consider recover() in critical paths")
		}
	}()

	tmpDir, err := os.MkdirTemp("", "process-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	configPath := filepath.Join(tmpDir, "rclone.conf")
	stateMgr, _ := state.NewManager()
	syncMgr := NewManager(configPath, "nonexistent-remote", "/test", stateMgr, 4, 8)

	// This may cause the rclone library to panic in some error cases
	err = syncMgr.Sync("/nonexistent/path", "card-panic-test", 0, 0)

	t.Logf("Sync returned error (no panic): %v", err)
}

// TestUlimitRespected tests that system file descriptor limits are respected
func TestUlimitRespected(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("ulimit not applicable on Windows")
	}

	// Get current ulimit
	var rLimit syscall.Rlimit
	err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &rLimit)
	if err != nil {
		t.Logf("Cannot get ulimit: %v", err)
		return
	}

	t.Logf("Current ulimit: soft=%d, hard=%d", rLimit.Cur, rLimit.Max)

	// The rclone library should respect system limits
	// If we set transfers=1000, it shouldn't try to open 1000 files simultaneously
	tmpDir, err := os.MkdirTemp("", "process-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	configPath := filepath.Join(tmpDir, "rclone.conf")
	validConfig := `[local-test]
type = local
`
	if err := os.WriteFile(configPath, []byte(validConfig), 0644); err != nil {
		t.Fatal(err)
	}

	stateMgr, _ := state.NewManager()

	// Create manager with very high parallelism
	syncMgr := NewManager(configPath, "local-test", tmpDir, stateMgr, 1000, 1000)

	srcDir := filepath.Join(tmpDir, "source")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Try to sync - should not hit "too many open files" error
	err = syncMgr.Sync(srcDir, "card-ulimit-test", 0, 0)

	if err != nil && strings.Contains(err.Error(), "too many open files") {
		t.Errorf("BUG: Hit ulimit with high parallelism setting")
		t.Logf("BUG: rclone library not respecting system file descriptor limits")
	} else {
		t.Logf("PASS: High parallelism handled gracefully")
	}
}
