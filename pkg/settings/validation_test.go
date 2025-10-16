package settings

import (
	"math"
	"testing"
)

func TestValidateRemoteName(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantError bool
	}{
		{"valid remote name", "my-remote", false},
		{"empty remote name", "", true},
		{"whitespace only", "   ", true},
		{"leading whitespace", " remote", true},
		{"trailing whitespace", "remote ", true},
		{"null byte", "remote\x00", true},
		{"newline", "remote\n", true},
		{"semicolon", "remote;", true},
		{"pipe", "remote|command", true},
		{"too long", string(make([]byte, MaxRemoteNameLength+1)), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRemoteName(tt.input)
			if (err != nil) != tt.wantError {
				t.Errorf("ValidateRemoteName(%q) error = %v, wantError %v", tt.input, err, tt.wantError)
			}
		})
	}
}

func TestValidateRemotePath(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantError bool
	}{
		{"valid path", "/photos", false},
		{"valid nested path", "/backups/photos", false},
		{"empty path", "", true},
		{"whitespace only", "   ", true},
		{"path traversal", "/photos/../etc", true},
		{"null byte", "/photos\x00", true},
		{"newline", "/photos\n", true},
		{"too long", "/" + string(make([]byte, MaxRemotePathLength)), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRemotePath(tt.input)
			if (err != nil) != tt.wantError {
				t.Errorf("ValidateRemotePath(%q) error = %v, wantError %v", tt.input, err, tt.wantError)
			}
		})
	}
}

func TestValidateReformatThreshold(t *testing.T) {
	tests := []struct {
		name      string
		input     float64
		wantError bool
	}{
		{"valid threshold", 0.3, false},
		{"zero threshold", 0.0, false},
		{"one threshold", 1.0, false},
		{"negative threshold", -0.1, true},
		{"above one", 1.5, true},
		{"NaN", math.NaN(), true},
		{"positive infinity", math.Inf(1), true},
		{"negative infinity", math.Inf(-1), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateReformatThreshold(tt.input)
			if (err != nil) != tt.wantError {
				t.Errorf("ValidateReformatThreshold(%v) error = %v, wantError %v", tt.input, err, tt.wantError)
			}
		})
	}
}

func TestValidateTransfers(t *testing.T) {
	tests := []struct {
		name      string
		input     int
		wantError bool
	}{
		{"valid transfers", 4, false},
		{"minimum transfers", MinTransfers, false},
		{"maximum transfers", MaxTransfers, false},
		{"zero transfers", 0, true},
		{"negative transfers", -1, true},
		{"above maximum", MaxTransfers + 1, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateTransfers(tt.input)
			if (err != nil) != tt.wantError {
				t.Errorf("ValidateTransfers(%d) error = %v, wantError %v", tt.input, err, tt.wantError)
			}
		})
	}
}

func TestValidateCheckers(t *testing.T) {
	tests := []struct {
		name      string
		input     int
		wantError bool
	}{
		{"valid checkers", 8, false},
		{"minimum checkers", MinCheckers, false},
		{"maximum checkers", MaxCheckers, false},
		{"zero checkers", 0, true},
		{"negative checkers", -1, true},
		{"above maximum", MaxCheckers + 1, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateCheckers(tt.input)
			if (err != nil) != tt.wantError {
				t.Errorf("ValidateCheckers(%d) error = %v, wantError %v", tt.input, err, tt.wantError)
			}
		})
	}
}

func TestValidateGooglePhotos(t *testing.T) {
	tests := []struct {
		name       string
		enabled    bool
		remoteName string
		wantError  bool
	}{
		{"disabled with no remote", false, "", false},
		{"disabled with remote", false, "gphotos", false},
		{"enabled with valid remote", true, "gphotos", false},
		{"enabled with empty remote", true, "", true},
		{"enabled with invalid remote", true, "bad;remote", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateGooglePhotos(tt.enabled, tt.remoteName)
			if (err != nil) != tt.wantError {
				t.Errorf("ValidateGooglePhotos(%v, %q) error = %v, wantError %v", tt.enabled, tt.remoteName, err, tt.wantError)
			}
		})
	}
}

func TestSettingsValidate(t *testing.T) {
	t.Run("default settings are valid", func(t *testing.T) {
		s := DefaultSettings()
		if err := s.Validate(); err != nil {
			t.Errorf("DefaultSettings().Validate() error = %v, want nil", err)
		}
	})

	t.Run("invalid remote name", func(t *testing.T) {
		s := DefaultSettings()
		s.RemoteName = "bad;name"
		if err := s.Validate(); err == nil {
			t.Error("Expected validation error for invalid remote name")
		}
	})

	t.Run("invalid threshold", func(t *testing.T) {
		s := DefaultSettings()
		s.ReformatThreshold = -1.0
		if err := s.Validate(); err == nil {
			t.Error("Expected validation error for negative threshold")
		}
	})

	t.Run("google photos enabled without remote", func(t *testing.T) {
		s := DefaultSettings()
		s.GooglePhotosEnabled = true
		s.GooglePhotosRemoteName = ""
		if err := s.Validate(); err == nil {
			t.Error("Expected validation error for Google Photos enabled without remote")
		}
	})
}

func TestSettersValidation(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("SetRemote rejects invalid names", func(t *testing.T) {
		s := DefaultSettings()
		err := s.SetRemote("bad;name", "/photos")
		if err == nil {
			t.Error("Expected SetRemote to reject invalid remote name")
		}
	})

	t.Run("SetRemote rejects invalid paths", func(t *testing.T) {
		s := DefaultSettings()
		err := s.SetRemote("remote", "/photos/../etc")
		if err == nil {
			t.Error("Expected SetRemote to reject path traversal")
		}
	})

	t.Run("SetReformatThreshold rejects invalid values", func(t *testing.T) {
		s := DefaultSettings()
		err := s.SetReformatThreshold(-1.0)
		if err == nil {
			t.Error("Expected SetReformatThreshold to reject negative value")
		}
	})

	t.Run("SetTransfers rejects invalid values", func(t *testing.T) {
		s := DefaultSettings()
		err := s.SetTransfers(0)
		if err == nil {
			t.Error("Expected SetTransfers to reject zero")
		}
	})

	t.Run("SetCheckers rejects invalid values", func(t *testing.T) {
		s := DefaultSettings()
		err := s.SetCheckers(-1)
		if err == nil {
			t.Error("Expected SetCheckers to reject negative value")
		}
	})

	t.Run("SetGooglePhotos rejects enabled without remote", func(t *testing.T) {
		s := DefaultSettings()
		err := s.SetGooglePhotos(true, "")
		if err == nil {
			t.Error("Expected SetGooglePhotos to reject enabled without remote name")
		}
	})

	_ = tmpDir // Avoid unused variable warning
}

func TestHelperMethods(t *testing.T) {
	s := DefaultSettings()
	s.RemoteName = "test-remote"
	s.RemotePath = "/test-path"
	s.GooglePhotosEnabled = true
	s.GooglePhotosRemoteName = "gphotos"

	t.Run("GetRemoteDestination", func(t *testing.T) {
		dest := s.GetRemoteDestination("card-123")
		expected := "test-remote:/test-path/card-123/DCIM/"
		if dest != expected {
			t.Errorf("GetRemoteDestination() = %q, want %q", dest, expected)
		}
	})

	t.Run("GetGooglePhotosDestination enabled", func(t *testing.T) {
		dest := s.GetGooglePhotosDestination()
		expected := "gphotos:"
		if dest != expected {
			t.Errorf("GetGooglePhotosDestination() = %q, want %q", dest, expected)
		}
	})

	t.Run("GetGooglePhotosDestination disabled", func(t *testing.T) {
		s.GooglePhotosEnabled = false
		dest := s.GetGooglePhotosDestination()
		if dest != "" {
			t.Errorf("GetGooglePhotosDestination() = %q, want empty", dest)
		}
	})

	t.Run("Clone", func(t *testing.T) {
		s2 := s.Clone()
		if s2.RemoteName != s.RemoteName {
			t.Error("Clone did not copy RemoteName")
		}
		if s2.RemotePath != s.RemotePath {
			t.Error("Clone did not copy RemotePath")
		}
		// Modify clone and verify original is unchanged
		s2.RemoteName = "different"
		if s.RemoteName == "different" {
			t.Error("Clone is not independent")
		}
	})

	t.Run("ToJSON", func(t *testing.T) {
		json := s.ToJSON()
		if json["remote_name"] != s.RemoteName {
			t.Error("ToJSON did not include remote_name")
		}
		if json["remote_path"] != s.RemotePath {
			t.Error("ToJSON did not include remote_path")
		}
	})
}
