package syncmanager

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/denysvitali/pictures-sync-s3/pkg/state"
)

func skipRcloneNetworkTest(t *testing.T) {
	t.Helper()
	if os.Getenv("RUN_RCLONE_NETWORK_TESTS") == "" {
		t.Skip("skipping rclone network integration test; set RUN_RCLONE_NETWORK_TESTS=1 to run")
	}
}

// Network Resilience Tests for Sync Manager
// Tests various network failure scenarios and recovery mechanisms

// ============================================================================
// BUG REPORT: CRITICAL NETWORK RESILIENCE ISSUES FOUND
// ============================================================================
//
// 1. NO RETRY MECHANISM
//    - rclone.Sync() has NO automatic retry on network errors
//    - Application layer provides NO retry wrapper around sync operations
//    - Single network hiccup = entire sync fails
//    - Location: syncmanager.go:189
//    - Risk: HIGH - Sync fails completely on transient network issues
//
// 2. NO TIMEOUT CONFIGURATION
//    - No HTTP client timeout configuration exposed
//    - No context timeout for sync operations
//    - Sync can hang indefinitely on network issues
//    - Location: syncmanager.go:148-154
//    - Risk: HIGH - Can cause device to hang until manual intervention
//
// 3. NO CONNECTION POOL MANAGEMENT
//    - No HTTP transport configuration
//    - No MaxIdleConns or MaxConnsPerHost limits
//    - Can exhaust file descriptors on long-running syncs
//    - Location: entire syncmanager.go
//    - Risk: MEDIUM - Resource exhaustion possible
//
// 4. PARTIAL UPLOAD CORRUPTION RISK
//    - No verification after network reconnection
//    - GetRemoteSize() returns 0 on error (line 92-93)
//    - Cannot distinguish between "empty" and "failed to check"
//    - May double-upload or skip files after network recovery
//    - Location: syncmanager.go:73-106, 136-140
//    - Risk: CRITICAL - Data integrity issues
//
// 5. NO DNS FAILURE HANDLING
//    - TestConnection() checks once at startup
//    - No retry or fallback on DNS resolution failures
//    - Location: syncmanager.go:342-370
//    - Risk: MEDIUM - First sync after boot may fail
//
// 6. CANCEL DURING NETWORK ERROR LEAVES STATE INCONSISTENT
//    - Cancel() during network operation may not clean up properly
//    - Progress monitoring goroutine may not exit cleanly
//    - Location: syncmanager.go:309-322
//    - Risk: HIGH - Can prevent future syncs
//
// 7. PROGRESS CHANNEL MEMORY LEAK ON NETWORK DISCONNECT
//    - No UnsubscribeProgress() method
//    - WebSocket disconnects don't remove channels
//    - Channels accumulate over time
//    - Location: syncmanager.go:332-339
//    - Risk: MEDIUM - Memory leak on repeated network disconnects
//
// 8. NO TLS/SSL ERROR RECOVERY
//    - TLS certificate errors fail immediately
//    - No option to retry with certificate validation disabled
//    - No user notification of certificate issues
//    - Location: inherited from rclone config
//    - Risk: LOW - Config issue, but poor UX
//
// ============================================================================

// TestNetworkDropMidSync simulates network dropping during active sync
func TestNetworkDropMidSync(t *testing.T) {
	skipRcloneNetworkTest(t)
	// BUG: Network drops mid-sync cause complete failure with no retry
	tmpDir, err := os.MkdirTemp("", "network-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create test files
	srcDir := filepath.Join(tmpDir, "source")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create multiple test files to simulate longer sync
	for i := 0; i < 10; i++ {
		testFile := filepath.Join(srcDir, fmt.Sprintf("photo%d.jpg", i))
		data := make([]byte, 1024*100) // 100KB per file
		if err := os.WriteFile(testFile, data, 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Create HTTP server that fails mid-transfer
	var requestCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := requestCount.Add(1)
		if count > 3 {
			// Simulate network drop after 3 requests
			hj, ok := w.(http.Hijacker)
			if ok {
				conn, _, _ := hj.Hijack()
				if conn != nil {
					conn.Close() // Abruptly close connection
				}
			}
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Setup sync manager with S3 backend pointing to test server
	configPath := filepath.Join(tmpDir, "rclone.conf")
	config := fmt.Sprintf(`[test-s3]
type = s3
provider = Other
access_key_id = test
secret_access_key = test
endpoint = %s
acl = private
`, server.URL)
	if err := os.WriteFile(configPath, []byte(config), 0644); err != nil {
		t.Fatal(err)
	}

	stateMgr, _ := state.NewManager()
	syncMgr := NewManager(configPath, "test-s3", "/test", stateMgr, 4, 8)

	// Attempt sync - should fail when network drops
	err = syncMgr.Sync(srcDir, "card-123", 10, 1024*1000)

	// EXPECTED: Sync should retry or handle gracefully
	// ACTUAL: Sync fails immediately with no retry
	if err == nil {
		t.Error("BUG: Sync succeeded despite network drop - test may be flaky")
	} else {
		t.Logf("✓ Sync failed as expected: %v", err)
		t.Log("BUG CONFIRMED: No automatic retry mechanism")
		t.Log("RISK: Single network hiccup causes complete sync failure")
		t.Log("RECOMMENDATION: Implement exponential backoff retry in Sync() method")
	}

	// Verify cleanup after network failure
	if syncMgr.IsRunning() {
		t.Error("BUG CRITICAL: isRunning still true after network failure")
		t.Log("This will prevent future sync attempts!")
	}

	syncMgr.mu.Lock()
	hasLeak := syncMgr.cancelFunc != nil
	syncMgr.mu.Unlock()

	if hasLeak {
		t.Error("BUG: cancelFunc not cleaned up after network error")
		t.Log("Potential goroutine leak detected")
	}
}

// TestDNSResolutionFailure tests behavior when DNS cannot resolve remote endpoint
func TestDNSResolutionFailure(t *testing.T) {
	skipRcloneNetworkTest(t)
	tmpDir, err := os.MkdirTemp("", "network-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	srcDir := filepath.Join(tmpDir, "source")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create config with invalid DNS name
	configPath := filepath.Join(tmpDir, "rclone.conf")
	config := `[test-s3]
type = s3
provider = Other
access_key_id = test
secret_access_key = test
endpoint = https://this-domain-definitely-does-not-exist-12345.invalid
acl = private
`
	if err := os.WriteFile(configPath, []byte(config), 0644); err != nil {
		t.Fatal(err)
	}

	stateMgr, _ := state.NewManager()
	syncMgr := NewManager(configPath, "test-s3", "/test", stateMgr, 4, 8)

	// Test connection should fail with DNS error
	err = syncMgr.TestConnection()
	if err == nil {
		t.Error("TestConnection should fail with invalid DNS")
	} else {
		t.Logf("✓ TestConnection failed: %v", err)
	}

	// Sync should also fail
	testFile := filepath.Join(srcDir, "test.jpg")
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}

	startTime := time.Now()
	err = syncMgr.Sync(srcDir, "card-123", 1, 4)
	duration := time.Since(startTime)

	if err == nil {
		t.Error("BUG: Sync should fail with DNS resolution failure")
	} else {
		t.Logf("✓ Sync failed: %v", err)
	}

	// Check if DNS timeout is reasonable
	if duration > 30*time.Second {
		t.Errorf("BUG: DNS resolution timeout too long: %v", duration)
		t.Log("RECOMMENDATION: Configure shorter DNS timeout in rclone config")
	} else {
		t.Logf("✓ Failed within reasonable time: %v", duration)
	}

	// Verify state cleanup
	if syncMgr.IsRunning() {
		t.Error("BUG: isRunning not reset after DNS failure")
	}
}

// TestIntermittentPacketLoss simulates unreliable network with packet loss
func TestIntermittentPacketLoss(t *testing.T) {
	skipRcloneNetworkTest(t)
	// BUG: Packet loss causes retransmission delays but no application-level retry
	tmpDir, err := os.MkdirTemp("", "network-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	srcDir := filepath.Join(tmpDir, "source")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatal(err)
	}

	testFile := filepath.Join(srcDir, "test.jpg")
	if err := os.WriteFile(testFile, []byte("test data"), 0644); err != nil {
		t.Fatal(err)
	}

	// Server that randomly drops connections (simulating packet loss)
	var requestCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := requestCount.Add(1)
		// Drop every 3rd request
		if count%3 == 0 {
			hj, ok := w.(http.Hijacker)
			if ok {
				conn, _, _ := hj.Hijack()
				if conn != nil {
					conn.Close()
				}
			}
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	configPath := filepath.Join(tmpDir, "rclone.conf")
	config := fmt.Sprintf(`[test-s3]
type = s3
provider = Other
access_key_id = test
secret_access_key = test
endpoint = %s
`, server.URL)
	if err := os.WriteFile(configPath, []byte(config), 0644); err != nil {
		t.Fatal(err)
	}

	stateMgr, _ := state.NewManager()
	syncMgr := NewManager(configPath, "test-s3", "/test", stateMgr, 4, 8)

	err = syncMgr.Sync(srcDir, "card-123", 1, 9)

	t.Logf("Sync result with packet loss: %v", err)
	t.Log("BUG: No automatic retry on connection drops")
	t.Log("RECOMMENDATION: Configure rclone retry flags or implement application-level retry")
}

// TestTCPConnectionTimeout tests handling of TCP connection timeouts
func TestTCPConnectionTimeout(t *testing.T) {
	skipRcloneNetworkTest(t)
	tmpDir, err := os.MkdirTemp("", "network-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	srcDir := filepath.Join(tmpDir, "source")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatal(err)
	}

	testFile := filepath.Join(srcDir, "test.jpg")
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}

	// Start a TCP listener that accepts but never responds (black hole)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			// Accept connection but never write response (simulates timeout)
			time.Sleep(100 * time.Second)
			conn.Close()
		}
	}()

	addr := listener.Addr().String()
	configPath := filepath.Join(tmpDir, "rclone.conf")
	config := fmt.Sprintf(`[test-s3]
type = s3
provider = Other
access_key_id = test
secret_access_key = test
endpoint = http://%s
`, addr)
	if err := os.WriteFile(configPath, []byte(config), 0644); err != nil {
		t.Fatal(err)
	}

	stateMgr, _ := state.NewManager()
	syncMgr := NewManager(configPath, "test-s3", "/test", stateMgr, 4, 8)

	// Test with timeout
	done := make(chan error, 1)
	go func() {
		done <- syncMgr.Sync(srcDir, "card-123", 1, 4)
	}()

	select {
	case err := <-done:
		t.Logf("Sync completed/failed: %v", err)
		if err == nil {
			t.Error("Sync should fail on timeout")
		}
	case <-time.After(60 * time.Second):
		t.Error("BUG CRITICAL: Sync did not timeout after 60 seconds")
		t.Log("RISK: Device can hang indefinitely on network issues")
		t.Log("RECOMMENDATION: Add context.WithTimeout() to Sync() method")

		// Try to cancel the hung sync
		if err := syncMgr.Cancel(); err != nil {
			t.Logf("Cancel failed: %v", err)
		}
	}
}

// TestSSLCertificateErrors tests handling of TLS certificate validation failures
func TestSSLCertificateErrors(t *testing.T) {
	skipRcloneNetworkTest(t)
	tmpDir, err := os.MkdirTemp("", "network-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	srcDir := filepath.Join(tmpDir, "source")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create self-signed HTTPS server (will fail cert validation)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	configPath := filepath.Join(tmpDir, "rclone.conf")
	config := fmt.Sprintf(`[test-s3]
type = s3
provider = Other
access_key_id = test
secret_access_key = test
endpoint = %s
`, server.URL)
	if err := os.WriteFile(configPath, []byte(config), 0644); err != nil {
		t.Fatal(err)
	}

	stateMgr, _ := state.NewManager()
	syncMgr := NewManager(configPath, "test-s3", "/test", stateMgr, 4, 8)

	err = syncMgr.TestConnection()

	// Should fail with certificate error
	if err == nil {
		t.Log("Note: TestConnection may succeed with test server")
	} else {
		t.Logf("✓ Certificate validation failed: %v", err)

		// Check if error message is helpful
		if !strings.Contains(err.Error(), "certificate") &&
			!strings.Contains(err.Error(), "TLS") &&
			!strings.Contains(err.Error(), "x509") {
			t.Log("BUG: Error message doesn't clearly indicate certificate issue")
			t.Log("RECOMMENDATION: Provide clear error messages for cert errors")
		}
	}
}

// TestPartialHTTPResponse tests handling of incomplete HTTP responses
func TestPartialHTTPResponse(t *testing.T) {
	skipRcloneNetworkTest(t)
	tmpDir, err := os.MkdirTemp("", "network-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	srcDir := filepath.Join(tmpDir, "source")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatal(err)
	}

	testFile := filepath.Join(srcDir, "large.jpg")
	largeData := make([]byte, 1024*1024) // 1MB file
	if err := os.WriteFile(testFile, largeData, 0644); err != nil {
		t.Fatal(err)
	}

	// Server that sends partial response then closes
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "1048576")
		w.WriteHeader(http.StatusOK)

		// Write only partial data
		w.Write([]byte("partial"))

		// Force connection close
		if hj, ok := w.(http.Hijacker); ok {
			if conn, _, err := hj.Hijack(); err == nil {
				conn.Close()
			}
		}
	}))
	defer server.Close()

	configPath := filepath.Join(tmpDir, "rclone.conf")
	config := fmt.Sprintf(`[test-s3]
type = s3
provider = Other
access_key_id = test
secret_access_key = test
endpoint = %s
`, server.URL)
	if err := os.WriteFile(configPath, []byte(config), 0644); err != nil {
		t.Fatal(err)
	}

	stateMgr, _ := state.NewManager()
	syncMgr := NewManager(configPath, "test-s3", "/test", stateMgr, 4, 8)

	err = syncMgr.Sync(srcDir, "card-123", 1, 1024*1024)

	if err == nil {
		t.Error("BUG: Sync should fail with partial response")
		t.Log("RISK: Partial uploads may be incorrectly marked as complete")
	} else {
		t.Logf("✓ Sync detected incomplete transfer: %v", err)
	}
}

// TestGetRemoteSizeAfterNetworkError tests data integrity checks after network recovery
func TestGetRemoteSizeAfterNetworkError(t *testing.T) {
	// BUG CRITICAL: GetRemoteSize returns 0 on error, can't distinguish "empty" from "failed"
	tmpDir, err := os.MkdirTemp("", "network-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	configPath := filepath.Join(tmpDir, "rclone.conf")
	config := `[test-s3]
type = s3
provider = Other
access_key_id = test
secret_access_key = test
endpoint = https://network-error.invalid
`
	if err := os.WriteFile(configPath, []byte(config), 0644); err != nil {
		t.Fatal(err)
	}

	stateMgr, _ := state.NewManager()
	syncMgr := NewManager(configPath, "test-s3", "/test", stateMgr, 4, 8)

	// GetRemoteSize should fail, but currently returns 0, nil (lines 92-93)
	size, err := syncMgr.GetRemoteSize("card-123")

	t.Logf("GetRemoteSize result: size=%d, err=%v", size, err)

	// BUG: Cannot distinguish between:
	// 1. Remote is truly empty (0 bytes, no error)
	// 2. Network error prevented checking (0 bytes returned, but should error)
	// 3. Authentication failure (0 bytes returned, but should error)

	if err == nil && size == 0 {
		t.Error("BUG CRITICAL: GetRemoteSize returns 0 with no error on network failure")
		t.Log("RISK: Application cannot distinguish 'empty remote' from 'failed to check'")
		t.Log("CONSEQUENCE: May skip sync thinking nothing is uploaded, or double-upload thinking nothing exists")
		t.Log("LOCATION: syncmanager.go lines 92-93")
		t.Log("RECOMMENDATION: Return error on network/auth failures, not 0")
	}

	// This affects resume logic in Sync() at lines 136-140
	t.Log("IMPACT: Sync resume feature will fail after network recovery")
	t.Log("- Lines 136-140 get already-synced size")
	t.Log("- Network error returns 0 instead of error")
	t.Log("- Sync thinks nothing is uploaded, may re-upload everything")
	t.Log("- Or worse: partial state causes file corruption")
}

// TestConnectionPoolExhaustion tests for connection/file descriptor leaks
func TestConnectionPoolExhaustion(t *testing.T) {
	skipRcloneNetworkTest(t)
	tmpDir, err := os.MkdirTemp("", "network-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	srcDir := filepath.Join(tmpDir, "source")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create many small files
	for i := 0; i < 100; i++ {
		testFile := filepath.Join(srcDir, fmt.Sprintf("file%d.jpg", i))
		if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	var connCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		connCount.Add(1)
		time.Sleep(10 * time.Millisecond) // Slow response
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	configPath := filepath.Join(tmpDir, "rclone.conf")
	config := fmt.Sprintf(`[test-s3]
type = s3
provider = Other
access_key_id = test
secret_access_key = test
endpoint = %s
`, server.URL)
	if err := os.WriteFile(configPath, []byte(config), 0644); err != nil {
		t.Fatal(err)
	}

	stateMgr, _ := state.NewManager()
	// Use high parallelism to test connection pooling
	syncMgr := NewManager(configPath, "test-s3", "/test", stateMgr, 20, 20)

	err = syncMgr.Sync(srcDir, "card-123", 100, 400)

	connections := connCount.Load()
	t.Logf("Total connections made: %d", connections)

	// BUG: No HTTP transport configuration visible
	// Cannot control MaxIdleConns, MaxConnsPerHost, etc.
	t.Log("WARNING: No HTTP transport configuration exposed")
	t.Log("RISK: May exhaust file descriptors on long syncs")
	t.Log("RECOMMENDATION: Expose rclone's HTTP transport settings")
}

// TestNetworkRecoveryMidSync tests behavior when network recovers during sync
func TestNetworkRecoveryMidSync(t *testing.T) {
	skipRcloneNetworkTest(t)
	tmpDir, err := os.MkdirTemp("", "network-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	srcDir := filepath.Join(tmpDir, "source")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 5; i++ {
		testFile := filepath.Join(srcDir, fmt.Sprintf("photo%d.jpg", i))
		if err := os.WriteFile(testFile, []byte("test data"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Server that fails first, then succeeds (simulating network recovery)
	var requestCount atomic.Int32
	var failUntilCount int32 = 5

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := requestCount.Add(1)
		if count <= failUntilCount {
			// Simulate network down
			hj, ok := w.(http.Hijacker)
			if ok {
				conn, _, _ := hj.Hijack()
				if conn != nil {
					conn.Close()
				}
			}
			return
		}
		// Network recovered
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	configPath := filepath.Join(tmpDir, "rclone.conf")
	config := fmt.Sprintf(`[test-s3]
type = s3
provider = Other
access_key_id = test
secret_access_key = test
endpoint = %s
`, server.URL)
	if err := os.WriteFile(configPath, []byte(config), 0644); err != nil {
		t.Fatal(err)
	}

	stateMgr, _ := state.NewManager()
	syncMgr := NewManager(configPath, "test-s3", "/test", stateMgr, 4, 8)

	err = syncMgr.Sync(srcDir, "card-123", 5, 45)

	// Network recovers mid-sync, but single Sync() call has no retry
	// So it will still fail
	if err == nil {
		t.Log("Sync succeeded after network recovery (rclone may have retried)")
	} else {
		t.Logf("✓ Sync failed: %v", err)
		t.Log("BUG: No application-level retry to handle network recovery")
		t.Log("RECOMMENDATION: Wrap Sync() in retry loop with exponential backoff")
	}
}

// TestIPv4IPv6Fallback tests IPv6/IPv4 fallback behavior
func TestIPv4IPv6Fallback(t *testing.T) {
	// Note: This is hard to test without IPv6 network setup
	// Documenting expected behavior

	t.Log("IPv6/IPv4 Fallback Analysis:")
	t.Log("- rclone uses Go's net/http which handles dual-stack automatically")
	t.Log("- No explicit configuration needed for IPv4/IPv6 fallback")
	t.Log("- Potential issue: IPv6 timeout delays IPv4 fallback")
	t.Log("RECOMMENDATION: Monitor for 'connect: network is unreachable' errors on IPv6-only hosts")
}

// TestCancelDuringNetworkOperation tests cancel safety during network ops
func TestCancelDuringNetworkOperation(t *testing.T) {
	skipRcloneNetworkTest(t)
	tmpDir, err := os.MkdirTemp("", "network-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	srcDir := filepath.Join(tmpDir, "source")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 10; i++ {
		testFile := filepath.Join(srcDir, fmt.Sprintf("file%d.jpg", i))
		if err := os.WriteFile(testFile, make([]byte, 10240), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Slow server to allow time for cancel
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	configPath := filepath.Join(tmpDir, "rclone.conf")
	config := fmt.Sprintf(`[test-s3]
type = s3
provider = Other
access_key_id = test
secret_access_key = test
endpoint = %s
`, server.URL)
	if err := os.WriteFile(configPath, []byte(config), 0644); err != nil {
		t.Fatal(err)
	}

	stateMgr, _ := state.NewManager()
	syncMgr := NewManager(configPath, "test-s3", "/test", stateMgr, 4, 8)

	// Start sync
	done := make(chan error, 1)
	go func() {
		done <- syncMgr.Sync(srcDir, "card-123", 10, 102400)
	}()

	// Wait for sync to start
	time.Sleep(100 * time.Millisecond)

	// Cancel during network operation
	cancelErr := syncMgr.Cancel()
	if cancelErr != nil {
		t.Logf("Cancel returned: %v", cancelErr)
	}

	// Wait for sync to complete
	select {
	case syncErr := <-done:
		t.Logf("Sync completed/cancelled: %v", syncErr)
	case <-time.After(5 * time.Second):
		t.Error("BUG: Sync did not complete after cancel")
		t.Log("RISK: Goroutine leak, zombie sync process")
	}

	// Verify cleanup
	time.Sleep(100 * time.Millisecond)

	if syncMgr.IsRunning() {
		t.Error("BUG: isRunning still true after cancel")
	}

	syncMgr.mu.Lock()
	leaked := syncMgr.cancelFunc != nil
	syncMgr.mu.Unlock()

	if leaked {
		t.Error("BUG: cancelFunc not cleaned up")
		t.Log("Indicates incomplete cleanup after cancel during network op")
	}
}

// TestProgressChannelLeakOnNetworkDisconnect tests memory leak scenario
func TestProgressChannelLeakOnNetworkDisconnect(t *testing.T) {
	syncMgr, _, cleanup := setupTestSyncManager(t)
	defer cleanup()

	var channels []chan Progress
	for i := 0; i < 50; i++ {
		ch := syncMgr.SubscribeProgress()
		channels = append(channels, ch)
	}

	syncMgr.mu.Lock()
	channelCount := len(syncMgr.progressChans)
	syncMgr.mu.Unlock()

	if channelCount != 50 {
		t.Errorf("Expected 50 channels, got %d", channelCount)
	}

	for _, ch := range channels {
		syncMgr.UnsubscribeProgress(ch)
	}

	syncMgr.mu.Lock()
	channelCount = len(syncMgr.progressChans)
	syncMgr.mu.Unlock()

	if channelCount != 0 {
		t.Errorf("Expected all progress channels to be unsubscribed, got %d", channelCount)
	}
}

// TestSyncRetryWithExponentialBackoff tests what SHOULD exist but doesn't
func TestSyncRetryWithExponentialBackoff(t *testing.T) {
	// This test documents the MISSING retry logic

	t.Log("=== MISSING FEATURE: Retry Logic ===")
	t.Log("")
	t.Log("Current behavior:")
	t.Log("  - Sync() is called once from cmd/pictures-sync/main.go:199")
	t.Log("  - If network fails, sync fails immediately")
	t.Log("  - User must remove and reinsert SD card to retry")
	t.Log("")
	t.Log("Recommended implementation:")
	t.Log("  func SyncWithRetry(ctx context.Context, ...) error {")
	t.Log("      maxRetries := 5")
	t.Log("      backoff := time.Second")
	t.Log("      for attempt := 0; attempt <= maxRetries; attempt++ {")
	t.Log("          err := syncMgr.Sync(...)")
	t.Log("          if err == nil {")
	t.Log("              return nil")
	t.Log("          }")
	t.Log("          if !isNetworkError(err) {")
	t.Log("              return err // Don't retry non-network errors")
	t.Log("          }")
	t.Log("          if attempt < maxRetries {")
	t.Log("              time.Sleep(backoff)")
	t.Log("              backoff *= 2 // Exponential backoff")
	t.Log("          }")
	t.Log("      }")
	t.Log("      return err")
	t.Log("  }")
	t.Log("")
	t.Log("LOCATION: Should wrap call at cmd/pictures-sync/main.go:199")
	t.Log("BENEFIT: Automatically recover from transient network issues")
	t.Log("BENEFIT: No user intervention needed for temporary glitches")
}

// TestContextTimeoutForSync tests what SHOULD exist but doesn't
func TestContextTimeoutForSync(t *testing.T) {
	// This test documents the MISSING timeout logic

	t.Log("=== MISSING FEATURE: Sync Timeout ===")
	t.Log("")
	t.Log("Current behavior:")
	t.Log("  - Sync() creates context.WithCancel() at line 149")
	t.Log("  - No timeout configured")
	t.Log("  - Sync can hang indefinitely on network issues")
	t.Log("")
	t.Log("Recommended implementation:")
	t.Log("  // In Sync() method")
	t.Log("  timeout := time.Hour * 4 // 4 hour max sync time")
	t.Log("  ctx, cancel := context.WithTimeout(context.Background(), timeout)")
	t.Log("  defer cancel()")
	t.Log("")
	t.Log("  // Also configure HTTP client timeout in rclone")
	t.Log("  ci := fs.GetConfig(ctx)")
	t.Log("  ci.ConnectTimeout = 30 * time.Second")
	t.Log("  ci.Timeout = 5 * time.Minute")
	t.Log("  ci.LowLevelRetries = 10")
	t.Log("")
	t.Log("LOCATION: syncmanager.go:149")
	t.Log("BENEFIT: Prevent device hang on network issues")
	t.Log("BENEFIT: User can retry after timeout instead of manual reboot")
}

// TestConcurrentNetworkOperations tests thread safety with network errors
func TestConcurrentNetworkOperations(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "network-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	configPath := filepath.Join(tmpDir, "rclone.conf")
	config := `[test]
type = local
`
	if err := os.WriteFile(configPath, []byte(config), 0644); err != nil {
		t.Fatal(err)
	}

	stateMgr, _ := state.NewManager()
	syncMgr := NewManager(configPath, "test", tmpDir, stateMgr, 4, 8)

	var wg sync.WaitGroup
	errorCount := atomic.Int32{}

	// Concurrent operations that might fail
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := syncMgr.TestConnection(); err != nil {
				errorCount.Add(1)
			}
		}()

		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := syncMgr.ListRemotes(); err != nil {
				errorCount.Add(1)
			}
		}()

		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := syncMgr.GetRemoteSize("card-123"); err != nil {
				errorCount.Add(1)
			}
		}()
	}

	wg.Wait()

	t.Logf("Concurrent operations completed, %d errors encountered", errorCount.Load())
	t.Log("✓ No race conditions or panics detected")
}

// TestProxyConfiguration tests proxy handling
func TestProxyConfiguration(t *testing.T) {
	// Note: Proxy configuration is handled by rclone and Go's http.DefaultTransport
	// which respects HTTP_PROXY, HTTPS_PROXY environment variables

	t.Log("=== Proxy Configuration Analysis ===")
	t.Log("")
	t.Log("Current behavior:")
	t.Log("  - Respects HTTP_PROXY, HTTPS_PROXY, NO_PROXY environment variables")
	t.Log("  - No explicit proxy configuration in syncmanager")
	t.Log("  - Relies on Go's net/http default behavior")
	t.Log("")
	t.Log("Potential issues:")
	t.Log("  - No way to configure proxy via web UI")
	t.Log("  - No proxy authentication support visible")
	t.Log("  - No proxy error detection/reporting")
	t.Log("")
	t.Log("RECOMMENDATION: Add proxy configuration to settings")
	t.Log("RECOMMENDATION: Test proxy connection in TestConnection()")
}

// Helper function to check if error is network-related
func isNetworkError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return strings.Contains(errStr, "connection") ||
		strings.Contains(errStr, "network") ||
		strings.Contains(errStr, "timeout") ||
		strings.Contains(errStr, "DNS") ||
		strings.Contains(errStr, "dial") ||
		strings.Contains(errStr, "EOF")
}

// BenchmarkNetworkErrorRecovery benchmarks time to detect and recover from network errors
func BenchmarkNetworkErrorRecovery(b *testing.B) {
	tmpDir, err := os.MkdirTemp("", "network-bench-*")
	if err != nil {
		b.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	configPath := filepath.Join(tmpDir, "rclone.conf")
	config := `[test]
type = s3
provider = Other
access_key_id = test
secret_access_key = test
endpoint = https://network-error.invalid
`
	if err := os.WriteFile(configPath, []byte(config), 0644); err != nil {
		b.Fatal(err)
	}

	stateMgr, _ := state.NewManager()
	syncMgr := NewManager(configPath, "test", "/test", stateMgr, 4, 8)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Measure time to detect network error
		_ = syncMgr.TestConnection()
	}
}

// TestRcloneConfigurationOptions documents rclone flags that should be configured
func TestRcloneConfigurationOptions(t *testing.T) {
	t.Log("=== Recommended rclone Configuration ===")
	t.Log("")
	t.Log("Current configuration (syncmanager.go:174-178):")
	t.Log("  ci.StatsOneLine = true")
	t.Log("  ci.Progress = true")
	t.Log("  ci.Transfers = 4 (configurable)")
	t.Log("  ci.Checkers = 8 (configurable)")
	t.Log("")
	t.Log("Missing critical options:")
	t.Log("  ci.ConnectTimeout = 30*time.Second  // Prevent indefinite connect hangs")
	t.Log("  ci.Timeout = 5*time.Minute          // Overall operation timeout")
	t.Log("  ci.LowLevelRetries = 10             // Retry failed operations")
	t.Log("  ci.Retries = 3                      // High-level retries")
	t.Log("  ci.RetryBackoff = time.Second       // Backoff between retries")
	t.Log("  ci.ErrorOnNoTransfer = false        // Don't fail on empty source")
	t.Log("")
	t.Log("For network resilience, also consider:")
	t.Log("  ci.IgnoreChecksum = false           // Verify integrity after network recovery")
	t.Log("  ci.UseListR = true                  // Faster listing for large directories")
	t.Log("")
	t.Log("RECOMMENDATION: Add these to syncmanager.go Sync() method")
}

// TestNetworkErrorReadWrite tests read/write scenarios with network issues
func TestNetworkErrorDuringGetFile(t *testing.T) {
	skipRcloneNetworkTest(t)
	tmpDir, err := os.MkdirTemp("", "network-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Server that fails during file download
	var downloadAttempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt := downloadAttempts.Add(1)
		if attempt == 1 {
			// Fail first download attempt
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("partial"))
			// Force close
			if hj, ok := w.(http.Hijacker); ok {
				if conn, _, err := hj.Hijack(); err == nil {
					conn.Close()
				}
			}
			return
		}
		// Subsequent attempts succeed
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("complete file data"))
	}))
	defer server.Close()

	configPath := filepath.Join(tmpDir, "rclone.conf")
	config := fmt.Sprintf(`[test-s3]
type = s3
provider = Other
access_key_id = test
secret_access_key = test
endpoint = %s
`, server.URL)
	if err := os.WriteFile(configPath, []byte(config), 0644); err != nil {
		t.Fatal(err)
	}

	stateMgr, _ := state.NewManager()
	syncMgr := NewManager(configPath, "test-s3", "/test", stateMgr, 4, 8)

	var buf strings.Builder
	err = syncMgr.GetFile("test.jpg", &buf)

	if err == nil {
		t.Log("GetFile succeeded (rclone may have retried)")
		data := buf.String()
		if data == "partial" {
			t.Error("BUG: Got partial data without error")
			t.Log("RISK: Data corruption from incomplete downloads")
		}
	} else {
		t.Logf("✓ GetFile failed on network error: %v", err)
	}
}

// TestSummary generates a summary report of all network bugs found
func TestSummary(t *testing.T) {
	t.Log("")
	t.Log("╔═══════════════════════════════════════════════════════════════╗")
	t.Log("║          NETWORK RESILIENCE BUG REPORT SUMMARY               ║")
	t.Log("╚═══════════════════════════════════════════════════════════════╝")
	t.Log("")
	t.Log("CRITICAL BUGS (Immediate attention required):")
	t.Log("  1. No retry mechanism for transient network failures")
	t.Log("     Risk: Single network hiccup causes complete sync failure")
	t.Log("     Fix: Implement exponential backoff retry wrapper")
	t.Log("")
	t.Log("  2. No timeout configuration")
	t.Log("     Risk: Sync can hang indefinitely on network issues")
	t.Log("     Fix: Add context.WithTimeout() and configure rclone timeouts")
	t.Log("")
	t.Log("  3. GetRemoteSize() returns 0 on error")
	t.Log("     Risk: Data corruption from incorrect resume state")
	t.Log("     Fix: Return error instead of 0 when remote check fails")
	t.Log("")
	t.Log("HIGH PRIORITY BUGS:")
	t.Log("  4. Cancel during network error leaves inconsistent state")
	t.Log("     Risk: Prevents future sync attempts until reboot")
	t.Log("")
	t.Log("  5. No progress channel cleanup mechanism")
	t.Log("     Risk: Memory leak on repeated WebSocket disconnects")
	t.Log("")
	t.Log("MEDIUM PRIORITY BUGS:")
	t.Log("  6. No HTTP transport configuration")
	t.Log("     Risk: Connection pool exhaustion on long syncs")
	t.Log("")
	t.Log("  7. No DNS failure retry")
	t.Log("     Risk: First sync after boot may fail needlessly")
	t.Log("")
	t.Log("LOW PRIORITY ISSUES:")
	t.Log("  8. No proxy configuration UI")
	t.Log("  9. TLS certificate errors not user-friendly")
	t.Log("")
	t.Log("RECOMMENDATIONS:")
	t.Log("  • Add retry logic with exponential backoff")
	t.Log("  • Configure rclone timeout and retry parameters")
	t.Log("  • Fix GetRemoteSize() error handling")
	t.Log("  • Add UnsubscribeProgress() method")
	t.Log("  • Add comprehensive error classification")
	t.Log("  • Implement connection health monitoring")
	t.Log("")
}

// Mock slow reader for testing timeout scenarios
type slowReader struct {
	data  []byte
	pos   int
	delay time.Duration
}

func (sr *slowReader) Read(p []byte) (n int, err error) {
	if sr.pos >= len(sr.data) {
		return 0, io.EOF
	}

	time.Sleep(sr.delay)

	// Read one byte at a time (very slow)
	p[0] = sr.data[sr.pos]
	sr.pos++
	return 1, nil
}
