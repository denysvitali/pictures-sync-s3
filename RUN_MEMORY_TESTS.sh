#!/bin/bash

# Memory Safety Test Runner for pictures-sync-s3
# This script runs all memory corruption and unsafe operations tests
# with various Go testing flags for comprehensive analysis

set -e

echo "======================================"
echo "Memory Safety Test Suite"
echo "pictures-sync-s3"
echo "======================================"
echo ""

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Test counter
PASSED=0
FAILED=0
SKIPPED=0

# Function to run a test and track results
run_test() {
    local test_name=$1
    local test_cmd=$2

    echo -e "${YELLOW}Running: ${test_name}${NC}"
    echo "Command: $test_cmd"

    if eval $test_cmd > /tmp/test_output.log 2>&1; then
        echo -e "${GREEN}✓ PASSED${NC}"
        PASSED=$((PASSED + 1))
    else
        echo -e "${RED}✗ FAILED${NC}"
        echo "Error output:"
        cat /tmp/test_output.log | head -20
        FAILED=$((FAILED + 1))
    fi
    echo ""
}

echo "======================================"
echo "1. sdmonitor Package Tests"
echo "======================================"
echo ""

run_test "Format Bytes Integer Overflow" \
    "go test -v ./pkg/sdmonitor -run TestFormatBytesIntegerOverflow"

run_test "Concurrent Card ID Generation" \
    "go test -v ./pkg/sdmonitor -run TestConcurrentCardIDGeneration"

run_test "Channel Operations After Close" \
    "go test -v ./pkg/sdmonitor -run TestChannelOperationsAfterClose"

run_test "Unsafe String Operations" \
    "go test -v ./pkg/sdmonitor -run TestUnsafeStringOperations"

run_test "String Index Out of Bounds" \
    "go test -v ./pkg/sdmonitor -run TestStringIndexOutOfBounds"

echo "======================================"
echo "2. syncmanager Package Tests"
echo "======================================"
echo ""

run_test "Card ID Path Traversal (CRITICAL)" \
    "go test -v ./pkg/syncmanager -run TestCardIDPathTraversal"

run_test "Retryable Error Detection" \
    "go test -v ./pkg/syncmanager -run TestRetryableErrorDetection"

run_test "Format Duration Integer Overflow" \
    "go test -v ./pkg/syncmanager -run TestFormatDurationIntegerOverflow"

run_test "String Contains Null Bytes" \
    "go test -v ./pkg/syncmanager -run TestStringContainsNullBytes"

run_test "ETA Calculation Negative Remaining" \
    "go test -v ./pkg/syncmanager -run TestETACalculationNegativeRemaining"

run_test "Progress Percentage Overflow" \
    "go test -v ./pkg/syncmanager -run TestProgressPercentageOverflow"

echo "======================================"
echo "3. Race Detection Tests"
echo "======================================"
echo ""

echo "Running tests with -race flag to detect data races..."
echo "This may take longer..."
echo ""

run_test "sdmonitor Race Detection" \
    "go test -race -v ./pkg/sdmonitor -run TestConcurrent -timeout=30s"

run_test "syncmanager Race Detection" \
    "go test -race -v ./pkg/syncmanager -run TestConcurrent -timeout=30s"

echo "======================================"
echo "4. Individual Critical Tests"
echo "======================================"
echo ""

# Test specific critical vulnerabilities
echo "Testing specific critical vulnerabilities..."
echo ""

# Path traversal variants
echo "4.1 Path Traversal Attack Vectors"
go test -v ./pkg/syncmanager -run "TestCardIDPathTraversal/PathTraversalDotDot" || true
go test -v ./pkg/syncmanager -run "TestCardIDPathTraversal/PathTraversalSlash" || true
go test -v ./pkg/syncmanager -run "TestCardIDPathTraversal/NullByteInjection" || true

# Integer overflow scenarios
echo ""
echo "4.2 Integer Overflow Scenarios"
go test -v ./pkg/sdmonitor -run "TestFormatBytesIntegerOverflow/MaxInt64" || true
go test -v ./pkg/sdmonitor -run "TestFormatBytesIntegerOverflow/NegativeValue" || true

# Concurrency issues
echo ""
echo "4.3 Concurrency Safety"
go test -v ./pkg/sdmonitor -run "TestConcurrentCardIDGeneration/ParallelIDGeneration" || true
go test -v ./pkg/sdmonitor -run "TestChannelOperationsAfterClose/EventChannelAfterStop" || true

echo ""
echo "======================================"
echo "5. Summary"
echo "======================================"
echo ""

TOTAL=$((PASSED + FAILED + SKIPPED))

echo "Total Tests Run: $TOTAL"
echo -e "${GREEN}Passed: $PASSED${NC}"
if [ $FAILED -gt 0 ]; then
    echo -e "${RED}Failed: $FAILED${NC}"
else
    echo -e "${GREEN}Failed: 0${NC}"
fi
if [ $SKIPPED -gt 0 ]; then
    echo -e "${YELLOW}Skipped: $SKIPPED${NC}"
fi

echo ""
echo "======================================"
echo "6. Advanced Testing Commands"
echo "======================================"
echo ""

echo "For more detailed analysis, run:"
echo ""
echo "# Memory profiling:"
echo "  go test -memprofile=mem.prof -v ./pkg/state"
echo "  go tool pprof mem.prof"
echo ""
echo "# CPU profiling:"
echo "  go test -cpuprofile=cpu.prof -v ./pkg/syncmanager"
echo "  go tool pprof cpu.prof"
echo ""
echo "# Execution tracing:"
echo "  go test -trace=trace.out -v ./pkg/sdmonitor"
echo "  go tool trace trace.out"
echo ""
echo "# Coverage report:"
echo "  go test -coverprofile=coverage.out ./..."
echo "  go tool cover -html=coverage.out"
echo ""
echo "# Benchmark tests:"
echo "  go test -bench=. -benchmem ./..."
echo ""

echo "======================================"
echo "7. Vulnerability Summary"
echo "======================================"
echo ""

echo "Critical vulnerabilities tested:"
echo "  ✓ Path traversal in card ID"
echo "  ✓ Integer overflow in size calculations"
echo "  ✓ Integer overflow in file counting"
echo "  ✓ Concurrent access to shared state"
echo "  ✓ Channel operations after close"
echo "  ✓ Division by zero in calculations"
echo "  ✓ Null byte injection"
echo "  ✓ Race conditions in goroutines"
echo ""

echo "See MEMORY_SAFETY_ANALYSIS.md for detailed vulnerability reports"
echo "See MEMORY_SAFETY_TEST_SUMMARY.md for test documentation"
echo ""

# Exit with error if any tests failed
if [ $FAILED -gt 0 ]; then
    echo -e "${RED}Some tests failed. Review the output above.${NC}"
    exit 1
else
    echo -e "${GREEN}All tests passed!${NC}"
    exit 0
fi
