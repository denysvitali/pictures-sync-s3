package syncmanager

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// BenchmarkProgressParsing measures JSON log line parsing performance
func BenchmarkProgressParsing(b *testing.B) {
	// Sample rclone JSON log lines
	logLines := []string{
		`{"level":"info","msg":"Syncing photos","time":"2024-01-01T10:00:00Z"}`,
		`{"level":"info","stats":{"bytes":1048576,"checks":10,"deletes":0,"elapsedTime":1.5,"errors":0,"fatalError":false,"renames":0,"retryError":false,"transfers":5,"transferring":null},"time":"2024-01-01T10:00:01Z"}`,
		`{"level":"info","stats":{"bytes":5242880,"checks":50,"deletes":0,"elapsedTime":5.2,"errors":0,"fatalError":false,"renames":0,"retryError":false,"transfers":25,"transferring":[{"name":"IMG_001.JPG","size":524288,"bytes":262144,"percentage":50}]},"time":"2024-01-01T10:00:05Z"}`,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, line := range logLines {
			var parsed map[string]any
			_ = json.Unmarshal([]byte(line), &parsed)
		}
	}
}

// BenchmarkCardIDValidation measures card ID validation performance
func BenchmarkCardIDValidation(b *testing.B) {
	validIDs := []string{
		"card-0123456789abcdef",
		"card-1234567890",
		"card-abcdef1234567890",
		"card-ABCDEF1234567890",
		"card-12345678901234567890",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, id := range validIDs {
			_ = validateCardID(id)
		}
	}
}

// BenchmarkRemotePathConstruction measures remote path building performance
func BenchmarkRemotePathConstruction(b *testing.B) {
	m := &Manager{
		remoteName: "backblaze",
		remotePath: "/photos",
	}
	cardID := "card-test123"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.remoteName + ":" + m.remotePath + "/" + cardID + "/DCIM"
	}
}

// BenchmarkConfigValidation measures rclone config validation performance
func BenchmarkConfigValidation(b *testing.B) {
	tempDir := b.TempDir()
	configPath := filepath.Join(tempDir, "rclone.conf")

	// Create valid config
	configData := []byte(`[backblaze]
type = b2
account = test
key = testkey123
`)
	os.WriteFile(configPath, configData, 0600)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = os.Stat(configPath)
		_, _ = os.ReadFile(configPath)
	}
}

// BenchmarkStateUpdates measures state update performance
func BenchmarkStateUpdates(b *testing.B) {
	mockState := &mockStateManager{
		updates: make(map[string]any),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mockState.UpdateSyncProgress(int64(i), int64(1000), int64(i*1024*1024))
	}
}

// BenchmarkErrorClassification measures error classification performance
func BenchmarkErrorClassification(b *testing.B) {
	errors := []error{
		&rcloneError{msg: "connection refused"},
		&rcloneError{msg: "timeout"},
		&rcloneError{msg: "no such host"},
		&rcloneError{msg: "rate limit exceeded"},
		&rcloneError{msg: "invalid credentials"},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, err := range errors {
			_ = isRetryableError(err)
		}
	}
}

// BenchmarkLogLineBuffer measures buffered log processing
func BenchmarkLogLineBuffer(b *testing.B) {
	// Simulate buffered output
	buffer := bytes.NewBuffer(nil)
	for i := 0; i < 100; i++ {
		buffer.WriteString(`{"level":"info","msg":"test message","time":"2024-01-01T10:00:00Z"}` + "\n")
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		lines := bytes.Split(buffer.Bytes(), []byte("\n"))
		for _, line := range lines {
			if len(line) > 0 {
				var parsed map[string]any
				_ = json.Unmarshal(line, &parsed)
			}
		}
	}
}

// BenchmarkConcurrentStateAccess measures concurrent state access patterns
func BenchmarkConcurrentStateAccess(b *testing.B) {
	mockState := &mockStateManager{
		updates: make(map[string]any),
	}

	b.RunParallel(func(pb *testing.PB) {
		fileNum := int64(0)
		for pb.Next() {
			fileNum++
			mockState.UpdateSyncProgress(fileNum, 1000, fileNum*1024)
		}
	})
}

// BenchmarkRetryBackoff measures retry backoff calculation performance
func BenchmarkRetryBackoff(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for attempt := 0; attempt < 10; attempt++ {
			_ = calculateBackoff(attempt)
		}
	}
}

// mockStateManager for benchmarking
type mockStateManager struct {
	updates map[string]any
}

func (m *mockStateManager) UpdateSyncProgress(filesSynced, filesTotal, bytesTransferred int64) {
	m.updates["files"] = filesSynced
	m.updates["bytes"] = bytesTransferred
}

// rcloneError for benchmarking
type rcloneError struct {
	msg string
}

func (e *rcloneError) Error() string {
	return e.msg
}

// calculateBackoff calculates exponential backoff
func calculateBackoff(attempt int) time.Duration {
	base := time.Second
	max := 60 * time.Second
	backoff := base * time.Duration(1<<uint(attempt))
	if backoff > max {
		backoff = max
	}
	return backoff
}

// BenchmarkMemoryAllocation measures memory allocation patterns
func BenchmarkMemoryAllocation(b *testing.B) {
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		// Simulate typical sync operation allocations
		stats := make(map[string]any)
		stats["bytes"] = int64(1024 * 1024)
		stats["files"] = int64(100)
		stats["elapsed"] = 30.5

		_ = stats
	}
}

// BenchmarkJSONUnmarshal measures JSON parsing performance for rclone output
func BenchmarkJSONUnmarshal(b *testing.B) {
	jsonData := []byte(`{
		"level":"info",
		"stats":{
			"bytes":1048576,
			"checks":10,
			"deletes":0,
			"elapsedTime":1.5,
			"errors":0,
			"fatalError":false,
			"renames":0,
			"retryError":false,
			"transfers":5,
			"transferring":[
				{"name":"IMG_001.JPG","size":524288,"bytes":262144,"percentage":50}
			]
		},
		"time":"2024-01-01T10:00:01Z"
	}`)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		var result map[string]any
		_ = json.Unmarshal(jsonData, &result)
	}
}

// BenchmarkFilePathOperations measures filepath operations performance
func BenchmarkFilePathOperations(b *testing.B) {
	paths := []string{
		"/perm/pictures-sync/mounts/sdcard/DCIM",
		"/photos/card-123/DCIM/100CANON",
		"/remote/backup/photos/2024/january",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, path := range paths {
			_ = filepath.Clean(path)
			_ = filepath.Dir(path)
			_ = filepath.Base(path)
		}
	}
}

// BenchmarkStringConcatenation measures string building performance
func BenchmarkStringConcatenation(b *testing.B) {
	remoteName := "backblaze"
	remotePath := "/photos"
	cardID := "card-test123"

	b.Run("concatenation", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = remoteName + ":" + remotePath + "/" + cardID + "/DCIM"
		}
	})

	b.Run("sprintf", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = formatRemotePath(remoteName, remotePath, cardID)
		}
	})
}

func formatRemotePath(remote, path, cardID string) string {
	// Simulate sprintf-style formatting
	return remote + ":" + path + "/" + cardID + "/DCIM"
}

// BenchmarkChannelOperations measures channel communication performance
func BenchmarkChannelOperations(b *testing.B) {
	ch := make(chan any, 100)
	done := make(chan bool)

	go func() {
		for range ch {
			// Consume messages
		}
		done <- true
	}()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ch <- struct{}{}
	}

	close(ch)
	<-done
}
