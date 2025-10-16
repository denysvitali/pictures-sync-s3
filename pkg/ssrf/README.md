# SSRF Protection Package

This package provides comprehensive Server-Side Request Forgery (SSRF) protection for network diagnostic endpoints.

## Overview

SSRF vulnerabilities allow attackers to abuse server functionality to access or scan internal networks, cloud metadata services, and other restricted resources. This package prevents such attacks by validating all hostnames and IP addresses before allowing network operations.

## Features

### 1. Hostname Validation
- Blocks localhost and localhost variants
- Blocks cloud metadata endpoints (AWS, Google Cloud, Azure, etc.)
- Blocks internal domain suffixes (.internal, .local, .localhost, .lan)
- Validates hostname format (no URLs, no credentials)
- Resolves DNS and validates all resulting IPs

### 2. IP Address Validation
- Blocks loopback addresses (127.0.0.0/8, ::1)
- Blocks private IP ranges (10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16)
- Blocks link-local addresses (169.254.0.0/16, fe80::/10)
- Blocks multicast addresses
- Blocks unspecified addresses (0.0.0.0, ::)
- Blocks carrier-grade NAT and reserved ranges

### 3. Rate Limiting
- Per-client IP rate limiting using token bucket algorithm
- Configurable request limits and time windows
- Automatic cleanup of expired rate limit buckets
- Prevents abuse of network diagnostic endpoints

### 4. Comprehensive Logging
- All network diagnostic requests are logged with client IP
- Blocked requests include reason for blocking
- Successful validations logged for security audit

## Protected IP Ranges

### IPv4
- `10.0.0.0/8` - Private network
- `172.16.0.0/12` - Private network
- `192.168.0.0/16` - Private network
- `127.0.0.0/8` - Loopback
- `169.254.0.0/16` - Link-local (includes AWS metadata at 169.254.169.254)
- `100.64.0.0/10` - Shared address space (Carrier-grade NAT)
- `192.0.0.0/24` - IETF Protocol Assignments
- `192.0.2.0/24` - TEST-NET-1
- `198.51.100.0/24` - TEST-NET-2
- `203.0.113.0/24` - TEST-NET-3
- `198.18.0.0/15` - Benchmark testing
- `240.0.0.0/4` - Reserved
- `224.0.0.0/4` - Multicast

### IPv6
- `::1/128` - Loopback
- `fc00::/7` - Unique local addresses (ULA)
- `fe80::/10` - Link-local
- `ff00::/8` - Multicast
- `2001:db8::/32` - Documentation

### Cloud Metadata Endpoints
- `169.254.169.254` - AWS EC2 metadata
- `metadata.google.internal` - Google Cloud metadata
- `metadata.azure.com` - Azure metadata
- `metadata.packet.net` - Packet/Equinix Metal metadata
- `169.254.169.123` - DigitalOcean metadata

## Usage

### Initialization

```go
import (
    "time"
    "github.com/denysvitali/pictures-sync-s3/pkg/ssrf"
)

// Create validator with rate limiting
// Allow 10 requests per minute per client IP
validator := ssrf.NewValidator(10, time.Minute)
defer validator.Stop() // Cleanup when done
```

### Validating Hostnames

```go
// Validate hostname and get resolved IPs
clientIP := "203.0.113.1" // Get from request
hostname := "google.com"

ips, err := validator.ValidateHostname(hostname, clientIP)
if err != nil {
    // Request blocked - log error and return to client
    log.Printf("SSRF blocked: %v", err)
    http.Error(w, "Request blocked", http.StatusForbidden)
    return
}

// Safe to use the validated IPs
for _, ip := range ips {
    // Perform network operation with ip
}
```

### Validating IP Addresses

```go
// Validate direct IP address
clientIP := "203.0.113.1"
ipStr := "8.8.8.8"

ip, err := validator.ValidateIP(ipStr, clientIP)
if err != nil {
    // Request blocked
    return
}

// Safe to use the validated IP
```

### Integration with HTTP Handlers

```go
func (ctx *Context) HandleDNSLookup(w http.ResponseWriter, r *http.Request) {
    var req struct {
        Hostname string `json:"hostname"`
    }

    json.NewDecoder(r.Body).Decode(&req)

    // Extract client IP
    clientIP := getClientIP(r)

    // Validate with SSRF protection
    ips, err := ctx.SSRFValidator.ValidateHostname(req.Hostname, clientIP)
    if err != nil {
        log.Printf("[SSRF] Blocked request from %s: %v", clientIP, err)
        JSONResponse(w, map[string]interface{}{
            "error": fmt.Sprintf("Request blocked: %v", err),
        })
        return
    }

    // Continue with safe IPs
    // ...
}
```

## Error Handling

The package returns `ValidationError` with a descriptive reason:

```go
type ValidationError struct {
    Reason string // Human-readable reason
    Target string // The hostname/IP that was blocked
}
```

Common error reasons:
- "localhost access not allowed"
- "cloud metadata endpoint access not allowed"
- "private IP address not allowed"
- "link-local address not allowed"
- "rate limit exceeded"
- "empty hostname"
- "URL scheme not allowed, hostname only"
- "credentials in hostname not allowed"
- "internal domain access not allowed"

## Rate Limiting

The validator uses a token bucket algorithm for rate limiting:

- Each client IP has its own bucket
- Bucket refills after the time window expires
- Old buckets are automatically cleaned up
- Configurable max requests and time window

Example configurations:

```go
// Strict: 5 requests per minute
validator := ssrf.NewValidator(5, time.Minute)

// Moderate: 10 requests per minute (default)
validator := ssrf.NewValidator(10, time.Minute)

// Lenient: 30 requests per minute
validator := ssrf.NewValidator(30, time.Minute)
```

## Security Logging

All validation attempts are logged:

```
[SSRF] Network diagnostic request from 203.0.113.1 for target: google.com
[SSRF] Validation successful for google.com from client 203.0.113.1, resolved to 4 IPs
```

Blocked requests include the reason:

```
[SSRF] DNS lookup blocked for 169.254.169.254 from 203.0.113.1:
    SSRF protection: cloud metadata endpoint access not allowed (target: 169.254.169.254)
```

Rate limit violations:

```
[SSRF] Rate limit exceeded for client 203.0.113.1
```

## Testing

The package includes comprehensive tests covering:

- All blocked hostname patterns
- All blocked IP ranges (IPv4 and IPv6)
- Rate limiting (per-client and time-based)
- Validation error formatting
- Performance benchmarks

Run tests:

```bash
go test ./pkg/ssrf/... -v
```

Run benchmarks:

```bash
go test ./pkg/ssrf/... -bench=.
```

## Attack Vectors Prevented

### 1. AWS Metadata Access
```
curl http://169.254.169.254/latest/meta-data/
curl http://metadata.google.internal/computeMetadata/v1/
```
**Status:** BLOCKED

### 2. Internal Network Scanning
```
curl http://192.168.1.1/admin
curl http://10.0.0.1/
curl http://172.16.0.1/
```
**Status:** BLOCKED

### 3. Localhost Access
```
curl http://localhost/admin
curl http://127.0.0.1:8080/api
```
**Status:** BLOCKED

### 4. Internal Domain Bypass
```
curl http://database.internal/
curl http://admin.local/
```
**Status:** BLOCKED

### 5. URL Manipulation
```
curl http://user:pass@internal.service/
curl https://evil.com@metadata/
```
**Status:** BLOCKED

## Performance

The validator is designed for minimal overhead:

- DNS resolution happens only once per validation
- IP range checks use efficient CIDR matching
- Rate limiting uses in-memory map with O(1) lookups
- Automatic cleanup prevents memory leaks

Benchmark results (typical):
```
BenchmarkValidateHostname-8    1000    1000000 ns/op
BenchmarkValidateIP-8        500000      2000 ns/op
```

## Best Practices

1. **Initialize once**: Create one validator instance per application
2. **Cleanup on shutdown**: Call `validator.Stop()` to stop cleanup goroutine
3. **Log all blocks**: Monitor logs for attack attempts
4. **Adjust rate limits**: Based on legitimate usage patterns
5. **Validate all user input**: Never trust hostname/IP from users
6. **Use with other security**: Combine with authentication, CSRF protection, etc.

## Integration Checklist

- [ ] Create SSRF validator on application startup
- [ ] Add validator to handler context
- [ ] Update DNS lookup endpoint to use validator
- [ ] Update ping endpoint to use validator
- [ ] Update network diagnostics endpoint to use validator
- [ ] Implement client IP extraction (X-Forwarded-For aware)
- [ ] Configure appropriate rate limits
- [ ] Monitor logs for blocked requests
- [ ] Test with legitimate and malicious inputs
- [ ] Document any allowed exceptions (e.g., gateway ping)

## Exception Handling

Some legitimate use cases may require exceptions:

### Gateway Ping
The network diagnostics endpoint allows pinging the local gateway even though it's a private IP, since it's determined from the routing table rather than user input.

```go
// Get gateway from routing table (trusted source)
gateway := getDefaultGatewayFromRoutes()

// Allow ping without SSRF validation (not user-controlled)
if gateway != "" {
    result["gateway_reachable"] = testICMPPing(gateway, 2*time.Second)
}
```

### Hardcoded Safe IPs
Public DNS servers and well-known services can be hardcoded:

```go
// These are safe, hardcoded IPs (not user input)
result["dns_google"] = testICMPPing("8.8.8.8", 2*time.Second)
result["dns_cloudflare"] = testICMPPing("1.1.1.1", 2*time.Second)
```

## References

- [OWASP SSRF Prevention Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Server_Side_Request_Forgery_Prevention_Cheat_Sheet.html)
- [PortSwigger SSRF](https://portswigger.net/web-security/ssrf)
- [AWS IMDS Security](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/instancedata-data-retrieval.html)
- [RFC 1918 - Private Address Space](https://tools.ietf.org/html/rfc1918)
- [RFC 3927 - Link-Local Addresses](https://tools.ietf.org/html/rfc3927)
