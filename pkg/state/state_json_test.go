package state

import (
	"encoding/json"
	"testing"
	"time"
)

func TestSyncRecordJSONFields(t *testing.T) {
	// Create a sync record directly without using the manager
	record := &SyncRecord{
		ID:              "test-123",
		StartTime:       time.Now(),
		EndTime:         time.Now().Add(5 * time.Minute),
		Status:          "syncing",
		FilesTotal:      100,
		FilesSynced:     25,
		BytesTotal:      1024 * 1024 * 50,  // 50MB
		BytesSynced:     1024 * 1024 * 12,  // 12MB
		CardID:          "card-abc",
		CurrentFile:     "IMG_025.jpg",
		CurrentFileSize: 1024 * 1024 * 2,  // 2MB
		TransferSpeed:   1024 * 1024,      // 1MB/s
		ETA:             "3m45s",
	}

	// Marshal to JSON
	jsonData, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("Failed to marshal SyncRecord: %v", err)
	}

	// Parse back as map to check field names
	var parsed map[string]interface{}
	err = json.Unmarshal(jsonData, &parsed)
	if err != nil {
		t.Fatalf("Failed to unmarshal JSON: %v", err)
	}

	// Check that critical fields have correct JSON names
	expectedFields := map[string]interface{}{
		"files_total":   float64(100),
		"files_synced":  float64(25),
		"bytes_total":   float64(1024 * 1024 * 50),
		"bytes_synced":  float64(1024 * 1024 * 12),
		"status":        "syncing",
		"card_id":       "card-abc",
		"current_file":  "IMG_025.jpg",
		"eta":          "3m45s",
	}

	for field, expectedValue := range expectedFields {
		actual, exists := parsed[field]
		if !exists {
			t.Errorf("Field '%s' not found in JSON output", field)
			continue
		}
		if actual != expectedValue {
			t.Errorf("Field '%s' = %v, want %v", field, actual, expectedValue)
		}
	}

	t.Logf("SyncRecord JSON: %s", string(jsonData))
}

func TestCurrentStateJSONStructure(t *testing.T) {
	// Create a state with a current sync
	state := CurrentState{
		Status: StatusSyncing,
		CurrentSync: &SyncRecord{
			ID:              "sync-456",
			StartTime:       time.Now(),
			Status:          "syncing",
			FilesTotal:      200,
			FilesSynced:     50,
			BytesTotal:      1024 * 1024 * 100,  // 100MB
			BytesSynced:     1024 * 1024 * 25,   // 25MB
			CardID:          "card-def",
			CurrentFile:     "IMG_050.jpg",
			CurrentFileSize: 1024 * 1024 * 3,
			TransferSpeed:   2 * 1024 * 1024,  // 2MB/s
			ETA:             "2m30s",
		},
		SDCardMounted: true,
		SDCardPath:    "/mnt/sdcard",
	}

	// Marshal to JSON
	jsonData, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("Failed to marshal CurrentState: %v", err)
	}

	// Parse as map
	var parsed map[string]interface{}
	err = json.Unmarshal(jsonData, &parsed)
	if err != nil {
		t.Fatalf("Failed to unmarshal JSON: %v", err)
	}

	// Check top-level fields
	if status, ok := parsed["status"].(string); !ok || status != "syncing" {
		t.Errorf("status = %v, want 'syncing'", parsed["status"])
	}

	// Check current_sync exists
	currentSync, ok := parsed["current_sync"].(map[string]interface{})
	if !ok {
		t.Fatal("current_sync is not present or not an object in JSON")
	}

	// Verify progress fields in current_sync
	progressFields := map[string]float64{
		"files_total":  200,
		"files_synced": 50,
		"bytes_total":  1024 * 1024 * 100,
		"bytes_synced": 1024 * 1024 * 25,
	}

	for field, expected := range progressFields {
		if actual, ok := currentSync[field].(float64); !ok || actual != expected {
			t.Errorf("current_sync.%s = %v (type %T), want %v",
				field, currentSync[field], currentSync[field], expected)
		}
	}

	t.Logf("CurrentState JSON: %s", string(jsonData))
}

func TestWebSocketDataFormat(t *testing.T) {
	// Simulate what the WebSocket sends
	state := CurrentState{
		Status: "syncing",  // lowercase as defined in const
		CurrentSync: &SyncRecord{
			FilesTotal:  100,
			FilesSynced: 25,
			BytesTotal:  1024 * 1024 * 50,
			BytesSynced: 1024 * 1024 * 12,
			Status:      "syncing",
		},
		SDCardMounted: true,
		SDCardPath:    "/mnt/sdcard",
	}

	// Marshal as would be sent over WebSocket
	jsonData, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("Failed to marshal state: %v", err)
	}

	// This is what the JavaScript receives
	var wsData map[string]interface{}
	err = json.Unmarshal(jsonData, &wsData)
	if err != nil {
		t.Fatalf("Failed to unmarshal WebSocket data: %v", err)
	}

	// Simulate JavaScript check: if (data.current_sync && data.status === 'syncing')
	status, hasStatus := wsData["status"].(string)
	currentSync, hasCurrentSync := wsData["current_sync"].(map[string]interface{})

	if !hasStatus {
		t.Error("WebSocket data missing 'status' field")
	}
	if !hasCurrentSync {
		t.Error("WebSocket data missing 'current_sync' field")
	}
	if status != "syncing" {
		t.Errorf("status = '%s', want 'syncing'", status)
	}

	// Check if current_sync has the required fields for progress calculation
	if currentSync != nil {
		requiredFields := []string{"files_total", "files_synced", "bytes_total", "bytes_synced"}
		for _, field := range requiredFields {
			if _, exists := currentSync[field]; !exists {
				t.Errorf("current_sync missing required field: %s", field)
			}
		}

		// Check that totals are non-zero (common bug)
		if filesTotal, ok := currentSync["files_total"].(float64); ok && filesTotal == 0 {
			t.Error("files_total is 0, progress bar won't show!")
		}
		if bytesTotal, ok := currentSync["bytes_total"].(float64); ok && bytesTotal == 0 {
			t.Error("bytes_total is 0, progress bar won't show!")
		}
	}

	t.Logf("WebSocket data structure: %s", string(jsonData))
}

func TestProgressCalculation(t *testing.T) {
	// Test the progress calculation logic
	testCases := []struct {
		name          string
		filesTotal    int64
		filesSynced   int64
		bytesTotal    int64
		bytesSynced   int64
		expectedFiles float64  // percentage
		expectedBytes float64  // percentage
	}{
		{
			name:          "25% progress",
			filesTotal:    100,
			filesSynced:   25,
			bytesTotal:    1024 * 1024 * 100,
			bytesSynced:   1024 * 1024 * 25,
			expectedFiles: 25.0,
			expectedBytes: 25.0,
		},
		{
			name:          "50% files, 30% bytes",
			filesTotal:    100,
			filesSynced:   50,
			bytesTotal:    1024 * 1024 * 100,
			bytesSynced:   1024 * 1024 * 30,
			expectedFiles: 50.0,
			expectedBytes: 30.0,
		},
		{
			name:          "zero totals should not crash",
			filesTotal:    0,
			filesSynced:   0,
			bytesTotal:    0,
			bytesSynced:   0,
			expectedFiles: 0.0,
			expectedBytes: 0.0,
		},
		{
			name:          "complete sync",
			filesTotal:    100,
			filesSynced:   100,
			bytesTotal:    1024 * 1024 * 100,
			bytesSynced:   1024 * 1024 * 100,
			expectedFiles: 100.0,
			expectedBytes: 100.0,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			record := &SyncRecord{
				FilesTotal:  tc.filesTotal,
				FilesSynced: tc.filesSynced,
				BytesTotal:  tc.bytesTotal,
				BytesSynced: tc.bytesSynced,
			}

			// Simulate JavaScript progress calculation
			var filesPercent, bytesPercent float64
			if record.FilesTotal > 0 {
				filesPercent = float64(record.FilesSynced) / float64(record.FilesTotal) * 100
			}
			if record.BytesTotal > 0 {
				bytesPercent = float64(record.BytesSynced) / float64(record.BytesTotal) * 100
			}

			if filesPercent != tc.expectedFiles {
				t.Errorf("Files progress = %.2f%%, want %.2f%%", filesPercent, tc.expectedFiles)
			}
			if bytesPercent != tc.expectedBytes {
				t.Errorf("Bytes progress = %.2f%%, want %.2f%%", bytesPercent, tc.expectedBytes)
			}
		})
	}
}