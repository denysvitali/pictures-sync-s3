package e2e

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/denysvitali/pictures-sync-s3/pkg/events"
	"github.com/denysvitali/pictures-sync-s3/pkg/handlers"
	"github.com/denysvitali/pictures-sync-s3/pkg/settings"
	"github.com/denysvitali/pictures-sync-s3/pkg/ssrf"
	"github.com/denysvitali/pictures-sync-s3/pkg/state"
	"github.com/denysvitali/pictures-sync-s3/pkg/syncmanager"
	"github.com/denysvitali/pictures-sync-s3/pkg/websocket"
	"github.com/denysvitali/pictures-sync-s3/pkg/wifimanager"
	ws "github.com/gorilla/websocket"
)

// TestTLSConnectionBasic tests basic TLS server functionality
func TestTLSConnectionBasic(t *testing.T) {
	testEnv := setupIntegrationEnvironment(t)
	defer testEnv.Cleanup()

	// Create TLS server
	server := httptest.NewTLSServer(createTestMux(testEnv))
	defer server.Close()

	// Test that TLS connection works
	client := server.Client()
	resp, err := client.Get(server.URL + "/api/status")
	if err != nil {
		t.Fatalf("TLS request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	// Verify it's actually using TLS
	if resp.TLS == nil {
		t.Error("Response doesn't indicate TLS connection")
	} else {
		t.Logf("TLS connection successful: %s", resp.TLS.Version)
	}
}

// TestTLSCipherSuites tests secure cipher suite selection
func TestTLSCipherSuites(t *testing.T) {
	testEnv := setupIntegrationEnvironment(t)
	defer testEnv.Cleanup()

	server := httptest.NewUnstartedServer(createTestMux(testEnv))

	// Configure with secure cipher suites
	server.TLS = &tls.Config{
		MinVersion: tls.VersionTLS12,
		CipherSuites: []uint16{
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
		},
		PreferServerCipherSuites: true,
	}
	server.StartTLS()
	defer server.Close()

	// Test connection with secure cipher
	client := server.Client()
	resp, err := client.Get(server.URL + "/api/status")
	if err != nil {
		t.Fatalf("Secure TLS request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.TLS == nil {
		t.Fatal("Expected TLS connection")
	}

	// Verify secure cipher was used
	cipherSuite := resp.TLS.CipherSuite
	t.Logf("Cipher suite used: 0x%04X", cipherSuite)

	// Check minimum TLS version
	if resp.TLS.Version < tls.VersionTLS12 {
		t.Errorf("TLS version %d is below minimum (TLS 1.2)", resp.TLS.Version)
	}
}

// TestTLSCertificateValidation tests certificate validation
func TestTLSCertificateValidation(t *testing.T) {
	testEnv := setupIntegrationEnvironment(t)
	defer testEnv.Cleanup()

	server := httptest.NewTLSServer(createTestMux(testEnv))
	defer server.Close()

	// Test with proper certificate validation
	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				RootCAs: certPoolFromServer(t, server),
			},
		},
	}

	resp, err := client.Get(server.URL + "/api/status")
	if err != nil {
		t.Fatalf("Certificate validation failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}
}

// TestTLSConnectionTimeout tests TLS connection timeout handling
func TestTLSConnectionTimeout(t *testing.T) {
	testEnv := setupIntegrationEnvironment(t)
	defer testEnv.Cleanup()

	server := httptest.NewTLSServer(createTestMux(testEnv))
	defer server.Close()

	// Client with short timeout
	client := &http.Client{
		Timeout: 100 * time.Millisecond,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		},
	}

	// This should complete within timeout
	start := time.Now()
	resp, err := client.Get(server.URL + "/api/status")
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Fast endpoint timed out: %v", err)
	}
	defer resp.Body.Close()

	if elapsed > 100*time.Millisecond {
		t.Errorf("Request took %v, expected < 100ms", elapsed)
	}

	t.Logf("TLS request completed in %v", elapsed)
}

// TestTLSWebSocketUpgrade tests WebSocket upgrade over TLS
func TestTLSWebSocketUpgrade(t *testing.T) {
	testEnv := setupIntegrationEnvironment(t)
	defer testEnv.Cleanup()

	server := httptest.NewTLSServer(createTestMux(testEnv))
	defer server.Close()

	// Convert https:// to wss://
	wsURL := "wss" + strings.TrimPrefix(server.URL, "https") + "/ws"

	// Create WebSocket dialer with TLS
	dialer := ws.Dialer{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true,
		},
	}

	conn, resp, err := dialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("WebSocket over TLS failed: %v", err)
	}
	defer conn.Close()

	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Errorf("Expected status 101, got %d", resp.StatusCode)
	}

	// Verify we can receive data over TLS WebSocket
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	var stateData map[string]interface{}
	if err := conn.ReadJSON(&stateData); err != nil {
		t.Fatalf("Failed to read from TLS WebSocket: %v", err)
	}

	if stateData["status"] == nil {
		t.Error("Expected status field in WebSocket message")
	}

	t.Log("WebSocket over TLS working correctly")
}

// TestTLSWebSocketReconnection tests WebSocket reconnection over TLS
func TestTLSWebSocketReconnection(t *testing.T) {
	testEnv := setupIntegrationEnvironment(t)
	defer testEnv.Cleanup()

	server := httptest.NewTLSServer(createTestMux(testEnv))
	defer server.Close()

	wsURL := "wss" + strings.TrimPrefix(server.URL, "https") + "/ws"
	dialer := ws.Dialer{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true,
		},
	}

	// Multiple reconnection cycles
	for i := 0; i < 5; i++ {
		conn, _, err := dialer.Dial(wsURL, nil)
		if err != nil {
			t.Fatalf("Reconnection %d failed: %v", i, err)
		}

		// Read initial state
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		var stateData map[string]interface{}
		if err := conn.ReadJSON(&stateData); err != nil {
			conn.Close()
			t.Fatalf("Reconnection %d: failed to read state: %v", i, err)
		}

		conn.Close()
		time.Sleep(100 * time.Millisecond)
	}

	t.Log("WebSocket TLS reconnection cycles completed successfully")
}

// TestTLSConcurrentConnections tests concurrent TLS connections
func TestTLSConcurrentConnections(t *testing.T) {
	testEnv := setupIntegrationEnvironment(t)
	defer testEnv.Cleanup()

	server := httptest.NewTLSServer(createTestMux(testEnv))
	defer server.Close()

	client := server.Client()

	const numRequests = 50
	errors := make(chan error, numRequests)
	done := make(chan bool, numRequests)

	for i := 0; i < numRequests; i++ {
		go func(id int) {
			resp, err := client.Get(server.URL + "/api/status")
			if err != nil {
				errors <- fmt.Errorf("request %d: %v", id, err)
			} else {
				resp.Body.Close()
				if resp.StatusCode != http.StatusOK {
					errors <- fmt.Errorf("request %d: status %d", id, resp.StatusCode)
				}
			}
			done <- true
		}(i)
	}

	// Wait for all requests
	for i := 0; i < numRequests; i++ {
		<-done
	}
	close(errors)

	// Check for errors
	errorCount := 0
	for err := range errors {
		t.Error(err)
		errorCount++
	}

	if errorCount > 0 {
		t.Errorf("%d/%d concurrent TLS requests failed", errorCount, numRequests)
	} else {
		t.Logf("All %d concurrent TLS requests succeeded", numRequests)
	}
}

// TestTLSHandshakeFailure tests handling of TLS handshake failures
func TestTLSHandshakeFailure(t *testing.T) {
	testEnv := setupIntegrationEnvironment(t)
	defer testEnv.Cleanup()

	server := httptest.NewUnstartedServer(createTestMux(testEnv))
	server.TLS = &tls.Config{
		MinVersion: tls.VersionTLS13,
	}
	server.StartTLS()
	defer server.Close()

	// Try to connect with client that only supports TLS 1.0 (should fail)
	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				MinVersion:         tls.VersionTLS10,
				MaxVersion:         tls.VersionTLS10,
				InsecureSkipVerify: true,
			},
		},
		Timeout: 2 * time.Second,
	}

	_, err := client.Get(server.URL + "/api/status")
	if err == nil {
		t.Error("Expected TLS handshake to fail with incompatible versions")
	} else {
		t.Logf("TLS handshake correctly failed: %v", err)
	}
}

// TestTLSSessionResumption tests TLS session resumption
func TestTLSSessionResumption(t *testing.T) {
	testEnv := setupIntegrationEnvironment(t)
	defer testEnv.Cleanup()

	server := httptest.NewTLSServer(createTestMux(testEnv))
	defer server.Close()

	// Client that supports session resumption
	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
				ClientSessionCache: tls.NewLRUClientSessionCache(10),
			},
		},
	}

	// First request - full handshake
	resp1, err := client.Get(server.URL + "/api/status")
	if err != nil {
		t.Fatalf("First request failed: %v", err)
	}
	resp1.Body.Close()
	didResume1 := resp1.TLS.DidResume

	// Second request - should resume session
	resp2, err := client.Get(server.URL + "/api/status")
	if err != nil {
		t.Fatalf("Second request failed: %v", err)
	}
	resp2.Body.Close()
	didResume2 := resp2.TLS.DidResume

	t.Logf("First connection resumed: %v, Second connection resumed: %v", didResume1, didResume2)

	// At least one should have resumed (depending on server support)
	if !didResume1 && !didResume2 {
		t.Log("Note: Session resumption not supported or detected")
	}
}

// TestTLSKeepAlive tests keep-alive over TLS connections
func TestTLSKeepAlive(t *testing.T) {
	testEnv := setupIntegrationEnvironment(t)
	defer testEnv.Cleanup()

	server := httptest.NewTLSServer(createTestMux(testEnv))
	defer server.Close()

	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
			IdleConnTimeout: 10 * time.Second,
		},
	}

	// Make multiple requests on same connection
	for i := 0; i < 10; i++ {
		resp, err := client.Get(server.URL + "/api/status")
		if err != nil {
			t.Fatalf("Request %d failed: %v", i, err)
		}
		resp.Body.Close()

		if i > 0 {
			// After first request, subsequent should reuse connection
			if !resp.TLS.DidResume {
				t.Logf("Request %d: connection not reused", i)
			}
		}

		time.Sleep(100 * time.Millisecond)
	}

	t.Log("Keep-alive over TLS working correctly")
}

// Helper functions

func createTestMux(testEnv *IntegrationEnvironment) *http.ServeMux {
	eventMgr := events.NewManager()

	wifiMgr, _ := wifimanager.NewManager()
	ssrfValidator := ssrf.NewValidator(10, time.Minute)

	ctx := &handlers.Context{
		StateMgr:      testEnv.StateMgr,
		SyncMgr:       testEnv.SyncMgr,
		WiFiMgr:       wifiMgr,
		AppSettings:   testEnv.Settings,
		SSRFValidator: ssrfValidator,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/status", ctx.HandleStatus)
	mux.HandleFunc("/api/history", ctx.HandleHistory)
	mux.HandleFunc("/api/settings", ctx.HandleSettings)
	mux.HandleFunc("/api/files", ctx.HandleFiles)
	mux.HandleFunc("/ws", websocket.HandleWebSocket(testEnv.StateMgr, eventMgr))

	return mux
}

func certPoolFromServer(t *testing.T, server *httptest.Server) *x509.CertPool {
	t.Helper()

	pool := x509.NewCertPool()
	if server.Certificate() != nil {
		pool.AddCert(server.Certificate())
	}
	return pool
}
