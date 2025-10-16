# Makefile for pictures-sync-s3
# Provides convenient commands for building, testing, and CI/CD operations

.PHONY: help build test lint security clean install-tools docker-build all

# Default target
.DEFAULT_GOAL := help

# Variables
GO := go
GOFMT := gofmt
GOLANGCI_LINT := golangci-lint
GOVULNCHECK := govulncheck
GOSEC := gosec

# Build variables
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_DATE := $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')
LDFLAGS := -s -w -X main.version=$(VERSION) -X main.buildDate=$(BUILD_DATE)

# Binary names
PICTURES_SYNC := pictures-sync
WEBUI := webui

# Output directories
DIST_DIR := dist
COVERAGE_DIR := coverage

# Colors for output
RED := \033[0;31m
GREEN := \033[0;32m
YELLOW := \033[0;33m
BLUE := \033[0;34m
NC := \033[0m # No Color

##@ General

help: ## Display this help message
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make $(BLUE)<target>$(NC)\n"} /^[a-zA-Z_-]+:.*?##/ { printf "  $(BLUE)%-15s$(NC) %s\n", $$1, $$2 } /^##@/ { printf "\n$(YELLOW)%s$(NC)\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

build: ## Build all binaries
	@echo "$(GREEN)Building $(PICTURES_SYNC)...$(NC)"
	@CGO_ENABLED=0 $(GO) build -trimpath -ldflags="$(LDFLAGS)" -o $(PICTURES_SYNC) ./cmd/pictures-sync
	@echo "$(GREEN)Building $(WEBUI)...$(NC)"
	@CGO_ENABLED=0 $(GO) build -trimpath -ldflags="$(LDFLAGS)" -o $(WEBUI) ./cmd/webui
	@echo "$(GREEN)Build complete!$(NC)"

build-all: ## Build for all platforms (linux amd64, arm64, armv7)
	@echo "$(GREEN)Building for all platforms...$(NC)"
	@mkdir -p $(DIST_DIR)
	@echo "Building linux/amd64..."
	@CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -trimpath -ldflags="$(LDFLAGS)" -o $(DIST_DIR)/$(PICTURES_SYNC)-linux-amd64 ./cmd/pictures-sync
	@CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -trimpath -ldflags="$(LDFLAGS)" -o $(DIST_DIR)/$(WEBUI)-linux-amd64 ./cmd/webui
	@echo "Building linux/arm64..."
	@CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GO) build -trimpath -ldflags="$(LDFLAGS)" -o $(DIST_DIR)/$(PICTURES_SYNC)-linux-arm64 ./cmd/pictures-sync
	@CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GO) build -trimpath -ldflags="$(LDFLAGS)" -o $(DIST_DIR)/$(WEBUI)-linux-arm64 ./cmd/webui
	@echo "Building linux/armv7..."
	@CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 $(GO) build -trimpath -ldflags="$(LDFLAGS)" -o $(DIST_DIR)/$(PICTURES_SYNC)-linux-armv7 ./cmd/pictures-sync
	@CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 $(GO) build -trimpath -ldflags="$(LDFLAGS)" -o $(DIST_DIR)/$(WEBUI)-linux-armv7 ./cmd/webui
	@echo "$(GREEN)All builds complete!$(NC)"
	@ls -lh $(DIST_DIR)/

run-webui: ## Run webui locally (port 8080)
	@echo "$(GREEN)Starting webui on http://localhost:8080$(NC)"
	@PORT=8080 ./$(WEBUI)

clean: ## Clean build artifacts
	@echo "$(YELLOW)Cleaning build artifacts...$(NC)"
	@rm -f $(PICTURES_SYNC) $(WEBUI)
	@rm -rf $(DIST_DIR)
	@rm -rf $(COVERAGE_DIR)
	@rm -f coverage.out coverage.html coverage.txt
	@rm -f *.test
	@echo "$(GREEN)Clean complete!$(NC)"

##@ Testing

test: ## Run all tests
	@echo "$(GREEN)Running tests...$(NC)"
	@$(GO) test -v ./...

test-race: ## Run tests with race detector
	@echo "$(GREEN)Running tests with race detector...$(NC)"
	@$(GO) test -race -v ./...

test-coverage: ## Run tests with coverage report
	@echo "$(GREEN)Running tests with coverage...$(NC)"
	@mkdir -p $(COVERAGE_DIR)
	@$(GO) test -race -coverprofile=$(COVERAGE_DIR)/coverage.out -covermode=atomic ./...
	@$(GO) tool cover -html=$(COVERAGE_DIR)/coverage.out -o $(COVERAGE_DIR)/coverage.html
	@$(GO) tool cover -func=$(COVERAGE_DIR)/coverage.out -o $(COVERAGE_DIR)/coverage.txt
	@echo "$(GREEN)Coverage report generated:$(NC)"
	@tail -n 1 $(COVERAGE_DIR)/coverage.txt
	@echo "$(BLUE)HTML report: $(COVERAGE_DIR)/coverage.html$(NC)"

test-short: ## Run short tests only
	@echo "$(GREEN)Running short tests...$(NC)"
	@$(GO) test -short ./...

test-integration: ## Run integration tests
	@echo "$(GREEN)Running integration tests...$(NC)"
	@$(GO) test -v -tags=integration ./...

benchmark: ## Run benchmarks
	@echo "$(GREEN)Running benchmarks...$(NC)"
	@$(GO) test -bench=. -benchmem -benchtime=5s ./...

##@ Code Quality

fmt: ## Format code
	@echo "$(GREEN)Formatting code...$(NC)"
	@$(GOFMT) -w .
	@echo "$(GREEN)Format complete!$(NC)"

fmt-check: ## Check code formatting
	@echo "$(GREEN)Checking code formatting...$(NC)"
	@if [ "$$($(GOFMT) -l . | wc -l)" -gt 0 ]; then \
		echo "$(RED)The following files are not formatted:$(NC)"; \
		$(GOFMT) -l .; \
		exit 1; \
	fi
	@echo "$(GREEN)All files are properly formatted!$(NC)"

lint: ## Run golangci-lint
	@echo "$(GREEN)Running golangci-lint...$(NC)"
	@$(GOLANGCI_LINT) run --timeout=10m

lint-fix: ## Run golangci-lint with auto-fix
	@echo "$(GREEN)Running golangci-lint with auto-fix...$(NC)"
	@$(GOLANGCI_LINT) run --fix --timeout=10m

vet: ## Run go vet
	@echo "$(GREEN)Running go vet...$(NC)"
	@$(GO) vet ./...

staticcheck: ## Run staticcheck
	@echo "$(GREEN)Running staticcheck...$(NC)"
	@staticcheck ./...

tidy: ## Run go mod tidy
	@echo "$(GREEN)Running go mod tidy...$(NC)"
	@$(GO) mod tidy
	@echo "$(GREEN)Dependencies tidied!$(NC)"

verify-tidy: ## Verify go.mod and go.sum are tidy
	@echo "$(GREEN)Verifying go.mod and go.sum...$(NC)"
	@$(GO) mod tidy
	@git diff --exit-code go.mod go.sum || (echo "$(RED)go.mod or go.sum is not tidy$(NC)" && exit 1)
	@echo "$(GREEN)go.mod and go.sum are tidy!$(NC)"

##@ Security

security: govulncheck gosec gitleaks ## Run all security checks

govulncheck: ## Run Go vulnerability check
	@echo "$(GREEN)Running govulncheck...$(NC)"
	@$(GOVULNCHECK) ./...

gosec: ## Run gosec security scanner
	@echo "$(GREEN)Running gosec...$(NC)"
	@$(GOSEC) -fmt=json -out=gosec-report.json -exclude-dir=test-data ./... || true
	@$(GOSEC) -exclude-dir=test-data ./...

gitleaks: ## Run gitleaks secret scanner
	@echo "$(GREEN)Running gitleaks...$(NC)"
	@docker run -v $(PWD):/path zricethezav/gitleaks:latest detect --source="/path" -v

trivy: ## Run Trivy vulnerability scanner
	@echo "$(GREEN)Running Trivy...$(NC)"
	@docker run --rm -v $(PWD):/workspace aquasec/trivy:latest fs --severity CRITICAL,HIGH,MEDIUM /workspace

nancy: ## Run Nancy dependency scanner
	@echo "$(GREEN)Running Nancy...$(NC)"
	@$(GO) list -json -deps ./... | docker run --rm -i sonatypecommunity/nancy:latest sleuth

##@ CI/CD

ci: fmt-check vet lint test-race ## Run all CI checks locally

ci-quick: fmt-check test-short ## Run quick CI checks

security-scan: govulncheck gosec ## Run security scans (no Docker required)

pre-commit: fmt lint test-short ## Run before committing

pre-push: ci security-scan ## Run before pushing

##@ Installation

install-tools: ## Install development tools
	@echo "$(GREEN)Installing development tools...$(NC)"
	@echo "Installing golangci-lint..."
	@$(GO) install github.com/golangci/golangci-lint/cmd/golangci-lint@v1.62.2
	@echo "Installing govulncheck..."
	@$(GO) install golang.org/x/vuln/cmd/govulncheck@latest
	@echo "Installing gosec..."
	@$(GO) install github.com/securego/gosec/v2/cmd/gosec@latest
	@echo "Installing staticcheck..."
	@$(GO) install honnef.co/go/tools/cmd/staticcheck@latest
	@echo "Installing go-licenses..."
	@$(GO) install github.com/google/go-licenses@latest
	@echo "$(GREEN)All tools installed!$(NC)"

verify-tools: ## Verify development tools are installed
	@echo "$(GREEN)Verifying development tools...$(NC)"
	@command -v $(GOLANGCI_LINT) >/dev/null 2>&1 || (echo "$(RED)golangci-lint not found$(NC)" && exit 1)
	@command -v $(GOVULNCHECK) >/dev/null 2>&1 || (echo "$(RED)govulncheck not found$(NC)" && exit 1)
	@command -v $(GOSEC) >/dev/null 2>&1 || (echo "$(RED)gosec not found$(NC)" && exit 1)
	@echo "$(GREEN)All tools are installed!$(NC)"

##@ Gokrazy

gokrazy-setup: ## Set up Gokrazy instance
	@echo "$(GREEN)Setting up Gokrazy instance...$(NC)"
	@./setup-gokrazy.sh

gokrazy-update: ## Deploy over-the-air update to Gokrazy
	@echo "$(GREEN)Deploying OTA update...$(NC)"
	@gok -i photo-backup update

gokrazy-edit: ## Edit Gokrazy instance configuration
	@gok -i photo-backup edit

##@ Release

release-check: ## Check if ready for release
	@echo "$(GREEN)Checking release readiness...$(NC)"
	@$(MAKE) verify-tidy
	@$(MAKE) test-coverage
	@$(MAKE) lint
	@$(MAKE) security-scan
	@echo "$(GREEN)Ready for release!$(NC)"

release-build: ## Build release artifacts
	@echo "$(GREEN)Building release artifacts...$(NC)"
	@$(MAKE) build-all
	@cd $(DIST_DIR) && sha256sum * > checksums.txt
	@echo "$(GREEN)Release artifacts ready in $(DIST_DIR)/$(NC)"

##@ Docker (for tools that require it)

docker-trivy: ## Run Trivy in Docker
	@docker run --rm -v $(PWD):/workspace aquasec/trivy:latest fs --severity CRITICAL,HIGH,MEDIUM /workspace

docker-gitleaks: ## Run Gitleaks in Docker
	@docker run -v $(PWD):/path zricethezav/gitleaks:latest detect --source="/path" -v

docker-nancy: ## Run Nancy in Docker
	@$(GO) list -json -deps ./... | docker run --rm -i sonatypecommunity/nancy:latest sleuth

##@ Information

info: ## Display project information
	@echo "$(BLUE)Project Information:$(NC)"
	@echo "  Version: $(VERSION)"
	@echo "  Build Date: $(BUILD_DATE)"
	@echo "  Go Version: $$($(GO) version)"
	@echo ""
	@echo "$(BLUE)Binaries:$(NC)"
	@echo "  pictures-sync: $(PICTURES_SYNC)"
	@echo "  webui: $(WEBUI)"
	@echo ""
	@echo "$(BLUE)Directories:$(NC)"
	@echo "  Distribution: $(DIST_DIR)"
	@echo "  Coverage: $(COVERAGE_DIR)"

list-targets: ## List all make targets
	@grep '^[a-zA-Z_-]*:' Makefile | sed 's/:.*//' | sort

##@ All-in-one

all: clean install-tools build test lint security ## Build and test everything

.PHONY: all
