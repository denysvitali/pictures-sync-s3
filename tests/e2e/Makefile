# Makefile for E2E Tests

.PHONY: all test test-short test-race test-integration test-coverage clean help

# Default target
all: test

# Run all E2E tests
test:
	@echo "Running all E2E tests..."
	go test -v -timeout 5m ./...

# Run tests excluding long-running ones
test-short:
	@echo "Running short E2E tests..."
	go test -v -short -timeout 2m ./...

# Run tests with race detection
test-race:
	@echo "Running E2E tests with race detection..."
	go test -v -race -timeout 10m ./...

# Run integration tests (requires E2E_INTEGRATION=1)
test-integration:
	@echo "Running integration tests..."
	E2E_INTEGRATION=1 go test -v -timeout 15m ./...

# Run specific test category
test-sync:
	@echo "Running sync workflow tests..."
	go test -v -run TestE2E.*Sync -timeout 5m

test-websocket:
	@echo "Running WebSocket tests..."
	go test -v -run TestE2EWebSocket -timeout 5m

test-hardware:
	@echo "Running hardware mock tests..."
	go test -v -run TestE2EHardware -timeout 5m

test-recovery:
	@echo "Running error recovery tests..."
	go test -v -run TestE2ERecovery -timeout 5m

test-config:
	@echo "Running configuration persistence tests..."
	go test -v -run TestE2E.*Persistence -timeout 5m

test-service:
	@echo "Running service integration tests..."
	go test -v -run TestE2EService -timeout 5m

# Generate coverage report
test-coverage:
	@echo "Running tests with coverage..."
	go test -v -coverprofile=coverage.out -timeout 5m ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

# Run specific test by name
test-one:
ifndef TEST
	@echo "Usage: make test-one TEST=TestE2EFullSyncWorkflow"
	@exit 1
endif
	@echo "Running test: $(TEST)"
	go test -v -run $(TEST) -timeout 5m

# Verbose test run with detailed output
test-verbose:
	@echo "Running tests with maximum verbosity..."
	go test -v -race -coverprofile=coverage.out -timeout 10m ./... 2>&1 | tee test.log

# Quick smoke test
test-smoke:
	@echo "Running smoke tests..."
	go test -v -run TestE2EFullSyncWorkflow -timeout 2m

# Benchmark tests
benchmark:
	@echo "Running benchmarks..."
	go test -bench=. -benchmem -timeout 10m ./...

# Check test compilation
check:
	@echo "Checking test compilation..."
	go test -c -o /dev/null ./...

# Format test code
fmt:
	@echo "Formatting test code..."
	gofmt -w -s *.go

# Lint tests
lint:
	@echo "Linting test code..."
	golangci-lint run --disable-all \
		--enable=errcheck \
		--enable=gosimple \
		--enable=govet \
		--enable=ineffassign \
		--enable=staticcheck \
		--enable=unused \
		./...

# Clean test artifacts
clean:
	@echo "Cleaning test artifacts..."
	rm -f coverage.out coverage.html test.log
	rm -rf /tmp/e2e-test-* /tmp/webui-test-*
	@echo "Clean complete"

# Help target
help:
	@echo "E2E Test Suite Makefile"
	@echo ""
	@echo "Available targets:"
	@echo "  make test              - Run all E2E tests"
	@echo "  make test-short        - Run short tests (skip long-running)"
	@echo "  make test-race         - Run tests with race detection"
	@echo "  make test-integration  - Run integration tests (requires E2E_INTEGRATION=1)"
	@echo "  make test-coverage     - Run tests with coverage report"
	@echo ""
	@echo "Test categories:"
	@echo "  make test-sync         - Sync workflow tests"
	@echo "  make test-websocket    - WebSocket communication tests"
	@echo "  make test-hardware     - Hardware mock tests"
	@echo "  make test-recovery     - Error recovery tests"
	@echo "  make test-config       - Configuration persistence tests"
	@echo "  make test-service      - Service integration tests"
	@echo ""
	@echo "Utilities:"
	@echo "  make test-one TEST=... - Run specific test by name"
	@echo "  make test-verbose      - Run with maximum verbosity"
	@echo "  make test-smoke        - Quick smoke test"
	@echo "  make benchmark         - Run benchmark tests"
	@echo "  make check             - Check test compilation"
	@echo "  make fmt               - Format test code"
	@echo "  make lint              - Lint test code"
	@echo "  make clean             - Clean test artifacts"
	@echo "  make help              - Show this help message"
	@echo ""
	@echo "Examples:"
	@echo "  make test-one TEST=TestE2EFullSyncWorkflow"
	@echo "  make test-coverage"
	@echo "  E2E_INTEGRATION=1 make test-integration"

# CI/CD friendly targets
ci-test: check test-race test-coverage
	@echo "CI tests complete"

ci-integration: check test-integration
	@echo "CI integration tests complete"

# Watch mode (requires entr or similar)
watch:
	@echo "Watching for changes..."
	find . -name '*.go' | entr -c make test-short
