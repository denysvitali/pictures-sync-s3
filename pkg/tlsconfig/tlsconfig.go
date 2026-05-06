package tlsconfig

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"log"
	"math/big"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	certificateValidity    = 10 * 365 * 24 * time.Hour
	certificateBackdate    = 5 * time.Minute
	certificateRenewWithin = 30 * 24 * time.Hour
)

var (
	rootCertFile                      = "/etc/ssl/gokrazy-web.pem"
	rootKeyFile                       = "/etc/ssl/gokrazy-web.key.pem"
	rootUsePermFile                   = "/etc/ssl/gokrazy-web.use-perm"
	permCertFile                      = "/perm/ssl/gokrazy-web.pem"
	permKeyFile                       = "/perm/ssl/gokrazy-web.key.pem"
	earliestReasonableCertificateTime = time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC)
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

// CertificateInfo describes the currently configured persistent certificate.
type CertificateInfo struct {
	CertFile          string    `json:"cert_file"`
	KeyFile           string    `json:"key_file"`
	Exists            bool      `json:"exists"`
	ValidNow          bool      `json:"valid_now"`
	NeedsRegeneration bool      `json:"needs_regeneration"`
	NotBefore         time.Time `json:"not_before,omitempty"`
	NotAfter          time.Time `json:"not_after,omitempty"`
	CommonName        string    `json:"common_name,omitempty"`
	DNSNames          []string  `json:"dns_names,omitempty"`
	IPAddresses       []string  `json:"ip_addresses,omitempty"`
	FingerprintSHA256 string    `json:"fingerprint_sha256,omitempty"`
	Error             string    `json:"error,omitempty"`
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

// PersistentConfig returns the certificate paths on the persistent partition.
func PersistentConfig() *Config {
	cfg := DefaultConfig()
	return &Config{
		CertFile:           permCertFile,
		KeyFile:            permKeyFile,
		InsecureSkipVerify: cfg.InsecureSkipVerify,
		MinVersion:         cfg.MinVersion,
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

// CurrentTimeCanIssueCertificate reports whether now is sane enough to write a
// long-lived certificate. This prevents persisting certificates generated at
// gokrazy's initial placeholder clock value.
func CurrentTimeCanIssueCertificate(now time.Time) bool {
	return !now.IsZero() && !now.Before(earliestReasonableCertificateTime)
}

// PersistentCertificateInfo inspects the certificate pair stored under /perm.
func PersistentCertificateInfo(now time.Time) (*CertificateInfo, error) {
	return inspectCertificate(PersistentConfig(), now)
}

// EnsurePersistentSelfSignedCertificate creates or renews the /perm certificate
// pair when it is missing, invalid, or close to expiry.
func EnsurePersistentSelfSignedCertificate(hosts []string) (*CertificateInfo, bool, error) {
	now := time.Now()
	info, err := PersistentCertificateInfo(now)
	if err == nil && info.Exists && !info.NeedsRegeneration {
		return info, false, nil
	}

	generated, genErr := GeneratePersistentSelfSignedCertificate(hosts)
	if genErr != nil {
		return info, false, genErr
	}
	return generated, true, nil
}

// GeneratePersistentSelfSignedCertificate writes a new self-signed certificate
// pair to /perm/ssl.
func GeneratePersistentSelfSignedCertificate(hosts []string) (*CertificateInfo, error) {
	return generateSelfSignedCertificate(PersistentConfig(), hosts, time.Now())
}

func generateSelfSignedCertificate(cfg *Config, hosts []string, now time.Time) (*CertificateInfo, error) {
	if !CurrentTimeCanIssueCertificate(now) {
		return nil, fmt.Errorf("system time %s is too early to issue a persistent certificate", now.UTC().Format(time.RFC3339))
	}

	dnsNames, ipAddresses := certificateNames(hosts)
	commonName := "gokrazy-web"
	if len(dnsNames) > 0 {
		commonName = dnsNames[0]
	}

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("generate private key: %w", err)
	}

	serialLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialLimit)
	if err != nil {
		return nil, fmt.Errorf("generate serial number: %w", err)
	}

	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"Photo Backup Station"},
			CommonName:   commonName,
		},
		NotBefore:             now.Add(-certificateBackdate).UTC(),
		NotAfter:              now.Add(certificateValidity).UTC(),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              dnsNames,
		IPAddresses:           ipAddresses,
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return nil, fmt.Errorf("create certificate: %w", err)
	}

	privateKeyBytes := x509.MarshalPKCS1PrivateKey(privateKey)

	if err := os.MkdirAll(filepath.Dir(cfg.CertFile), 0700); err != nil {
		return nil, fmt.Errorf("create certificate directory: %w", err)
	}
	if err := writePEMFileAtomic(cfg.KeyFile, 0600, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: privateKeyBytes}); err != nil {
		return nil, fmt.Errorf("write private key: %w", err)
	}
	if err := writePEMFileAtomic(cfg.CertFile, 0644, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes}); err != nil {
		return nil, fmt.Errorf("write certificate: %w", err)
	}

	return inspectCertificate(cfg, now)
}

func inspectCertificate(cfg *Config, now time.Time) (*CertificateInfo, error) {
	info := &CertificateInfo{
		CertFile: cfg.CertFile,
		KeyFile:  cfg.KeyFile,
		Exists:   cfg.CertificatesExist(),
	}
	if !info.Exists {
		info.NeedsRegeneration = true
		return info, nil
	}

	certPEM, err := os.ReadFile(cfg.CertFile)
	if err != nil {
		info.Error = err.Error()
		info.NeedsRegeneration = true
		return info, err
	}

	block, _ := pem.Decode(certPEM)
	if block == nil || block.Type != "CERTIFICATE" {
		err := fmt.Errorf("certificate file does not contain a PEM certificate")
		info.Error = err.Error()
		info.NeedsRegeneration = true
		return info, err
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		info.Error = err.Error()
		info.NeedsRegeneration = true
		return info, err
	}

	fingerprint := x509SHA256Fingerprint(cert.Raw)
	ipAddresses := make([]string, 0, len(cert.IPAddresses))
	for _, ip := range cert.IPAddresses {
		ipAddresses = append(ipAddresses, ip.String())
	}
	sort.Strings(ipAddresses)

	info.NotBefore = cert.NotBefore
	info.NotAfter = cert.NotAfter
	info.CommonName = cert.Subject.CommonName
	info.DNSNames = append([]string(nil), cert.DNSNames...)
	info.IPAddresses = ipAddresses
	info.FingerprintSHA256 = fingerprint
	info.ValidNow = !now.Before(cert.NotBefore) && !now.After(cert.NotAfter)
	info.NeedsRegeneration = !info.ValidNow || cert.NotAfter.Sub(now) <= certificateRenewWithin

	if _, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile); err != nil {
		info.Error = err.Error()
		info.ValidNow = false
		info.NeedsRegeneration = true
		return info, err
	}

	return info, nil
}

func writePEMFileAtomic(path string, mode os.FileMode, block *pem.Block) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}

	tmpFile, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmpFile.Name()
	renamed := false
	defer func() {
		if !renamed {
			_ = os.Remove(tmpName)
		}
	}()

	if err := tmpFile.Chmod(mode); err != nil {
		_ = tmpFile.Close()
		return err
	}
	if err := pem.Encode(tmpFile, block); err != nil {
		_ = tmpFile.Close()
		return err
	}
	if err := tmpFile.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	renamed = true
	return nil
}

func certificateNames(extraHosts []string) ([]string, []net.IP) {
	dnsSet := map[string]struct{}{}
	ipSet := map[string]net.IP{}

	addHost := func(host string) {
		host = normalizeCertificateHost(host)
		if host == "" {
			return
		}
		if ip := net.ParseIP(host); ip != nil {
			ipSet[ip.String()] = ip
			return
		}
		dnsSet[host] = struct{}{}
	}

	addHost("localhost")
	addHost("photo-backup")
	addHost("photo-backup.local")
	if hostname, err := os.Hostname(); err == nil {
		addHost(hostname)
		if hostname != "" && !strings.HasSuffix(hostname, ".local") {
			addHost(hostname + ".local")
		}
	}
	for _, host := range extraHosts {
		addHost(host)
	}

	for _, ip := range interfaceIPAddresses() {
		ipSet[ip.String()] = ip
	}

	dnsNames := make([]string, 0, len(dnsSet))
	for name := range dnsSet {
		dnsNames = append(dnsNames, name)
	}
	sort.Strings(dnsNames)

	ipKeys := make([]string, 0, len(ipSet))
	for key := range ipSet {
		ipKeys = append(ipKeys, key)
	}
	sort.Strings(ipKeys)

	ipAddresses := make([]net.IP, 0, len(ipKeys))
	for _, key := range ipKeys {
		ipAddresses = append(ipAddresses, ipSet[key])
	}

	return dnsNames, ipAddresses
}

func normalizeCertificateHost(host string) string {
	host = strings.TrimSpace(strings.ToLower(host))
	if host == "" {
		return ""
	}

	if strings.Contains(host, "://") {
		if parsed, err := url.Parse(host); err == nil {
			host = parsed.Host
		}
	}

	if parsedHost, _, err := net.SplitHostPort(host); err == nil {
		host = parsedHost
	}
	host = strings.Trim(host, "[]")
	host = strings.TrimSuffix(host, ".")
	if host == "" || len(host) > 253 {
		return ""
	}
	if strings.ContainsAny(host, "/\\ \t\r\n") {
		return ""
	}
	return host
}

func interfaceIPAddresses() []net.IP {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}

	ips := make([]net.IP, 0, len(addrs)+2)
	ips = append(ips, net.ParseIP("127.0.0.1"), net.ParseIP("::1"))
	for _, addr := range addrs {
		var ip net.IP
		switch value := addr.(type) {
		case *net.IPNet:
			ip = value.IP
		case *net.IPAddr:
			ip = value.IP
		}
		if ip == nil || ip.IsUnspecified() || ip.IsMulticast() {
			continue
		}
		ips = append(ips, ip)
	}
	return ips
}

func x509SHA256Fingerprint(raw []byte) string {
	digest := sha256Sum(raw)
	encoded := strings.ToUpper(hex.EncodeToString(digest))
	parts := make([]string, 0, len(encoded)/2)
	for i := 0; i < len(encoded); i += 2 {
		parts = append(parts, encoded[i:i+2])
	}
	return strings.Join(parts, ":")
}

func sha256Sum(raw []byte) []byte {
	digest := sha256.Sum256(raw)
	return digest[:]
}

// LoadOrDefault attempts to load certificates and returns whether TLS should be used
// Returns (tlsConfig, useTLS, error)
func LoadOrDefault() (*tls.Config, bool, error) {
	cfg := ResolveConfig()
	now := time.Now()

	if !CurrentTimeCanIssueCertificate(now) {
		log.Printf("System time %s is too early for reliable TLS certificates", now.UTC().Format(time.RFC3339))
		return nil, false, nil
	}

	// Check if certificates exist
	if !cfg.CertificatesExist() {
		log.Printf("TLS certificates not found at %s and %s", cfg.CertFile, cfg.KeyFile)
		return nil, false, nil
	}

	info, err := inspectCertificate(cfg, now)
	if err != nil || !info.ValidNow {
		if err != nil {
			log.Printf("TLS certificate at %s is not usable: %v", cfg.CertFile, err)
		} else {
			log.Printf("TLS certificate at %s is not valid for current time %s", cfg.CertFile, now.UTC().Format(time.RFC3339))
		}
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
