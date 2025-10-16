# CI/CD Quick Start Guide

Get started with the CI/CD pipeline in 5 minutes.

## Prerequisites

- GitHub repository (this is already set up)
- Go 1.25+ installed locally
- Git configured

## Step 1: Enable GitHub Actions

GitHub Actions should be automatically enabled. Verify by:

1. Go to your repository on GitHub
2. Click the "Actions" tab
3. If disabled, click "Enable GitHub Actions"

## Step 2: Configure Secrets (Optional)

For enhanced features, add these secrets:

1. Go to Settings → Secrets and variables → Actions
2. Add the following secrets:

### Optional Secrets

- `CODECOV_TOKEN` - For code coverage reporting
  - Sign up at https://codecov.io
  - Add your repository
  - Copy the token

### Not Required Initially

The pipeline will work without these secrets. They enhance functionality but aren't blocking.

## Step 3: Install Local Tools

Run this command to install all development tools:

```bash
make install-tools
```

Or manually:

```bash
go install github.com/golangci/golangci-lint/cmd/golangci-lint@v1.62.2
go install golang.org/x/vuln/cmd/govulncheck@latest
go install github.com/securego/gosec/v2/cmd/gosec@latest
```

## Step 4: Run Checks Locally

Before pushing code, run these checks:

```bash
# Quick checks (runs in ~30 seconds)
make ci-quick

# Full CI suite (runs in ~5 minutes)
make ci

# Security scans (runs in ~2 minutes)
make security-scan
```

## Step 5: Make Your First Commit

```bash
# Format code
make fmt

# Run tests
make test

# Check everything
make pre-commit

# Commit and push
git add .
git commit -m "feat: add CI/CD pipeline"
git push
```

## Step 6: Monitor Pipeline

1. Go to Actions tab on GitHub
2. Watch the pipeline run
3. All checks should pass ✓

## Common Workflows

### Before Committing

```bash
make pre-commit
```

This runs:
- Code formatting
- Linting
- Quick tests

### Before Pushing

```bash
make pre-push
```

This runs:
- All CI checks
- Security scans

### Creating a Pull Request

1. Push your branch
2. Create PR on GitHub
3. Pipeline automatically runs
4. Wait for all checks to pass
5. Request review

### Fixing Failed Checks

#### Formatting Issues

```bash
make fmt
git add .
git commit -m "style: format code"
```

#### Lint Issues

```bash
# Auto-fix what's possible
make lint-fix

# Check remaining issues
make lint
```

#### Test Failures

```bash
# Run tests with verbose output
go test -v ./...

# Run specific package
go test -v ./pkg/syncmanager
```

#### Security Issues

```bash
# Check vulnerabilities
make govulncheck

# Update dependencies
go get -u ./...
go mod tidy
```

## Understanding Check Status

### CI Pipeline Status

| Check | What It Does | Fix If Failed |
|-------|--------------|---------------|
| Test and Coverage | Runs all tests | Fix failing tests |
| Cross-Platform Build | Builds for all platforms | Fix build errors |
| Code Quality Checks | Linting and formatting | Run `make lint-fix` |
| Dependency Analysis | Checks go.mod/go.sum | Run `make tidy` |

### Security Pipeline Status

| Check | What It Does | Fix If Failed |
|-------|--------------|---------------|
| govulncheck | Go vulnerabilities | Update dependencies |
| gosec | Security SAST | Fix security issues |
| gitleaks | Secret detection | Remove secrets |
| trivy | Vulnerability scan | Update dependencies |
| CodeQL | Code analysis | Fix issues in Security tab |

### What Blocks Merging?

These **will block** your PR:

- Test failures
- Build failures
- Critical lint errors
- Known vulnerabilities (govulncheck)
- Leaked secrets (gitleaks)
- Formatting issues

These **won't block** (but should be addressed):

- gosec warnings (may be false positives)
- Trivy findings (informational)
- TODO comments
- Large PR size warnings

## Quick Commands Reference

```bash
# Build
make build              # Build both binaries
make build-all          # Build for all platforms

# Test
make test              # Run all tests
make test-race         # Run with race detector
make test-coverage     # Generate coverage report

# Quality
make fmt               # Format code
make lint              # Run linter
make vet               # Run go vet

# Security
make security          # Run all security checks
make govulncheck       # Check vulnerabilities
make gosec             # Security scanner

# CI/CD
make ci                # Run full CI locally
make ci-quick          # Quick CI checks
make pre-commit        # Before committing
make pre-push          # Before pushing

# Cleanup
make clean             # Remove build artifacts

# Help
make help              # Show all targets
```

## Troubleshooting

### "golangci-lint not found"

```bash
make install-tools
```

### "Tests fail in CI but pass locally"

```bash
# Run with race detector
make test-race

# Check for timing issues
go test -count=10 ./...
```

### "go.mod is not tidy"

```bash
make tidy
git add go.mod go.sum
git commit -m "chore: tidy dependencies"
```

### "Formatting check failed"

```bash
make fmt
git add .
git commit -m "style: format code"
```

### "Security vulnerability detected"

```bash
# Check what's vulnerable
make govulncheck

# Update dependencies
go get -u ./...
go mod tidy

# Test everything still works
make test

# Commit update
git add go.mod go.sum
git commit -m "deps: update dependencies to fix vulnerabilities"
```

### "Secret detected by gitleaks"

If you accidentally committed a secret:

1. **Rotate the secret immediately** (change password, regenerate API key)
2. Remove from Git history:
   ```bash
   git filter-branch --force --index-filter \
     "git rm --cached --ignore-unmatch path/to/file" \
     --prune-empty --tag-name-filter cat -- --all
   ```
3. Force push (coordinate with team first!)
   ```bash
   git push origin --force --all
   ```

## Integration with IDEs

### VS Code

Install these extensions:
- Go (official)
- golangci-lint
- CodeQL

Add to `.vscode/settings.json`:
```json
{
  "go.lintTool": "golangci-lint",
  "go.lintOnSave": "package",
  "go.formatTool": "gofmt"
}
```

### GoLand

1. Go to Settings → Tools → File Watchers
2. Add golangci-lint watcher
3. Enable "Format on save"

## Next Steps

1. **Review the full documentation**: See `CI_CD.md`
2. **Set up branch protection**: Require status checks
3. **Configure Dependabot alerts**: Enable in Settings
4. **Review Security tab**: Check for any issues
5. **Customize pipeline**: Adjust workflows for your needs

## Getting Help

- **Documentation**: See `CI_CD.md` for comprehensive docs
- **Security**: See `.github/SECURITY.md` for security policy
- **Issues**: Open an issue on GitHub
- **Make targets**: Run `make help` for all commands

## Success Criteria

You're ready when:

- [ ] All checks pass on your branch
- [ ] Local `make ci` passes
- [ ] Security scans show no critical issues
- [ ] Code is formatted and linted
- [ ] Tests have good coverage
- [ ] PR is ready for review

---

**Pro Tip**: Set up a git hook to run checks automatically:

```bash
# .git/hooks/pre-push
#!/bin/bash
make pre-push
```

```bash
chmod +x .git/hooks/pre-push
```
