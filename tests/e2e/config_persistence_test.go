package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/denysvitali/pictures-sync-s3/pkg/settings"
	"github.com/denysvitali/pictures-sync-s3/pkg/state"
	"github.com/denysvitali/pictures-sync-s3/pkg/wifimanager"
)

// TestE2ESettingsPersistence tests settings persist across restarts
func TestE2ESettingsPersistence(t *testing.T) {
	testEnv := setupTestEnvironment(t)
	defer testEnv.Cleanup()

	// Override settings path for testing
	originalSettingsFile := settings.SettingsFile
	settings.SettingsFile = filepath.Join(testEnv.BaseDir, "settings.json")
	defer func() {
		settings.SettingsFile = originalSettingsFile
	}()

	// === First session ===
	appSettings1, err := settings.Load()
	if err != nil {
		t.Fatalf("Failed to load settings in session 1: %v", err)
	}

	// Configure settings
	testValues := map[string]interface{}{
		"remote_name":         "persist-test-remote",
		"remote_path":         "/persist/test/path",
		"reformat_threshold":  0.25,
		"transfers":           8,
		"checkers":            16,
		"google_photos":       true,
		"google_photos_name":  "google-photos-remote",
	}

	appSettings1.SetRemoteName(testValues["remote_name"].(string))
	appSettings1.SetRemotePath(testValues["remote_path"].(string))
	appSettings1.SetReformatThreshold(testValues["reformat_threshold"].(float64))
	appSettings1.SetTransfers(testValues["transfers"].(int))
	appSettings1.SetCheckers(testValues["checkers"].(int))
	appSettings1.SetGooglePhotos(testValues["google_photos"].(bool), testValues["google_photos_name"].(string))

	// Verify settings file was created
	if _, err := os.Stat(settings.SettingsFile); os.IsNotExist(err) {
		t.Fatal("Settings file was not created")
	}

	// === Simulate restart - create new settings instance ===
	appSettings2, err := settings.Load()
	if err != nil {
		t.Fatalf("Failed to load settings in session 2: %v", err)
	}

	// Verify all settings persisted
	checks := []struct {
		name     string
		expected interface{}
		actual   interface{}
	}{
		{"remote_name", testValues["remote_name"], appSettings2.GetRemoteName()},
		{"remote_path", testValues["remote_path"], appSettings2.GetRemotePath()},
		{"reformat_threshold", testValues["reformat_threshold"], appSettings2.GetReformatThreshold()},
		{"transfers", testValues["transfers"], appSettings2.GetTransfers()},
		{"checkers", testValues["checkers"], appSettings2.GetCheckers()},
		{"google_photos", testValues["google_photos"], appSettings2.GetGooglePhotosEnabled()},
		{"google_photos_name", testValues["google_photos_name"], appSettings2.GetGooglePhotosRemoteName()},
	}

	for _, check := range checks {
		if check.expected != check.actual {
			t.Errorf("%s not persisted: expected %v, got %v", check.name, check.expected, check.actual)
		}
	}

	t.Log("All settings persisted successfully across restart")
}

// TestE2EHistoryPersistence tests sync history persists across restarts
func TestE2EHistoryPersistence(t *testing.T) {
	testEnv := setupTestEnvironment(t)
	defer testEnv.Cleanup()

	// Override state directory for testing
	originalBaseDir := state.BaseDir
	state.BaseDir = filepath.Join(testEnv.BaseDir, "state")
	state.HistoryFile = filepath.Join(state.BaseDir, "sync-history.json")
	state.StateFile = filepath.Join(state.BaseDir, "state.json")
	defer func() {
		state.BaseDir = originalBaseDir
		state.HistoryFile = filepath.Join(originalBaseDir, "sync-history.json")
		state.StateFile = filepath.Join(originalBaseDir, "state.json")
	}()

	// Create state directory
	if err := os.MkdirAll(state.BaseDir, 0755); err != nil {
		t.Fatalf("Failed to create state dir: %v", err)
	}

	// === First session - create sync history ===
	stateMgr1, err := state.NewManager()
	if err != nil {
		t.Fatalf("Failed to create state manager 1: %v", err)
	}

	// Perform multiple syncs
	syncs := []struct {
		cardID     string
		files      int64
		bytes      int64
		shouldFail bool
	}{
		{"persist-card-1", 100, 100 * 1024 * 1024, false},
		{"persist-card-2", 150, 150 * 1024 * 1024, false},
		{"persist-card-3", 80, 80 * 1024 * 1024, true},
		{"persist-card-1", 110, 110 * 1024 * 1024, false}, // Same card, more files
	}

	for _, sync := range syncs {
		stateMgr1.StartSync(sync.cardID, sync.files, sync.bytes)

		if sync.shouldFail {
			stateMgr1.FinishSync(false, fmt.Errorf("test error for %s", sync.cardID))
		} else {
			stateMgr1.FinishSync(true, nil)
		}

		time.Sleep(10 * time.Millisecond) // Small delay between syncs
	}

	// Get history from first session
	history1 := stateMgr1.GetHistory()
	if len(history1) != len(syncs) {
		t.Errorf("Session 1 history length = %d, want %d", len(history1), len(syncs))
	}

	// === Simulate restart ===
	stateMgr2, err := state.NewManager()
	if err != nil {
		t.Fatalf("Failed to create state manager 2: %v", err)
	}

	// Get history from second session
	history2 := stateMgr2.GetHistory()
	if len(history2) != len(history1) {
		t.Errorf("Session 2 history length = %d, want %d", len(history2), len(history1))
	}

	// Verify each sync record persisted correctly
	for i := range history1 {
		h1 := history1[i]
		h2 := history2[i]

		if h1.CardID != h2.CardID {
			t.Errorf("Record %d CardID: %s != %s", i, h1.CardID, h2.CardID)
		}
		if h1.FilesTotal != h2.FilesTotal {
			t.Errorf("Record %d FilesTotal: %d != %d", i, h1.FilesTotal, h2.FilesTotal)
		}
		if h1.Status != h2.Status {
			t.Errorf("Record %d Status: %s != %s", i, h1.Status, h2.Status)
		}
	}

	t.Logf("Successfully persisted %d sync records across restart", len(history2))
}

// TestE2ERcloneConfigPersistence tests rclone config persists
func TestE2ERcloneConfigPersistence(t *testing.T) {
	testEnv := setupTestEnvironment(t)
	defer testEnv.Cleanup()

	// Override config path
	originalConfigFile := state.ConfigFile
	state.ConfigFile = filepath.Join(testEnv.BaseDir, "rclone.conf")
	defer func() {
		state.ConfigFile = originalConfigFile
	}()

	// Create rclone config
	configContent := `[test-remote]
type = s3
provider = AWS
access_key_id = TEST_ACCESS_KEY
secret_access_key = TEST_SECRET_KEY
region = us-west-2
endpoint = https://s3.us-west-2.amazonaws.com

[backup-remote]
type = b2
account = test_account_id
key = test_app_key
`

	if err := os.WriteFile(state.ConfigFile, []byte(configContent), 0600); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	// Verify file exists and has correct permissions
	info, err := os.Stat(state.ConfigFile)
	if err != nil {
		t.Fatalf("Config file not found: %v", err)
	}

	perm := info.Mode().Perm()
	if perm != 0600 {
		t.Logf("Warning: Config permissions are %o, should be 0600 for security", perm)
	}

	// Verify can read config
	readContent, err := os.ReadFile(state.ConfigFile)
	if err != nil {
		t.Fatalf("Failed to read config: %v", err)
	}

	if string(readContent) != configContent {
		t.Error("Config content does not match")
	}

	// Verify EnsureRcloneConfig works
	exists, err := state.EnsureRcloneConfig()
	if err != nil {
		t.Errorf("EnsureRcloneConfig error: %v", err)
	}
	if !exists {
		t.Error("EnsureRcloneConfig should report config exists")
	}

	t.Log("Rclone config persistence verified")
}

// TestE2EWiFiConfigPersistence tests WiFi configuration persistence
func TestE2EWiFiConfigPersistence(t *testing.T) {
	testEnv := setupTestEnvironment(t)
	defer testEnv.Cleanup()

	// Override WiFi config path
	originalWiFiFile := wifimanager.WiFiConfigFile
	wifimanager.WiFiConfigFile = filepath.Join(testEnv.BaseDir, "extra-wifi.json")
	defer func() {
		wifimanager.WiFiConfigFile = originalWiFiFile
	}()

	// === First session - add networks ===
	wifiMgr1, err := wifimanager.NewManager()
	if err != nil {
		t.Fatalf("Failed to create WiFi manager 1: %v", err)
	}

	// Add test networks
	networks := []struct {
		ssid     string
		password string
	}{
		{"TestNetwork1", "password123"},
		{"TestNetwork2", "password456"},
		{"OpenNetwork", ""},
	}

	for _, net := range networks {
		if err := wifiMgr1.AddNetwork(net.ssid, net.password); err != nil {
			t.Errorf("Failed to add network %s: %v", net.ssid, err)
		}
	}

	// Verify networks were added
	savedNetworks1 := wifiMgr1.GetNetworks()
	if len(savedNetworks1) != len(networks) {
		t.Errorf("Session 1: added %d networks, got %d", len(networks), len(savedNetworks1))
	}

	// === Simulate restart ===
	wifiMgr2, err := wifimanager.NewManager()
	if err != nil {
		t.Fatalf("Failed to create WiFi manager 2: %v", err)
	}

	// Verify networks persisted
	savedNetworks2 := wifiMgr2.GetNetworks()
	if len(savedNetworks2) != len(networks) {
		t.Errorf("Session 2: expected %d networks, got %d", len(networks), len(savedNetworks2))
	}

	// Verify each network
	for _, expected := range networks {
		found := false
		for _, saved := range savedNetworks2 {
			if saved.SSID == expected.ssid {
				found = true
				// Note: We don't check password as it's stored hashed/encrypted
				break
			}
		}
		if !found {
			t.Errorf("Network %s not found after restart", expected.ssid)
		}
	}

	t.Logf("Successfully persisted %d WiFi networks", len(savedNetworks2))
}

// TestE2EStatePersistenceAcrossMultipleRestarts tests state across multiple restart cycles
func TestE2EStatePersistenceAcrossMultipleRestarts(t *testing.T) {
	testEnv := setupTestEnvironment(t)
	defer testEnv.Cleanup()

	// Override state directory
	originalBaseDir := state.BaseDir
	state.BaseDir = filepath.Join(testEnv.BaseDir, "state")
	state.StateFile = filepath.Join(state.BaseDir, "state.json")
	defer func() {
		state.BaseDir = originalBaseDir
		state.StateFile = filepath.Join(originalBaseDir, "state.json")
	}()

	if err := os.MkdirAll(state.BaseDir, 0755); err != nil {
		t.Fatalf("Failed to create state dir: %v", err)
	}

	// Perform multiple restart cycles
	const numCycles = 5

	for i := 0; i < numCycles; i++ {
		t.Logf("Restart cycle %d", i+1)

		stateMgr, err := state.NewManager()
		if err != nil {
			t.Fatalf("Cycle %d: failed to create manager: %v", i, err)
		}

		// Set some state
		stateMgr.SetStatus(state.StatusDetected)
		stateMgr.SetSDCard(true, fmt.Sprintf("/mnt/cycle-%d", i))

		// Verify state was saved
		time.Sleep(100 * time.Millisecond)

		// Read state file directly
		stateData, err := os.ReadFile(state.StateFile)
		if err != nil {
			t.Errorf("Cycle %d: failed to read state file: %v", i, err)
		}

		var savedState state.CurrentState
		if err := json.Unmarshal(stateData, &savedState); err != nil {
			t.Errorf("Cycle %d: failed to parse state: %v", i, err)
		}

		if savedState.Status != state.StatusDetected {
			t.Errorf("Cycle %d: status not saved correctly", i)
		}
	}

	t.Logf("Successfully completed %d restart cycles", numCycles)
}

// TestE2EConfigurationMigration tests handling of config format changes
func TestE2EConfigurationMigration(t *testing.T) {
	testEnv := setupTestEnvironment(t)
	defer testEnv.Cleanup()

	// Override settings path
	originalSettingsFile := settings.SettingsFile
	settings.SettingsFile = filepath.Join(testEnv.BaseDir, "settings.json")
	defer func() {
		settings.SettingsFile = originalSettingsFile
	}()

	// Create old format settings (simulating older version)
	oldFormatSettings := map[string]interface{}{
		"remote_name": "old-remote",
		"remote_path": "/old/path",
		// Missing new fields that should be added with defaults
	}

	oldData, _ := json.Marshal(oldFormatSettings)
	if err := os.WriteFile(settings.SettingsFile, oldData, 0644); err != nil {
		t.Fatalf("Failed to write old format settings: %v", err)
	}

	// Load settings - should migrate to new format
	appSettings, err := settings.Load()
	if err != nil {
		t.Fatalf("Failed to load old format settings: %v", err)
	}

	// Verify old values preserved
	if appSettings.GetRemoteName() != "old-remote" {
		t.Errorf("Old remote_name not preserved: got %s", appSettings.GetRemoteName())
	}

	// Verify new fields have defaults
	if appSettings.GetReformatThreshold() == 0 {
		t.Error("Reformat threshold should have default value")
	}

	t.Log("Successfully migrated old config format")
}

// TestE2EAtomicWrites tests that state saves are atomic
func TestE2EAtomicWrites(t *testing.T) {
	testEnv := setupTestEnvironment(t)
	defer testEnv.Cleanup()

	// Override state directory
	originalBaseDir := state.BaseDir
	state.BaseDir = filepath.Join(testEnv.BaseDir, "state")
	state.StateFile = filepath.Join(state.BaseDir, "state.json")
	defer func() {
		state.BaseDir = originalBaseDir
		state.StateFile = filepath.Join(originalBaseDir, "state.json")
	}()

	if err := os.MkdirAll(state.BaseDir, 0755); err != nil {
		t.Fatalf("Failed to create state dir: %v", err)
	}

	stateMgr, err := state.NewManager()
	if err != nil {
		t.Fatalf("Failed to create state manager: %v", err)
	}

	// Perform many rapid state changes
	for i := 0; i < 100; i++ {
		stateMgr.SetStatus(state.StatusSyncing)
		stateMgr.UpdateSyncProgress(int64(i), int64(i)*1024, fmt.Sprintf("file%d.jpg", i), 1024, 1024, "1s")
	}

	// Wait for writes to complete
	time.Sleep(500 * time.Millisecond)

	// Read state file - should be valid JSON (atomic write ensures no partial writes)
	stateData, err := os.ReadFile(state.StateFile)
	if err != nil {
		t.Fatalf("Failed to read state file: %v", err)
	}

	var savedState state.CurrentState
	if err := json.Unmarshal(stateData, &savedState); err != nil {
		t.Errorf("State file corrupted (non-atomic write): %v", err)
	}

	// Verify no temporary files left behind
	tempFiles, _ := filepath.Glob(filepath.Join(state.BaseDir, "*.tmp"))
	if len(tempFiles) > 0 {
		t.Errorf("Found %d temporary files not cleaned up: %v", len(tempFiles), tempFiles)
	}

	t.Log("Atomic writes verified - no corruption during rapid updates")
}

// TestE2EConfigBackupAndRestore tests configuration backup/restore
func TestE2EConfigBackupAndRestore(t *testing.T) {
	testEnv := setupTestEnvironment(t)
	defer testEnv.Cleanup()

	// Override settings path
	originalSettingsFile := settings.SettingsFile
	settings.SettingsFile = filepath.Join(testEnv.BaseDir, "settings.json")
	defer func() {
		settings.SettingsFile = originalSettingsFile
	}()

	// Create initial settings
	appSettings1, _ := settings.Load()
	appSettings1.SetRemoteName("backup-test")
	appSettings1.SetRemotePath("/backup/test")
	appSettings1.SetReformatThreshold(0.35)

	// Create backup
	backupPath := settings.SettingsFile + ".backup"
	if err := copyFile(settings.SettingsFile, backupPath); err != nil {
		t.Fatalf("Failed to create backup: %v", err)
	}

	// Modify settings
	appSettings1.SetRemoteName("modified")
	appSettings1.SetRemotePath("/modified")

	// Verify modification
	appSettings2, _ := settings.Load()
	if appSettings2.GetRemoteName() != "modified" {
		t.Error("Modification did not take effect")
	}

	// Restore from backup
	if err := copyFile(backupPath, settings.SettingsFile); err != nil {
		t.Fatalf("Failed to restore backup: %v", err)
	}

	// Load restored settings
	appSettings3, _ := settings.Load()
	if appSettings3.GetRemoteName() != "backup-test" {
		t.Errorf("Restore failed: got %s, want backup-test", appSettings3.GetRemoteName())
	}
	if appSettings3.GetReformatThreshold() != 0.35 {
		t.Errorf("Restore failed: threshold = %f, want 0.35", appSettings3.GetReformatThreshold())
	}

	t.Log("Configuration backup and restore successful")
}

// TestE2EStateConsistencyAfterCrash tests state consistency after simulated crash
func TestE2EStateConsistencyAfterCrash(t *testing.T) {
	testEnv := setupTestEnvironment(t)
	defer testEnv.Cleanup()

	// Override state directory
	originalBaseDir := state.BaseDir
	state.BaseDir = filepath.Join(testEnv.BaseDir, "state")
	state.StateFile = filepath.Join(state.BaseDir, "state.json")
	state.HistoryFile = filepath.Join(state.BaseDir, "sync-history.json")
	defer func() {
		state.BaseDir = originalBaseDir
		state.StateFile = filepath.Join(originalBaseDir, "state.json")
		state.HistoryFile = filepath.Join(originalBaseDir, "sync-history.json")
	}()

	if err := os.MkdirAll(state.BaseDir, 0755); err != nil {
		t.Fatalf("Failed to create state dir: %v", err)
	}

	// Create state and start a sync
	stateMgr1, _ := state.NewManager()
	stateMgr1.SetStatus(state.StatusSyncing)
	stateMgr1.SetSDCard(true, "/mnt/crash-test")
	stateMgr1.StartSync("crash-card", 100, 100*1024*1024)
	stateMgr1.UpdateSyncProgress(50, 50*1024*1024, "IMG_0050.JPG", 2*1024*1024, 1.5*1024*1024, "1m")

	// Wait for state to be written
	time.Sleep(200 * time.Millisecond)

	// Simulate crash - don't call cleanup, just create new manager
	stateMgr2, err := state.NewManager()
	if err != nil {
		t.Fatalf("Failed to create manager after crash: %v", err)
	}

	// Verify state was loaded
	crashedState := stateMgr2.GetState()
	t.Logf("State after crash: status=%s, mounted=%v", crashedState.Status, crashedState.SDCardMounted)

	// State should be consistent (even if sync was incomplete)
	if crashedState.Status == "" {
		t.Error("State became invalid after crash")
	}

	// Should be able to continue operations
	stateMgr2.SetStatus(state.StatusIdle)
	stateMgr2.StartSync("post-crash", 50, 50*1024*1024)
	stateMgr2.FinishSync(true, nil)

	// Verify new sync was recorded
	history := stateMgr2.GetHistory()
	found := false
	for _, record := range history {
		if record.CardID == "post-crash" {
			found = true
			break
		}
	}

	if !found {
		t.Error("Failed to record sync after crash recovery")
	}

	t.Log("State remained consistent after simulated crash")
}
