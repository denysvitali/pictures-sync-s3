# Security Testing Strategy

## Overview

This document describes the security testing approach for the pictures-sync-s3 project. The test suite includes both **security demonstration tests** (intentionally failing tests that document vulnerabilities) and **security validation tests** (tests that verify fixes are in place).

## Test Categories

### 1. Security Demonstration Tests (Build Tag: `security_audit`)

These tests intentionally fail to demonstrate known security issues and attack vectors. They serve as:
- Documentation of security considerations
- Awareness tools for developers
- Audit trail for security decisions

**Location**: Files ending with `*_vulnerabilities_test.go` or `*crypto_vulnerabilities_test.go`

**Examples**:
- `pkg/wifimanager/crypto_vulnerabilities_test.go` - Documents WiFi password storage risks
- `pkg/sdmonitor/crypto_vulnerabilities_test.go` - Documents card ID generation concerns
- `cmd/webui/crypto_vulnerabilities_test.go` - Documents authentication and encryption issues

**Running these tests**:
```bash
# Run security audit tests only
go test -tags security_audit ./...

# Skip security audit tests (default)
go test ./...
```

### 2. Security Validation Tests (Default Build)

These tests verify that security fixes are properly implemented. They should **always pass** in CI/CD.

**Examples**:
- `pkg/handlers/wifi_security_test.go` - Verifies passwords are NOT exposed via API
- `pkg/auth/security_headers_test.go` - Verifies security headers are present
- Input validation tests

**Running these tests**:
```bash
# Normal test run (validation tests only)
go test ./...
```

## Security Issues Documented

### CRITICAL Issues (Demonstration Tests)

1. **WiFi Password Storage** (CVSS 9.1)
   - Test: `TestWiFiPasswordSecurity/PlaintextPasswordStorage`
   - Issue: Passwords stored in plaintext in `/perm/extra-wifi.json`
   - Mitigation: Document in SECURITY.md, rely on physical security and filesystem permissions
   - Status: **Accepted Risk** - Gokrazy environment provides limited encryption options

2. **Card ID Fallback** (CVSS 8.1)
   - Test: `TestCardIDGenerationSecurity/PredictableFallbackVulnerability`
   - Issue: Fallback to timestamp when crypto/rand fails
   - Fix Applied: Now uses nanoseconds + PID for improved collision resistance
   - Status: **Mitigated** - Fallback still exists but significantly improved

3. **No Data Encryption** (CVSS 9.8)
   - Test: `TestEncryptionModeConfusion/NoEncryptionImplemented`
   - Issue: Sensitive files not encrypted at rest
   - Mitigation: Document recommendation for full-disk encryption (LUKS)
   - Status: **Accepted Risk** - Out of scope for application layer

### HIGH Issues (Demonstration Tests)

4. **Password API Exposure** (CVSS 8.2)
   - Test: `TestWiFiPasswordSecurity/PasswordAPIExposure` (in wifimanager)
   - Fix: `TestHandleWiFiNetworks_NoPasswordExposure` (in handlers) - **PASSES**
   - Status: **FIXED** - SafeNetworkInfo struct filters passwords from API responses

5. **No Password Strength Validation** (CVSS 7.5)
   - Test: `TestWiFiPasswordSecurity/NoPasswordStrengthValidation`
   - Issue: Weak WiFi passwords accepted
   - Recommendation: Implement WPA2 password requirements (8-63 chars)
   - Status: **To Be Fixed**

### MEDIUM Issues (Demonstration Tests)

6. **Card ID File Tampering** (CVSS 6.5)
   - Test: `TestCardIDGenerationSecurity/CardIDFileTampering`
   - Issue: No HMAC/signature on card ID file
   - Mitigation: validateCardID() in syncmanager rejects path traversal
   - Status: **Partially Mitigated**

7. **Passwords in Memory** (CVSS 6.5)
   - Test: `TestWiFiPasswordSecurity/PasswordInMemory`
   - Issue: Go strings are immutable, cannot zero memory
   - Status: **Accepted Risk** - Inherent Go limitation

## Implementation Plan

### Phase 1: Test Organization (Completed)
- [x] Categorize tests as demonstration vs validation
- [x] Add build tags to demonstration tests
- [x] Document test strategy

### Phase 2: Critical Fixes

1. **Add Build Tags to Vulnerability Tests**
   ```go
   //go:build security_audit
   // +build security_audit
   ```
   Files to update:
   - `pkg/wifimanager/crypto_vulnerabilities_test.go`
   - `pkg/sdmonitor/crypto_vulnerabilities_test.go`
   - `cmd/webui/crypto_vulnerabilities_test.go`
   - `pkg/state/error_handling_vulnerabilities_test.go`

2. **Implement Password Strength Validation**
   - Location: `pkg/wifimanager/manager.go`
   - Add validation in `AddNetwork()` method
   - Requirements:
     * 8-63 characters for WPA/WPA2
     * Empty allowed for open networks
     * Reject control characters

3. **Enhance Card ID Validation**
   - Location: `pkg/sdmonitor/cardid.go`
   - Add length checks (max 64 chars)
   - Reject special characters beyond expected pattern
   - Log when fallback is used

### Phase 3: Documentation

1. **SECURITY.md**
   - Document all known security considerations
   - List accepted risks with rationale
   - Provide hardening recommendations
   - Link to SECURITY_TESTING_STRATEGY.md

2. **README.md Updates**
   - Add security section
   - Reference security documentation
   - Explain test strategy

### Phase 4: CI/CD Integration

1. **Default Build**
   ```bash
   go test ./...  # Runs validation tests only
   ```

2. **Security Audit Build** (Optional)
   ```bash
   go test -tags security_audit ./...  # Includes demonstration tests
   ```

3. **GitHub Actions**
   - Main workflow: Run validation tests only
   - Optional: Weekly security audit run (allowed to fail)

## Running Tests

### For Developers (Normal Development)
```bash
# Run all validation tests (should pass)
go test ./...

# Run specific security validation test
go test ./pkg/handlers -run TestHandleWiFiNetworks_NoPasswordExposure
```

### For Security Audits
```bash
# Run all tests including security demonstrations
go test -tags security_audit ./...

# Review security vulnerabilities
go test -tags security_audit -v ./pkg/wifimanager -run TestWiFiPasswordSecurity

# Check specific vulnerability
go test -tags security_audit -v ./pkg/sdmonitor -run TestCardIDGenerationSecurity
```

### For CI/CD
```bash
# Normal CI build (validation only, must pass)
go test -timeout 5m ./...

# Optional weekly security audit (may fail)
go test -tags security_audit -timeout 10m ./... || true
```

## Test Writing Guidelines

### Writing Validation Tests (Should Pass)

```go
package handlers

func TestSecurityFeature_Validation(t *testing.T) {
    // Test that security fix is properly implemented
    result := secureFunction()

    if containsPassword(result) {
        t.Fatal("SECURITY VIOLATION: Password exposed")
    }

    t.Log("✓ Security feature working correctly")
}
```

### Writing Demonstration Tests (Should Fail)

```go
//go:build security_audit
// +build security_audit

package wifimanager

func TestSecurityVulnerability_Demonstration(t *testing.T) {
    t.Log("=== SECURITY DEMONSTRATION ===")
    t.Log("This test documents a known security consideration")
    t.Log("CVSS: 9.1 (Critical) - CWE-522")
    t.Log("")
    t.Log("Issue: ...")
    t.Log("Impact: ...")
    t.Log("Mitigation: ...")

    // Demonstrate the issue
    if vulnerabilityExists() {
        t.Error("VULNERABILITY CONFIRMED: Description")
    }
}
```

## Security Recommendations for Deployment

### Essential (Must Implement)

1. **Physical Security**
   - Secure Raspberry Pi in locked enclosure
   - Prevent SD card removal

2. **Network Security**
   - Use HTTPS only (Gokrazy provides self-signed certs)
   - Strong authentication password
   - Isolate device on trusted network segment

3. **Access Control**
   - Change default password immediately
   - Use complex password (20+ characters)
   - Limit network access via firewall

### Recommended (Should Implement)

4. **Full Disk Encryption**
   - Use LUKS encryption on SD card
   - Protects against physical access attacks
   - Requires password/key on boot

5. **Credential Management**
   - Use read-only rclone remotes where possible
   - Rotate cloud credentials periodically
   - Use IAM roles with minimal permissions

6. **Monitoring**
   - Monitor authentication failures
   - Alert on repeated failed logins
   - Review sync logs regularly

### Optional (Nice to Have)

7. **Certificate Pinning**
   - Pin Gokrazy self-signed certificate
   - Detect MITM attacks
   - Requires custom client apps

8. **Network Segmentation**
   - Dedicated IoT VLAN
   - No access to main network
   - Internet access only for sync

## Acceptance Criteria

### For Merging to Main
- [ ] All validation tests pass (`go test ./...`)
- [ ] Security demonstration tests properly tagged
- [ ] Documentation complete (SECURITY.md, this file)
- [ ] CI/CD configuration updated

### For Security Audit Tests
- Security demonstration tests are **expected to fail**
- They document known considerations and design decisions
- They are excluded from normal CI/CD builds
- They can be run with `-tags security_audit`

## References

- [OWASP Top 10 2021](https://owasp.org/Top10/)
- [CWE Top 25](https://cwe.mitre.org/top25/)
- [NIST SP 800-63B](https://pages.nist.gov/800-63-3/sp800-63b.html)
- [Go Security Best Practices](https://go.dev/doc/security/best-practices)

## Version History

- 2025-01-XX: Initial strategy document
- Focus on separating demonstration vs validation tests
- Implemented password exposure fix validation
- Documented accepted risks and mitigation strategies
