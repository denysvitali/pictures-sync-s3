# Security Policy

## Supported Versions

We release patches for security vulnerabilities for the following versions:

| Version | Supported          |
| ------- | ------------------ |
| main    | :white_check_mark: |
| < 1.0   | :x:                |

## Reporting a Vulnerability

We take the security of pictures-sync-s3 seriously. If you believe you have found a security vulnerability, please report it to us as described below.

### Please Do Not

- Open a public GitHub issue for security vulnerabilities
- Disclose the vulnerability publicly before it has been addressed

### Please Do

1. **Report via GitHub Security Advisories** (preferred):
   - Go to the Security tab
   - Click "Report a vulnerability"
   - Fill out the form with details

2. **Report via Email**:
   - Email: [Your security email here]
   - Include detailed steps to reproduce
   - Include potential impact assessment

### What to Include

Please include the following information:

- Type of vulnerability (e.g., command injection, path traversal)
- Affected component(s)
- Step-by-step instructions to reproduce
- Proof of concept or exploit code (if possible)
- Potential impact of the vulnerability
- Suggested fix (if you have one)

## Security Measures

This project implements several security measures:

### Automated Security Scanning

- **Daily vulnerability scans** with govulncheck
- **SAST scanning** with gosec and CodeQL
- **Secret detection** with Gitleaks
- **Dependency scanning** with Trivy and Nancy
- **Supply chain security** with OSSF Scorecard

### Security Best Practices

1. **Least Privilege**: Services run with minimal required permissions
2. **Read-Only Mounts**: SD cards mounted read-only to prevent corruption
3. **Input Validation**: All user inputs validated and sanitized
4. **Secure Defaults**: Security-focused default configuration
5. **No Hardcoded Secrets**: All credentials stored in `/perm` partition

### Code Review Process

- All PRs require security scan passage
- Critical security findings block merges
- Regular security audits of dependencies
- Automated Dependabot security updates

## Security Features

### Authentication

- Token-based WebSocket authentication
- Configurable authentication for web UI
- No default credentials

### Data Protection

- Credentials stored only in `/perm/pictures-sync/rclone.conf`
- WiFi passwords stored in `/perm/wifi.json`
- File permissions strictly controlled
- No logging of sensitive data

### Network Security

- HTTPS recommended for production
- WebSocket security with token authentication
- Time synchronization via NTP before operations
- Network isolation options

### Filesystem Security

- Path traversal prevention
- Safe file operations with atomic writes
- Temporary file cleanup
- Mount point validation

## Vulnerability Disclosure Timeline

1. **Day 0**: Vulnerability reported
2. **Day 1**: Acknowledgment sent to reporter
3. **Day 7**: Initial assessment completed
4. **Day 30**: Fix developed and tested (target)
5. **Day 45**: Security advisory published (target)
6. **Day 60**: Full public disclosure (target)

Timelines may vary based on severity and complexity.

## Security Updates

Security updates are released as soon as possible after a vulnerability is confirmed and fixed.

### How to Stay Updated

1. **Watch this repository** for security advisories
2. **Enable Dependabot alerts** on your fork
3. **Subscribe to releases** for update notifications
4. **Follow security tab** for disclosed vulnerabilities

### Applying Updates

For Gokrazy deployments:

```bash
# Pull latest changes
git pull origin master

# Update dependencies
go get -u ./...
go mod tidy

# Deploy update
gok -i photo-backup update
```

## Security Hardening Recommendations

### Production Deployment

1. **Use HTTPS**: Configure reverse proxy with TLS
2. **Enable Authentication**: Set up web UI authentication
3. **Limit Network Access**: Use firewall rules
4. **Regular Updates**: Apply security updates promptly
5. **Monitor Logs**: Watch for suspicious activity

### Filesystem Hardening

```bash
# Ensure proper permissions on Gokrazy
chmod 700 /perm/pictures-sync
chmod 600 /perm/pictures-sync/rclone.conf
chmod 600 /perm/wifi.json
```

### Network Hardening

```bash
# Example iptables rules (if needed)
iptables -A INPUT -p tcp --dport 8080 -s 192.168.1.0/24 -j ACCEPT
iptables -A INPUT -p tcp --dport 8080 -j DROP
```

## Known Security Considerations

### Raspberry Pi Specific

1. **Physical Access**: Device has physical SD card access
2. **Default Passwords**: Change default Gokrazy password
3. **Network Exposure**: Limit network access to trusted devices
4. **Storage Security**: `/perm` partition not encrypted by default

### rclone Integration

1. **Cloud Credentials**: Stored in plain text in rclone.conf
2. **Encryption**: Use rclone crypt for cloud-side encryption
3. **Bandwidth**: No rate limiting by default

### Web UI

1. **Local Network**: Designed for local network use
2. **No Built-in HTTPS**: Use reverse proxy for HTTPS
3. **WebSocket Security**: Token-based authentication implemented

## Security Checklist for Deployments

- [ ] Changed default Gokrazy web interface password
- [ ] Enabled web UI authentication (if needed)
- [ ] Configured HTTPS via reverse proxy
- [ ] Restricted network access to trusted devices
- [ ] Enabled firewall rules
- [ ] Regular security update schedule established
- [ ] Backups configured and tested
- [ ] Cloud storage encryption configured (if needed)
- [ ] Monitoring and alerting set up
- [ ] Incident response plan documented

## Contact

For security-related questions or concerns:

- GitHub Security Advisories: [Report vulnerability](https://github.com/denysvitali/pictures-sync-s3/security/advisories/new)
- Security Email: [Configure your security email]

## Acknowledgments

We appreciate the security research community's efforts in responsibly disclosing vulnerabilities. Security researchers who report valid vulnerabilities will be acknowledged in our security advisories (with permission).

## License

This security policy is licensed under [CC BY 4.0](https://creativecommons.org/licenses/by/4.0/).
