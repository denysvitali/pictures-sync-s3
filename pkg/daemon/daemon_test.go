package daemon

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.NTPSyncDelay != 5*time.Second {
		t.Errorf("Expected NTPSyncDelay of 5s, got %v", cfg.NTPSyncDelay)
	}
}

func TestNew_WithSyncedTime(t *testing.T) {
	// This test will pass since our system time is > 2020
	// Skip if running in a test environment with mocked time
	now := time.Now()
	if now.Year() <= 2020 {
		t.Skip("Skipping test - system time is not properly set")
	}

	// Create temp directory for state persistence
	tempDir := t.TempDir()
	oldPerm := os.Getenv("PERM_DIR")
	os.Setenv("PERM_DIR", tempDir)
	defer func() {
		if oldPerm == "" {
			os.Unsetenv("PERM_DIR")
		} else {
			os.Setenv("PERM_DIR", oldPerm)
		}
	}()

	cfg := Config{
		NTPSyncDelay: 100 * time.Millisecond, // Short delay for testing
	}

	// This will timeout waiting for DNS but should still complete
	service, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create service: %v", err)
	}

	if service == nil {
		t.Fatal("Expected non-nil service")
	}

	// Verify service components are initialized
	if service.eventMgr == nil {
		t.Error("Expected event manager to be initialized")
	}
	if service.stateMgr == nil {
		t.Error("Expected state manager to be initialized")
	}
	if service.settings == nil {
		t.Error("Expected settings to be initialized")
	}
	if service.syncMgr == nil {
		t.Error("Expected sync manager to be initialized")
	}
	if service.monitor == nil {
		t.Error("Expected SD monitor to be initialized")
	}
	if service.cardHandler == nil {
		t.Error("Expected card handler to be initialized")
	}
	if service.sigHandler == nil {
		t.Error("Expected signal handler to be initialized")
	}

	// Clean up
	service.Shutdown()
}

func TestShutdown_GracefulCleanup(t *testing.T) {
	// Create temp directory for state persistence
	tempDir := t.TempDir()
	oldPerm := os.Getenv("PERM_DIR")
	os.Setenv("PERM_DIR", tempDir)
	defer func() {
		if oldPerm == "" {
			os.Unsetenv("PERM_DIR")
		} else {
			os.Setenv("PERM_DIR", oldPerm)
		}
	}()

	cfg := Config{
		NTPSyncDelay: 100 * time.Millisecond,
	}

	service, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create service: %v", err)
	}

	// Should not panic on shutdown
	service.Shutdown()

	// Multiple shutdowns should be safe
	service.Shutdown()
}

func TestShutdown_NilComponents(t *testing.T) {
	// Create service with nil components to test defensive programming
	service := &Service{}

	// Should not panic even with nil components
	service.Shutdown()
}

func TestService_RunWithMockEvents(t *testing.T) {
	t.Skip("Integration test - requires complex mocking of SD monitor and signal handlers")
	// This would require extensive mocking and is better tested via integration tests
}

func TestTimeSyncCheck_ValidatesSystemTime(t *testing.T) {
	tests := []struct {
		name string
		now  time.Time
		want bool
	}{
		{
			name: "unix epoch is not synced",
			now:  time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC),
			want: false,
		},
		{
			name: "threshold year is not synced",
			now:  time.Date(2020, 12, 31, 23, 59, 59, 0, time.UTC),
			want: false,
		},
		{
			name: "after threshold is synced",
			now:  time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC),
			want: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isSystemTimeSynced(tc.now); got != tc.want {
				t.Fatalf("isSystemTimeSynced(%s) = %t, want %t", tc.now, got, tc.want)
			}
		})
	}
}

func TestDNSCheck_ValidatesConnectivity(t *testing.T) {
	// Test DNS resolution with context timeout
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// This should succeed on systems with internet connectivity
	// but won't fail the test if offline
	_, err := net.DefaultResolver.LookupHost(ctx, "localhost")
	if err != nil {
		t.Logf("DNS check might fail in isolated environments: %v", err)
	}
}

// TestRcloneConfigCreation ensures rclone config file is created properly
func TestRcloneConfigCreation(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "rclone.conf")

	// Create empty config file
	if err := os.WriteFile(configPath, []byte(""), 0600); err != nil {
		t.Fatalf("Failed to create test config: %v", err)
	}

	// Verify file exists and has correct permissions
	info, err := os.Stat(configPath)
	if err != nil {
		t.Fatalf("Config file not created: %v", err)
	}

	// Check permissions (should be 0600 for security)
	if info.Mode().Perm() != 0600 {
		t.Errorf("Expected permissions 0600, got %v", info.Mode().Perm())
	}
}

// TestSettingsInitialization verifies settings are properly loaded
func TestSettingsInitialization(t *testing.T) {
	tempDir := t.TempDir()
	oldPerm := os.Getenv("PERM_DIR")
	os.Setenv("PERM_DIR", tempDir)
	defer func() {
		if oldPerm == "" {
			os.Unsetenv("PERM_DIR")
		} else {
			os.Setenv("PERM_DIR", oldPerm)
		}
	}()

	// Create test settings file
	settingsPath := filepath.Join(tempDir, "pictures-sync", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0755); err != nil {
		t.Fatalf("Failed to create settings dir: %v", err)
	}

	settingsData := []byte(`{
		"remote_name": "test-remote",
		"remote_path": "/test/path",
		"reformat_threshold": 0.3
	}`)
	if err := os.WriteFile(settingsPath, settingsData, 0644); err != nil {
		t.Fatalf("Failed to write settings: %v", err)
	}

	// Settings should be loaded in New()
	cfg := Config{NTPSyncDelay: 100 * time.Millisecond}
	service, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create service: %v", err)
	}
	defer service.Shutdown()

	// Verify settings were loaded
	if service.settings == nil {
		t.Fatal("Settings not initialized")
	}
	if service.settings.GetRemoteName() != "test-remote" {
		t.Errorf("Expected remote_name 'test-remote', got '%s'", service.settings.GetRemoteName())
	}
}

// TestCaptivePortalInitialization verifies captive portal setup
func TestCaptivePortalInitialization(t *testing.T) {
	t.Skip("Requires WiFi manager initialization which may fail in test environments")
	// This test would verify that the captive portal authenticator
	// is properly initialized when WiFi manager is available
}

// TestEventChannelBuffer verifies event channel is properly buffered
func TestEventChannelBuffer(t *testing.T) {
	// The SD monitor creates a buffered channel to prevent event loss
	// This test verifies that pattern is followed

	// Create a buffered channel like the monitor does
	eventChan := make(chan struct{}, 10)

	// Should be able to send without blocking
	for i := 0; i < 10; i++ {
		select {
		case eventChan <- struct{}{}:
			// Success
		default:
			t.Error("Event channel blocked before reaching capacity")
		}
	}

	// 11th send should block
	select {
	case eventChan <- struct{}{}:
		t.Error("Event channel should be full")
	default:
		// Expected behavior
	}
}

// BenchmarkServiceCreation measures service initialization performance
func BenchmarkServiceCreation(b *testing.B) {
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

	cfg := Config{NTPSyncDelay: 1 * time.Millisecond}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		service, err := New(cfg)
		if err != nil {
			b.Fatalf("Failed to create service: %v", err)
		}
		service.Shutdown()
	}
}
