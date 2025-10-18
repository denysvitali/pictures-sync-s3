# Testing Guide for Pictures-Sync-S3

This guide provides comprehensive information about testing patterns, best practices, and helper functions used in this project.

## Table of Contents

1. [Testing Philosophy](#testing-philosophy)
2. [Test Organization](#test-organization)
3. [Helper Functions](#helper-functions)
4. [Common Patterns](#common-patterns)
5. [Running Tests](#running-tests)
6. [Benchmarking](#benchmarking)
7. [Coverage Analysis](#coverage-analysis)

## Testing Philosophy

### Core Principles

1. **Test Behavior, Not Implementation**: Focus on what the code does, not how it does it
2. **Fast and Deterministic**: Tests should run quickly and produce consistent results
3. **Isolated and Independent**: Each test should be able to run independently
4. **Readable and Maintainable**: Tests serve as documentation and should be clear
5. **Catch Real Bugs**: Focus on edge cases and error conditions that users might encounter

### What to Test

- **Happy paths**: Verify normal operation works correctly
- **Error conditions**: Test how code handles invalid input and failures
- **Edge cases**: Boundary conditions, empty inputs, extreme values
- **Concurrency**: Race conditions and thread safety where applicable
- **Security**: Path traversal, injection attacks, authentication bypass
- **Performance**: Benchmarks for critical paths

## Test Organization

### File Structure

```
pkg/
├── package/
│   ├── file.go              # Implementation
│   ├── file_test.go         # Main tests
│   ├── benchmarks_test.go   # Performance benchmarks (optional)
│   └── security_test.go     # Security tests (if needed)
```

### Test Naming Conventions

```go
// Function tests
func TestFunctionName_Scenario(t *testing.T)

// Examples:
func TestHandleStatus_GET(t *testing.T)
func TestHandleStatus_WrongMethod(t *testing.T)
func TestValidatePath_Traversal(t *testing.T)

// Benchmarks
func BenchmarkFunctionName(b *testing.B)

// Examples:
func BenchmarkProgressParsing(b *testing.B)
func BenchmarkCardIDValidation(b *testing.B)
```

## Helper Functions

### Test Environment Setup

```go
// setupTestEnvironment creates a complete test environment
func setupTestEnvironment(t *testing.T) (*Handler, *state.Manager, string) {
    t.Helper()

    tempDir := t.TempDir()

    // Set up temp directory as PERM_DIR
    oldPerm := os.Getenv("PERM_DIR")
    os.Setenv("PERM_DIR", tempDir)
    t.Cleanup(func() {
        if oldPerm == "" {
            os.Unsetenv("PERM_DIR")
        } else {
            os.Setenv("PERM_DIR", oldPerm)
        }
    })

    // Create dependencies...
    stateMgr, err := state.NewManager()
    if err != nil {
        t.Fatalf("Failed to create state manager: %v", err)
    }

    return handler, stateMgr, tempDir
}
```

### Mock Implementations

```go
// mockSyncManager implements syncmanager.Manager interface for testing
type mockSyncManager struct {
    isRunning    bool
    cancelCalled bool
    syncError    error
}

func (m *mockSyncManager) IsRunning() bool {
    return m.isRunning
}

func (m *mockSyncManager) Cancel() error {
    m.cancelCalled = true
    return nil
}
```

### HTTP Testing

```go
// Test HTTP handlers
func TestHandleStatus(t *testing.T) {
    req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
    w := httptest.NewRecorder()

    handler(w, req)

    if w.Code != http.StatusOK {
        t.Errorf("Expected status 200, got %d", w.Code)
    }

    var response map[string]interface{}
    if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
        t.Fatalf("Failed to decode response: %v", err)
    }
}
```

### File System Testing

```go
func TestFileOperation(t *testing.T) {
    // Use t.TempDir() for automatic cleanup
    tempDir := t.TempDir()

    testFile := filepath.Join(tempDir, "test.txt")
    if err := os.WriteFile(testFile, []byte("data"), 0644); err != nil {
        t.Fatalf("Setup failed: %v", err)
    }

    // Test file operations...
}
```

## Common Patterns

### Table-Driven Tests

```go
func TestValidation(t *testing.T) {
    tests := []struct {
        name      string
        input     string
        expectErr bool
    }{
        {"valid input", "card-123", false},
        {"invalid format", "../etc/passwd", true},
        {"empty string", "", true},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            err := Validate(tt.input)
            if (err != nil) != tt.expectErr {
                t.Errorf("Validate(%s) error = %v, expectErr = %v",
                    tt.input, err, tt.expectErr)
            }
        })
    }
}
```

### Concurrent Testing

```go
func TestConcurrentAccess(t *testing.T) {
    manager := NewManager()

    const goroutines = 10
    done := make(chan bool, goroutines)

    for i := 0; i < goroutines; i++ {
        go func(id int) {
            defer func() { done <- true }()
            manager.DoSomething(id)
        }(i)
    }

    for i := 0; i < goroutines; i++ {
        <-done
    }
}
```

### Error Testing

```go
func TestErrorHandling(t *testing.T) {
    // Test with nil input
    err := Function(nil)
    if err == nil {
        t.Error("Expected error for nil input")
    }

    // Test error message
    if !strings.Contains(err.Error(), "expected text") {
        t.Errorf("Error message doesn't contain expected text: %v", err)
    }
}
```

### Security Testing

```go
func TestPathTraversal(t *testing.T) {
    testCases := []string{
        "../../../etc/passwd",
        "/etc/passwd",
        "DCIM/../../root/.ssh/id_rsa",
    }

    for _, path := range testCases {
        t.Run(path, func(t *testing.T) {
            err := ValidatePath(path)
            if err == nil {
                t.Errorf("Path traversal not detected for: %s", path)
            }
        })
    }
}
```

## Running Tests

### Run All Tests

```bash
# Run all tests in the project
go test ./...

# Run tests with verbose output
go test -v ./...

# Run tests with race detector (important for concurrent code)
go test -race ./...
```

### Run Specific Tests

```bash
# Run tests in a specific package
go test ./pkg/state

# Run a specific test function
go test -run TestFunctionName ./pkg/package

# Run tests matching a pattern
go test -run "TestHandle.*" ./pkg/handlers
```

### Test Coverage

```bash
# Generate coverage report
go test -cover ./...

# Generate detailed coverage profile
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out

# Get coverage for specific package
go test -cover ./pkg/state
```

### Test Timing

```bash
# Show test duration for slow tests
go test -v -timeout 30s ./...

# Run short tests only (skip integration tests)
go test -short ./...
```

## Benchmarking

### Running Benchmarks

```bash
# Run all benchmarks
go test -bench=. ./...

# Run benchmarks in specific package
go test -bench=. ./pkg/syncmanager

# Run specific benchmark
go test -bench=BenchmarkProgressParsing ./pkg/syncmanager

# Run with memory allocation stats
go test -bench=. -benchmem ./pkg/syncmanager

# Run benchmarks multiple times for accuracy
go test -bench=. -count=5 ./pkg/syncmanager
```

### Writing Benchmarks

```go
func BenchmarkFunction(b *testing.B) {
    // Setup code (not measured)
    data := setupTestData()

    b.ResetTimer()  // Reset timer after setup

    for i := 0; i < b.N; i++ {
        // Code to benchmark
        Function(data)
    }
}

// Benchmark with memory allocation reporting
func BenchmarkAllocation(b *testing.B) {
    b.ReportAllocs()

    for i := 0; i < b.N; i++ {
        result := make([]byte, 1024)
        _ = result
    }
}

// Parallel benchmark
func BenchmarkParallel(b *testing.B) {
    b.RunParallel(func(pb *testing.PB) {
        for pb.Next() {
            Function()
        }
    })
}
```

### Benchmark Comparison

```bash
# Save benchmark results
go test -bench=. -benchmem ./pkg/syncmanager > old.txt

# After making changes
go test -bench=. -benchmem ./pkg/syncmanager > new.txt

# Compare results with benchstat (install first)
go install golang.org/x/perf/cmd/benchstat@latest
benchstat old.txt new.txt
```

## Coverage Analysis

### Package-Level Coverage

Current coverage levels by package:
- `pkg/auth`: 92.4% (excellent)
- `pkg/captiveportal`: 62.9% (good)
- `pkg/events`: 100% (perfect)
- `pkg/state`: High coverage with comprehensive tests
- `pkg/syncmanager`: Extensive test suite with security tests
- `pkg/daemon`: New comprehensive tests added
- `pkg/handlers`: Significantly improved with new tests
- `pkg/middleware`: Comprehensive with edge case coverage
- `pkg/utils`: 100% coverage of utilities

### Improving Coverage

1. **Identify Gaps**: Use coverage reports to find untested code
2. **Focus on Critical Paths**: Prioritize tests for core functionality
3. **Edge Cases**: Add tests for error conditions and boundaries
4. **Integration Tests**: Test component interactions
5. **Security Tests**: Verify input validation and sanitization

### Coverage Goals

- **Critical packages** (state, syncmanager, sdmonitor): > 80%
- **Business logic** (handlers, daemon): > 70%
- **Utilities**: > 90%
- **Overall project**: > 70%

## Best Practices

### DO

✅ Use `t.Helper()` in helper functions for better error messages
✅ Use `t.TempDir()` for automatic cleanup
✅ Use table-driven tests for multiple similar cases
✅ Test error paths, not just happy paths
✅ Use meaningful test names that describe the scenario
✅ Clean up resources with `t.Cleanup()` or `defer`
✅ Use `-race` flag regularly to detect race conditions
✅ Write benchmarks for performance-critical code

### DON'T

❌ Don't use `t.Fatal()` in goroutines (causes panic)
❌ Don't hardcode file paths (use filepath.Join)
❌ Don't leave goroutines running after tests
❌ Don't test implementation details
❌ Don't ignore errors in test code
❌ Don't use time.Sleep() for synchronization
❌ Don't commit failing tests
❌ Don't skip tests without a good reason

## Test Categories

### Unit Tests
- Test individual functions in isolation
- Use mocks for dependencies
- Fast execution (< 1s per test)
- Focus on single responsibility

### Integration Tests
- Test multiple components together
- Use real implementations where possible
- May use filesystem, but not network
- Mark with `t.Skip()` if dependencies unavailable

### Security Tests
- Path traversal attacks
- Injection vulnerabilities
- Authentication bypass
- Input validation failures
- Memory safety issues

### Performance Tests
- Benchmarks for critical paths
- Memory allocation profiling
- Concurrency performance
- Scalability testing

## Debugging Tests

### Verbose Output

```bash
# Show test output even for passing tests
go test -v ./pkg/state

# Show full stack traces on failure
go test -v -failfast ./...
```

### Running One Test

```bash
# Useful for debugging specific failures
go test -v -run TestSpecificFunction ./pkg/package
```

### Race Detector

```bash
# Enable race detection (slower but catches bugs)
go test -race ./...
```

### Memory Profiling

```bash
# Profile memory allocations
go test -memprofile=mem.prof ./pkg/syncmanager
go tool pprof mem.prof
```

## Continuous Integration

Tests should run automatically on:
- Every commit (fast tests only)
- Pull requests (full test suite + race detector)
- Before releases (all tests + benchmarks + coverage)

## Contributing

When adding new features:
1. Write tests first (TDD) or alongside implementation
2. Aim for >80% coverage of new code
3. Include edge cases and error conditions
4. Add security tests if handling user input
5. Update this guide if adding new patterns

## Resources

- [Go Testing Package](https://pkg.go.dev/testing)
- [Table Driven Tests](https://github.com/golang/go/wiki/TableDrivenTests)
- [Advanced Testing with Go](https://about.sourcegraph.com/go/advanced-testing-in-go)
- [Go Test Comments](https://github.com/golang/go/wiki/TestComments)
