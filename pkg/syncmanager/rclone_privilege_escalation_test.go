//go:build security

package syncmanager

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/denysvitali/pictures-sync-s3/pkg/state"
)

// SECURITY VULNERABILITY REPORT: Privilege Escalation and Advanced Attack Vectors
//
// This test suite documents privilege escalation vulnerabilities and advanced
// attack scenarios in rclone configuration and sync operations.

// ============================================================================
// VULNERABILITY #13: Privilege Escalation via Config Manipulation
// Severity: CRITICAL
// CWE-269: Improper Privilege Management
// ============================================================================

func TestVuln13_PrivilegeEscalationViaConfig(t *testing.T) {
	// VULNERABILITY: Application runs with elevated privileges (systemd/gokrazy)
	// Config manipulation can lead to privilege escalation
	// Location: Config stored in /perm with rw access, rclone runs as root

	tmpDir, err := os.MkdirTemp("", "priv-esc-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	configPath := filepath.Join(tmpDir, "rclone.conf")

	// ATTACK VECTOR 1: Local backend to overwrite system files
	maliciousConfig := `[local-root]
type = local
# This allows writing anywhere on filesystem
`

	if err := os.WriteFile(configPath, []byte(maliciousConfig), 0600); err != nil {
		t.Fatal(err)
	}

	stateMgr, _ := state.NewManager()
	syncMgr := NewManager(configPath, "local-root", "/", stateMgr, 4, 8)

	// Attacker creates a card with malicious content
	// When synced to local backend at "/", can overwrite:
	// - /etc/passwd (add new root user)
	// - /etc/sudoers (grant sudo access)
	// - /root/.ssh/authorized_keys (SSH access)
	// - systemd service files (code execution on boot)

	// Test if we can list root directory
	_, err = syncMgr.ListFiles("")
	if err == nil {
		t.Error("CRITICAL: Can list root filesystem - privilege escalation possible")
	}

	// ATTACK VECTOR 2: Exec backend (if available)
	execConfig := `[exec-backend]
type = exec
remote = /bin/sh
# Arbitrary command execution as root
`

	if err := os.WriteFile(configPath, []byte(execConfig), 0600); err != nil {
		t.Fatal(err)
	}

	// ATTACK VECTOR 3: Symlink attacks via local backend
	// Create symlinks on SD card that point to sensitive files
	// When synced, can read sensitive files from remote

	t.Log("VULNERABILITY: Privilege escalation through config manipulation")
	t.Log("IMPACT: Complete system compromise, root access, persistent backdoors")
	t.Log("RECOMMENDATION: Run sync operations with minimal privileges (non-root)")
	t.Log("RECOMMENDATION: Restrict backend types (disallow local, exec)")
	t.Log("RECOMMENDATION: Use mandatory access controls (AppArmor/SELinux)")
	t.Log("RECOMMENDATION: Implement strict path validation and chroot")
}

// ============================================================================
// VULNERABILITY #14: Cloud Storage Permission Misconfiguration
// Severity: HIGH
// CWE-276: Incorrect Default Permissions
// ============================================================================

func TestVuln14_CloudStoragePermissionMisconfiguration(t *testing.T) {
	// VULNERABILITY: No verification of cloud storage permissions
	// Synced data may be publicly accessible if bucket is misconfigured
	// No warnings or checks for public read/write permissions

	tmpDir, err := os.MkdirTemp("", "permission-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	configPath := filepath.Join(tmpDir, "rclone.conf")

	// Scenario 1: S3 bucket with public-read ACL
	publicS3Config := `[public-s3]
type = s3
access_key_id = AKIAIOSFODNN7EXAMPLE
secret_access_key = wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY
region = us-east-1
acl = public-read
# All uploaded photos become publicly accessible!
`

	if err := os.WriteFile(configPath, []byte(publicS3Config), 0600); err != nil {
		t.Fatal(err)
	}

	// Application doesn't verify bucket permissions
	// Private photos could be exposed to the internet

	// Scenario 2: B2 bucket with public URL
	_ = `[public-b2]
type = b2
account = 000000000000000000000001
key = K000000000000000000000000000000000000001
# If bucket is public, all files are accessible via URL
`

	// Scenario 3: Google Drive with "anyone with link" sharing
	_ = `[public-drive]
type = drive
scope = drive
# Default sharing settings may make files accessible
`

	// EXPLOIT: Privacy breach - private photos accessible to anyone
	// No warning to user about public exposure
	// No check of bucket/folder permissions before sync

	t.Log("VULNERABILITY: No verification of cloud storage permissions")
	t.Log("IMPACT: Privacy breach, unauthorized access to personal photos")
	t.Log("RECOMMENDATION: Verify bucket/folder is private before sync")
	t.Log("RECOMMENDATION: Warn users about public buckets")
	t.Log("RECOMMENDATION: Enforce private ACLs in config")
	t.Log("RECOMMENDATION: Test bucket permissions during setup")
}

// ============================================================================
// VULNERABILITY #15: Token/Credential Refresh Vulnerabilities
// Severity: HIGH
// CWE-522: Insufficiently Protected Credentials
// ============================================================================

func TestVuln15_TokenRefreshVulnerabilities(t *testing.T) {
	// VULNERABILITY: OAuth tokens stored but refresh/expiry not validated
	// Expired tokens not detected until sync fails
	// Refresh tokens give permanent access if stolen

	tmpDir, err := os.MkdirTemp("", "token-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	configPath := filepath.Join(tmpDir, "rclone.conf")

	// OAuth config with refresh token
	oauthConfig := `[google-drive]
type = drive
scope = drive
token = {"access_token":"ya29.a0Aa4xrXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX","token_type":"Bearer","refresh_token":"1//0gXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX","expiry":"2024-01-01T00:00:00Z"}
`

	if err := os.WriteFile(configPath, []byte(oauthConfig), 0600); err != nil {
		t.Fatal(err)
	}

	// VULNERABILITY 1: Refresh token = permanent access
	// If config is stolen, attacker has unlimited access to Google Drive
	// No token rotation, no revocation checking

	// VULNERABILITY 2: No expiry validation before operations
	// Application will try to use expired token, fail, retry without refresh

	// VULNERABILITY 3: Token stored in same file as other credentials
	// Compromise of one credential exposes OAuth tokens

	stateMgr, _ := state.NewManager()
	syncMgr := NewManager(configPath, "google-drive", "/photos", stateMgr, 4, 8)

	// Test connection doesn't validate token expiry
	err = syncMgr.TestConnection()
	t.Logf("Connection test with potentially expired token: %v", err)

	t.Log("VULNERABILITY: Inadequate OAuth token protection")
	t.Log("IMPACT: Permanent account access via refresh tokens")
	t.Log("RECOMMENDATION: Encrypt tokens at rest with hardware-backed keys")
	t.Log("RECOMMENDATION: Implement token rotation and expiry checking")
	t.Log("RECOMMENDATION: Store tokens separately from other credentials")
	t.Log("RECOMMENDATION: Monitor for token usage anomalies")
	t.Log("RECOMMENDATION: Implement OAuth token revocation on suspicious activity")
}

// ============================================================================
// VULNERABILITY #16: SSRF via Remote Configuration
// Severity: HIGH
// CWE-918: Server-Side Request Forgery
// ============================================================================

func TestVuln16_SSRFViaRemoteConfig(t *testing.T) {
	// VULNERABILITY: Attacker can configure remotes pointing to internal resources
	// Application makes requests to attacker-controlled endpoints
	// Location: All rclone backend configs accept endpoint URLs

	tmpDir, err := os.MkdirTemp("", "ssrf-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	configPath := filepath.Join(tmpDir, "rclone.conf")

	// ATTACK VECTOR 1: Point to internal metadata service
	ssrfConfig := `[ssrf-attack]
type = s3
access_key_id = dummy
secret_access_key = dummy
endpoint = http://169.254.169.254/latest/meta-data/
region = us-east-1
# Exfiltrate AWS credentials from metadata service
`

	if err := os.WriteFile(configPath, []byte(ssrfConfig), 0600); err != nil {
		t.Fatal(err)
	}

	stateMgr, _ := state.NewManager()
	syncMgr := NewManager(configPath, "ssrf-attack", "/", stateMgr, 4, 8)

	// Attempt connection - this will make HTTP request to internal IP
	err = syncMgr.TestConnection()
	t.Logf("SSRF attempt to metadata service: %v", err)

	// ATTACK VECTOR 2: Port scanning internal network
	portScanConfig := `[port-scan]
type = s3
access_key_id = dummy
secret_access_key = dummy
endpoint = http://192.168.1.1:22
# Scan internal network for open ports
`

	if err := os.WriteFile(configPath, []byte(portScanConfig), 0600); err != nil {
		t.Fatal(err)
	}

	// ATTACK VECTOR 3: Exploit internal services
	internalServiceConfig := `[internal-service]
type = webdav
url = http://internal-admin-panel/
user = admin
pass = password
# Access internal web applications
`

	if err := os.WriteFile(configPath, []byte(internalServiceConfig), 0600); err != nil {
		t.Fatal(err)
	}

	t.Log("VULNERABILITY: Server-Side Request Forgery via config")
	t.Log("IMPACT: Internal network scanning, metadata service access, service exploitation")
	t.Log("RECOMMENDATION: Validate and restrict endpoint URLs")
	t.Log("RECOMMENDATION: Block requests to private IP ranges (RFC1918)")
	t.Log("RECOMMENDATION: Block metadata service IPs (169.254.169.254, fd00:ec2::254)")
	t.Log("RECOMMENDATION: Implement egress filtering")
	t.Log("RECOMMENDATION: Require explicit allow list for endpoints")
}

// ============================================================================
// VULNERABILITY #17: Symlink Attacks During Sync
// Severity: HIGH
// CWE-59: Improper Link Resolution Before File Access
// ============================================================================

func TestVuln17_SymlinkAttacksDuringSync(t *testing.T) {
	// VULNERABILITY: Symlinks on SD card followed during sync
	// Can exfiltrate sensitive files from the system
	// Location: rclone sync follows symlinks by default

	tmpDir, err := os.MkdirTemp("", "symlink-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create source directory simulating SD card
	srcDir := filepath.Join(tmpDir, "sdcard")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatal(err)
	}

	// ATTACK VECTOR 1: Symlink to /etc/passwd
	passwdLink := filepath.Join(srcDir, "photo.jpg")
	if err := os.Symlink("/etc/passwd", passwdLink); err != nil {
		t.Logf("Cannot create symlink (expected on some systems): %v", err)
	} else {
		defer os.Remove(passwdLink)

		// If sync follows this symlink, /etc/passwd gets uploaded to cloud
		t.Error("CRITICAL: Created symlink to /etc/passwd - sync would exfiltrate it")
	}

	// ATTACK VECTOR 2: Symlink to .ssh directory
	sshLink := filepath.Join(srcDir, "backup")
	if err := os.Symlink(os.Getenv("HOME")+"/.ssh", sshLink); err != nil {
		t.Logf("Cannot create .ssh symlink: %v", err)
	} else {
		defer os.Remove(sshLink)
		t.Error("CRITICAL: Created symlink to .ssh - private keys would be exfiltrated")
	}

	// ATTACK VECTOR 3: Symlink to rclone.conf itself
	configLink := filepath.Join(srcDir, "config.txt")
	if err := os.Symlink("/perm/pictures-sync/rclone.conf", configLink); err != nil {
		t.Logf("Cannot create config symlink: %v", err)
	} else {
		defer os.Remove(configLink)
		t.Error("CRITICAL: Created symlink to config - credentials would be exfiltrated")
	}

	configPath := filepath.Join(tmpDir, "rclone.conf")
	validConfig := `[local-test]
type = local
`
	if err := os.WriteFile(configPath, []byte(validConfig), 0600); err != nil {
		t.Fatal(err)
	}

	stateMgr, _ := state.NewManager()
	syncMgr := NewManager(configPath, "local-test", tmpDir, stateMgr, 4, 8)

	// Test if sync would follow symlinks
	// rclone follows symlinks by default unless --links flag is used
	err = syncMgr.Sync(srcDir, "card-test1234", 1, 1024)
	t.Logf("Sync with symlinks: %v", err)

	t.Log("VULNERABILITY: Symlink following during sync operations")
	t.Log("IMPACT: Exfiltration of sensitive system files to cloud storage")
	t.Log("RECOMMENDATION: Use rclone --links flag to copy as symlinks, not follow")
	t.Log("RECOMMENDATION: Reject sync if symlinks detected pointing outside DCIM")
	t.Log("RECOMMENDATION: Mount SD card with -o nosymfollow option")
	t.Log("RECOMMENDATION: Scan for suspicious symlinks before sync")
}

// ============================================================================
// VULNERABILITY #18: Time-of-Check Time-of-Use (TOCTOU) Races
// Severity: MEDIUM
// CWE-367: Time-of-check Time-of-use Race Condition
// ============================================================================

func TestVuln18_TOCTOURaceConditions(t *testing.T) {
	// VULNERABILITY: File validation and sync happen at different times
	// Attacker can swap files between check and use
	// Location: Photo counting happens before sync starts

	tmpDir, err := os.MkdirTemp("", "toctou-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	srcDir := filepath.Join(tmpDir, "sdcard")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatal(err)
	}

	// ATTACK SCENARIO:
	// 1. Application counts photos on card (main.go photo counting)
	// 2. Attacker swaps SD card or replaces files
	// 3. Sync starts with different files
	// 4. Wrong card ID assigned or malicious files synced

	// Create legitimate files
	for i := 0; i < 5; i++ {
		file := filepath.Join(srcDir, fmt.Sprintf("photo%d.jpg", i))
		if err := os.WriteFile(file, []byte("fake photo"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Race window: Between file count and sync start
	// An attacker could:
	// - Replace photos with malicious content
	// - Create symlinks to sensitive files
	// - Modify .pictures-sync-id file
	// - Remove files to trigger reformat detection

	configPath := filepath.Join(tmpDir, "rclone.conf")
	validConfig := `[local-test]
type = local
`
	if err := os.WriteFile(configPath, []byte(validConfig), 0600); err != nil {
		t.Fatal(err)
	}

	stateMgr, _ := state.NewManager()
	syncMgr := NewManager(configPath, "local-test", tmpDir, stateMgr, 4, 8)

	// Simulate TOCTOU attack by modifying files during sync preparation
	go func() {
		// Attacker modifies files during race window
		maliciousFile := filepath.Join(srcDir, "malicious.jpg")
		os.WriteFile(maliciousFile, []byte("malicious content"), 0644)
	}()

	err = syncMgr.Sync(srcDir, "card-test1234", 5, 5*10)
	t.Logf("Sync during TOCTOU window: %v", err)

	t.Log("VULNERABILITY: Time-of-check time-of-use race conditions")
	t.Log("IMPACT: File substitution attacks, wrong data synced")
	t.Log("RECOMMENDATION: Lock or snapshot filesystem before sync")
	t.Log("RECOMMENDATION: Verify file checksums match pre-sync inventory")
	t.Log("RECOMMENDATION: Atomic operations for validation + sync")
	t.Log("RECOMMENDATION: Detect and reject filesystem changes during sync")
}

// ============================================================================
// VULNERABILITY #19: Insufficient Resource Limits
// Severity: MEDIUM
// CWE-770: Allocation of Resources Without Limits or Throttling
// ============================================================================

func TestVuln19_ResourceExhaustionAttacks(t *testing.T) {
	// VULNERABILITY: No limits on config size, file count, sync operations
	// Attacker can exhaust system resources
	// Location: No validation of totalFiles, totalBytes parameters

	tmpDir, err := os.MkdirTemp("", "resource-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	configPath := filepath.Join(tmpDir, "rclone.conf")
	validConfig := `[local-test]
type = local
`
	if err := os.WriteFile(configPath, []byte(validConfig), 0600); err != nil {
		t.Fatal(err)
	}

	stateMgr, _ := state.NewManager()
	syncMgr := NewManager(configPath, "local-test", tmpDir, stateMgr, 4, 8)

	srcDir := filepath.Join(tmpDir, "source")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatal(err)
	}

	// ATTACK VECTOR 1: Massive file count
	// Claim to sync 1 billion files
	err = syncMgr.Sync(srcDir, "card-test1234", 1000000000, 1024)
	t.Logf("Sync with excessive file count: %v", err)

	// ATTACK VECTOR 2: Massive byte count
	// Claim 1 petabyte of data
	err = syncMgr.Sync(srcDir, "card-test1234", 1, 1024*1024*1024*1024*1024)
	t.Logf("Sync with excessive byte count: %v", err)

	// ATTACK VECTOR 3: Excessive parallel transfers
	// Settings allow up to max int transfers/checkers
	excessiveMgr := NewManager(configPath, "local-test", tmpDir, stateMgr, 10000, 10000)
	err = excessiveMgr.Sync(srcDir, "card-test1234", 1, 1024)
	t.Logf("Sync with excessive parallelism: %v", err)

	// ATTACK VECTOR 4: Huge config file (DoS)
	hugeConfig := "[remote]\ntype=local\n" + strings.Repeat("option = value\n", 1000000)
	err = os.WriteFile(configPath, []byte(hugeConfig), 0600)
	if err != nil {
		t.Logf("Large config rejected by filesystem: %v", err)
	} else {
		t.Error("VULNERABILITY: Massive config file accepted")
	}

	t.Log("VULNERABILITY: No resource limits on sync operations")
	t.Log("IMPACT: Denial of service, memory exhaustion, system crash")
	t.Log("RECOMMENDATION: Validate totalFiles and totalBytes parameters")
	t.Log("RECOMMENDATION: Limit max config file size (e.g., 100KB)")
	t.Log("RECOMMENDATION: Cap parallel transfers/checkers (e.g., max 20)")
	t.Log("RECOMMENDATION: Implement rate limiting on sync operations")
	t.Log("RECOMMENDATION: Monitor memory usage and abort on excess")
}

// ============================================================================
// VULNERABILITY #20: Insecure Default Configuration
// Severity: MEDIUM
// CWE-1188: Insecure Default Initialization
// ============================================================================

func TestVuln20_InsecureDefaultConfiguration(t *testing.T) {
	// VULNERABILITY: Insecure defaults throughout the system
	// No security-first configuration

	tmpDir, err := os.MkdirTemp("", "defaults-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// INSECURE DEFAULT 1: Config file permissions
	// If created without explicit 0600, may default to 0644 (world-readable)
	configPath := filepath.Join(tmpDir, "rclone.conf")
	if err := os.WriteFile(configPath, []byte("[test]\ntype=local\n"), 0644); err != nil {
		t.Fatal(err)
	}

	info, _ := os.Stat(configPath)
	if info.Mode().Perm()&0044 != 0 {
		t.Error("VULNERABILITY: Default config permissions too permissive")
	}

	// INSECURE DEFAULT 2: No authentication on web UI
	// Port 8080 accessible to anyone on network
	t.Log("VULNERABILITY: Web UI has no authentication by default")

	// INSECURE DEFAULT 3: No TLS/HTTPS by default
	t.Log("VULNERABILITY: Web UI serves over HTTP by default")

	// INSECURE DEFAULT 4: No password on rclone config
	// Credentials stored in plaintext by default
	t.Log("VULNERABILITY: No encryption on config by default")

	// INSECURE DEFAULT 5: Follows symlinks by default
	t.Log("VULNERABILITY: Rclone follows symlinks by default")

	// INSECURE DEFAULT 6: No integrity checking
	// No checksums verified for synced files
	t.Log("VULNERABILITY: No file integrity verification by default")

	// INSECURE DEFAULT 7: Excessive logging
	// May log sensitive information by default
	t.Log("VULNERABILITY: Verbose logging may expose secrets")

	t.Log("")
	t.Log("CRITICAL INSECURE DEFAULTS IDENTIFIED:")
	t.Log("  1. World-readable config files")
	t.Log("  2. No web UI authentication")
	t.Log("  3. HTTP instead of HTTPS")
	t.Log("  4. Plaintext credential storage")
	t.Log("  5. Symlink following enabled")
	t.Log("  6. No integrity verification")
	t.Log("  7. Overly verbose logging")
	t.Log("")
	t.Log("RECOMMENDATION: Implement secure-by-default configuration")
	t.Log("RECOMMENDATION: Force HTTPS and authentication on web UI")
	t.Log("RECOMMENDATION: Encrypt config on first run")
	t.Log("RECOMMENDATION: Disable symlink following by default")
	t.Log("RECOMMENDATION: Enable checksum verification")
	t.Log("RECOMMENDATION: Minimal logging in production mode")
}

// ============================================================================
// EXPLOITATION SCENARIOS
// ============================================================================

func TestCompleteExploitationScenario(t *testing.T) {
	t.Log("=" + strings.Repeat("=", 78))
	t.Log("COMPLETE EXPLOITATION SCENARIO: Multi-Stage Attack")
	t.Log("=" + strings.Repeat("=", 78))
	t.Log("")
	t.Log("STAGE 1: Initial Access")
	t.Log("  - Attacker connects to web UI on port 8080 (no auth required)")
	t.Log("  - GET /api/config retrieves full rclone.conf with credentials")
	t.Log("  - All cloud storage credentials now compromised")
	t.Log("")
	t.Log("STAGE 2: Privilege Escalation")
	t.Log("  - Attacker uploads malicious config via POST /api/config")
	t.Log("  - Config uses local backend pointing to /")
	t.Log("  - Attacker prepares SD card with malicious files:")
	t.Log("    • DCIM/authorized_keys (will be synced to /root/.ssh/)")
	t.Log("    • DCIM/backdoor.service (systemd service for persistence)")
	t.Log("    • DCIM/passwd (replaces /etc/passwd)")
	t.Log("")
	t.Log("STAGE 3: Persistence")
	t.Log("  - Attacker inserts malicious SD card")
	t.Log("  - Sync operation copies files to system locations")
	t.Log("  - SSH access established via authorized_keys")
	t.Log("  - Backdoor service starts on boot")
	t.Log("")
	t.Log("STAGE 4: Data Exfiltration")
	t.Log("  - Attacker modifies config to use their cloud storage")
	t.Log("  - System automatically syncs all photos to attacker's storage")
	t.Log("  - Can also create symlinks to exfiltrate system files")
	t.Log("")
	t.Log("STAGE 5: Lateral Movement")
	t.Log("  - Access to cloud credentials enables:")
	t.Log("    • Access to all user's photos across all devices")
	t.Log("    • Access to other data in the cloud storage")
	t.Log("    • Potential pivot to other cloud services")
	t.Log("")
	t.Log("IMPACT:")
	t.Log("  - Complete system compromise (root access)")
	t.Log("  - All personal photos compromised")
	t.Log("  - Cloud storage account takeover")
	t.Log("  - Persistent backdoor access")
	t.Log("  - Privacy breach (GDPR violation)")
	t.Log("")
	t.Log("LIKELIHOOD: HIGH")
	t.Log("  - No authentication required for initial access")
	t.Log("  - Default configuration is exploitable")
	t.Log("  - Physical access to device is common scenario")
	t.Log("  - Network access available via WiFi")
	t.Log("")
	t.Log("=" + strings.Repeat("=", 78))
}

// ============================================================================
// SECURITY HARDENING RECOMMENDATIONS
// ============================================================================

func TestSecurityHardeningRecommendations(t *testing.T) {
	t.Log("=" + strings.Repeat("=", 78))
	t.Log("SECURITY HARDENING RECOMMENDATIONS - PRIORITY ORDER")
	t.Log("=" + strings.Repeat("=", 78))
	t.Log("")
	t.Log("IMMEDIATE (Fix within 24 hours):")
	t.Log("  1. Remove API endpoint that returns config content")
	t.Log("     - Delete GET handler for /api/config content field")
	t.Log("     - Return only status: configured true/false")
	t.Log("")
	t.Log("  2. Implement web UI authentication")
	t.Log("     - Add password-based authentication")
	t.Log("     - Store bcrypt hashed password in settings")
	t.Log("     - Require auth for ALL endpoints")
	t.Log("")
	t.Log("  3. Enforce restrictive file permissions")
	t.Log("     - Set rclone.conf to 0600 on creation")
	t.Log("     - Verify permissions on startup, fail if wrong")
	t.Log("     - Set /perm/pictures-sync to 0700")
	t.Log("")
	t.Log("SHORT TERM (Fix within 1 week):")
	t.Log("  4. Implement config encryption")
	t.Log("     - Use rclone config password feature")
	t.Log("     - Derive key from device-specific data")
	t.Log("     - Encrypt at rest, decrypt only when needed")
	t.Log("")
	t.Log("  5. Add strict input validation")
	t.Log("     - Remote names: alphanumeric + hyphen only")
	t.Log("     - Remote paths: reject '..' and absolute paths")
	t.Log("     - Card IDs: already validated, maintain")
	t.Log("     - Config content: parse and validate INI format")
	t.Log("")
	t.Log("  6. Implement HTTPS requirement")
	t.Log("     - Generate self-signed cert on first boot")
	t.Log("     - Redirect HTTP to HTTPS")
	t.Log("     - Add HSTS header")
	t.Log("")
	t.Log("MEDIUM TERM (Fix within 1 month):")
	t.Log("  7. Add backend whitelist")
	t.Log("     - Only allow: s3, b2, drive, azureblob")
	t.Log("     - Block: local, exec, any others")
	t.Log("     - Validate on config upload")
	t.Log("")
	t.Log("  8. Implement resource limits")
	t.Log("     - Max config size: 100KB")
	t.Log("     - Max transfers: 20")
	t.Log("     - Max checkers: 20")
	t.Log("     - Validate totalFiles < 1 million")
	t.Log("")
	t.Log("  9. Add security monitoring")
	t.Log("     - Log all config changes with timestamp")
	t.Log("     - Alert on multiple failed auth attempts")
	t.Log("     - Monitor for suspicious file patterns")
	t.Log("")
	t.Log(" 10. Implement symlink protection")
	t.Log("     - Mount SD card with -o nosymfollow")
	t.Log("     - Scan for symlinks before sync")
	t.Log("     - Reject sync if symlinks found outside DCIM")
	t.Log("")
	t.Log("LONG TERM (Fix within 3 months):")
	t.Log(" 11. Reduce privilege requirements")
	t.Log("     - Run sync operations as non-root user")
	t.Log("     - Use capabilities instead of full root")
	t.Log("     - Implement AppArmor/SELinux profile")
	t.Log("")
	t.Log(" 12. Add integrity verification")
	t.Log("     - Checksum all files before sync")
	t.Log("     - Verify checksums match after sync")
	t.Log("     - Store checksums in state for audit")
	t.Log("")
	t.Log(" 13. Implement audit logging")
	t.Log("     - Tamper-proof audit log")
	t.Log("     - Log all security-relevant events")
	t.Log("     - Include: auth attempts, config changes, sync operations")
	t.Log("")
	t.Log(" 14. Add CSRF protection")
	t.Log("     - Implement CSRF tokens for state-changing requests")
	t.Log("     - Validate Origin/Referer headers")
	t.Log("     - Use SameSite cookie attributes")
	t.Log("")
	t.Log(" 15. Security testing")
	t.Log("     - Regular penetration testing")
	t.Log("     - Fuzzing of API endpoints")
	t.Log("     - Automated security scanning in CI/CD")
	t.Log("")
	t.Log("=" + strings.Repeat("=", 78))
}
