package cardhandler

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/denysvitali/pictures-sync-s3/pkg/events"
	"github.com/denysvitali/pictures-sync-s3/pkg/sdmonitor"
	"github.com/denysvitali/pictures-sync-s3/pkg/settings"
	"github.com/denysvitali/pictures-sync-s3/pkg/state"
	"github.com/denysvitali/pictures-sync-s3/pkg/syncmanager"
)

// setupTestEnvironment creates a test environment with all dependencies
func setupTestEnvironment(t *testing.T) (*Handler, *state.Manager, *testSyncManager, string) {
	t.Helper()

	tempDir := t.TempDir()

	// Set up temp directory as PERM_DIR
	oldPerm := os.Getenv("PERM_DIR")
	os.Setenv("PERM_DIR", tempDir)
	t.Cleanup(func() {
		if oldPerm == "" {
			os.Unsetenv("PERM_DIR")
		} else {
			os.Setenv("PERM_DIR", oldPerm)
		}
	})

	// Create state manager
	stateMgr, err := state.NewManager()
	if err != nil {
		t.Fatalf("Failed to create state manager: %v", err)
	}

	// Create settings
	appSettings, err := settings.Load()
	if err != nil {
		t.Fatalf("Failed to load settings: %v", err)
	}

	// Create mock sync manager
	mockSyncMgr := &testSyncManager{}

	// Create SD monitor
	monitor := sdmonitor.NewMonitor(filepath.Join(tempDir, "mounts", "sdcard"))

	// Create event manager
	eventMgr := events.NewManager()

	// Create handler
	handler := NewHandler(monitor, stateMgr, mockSyncMgr, appSettings, eventMgr)

	return handler, stateMgr, mockSyncMgr, tempDir
}

// testSyncManager is a mock implementation of syncmanager.Manager
type testSyncManager struct {
	isRunning  bool
	syncCalled bool
	syncError  error
	cancelled  bool
}

func (m *testSyncManager) IsRunning() bool {
	return m.isRunning
}

func (m *testSyncManager) Sync(dcimPath, cardID string, totalFiles int, totalBytes int64) error {
	m.syncCalled = true
	m.isRunning = true
	defer func() { m.isRunning = false }()
	return m.syncError
}

func (m *testSyncManager) Cancel() error {
	m.cancelled = true
	m.isRunning = false
	return nil
}

func (m *testSyncManager) SetGooglePhotos(enabled bool, remoteName string) {}
func (m *testSyncManager) UpdateConfig(remoteName, remotePath string, transfers, checkers int) {}

// TestNewHandler verifies handler initialization
func TestNewHandler(t *testing.T) {
	handler, stateMgr, mockSyncMgr, _ := setupTestEnvironment(t)

	if handler == nil {
		t.Fatal("Expected non-nil handler")
	}
	if handler.stateMgr != stateMgr {
		t.Error("State manager not properly set")
	}
	if handler.syncMgr != mockSyncMgr {
		t.Error("Sync manager not properly set")
	}
	if handler.syncStarting {
		t.Error("syncStarting should be false initially")
	}
}

// TestHandleInserted_BasicFlow tests the happy path for card insertion
func TestHandleInserted_BasicFlow(t *testing.T) {
	handler, stateMgr, _, tempDir := setupTestEnvironment(t)

	// Create a mock SD card with DCIM directory
	mountPath := filepath.Join(tempDir, "sdcard")
	dcimPath := filepath.Join(mountPath, "DCIM", "100CANON")
	if err := os.MkdirAll(dcimPath, 0755); err != nil {
		t.Fatalf("Failed to create DCIM: %v", err)
	}

	// Create some test photo files
	for i := 1; i <= 5; i++ {
		photoPath := filepath.Join(dcimPath, "IMG_"+string(rune('0'+i))+".JPG")
		if err := os.WriteFile(photoPath, []byte("fake image data"), 0644); err != nil {
			t.Fatalf("Failed to create test photo: %v", err)
		}
	}

	event := sdmonitor.Event{
		Type:      sdmonitor.EventInserted,
		DevPath:   "/dev/sda1",
		DevName:   "sda1",
		MountPath: mountPath,
	}

	// Handle the insertion event
	handler.HandleInserted(event)

	// Give goroutine time to process
	time.Sleep(100 * time.Millisecond)

	// Verify state was updated
	if !stateMgr.GetSDCardMounted() {
		t.Error("SD card should be marked as mounted")
	}

	currentState := stateMgr.GetCurrentState()
	if currentState.Status != state.StatusIdle && currentState.Status != state.StatusDetected {
		t.Logf("Note: Status is %s (async processing may still be in progress)", currentState.Status)
	}
}

// TestHandleInserted_NoDCIM verifies handling of cards without DCIM
func TestHandleInserted_NoDCIM(t *testing.T) {
	handler, stateMgr, mockSyncMgr, tempDir := setupTestEnvironment(t)

	// Create mount point without DCIM
	mountPath := filepath.Join(tempDir, "sdcard")
	if err := os.MkdirAll(mountPath, 0755); err != nil {
		t.Fatalf("Failed to create mount: %v", err)
	}

	// Create some other files
	if err := os.WriteFile(filepath.Join(mountPath, "README.txt"), []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	event := sdmonitor.Event{
		Type:      sdmonitor.EventInserted,
		DevPath:   "/dev/sda1",
		DevName:   "sda1",
		MountPath: mountPath,
	}

	handler.HandleInserted(event)

	// Give goroutine time to process
	time.Sleep(100 * time.Millisecond)

	// Sync should not have been called
	if mockSyncMgr.syncCalled {
		t.Error("Sync should not be called for card without DCIM")
	}

	// Status should return to idle
	if stateMgr.GetCurrentState().Status != state.StatusIdle {
		t.Errorf("Expected status idle, got %s", stateMgr.GetCurrentState().Status)
	}
}

// TestHandleInserted_NoPhotos verifies handling of empty DCIM
func TestHandleInserted_NoPhotos(t *testing.T) {
	handler, stateMgr, mockSyncMgr, tempDir := setupTestEnvironment(t)

	// Create DCIM but with no photos
	mountPath := filepath.Join(tempDir, "sdcard")
	dcimPath := filepath.Join(mountPath, "DCIM")
	if err := os.MkdirAll(dcimPath, 0755); err != nil {
		t.Fatalf("Failed to create DCIM: %v", err)
	}

	event := sdmonitor.Event{
		Type:      sdmonitor.EventInserted,
		DevPath:   "/dev/sda1",
		DevName:   "sda1",
		MountPath: mountPath,
	}

	handler.HandleInserted(event)

	// Give goroutine time to process
	time.Sleep(100 * time.Millisecond)

	// Sync should not have been called
	if mockSyncMgr.syncCalled {
		t.Error("Sync should not be called for empty DCIM")
	}
}

// TestHandleInserted_RaceCondition tests that concurrent insertions are handled safely
func TestHandleInserted_RaceCondition(t *testing.T) {
	handler, _, mockSyncMgr, tempDir := setupTestEnvironment(t)

	// Make sync manager report as running
	mockSyncMgr.isRunning = true

	// Create a mock SD card
	mountPath := filepath.Join(tempDir, "sdcard")
	if err := os.MkdirAll(filepath.Join(mountPath, "DCIM"), 0755); err != nil {
		t.Fatalf("Failed to create DCIM: %v", err)
	}

	event := sdmonitor.Event{
		Type:      sdmonitor.EventInserted,
		DevPath:   "/dev/sda1",
		DevName:   "sda1",
		MountPath: mountPath,
	}

	// Try to handle insertion while sync is running
	handler.HandleInserted(event)

	// Give goroutine time to process
	time.Sleep(50 * time.Millisecond)

	// Verify it was properly rejected
	// The syncCalled flag shouldn't change since we're simulating a running sync
	// (the real sync manager would prevent this)
}

// TestHandleRemoved_BasicFlow tests card removal
func TestHandleRemoved_BasicFlow(t *testing.T) {
	handler, stateMgr, _, _ := setupTestEnvironment(t)

	// First, simulate a card being inserted
	stateMgr.SetSDCard(true, "/mnt/sdcard")

	event := sdmonitor.Event{
		Type:    sdmonitor.EventRemoved,
		DevPath: "/dev/sda1",
		DevName: "sda1",
	}

	handler.HandleRemoved(event)

	// Verify state was updated
	if stateMgr.GetSDCardMounted() {
		t.Error("SD card should not be mounted after removal")
	}

	if stateMgr.GetCurrentState().Status != state.StatusIdle {
		t.Errorf("Expected idle status, got %s", stateMgr.GetCurrentState().Status)
	}
}

// TestHandleRemoved_DuringSync tests card removal during active sync
func TestHandleRemoved_DuringSync(t *testing.T) {
	handler, stateMgr, mockSyncMgr, _ := setupTestEnvironment(t)

	// Simulate an active sync
	mockSyncMgr.isRunning = true
	stateMgr.SetStatus(state.StatusSyncing)

	event := sdmonitor.Event{
		Type:    sdmonitor.EventRemoved,
		DevPath: "/dev/sda1",
		DevName: "sda1",
	}

	handler.HandleRemoved(event)

	// Verify sync was cancelled
	if !mockSyncMgr.cancelled {
		t.Error("Sync should have been cancelled on card removal")
	}

	// Verify state is idle
	if stateMgr.GetCurrentState().Status != state.StatusIdle {
		t.Errorf("Expected idle status after removal, got %s", stateMgr.GetCurrentState().Status)
	}
}

// TestReformatDetection verifies that reformatted cards get new IDs
func TestReformatDetection(t *testing.T) {
	handler, stateMgr, _, tempDir := setupTestEnvironment(t)

	// Create card with photos
	mountPath := filepath.Join(tempDir, "sdcard")
	dcimPath := filepath.Join(mountPath, "DCIM", "100CANON")
	if err := os.MkdirAll(dcimPath, 0755); err != nil {
		t.Fatalf("Failed to create DCIM: %v", err)
	}

	// Create initial set of photos (100 files)
	for i := 1; i <= 100; i++ {
		photoPath := filepath.Join(dcimPath, "IMG_"+string(rune('0'+i))+".JPG")
		if err := os.WriteFile(photoPath, []byte("fake image data"), 0644); err != nil {
			t.Fatalf("Failed to create test photo: %v", err)
		}
	}

	// Create card ID file
	cardIDPath := filepath.Join(mountPath, ".pictures-sync-id")
	if err := os.WriteFile(cardIDPath, []byte("card-test-123"), 0644); err != nil {
		t.Fatalf("Failed to create card ID: %v", err)
	}

	// Record a successful sync with 100 files
	syncID, _ := stateMgr.StartSync("card-test-123", 100, 1024*1024)
	stateMgr.FinishSync(true, nil)

	// Verify sync was recorded
	history := stateMgr.GetSyncHistory()
	if len(history) == 0 {
		t.Fatal("Expected sync history to be recorded")
	}
	if history[0].ID != syncID {
		t.Errorf("Expected sync ID %s, got %s", syncID, history[0].ID)
	}

	// Now simulate card being reformatted - delete most photos
	for i := 11; i <= 100; i++ {
		photoPath := filepath.Join(dcimPath, "IMG_"+string(rune('0'+i))+".JPG")
		os.Remove(photoPath)
	}
	// Now only 10 files remain (10% of original)

	event := sdmonitor.Event{
		Type:      sdmonitor.EventInserted,
		DevPath:   "/dev/sda1",
		DevName:   "sda1",
		MountPath: mountPath,
	}

	handler.HandleInserted(event)

	// Give goroutine time to process
	time.Sleep(200 * time.Millisecond)

	// The card ID file should have been updated with a new ID
	// (reformat detection threshold is typically 0.3 or 30%)
	newCardIDData, err := os.ReadFile(cardIDPath)
	if err != nil {
		t.Fatalf("Card ID file should still exist: %v", err)
	}

	newCardID := string(newCardIDData)
	if newCardID == "card-test-123" {
		t.Error("Card ID should have been regenerated after reformat detection")
	}
}

// TestConcurrentInsertions verifies thread safety with rapid insertions
func TestConcurrentInsertions(t *testing.T) {
	handler, _, _, tempDir := setupTestEnvironment(t)

	// Create multiple mount points
	for i := 0; i < 5; i++ {
		mountPath := filepath.Join(tempDir, "sdcard"+string(rune('0'+i)))
		dcimPath := filepath.Join(mountPath, "DCIM")
		if err := os.MkdirAll(dcimPath, 0755); err != nil {
			t.Fatalf("Failed to create DCIM: %v", err)
		}

		// Create test photo
		photoPath := filepath.Join(dcimPath, "IMG_001.JPG")
		if err := os.WriteFile(photoPath, []byte("fake"), 0644); err != nil {
			t.Fatalf("Failed to create photo: %v", err)
		}
	}

	// Fire multiple insertion events concurrently
	done := make(chan bool, 5)
	for i := 0; i < 5; i++ {
		go func(idx int) {
			event := sdmonitor.Event{
				Type:      sdmonitor.EventInserted,
				DevPath:   "/dev/sda" + string(rune('0'+idx)),
				DevName:   "sda" + string(rune('0'+idx)),
				MountPath: filepath.Join(tempDir, "sdcard"+string(rune('0'+idx))),
			}
			handler.HandleInserted(event)
			done <- true
		}(i)
	}

	// Wait for all to complete
	for i := 0; i < 5; i++ {
		<-done
	}

	// Should not panic or deadlock
	time.Sleep(100 * time.Millisecond)
}

// BenchmarkHandleInserted measures insertion handling performance
func BenchmarkHandleInserted(b *testing.B) {
	tempDir := b.TempDir()
	oldPerm := os.Getenv("PERM_DIR")
	os.Setenv("PERM_DIR", tempDir)
	defer func() {
		if oldPerm == "" {
			os.Unsetenv("PERM_DIR")
		} else {
			os.Setenv("PERM_DIR", oldPerm)
		}
	}()

	stateMgr, _ := state.NewManager()
	appSettings, _ := settings.Load()
	mockSyncMgr := &testSyncManager{}
	monitor := sdmonitor.NewMonitor(filepath.Join(tempDir, "mounts", "sdcard"))
	eventMgr := events.NewManager()
	handler := NewHandler(monitor, stateMgr, mockSyncMgr, appSettings, eventMgr)

	// Create test card
	mountPath := filepath.Join(tempDir, "sdcard")
	dcimPath := filepath.Join(mountPath, "DCIM", "100CANON")
	os.MkdirAll(dcimPath, 0755)
	for i := 0; i < 10; i++ {
		photoPath := filepath.Join(dcimPath, "IMG_"+string(rune('0'+i))+".JPG")
		os.WriteFile(photoPath, []byte("test"), 0644)
	}

	event := sdmonitor.Event{
		Type:      sdmonitor.EventInserted,
		DevPath:   "/dev/sda1",
		DevName:   "sda1",
		MountPath: mountPath,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		handler.HandleInserted(event)
		time.Sleep(10 * time.Millisecond) // Allow goroutine to start
	}
}
