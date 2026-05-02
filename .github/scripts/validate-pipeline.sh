#!/bin/bash
# Pipeline Validation Script
# Validates that all CI/CD components are properly configured

set -e

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Counters
CHECKS=0
PASSED=0
FAILED=0
WARNINGS=0

# Check function
check() {
    local name="$1"
    local command="$2"
    local level="${3:-error}"  # error or warning

    CHECKS=$((CHECKS + 1))
    echo -n "Checking $name... "

    if eval "$command" > /dev/null 2>&1; then
        echo -e "${GREEN}✓${NC}"
        PASSED=$((PASSED + 1))
        return 0
    else
        if [ "$level" = "warning" ]; then
            echo -e "${YELLOW}⚠${NC}"
            WARNINGS=$((WARNINGS + 1))
            return 0
        else
            echo -e "${RED}✗${NC}"
            FAILED=$((FAILED + 1))
            return 1
        fi
    fi
}

# File exists check
check_file() {
    local name="$1"
    local file="$2"
    local required="${3:-true}"

    if [ "$required" = "true" ]; then
        check "$name exists" "test -f '$file'"
    else
        check "$name exists" "test -f '$file'" warning
    fi
}

# Directory check
check_dir() {
    local name="$1"
    local dir="$2"
    check "$name exists" "test -d '$dir'"
}

echo -e "${BLUE}=== CI/CD Pipeline Validation ===${NC}\n"

# Check directory structure
echo -e "${YELLOW}Directory Structure:${NC}"
check_dir ".github directory" ".github"
check_dir "workflows directory" ".github/workflows"
check_dir "scripts directory" ".github/scripts"

echo ""

# Check workflow files
echo -e "${YELLOW}Workflow Files:${NC}"
check_file "CI workflow" ".github/workflows/ci.yml"
check_file "Security workflow" ".github/workflows/security.yml"
check_file "CodeQL workflow" ".github/workflows/codeql.yml"
check_file "PR checks workflow" ".github/workflows/pr-checks.yml"

echo ""

# Check configuration files
echo -e "${YELLOW}Configuration Files:${NC}"
check_file "golangci-lint config" ".golangci.yml"
check_file "Renovate config" "renovate.json"
check_file "Gitleaks ignore" ".gitleaksignore"
check_file "Makefile" "Makefile"

echo ""

# Check documentation
echo -e "${YELLOW}Documentation:${NC}"
check_file "CI/CD documentation" "CI_CD.md"
check_file "Security policy" ".github/SECURITY.md"
check_file "Quick start guide" ".github/QUICKSTART_CI.md"
check_file "CI/CD summary" ".github/CI_CD_SUMMARY.md"
check_file "Badges reference" ".github/BADGES.md"

echo ""

# Validate workflow syntax
echo -e "${YELLOW}Workflow Syntax:${NC}"
for workflow in .github/workflows/*.yml; do
    name=$(basename "$workflow" .yml)
    check "$name syntax" "python3 -c 'import yaml; yaml.safe_load(open(\"$workflow\"))'" warning
done

echo ""

# Check Go files
echo -e "${YELLOW}Go Environment:${NC}"
check "go.mod exists" "test -f go.mod"
check "go.sum exists" "test -f go.sum"
check "Go module name" "grep -q 'module github.com/denysvitali/pictures-sync-s3' go.mod"

echo ""

# Check for required Go packages
echo -e "${YELLOW}Main Packages:${NC}"
check "pictures-sync package" "test -d cmd/pictures-sync"
check "webui package" "test -d cmd/webui"
check "pictures-sync main.go" "test -f cmd/pictures-sync/main.go"
check "webui main.go" "test -f cmd/webui/main.go"

echo ""

# Validate golangci-lint config
echo -e "${YELLOW}Linter Configuration:${NC}"
check "golangci.yml syntax" "python3 -c 'import yaml; yaml.safe_load(open(\".golangci.yml\"))'" warning
check "golangci.yml has linters" "grep -q 'linters:' .golangci.yml"
check "golangci.yml has enabled linters" "grep -q 'enable:' .golangci.yml"

echo ""

# Check gitignore
echo -e "${YELLOW}Git Configuration:${NC}"
check ".gitignore exists" "test -f .gitignore"
check "gitignore has coverage" "grep -q 'coverage' .gitignore"
check "gitignore has dist" "grep -q 'dist' .gitignore"
check "Git repository" "test -d .git"

echo ""

# Check for common tools
echo -e "${YELLOW}Development Tools (optional):${NC}"
check "go installed" "command -v go" warning
check "golangci-lint installed" "command -v golangci-lint" warning
check "govulncheck installed" "command -v govulncheck" warning
check "gosec installed" "command -v gosec" warning
check "make installed" "command -v make" warning

echo ""

# Validate Makefile
echo -e "${YELLOW}Makefile Validation:${NC}"
check "Makefile has help" "grep -q '^help:' Makefile"
check "Makefile has build" "grep -q '^build:' Makefile"
check "Makefile has test" "grep -q '^test:' Makefile"
check "Makefile has lint" "grep -q '^lint:' Makefile"
check "Makefile has security" "grep -q '^security:' Makefile"

echo ""

# Check workflow trigger configuration
echo -e "${YELLOW}Workflow Triggers:${NC}"
check "CI on push" "grep -q 'on:' .github/workflows/ci.yml && grep -A5 'on:' .github/workflows/ci.yml | grep -q 'push'" warning
check "CI on pull_request" "grep -q 'pull_request' .github/workflows/ci.yml" warning
check "Security on schedule" "grep -q 'schedule' .github/workflows/security.yml" warning

echo ""

# Check security configuration
echo -e "${YELLOW}Security Configuration:${NC}"
check "Security workflow has govulncheck" "grep -q 'govulncheck' .github/workflows/security.yml"
check "Security workflow has gosec" "grep -q 'gosec' .github/workflows/security.yml"
check "Security workflow has gitleaks" "grep -q 'gitleaks' .github/workflows/security.yml"
check "Security workflow has trivy" "grep -q 'trivy' .github/workflows/security.yml"
check "Security workflow has CodeQL" "grep -q 'codeql' .github/workflows/security.yml"

echo ""

# Check for best practices
echo -e "${YELLOW}Best Practices:${NC}"
check "CI has timeout" "grep -q 'timeout-minutes:' .github/workflows/ci.yml" warning
check "Workflows use cache" "grep -q 'cache:' .github/workflows/ci.yml" warning
check "Security has SARIF upload" "grep -q 'upload-sarif' .github/workflows/security.yml" warning
check "Renovate config exists" "test -f renovate.json" warning

echo ""

# Summary
echo -e "${BLUE}=== Validation Summary ===${NC}"
echo -e "Total checks: $CHECKS"
echo -e "${GREEN}Passed: $PASSED${NC}"
if [ $WARNINGS -gt 0 ]; then
    echo -e "${YELLOW}Warnings: $WARNINGS${NC}"
fi
if [ $FAILED -gt 0 ]; then
    echo -e "${RED}Failed: $FAILED${NC}"
fi

echo ""

# Final result
if [ $FAILED -eq 0 ]; then
    echo -e "${GREEN}✓ Pipeline validation successful!${NC}"
    echo ""
    echo "Next steps:"
    echo "  1. Review the documentation: cat CI_CD.md"
    echo "  2. Install development tools: make install-tools"
    echo "  3. Run local CI checks: make ci"
    echo "  4. Push to GitHub to trigger workflows"
    exit 0
else
    echo -e "${RED}✗ Pipeline validation failed!${NC}"
    echo ""
    echo "Please fix the failed checks above before proceeding."
    exit 1
fi
