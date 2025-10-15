package syncmanager

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/denysvitali/pictures-sync-s3/pkg/state"
)

func setupTestSyncManager(t *testing.T) (*Manager, *state.Manager, func()) {
	// Create temporary directory for test
	tmpDir, err := os.MkdirTemp("", "syncmanager-test-*")
	if err != nil {
		t.Fatal(err)
	}

	// Create test state manager
	testStateManager, err := state.NewManager()
	if err != nil {
		// Create a mock manager for testing
		testStateManager = &state.Manager{}
	}

	// Create sync manager
	configPath := filepath.Join(tmpDir, "rclone.conf")
	syncMgr := NewManager(configPath, "test-remote", "/test/path", testStateManager, 4, 8)

	cleanup := func() {
		os.RemoveAll(tmpDir)
	}

	return syncMgr, testStateManager, cleanup
}

func TestNewManager(t *testing.T) {
	syncMgr, _, cleanup := setupTestSyncManager(t)
	defer cleanup()

	if syncMgr == nil {
		t.Fatal("NewManager returned nil")
	}

	if syncMgr.configPath == "" {
		t.Error("Config path not set")
	}

	if syncMgr.remoteName != "test-remote" {
		t.Errorf("Remote name = %v, want test-remote", syncMgr.remoteName)
	}

	if syncMgr.remotePath != "/test/path" {
		t.Errorf("Remote path = %v, want /test/path", syncMgr.remotePath)
	}

	// Check that manager was created
	if syncMgr == nil {
		t.Error("Manager should not be nil")
	}
}

func TestSetRemote(t *testing.T) {
	syncMgr, _, cleanup := setupTestSyncManager(t)
	defer cleanup()

	// Test SetRemote
	syncMgr.SetRemote("new-remote", "/new/path")

	if syncMgr.remoteName != "new-remote" {
		t.Errorf("Remote name after SetRemote = %v, want new-remote", syncMgr.remoteName)
	}

	if syncMgr.remotePath != "/new/path" {
		t.Errorf("Remote path after SetRemote = %v, want /new/path", syncMgr.remotePath)
	}
}

func TestSetGooglePhotos(t *testing.T) {
	syncMgr, _, cleanup := setupTestSyncManager(t)
	defer cleanup()

	// Test enabling Google Photos
	syncMgr.SetGooglePhotos(true, "google-photos-remote")

	// Test disabling Google Photos
	syncMgr.SetGooglePhotos(false, "")

	// Note: We can't test internal state since fields are not exported
	// This test just ensures the method doesn't panic
}

func TestSubscribeProgress(t *testing.T) {
	syncMgr, _, cleanup := setupTestSyncManager(t)
	defer cleanup()

	// Test subscribing to progress
	progressChan := syncMgr.SubscribeProgress()
	if progressChan == nil {
		t.Error("SubscribeProgress should return a channel")
	}

	// Test that we can subscribe multiple times
	progressChan2 := syncMgr.SubscribeProgress()
	if progressChan2 == nil {
		t.Error("Second SubscribeProgress should return a channel")
	}
}

func TestTestConnection(t *testing.T) {
	syncMgr, _, cleanup := setupTestSyncManager(t)
	defer cleanup()

	// Test connection (will likely fail without proper config, but shouldn't panic)
	err := syncMgr.TestConnection()
	if err != nil {
		t.Logf("TestConnection failed as expected in test environment: %v", err)
	}
}

func TestGetConfig(t *testing.T) {
	syncMgr, _, cleanup := setupTestSyncManager(t)
	defer cleanup()

	// Test getting config (should return empty string if no config file)
	config := syncMgr.GetConfig()
	t.Logf("Config returned: %q", config)
}

func TestIsRunning(t *testing.T) {
	syncMgr, _, cleanup := setupTestSyncManager(t)
	defer cleanup()

	// Initially not running
	if syncMgr.IsRunning() {
		t.Error("Sync manager should not be running initially")
	}
}

func TestCancel(t *testing.T) {
	syncMgr, _, cleanup := setupTestSyncManager(t)
	defer cleanup()

	// Test canceling when not running
	err := syncMgr.Cancel()
	if err == nil {
		t.Error("Cancel should return error when not running")
	}
}

func TestListRemotes(t *testing.T) {
	syncMgr, _, cleanup := setupTestSyncManager(t)
	defer cleanup()

	// Test listing remotes (will likely fail without config, but shouldn't panic)
	remotes, err := syncMgr.ListRemotes()
	if err != nil {
		t.Logf("ListRemotes failed as expected in test environment: %v", err)
	} else {
		t.Logf("Found %d remotes", len(remotes))
	}
}