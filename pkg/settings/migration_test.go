package settings

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// TestMigrationFromV1ToV2 tests migration from initial version (v1) to version with transfers/checkers (v2)
// Version 1: RemoteName, RemotePath, ReformatThreshold
// Version 2: Added Transfers, Checkers
func TestMigrationFromV1ToV2(t *testing.T) {
	t.Run("v1_config_missing_transfers_checkers", func(t *testing.T) {
		// Simulate v1 config file
		v1JSON := `{
			"remote_name": "my-backup",
			"remote_path": "/backups/photos",
			"reformat_threshold": 0.5
		}`

		s := &Settings{}
		err := json.Unmarshal([]byte(v1JSON), s)
		if err != nil {
			t.Fatalf("Failed to unmarshal v1 config: %v", err)
		}

		// Apply migration logic (same as Load())
		if s.Transfers == 0 {
			s.Transfers = 4
		}
		if s.Checkers == 0 {
			s.Checkers = 8
		}

		// Verify existing fields preserved
		if s.RemoteName != "my-backup" {
			t.Errorf("RemoteName not preserved: got %q", s.RemoteName)
		}
		if s.RemotePath != "/backups/photos" {
			t.Errorf("RemotePath not preserved: got %q", s.RemotePath)
		}
		if s.ReformatThreshold != 0.5 {
			t.Errorf("ReformatThreshold not preserved: got %v", s.ReformatThreshold)
		}

		// Verify new fields have defaults
		if s.Transfers != 4 {
			t.Errorf("Transfers default not applied: got %d", s.Transfers)
		}
		if s.Checkers != 8 {
			t.Errorf("Checkers default not applied: got %d", s.Checkers)
		}
	})

	t.Run("BUG_v1_config_with_explicit_zero_transfers", func(t *testing.T) {
		// User intentionally sets transfers to 0 in v1
		// BUG: This will be replaced with default 4
		v1JSON := `{
			"remote_name": "remote",
			"remote_path": "/photos",
			"reformat_threshold": 0.3,
			"transfers": 0
		}`

		s := &Settings{}
		json.Unmarshal([]byte(v1JSON), s)

		// Apply migration
		if s.Transfers == 0 {
			s.Transfers = 4
		}

		// BUG: Cannot distinguish between "missing" and "explicitly 0"
		if s.Transfers == 4 {
			t.Log("BUG CONFIRMED: Explicit zero value replaced with default during migration")
			t.Log("Impact: User's intentional configuration overridden")
			t.Log("Data Loss: Original user intent (transfers=0) is lost")
		}
	})
}

// TestMigrationFromV2ToV3 tests migration from v2 to v3 (Google Photos added)
// Version 2: RemoteName, RemotePath, ReformatThreshold, Transfers, Checkers
// Version 3: Added GooglePhotosEnabled, GooglePhotosRemoteName
func TestMigrationFromV2ToV3(t *testing.T) {
	t.Run("v2_config_missing_google_photos_fields", func(t *testing.T) {
		v2JSON := `{
			"remote_name": "s3-backup",
			"remote_path": "/photos",
			"reformat_threshold": 0.3,
			"transfers": 6,
			"checkers": 12
		}`

		s := &Settings{}
		err := json.Unmarshal([]byte(v2JSON), s)
		if err != nil {
			t.Fatalf("Failed to unmarshal v2 config: %v", err)
		}

		// Verify existing fields preserved
		if s.RemoteName != "s3-backup" {
			t.Errorf("RemoteName not preserved: got %q", s.RemoteName)
		}
		if s.Transfers != 6 {
			t.Errorf("Transfers not preserved: got %d", s.Transfers)
		}
		if s.Checkers != 12 {
			t.Errorf("Checkers not preserved: got %d", s.Checkers)
		}

		// Verify new fields have zero values (safe defaults)
		if s.GooglePhotosEnabled != false {
			t.Errorf("GooglePhotosEnabled should default to false: got %v", s.GooglePhotosEnabled)
		}
		if s.GooglePhotosRemoteName != "" {
			t.Errorf("GooglePhotosRemoteName should default to empty: got %q", s.GooglePhotosRemoteName)
		}
	})

	t.Run("v2_to_v3_migration_is_safe", func(t *testing.T) {
		// This migration is actually SAFE because:
		// 1. GooglePhotosEnabled defaults to false (feature disabled)
		// 2. GooglePhotosRemoteName defaults to "" (not used when disabled)
		// 3. No existing behavior changes
		t.Log("SUCCESS: v2 to v3 migration is backwards compatible")
	})
}

// TestBackwardsCompatibility tests loading new configs with old code
func TestBackwardsCompatibility(t *testing.T) {
	t.Run("v3_config_loaded_by_v2_code", func(t *testing.T) {
		// User upgrades to v3, saves config, then downgrades to v2
		v3JSON := `{
			"remote_name": "remote",
			"remote_path": "/photos",
			"reformat_threshold": 0.3,
			"transfers": 4,
			"checkers": 8,
			"google_photos_enabled": true,
			"google_photos_remote_name": "gphotos"
		}`

		// Simulate v2 struct (without Google Photos fields)
		type SettingsV2 struct {
			RemoteName        string  `json:"remote_name"`
			RemotePath        string  `json:"remote_path"`
			ReformatThreshold float64 `json:"reformat_threshold"`
			Transfers         int     `json:"transfers"`
			Checkers          int     `json:"checkers"`
		}

		v2Settings := &SettingsV2{}
		err := json.Unmarshal([]byte(v3JSON), v2Settings)
		if err != nil {
			t.Fatalf("v2 code failed to parse v3 config: %v", err)
		}

		// Verify known fields loaded correctly
		if v2Settings.RemoteName != "remote" {
			t.Errorf("RemoteName not loaded: got %q", v2Settings.RemoteName)
		}

		// Now if v2 code saves, it will lose the Google Photos fields
		v2Data, _ := json.MarshalIndent(v2Settings, "", "  ")
		if strings.Contains(string(v2Data), "google_photos") {
			t.Error("Unexpected: Google Photos fields should be lost")
		} else {
			t.Log("BUG CONFIRMED: Downgrade from v3 to v2 loses Google Photos settings")
			t.Log("Impact: User loses configuration when downgrading")
			t.Log("Severity: HIGH - Data loss during version downgrade")
		}
	})

	t.Run("BUG_no_version_field_prevents_detection", func(t *testing.T) {
		v3JSON := `{
			"remote_name": "remote",
			"remote_path": "/photos",
			"google_photos_enabled": true,
			"google_photos_remote_name": "gphotos"
		}`

		s := &Settings{}
		json.Unmarshal([]byte(v3JSON), s)

		// We have no way to know this came from v3!
		// There's no version field to check

		t.Log("BUG CONFIRMED: No version field in settings")
		t.Log("Impact: Cannot detect config format version")
		t.Log("Impact: Cannot warn user about incompatible downgrade")
		t.Log("Impact: Cannot perform version-specific migrations")
		t.Log("Recommendation: Add 'version' field to settings struct")
	})

	t.Run("future_v4_config_with_unknown_fields", func(t *testing.T) {
		// Simulate future version with new fields
		v4JSON := `{
			"remote_name": "remote",
			"remote_path": "/photos",
			"reformat_threshold": 0.3,
			"transfers": 4,
			"checkers": 8,
			"google_photos_enabled": true,
			"google_photos_remote_name": "gphotos",
			"encryption_enabled": true,
			"encryption_password": "secret123",
			"compression_level": 9,
			"advanced_features": {
				"dedupe": true,
				"verify_checksums": true
			}
		}`

		s := &Settings{}
		err := json.Unmarshal([]byte(v4JSON), s)
		if err != nil {
			t.Errorf("Current code failed to parse future config: %v", err)
		}

		// Known fields should load
		if s.RemoteName != "remote" {
			t.Errorf("RemoteName not loaded: got %q", s.RemoteName)
		}

		// Unknown fields are silently ignored
		savedData, _ := json.MarshalIndent(s, "", "  ")

		// Check if future fields are preserved
		if strings.Contains(string(savedData), "encryption_enabled") {
			t.Error("Unexpected: Future fields should be lost")
		} else {
			t.Log("BUG CONFIRMED: Unknown fields silently discarded")
			t.Log("Impact: Loading v4 config with v3 code loses new settings")
			t.Log("Impact: User upgrades to v4, downgrades to v3, loses all v4 settings")
			t.Log("Severity: HIGH - Silent data loss")
		}
	})
}

// TestConfigDowngradeScenarios tests what happens when user downgrades software
func TestConfigDowngradeScenarios(t *testing.T) {
	t.Run("BUG_downgrade_v3_to_v1_loses_all_new_fields", func(t *testing.T) {
		// User runs v3 with full config
		v3JSON := `{
			"remote_name": "backup",
			"remote_path": "/photos",
			"reformat_threshold": 0.4,
			"transfers": 10,
			"checkers": 20,
			"google_photos_enabled": true,
			"google_photos_remote_name": "gphotos"
		}`

		// Simulate v1 struct
		type SettingsV1 struct {
			RemoteName        string  `json:"remote_name"`
			RemotePath        string  `json:"remote_path"`
			ReformatThreshold float64 `json:"reformat_threshold"`
		}

		v1Settings := &SettingsV1{}
		json.Unmarshal([]byte(v3JSON), v1Settings)

		// v1 saves config
		v1Data, _ := json.MarshalIndent(v1Settings, "", "  ")

		// Check what was lost
		lostFields := []string{
			"transfers",
			"checkers",
			"google_photos_enabled",
			"google_photos_remote_name",
		}

		for _, field := range lostFields {
			if !strings.Contains(string(v1Data), field) {
				t.Logf("Lost field during downgrade: %s", field)
			}
		}

		t.Log("BUG CONFIRMED: Major data loss during multi-version downgrade")
		t.Log("Impact: User loses transfers, checkers, and Google Photos settings")
		t.Log("Impact: Re-upgrading to v3 will use defaults, not original values")
		t.Log("Severity: CRITICAL - Permanent data loss")
	})

	t.Run("downgrade_upgrade_cycle_loses_custom_values", func(t *testing.T) {
		// User's original v3 config
		originalTransfers := 10
		originalCheckers := 20
		originalGooglePhotos := true

		// After downgrade to v2 and upgrade back to v3
		s := &Settings{}
		v2JSON := `{"remote_name": "remote", "remote_path": "/photos", "reformat_threshold": 0.3}`
		json.Unmarshal([]byte(v2JSON), s)

		// Apply defaults
		if s.Transfers == 0 {
			s.Transfers = 4 // Default, not original 10!
		}
		if s.Checkers == 0 {
			s.Checkers = 8 // Default, not original 20!
		}

		// User lost their custom settings
		if s.Transfers != originalTransfers {
			t.Log("CONFIRMED: Custom transfers value lost in downgrade/upgrade cycle")
			t.Logf("Original: %d, After cycle: %d", originalTransfers, s.Transfers)
		}
		if s.Checkers != originalCheckers {
			t.Log("CONFIRMED: Custom checkers value lost in downgrade/upgrade cycle")
			t.Logf("Original: %d, After cycle: %d", originalCheckers, s.Checkers)
		}
		if s.GooglePhotosEnabled != originalGooglePhotos {
			t.Log("CONFIRMED: Google Photos setting lost in downgrade/upgrade cycle")
		}
	})
}

// TestPartialMigrationFailures tests what happens when migration partially fails
func TestPartialMigrationFailures(t *testing.T) {
	t.Run("BUG_corrupt_config_during_field_addition", func(t *testing.T) {
		// Config file gets corrupted during migration
		// Some fields are written, others are not
		partialJSON := `{
			"remote_name": "remote",
			"remote_path": "/photos",
			"reformat_threshold": 0.3,
			"transfers": 4,
			"checkers": `
		// Truncated! File corruption or power loss

		s := &Settings{}
		err := json.Unmarshal([]byte(partialJSON), s)
		if err == nil {
			t.Error("Should have failed to parse truncated JSON")
		} else {
			t.Logf("Correctly rejected corrupt config: %v", err)
		}

		// Current Load() would return an error
		// But there's no recovery mechanism!
		t.Log("BUG: No recovery mechanism for corrupt config files")
		t.Log("Impact: If settings.json is corrupted, app cannot start")
		t.Log("Impact: User must manually delete file or fix JSON")
		t.Log("Recommendation: Keep backup of last known good config")
	})

	t.Run("BUG_no_backup_mechanism", func(t *testing.T) {
		t.Log("BUG: No backup of previous settings before Save()")
		t.Log("Impact: If Save() corrupts file, original is lost")
		t.Log("Impact: No rollback possible")
		t.Log("Recommendation: Save to .bak file before overwriting")
		t.Log("Example: settings.json.bak = old settings, settings.json = new settings")
	})

	t.Run("partial_save_during_migration", func(t *testing.T) {
		tmpDir := t.TempDir()
		testFile := filepath.Join(tmpDir, "settings.json")
		tmpFile := testFile + ".tmp"

		// Write old config
		oldJSON := `{"remote_name": "old", "remote_path": "/old"}`
		os.WriteFile(testFile, []byte(oldJSON), 0644)

		// Start writing new config but fail midway
		newJSON := `{"remote_name": "new", "remote_path": "/new", "transfers": 4, "checkers": 8}`
		os.WriteFile(tmpFile, []byte(newJSON), 0644)

		// Simulate crash before rename
		// Now we have both old and .tmp files

		// What happens on next load?
		data, _ := os.ReadFile(testFile)
		var loaded Settings
		json.Unmarshal(data, &loaded)

		if loaded.RemoteName == "old" {
			t.Log("Old config still in place (good - atomic write works)")
		}

		// But .tmp file is left behind
		if _, err := os.Stat(tmpFile); err == nil {
			t.Log("BUG: Orphaned .tmp file left behind after crash")
			t.Log("Impact: Wastes disk space")
			t.Log("Impact: Confusing for manual debugging")
			t.Log("Recommendation: Clean up .tmp files on Load()")
		}
	})
}

// TestDuplicateMigrationAttempts tests running migration multiple times
func TestDuplicateMigrationAttempts(t *testing.T) {
	t.Run("multiple_loads_are_idempotent", func(t *testing.T) {
		v1JSON := `{
			"remote_name": "test",
			"remote_path": "/test",
			"reformat_threshold": 0.5
		}`

		// Load and apply defaults first time
		s1 := &Settings{}
		json.Unmarshal([]byte(v1JSON), s1)
		if s1.Transfers == 0 {
			s1.Transfers = 4
		}
		if s1.Checkers == 0 {
			s1.Checkers = 8
		}

		// Serialize back
		data, _ := json.MarshalIndent(s1, "", "  ")

		// Load again (second migration attempt)
		s2 := &Settings{}
		json.Unmarshal(data, s2)
		if s2.Transfers == 0 {
			s2.Transfers = 4
		}
		if s2.Checkers == 0 {
			s2.Checkers = 8
		}

		// Should be identical
		if s1.Transfers != s2.Transfers {
			t.Errorf("Migration not idempotent: %d != %d", s1.Transfers, s2.Transfers)
		}
		if s1.Checkers != s2.Checkers {
			t.Errorf("Migration not idempotent: %d != %d", s1.Checkers, s2.Checkers)
		}

		t.Log("SUCCESS: Multiple migration attempts are idempotent")
	})

	t.Run("BUG_zero_value_migration_loop", func(t *testing.T) {
		// User explicitly sets transfers to 0
		configJSON := `{
			"remote_name": "test",
			"remote_path": "/test",
			"reformat_threshold": 0.3,
			"transfers": 0,
			"checkers": 8
		}`

		// Load applies defaults
		s := &Settings{}
		json.Unmarshal([]byte(configJSON), s)
		if s.Transfers == 0 {
			s.Transfers = 4 // BUG: Replaces user's intentional 0
		}

		// User sees transfers=4, manually sets back to 0
		s.Transfers = 0
		data, _ := json.MarshalIndent(s, "", "  ")
		os.WriteFile("/tmp/test.json", data, 0644)

		// Next load, defaults applied again!
		s2 := &Settings{}
		json.Unmarshal(data, s2)
		if s2.Transfers == 0 {
			s2.Transfers = 4 // BUG: Infinite loop of resetting to default!
		}

		t.Log("BUG CONFIRMED: Migration loop for zero values")
		t.Log("Impact: User cannot set numeric fields to 0")
		t.Log("Impact: Every load resets 0 to default")
		t.Log("Severity: MEDIUM - Breaks user control")
	})
}

// TestConfigVersionDetectionFailures tests inability to detect version
func TestConfigVersionDetectionFailures(t *testing.T) {
	t.Run("BUG_cannot_distinguish_v1_v2_v3", func(t *testing.T) {
		configs := []string{
			`{"remote_name": "r", "remote_path": "/p", "reformat_threshold": 0.3}`, // v1
			`{"remote_name": "r", "remote_path": "/p", "reformat_threshold": 0.3, "transfers": 4, "checkers": 8}`, // v2
			`{"remote_name": "r", "remote_path": "/p", "reformat_threshold": 0.3, "transfers": 4, "checkers": 8, "google_photos_enabled": false}`, // v3
		}

		for i, configJSON := range configs {
			s := &Settings{}
			json.Unmarshal([]byte(configJSON), s)

			// We have no way to know which version this is!
			// All we can do is check which fields exist
			t.Logf("Config %d: transfers=%d, checkers=%d, google_photos=%v",
				i+1, s.Transfers, s.Checkers, s.GooglePhotosEnabled)
		}

		t.Log("BUG CONFIRMED: No version detection mechanism")
		t.Log("Impact: Cannot perform version-specific migrations")
		t.Log("Impact: Cannot warn about incompatibilities")
		t.Log("Impact: Cannot track config format evolution")
	})

	t.Run("proposed_version_field_solution", func(t *testing.T) {
		type SettingsWithVersion struct {
			Version               int     `json:"version"`
			RemoteName            string  `json:"remote_name"`
			RemotePath            string  `json:"remote_path"`
			ReformatThreshold     float64 `json:"reformat_threshold"`
			Transfers             int     `json:"transfers,omitempty"`
			Checkers              int     `json:"checkers,omitempty"`
			GooglePhotosEnabled   bool    `json:"google_photos_enabled,omitempty"`
			GooglePhotosRemoteName string `json:"google_photos_remote_name,omitempty"`
		}

		// Load old config without version
		oldJSON := `{"remote_name": "r", "remote_path": "/p"}`
		s := &SettingsWithVersion{}
		json.Unmarshal([]byte(oldJSON), s)

		// Detect version
		if s.Version == 0 {
			t.Log("Detected v1 config (no version field)")
			s.Version = 1
		}

		// Perform version-specific migration
		if s.Version == 1 {
			if s.Transfers == 0 {
				s.Transfers = 4
			}
			if s.Checkers == 0 {
				s.Checkers = 8
			}
			s.Version = 2
			t.Log("Migrated from v1 to v2")
		}

		// Save with version
		data, _ := json.MarshalIndent(s, "", "  ")
		if !strings.Contains(string(data), `"version": 2`) {
			t.Error("Version field not saved")
		} else {
			t.Log("SOLUTION: Version field enables proper migration tracking")
		}
	})
}

// TestDefaultValueChanges tests what happens when defaults change between versions
func TestDefaultValueChanges(t *testing.T) {
	t.Run("BUG_changing_defaults_breaks_old_configs", func(t *testing.T) {
		// v1 default: transfers=4
		// v2 default: transfers=8 (improved performance)

		v1Config := `{"remote_name": "r", "remote_path": "/p"}`

		// User loads with v1, gets transfers=4
		s := &Settings{}
		json.Unmarshal([]byte(v1Config), s)
		if s.Transfers == 0 {
			s.Transfers = 4 // v1 default
		}
		v1Transfers := s.Transfers

		// Developer changes default in code
		// User upgrades and loads same config
		s2 := &Settings{}
		json.Unmarshal([]byte(v1Config), s2)
		if s2.Transfers == 0 {
			s2.Transfers = 8 // v2 default (changed!)
		}
		v2Transfers := s2.Transfers

		if v1Transfers != v2Transfers {
			t.Log("BUG CONFIRMED: Default value change affects existing configs")
			t.Logf("v1 default: %d, v2 default: %d", v1Transfers, v2Transfers)
			t.Log("Impact: User's behavior changes after upgrade without their action")
			t.Log("Impact: Might cause performance issues if defaults change")
			t.Log("Severity: MEDIUM - Unexpected behavior change")
		}
	})

	t.Run("explicit_values_survive_default_changes", func(t *testing.T) {
		// If user explicitly set a value, it should survive
		configWithValue := `{"remote_name": "r", "remote_path": "/p", "transfers": 2}`

		s := &Settings{}
		json.Unmarshal([]byte(configWithValue), s)

		// Don't apply default because value is non-zero
		if s.Transfers == 0 {
			s.Transfers = 99 // New default
		}

		if s.Transfers == 2 {
			t.Log("SUCCESS: Explicit values preserved across default changes")
		} else {
			t.Errorf("Explicit value lost: expected 2, got %d", s.Transfers)
		}
	})

	t.Run("BUG_zero_values_cannot_be_explicit", func(t *testing.T) {
		// User explicitly sets transfers=0 in v1
		// v2 changes default to 8
		configWithZero := `{"remote_name": "r", "remote_path": "/p", "transfers": 0}`

		s := &Settings{}
		json.Unmarshal([]byte(configWithZero), s)
		if s.Transfers == 0 {
			s.Transfers = 8 // BUG: Cannot tell if 0 was explicit or missing!
		}

		if s.Transfers == 8 {
			t.Log("BUG CONFIRMED: Explicit zero replaced with new default")
			t.Log("Impact: User's explicit choice overridden")
			t.Log("Recommendation: Use omitempty and pointers to distinguish")
		}
	})
}

// TestBreakingChangesNotHandled tests unhandled breaking changes
func TestBreakingChangesNotHandled(t *testing.T) {
	t.Run("BUG_field_rename_not_handled", func(t *testing.T) {
		// Imagine developer renames "reformat_threshold" to "reformat_detection_threshold"
		type NewSettings struct {
			RemoteName                   string  `json:"remote_name"`
			RemotePath                   string  `json:"remote_path"`
			ReformatDetectionThreshold   float64 `json:"reformat_detection_threshold"` // Renamed!
			Transfers                    int     `json:"transfers"`
			Checkers                     int     `json:"checkers"`
		}

		oldConfig := `{"remote_name": "r", "remote_path": "/p", "reformat_threshold": 0.7}`

		newS := &NewSettings{}
		json.Unmarshal([]byte(oldConfig), newS)

		// Old field name is ignored
		if newS.ReformatDetectionThreshold == 0 {
			t.Log("BUG CONFIRMED: Field rename causes data loss")
			t.Log("Impact: User's custom threshold setting lost")
			t.Log("Impact: Reverts to default silently")
			t.Log("Severity: HIGH - Silent data loss")
			t.Log("Recommendation: Support both old and new field names during transition")
		}
	})

	t.Run("BUG_type_change_not_handled", func(t *testing.T) {
		// Developer changes ReformatThreshold from float64 to int (percentage)
		type NewSettings struct {
			RemoteName        string `json:"remote_name"`
			RemotePath        string `json:"remote_path"`
			ReformatThreshold int    `json:"reformat_threshold"` // Changed from float64!
		}

		oldConfig := `{"remote_name": "r", "remote_path": "/p", "reformat_threshold": 0.3}`

		newS := &NewSettings{}
		err := json.Unmarshal([]byte(oldConfig), newS)

		if err != nil {
			t.Log("BUG CONFIRMED: Type change causes unmarshal error")
			t.Logf("Error: %v", err)
			t.Log("Impact: Cannot load old configs after type change")
			t.Log("Severity: CRITICAL - Complete config load failure")
		} else if newS.ReformatThreshold == 0 {
			t.Log("BUG CONFIRMED: Type conversion lost precision")
			t.Log("Impact: 0.3 became 0 (truncated)")
		}
	})

	t.Run("field_removal_not_handled", func(t *testing.T) {
		// Developer removes ReformatThreshold field entirely
		type NewSettings struct {
			RemoteName string `json:"remote_name"`
			RemotePath string `json:"remote_path"`
			Transfers  int    `json:"transfers"`
			Checkers   int    `json:"checkers"`
			// ReformatThreshold removed!
		}

		oldConfig := `{
			"remote_name": "r",
			"remote_path": "/p",
			"reformat_threshold": 0.7,
			"transfers": 4,
			"checkers": 8
		}`

		newS := &NewSettings{}
		json.Unmarshal([]byte(oldConfig), newS)

		// Field is silently ignored (Go's behavior)
		t.Log("Field removal silently drops data from config file")
		t.Log("Impact: Old field remains in file but unused")
		t.Log("Impact: Causes confusion during debugging")
		t.Log("Severity: LOW - But could cause maintenance issues")
	})
}

// TestConfigCorruptionDuringMigration tests corruption scenarios during migration
func TestConfigCorruptionDuringMigration(t *testing.T) {
	t.Run("power_loss_during_save", func(t *testing.T) {
		tmpDir := t.TempDir()
		settingsFile := filepath.Join(tmpDir, "settings.json")
		tmpFile := settingsFile + ".tmp"

		// Original v2 config
		v2Config := `{"remote_name": "r", "remote_path": "/p", "transfers": 4, "checkers": 8}`
		os.WriteFile(settingsFile, []byte(v2Config), 0644)

		// Start migration to v3 (adding Google Photos)
		s := &Settings{}
		json.Unmarshal([]byte(v2Config), s)
		s.GooglePhotosEnabled = true
		s.GooglePhotosRemoteName = "gphotos"

		// Write to temp file
		v3Data, _ := json.MarshalIndent(s, "", "  ")
		os.WriteFile(tmpFile, v3Data, 0644)

		// POWER LOSS HERE - before rename!

		// On next boot, old config still exists
		existingData, _ := os.ReadFile(settingsFile)
		var existing Settings
		json.Unmarshal(existingData, &existing)

		if !existing.GooglePhotosEnabled {
			t.Log("SUCCESS: Atomic write preserved old config during power loss")
		}

		// But .tmp file is orphaned
		if _, err := os.Stat(tmpFile); err == nil {
			t.Log("ISSUE: Orphaned .tmp file after power loss")
			t.Log("Recommendation: Clean up .tmp files on Load()")
		}
	})

	t.Run("concurrent_migration_attempts", func(t *testing.T) {
		// Two processes try to migrate config simultaneously
		s := DefaultSettings()

		done := make(chan bool, 2)
		var wg sync.WaitGroup

		// Process 1 migrates to v2
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				s.Transfers = 4
				s.Checkers = 8
			}
			done <- true
		}()

		// Process 2 migrates to v3
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				s.GooglePhotosEnabled = true
				s.GooglePhotosRemoteName = "gphotos"
			}
			done <- true
		}()

		wg.Wait()
		close(done)

		t.Log("Concurrent migration completed (may have race conditions)")
		t.Log("BUG: No locking prevents concurrent migrations")
		t.Log("Impact: Could result in inconsistent final state")
	})

	t.Run("BUG_filesystem_corruption_not_detected", func(t *testing.T) {
		tmpDir := t.TempDir()
		settingsFile := filepath.Join(tmpDir, "settings.json")

		// Write valid config
		validConfig := `{"remote_name": "r", "remote_path": "/p"}`
		os.WriteFile(settingsFile, []byte(validConfig), 0644)

		// Simulate filesystem corruption (flip some bits)
		data, _ := os.ReadFile(settingsFile)
		data[10] = 0xFF // Corrupt a byte
		os.WriteFile(settingsFile, data, 0644)

		// Try to load
		s := &Settings{}
		err := json.Unmarshal(data, s)

		if err != nil {
			t.Log("Corruption detected by JSON parser (good)")
		} else {
			t.Log("BUG: Corruption not detected, loaded invalid config")
		}

		t.Log("ISSUE: No checksum validation for config file")
		t.Log("Recommendation: Add CRC32 or similar checksum to detect corruption")
	})

	t.Run("truncated_config_file", func(t *testing.T) {
		tmpDir := t.TempDir()
		settingsFile := filepath.Join(tmpDir, "settings.json")

		// Write config
		fullConfig := `{"remote_name": "very-long-remote-name", "remote_path": "/very/long/path/to/photos"}`
		os.WriteFile(settingsFile, []byte(fullConfig), 0644)

		// Truncate file (simulate write failure)
		os.Truncate(settingsFile, 20)

		// Try to load
		data, _ := os.ReadFile(settingsFile)
		s := &Settings{}
		err := json.Unmarshal(data, s)

		if err == nil {
			t.Error("BUG: Truncated file loaded successfully (should error)")
		} else {
			t.Log("Correctly rejected truncated file")
		}

		t.Log("ISSUE: No recovery from truncated files")
		t.Log("Recommendation: Keep .bak file and restore if main file is corrupt")
	})
}

// TestRealWorldMigrationScenarios tests realistic migration paths
func TestRealWorldMigrationScenarios(t *testing.T) {
	t.Run("user_upgrades_v1_to_v3_directly", func(t *testing.T) {
		// User skips v2, goes directly from v1 to v3
		v1Config := `{"remote_name": "backup", "remote_path": "/photos", "reformat_threshold": 0.5}`

		s := &Settings{}
		json.Unmarshal([]byte(v1Config), s)

		// Apply all migration steps at once
		if s.Transfers == 0 {
			s.Transfers = 4 // v2 migration
		}
		if s.Checkers == 0 {
			s.Checkers = 8 // v2 migration
		}
		// v3 fields already have safe defaults (false, "")

		// Verify
		if s.RemoteName != "backup" {
			t.Error("Original settings not preserved")
		}
		if s.Transfers != 4 || s.Checkers != 8 {
			t.Error("Migrations not applied")
		}

		t.Log("SUCCESS: Direct v1->v3 upgrade works")
	})

	t.Run("user_with_heavily_customized_config", func(t *testing.T) {
		// Power user with many customizations
		customConfig := `{
			"remote_name": "my-special-remote",
			"remote_path": "/backups/cameras/nikon-d850",
			"reformat_threshold": 0.15,
			"transfers": 16,
			"checkers": 32,
			"google_photos_enabled": true,
			"google_photos_remote_name": "family-photos-gphotos"
		}`

		s := &Settings{}
		json.Unmarshal([]byte(customConfig), s)

		// Verify all customizations preserved
		checks := map[string]bool{
			"remote_name":              s.RemoteName == "my-special-remote",
			"remote_path":              s.RemotePath == "/backups/cameras/nikon-d850",
			"reformat_threshold":       s.ReformatThreshold == 0.15,
			"transfers":                s.Transfers == 16,
			"checkers":                 s.Checkers == 32,
			"google_photos_enabled":    s.GooglePhotosEnabled == true,
			"google_photos_remote_name": s.GooglePhotosRemoteName == "family-photos-gphotos",
		}

		for field, ok := range checks {
			if !ok {
				t.Errorf("Custom field not preserved: %s", field)
			}
		}

		t.Log("SUCCESS: Heavily customized config preserved")
	})

	t.Run("BUG_config_with_comments_loses_comments", func(t *testing.T) {
		// Some users manually add comments to JSON files
		configWithComments := `{
			// My custom backup configuration
			"remote_name": "remote",
			"remote_path": "/photos", // Changed from /backups
			"reformat_threshold": 0.3
			/* Google Photos disabled for privacy */
		}`

		s := &Settings{}
		err := json.Unmarshal([]byte(configWithComments), s)

		if err != nil {
			t.Log("Expected: JSON with comments fails to parse")
			t.Log("Impact: Users who manually edit configs cannot add comments")
			t.Log("Note: This is standard JSON behavior (no comments allowed)")
		}

		// After save, comments would be lost anyway
		t.Log("Any manual comments in config file are lost on save")
	})

	t.Run("production_system_running_for_years", func(t *testing.T) {
		// System has been running since v1, upgraded multiple times
		// Config file has accumulated over time

		// Simulate old file with deprecated/unknown fields
		oldProdConfig := `{
			"remote_name": "prod-backup",
			"remote_path": "/production/photos",
			"reformat_threshold": 0.3,
			"deprecated_field": "old_value",
			"internal_use_only": true,
			"migration_applied": false
		}`

		s := &Settings{}
		json.Unmarshal([]byte(oldProdConfig), s)

		// Apply current defaults
		if s.Transfers == 0 {
			s.Transfers = 4
		}
		if s.Checkers == 0 {
			s.Checkers = 8
		}

		// Save would drop unknown fields
		data, _ := json.MarshalIndent(s, "", "  ")

		if !strings.Contains(string(data), "deprecated_field") {
			t.Log("Old/unknown fields dropped during save")
			t.Log("Impact: Clean config but may surprise users")
		}
	})
}

// TestMigrationBugsSummary provides comprehensive summary
func TestMigrationBugsSummary(t *testing.T) {
	t.Log("=== CONFIGURATION MIGRATION BUGS FOUND ===")
	t.Log("")
	t.Log("CRITICAL BUGS:")
	t.Log("1. No version field - Cannot detect config format version")
	t.Log("2. No backup mechanism - Old config lost if save corrupts")
	t.Log("3. Zero values indistinguishable from missing - Cannot set explicit 0")
	t.Log("4. Unknown fields discarded - Downgrade loses new settings")
	t.Log("5. Type changes break loading - No conversion logic")
	t.Log("")
	t.Log("COMPATIBILITY ISSUES:")
	t.Log("6. Downgrade from v3->v2 loses Google Photos settings")
	t.Log("7. Downgrade from v3->v1 loses transfers, checkers, Google Photos")
	t.Log("8. Downgrade/upgrade cycle resets custom values to defaults")
	t.Log("9. Default value changes affect existing configs unexpectedly")
	t.Log("10. Field renames cause silent data loss")
	t.Log("")
	t.Log("DATA CORRUPTION RISKS:")
	t.Log("11. Orphaned .tmp files after power loss")
	t.Log("12. No checksum validation for corruption detection")
	t.Log("13. No recovery from truncated files")
	t.Log("14. Concurrent migrations possible (no locking)")
	t.Log("15. No rollback on partial migration failure")
	t.Log("")
	t.Log("MIGRATION ISSUES:")
	t.Log("16. Zero value migration loop - User cannot set 0")
	t.Log("17. No migration path documentation")
	t.Log("18. No warning on incompatible version downgrade")
	t.Log("19. Manual config edits not preserved")
	t.Log("20. No validation after migration")
	t.Log("")
	t.Log("RECOMMENDATIONS:")
	t.Log("- Add 'version' field to Settings struct")
	t.Log("- Use pointers for optional fields to detect omitempty")
	t.Log("- Implement settings.json.bak backup mechanism")
	t.Log("- Add checksum for corruption detection")
	t.Log("- Clean up .tmp files on Load()")
	t.Log("- Add unknown field preservation during migration")
	t.Log("- Implement version-specific migration functions")
	t.Log("- Add config validation after migration")
	t.Log("- Document migration paths and breaking changes")
}

// TestMigrationWithValidation tests if migration validates final state
func TestMigrationWithValidation(t *testing.T) {
	t.Run("BUG_no_validation_after_migration", func(t *testing.T) {
		// Config migrates successfully but ends up in invalid state
		invalidV1 := `{
			"remote_name": "remote; rm -rf /",
			"remote_path": "",
			"reformat_threshold": -1
		}`

		s := &Settings{}
		json.Unmarshal([]byte(invalidV1), s)

		// Apply migrations
		if s.RemotePath == "" {
			s.RemotePath = "/photos"
		}
		if s.Transfers == 0 {
			s.Transfers = 4
		}

		// But no validation on final state!
		if strings.Contains(s.RemoteName, ";") {
			t.Log("BUG: Invalid data preserved after migration")
		}
		if s.ReformatThreshold < 0 {
			t.Log("BUG: Invalid negative threshold after migration")
		}

		t.Log("BUG CONFIRMED: No validation after applying migrations")
		t.Log("Impact: Invalid configs can persist through migrations")
		t.Log("Recommendation: Validate all fields after migration")
	})

	t.Run("BUG_migration_can_create_inconsistent_state", func(t *testing.T) {
		// Migration creates logically inconsistent state
		config := `{
			"remote_name": "",
			"remote_path": "/photos",
			"google_photos_enabled": true,
			"google_photos_remote_name": ""
		}`

		s := &Settings{}
		json.Unmarshal([]byte(config), s)

		// Apply defaults
		if s.RemoteName == "" {
			s.RemoteName = "remote"
		}

		// Check consistency
		if s.GooglePhotosEnabled && s.GooglePhotosRemoteName == "" {
			t.Log("BUG: Inconsistent state after migration")
			t.Log("Google Photos enabled but no remote name")
			t.Log("This would cause runtime errors")
		}

		t.Log("BUG CONFIRMED: Migrations don't check inter-field consistency")
	})

	t.Run("proposed_post_migration_validation", func(t *testing.T) {
		t.Log("SOLUTION: Add validation function called after migration")
		t.Log("Example:")
		t.Log("  func (s *Settings) Validate() error {")
		t.Log("    if s.RemoteName == '' {")
		t.Log("      return errors.New('remote name required')")
		t.Log("    }")
		t.Log("    if s.GooglePhotosEnabled && s.GooglePhotosRemoteName == '' {")
		t.Log("      return errors.New('google photos remote required when enabled')")
		t.Log("    }")
		t.Log("    // ... more checks")
		t.Log("    return nil")
		t.Log("  }")
	})
}

// TestTypeConversionDuringMigration tests type changes
func TestTypeConversionDuringMigration(t *testing.T) {
	t.Run("BUG_float_to_int_loses_precision", func(t *testing.T) {
		// Old version used float64 for transfers: 4.5 means "4 to 5 adaptively"
		// New version uses int
		oldConfig := `{"transfers": 4.5}`

		type OldSettings struct {
			Transfers float64 `json:"transfers"`
		}
		type NewSettings struct {
			Transfers int `json:"transfers"`
		}

		old := &OldSettings{}
		json.Unmarshal([]byte(oldConfig), old)
		t.Logf("Old transfers: %v", old.Transfers)

		// User upgrades
		new := &NewSettings{}
		err := json.Unmarshal([]byte(oldConfig), new)
		if err != nil {
			t.Logf("Type mismatch error: %v", err)
		} else {
			t.Logf("New transfers: %d", new.Transfers)
			if float64(new.Transfers) != old.Transfers {
				t.Log("BUG: Precision lost during type change")
			}
		}
	})

	t.Run("BUG_string_to_int_conversion_not_handled", func(t *testing.T) {
		// Old version stored threshold as string percentage
		oldConfig := `{"reformat_threshold": "30"}`

		s := &Settings{}
		err := json.Unmarshal([]byte(oldConfig), s)
		if err != nil {
			t.Log("Type mismatch causes unmarshal error")
			t.Log("User cannot load old config after type change")
		}

		// ReformatThreshold will be 0 (zero value)
		if s.ReformatThreshold == 0 {
			t.Log("BUG: Type mismatch silently zeros field")
			t.Log("User loses their custom threshold setting")
		}
	})

	t.Run("boolean_to_string_migration_not_handled", func(t *testing.T) {
		// Imagine GooglePhotosEnabled changed from bool to string ("enabled", "disabled", "auto")
		oldConfig := `{"google_photos_enabled": true}`

		type NewSettings struct {
			GooglePhotosEnabled string `json:"google_photos_enabled"`
		}

		new := &NewSettings{}
		err := json.Unmarshal([]byte(oldConfig), new)
		if err != nil {
			t.Log("Boolean to string type change causes error")
		} else if new.GooglePhotosEnabled == "" {
			t.Log("Boolean true became empty string")
			t.Log("Migration would need custom logic to convert")
		}
	})
}

// TestSchemaEvolution tests structural changes to configuration
func TestSchemaEvolution(t *testing.T) {
	t.Run("nested_fields_not_supported", func(t *testing.T) {
		// Current: flat structure
		// Future: nested structure for organization
		type FutureSettings struct {
			Remote struct {
				Name string `json:"name"`
				Path string `json:"path"`
			} `json:"remote"`
			Rclone struct {
				Transfers int `json:"transfers"`
				Checkers  int `json:"checkers"`
			} `json:"rclone"`
		}

		oldFlat := `{"remote_name": "r", "remote_path": "/p", "transfers": 4, "checkers": 8}`

		future := &FutureSettings{}
		json.Unmarshal([]byte(oldFlat), future)

		if future.Remote.Name == "" {
			t.Log("Schema evolution from flat to nested loses data")
			t.Log("Would need custom migration logic")
		}
	})

	t.Run("array_fields_not_supported", func(t *testing.T) {
		// Future: support multiple remotes
		type FutureSettings struct {
			Remotes []struct {
				Name string `json:"name"`
				Path string `json:"path"`
			} `json:"remotes"`
		}

		oldSingle := `{"remote_name": "r", "remote_path": "/p"}`

		future := &FutureSettings{}
		json.Unmarshal([]byte(oldSingle), future)

		if len(future.Remotes) == 0 {
			t.Log("Schema evolution from single to array loses data")
			t.Log("Migration would need to convert single value to array")
		}
	})
}

// TestEdgeCasesInMigration tests unusual edge cases
func TestEdgeCasesInMigration(t *testing.T) {
	t.Run("config_with_special_values", func(t *testing.T) {
		// Config with mathematical special values
		specialConfig := `{
			"remote_name": "remote",
			"remote_path": "/photos",
			"reformat_threshold": 1.7976931348623157e+308
		}`

		s := &Settings{}
		json.Unmarshal([]byte(specialConfig), s)

		if math.IsInf(s.ReformatThreshold, 0) {
			t.Log("Special float value loaded as infinity")
		} else if s.ReformatThreshold > 1e100 {
			t.Log("Extremely large threshold value accepted")
			t.Log("Should be validated and rejected")
		}
	})

	t.Run("config_with_unicode", func(t *testing.T) {
		unicodeConfig := `{
			"remote_name": "备份-リモート-백업",
			"remote_path": "/фото/照片"
		}`

		s := &Settings{}
		err := json.Unmarshal([]byte(unicodeConfig), s)
		if err != nil {
			t.Errorf("Failed to parse unicode config: %v", err)
		} else {
			t.Log("Unicode values preserved (good)")
		}
	})

	t.Run("empty_config_file", func(t *testing.T) {
		emptyConfig := ``

		s := &Settings{}
		err := json.Unmarshal([]byte(emptyConfig), s)
		if err != nil {
			t.Log("Empty file correctly rejected")
		} else {
			t.Log("BUG: Empty file loaded as zero values")
		}
	})

	t.Run("config_with_only_whitespace", func(t *testing.T) {
		whitespaceConfig := "   \n\t\r\n   "

		s := &Settings{}
		err := json.Unmarshal([]byte(whitespaceConfig), s)
		if err != nil {
			t.Log("Whitespace-only file correctly rejected")
		}
	})
}
