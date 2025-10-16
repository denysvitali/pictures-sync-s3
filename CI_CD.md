# CI/CD Pipeline Documentation

## Overview

This document describes the comprehensive CI/CD pipeline implemented for the pictures-sync-s3 project. The pipeline enforces security best practices, automates quality checks, and ensures build reproducibility.

## Pipeline Components

### 1. Main CI Pipeline (`.github/workflows/ci.yml`)

The primary continuous integration pipeline that runs on every push and pull request.

#### Jobs

1. **Test and Coverage**
   - Runs all tests with race detector
   - Generates code coverage reports
   - Uploads coverage to Codecov
   - **Timeout**: 15 minutes
   - **Artifacts**: Coverage reports (30 days retention)

2. **Cross-Platform Build**
   - Builds for multiple architectures:
     - `linux/amd64` - Standard x86-64
     - `linux/arm64` - ARM 64-bit (RPi 4)
     - `linux/armv7` - ARM 32-bit (older RPis)
   - Creates optimized binaries with trimmed paths
   - Embeds version and build date
   - **Artifacts**: Compiled binaries (30 days retention)

3. **Code Quality Checks**
   - golangci-lint with comprehensive rule set
   - go vet static analysis
   - gofmt formatting verification
   - staticcheck advanced analysis
   - **Timeout**: 10 minutes

4. **Dependency Analysis**
   - Checks for outdated dependencies
   - Verifies go.mod/go.sum are tidy
   - Monitors dependency count
   - **Continues on error**: Yes (informational)

5. **Integration Tests**
   - Runs integration-tagged tests
   - Validates test data structure
   - **Continues on error**: Yes (may require specific setup)

6. **Build Reproducibility**
   - Builds binary twice with same inputs
   - Compares SHA256 checksums
   - Ensures deterministic builds
   - **Critical**: Must pass for release

7. **Performance Benchmarks**
   - Runs Go benchmarks with memory profiling
   - 5-second benchmark time
   - **Artifacts**: Benchmark results (30 days retention)

8. **Release Artifacts** (tags only)
   - Creates tar.gz archives for each platform
   - Generates SHA256 checksums
   - **Artifacts**: Release archives (90 days retention)

9. **CI Success Gate**
   - Aggregates all job results
   - Fails if any critical job fails
   - Required for merge

### 2. Security Pipeline (`.github/workflows/security.yml`)

Comprehensive security scanning running on push, PR, and daily schedule (2 AM UTC).

#### Security Jobs

1. **Go Vulnerability Check (govulncheck)**
   - Scans for known Go vulnerabilities
   - Uses official Go vulnerability database
   - **Blocks merge**: Yes
   - **Artifacts**: Vulnerability report (90 days)

2. **Dependency Scanning (Nancy)**
   - Checks all dependencies for CVEs
   - Uses Sonatype OSS Index
   - **Artifacts**: Nancy report (90 days)

3. **SAST Scanning (gosec)**
   - Static Application Security Testing
   - Checks for common security issues:
     - Hardcoded credentials (G101)
     - SQL injection (G201-G203)
     - Command injection (G204)
     - Path traversal (G304-G305)
     - Weak crypto (G401-G505)
     - Memory aliasing (G601)
   - Uploads SARIF to GitHub Security
   - **Format**: SARIF + JSON

4. **Secret Scanning (Gitleaks)**
   - Scans entire Git history for secrets
   - Checks for API keys, passwords, tokens
   - **Blocks merge**: Yes
   - Supports `.gitleaksignore` for false positives

5. **Container/Filesystem Scanning (Trivy)**
   - Vulnerability scanning
   - Secret detection
   - Configuration scanning
   - **Severities**: CRITICAL, HIGH, MEDIUM
   - **Format**: SARIF + JSON

6. **CodeQL Analysis**
   - Advanced semantic code analysis
   - Security and quality queries
   - Integrated with GitHub Security tab
   - **Async**: Results appear in Security tab

7. **License Compliance**
   - Scans all dependency licenses
   - Blocks forbidden licenses:
     - GPL-2.0
     - GPL-3.0
     - AGPL-3.0
   - **Artifacts**: License report (90 days)

8. **Supply Chain Security (OSSF Scorecard)**
   - Evaluates project security posture
   - Checks for security best practices
   - Uploads results to GitHub Security

9. **Configuration Security Audit**
   - Checks for hardcoded credentials
   - Validates file permissions
   - Audits build tags for unsafe usage

10. **Memory Safety Analysis**
    - Race condition detection
    - Memory sanitizer checks
    - **Artifacts**: Race/memory reports (90 days)

11. **Security Gate**
    - Aggregates all security scan results
    - **Critical failures block merge**:
      - govulncheck vulnerabilities
      - Gitleaks secret detection
    - **Warnings don't block**:
      - gosec findings (may have false positives)
      - Trivy findings (network issues)
      - CodeQL (runs async)

### 3. CodeQL Advanced (`.github/workflows/codeql.yml`)

Dedicated CodeQL pipeline for detailed code analysis.

- Runs weekly on Monday at 3 AM UTC
- Uses extended security query pack
- Excludes test-data and vendor directories
- Results appear in GitHub Security tab

### 4. Automated Dependency Updates (`.github/dependabot.yml`)

Dependabot configuration for automated security and version updates.

#### Go Modules
- **Schedule**: Weekly on Monday at 6 AM UTC
- **PR Limit**: 10 open PRs
- **Grouping**: Minor and patch updates grouped
- **Ignored**:
  - rclone major versions (needs careful testing)
  - Go version updates (manual only)
- **Labels**: dependencies, go, security

#### GitHub Actions
- **Schedule**: Weekly on Monday at 6 AM UTC
- **PR Limit**: 5 open PRs
- **Grouping**: Minor and patch updates grouped
- **Labels**: dependencies, github-actions, ci

## Configuration Files

### `.golangci.yml`

Comprehensive linting configuration with 30+ enabled linters.

#### Enabled Linter Categories

**Security Linters:**
- `gosec` - Security-focused analysis
- `gocritic` - Comprehensive checks including security
- `exportloopref` - Prevents loop variable bugs
- `nilerr` - Nil error return detection

**Code Quality:**
- `errcheck` - Unchecked error detection
- `gosimple` - Code simplification
- `govet` - Standard Go checks
- `staticcheck` - Advanced static analysis
- `revive` - Style guide enforcement

**Performance:**
- `bodyclose` - HTTP response body closure
- `prealloc` - Slice preallocation
- `unconvert` - Unnecessary conversions

**Maintainability:**
- `gocyclo` - Cyclomatic complexity (max 15)
- `gocognit` - Cognitive complexity (max 20)
- `funlen` - Function length (100 lines/50 statements)
- `maintidx` - Maintainability index

#### Exclusions for Tests
Test files (`*_test.go`) are excluded from:
- `gocyclo` - Tests can be complex
- `errcheck` - Test error handling is different
- `gosec` - Security rules don't apply to tests
- `funlen` - Tests can be long
- `maintidx` - Maintainability less critical for tests

#### Custom Rules

**Allowed Globals/Inits:**
- Main packages (`cmd/`) can use globals and init functions
- Necessary for Gokrazy initialization

**Long Lines:**
- Web UI files can have long lines (embedded HTML/CSS/JS)
- go:generate directives excluded

**Complexity:**
- LED pattern files allowed higher complexity
- System interaction code gets special handling

### `.gitleaksignore`

Suppression file for false positive secret detections.

## Security Gate Behavior

### Blocking Failures

The following security issues will **block PR merges**:

1. **Known Vulnerabilities** (govulncheck)
   - Any vulnerability in direct or indirect dependencies
   - Must be addressed or explicitly accepted

2. **Leaked Secrets** (gitleaks)
   - Detected API keys, passwords, tokens in Git history
   - Must be removed and rotated

3. **Critical Severity Issues**
   - CRITICAL severity findings from any scanner
   - Requires immediate action

### Warning-Only Failures

These generate warnings but don't block merges:

1. **SAST Findings** (gosec)
   - May have false positives
   - Should be reviewed but not blocking

2. **Trivy Findings**
   - May fail due to network issues
   - Informational only

3. **CodeQL Results**
   - Runs asynchronously
   - Results appear in Security tab
   - Reviewed separately

## Running Locally

### Prerequisites

```bash
# Install required tools
go install github.com/golangci/golangci-lint/cmd/golangci-lint@v1.62.2
go install golang.org/x/vuln/cmd/govulncheck@latest
go install github.com/securego/gosec/v2/cmd/gosec@latest
```

### Run Linting

```bash
# Run all linters
golangci-lint run

# Run with auto-fix
golangci-lint run --fix

# Run specific linters
golangci-lint run --disable-all --enable=gosec,errcheck
```

### Run Security Scans

```bash
# Vulnerability scanning
govulncheck ./...

# Security SAST
gosec ./...

# Secret scanning (requires Docker)
docker run -v $(pwd):/path zricethezav/gitleaks:latest detect --source="/path" -v
```

### Run Tests

```bash
# All tests with race detector
go test -race -v ./...

# With coverage
go test -race -coverprofile=coverage.out -covermode=atomic ./...

# View coverage
go tool cover -html=coverage.out
```

### Build for Raspberry Pi

```bash
# ARM64 (Raspberry Pi 4)
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -trimpath \
  -ldflags="-s -w" -o pictures-sync-arm64 ./cmd/pictures-sync

# ARMv7 (Raspberry Pi 3)
GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=0 go build -trimpath \
  -ldflags="-s -w" -o pictures-sync-armv7 ./cmd/pictures-sync
```

## Artifacts and Reports

### Artifact Retention

| Artifact Type | Retention Period | Purpose |
|---------------|------------------|---------|
| Coverage Reports | 30 days | Code coverage analysis |
| Build Binaries | 30 days | Testing/deployment |
| Benchmark Results | 30 days | Performance tracking |
| Security Reports | 90 days | Audit trail |
| Release Archives | 90 days | Distribution |

### Accessing Artifacts

1. **Via GitHub UI**:
   - Go to Actions tab
   - Click on workflow run
   - Scroll to "Artifacts" section

2. **Via GitHub API**:
   ```bash
   gh run download <run-id>
   ```

3. **Via Codecov** (coverage):
   - Visit `https://codecov.io/gh/denysvitali/pictures-sync-s3`

## GitHub Security Integration

### Security Tab

All security findings are aggregated in GitHub's Security tab:

1. **Code Scanning Alerts**
   - CodeQL findings
   - gosec SARIF results
   - Trivy SARIF results

2. **Dependabot Alerts**
   - Known vulnerabilities in dependencies
   - Automated PRs for fixes

3. **Secret Scanning** (if enabled on repo)
   - Gitleaks integration
   - Partner patterns

### Security Dashboard

Access: Repository → Security → Overview

Shows:
- Open security alerts
- Recent security updates
- Security policy status
- Dependency graph

## Troubleshooting

### Common Issues

#### 1. Linter Failures

**Problem**: golangci-lint reports errors
**Solution**:
```bash
# Run locally to see details
golangci-lint run -v

# Apply auto-fixes
golangci-lint run --fix
```

#### 2. Test Failures in CI

**Problem**: Tests pass locally but fail in CI
**Solution**:
- Check for race conditions: `go test -race ./...`
- Verify time-dependent tests
- Check filesystem assumptions

#### 3. Vulnerability False Positives

**Problem**: govulncheck reports vulnerability in indirect dependency
**Solution**:
- Update direct dependencies: `go get -u ./...`
- If unfixable, document in security review
- Consider replacing vulnerable dependency

#### 4. Build Reproducibility Fails

**Problem**: Builds produce different checksums
**Solution**:
- Ensure no timestamps in build
- Use `-trimpath` flag
- Avoid external data during build

#### 5. Gitleaks False Positive

**Problem**: Gitleaks detects test credentials as real secrets
**Solution**:
Add to `.gitleaksignore`:
```
test-data/**/*:password
*_test.go:api-key
```

## Best Practices

### For Contributors

1. **Run tests locally before pushing**
   ```bash
   go test -race ./...
   ```

2. **Run linters before committing**
   ```bash
   golangci-lint run
   ```

3. **Check for vulnerabilities**
   ```bash
   govulncheck ./...
   ```

4. **Keep dependencies updated**
   ```bash
   go get -u ./...
   go mod tidy
   ```

### For Maintainers

1. **Review Dependabot PRs promptly**
   - Security updates should be merged quickly
   - Test thoroughly before merging dependency updates

2. **Monitor Security tab**
   - Check weekly for new alerts
   - Triage and assign security issues

3. **Review CodeQL findings**
   - Security findings require investigation
   - Quality findings can be scheduled

4. **Update Go version carefully**
   - Test thoroughly on Raspberry Pi hardware
   - Update CI workflow Go version
   - Update go.mod directive

## Release Process

### Creating a Release

1. **Tag the release**
   ```bash
   git tag -a v1.0.0 -m "Release v1.0.0"
   git push origin v1.0.0
   ```

2. **CI builds release artifacts**
   - Binaries for all platforms
   - Checksums file
   - 90-day retention

3. **Download artifacts**
   ```bash
   gh run download --name release-archives
   ```

4. **Create GitHub release**
   ```bash
   gh release create v1.0.0 \
     --title "Version 1.0.0" \
     --notes "Release notes here" \
     release/*.tar.gz \
     release/checksums.txt
   ```

### Verifying Release

```bash
# Download release
wget https://github.com/denysvitali/pictures-sync-s3/releases/download/v1.0.0/pictures-sync-s3-linux-arm64.tar.gz
wget https://github.com/denysvitali/pictures-sync-s3/releases/download/v1.0.0/checksums.txt

# Verify checksum
sha256sum -c checksums.txt --ignore-missing
```

## Performance Considerations

### Pipeline Optimization

1. **Caching**
   - Go module cache reduces download time
   - Build cache speeds up compilation
   - golangci-lint cache persists between runs

2. **Parallel Execution**
   - Jobs run in parallel when possible
   - Matrix builds run concurrently
   - Independent checks don't wait

3. **Resource Limits**
   - Test timeout: 15 minutes
   - Lint timeout: 10 minutes
   - Total workflow timeout: 60 minutes

### Cost Optimization

For private repositories:

1. **Reduce scheduled runs** if needed
2. **Limit artifact retention** to minimum needed
3. **Use pull_request_target** carefully to avoid unnecessary runs
4. **Cache effectively** to reduce build time

## Security Compliance

### Achieved Standards

This pipeline helps achieve:

1. **OWASP ASVS Level 1**
   - Automated vulnerability scanning
   - Dependency checking
   - Secret detection

2. **CIS Benchmarks**
   - Static code analysis
   - Security configuration
   - Continuous monitoring

3. **NIST Cybersecurity Framework**
   - Identify: Vulnerability scanning
   - Protect: Security gates
   - Detect: Continuous monitoring
   - Respond: Automated alerts

### Audit Trail

All security scans maintain audit trail:
- Scan results stored as artifacts (90 days)
- SARIF uploaded to GitHub Security
- Dependabot alerts tracked
- All changes logged in Git history

## Support and Maintenance

### Updating the Pipeline

To modify the pipeline:

1. Edit workflow files in `.github/workflows/`
2. Test in a branch first
3. Review changes in PR
4. Merge only after successful test run

### Adding New Linters

To add a new linter to golangci-lint:

1. Add to `linters.enable` in `.golangci.yml`
2. Configure in `linters-settings` if needed
3. Run locally: `golangci-lint run`
4. Adjust exclusions if needed
5. Commit and push

### Troubleshooting Help

- **GitHub Discussions**: For questions
- **Issues**: For bugs in pipeline
- **Security Tab**: For security concerns

## References

- [GitHub Actions Documentation](https://docs.github.com/en/actions)
- [golangci-lint Linters](https://golangci-lint.run/usage/linters/)
- [Go Vulnerability Database](https://vuln.go.dev/)
- [gosec Rules](https://github.com/securego/gosec#available-rules)
- [Dependabot Documentation](https://docs.github.com/en/code-security/dependabot)
- [CodeQL for Go](https://codeql.github.com/docs/codeql-language-guides/codeql-for-go/)
