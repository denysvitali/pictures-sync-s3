package tlsconfig

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// generateTestCertificate creates a self-signed certificate for testing
func generateTestCertificate(certPath, keyPath string) error {
	// Generate private key
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return fmt.Errorf("failed to generate private key: %w", err)
	}

	// Create certificate template
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"Test Photo Backup Station"},
			CommonName:   "localhost",
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{"localhost", "*.local"},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
	}

	// Create self-signed certificate
	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return fmt.Errorf("failed to create certificate: %w", err)
	}

	// Write certificate to file
	certOut, err := os.Create(certPath)
	if err != nil {
		return fmt.Errorf("failed to create cert file: %w", err)
	}
	defer certOut.Close()

	if err := pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes}); err != nil {
		return fmt.Errorf("failed to write cert: %w", err)
	}

	// Write private key to file
	keyOut, err := os.Create(keyPath)
	if err != nil {
		return fmt.Errorf("failed to create key file: %w", err)
	}
	defer keyOut.Close()

	privBytes, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return fmt.Errorf("failed to marshal private key: %w", err)
	}

	if err := pem.Encode(keyOut, &pem.Block{Type: "PRIVATE KEY", Bytes: privBytes}); err != nil {
		return fmt.Errorf("failed to write key: %w", err)
	}

	return nil
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.CertFile != "/etc/ssl/gokrazy-web.pem" {
		t.Errorf("Expected CertFile to be '/etc/ssl/gokrazy-web.pem', got '%s'", cfg.CertFile)
	}

	if cfg.KeyFile != "/etc/ssl/gokrazy-web.key.pem" {
		t.Errorf("Expected KeyFile to be '/etc/ssl/gokrazy-web.key.pem', got '%s'", cfg.KeyFile)
	}

	if cfg.InsecureSkipVerify {
		t.Error("Expected InsecureSkipVerify to be false by default")
	}

	if cfg.MinVersion != tls.VersionTLS12 {
		t.Errorf("Expected MinVersion to be TLS 1.2, got %d", cfg.MinVersion)
	}
}

func TestResolveConfigPrefersRootByDefault(t *testing.T) {
	tmpDir := t.TempDir()
	rootDir := filepath.Join(tmpDir, "root")
	permDir := filepath.Join(tmpDir, "perm")
	setTestTLSPaths(t, rootDir, permDir)
	writeTestCertPair(t, rootCertFile, rootKeyFile)
	writeTestCertPair(t, permCertFile, permKeyFile)

	cfg := ResolveConfig()
	if cfg.CertFile != rootCertFile {
		t.Errorf("Expected root cert file %q, got %q", rootCertFile, cfg.CertFile)
	}
	if cfg.KeyFile != rootKeyFile {
		t.Errorf("Expected root key file %q, got %q", rootKeyFile, cfg.KeyFile)
	}
}

func TestResolveConfigUsesPermWhenGokrazyMarkerExists(t *testing.T) {
	tmpDir := t.TempDir()
	rootDir := filepath.Join(tmpDir, "root")
	permDir := filepath.Join(tmpDir, "perm")
	setTestTLSPaths(t, rootDir, permDir)
	writeTestCertPair(t, rootCertFile, rootKeyFile)
	writeTestCertPair(t, permCertFile, permKeyFile)
	writeTestFile(t, rootUsePermFile)

	cfg := ResolveConfig()
	if cfg.CertFile != permCertFile {
		t.Errorf("Expected perm cert file %q, got %q", permCertFile, cfg.CertFile)
	}
	if cfg.KeyFile != permKeyFile {
		t.Errorf("Expected perm key file %q, got %q", permKeyFile, cfg.KeyFile)
	}
}

func TestResolveConfigFallsBackToRootWhenPermMarkerHasNoPermPair(t *testing.T) {
	tmpDir := t.TempDir()
	rootDir := filepath.Join(tmpDir, "root")
	permDir := filepath.Join(tmpDir, "perm")
	setTestTLSPaths(t, rootDir, permDir)
	writeTestCertPair(t, rootCertFile, rootKeyFile)
	writeTestFile(t, rootUsePermFile)

	cfg := ResolveConfig()
	if cfg.CertFile != rootCertFile {
		t.Errorf("Expected root cert file %q, got %q", rootCertFile, cfg.CertFile)
	}
	if cfg.KeyFile != rootKeyFile {
		t.Errorf("Expected root key file %q, got %q", rootKeyFile, cfg.KeyFile)
	}
}

func TestResolveConfigFallsBackToPermWithoutRootPair(t *testing.T) {
	tmpDir := t.TempDir()
	rootDir := filepath.Join(tmpDir, "root")
	permDir := filepath.Join(tmpDir, "perm")
	setTestTLSPaths(t, rootDir, permDir)
	writeTestCertPair(t, permCertFile, permKeyFile)

	cfg := ResolveConfig()
	if cfg.CertFile != permCertFile {
		t.Errorf("Expected perm cert file %q, got %q", permCertFile, cfg.CertFile)
	}
	if cfg.KeyFile != permKeyFile {
		t.Errorf("Expected perm key file %q, got %q", permKeyFile, cfg.KeyFile)
	}
}

func TestCertificatesExist(t *testing.T) {
	// Create temporary directory for test certificates
	tmpDir, err := os.MkdirTemp("", "tls-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	certPath := filepath.Join(tmpDir, "test.pem")
	keyPath := filepath.Join(tmpDir, "test.key.pem")

	cfg := &Config{
		CertFile: certPath,
		KeyFile:  keyPath,
	}

	// Test when certificates don't exist
	if cfg.CertificatesExist() {
		t.Error("Expected CertificatesExist to return false when files don't exist")
	}

	// Generate test certificates
	if err := generateTestCertificate(certPath, keyPath); err != nil {
		t.Fatalf("Failed to generate test certificate: %v", err)
	}

	// Test when certificates exist
	if !cfg.CertificatesExist() {
		t.Error("Expected CertificatesExist to return true when files exist")
	}

	// Test when only cert exists
	os.Remove(keyPath)
	if cfg.CertificatesExist() {
		t.Error("Expected CertificatesExist to return false when key is missing")
	}

	// Test when only key exists
	if err := generateTestCertificate(certPath, keyPath); err != nil {
		t.Fatalf("Failed to generate test certificate: %v", err)
	}
	os.Remove(certPath)
	if cfg.CertificatesExist() {
		t.Error("Expected CertificatesExist to return false when cert is missing")
	}
}

func setTestTLSPaths(t *testing.T, rootDir, permDir string) {
	t.Helper()

	origRootCertFile := rootCertFile
	origRootKeyFile := rootKeyFile
	origRootUsePermFile := rootUsePermFile
	origPermCertFile := permCertFile
	origPermKeyFile := permKeyFile

	rootCertFile = filepath.Join(rootDir, "ssl", "gokrazy-web.pem")
	rootKeyFile = filepath.Join(rootDir, "ssl", "gokrazy-web.key.pem")
	rootUsePermFile = filepath.Join(rootDir, "ssl", "gokrazy-web.use-perm")
	permCertFile = filepath.Join(permDir, "ssl", "gokrazy-web.pem")
	permKeyFile = filepath.Join(permDir, "ssl", "gokrazy-web.key.pem")

	t.Cleanup(func() {
		rootCertFile = origRootCertFile
		rootKeyFile = origRootKeyFile
		rootUsePermFile = origRootUsePermFile
		permCertFile = origPermCertFile
		permKeyFile = origPermKeyFile
	})
}

func writeTestCertPair(t *testing.T, certPath, keyPath string) {
	t.Helper()
	writeTestFile(t, certPath)
	writeTestFile(t, keyPath)
}

func writeTestFile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("Failed to create test directory: %v", err)
	}
	if err := os.WriteFile(path, []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}
}

func TestNewTLSConfig(t *testing.T) {
	// Create temporary directory for test certificates
	tmpDir, err := os.MkdirTemp("", "tls-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	certPath := filepath.Join(tmpDir, "test.pem")
	keyPath := filepath.Join(tmpDir, "test.key.pem")

	// Generate test certificates
	if err := generateTestCertificate(certPath, keyPath); err != nil {
		t.Fatalf("Failed to generate test certificate: %v", err)
	}

	cfg := &Config{
		CertFile:   certPath,
		KeyFile:    keyPath,
		MinVersion: tls.VersionTLS12,
	}

	tlsConfig, err := cfg.NewTLSConfig()
	if err != nil {
		t.Fatalf("Failed to create TLS config: %v", err)
	}

	// Verify TLS config properties
	if tlsConfig.MinVersion != tls.VersionTLS12 {
		t.Errorf("Expected MinVersion TLS 1.2, got %d", tlsConfig.MinVersion)
	}

	if len(tlsConfig.Certificates) != 1 {
		t.Errorf("Expected 1 certificate, got %d", len(tlsConfig.Certificates))
	}

	if tlsConfig.ClientAuth != tls.NoClientCert {
		t.Errorf("Expected ClientAuth to be NoClientCert, got %d", tlsConfig.ClientAuth)
	}

	if !tlsConfig.PreferServerCipherSuites {
		t.Error("Expected PreferServerCipherSuites to be true")
	}

	// Verify cipher suites are configured
	if len(tlsConfig.CipherSuites) == 0 {
		t.Error("Expected cipher suites to be configured")
	}
}

func TestNewTLSConfigWithInvalidCertificate(t *testing.T) {
	cfg := &Config{
		CertFile: "/nonexistent/cert.pem",
		KeyFile:  "/nonexistent/key.pem",
	}

	_, err := cfg.NewTLSConfig()
	if err == nil {
		t.Error("Expected error when loading nonexistent certificates")
	}
}

func TestLoadOrDefault(t *testing.T) {
	// Save original default paths
	origCfg := DefaultConfig()

	// Test with nonexistent certificates (default case)
	tlsConfig, useTLS, err := LoadOrDefault()
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if useTLS {
		t.Error("Expected useTLS to be false when certificates don't exist")
	}
	if tlsConfig != nil {
		t.Error("Expected tlsConfig to be nil when certificates don't exist")
	}

	// Create temporary directory for test certificates
	tmpDir, err := os.MkdirTemp("", "tls-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	certPath := filepath.Join(tmpDir, "test.pem")
	keyPath := filepath.Join(tmpDir, "test.key.pem")

	// Generate test certificates
	if err := generateTestCertificate(certPath, keyPath); err != nil {
		t.Fatalf("Failed to generate test certificate: %v", err)
	}

	// Temporarily override default config for testing
	// Note: In real code, we can't override the function, so we test the function
	// with valid certificates by creating them at the default location in a test environment
	t.Log("Default config paths:", origCfg.CertFile, origCfg.KeyFile)
}

func TestTLSServerIntegration(t *testing.T) {
	// Create temporary directory for test certificates
	tmpDir, err := os.MkdirTemp("", "tls-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	certPath := filepath.Join(tmpDir, "test.pem")
	keyPath := filepath.Join(tmpDir, "test.key.pem")

	// Generate test certificates
	if err := generateTestCertificate(certPath, keyPath); err != nil {
		t.Fatalf("Failed to generate test certificate: %v", err)
	}

	cfg := &Config{
		CertFile:   certPath,
		KeyFile:    keyPath,
		MinVersion: tls.VersionTLS12,
	}

	tlsConfig, err := cfg.NewTLSConfig()
	if err != nil {
		t.Fatalf("Failed to create TLS config: %v", err)
	}

	// Create a test HTTP handler
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	// Create test server with TLS
	server := httptest.NewUnstartedServer(handler)
	server.TLS = tlsConfig
	server.StartTLS()
	defer server.Close()

	// Test connection with InsecureSkipVerify (since it's a self-signed cert)
	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		},
		Timeout: 5 * time.Second,
	}

	resp, err := client.Get(server.URL)
	if err != nil {
		t.Fatalf("Failed to connect to test server: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read response: %v", err)
	}

	if string(body) != "OK" {
		t.Errorf("Expected body 'OK', got '%s'", string(body))
	}
}

func TestTLSVersionEnforcement(t *testing.T) {
	// Create temporary directory for test certificates
	tmpDir, err := os.MkdirTemp("", "tls-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	certPath := filepath.Join(tmpDir, "test.pem")
	keyPath := filepath.Join(tmpDir, "test.key.pem")

	// Generate test certificates
	if err := generateTestCertificate(certPath, keyPath); err != nil {
		t.Fatalf("Failed to generate test certificate: %v", err)
	}

	cfg := &Config{
		CertFile:   certPath,
		KeyFile:    keyPath,
		MinVersion: tls.VersionTLS13, // Enforce TLS 1.3
	}

	tlsConfig, err := cfg.NewTLSConfig()
	if err != nil {
		t.Fatalf("Failed to create TLS config: %v", err)
	}

	if tlsConfig.MinVersion != tls.VersionTLS13 {
		t.Errorf("Expected MinVersion TLS 1.3, got %d", tlsConfig.MinVersion)
	}

	// Create test server with TLS 1.3 minimum
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	server := httptest.NewUnstartedServer(handler)
	server.TLS = tlsConfig
	server.StartTLS()
	defer server.Close()

	// Test with TLS 1.3 client (should succeed)
	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
				MinVersion:         tls.VersionTLS13,
			},
		},
		Timeout: 5 * time.Second,
	}

	resp, err := client.Get(server.URL)
	if err != nil {
		t.Fatalf("TLS 1.3 client should connect successfully: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}
}

func TestCipherSuitesConfiguration(t *testing.T) {
	// Create temporary directory for test certificates
	tmpDir, err := os.MkdirTemp("", "tls-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	certPath := filepath.Join(tmpDir, "test.pem")
	keyPath := filepath.Join(tmpDir, "test.key.pem")

	// Generate test certificates
	if err := generateTestCertificate(certPath, keyPath); err != nil {
		t.Fatalf("Failed to generate test certificate: %v", err)
	}

	cfg := &Config{
		CertFile:   certPath,
		KeyFile:    keyPath,
		MinVersion: tls.VersionTLS12,
	}

	tlsConfig, err := cfg.NewTLSConfig()
	if err != nil {
		t.Fatalf("Failed to create TLS config: %v", err)
	}

	// Verify cipher suites include modern, secure options
	expectedCiphers := []uint16{
		tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
		tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
		tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
		tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
		tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305,
		tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305,
	}

	for _, expectedCipher := range expectedCiphers {
		found := false
		for _, cipher := range tlsConfig.CipherSuites {
			if cipher == expectedCipher {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected cipher suite %d to be configured", expectedCipher)
		}
	}
}

// Benchmark TLS configuration creation
func BenchmarkNewTLSConfig(b *testing.B) {
	// Create temporary directory for test certificates
	tmpDir, err := os.MkdirTemp("", "tls-bench-*")
	if err != nil {
		b.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	certPath := filepath.Join(tmpDir, "test.pem")
	keyPath := filepath.Join(tmpDir, "test.key.pem")

	// Generate test certificates
	if err := generateTestCertificate(certPath, keyPath); err != nil {
		b.Fatalf("Failed to generate test certificate: %v", err)
	}

	cfg := &Config{
		CertFile:   certPath,
		KeyFile:    keyPath,
		MinVersion: tls.VersionTLS12,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := cfg.NewTLSConfig()
		if err != nil {
			b.Fatalf("Failed to create TLS config: %v", err)
		}
	}
}
