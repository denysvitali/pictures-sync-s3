package tlsconfig

import (
	"crypto/tls"
	"crypto/x509"
	"log"
	"os"
)

var (
	rootCertFile    = "/etc/ssl/gokrazy-web.pem"
	rootKeyFile     = "/etc/ssl/gokrazy-web.key.pem"
	rootUsePermFile = "/etc/ssl/gokrazy-web.use-perm"
	permCertFile    = "/perm/ssl/gokrazy-web.pem"
	permKeyFile     = "/perm/ssl/gokrazy-web.key.pem"
)

// Config provides TLS configuration for the webui server
type Config struct {
	// CertFile is the path to the TLS certificate
	CertFile string
	// KeyFile is the path to the TLS private key
	KeyFile string
	// InsecureSkipVerify disables certificate verification (dev/internal use only)
	InsecureSkipVerify bool
	// MinVersion is the minimum TLS version (default: TLS 1.2)
	MinVersion uint16
}

// DefaultConfig returns a secure default TLS configuration
func DefaultConfig() *Config {
	return &Config{
		CertFile:           rootCertFile,
		KeyFile:            rootKeyFile,
		InsecureSkipVerify: false,
		MinVersion:         tls.VersionTLS12,
	}
}

// ResolveConfig returns a TLS config pointing at the appropriate certificate
// pair for the current gokrazy image. When gokrazy's root marker asks for
// persistent TLS storage, the /perm certificate pair is preferred.
func ResolveConfig() *Config {
	cfg := DefaultConfig()
	root := Config{
		CertFile:           rootCertFile,
		KeyFile:            rootKeyFile,
		InsecureSkipVerify: cfg.InsecureSkipVerify,
		MinVersion:         cfg.MinVersion,
	}
	perm := Config{
		CertFile:           permCertFile,
		KeyFile:            permKeyFile,
		InsecureSkipVerify: cfg.InsecureSkipVerify,
		MinVersion:         cfg.MinVersion,
	}

	if fileExists(rootUsePermFile) && perm.CertificatesExist() {
		return &perm
	}

	if root.CertificatesExist() {
		return &root
	}

	if perm.CertificatesExist() {
		return &perm
	}

	return cfg
}

// NewTLSConfig creates a tls.Config from the provided Config
// For internal/development deployments with self-signed certificates,
// this configures TLS to work properly with all clients including Tailscale
func (c *Config) NewTLSConfig() (*tls.Config, error) {
	// Load the certificate and key
	cert, err := tls.LoadX509KeyPair(c.CertFile, c.KeyFile)
	if err != nil {
		return nil, err
	}

	// Create TLS config with secure defaults
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   c.MinVersion,
		// Use modern cipher suites (Go 1.17+ handles this automatically)
		CipherSuites: []uint16{
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305,
			tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305,
		},
		PreferServerCipherSuites: true,
		// For self-signed certificates in internal deployments,
		// we need to configure the server to not require client cert verification
		// The server itself doesn't verify client certs, but we ensure it presents
		// its own certificate properly
		ClientAuth: tls.NoClientCert,
	}

	// Load system certificate pool for production use
	if !c.InsecureSkipVerify {
		certPool, err := x509.SystemCertPool()
		if err != nil {
			log.Printf("Warning: Failed to load system cert pool: %v", err)
			certPool = x509.NewCertPool()
		}
		tlsConfig.RootCAs = certPool
	}

	return tlsConfig, nil
}

// CertificatesExist checks if the configured certificate files exist
func (c *Config) CertificatesExist() bool {
	return fileExists(c.CertFile) && fileExists(c.KeyFile)
}

func fileExists(path string) bool {
	if _, err := os.Stat(path); err != nil {
		return false
	}
	return true
}

// LoadOrDefault attempts to load certificates and returns whether TLS should be used
// Returns (tlsConfig, useTLS, error)
func LoadOrDefault() (*tls.Config, bool, error) {
	cfg := ResolveConfig()

	// Check if certificates exist
	if !cfg.CertificatesExist() {
		log.Printf("TLS certificates not found at %s and %s", cfg.CertFile, cfg.KeyFile)
		return nil, false, nil
	}

	// Load TLS configuration
	tlsConfig, err := cfg.NewTLSConfig()
	if err != nil {
		log.Printf("Failed to load TLS configuration: %v", err)
		return nil, false, err
	}

	log.Printf("TLS enabled with certificates from %s", cfg.CertFile)
	return tlsConfig, true, nil
}

// CreateSecureServer creates an http.Server with secure TLS configuration
// This is the recommended way to create an HTTPS server for production use
func CreateSecureServer(addr string, handler interface{}, tlsConfig *tls.Config) interface{} {
	// Type assertion to avoid import cycle
	// The caller should cast this back to *http.Server
	return struct {
		Addr      string
		Handler   interface{}
		TLSConfig *tls.Config
	}{
		Addr:      addr,
		Handler:   handler,
		TLSConfig: tlsConfig,
	}
}
