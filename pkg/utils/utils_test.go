package utils

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ========== format.go tests ==========

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		name     string
		bytes    int64
		expected string
	}{
		{"zero bytes", 0, "0 B"},
		{"less than 1 KB", 500, "500 B"},
		{"exactly 1 KB", 1024, "1.0 KB"},
		{"1.5 KB", 1536, "1.5 KB"},
		{"1 MB", 1024 * 1024, "1.0 MB"},
		{"2.5 MB", 1024 * 1024 * 2.5, "2.5 MB"},
		{"1 GB", 1024 * 1024 * 1024, "1.0 GB"},
		{"1.5 TB", 1024 * 1024 * 1024 * 1024 * 1.5, "1.5 TB"},
		{"large value", 5 * 1024 * 1024 * 1024 * 1024, "5.0 TB"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatBytes(tt.bytes)
			if result != tt.expected {
				t.Errorf("FormatBytes(%d) = %s, expected %s", tt.bytes, result, tt.expected)
			}
		})
	}
}

func TestFormatBytesFloat(t *testing.T) {
	tests := []struct {
		name     string
		bytes    float64
		expected string
	}{
		{"0.5 KB", 512.5, "512 B"},
		{"1.5 KB", 1536.7, "1.5 KB"},
		{"2.3 MB", 2.3 * 1024 * 1024, "2.3 MB"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatBytesFloat(tt.bytes)
			if result != tt.expected {
				t.Errorf("FormatBytesFloat(%.1f) = %s, expected %s", tt.bytes, result, tt.expected)
			}
		})
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		name     string
		seconds  int
		expected string
	}{
		{"negative", -10, "0s"},
		{"zero", 0, "0s"},
		{"30 seconds", 30, "30s"},
		{"59 seconds", 59, "59s"},
		{"1 minute", 60, "1m 0s"},
		{"1 minute 30 seconds", 90, "1m 30s"},
		{"59 minutes", 3540, "59m 0s"},
		{"1 hour", 3600, "1h 0m"},
		{"1 hour 30 minutes", 5400, "1h 30m"},
		{"2 hours 45 minutes", 9900, "2h 45m"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatDuration(tt.seconds)
			if result != tt.expected {
				t.Errorf("FormatDuration(%d) = %s, expected %s", tt.seconds, result, tt.expected)
			}
		})
	}
}

func TestFormatDurationTime(t *testing.T) {
	duration := 90 * time.Second
	expected := "1m 30s"
	result := FormatDurationTime(duration)
	if result != expected {
		t.Errorf("FormatDurationTime(90s) = %s, expected %s", result, expected)
	}
}

func TestFormatPercentage(t *testing.T) {
	tests := []struct {
		name     string
		value    float64
		expected string
	}{
		{"zero", 0.0, "0.0%"},
		{"25%", 25.0, "25.0%"},
		{"50.5%", 50.5, "50.5%"},
		{"100%", 100.0, "100.0%"},
		{"decimal precision", 33.33, "33.3%"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatPercentage(tt.value)
			if result != tt.expected {
				t.Errorf("FormatPercentage(%.2f) = %s, expected %s", tt.value, result, tt.expected)
			}
		})
	}
}

func TestFormatSpeed(t *testing.T) {
	tests := []struct {
		name           string
		bytesPerSecond float64
		expected       string
	}{
		{"slow", 100, "100 B/s"},
		{"1 KB/s", 1024, "1.0 KB/s"},
		{"5.5 MB/s", 5.5 * 1024 * 1024, "5.5 MB/s"},
		{"100 MB/s", 100 * 1024 * 1024, "100.0 MB/s"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatSpeed(tt.bytesPerSecond)
			if result != tt.expected {
				t.Errorf("FormatSpeed(%.0f) = %s, expected %s", tt.bytesPerSecond, result, tt.expected)
			}
		})
	}
}

// ========== path.go tests ==========

func TestValidatePath(t *testing.T) {
	tests := []struct {
		name      string
		path      string
		shouldErr bool
	}{
		{"valid relative path", "photos/2024", false},
		{"valid filename", "photo.jpg", false},
		{"directory traversal", "../etc/passwd", true},
		{"hidden traversal", "photos/../../../etc", true},
		{"absolute path", "/etc/passwd", true},
		{"windows absolute", "C:\\Users\\test", true},
		{"windows traversal", "..\\..\\windows", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePath(tt.path)
			if tt.shouldErr && err == nil {
				t.Errorf("ValidatePath(%s) should return error", tt.path)
			}
			if !tt.shouldErr && err != nil {
				t.Errorf("ValidatePath(%s) unexpected error: %v", tt.path, err)
			}
		})
	}
}

func TestSanitizePath(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected string
	}{
		{"absolute to relative", "/photos/2024", "photos/2024"},
		{"windows absolute", "\\Users\\test", "Users/test"},
		{"clean traversal", "photos/../images", "images"},
		{"multiple slashes", "photos//2024", "photos/2024"},
		{"trailing slash", "photos/", "photos"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SanitizePath(tt.path)
			if result != tt.expected {
				t.Errorf("SanitizePath(%s) = %s, expected %s", tt.path, result, tt.expected)
			}
		})
	}
}

func TestValidateCardID(t *testing.T) {
	tests := []struct {
		name      string
		cardID    string
		shouldErr bool
	}{
		{"valid hex format", "card-0123456789abcdef", false},
		{"valid uppercase hex", "card-0123456789ABCDEF", false},
		{"valid timestamp", "card-1234567890", false},
		{"valid long timestamp", "card-12345678901234", false},
		{"empty", "", true},
		{"no prefix", "0123456789abcdef", true},
		{"wrong length hex", "card-012345", true},
		{"traversal attempt", "card-../../../etc", true},
		{"path separator", "card-1234/5678", true},
		{"special chars", "card-test@123", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateCardID(tt.cardID)
			if tt.shouldErr && err == nil {
				t.Errorf("ValidateCardID(%s) should return error", tt.cardID)
			}
			if !tt.shouldErr && err != nil {
				t.Errorf("ValidateCardID(%s) unexpected error: %v", tt.cardID, err)
			}
		})
	}
}

func TestJoinPathSafe(t *testing.T) {
	tests := []struct {
		name      string
		base      string
		elements  []string
		shouldErr bool
	}{
		{"valid join", "/base", []string{"photos", "2024"}, false},
		{"traversal escape", "/base", []string{"photos", "..", "..", "etc"}, true},
		{"single element", "/base", []string{"photos"}, false},
		{"multiple valid", "/base/photos", []string{"2024", "jan", "photo.jpg"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := JoinPathSafe(tt.base, tt.elements...)
			if tt.shouldErr && err == nil {
				t.Errorf("JoinPathSafe should return error for %v", tt.elements)
			}
			if !tt.shouldErr && err != nil {
				t.Errorf("JoinPathSafe unexpected error: %v", err)
			}
			if !tt.shouldErr && !strings.HasPrefix(result, tt.base) {
				t.Errorf("JoinPathSafe result %s doesn't start with base %s", result, tt.base)
			}
		})
	}
}

// ========== error.go tests ==========

func TestWrapError(t *testing.T) {
	originalErr := errors.New("original error")

	wrapped := WrapError(originalErr, "additional context")
	if wrapped == nil {
		t.Fatal("WrapError returned nil")
	}

	errStr := wrapped.Error()
	if !strings.Contains(errStr, "additional context") {
		t.Errorf("wrapped error missing context: %s", errStr)
	}
	if !strings.Contains(errStr, "original error") {
		t.Errorf("wrapped error missing original message: %s", errStr)
	}
}

func TestWrapErrorNil(t *testing.T) {
	wrapped := WrapError(nil, "context")
	if wrapped != nil {
		t.Error("WrapError(nil) should return nil")
	}
}

func TestWrapErrorf(t *testing.T) {
	originalErr := errors.New("test error")

	wrapped := WrapErrorf(originalErr, "error at line %d", 42)
	if wrapped == nil {
		t.Fatal("WrapErrorf returned nil")
	}

	errStr := wrapped.Error()
	if !strings.Contains(errStr, "error at line 42") {
		t.Errorf("wrapped error missing formatted context: %s", errStr)
	}
}

func TestIsRetryableNetworkError(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		retryable bool
	}{
		{"nil error", nil, false},
		{"connection refused", errors.New("connection refused"), true},
		{"timeout", errors.New("timeout exceeded"), true},
		{"no such host", errors.New("no such host"), true},
		{"rate limit", errors.New("rate limit exceeded"), true},
		{"429 status", errors.New("HTTP 429 Too Many Requests"), true},
		{"503 status", errors.New("HTTP 503 Service Unavailable"), true},
		{"broken pipe", errors.New("broken pipe"), true},
		{"EOF", errors.New("unexpected EOF"), true},
		{"not retryable", errors.New("invalid argument"), false},
		{"authentication error", errors.New("authentication failed"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsRetryableNetworkError(tt.err)
			if result != tt.retryable {
				t.Errorf("IsRetryableNetworkError(%v) = %v, expected %v", tt.err, result, tt.retryable)
			}
		})
	}
}

func TestJoinErrors(t *testing.T) {
	tests := []struct {
		name   string
		errors []error
		isNil  bool
	}{
		{"empty slice", []error{}, true},
		{"all nil", []error{nil, nil}, true},
		{"single error", []error{errors.New("error1")}, false},
		{"multiple errors", []error{errors.New("error1"), errors.New("error2")}, false},
		{"mixed with nil", []error{errors.New("error1"), nil, errors.New("error2")}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := JoinErrors(tt.errors)
			if tt.isNil && result != nil {
				t.Errorf("JoinErrors should return nil, got %v", result)
			}
			if !tt.isNil && result == nil {
				t.Error("JoinErrors should return error, got nil")
			}
			if result != nil {
				errStr := result.Error()
				if !strings.Contains(errStr, "multiple errors") {
					t.Errorf("joined error missing prefix: %s", errStr)
				}
			}
		})
	}
}

// ========== fileio.go tests ==========

func TestAtomicWrite(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "test.txt")
	data := []byte("test data")

	err := AtomicWrite(filePath, data, 0644)
	if err != nil {
		t.Fatalf("AtomicWrite failed: %v", err)
	}

	// Check file exists
	if !FileExists(filePath) {
		t.Error("file was not created")
	}

	// Check temp file was cleaned up
	tmpFile := filePath + ".tmp"
	if FileExists(tmpFile) {
		t.Error("temp file was not cleaned up")
	}

	// Verify content
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}
	if string(content) != string(data) {
		t.Errorf("file content = %s, expected %s", content, data)
	}
}

func TestReadFileWithDefault(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("existing file", func(t *testing.T) {
		filePath := filepath.Join(tmpDir, "exists.txt")
		expected := []byte("content")
		os.WriteFile(filePath, expected, 0644)

		result, err := ReadFileWithDefault(filePath, []byte("default"))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if string(result) != string(expected) {
			t.Errorf("got %s, expected %s", result, expected)
		}
	})

	t.Run("missing file", func(t *testing.T) {
		filePath := filepath.Join(tmpDir, "missing.txt")
		defaultValue := []byte("default")

		result, err := ReadFileWithDefault(filePath, defaultValue)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if string(result) != string(defaultValue) {
			t.Errorf("got %s, expected %s", result, defaultValue)
		}
	})
}

func TestFileExists(t *testing.T) {
	tmpDir := t.TempDir()

	existingFile := filepath.Join(tmpDir, "exists.txt")
	os.WriteFile(existingFile, []byte("test"), 0644)

	if !FileExists(existingFile) {
		t.Error("FileExists returned false for existing file")
	}

	missingFile := filepath.Join(tmpDir, "missing.txt")
	if FileExists(missingFile) {
		t.Error("FileExists returned true for missing file")
	}

	if FileExists(tmpDir) {
		// Directory should also return true
	}
}

func TestEnsureDir(t *testing.T) {
	tmpDir := t.TempDir()

	nestedDir := filepath.Join(tmpDir, "a", "b", "c")
	err := EnsureDir(nestedDir, 0755)
	if err != nil {
		t.Fatalf("EnsureDir failed: %v", err)
	}

	if !FileExists(nestedDir) {
		t.Error("directory was not created")
	}

	// Second call should not error
	err = EnsureDir(nestedDir, 0755)
	if err != nil {
		t.Errorf("EnsureDir failed on existing dir: %v", err)
	}
}

// ========== json.go tests ==========

func TestMarshalJSONIndent(t *testing.T) {
	data := map[string]interface{}{
		"key1": "value1",
		"key2": 42,
	}

	result, err := MarshalJSONIndent(data)
	if err != nil {
		t.Fatalf("MarshalJSONIndent failed: %v", err)
	}

	// Should contain indentation
	if !strings.Contains(string(result), "\n") {
		t.Error("result should be indented")
	}
}

func TestUnmarshalJSON(t *testing.T) {
	jsonData := []byte(`{"key": "value", "num": 42}`)
	var result map[string]interface{}

	err := UnmarshalJSON(jsonData, &result)
	if err != nil {
		t.Fatalf("UnmarshalJSON failed: %v", err)
	}

	if result["key"] != "value" {
		t.Errorf("got %v, expected 'value'", result["key"])
	}
}

func TestSaveJSON(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "subdir", "test.json")

	data := map[string]string{
		"key": "value",
	}

	err := SaveJSON(filePath, data, 0644)
	if err != nil {
		t.Fatalf("SaveJSON failed: %v", err)
	}

	if !FileExists(filePath) {
		t.Error("file was not created")
	}

	// Verify parent dir was created
	parentDir := filepath.Dir(filePath)
	if !FileExists(parentDir) {
		t.Error("parent directory was not created")
	}
}

func TestLoadJSON(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("existing file", func(t *testing.T) {
		filePath := filepath.Join(tmpDir, "test.json")
		original := map[string]string{"key": "value"}
		SaveJSON(filePath, original, 0644)

		var loaded map[string]string
		err := LoadJSON(filePath, &loaded, nil)
		if err != nil {
			t.Fatalf("LoadJSON failed: %v", err)
		}

		if loaded["key"] != "value" {
			t.Errorf("got %s, expected 'value'", loaded["key"])
		}
	})

	t.Run("missing file with default", func(t *testing.T) {
		filePath := filepath.Join(tmpDir, "missing.json")
		defaultValue := map[string]string{"default": "yes"}

		var loaded map[string]string
		err := LoadJSON(filePath, &loaded, defaultValue)
		if err != nil {
			t.Fatalf("LoadJSON failed: %v", err)
		}

		if loaded["default"] != "yes" {
			t.Error("default value not loaded")
		}
	})
}

func TestLoadJSONOrDefault(t *testing.T) {
	tmpDir := t.TempDir()
	defaultValue := map[string]string{"default": "value"}

	t.Run("missing file", func(t *testing.T) {
		filePath := filepath.Join(tmpDir, "missing.json")

		result, err := LoadJSONOrDefault(filePath, defaultValue)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if result == nil {
			t.Fatal("result should not be nil")
		}
	})

	t.Run("existing file", func(t *testing.T) {
		filePath := filepath.Join(tmpDir, "exists.json")
		os.WriteFile(filePath, []byte(`{"key": "value"}`), 0644)

		result, err := LoadJSONOrDefault(filePath, defaultValue)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		resultMap, ok := result.(map[string]interface{})
		if !ok {
			t.Fatal("result should be a map")
		}

		if resultMap["key"] != "value" {
			t.Error("wrong value loaded")
		}
	})
}

// ========== Benchmarks ==========

func BenchmarkFormatBytes(b *testing.B) {
	for i := 0; i < b.N; i++ {
		FormatBytes(1024 * 1024 * 500)
	}
}

func BenchmarkFormatDuration(b *testing.B) {
	for i := 0; i < b.N; i++ {
		FormatDuration(3665)
	}
}

func BenchmarkSanitizePath(b *testing.B) {
	for i := 0; i < b.N; i++ {
		SanitizePath("/photos/2024/../2023/photo.jpg")
	}
}

func BenchmarkValidateCardID(b *testing.B) {
	for i := 0; i < b.N; i++ {
		ValidateCardID("card-0123456789abcdef")
	}
}

func BenchmarkAtomicWrite(b *testing.B) {
	tmpDir := b.TempDir()
	data := []byte("benchmark data")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		filePath := filepath.Join(tmpDir, "bench.txt")
		AtomicWrite(filePath, data, 0644)
	}
}
