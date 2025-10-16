# Security Fix: WiFi Password Exposure (Critical)

## Vulnerability Summary

**Severity**: CRITICAL
**CVSS Score**: 8.2 (High)
**Issue**: WiFi passwords were exposed in plaintext via the `/api/wifi/networks` API endpoint
**Impact**: Any authenticated user or attacker with compromised credentials could retrieve all stored WiFi passwords with a single API call

## Problem Description

The `HandleWiFiNetworks` API handler was returning complete `Network` objects that included the `PSK` (password) field in JSON responses. This meant that WiFi credentials for all saved networks were transmitted over the network and could be viewed in API responses.

### Vulnerable Code (BEFORE)

```go
// HandleWiFiNetworks returns saved networks
func (ctx *Context) HandleWiFiNetworks(w http.ResponseWriter, r *http.Request) {
	// ... validation ...

	networks := ctx.WiFiMgr.GetNetworks()
	JSONResponse(w, networks)  // ← Exposes PSK field!
}
```

### Vulnerable Response Example

```json
GET /api/wifi/networks

[
  {
    "ssid": "HomeNetwork",
    "psk": "MySecretPassword123"  ← PASSWORD IN PLAINTEXT!
  },
  {
    "ssid": "GuestNetwork",
    "psk": "GuestPass456"  ← PASSWORD IN PLAINTEXT!
  }
]
```

## Security Fix

### Fixed Code (AFTER)

```go
// SafeNetworkInfo represents network information without sensitive credentials.
// SECURITY: This struct is used to prevent password exposure via API responses.
// Even with authentication, credentials should never be returned in API calls.
type SafeNetworkInfo struct {
	SSID        string `json:"ssid"`
	HasPassword bool   `json:"has_password"`
}

// HandleWiFiNetworks returns saved networks without exposing passwords
// SECURITY FIX: Filters out PSK field to prevent credential exposure
func (ctx *Context) HandleWiFiNetworks(w http.ResponseWriter, r *http.Request) {
	// ... validation ...

	networks := ctx.WiFiMgr.GetNetworks()

	// Convert to safe response that excludes passwords
	safeNetworks := make([]SafeNetworkInfo, len(networks))
	for i, network := range networks {
		safeNetworks[i] = SafeNetworkInfo{
			SSID:        network.SSID,
			HasPassword: network.PSK != "",
		}
	}

	JSONResponse(w, safeNetworks)
}
```

### Safe Response Example

```json
GET /api/wifi/networks

[
  {
    "ssid": "HomeNetwork",
    "has_password": true  ← No password exposed!
  },
  {
    "ssid": "GuestNetwork",
    "has_password": true  ← No password exposed!
  }
]
```

## Changes Made

1. **Created `SafeNetworkInfo` struct** (lines 44-50 in `/workspace/pictures-sync-s3/pkg/handlers/wifi.go`):
   - Only includes `SSID` (network name)
   - Includes `has_password` boolean flag (safe metadata)
   - Explicitly excludes `PSK` password field

2. **Updated `HandleWiFiNetworks` handler** (lines 52-77):
   - Fetches networks from WiFi manager as before
   - Transforms Network objects into SafeNetworkInfo objects
   - Returns sanitized response that never includes passwords

3. **Added security documentation**:
   - Comments explaining the security rationale
   - Clear indication this is a security fix

4. **Added security test** (`/workspace/pictures-sync-s3/pkg/handlers/wifi_security_test.go`):
   - `TestHandleWiFiNetworks_NoPasswordExposure`: Verifies passwords are not in response body
   - `TestSafeNetworkInfo_Structure`: Verifies struct doesn't contain password fields

## Frontend Compatibility

The web UI (`/workspace/pictures-sync-s3/web/app.js`) only uses the `ssid` field from network objects, so this change is fully backward compatible:

```javascript
// displaySavedNetworks function only uses network.ssid
list.innerHTML = networks.map(network => `
    <div class="network-item">
        <div class="network-info">
            <div class="network-name">${escapeHtml(network.ssid)}</div>
        </div>
        ...
    </div>
`).join('');
```

The new `has_password` field is available if the UI wants to display lock icons or indicators in the future.

## Security Principles Applied

1. **Least Privilege**: API responses only include the minimum necessary information
2. **Defense in Depth**: Passwords protected even when authentication is bypassed
3. **Data Minimization**: Credentials never transmitted unless absolutely necessary
4. **Secure by Default**: New struct explicitly designed to be safe

## Attack Scenarios Prevented

### Before Fix
1. Attacker obtains HTTP Basic Auth credentials (weak password, shoulder surfing, etc.)
2. Single API call: `GET /api/wifi/networks`
3. All WiFi passwords exposed in response
4. Attacker gains access to all networks the device has connected to

### After Fix
1. Even if attacker obtains authentication credentials
2. API call: `GET /api/wifi/networks`
3. Response only contains network names and metadata
4. Passwords remain secure in `/perm/extra-wifi.json`
5. No credential exposure through API

## Additional Recommendations

While this fix resolves the immediate critical vulnerability, consider these additional security enhancements:

1. **Encrypt WiFi passwords at rest**: Store encrypted PSK values in `/perm/extra-wifi.json`
2. **Rate limiting**: Limit API calls to prevent brute force attacks
3. **Audit logging**: Log all WiFi configuration changes
4. **Strong authentication**: Replace HTTP Basic Auth with token-based authentication
5. **HTTPS enforcement**: Ensure all API traffic is encrypted in transit

## Testing

Run the security test suite:

```bash
go test ./pkg/handlers -v -run TestHandleWiFiNetworks_NoPasswordExposure
go test ./pkg/handlers -v -run TestSafeNetworkInfo_Structure
```

## Files Modified

- `/workspace/pictures-sync-s3/pkg/handlers/wifi.go` - Applied security fix
- `/workspace/pictures-sync-s3/pkg/handlers/wifi_security_test.go` - Added security tests (NEW)

## References

- CWE-200: Exposure of Sensitive Information to an Unauthorized Actor
- CWE-522: Insufficiently Protected Credentials
- OWASP Top 10: A01:2021 – Broken Access Control
