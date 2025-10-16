# WebSocket Security Documentation

## Overview

The WebSocket implementation in this package includes multiple layers of security to protect against unauthorized access and abuse.

## Security Features

### 1. Strict Origin Validation

The `checkOriginStrict()` function enforces secure origin validation with the following rules:

#### Empty Origin Headers
- **Status**: REJECTED
- **Reason**: Empty origins could be from non-browser clients attempting to bypass CORS
- **Log**: "WebSocket: Rejected connection with empty Origin from {IP}"

#### Private IP Range Validation (RFC 1918)
The system correctly validates private IP ranges:

**Accepted Ranges:**
- `10.0.0.0/8` - 10.0.0.0 to 10.255.255.255
- `172.16.0.0/12` - 172.16.0.0 to 172.31.255.255 (FIXED)
- `192.168.0.0/16` - 192.168.0.0 to 192.168.255.255
- `169.254.0.0/16` - Link-local addresses
- `127.0.0.0/8` - Loopback
- `fc00::/7` - IPv6 Unique Local Addresses
- `fe80::/10` - IPv6 Link-local

**Previously Vulnerable (Now Fixed):**
The original implementation accepted ALL 172.x.x.x addresses, including non-private ranges:
- `172.32.0.0 - 172.255.255.255` were incorrectly accepted

**Current Implementation:**
Only RFC 1918 private range `172.16.0.0/12` (172.16.0.0 - 172.31.255.255) is accepted.

#### Localhost and mDNS
- `localhost` and `127.0.0.1` are accepted
- `.local` domains (mDNS) are accepted for local network discovery

#### Same-Origin Policy
- Connections where `Origin` header exactly matches `Host` header are accepted
- Port numbers are validated when present in the origin

### 2. Configurable Whitelist

The package supports a strict whitelist mode:

```go
// Set allowed origins
websocket.SetAllowedOrigins([]string{
    "example.com",
    "trusted.example.com",
})

// When whitelist is active, ONLY listed origins are accepted
// Private IPs are REJECTED when whitelist is active
```

**Security Note:** When a whitelist is configured, even private IP addresses are rejected unless explicitly listed.

### 3. Rate Limiting Per IP

**Connection Rate Limits:**
- **Rate**: 5 connections per minute per IP
- **Burst**: 2 connections
- **Implementation**: Token bucket algorithm via `golang.org/x/time/rate`

**Cleanup:**
- Rate limiter state is cleaned up every 5 minutes
- Prevents memory leaks from storing limiters for inactive IPs

**HTTP Response:**
- Status: `429 Too Many Requests`
- Body: "Too many connection attempts. Please try again later."

### 4. Token-Based Authentication

WebSocket connections require a one-time use token:

1. Client requests token via `/api/ws-token` (Basic Auth protected)
2. Server generates cryptographically secure 32-byte token
3. Token valid for 5 minutes
4. Client sends token as first WebSocket message: `{"type": "auth", "token": "..."}`
5. Token is deleted after successful validation (one-time use)

**Security Properties:**
- Tokens are cryptographically random (256 bits)
- Tokens expire after 5 minutes
- Tokens are single-use (deleted after validation)
- Failed auth attempts are logged with IP address

### 5. Security Logging

All suspicious connection attempts are logged:

```go
// Empty origin
log.Printf("WebSocket: Rejected connection with empty Origin from %s", r.RemoteAddr)

// Invalid origin format
log.Printf("WebSocket: Rejected connection with invalid Origin '%s' from %s: %v", origin, r.RemoteAddr, err)

// Non-whitelisted origin
log.Printf("WebSocket: Rejected connection from non-whitelisted origin '%s' (from %s)", origin, r.RemoteAddr)

// Non-private IP origin
log.Printf("WebSocket: Rejected non-private origin '%s' from %s", origin, r.RemoteAddr)

// Port mismatch
log.Printf("WebSocket: Rejected private IP origin with mismatched port: %s (expected %s) from %s",
    origin, requestPort, r.RemoteAddr)

// Rate limit exceeded
log.Printf("WebSocket: Rate limit exceeded for IP %s", clientIP)

// Auth failures
log.Printf("WebSocket auth failed: invalid token from %s", r.RemoteAddr)
```

## Usage Examples

### Default Mode (Private IP Validation)

```go
// Accept connections from private IPs and same-origin
http.HandleFunc("/ws", websocket.HandleWebSocket(stateMgr, eventMgr))
```

### Whitelist Mode (Production)

```go
// Only accept specific origins
websocket.SetAllowedOrigins([]string{
    "photos.example.com",
    "backup.example.com",
})

http.HandleFunc("/ws", websocket.HandleWebSocket(stateMgr, eventMgr))
```

### Starting Cleanup Goroutines

```go
ctx := context.Background()

// Start token cleanup
go websocket.CleanupExpiredWSTokens(ctx)

// Start rate limiter cleanup
go websocket.StartRateLimiterCleanup(ctx)
```

## Testing

Comprehensive security tests are provided in `security_test.go`:

```bash
# Run all security tests
go test -v ./pkg/websocket

# Test RFC 1918 validation specifically
go test -v ./pkg/websocket -run TestCheckOriginStrict_PrivateIPRanges

# Test rate limiting (requires 13+ seconds)
go test -v ./pkg/websocket -run TestRateLimiting
```

## Security Considerations

1. **Never disable origin validation** - The empty origin bypass was a critical vulnerability
2. **Use whitelist in production** - Default private IP validation is for local network appliances
3. **Monitor logs** - Rejected connections may indicate attack attempts
4. **Token expiry** - 5-minute token lifetime balances security and usability
5. **Rate limiting** - Prevents brute force token guessing and DoS attacks

## Migration from Old Code

If you're upgrading from the vulnerable version:

### Before (VULNERABLE):
```go
CheckOrigin: func(r *http.Request) bool {
    origin := r.Header.Get("Origin")
    if origin == "" {
        return true // VULNERABLE: accepts empty origins
    }
    // ...
    if strings.HasPrefix(host, "172.") { // VULNERABLE: accepts 172.32-255.x.x
        return true
    }
}
```

### After (SECURE):
```go
CheckOrigin: checkOriginStrict,
// Properly validates RFC 1918 ranges
// Rejects empty origins
// Supports configurable whitelist
```

## References

- RFC 1918: Address Allocation for Private Internets
- RFC 6455: The WebSocket Protocol
- OWASP WebSocket Security Cheat Sheet
