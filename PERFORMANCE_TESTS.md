# Performance and Load Testing

This document describes the comprehensive performance and load testing suite for the pictures-sync-s3 application.

## Overview

The test suite includes benchmarks and load tests for all critical paths in the application:

1. **SD Card Operations** (`pkg/sdmonitor/`)
2. **State Management** (`pkg/state/`)
3. **HTTP Endpoints** (`pkg/handlers/`)
4. **WebSocket Connections** (`pkg/websocket/`)

## Running Tests

### Quick Benchmark Run

Run all benchmarks with minimal iterations:

```bash
go test -run=^$ -bench=. -benchtime=10x ./pkg/sdmonitor/ ./pkg/state/ ./pkg/websocket/ ./pkg/handlers/
```

### Full Benchmark Suite

Run comprehensive benchmarks with memory profiling:

```bash
go test -run=^$ -bench=. -benchmem -benchtime=1000x ./...
```

### Load Tests Only

Run only load and stress tests (skipped in short mode):

```bash
go test -v -run="Load|Stress" ./...
```

### Short Mode

Skip long-running stress and load tests:

```bash
go test -short ./...
```

## Test Categories

### 1. SD Card Operations (`pkg/sdmonitor/`)

#### Performance Benchmarks (`performance_test.go`)
- `BenchmarkCountPhotos/*` - Photo counting with varying file counts (10 to 5000 files)
- `BenchmarkCountPhotosMemory` - Memory allocations during photo counting
- `BenchmarkCountPhotosParallel` - Concurrent photo counting
- `BenchmarkCountPhotosNestedDirs` - Multiple subdirectories
- `BenchmarkCountPhotosMixedFiles` - Mixed file types
- `BenchmarkFindStorageDevice` - Device detection overhead
- `BenchmarkMonitorCreation` - Monitor initialization
- `BenchmarkGetCachedMounts` - Mount cache performance

#### Card ID Benchmarks (`cardid_performance_test.go`)
- `BenchmarkGenerateCardID` - Card ID generation speed
- `BenchmarkGenerateCardIDParallel` - Parallel ID generation
- `BenchmarkGenerateCardIDUniqueness` - Uniqueness verification
- `BenchmarkGetOrCreateCardID_*` - Card ID persistence operations
- `BenchmarkCardIDStressTest` - Stress test under high concurrency (100 goroutines)
- `BenchmarkCardIDCollisionTest` - Collision detection at scale (10,000 IDs)

### 2. State Management (`pkg/state/`)

#### Core Performance (`performance_test.go`)
- `BenchmarkManagerCreation` - State manager initialization
- `BenchmarkGetState` - State retrieval
- `BenchmarkGetStateParallel` - Concurrent state reads
- `BenchmarkSetStatus` - Status updates with disk persistence
- `BenchmarkStartSync` - Sync operation start
- `BenchmarkUpdateSyncProgress` - Progress updates (with throttling)
- `BenchmarkFinishSync` - Sync completion
- `BenchmarkGetHistory` - History retrieval (10 to 1000 records)
- `BenchmarkFindLastSyncByCardID` - Card ID lookup
- `BenchmarkReload` - State reload from disk
- `BenchmarkSubscribeUnsubscribe` - Notification subscription
- `BenchmarkConcurrentOperations` - Mixed concurrent operations

#### Memory Benchmarks (`memory_benchmark_test.go`)
- `BenchmarkMemoryUsageIdle` - Baseline memory usage
- `BenchmarkMemoryUsageWithHistory` - Memory with large history (100 to 10,000 records)
- `BenchmarkMemoryUsageSubscribers` - Memory with many subscribers (10 to 1,000)
- `BenchmarkMemoryLeakDetection` - Long-running memory leak detection
- `BenchmarkConcurrentMemoryUsage` - Memory under concurrent load
- `TestMemoryGrowthPattern` - Memory growth over 10 iterations
- `BenchmarkGarbageCollectionImpact` - GC impact measurement
- `BenchmarkStructCopyOverhead` - State copying overhead
- `TestConcurrentMemorySafety` - Thread safety verification (100 goroutines, 1,000 iterations each)

#### File I/O Stress Tests (`io_stress_test.go`)
- `BenchmarkFileWrite` - Raw file write performance
- `BenchmarkFileRead` - Raw file read performance
- `BenchmarkAtomicWrite` - Atomic file write overhead
- `BenchmarkConcurrentFileWrites` - Concurrent file operations
- `BenchmarkStateSaveLoad` - State persistence roundtrip
- `BenchmarkHistorySaveLoad` - History persistence (10 to 1,000 records)
- `TestConcurrentFileAccessStress` - Concurrent file I/O safety (50 writers, 50 readers, 100 ops each)
- `TestFileCorruptionDetection` - Corrupted file handling
- `TestPartialWriteRecovery` - Recovery from partial writes
- `TestStressFileOperations` - Extreme load test (100 goroutines, 10 seconds)
- `BenchmarkJSONMarshalUnmarshal` - JSON serialization overhead

#### Timeout and Resource Limits (`timeout_test.go`)
- `TestOperationTimeout` - Operation timeout constraints (100ms)
- `TestLongRunningOperations` - 5-second sync with 100 updates/sec
- `TestResourceLimits` - Resource constraints (10 to 100 goroutines, 100 to 1,000 operations)
- `TestDeadlockDetectionTimeout` - Deadlock detection (100 goroutines, mixed operations)
- `TestRateLimiting` - Progress update throttling (1,000 rapid updates)
- `TestGracefulShutdown` - Shutdown behavior (50 goroutines)
- `BenchmarkOperationLatency` - Critical operation latency
- `BenchmarkWorstCaseLatency` - Worst-case scenarios (1,000 history items, 100 subscribers)

### 3. HTTP Endpoints (`pkg/handlers/`)

#### Load Tests (`load_test.go`)
- `BenchmarkHandleStatusLoad` - Status endpoint performance
- `BenchmarkHandleStatusParallel` - Concurrent status requests
- `BenchmarkHandleHistory` - History endpoint (10 records)
- `BenchmarkHandleHistoryLarge` - Large history endpoint (1,000 records)
- `BenchmarkJSONResponse` - JSON serialization
- `BenchmarkJSONResponseLarge` - Large JSON responses (1,000 records)
- `BenchmarkConcurrentEndpoints` - Multiple endpoints concurrently
- `TestLoadTestStatus` - Load test scenarios (10 to 100 concurrency, 1,000 to 10,000 requests)
- `BenchmarkEndToEndRequest` - Full request/response cycle
- `BenchmarkResponseParsing` - JSON response parsing
- `BenchmarkMethodValidation` - HTTP method validation overhead
- `TestStressEndpoints` - Extreme stress test (200 goroutines, 10 seconds)
- `TestTimeoutBehavior` - Timeout handling (100ms)

### 4. WebSocket Connections (`pkg/websocket/`)

#### Load Tests (`load_test.go`)
- `BenchmarkWebSocketUpgrade` - WebSocket handshake
- `BenchmarkWebSocketAuth` - Authentication (valid and invalid tokens)
- `BenchmarkWebSocketMessageReceive` - Message reception
- `BenchmarkConcurrentConnections` - Simultaneous connections (10 to 100)
- `BenchmarkTokenGeneration` - Token generation speed
- `BenchmarkTokenValidation` - Token validation
- `BenchmarkOriginValidation` - Origin checking
- `BenchmarkRateLimiter` - Rate limiting overhead
- `TestWebSocketLoadTest` - Load test scenarios (10 to 100 concurrency, 10 to 30 messages)
- `TestWebSocketStress` - Extreme stress test (200 goroutines, 10 seconds)
- `BenchmarkNotificationBroadcast` - Broadcast to 10 connections
- `TestConnectionTimeout` - Connection timeout (5 seconds)
- `BenchmarkMemoryUsagePerConnection` - Memory per connection
- `BenchmarkTokenCleanup` - Expired token cleanup (1,000 tokens)

## Performance Targets

### Latency
- **GetState**: < 10μs
- **SetStatus**: < 1ms (with disk persistence)
- **UpdateSyncProgress**: < 100μs (throttled disk writes)
- **HTTP Status Endpoint**: < 1ms
- **WebSocket Auth**: < 100ms

### Throughput
- **HTTP Requests**: > 1,000 req/sec (per endpoint)
- **WebSocket Connections**: > 100 concurrent connections
- **Progress Updates**: > 100 updates/sec
- **File I/O**: > 500 operations/sec

### Scalability
- **History Size**: Tested up to 10,000 records
- **Photo Count**: Tested up to 5,000 files
- **Concurrent Goroutines**: Tested up to 200
- **WebSocket Subscribers**: Tested up to 1,000

### Error Rates
- **Stress Tests**: < 1% error rate
- **Load Tests**: < 5% error rate
- **Memory Leaks**: < 10x growth over 10 iterations

## Continuous Integration

These tests can be integrated into CI pipelines:

```yaml
# Quick smoke test
- go test -short -run=^$ -bench=BenchmarkCountPhotos/10_files -benchtime=10x ./pkg/sdmonitor/

# Comprehensive nightly tests
- go test -v -bench=. -benchmem -benchtime=1000x ./...
- go test -v -run="Load|Stress" ./...
```

## Troubleshooting

### Tests Timing Out
Increase timeout for stress tests:
```bash
go test -timeout 30m -run="Stress" ./...
```

### Memory Tests Failing
Check available system memory:
```bash
free -h
go test -v -run=TestMemoryGrowthPattern ./pkg/state/
```

### Rate Limit Errors
Adjust rate limits in source or reduce concurrency:
```bash
go test -v -run=TestLoadTest -parallel=1 ./...
```

## Adding New Tests

When adding new features, include:

1. **Benchmark** for critical path functions
2. **Load test** for HTTP endpoints
3. **Memory benchmark** for stateful operations
4. **Stress test** for concurrent scenarios
5. **Timeout test** for long-running operations

Example benchmark:

```go
func BenchmarkMyFeature(b *testing.B) {
    // Setup
    setup := createTestSetup(b)

    b.ReportAllocs()  // Track memory allocations
    b.ResetTimer()    // Exclude setup time

    for i := 0; i < b.N; i++ {
        // Benchmark code
        result := myFeature(setup)
        _ = result  // Prevent optimization
    }
}
```

Example load test:

```go
func TestMyFeatureLoad(t *testing.T) {
    if testing.Short() {
        t.Skip("Skipping load test in short mode")
    }

    const (
        concurrency = 100
        requests    = 10000
    )

    // Test implementation with metrics
}
```

## Profiling

Generate CPU and memory profiles:

```bash
# CPU profile
go test -cpuprofile=cpu.prof -bench=. ./pkg/state/
go tool pprof cpu.prof

# Memory profile
go test -memprofile=mem.prof -bench=. ./pkg/state/
go tool pprof mem.prof

# Trace
go test -trace=trace.out -bench=. ./pkg/state/
go tool trace trace.out
```

## References

- [Go Testing Documentation](https://pkg.go.dev/testing)
- [Go Benchmarking](https://dave.cheney.net/2013/06/30/how-to-write-benchmarks-in-go)
- [Profiling Go Programs](https://go.dev/blog/pprof)
