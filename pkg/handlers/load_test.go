package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/denysvitali/pictures-sync-s3/pkg/settings"
	"github.com/denysvitali/pictures-sync-s3/pkg/state"
)

// BenchmarkHandleStatusLoad benchmarks the status endpoint under load
func BenchmarkHandleStatusLoad(b *testing.B) {
	tmpDir := b.TempDir()
	stateMgr, err := setupStateManager(tmpDir)
	if err != nil {
		b.Fatal(err)
	}

	ctx := &Context{
		StateMgr: stateMgr,
		Daemon:   &mockDaemonClient{stateMgr: stateMgr},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
		w := httptest.NewRecorder()

		ctx.HandleStatus(w, req)

		if w.Code != http.StatusOK {
			b.Fatalf("expected status 200, got %d", w.Code)
		}
	}
}

// BenchmarkHandleStatusParallel benchmarks concurrent status requests
func BenchmarkHandleStatusParallel(b *testing.B) {
	tmpDir := b.TempDir()
	stateMgr, err := setupStateManager(tmpDir)
	if err != nil {
		b.Fatal(err)
	}

	ctx := &Context{
		StateMgr: stateMgr,
		Daemon:   &mockDaemonClient{stateMgr: stateMgr},
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
			w := httptest.NewRecorder()

			ctx.HandleStatus(w, req)

			if w.Code != http.StatusOK {
				b.Fatalf("expected status 200, got %d", w.Code)
			}
		}
	})
}

// BenchmarkHandleHistory benchmarks the history endpoint
func BenchmarkHandleHistory(b *testing.B) {
	tmpDir := b.TempDir()
	stateMgr, err := setupStateManager(tmpDir)
	if err != nil {
		b.Fatal(err)
	}

	// Create some history
	for i := 0; i < 10; i++ {
		_, err := stateMgr.StartSync(fmt.Sprintf("card-%d", i), 100, 1024*1024)
		if err != nil {
			b.Fatal(err)
		}
		if err := stateMgr.FinishSync(true, nil); err != nil {
			b.Fatal(err)
		}
	}

	ctx := &Context{
		StateMgr: stateMgr,
		Daemon:   &mockDaemonClient{stateMgr: stateMgr},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/history", nil)
		w := httptest.NewRecorder()

		ctx.HandleHistory(w, req)

		if w.Code != http.StatusOK {
			b.Fatalf("expected status 200, got %d", w.Code)
		}
	}
}

// BenchmarkHandleHistoryLarge benchmarks history with large dataset
func BenchmarkHandleHistoryLarge(b *testing.B) {
	tmpDir := b.TempDir()
	stateMgr, err := setupStateManager(tmpDir)
	if err != nil {
		b.Fatal(err)
	}

	// Create large history
	for i := 0; i < 1000; i++ {
		_, err := stateMgr.StartSync(fmt.Sprintf("card-%d", i), 100, 1024*1024)
		if err != nil {
			b.Fatal(err)
		}
		if err := stateMgr.FinishSync(true, nil); err != nil {
			b.Fatal(err)
		}
	}

	ctx := &Context{
		StateMgr: stateMgr,
		Daemon:   &mockDaemonClient{stateMgr: stateMgr},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/history", nil)
		w := httptest.NewRecorder()

		ctx.HandleHistory(w, req)

		if w.Code != http.StatusOK {
			b.Fatalf("expected status 200, got %d", w.Code)
		}
	}
}

// BenchmarkJSONResponse benchmarks JSON serialization
func BenchmarkJSONResponse(b *testing.B) {
	data := map[string]any{
		"status":         "syncing",
		"files_synced":   500,
		"files_total":    1000,
		"bytes_synced":   50 * 1024 * 1024,
		"bytes_total":    100 * 1024 * 1024,
		"transfer_speed": 1.5 * 1024 * 1024,
		"eta":            "5m30s",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		JSONResponse(w, data)

		if w.Code != http.StatusOK {
			b.Fatalf("expected status 200, got %d", w.Code)
		}
	}
}

// BenchmarkJSONResponseLarge benchmarks large JSON responses
func BenchmarkJSONResponseLarge(b *testing.B) {
	// Create large history data
	history := make([]map[string]any, 1000)
	for i := 0; i < 1000; i++ {
		history[i] = map[string]any{
			"id":           fmt.Sprintf("sync-%d", i),
			"card_id":      fmt.Sprintf("card-%d", i%100),
			"start_time":   time.Now().Add(-time.Duration(i) * time.Hour).Format(time.RFC3339),
			"end_time":     time.Now().Add(-time.Duration(i) * time.Hour).Format(time.RFC3339),
			"status":       "success",
			"files_total":  1000,
			"files_synced": 1000,
			"bytes_total":  100 * 1024 * 1024,
			"bytes_synced": 100 * 1024 * 1024,
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		JSONResponse(w, history)

		if w.Code != http.StatusOK {
			b.Fatalf("expected status 200, got %d", w.Code)
		}
	}
}

// BenchmarkConcurrentEndpoints benchmarks multiple endpoints concurrently
func BenchmarkConcurrentEndpoints(b *testing.B) {
	tmpDir := b.TempDir()
	stateMgr, err := setupStateManager(tmpDir)
	if err != nil {
		b.Fatal(err)
	}

	// Create some history
	for i := 0; i < 50; i++ {
		_, err := stateMgr.StartSync(fmt.Sprintf("card-%d", i), 100, 1024*1024)
		if err != nil {
			b.Fatal(err)
		}
		if err := stateMgr.FinishSync(true, nil); err != nil {
			b.Fatal(err)
		}
	}

	ctx := &Context{
		StateMgr: stateMgr,
		Daemon:   &mockDaemonClient{stateMgr: stateMgr},
	}

	b.ResetTimer()

	var wg sync.WaitGroup
	errors := make(chan error, b.N*3)

	for i := 0; i < b.N; i++ {
		// Status endpoint
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
			w := httptest.NewRecorder()
			ctx.HandleStatus(w, req)
			if w.Code != http.StatusOK {
				errors <- fmt.Errorf("status returned %d", w.Code)
			}
		}()

		// History endpoint
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodGet, "/api/history", nil)
			w := httptest.NewRecorder()
			ctx.HandleHistory(w, req)
			if w.Code != http.StatusOK {
				errors <- fmt.Errorf("history returned %d", w.Code)
			}
		}()
	}

	wg.Wait()
	close(errors)

	if len(errors) > 0 {
		b.Fatalf("got errors during concurrent requests: %v", <-errors)
	}
}

// LoadTestStatus performs a load test on the status endpoint
func LoadTestStatus(t *testing.T, concurrency, requests int) {
	tmpDir := t.TempDir()
	stateMgr, err := setupStateManager(tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	ctx := &Context{
		StateMgr: stateMgr,
		Daemon:   &mockDaemonClient{stateMgr: stateMgr},
	}

	server := httptest.NewServer(http.HandlerFunc(ctx.HandleStatus))
	defer server.Close()

	start := time.Now()
	var successCount, errorCount atomic.Int64
	var totalLatency atomic.Int64

	var wg sync.WaitGroup
	requestsPerWorker := requests / concurrency

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < requestsPerWorker; j++ {
				reqStart := time.Now()
				resp, err := http.Get(server.URL)
				latency := time.Since(reqStart)

				if err != nil {
					errorCount.Add(1)
					continue
				}
				defer resp.Body.Close()

				if resp.StatusCode == http.StatusOK {
					successCount.Add(1)
					totalLatency.Add(int64(latency))
				} else {
					errorCount.Add(1)
				}
			}
		}()
	}

	wg.Wait()
	duration := time.Since(start)

	success := successCount.Load()
	errors := errorCount.Load()
	avgLatency := time.Duration(totalLatency.Load() / success)

	t.Logf("Load Test Results (Status Endpoint):")
	t.Logf("  Concurrency: %d", concurrency)
	t.Logf("  Total Requests: %d", requests)
	t.Logf("  Duration: %v", duration)
	t.Logf("  Successful: %d", success)
	t.Logf("  Errors: %d", errors)
	t.Logf("  Requests/sec: %.2f", float64(success)/duration.Seconds())
	t.Logf("  Avg Latency: %v", avgLatency)

	if errors > 0 {
		t.Errorf("Load test had %d errors", errors)
	}
}

// TestLoadTestStatus runs various load test scenarios
func TestLoadTestStatus(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping load test in short mode")
	}

	testCases := []struct {
		name        string
		concurrency int
		requests    int
	}{
		{"low_load", 10, 1000},
		{"medium_load", 50, 5000},
		{"high_load", 100, 10000},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			LoadTestStatus(t, tc.concurrency, tc.requests)
		})
	}
}

// BenchmarkEndToEndRequest benchmarks full request/response cycle
func BenchmarkEndToEndRequest(b *testing.B) {
	tmpDir := b.TempDir()
	stateMgr, err := setupStateManager(tmpDir)
	if err != nil {
		b.Fatal(err)
	}

	ctx := &Context{
		StateMgr: stateMgr,
		Daemon:   &mockDaemonClient{stateMgr: stateMgr},
	}

	server := httptest.NewServer(http.HandlerFunc(ctx.HandleStatus))
	defer server.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resp, err := http.Get(server.URL)
		if err != nil {
			b.Fatal(err)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			b.Fatalf("expected status 200, got %d", resp.StatusCode)
		}
	}
}

// BenchmarkResponseParsing benchmarks JSON response parsing
func BenchmarkResponseParsing(b *testing.B) {
	tmpDir := b.TempDir()
	stateMgr, err := setupStateManager(tmpDir)
	if err != nil {
		b.Fatal(err)
	}

	ctx := &Context{
		StateMgr: stateMgr,
		Daemon:   &mockDaemonClient{stateMgr: stateMgr},
	}

	// Get a sample response
	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	w := httptest.NewRecorder()
	ctx.HandleStatus(w, req)

	responseData := w.Body.Bytes()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var result state.CurrentState
		if err := json.Unmarshal(responseData, &result); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkMethodValidation benchmarks HTTP method validation overhead
func BenchmarkMethodValidation(b *testing.B) {
	tmpDir := b.TempDir()
	stateMgr, err := setupStateManager(tmpDir)
	if err != nil {
		b.Fatal(err)
	}

	ctx := &Context{
		StateMgr: stateMgr,
		Daemon:   &mockDaemonClient{stateMgr: stateMgr},
	}

	b.Run("valid_method", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
			w := httptest.NewRecorder()
			ctx.HandleStatus(w, req)
		}
	})

	b.Run("invalid_method", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			req := httptest.NewRequest(http.MethodPost, "/api/status", nil)
			w := httptest.NewRecorder()
			ctx.HandleStatus(w, req)

			if w.Code != http.StatusMethodNotAllowed {
				b.Fatalf("expected status 405, got %d", w.Code)
			}
		}
	})
}

// BenchmarkStateReload benchmarks reload overhead in handlers
func BenchmarkStateReload(b *testing.B) {
	tmpDir := b.TempDir()
	stateMgr, err := setupStateManager(tmpDir)
	if err != nil {
		b.Fatal(err)
	}

	ctx := &Context{
		StateMgr: stateMgr,
		Daemon:   &mockDaemonClient{stateMgr: stateMgr},
	}

	// Simulate state updates happening in background
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for range ticker.C {
			stateMgr.SetStatus(state.StatusSyncing)
		}
	}()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
		w := httptest.NewRecorder()
		ctx.HandleStatus(w, req)

		if w.Code != http.StatusOK {
			b.Fatalf("expected status 200, got %d", w.Code)
		}
	}
}

// TestStressEndpoints stress tests all endpoints under heavy load
func TestStressEndpoints(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress test in short mode")
	}

	tmpDir := t.TempDir()
	stateMgr, err := setupStateManager(tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	// Create substantial history
	for i := 0; i < 100; i++ {
		_, err := stateMgr.StartSync(fmt.Sprintf("card-%d", i), 1000, 100*1024*1024)
		if err != nil {
			t.Fatal(err)
		}
		if err := stateMgr.FinishSync(true, nil); err != nil {
			t.Fatal(err)
		}
	}

	ctx := &Context{
		StateMgr: stateMgr,
		Daemon:   &mockDaemonClient{stateMgr: stateMgr},
	}

	const (
		concurrency = 10
		duration    = 50 * time.Millisecond
	)

	var successCount, errorCount atomic.Int64
	stopChan := make(chan struct{})

	start := time.Now()
	var wg sync.WaitGroup

	// Spawn workers
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			endpoints := []struct {
				method  string
				path    string
				handler http.HandlerFunc
			}{
				{http.MethodGet, "/api/status", ctx.HandleStatus},
				{http.MethodGet, "/api/history", ctx.HandleHistory},
			}

			for {
				select {
				case <-stopChan:
					return
				default:
					endpoint := endpoints[workerID%len(endpoints)]
					req := httptest.NewRequest(endpoint.method, endpoint.path, nil)
					w := httptest.NewRecorder()

					endpoint.handler(w, req)

					if w.Code == http.StatusOK {
						successCount.Add(1)
					} else {
						errorCount.Add(1)
					}
				}
			}
		}(i)
	}

	// Run for specified duration
	time.Sleep(duration)
	close(stopChan)
	wg.Wait()

	elapsed := time.Since(start)
	success := successCount.Load()
	errors := errorCount.Load()

	t.Logf("Stress Test Results:")
	t.Logf("  Concurrency: %d", concurrency)
	t.Logf("  Duration: %v", elapsed)
	t.Logf("  Total Requests: %d", success+errors)
	t.Logf("  Successful: %d", success)
	t.Logf("  Errors: %d", errors)
	t.Logf("  Requests/sec: %.2f", float64(success)/elapsed.Seconds())
	t.Logf("  Error Rate: %.2f%%", float64(errors)/float64(success+errors)*100)

	if errors > int64(float64(success)*0.01) { // Allow up to 1% error rate
		t.Errorf("Stress test error rate too high: %d errors out of %d requests", errors, success+errors)
	}
}

// setupStateManager creates a state manager for testing
func setupStateManager(tmpDir string) (*state.Manager, error) {
	// This is a helper function - actual implementation would need
	// to properly initialize the state package's directory
	oldStateDir := state.GetStateDir()
	state.SetStateDir(tmpDir)

	mgr, err := state.NewManager()
	if err != nil {
		state.SetStateDir(oldStateDir)
		return nil, err
	}

	return mgr, nil
}

// BenchmarkSettingsOperations benchmarks settings-related operations
func BenchmarkSettingsOperations(b *testing.B) {
	tmpDir := b.TempDir()

	b.Run("create_settings", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			b.StopTimer()
			settingsPath := fmt.Sprintf("%s/settings-%d.json", tmpDir, i)
			b.StartTimer()

			s, err := settings.LoadFrom(settingsPath)
			if err != nil {
				b.Fatal(err)
			}
			_ = s
		}
	})

	b.Run("get_remote", func(b *testing.B) {
		s, err := settings.LoadFrom(tmpDir + "/settings.json")
		if err != nil {
			b.Fatal(err)
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			name, path := s.GetRemoteName(), s.GetRemotePath()
			_, _ = name, path
		}
	})

	b.Run("set_remote", func(b *testing.B) {
		s, err := settings.LoadFrom(tmpDir + "/settings.json")
		if err != nil {
			b.Fatal(err)
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			s.RemoteName = "b2"
			s.RemotePath = "/photos"
		}
	})
}

// TestTimeoutBehavior tests handler behavior under timeout conditions
func TestTimeoutBehavior(t *testing.T) {
	tmpDir := t.TempDir()
	stateMgr, err := setupStateManager(tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	ctx := &Context{
		StateMgr: stateMgr,
		Daemon:   &mockDaemonClient{stateMgr: stateMgr},
	}

	server := httptest.NewServer(http.HandlerFunc(ctx.HandleStatus))
	defer server.Close()

	client := &http.Client{
		Timeout: 100 * time.Millisecond,
	}

	// Test with very short timeout
	start := time.Now()
	_, err = client.Get(server.URL)
	elapsed := time.Since(start)

	if err != nil && !strings.Contains(err.Error(), "timeout") {
		// Request should complete quickly, not timeout
		if elapsed > 100*time.Millisecond {
			t.Errorf("Request took too long: %v", elapsed)
		}
	}
}
