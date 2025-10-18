package e2e

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/denysvitali/pictures-sync-s3/pkg/daemon"
	"github.com/denysvitali/pictures-sync-s3/pkg/daemon/cardhandler"
	"github.com/denysvitali/pictures-sync-s3/pkg/events"
	"github.com/denysvitali/pictures-sync-s3/pkg/sdmonitor"
	"github.com/denysvitali/pictures-sync-s3/pkg/settings"
	"github.com/denysvitali/pictures-sync-s3/pkg/state"
	"github.com/denysvitali/pictures-sync-s3/pkg/syncmanager"
)

// TestE2EFullSyncWorkflow tests the complete sync workflow from SD card insertion to completion
func TestE2EFullSyncWorkflow(t *testing.T) {
	// Create temporary test environment
	testEnv := setupTestEnvironment(t)
	defer testEnv.Cleanup()

	// Create a mock SD card with photos
	mockCard := testEnv.CreateMockSDCard("test-card-001", 100)

	// Initialize event manager to track events
	eventMgr := events.NewManager()
	eventChan := eventMgr.Subscribe()
	defer eventMgr.Unsubscribe(eventChan)

	// Track events received
	receivedEvents := make([]events.EventType, 0)
	go func() {
		for event := range eventChan {
			receivedEvents = append(receivedEvents, event.Type)
			t.Logf("Event received: %s - %s", event.Type, event.Message)
		}
	}()

	// Initialize state manager
	stateMgr, err := state.NewManager()
	if err != nil {
		t.Fatalf("Failed to create state manager: %v", err)
	}

	// Subscribe to state changes
	stateChan := stateMgr.Subscribe()
	defer stateMgr.Unsubscribe(stateChan)

	stateTransitions := make([]state.SyncStatus, 0)
	go func() {
		for s := range stateChan {
			stateTransitions = append(stateTransitions, s.Status)
			t.Logf("State transition: %s", s.Status)
		}
	}()

	// Load settings
	appSettings, err := settings.Load()
	if err != nil {
		t.Fatalf("Failed to load settings: %v", err)
	}

	// Configure test remote
	appSettings.SetRemoteName("test-remote")
	appSettings.SetRemotePath("/test/photos")

	// Initialize sync manager (will use mock rclone in test environment)
	syncMgr := syncmanager.NewManager(
		testEnv.RcloneConfigPath,
		appSettings.GetRemoteName(),
		appSettings.GetRemotePath(),
		stateMgr,
		appSettings.GetTransfers(),
		appSettings.GetCheckers(),
	)

	// Initialize SD monitor
	monitor := sdmonitor.NewMonitor(testEnv.MountDir)

	// Initialize card handler
	handler := cardhandler.NewHandler(monitor, stateMgr, syncMgr, appSettings, eventMgr)

	// Start monitoring
	if err := monitor.Start(); err != nil {
		t.Fatalf("Failed to start monitor: %v", err)
	}
	defer monitor.Stop()

	// Simulate SD card insertion by creating mount point and triggering event
	if err := testEnv.MountMockCard(mockCard); err != nil {
		t.Fatalf("Failed to mount mock card: %v", err)
	}

	// Create insertion event
	event := sdmonitor.Event{
		Type:      sdmonitor.EventInserted,
		DevName:   mockCard.DevName,
		MountPath: mockCard.MountPath,
	}

	// Handle the insertion
	handler.HandleInserted(event)

	// Wait for sync to complete (with timeout)
	timeout := time.After(30 * time.Second)
	syncCompleted := false

	for !syncCompleted {
		select {
		case <-timeout:
			t.Fatal("Sync did not complete within 30 seconds")
		case <-time.After(100 * time.Millisecond):
			currentState := stateMgr.GetState()
			if currentState.Status == state.StatusSuccess || currentState.Status == state.StatusError {
				syncCompleted = true
				if currentState.Status == state.StatusError {
					t.Errorf("Sync failed: %s", currentState.Error)
				}
			}
		}
	}

	// Verify state transitions occurred in expected order
	expectedStates := []state.SyncStatus{
		state.StatusDetected,
		state.StatusSyncing,
		state.StatusSuccess,
	}

	for _, expected := range expectedStates {
		found := false
		for _, actual := range stateTransitions {
			if actual == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected state transition %s not found in %v", expected, stateTransitions)
		}
	}

	// Verify sync history was recorded
	history := stateMgr.GetHistory()
	if len(history) == 0 {
		t.Fatal("No sync history recorded")
	}

	lastSync := history[len(history)-1]
	if lastSync.CardID != mockCard.CardID {
		t.Errorf("Sync card ID = %s, want %s", lastSync.CardID, mockCard.CardID)
	}
	if lastSync.Status != "success" {
		t.Errorf("Sync status = %s, want success", lastSync.Status)
	}

	// Verify events were emitted in correct sequence
	expectedEventTypes := []events.EventType{
		events.EventSDCardInserted,
		events.EventPhotosDetected,
		events.EventSyncStarted,
		events.EventSyncCompleted,
	}

	for _, expectedType := range expectedEventTypes {
		found := false
		for _, actualType := range receivedEvents {
			if actualType == expectedType {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected event %s not found in received events", expectedType)
		}
	}

	t.Logf("E2E sync workflow completed successfully")
}

// TestE2EReformatDetection tests card reformat detection
func TestE2EReformatDetection(t *testing.T) {
	testEnv := setupTestEnvironment(t)
	defer testEnv.Cleanup()

	// Create initial card with 100 photos
	mockCard := testEnv.CreateMockSDCard("reformat-card", 100)

	// Initialize components
	eventMgr := events.NewManager()
	stateMgr, _ := state.NewManager()
	appSettings, _ := settings.Load()
	appSettings.SetReformatThreshold(0.3) // 30% threshold

	syncMgr := syncmanager.NewManager(
		testEnv.RcloneConfigPath,
		appSettings.GetRemoteName(),
		appSettings.GetRemotePath(),
		stateMgr,
		appSettings.GetTransfers(),
		appSettings.GetCheckers(),
	)

	monitor := sdmonitor.NewMonitor(testEnv.MountDir)
	handler := cardhandler.NewHandler(monitor, stateMgr, syncMgr, appSettings, eventMgr)

	monitor.Start()
	defer monitor.Stop()

	// First sync - simulate with 100 files
	testEnv.MountMockCard(mockCard)
	event := sdmonitor.Event{
		Type:      sdmonitor.EventInserted,
		DevName:   mockCard.DevName,
		MountPath: mockCard.MountPath,
	}

	handler.HandleInserted(event)
	time.Sleep(2 * time.Second) // Wait for first sync

	// Record first sync manually to simulate completion
	stateMgr.StartSync(mockCard.CardID, 100, 100*1024*1024)
	stateMgr.FinishSync(true, nil)

	firstCardID := mockCard.CardID

	// Simulate card removal
	handler.HandleRemoved(sdmonitor.Event{
		Type:    sdmonitor.EventRemoved,
		DevName: mockCard.DevName,
	})
	testEnv.UnmountMockCard(mockCard)

	// "Reformat" the card - clear most files (simulate reformat)
	testEnv.ReformatMockCard(mockCard, 20) // Only 20 files now (20% of original)

	// Re-insert the card
	testEnv.MountMockCard(mockCard)
	handler.HandleInserted(event)
	time.Sleep(2 * time.Second)

	// Verify new card ID was created
	history := stateMgr.GetHistory()
	if len(history) < 2 {
		t.Fatal("Expected at least 2 sync records after reformat")
	}

	lastSync := history[len(history)-1]
	if lastSync.CardID == firstCardID {
		t.Error("Card ID should have changed after reformat detection")
	}

	t.Logf("Reformat detection successful: %s -> %s", firstCardID, lastSync.CardID)
}

// TestE2ECardRemovalDuringSync tests handling card removal during active sync
func TestE2ECardRemovalDuringSync(t *testing.T) {
	testEnv := setupTestEnvironment(t)
	defer testEnv.Cleanup()

	mockCard := testEnv.CreateMockSDCard("removal-card", 50)

	eventMgr := events.NewManager()
	stateMgr, _ := state.NewManager()
	appSettings, _ := settings.Load()

	syncMgr := syncmanager.NewManager(
		testEnv.RcloneConfigPath,
		appSettings.GetRemoteName(),
		appSettings.GetRemotePath(),
		stateMgr,
		appSettings.GetTransfers(),
		appSettings.GetCheckers(),
	)

	monitor := sdmonitor.NewMonitor(testEnv.MountDir)
	handler := cardhandler.NewHandler(monitor, stateMgr, syncMgr, appSettings, eventMgr)

	monitor.Start()
	defer monitor.Stop()

	// Insert card and start sync
	testEnv.MountMockCard(mockCard)
	event := sdmonitor.Event{
		Type:      sdmonitor.EventInserted,
		DevName:   mockCard.DevName,
		MountPath: mockCard.MountPath,
	}

	handler.HandleInserted(event)

	// Wait for sync to start
	timeout := time.After(5 * time.Second)
	for {
		select {
		case <-timeout:
			t.Fatal("Sync did not start within 5 seconds")
		case <-time.After(100 * time.Millisecond):
			if stateMgr.GetState().Status == state.StatusSyncing {
				goto syncStarted
			}
		}
	}

syncStarted:
	// Simulate card removal during sync
	handler.HandleRemoved(sdmonitor.Event{
		Type:    sdmonitor.EventRemoved,
		DevName: mockCard.DevName,
	})

	// Wait a moment for cancellation to process
	time.Sleep(500 * time.Millisecond)

	// Verify sync was cancelled and error was recorded
	finalState := stateMgr.GetState()
	if finalState.Status != state.StatusIdle {
		t.Errorf("Expected status idle after removal, got %s", finalState.Status)
	}

	// Check history for error
	history := stateMgr.GetHistory()
	if len(history) == 0 {
		t.Fatal("No history recorded")
	}

	lastSync := history[len(history)-1]
	if lastSync.Status != "error" {
		t.Errorf("Expected error status in history, got %s", lastSync.Status)
	}
	if lastSync.Error == "" {
		t.Error("Expected error message in history")
	}

	t.Logf("Card removal during sync handled correctly: %s", lastSync.Error)
}

// TestE2EMultipleCardsSequential tests syncing multiple cards sequentially
func TestE2EMultipleCardsSequential(t *testing.T) {
	testEnv := setupTestEnvironment(t)
	defer testEnv.Cleanup()

	eventMgr := events.NewManager()
	stateMgr, _ := state.NewManager()
	appSettings, _ := settings.Load()

	syncMgr := syncmanager.NewManager(
		testEnv.RcloneConfigPath,
		appSettings.GetRemoteName(),
		appSettings.GetRemotePath(),
		stateMgr,
		appSettings.GetTransfers(),
		appSettings.GetCheckers(),
	)

	monitor := sdmonitor.NewMonitor(testEnv.MountDir)
	handler := cardhandler.NewHandler(monitor, stateMgr, syncMgr, appSettings, eventMgr)

	monitor.Start()
	defer monitor.Stop()

	// Test syncing 3 different cards
	cards := []struct {
		id    string
		files int
	}{
		{"card-alpha", 50},
		{"card-beta", 75},
		{"card-gamma", 100},
	}

	for i, cardInfo := range cards {
		t.Logf("Testing card %d: %s with %d files", i+1, cardInfo.id, cardInfo.files)

		mockCard := testEnv.CreateMockSDCard(cardInfo.id, cardInfo.files)

		// Insert and sync
		testEnv.MountMockCard(mockCard)
		handler.HandleInserted(sdmonitor.Event{
			Type:      sdmonitor.EventInserted,
			DevName:   mockCard.DevName,
			MountPath: mockCard.MountPath,
		})

		// Wait for sync to complete
		waitForSyncCompletion(t, stateMgr, 15*time.Second)

		// Verify sync succeeded
		currentState := stateMgr.GetState()
		if currentState.Status != state.StatusSuccess {
			t.Errorf("Card %s sync failed: %s", cardInfo.id, currentState.Error)
		}

		// Remove card
		handler.HandleRemoved(sdmonitor.Event{
			Type:    sdmonitor.EventRemoved,
			DevName: mockCard.DevName,
		})
		testEnv.UnmountMockCard(mockCard)

		time.Sleep(500 * time.Millisecond) // Brief pause between cards
	}

	// Verify all cards were synced
	history := stateMgr.GetHistory()
	if len(history) != len(cards) {
		t.Errorf("Expected %d sync records, got %d", len(cards), len(history))
	}

	// Verify each card has a record
	for _, cardInfo := range cards {
		found := false
		for _, record := range history {
			if record.CardID == cardInfo.id {
				found = true
				if record.Status != "success" {
					t.Errorf("Card %s sync status = %s, want success", cardInfo.id, record.Status)
				}
				break
			}
		}
		if !found {
			t.Errorf("No sync record found for card %s", cardInfo.id)
		}
	}

	t.Logf("Successfully synced %d cards sequentially", len(cards))
}

// TestE2EEmptyCard tests handling of card with no photos
func TestE2EEmptyCard(t *testing.T) {
	testEnv := setupTestEnvironment(t)
	defer testEnv.Cleanup()

	mockCard := testEnv.CreateMockSDCard("empty-card", 0) // No photos

	eventMgr := events.NewManager()
	eventChan := eventMgr.Subscribe()
	defer eventMgr.Unsubscribe(eventChan)

	stateMgr, _ := state.NewManager()
	appSettings, _ := settings.Load()

	syncMgr := syncmanager.NewManager(
		testEnv.RcloneConfigPath,
		appSettings.GetRemoteName(),
		appSettings.GetRemotePath(),
		stateMgr,
		appSettings.GetTransfers(),
		appSettings.GetCheckers(),
	)

	monitor := sdmonitor.NewMonitor(testEnv.MountDir)
	handler := cardhandler.NewHandler(monitor, stateMgr, syncMgr, appSettings, eventMgr)

	monitor.Start()
	defer monitor.Stop()

	// Track events
	receivedNoPhotosEvent := false
	go func() {
		for event := range eventChan {
			if event.Type == events.EventNoPhotosFound {
				receivedNoPhotosEvent = true
			}
		}
	}()

	// Insert empty card
	testEnv.MountMockCard(mockCard)
	handler.HandleInserted(sdmonitor.Event{
		Type:      sdmonitor.EventInserted,
		DevName:   mockCard.DevName,
		MountPath: mockCard.MountPath,
	})

	time.Sleep(2 * time.Second)

	// Verify no sync was started
	history := stateMgr.GetHistory()
	if len(history) != 0 {
		t.Error("Empty card should not create sync history")
	}

	// Verify correct event was emitted
	if !receivedNoPhotosEvent {
		t.Error("EventNoPhotosFound should have been emitted")
	}

	// Verify state went back to idle
	if stateMgr.GetState().Status != state.StatusIdle {
		t.Errorf("Expected idle status for empty card, got %s", stateMgr.GetState().Status)
	}

	t.Log("Empty card handling verified")
}

// waitForSyncCompletion waits for a sync to complete or timeout
func waitForSyncCompletion(t *testing.T, stateMgr *state.Manager, timeout time.Duration) {
	deadline := time.After(timeout)
	for {
		select {
		case <-deadline:
			t.Fatal("Sync did not complete within timeout")
		case <-time.After(100 * time.Millisecond):
			status := stateMgr.GetState().Status
			if status == state.StatusSuccess || status == state.StatusError || status == state.StatusIdle {
				return
			}
		}
	}
}

// TestEnvironment holds test environment resources
type TestEnvironment struct {
	BaseDir          string
	MountDir         string
	RcloneConfigPath string
	MockCards        []*MockCard
	t                *testing.T
}

// MockCard represents a mock SD card for testing
type MockCard struct {
	CardID    string
	DevName   string
	MountPath string
	NumFiles  int
}

// setupTestEnvironment creates a test environment
func setupTestEnvironment(t *testing.T) *TestEnvironment {
	baseDir, err := os.MkdirTemp("", "e2e-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	mountDir := filepath.Join(baseDir, "mounts")
	if err := os.MkdirAll(mountDir, 0755); err != nil {
		t.Fatalf("Failed to create mount dir: %v", err)
	}

	rcloneConfigPath := filepath.Join(baseDir, "rclone.conf")

	// Create minimal rclone config for testing
	configContent := `[test-remote]
type = local
`
	if err := os.WriteFile(rcloneConfigPath, []byte(configContent), 0600); err != nil {
		t.Fatalf("Failed to create rclone config: %v", err)
	}

	return &TestEnvironment{
		BaseDir:          baseDir,
		MountDir:         mountDir,
		RcloneConfigPath: rcloneConfigPath,
		MockCards:        make([]*MockCard, 0),
		t:                t,
	}
}

// CreateMockSDCard creates a mock SD card with photos
func (env *TestEnvironment) CreateMockSDCard(cardID string, numFiles int) *MockCard {
	devName := fmt.Sprintf("mock-%s", cardID)
	mountPath := filepath.Join(env.MountDir, devName)

	card := &MockCard{
		CardID:    cardID,
		DevName:   devName,
		MountPath: mountPath,
		NumFiles:  numFiles,
	}

	env.MockCards = append(env.MockCards, card)
	return card
}

// MountMockCard creates the filesystem structure for a mock card
func (env *TestEnvironment) MountMockCard(card *MockCard) error {
	// Create mount point
	if err := os.MkdirAll(card.MountPath, 0755); err != nil {
		return err
	}

	// Create DCIM directory
	dcimPath := filepath.Join(card.MountPath, "DCIM", "100CANON")
	if err := os.MkdirAll(dcimPath, 0755); err != nil {
		return err
	}

	// Create card ID file
	cardIDFile := filepath.Join(card.MountPath, ".pictures-sync-id")
	if err := os.WriteFile(cardIDFile, []byte(card.CardID), 0644); err != nil {
		return err
	}

	// Create mock photo files
	for i := 0; i < card.NumFiles; i++ {
		filename := filepath.Join(dcimPath, fmt.Sprintf("IMG_%04d.JPG", i+1))
		// Create small files for testing
		if err := os.WriteFile(filename, []byte("fake jpeg data"), 0644); err != nil {
			return err
		}
	}

	return nil
}

// UnmountMockCard removes the mock card filesystem
func (env *TestEnvironment) UnmountMockCard(card *MockCard) error {
	return os.RemoveAll(card.MountPath)
}

// ReformatMockCard simulates reformatting by removing most files
func (env *TestEnvironment) ReformatMockCard(card *MockCard, newFileCount int) error {
	// Clear existing DCIM
	dcimPath := filepath.Join(card.MountPath, "DCIM")
	if err := os.RemoveAll(dcimPath); err != nil {
		return err
	}

	// Remove card ID file (simulating reformat)
	cardIDFile := filepath.Join(card.MountPath, ".pictures-sync-id")
	os.Remove(cardIDFile)

	// Update file count
	card.NumFiles = newFileCount

	return nil
}

// Cleanup removes all test resources
func (env *TestEnvironment) Cleanup() {
	if env.BaseDir != "" {
		os.RemoveAll(env.BaseDir)
	}
}

// copyFile copies a file from src to dst
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}
