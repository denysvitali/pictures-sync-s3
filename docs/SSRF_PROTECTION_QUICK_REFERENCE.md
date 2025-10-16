# SSRF Protection Quick Reference

## What is Protected?

The following network diagnostic endpoints now have SSRF protection:
- `/api/network/dns-lookup` - DNS resolution
- `/api/network/ping` - ICMP ping
- `/api/network/diagnostics` - Full network diagnostics

## Blocked Patterns

### Hostnames
| Pattern | Example | Reason |
|---------|---------|--------|
| localhost | `localhost` | Loopback access |
| localhost.localdomain | `localhost.localdomain` | Loopback variant |
| Cloud metadata | `metadata.google.internal` | Cloud metadata access |
| Cloud metadata | `169.254.169.254` | AWS metadata IP |
| Internal domains | `*.internal` | Internal DNS |
| Local domains | `*.local` | Local network |
| LAN domains | `*.lan` | LAN-only services |

### IP Addresses
| Range | CIDR | Purpose |
|-------|------|---------|
| 10.0.0.0 - 10.255.255.255 | 10.0.0.0/8 | Private network |
| 172.16.0.0 - 172.31.255.255 | 172.16.0.0/12 | Private network |
| 192.168.0.0 - 192.168.255.255 | 192.168.0.0/16 | Private network |
| 127.0.0.0 - 127.255.255.255 | 127.0.0.0/8 | Loopback |
| 169.254.0.0 - 169.254.255.255 | 169.254.0.0/16 | Link-local (AWS metadata) |
| 224.0.0.0 - 239.255.255.255 | 224.0.0.0/4 | Multicast |
| 240.0.0.0 - 255.255.255.255 | 240.0.0.0/4 | Reserved |
| ::1 | ::1/128 | IPv6 loopback |
| fc00:: - fdff:ffff:... | fc00::/7 | IPv6 ULA |
| fe80:: - febf:ffff:... | fe80::/10 | IPv6 link-local |

## Rate Limiting

- **Limit**: 10 requests per minute per client IP
- **Scope**: Per client IP (respects X-Forwarded-For)
- **Action**: Returns error after limit exceeded
- **Reset**: Automatic after 60 seconds

## What Still Works?

### Allowed Operations
| Target | Type | Why Allowed |
|--------|------|-------------|
| google.com | DNS/Ping | Public domain (validated) |
| cloudflare.com | DNS/Ping | Public domain (validated) |
| 8.8.8.8 | Ping | Hardcoded safe IP (Google DNS) |
| 1.1.1.1 | Ping | Hardcoded safe IP (Cloudflare DNS) |
| Local gateway | Ping | From routing table (not user input) |

### Any Public Domain
Users can still diagnose legitimate issues by:
- Looking up any public domain (e.g., `example.com`)
- Pinging any public IP (e.g., `8.8.4.4`)
- Running network diagnostics to check internet connectivity

## Error Messages

| Error | Meaning |
|-------|---------|
| "localhost access not allowed" | Tried to access localhost |
| "cloud metadata endpoint access not allowed" | Tried to access cloud metadata |
| "private IP address not allowed" | Tried to access internal network |
| "link-local address not allowed" | Tried to access link-local (169.254.x.x) |
| "rate limit exceeded" | Too many requests from this IP |
| "URL scheme not allowed" | Used http:// instead of just hostname |
| "credentials in hostname not allowed" | Used user:pass@hostname |
| "internal domain access not allowed" | Used .internal/.local domain |

## Testing

### Test Legitimate Use
```bash
# Should work - public DNS
curl -X POST https://device/api/network/dns-lookup \
  -d '{"hostname": "google.com"}'

# Should work - public IP
curl -X POST https://device/api/network/ping \
  -d '{"hostname": "8.8.8.8", "count": 4}'
```

### Test Protection
```bash
# Should block - AWS metadata
curl -X POST https://device/api/network/dns-lookup \
  -d '{"hostname": "169.254.169.254"}'
# Returns: "Request blocked: cloud metadata endpoint access not allowed"

# Should block - private network
curl -X POST https://device/api/network/ping \
  -d '{"hostname": "192.168.1.1"}'
# Returns: "Request blocked: private IP address not allowed"

# Should block - localhost
curl -X POST https://device/api/network/dns-lookup \
  -d '{"hostname": "localhost"}'
# Returns: "Request blocked: localhost access not allowed"
```

## Monitoring

All SSRF events are logged:

**Successful request:**
```
[SSRF] Network diagnostic request from 203.0.113.1 for target: google.com
[SSRF] Validation successful for google.com from client 203.0.113.1, resolved to 4 IPs
```

**Blocked request:**
```
[SSRF] DNS lookup blocked for 169.254.169.254 from 203.0.113.1:
    SSRF protection: cloud metadata endpoint access not allowed (target: 169.254.169.254)
```

**Rate limit:**
```
[SSRF] Rate limit exceeded for client 203.0.113.1
```

## Configuration

Current configuration in `/workspace/pictures-sync-s3/cmd/webui/main.go`:

```go
// Initialize SSRF validator with rate limiting
// Allow 10 network diagnostic requests per minute per client IP
ssrfValidator := ssrf.NewValidator(10, time.Minute)
```

To adjust rate limits, modify these parameters:
- First parameter: max requests per window
- Second parameter: time window duration

Examples:
```go
// Strict: 5 per minute
ssrfValidator := ssrf.NewValidator(5, time.Minute)

// Lenient: 30 per minute
ssrfValidator := ssrf.NewValidator(30, time.Minute)

// Very strict: 10 per hour
ssrfValidator := ssrf.NewValidator(10, time.Hour)
```

## Attack Scenarios Prevented

### Scenario 1: AWS Metadata Theft
**Attack:** Attacker tries to steal AWS credentials
```
POST /api/network/dns-lookup
{"hostname": "169.254.169.254"}
```
**Protection:** BLOCKED - Cloud metadata endpoint not allowed

### Scenario 2: Internal Port Scanning
**Attack:** Attacker scans internal network
```
POST /api/network/ping
{"hostname": "192.168.1.1"}
```
**Protection:** BLOCKED - Private IP address not allowed

### Scenario 3: Localhost Access
**Attack:** Attacker probes local services
```
POST /api/network/dns-lookup
{"hostname": "localhost"}
```
**Protection:** BLOCKED - Localhost access not allowed

### Scenario 4: Internal DNS Enumeration
**Attack:** Attacker enumerates internal services
```
POST /api/network/dns-lookup
{"hostname": "database.internal"}
```
**Protection:** BLOCKED - Internal domain not allowed

### Scenario 5: Rate Limit Bypass
**Attack:** Attacker floods network diagnostic endpoints
```
# 11th request within 60 seconds
POST /api/network/ping
{"hostname": "google.com"}
```
**Protection:** BLOCKED - Rate limit exceeded

## Troubleshooting

### "Rate limit exceeded" for legitimate use
**Solution:** Wait 60 seconds for rate limit to reset, or adjust rate limit in code

### "Request blocked" for public domain
**Possible causes:**
1. Domain resolves to private IP
2. DNS returns link-local address
3. Domain uses blocked suffix (.internal, .local)

**Solution:** Check DNS resolution manually, verify domain is truly public

### Gateway ping not working
**Note:** Gateway ping in diagnostics should work. If it doesn't:
1. Check `/proc/net/route` for default gateway
2. Verify gateway is being read correctly
3. Gateway ping bypasses SSRF (trusted source)

## Security Audit

Regular monitoring recommendations:
1. Review logs for blocked attempts
2. Monitor rate limit violations
3. Look for patterns in attack attempts
4. Update block lists if new attack vectors discovered

## Support

For issues or questions:
1. Check logs for error reason
2. Verify request is legitimate
3. Confirm target is not in blocked ranges
4. Review rate limiting status
5. See `/workspace/pictures-sync-s3/pkg/ssrf/README.md` for details
