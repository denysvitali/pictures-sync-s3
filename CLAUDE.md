# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) and agents when working with code in this repository.

## 🚀 Agent Efficiency Guidelines

### ⚡ Core Efficiency Principles
1. **Work in Parallel**: When spawning multiple agents, ALWAYS send a single message with multiple Task tool calls for maximum performance
2. **Use Specialized Tools**: Prefer dedicated tools over bash commands (Read vs cat, Edit vs sed, Grep vs grep, Glob vs find)
3. **Cache Analysis**: Before analyzing code, check if similar work was done recently in conversation history
4. **Target Changes**: Focus on high-impact changes - prefer fixing critical issues over cosmetic improvements
5. **Verify First**: Use Read/Glob tools to understand current state before making assumptions
6. **Document Intent**: When making changes, explain the reasoning and expected impact
7. **⚠️ NO USELESS DOCUMENTATION**: Do NOT create .md files in the repository root. Only README.md and CLAUDE.md should exist there. Put specific documentation in relevant package directories or skip it entirely if not essential.
8. **⚠️ NEVER COMMIT WEBUI DIST ASSETS**: Do NOT commit, stage, force-add, or preserve generated files under `pkg/webui/dist/` under any circumstance. These are build artifacts with hashed filenames and must remain out of Git history, even after running a frontend build.

### 🎯 Task Prioritization (High to Low)
1. **Security vulnerabilities** (CVSS 7.0+)
2. **Compilation errors** and broken functionality
3. **Performance bottlenecks** in critical paths
4. **Code quality issues** affecting maintainability
5. **Documentation gaps** for complex systems
6. **Cosmetic improvements** and style consistency

### 📋 Agent Collaboration Patterns
- **Code Analysis**: Use 2-3 agents for comprehensive coverage (security, performance, architecture)
- **Testing**: Deploy agents for unit, integration, and e2e testing in parallel
- **Refactoring**: Assign agents to different packages/components to avoid conflicts
- **Documentation**: Split by audience (developers, users, deployment) across agents

## Project Overview

A Gokrazy-based photo backup appliance for Raspberry Pi 4 that automatically syncs SD card photos to cloud storage (Backblaze B2, S3, etc.) using rclone. The system features enterprise-grade security, real-time WebSocket updates, comprehensive testing, and modern Go patterns.

## 🔧 Build and Development Commands

### Local Development
```bash
# Build all services
go build ./cmd/pictures-sync
go build ./cmd/webui

# Run comprehensive tests
go test ./...                              # All tests
go test -tags security_audit ./...         # Include security vulnerability tests
go test -short ./...                       # Quick smoke tests
go test -race ./...                        # Race condition detection
go test -bench=. -benchmem ./...          # Performance benchmarks

# Run services locally
PORT=8080 ./webui                         # WebUI only (no hardware needed)

# Performance and load testing
go test -run=^$ -bench=. ./pkg/state/     # State manager benchmarks
```

### Gokrazy Deployment
```bash
# Initial setup
./setup-gokrazy.sh

# Deploy to SD card (DESTRUCTIVE!)
gok -i photo-backup overwrite --full /dev/sdX

# Over-the-air updates
gok -i photo-backup update

# Edit configuration
gok -i photo-backup edit
```

## 🏗️ Modern Architecture (2024 Update)

### Enhanced Service Model
1. **pictures-sync** (`cmd/pictures-sync/main.go`):
   - Event-driven daemon with channel-based architecture
   - SD card monitoring with hardware abstraction
   - rclone integration with real-time progress
   - LED feedback system

2. **webui** (`cmd/webui/main.go`):
   - Modern HTTP server with middleware chains
   - Bootstrap 5.3.8 responsive UI
   - Real-time WebSocket with authentication
   - gzip compression and optimized asset delivery

3. **webui** (`cmd/webui/main.go`):
   - E2E testing support
   - UI development without hardware

### 📦 Package Structure (Modernized)

#### Core Packages
- **`pkg/state`**: Thread-safe state management
  - Event-driven updates with pub/sub pattern
  - Atomic persistence with rollback capability
  - Real-time WebSocket broadcasting
  - Comprehensive performance testing

- **`pkg/sdmonitor`**: Hardware abstraction layer
  - Pluggable device detection (USB/built-in SD)
  - Card ID system with collision detection
  - Read-only mounting for safety
  - Performance testing with large datasets

- **`pkg/syncmanager`**: Cloud sync orchestration
  - rclone subprocess management
  - JSON log parsing with structured progress
  - Retry logic with exponential backoff
  - Memory-efficient streaming

- **`pkg/settings`**: Configuration management
  - Type-safe JSON persistence
  - Migration support for upgrades
  - Input validation and sanitization
  - Thread-safe concurrent access

#### New Infrastructure Packages (2024)
- **`pkg/middleware`**: HTTP middleware framework
  - Chainable middleware system (panic recovery, logging, auth)
  - Request validation and size limiting
  - Rate limiting and security headers
  - Test coverage with benchmarks

- **`pkg/httputil`**: HTTP utilities and standardization
  - Consistent JSON response formatting
  - Request parsing with security limits
  - Query parameter validation
  - Type-safe error handling

- **`pkg/captiveportal`**: Network portal detection
  - Captive portal bypass for automated syncing
  - Network connectivity validation
  - Fallback authentication mechanisms

- **`pkg/validation`**: Input validation framework
  - rclone config validation
  - Network parameter sanitization
  - Security-focused input checking

#### Enhanced Existing Packages
- **`pkg/websocket`**: Enterprise WebSocket implementation
  - Authentication token system
  - Ping/pong heartbeat monitoring
  - Rate limiting and origin validation
  - Connection cleanup and resource management
  - Load testing (supports 100+ concurrent connections)

- **`pkg/wifimanager`**: WiFi management with security
  - WPA/WPA2 password validation
  - Network scanning with signal strength
  - Secure credential storage
  - Mobile-optimized UI controls

- **`pkg/handlers`**: Modernized HTTP handlers
  - Standardized error responses
  - Security-hardened path validation
  - Comprehensive test coverage

### 🔐 Security Architecture (Enhanced 2024)

#### Multi-Layer Security
1. **Authentication & Authorization**
   - Basic auth with secure password handling
   - WebSocket token-based authentication
   - Rate limiting (5 connections/minute per IP)
   - Origin validation for WebSocket connections

2. **Input Validation**
   - Request size limits (10MB default)
   - Strict JSON parsing (rejects unknown fields)
   - Path traversal protection
   - WiFi password strength validation

3. **Data Protection**
   - WiFi passwords excluded from API responses
   - Atomic file writes prevent corruption
   - Read-only SD card mounting
   - Secure card ID generation

4. **Network Security**
   - CORS protection with private IP validation
   - TLS/SSL support for production
   - Captive portal detection and bypass

### 🎯 Card ID System (Enhanced)

Critical feature with collision detection:

1. **Generation**: Nanosecond precision + PID for uniqueness
2. **Storage**: `.pictures-sync-id` file on card root
3. **Validation**: Format validation and corruption detection
4. **Reformat Detection**: Smart threshold-based detection (30% default)
5. **Remote Structure**: `remote:/photos/card-{id}/DCIM/`
6. **Performance**: Tested with 10,000 concurrent generations

### 📊 State Flow (Real-time)

```
SD Card Event → Hardware Detection → Card ID Resolution →
State Update → WebSocket Broadcast → UI Update →
Sync Orchestration → Progress Tracking → History Storage
```

**Key Improvements:**
- Sub-100ms state updates
- Real-time progress via WebSocket
- Automatic error recovery
- Concurrent operation support

## 🧪 Testing Strategy (Comprehensive 2024)

### Test Categories
1. **Unit Tests**: Comprehensive coverage across all packages
2. **Integration Tests**: Service-to-service communication
3. **Performance Tests**: Benchmarks for critical paths
4. **Load Tests**: WebSocket and HTTP endpoint stress testing
5. **Security Tests**: Vulnerability scanning and penetration testing

### Test Execution
```bash
# Test categories
make test                   # All validation tests

# Coverage and quality
make test-coverage          # Generate coverage reports
make test-race              # Race condition detection
make lint                   # Code quality checks
```

### Mock Infrastructure
- **TestEnvironment**: Isolated test execution
- **MockCard**: Realistic SD card simulation
- **MockBackend**: Complete backend simulation for UI testing
- **Hardware Mocks**: SD card events without physical hardware

## 🚀 Performance Characteristics

### Benchmarks (Production Tested)
- **Photo Counting**: 1000 files in ~2ms
- **State Updates**: <100μs with WebSocket broadcast
- **WebSocket**: 100+ concurrent connections supported
- **HTTP Endpoints**: <10ms response time
- **Asset Delivery**: 83.6% size reduction with gzip

### Scalability
- **Large Cards**: Tested with 5,000+ photos
- **Concurrent Operations**: 200 goroutine stress tests
- **Memory Usage**: <50MB baseline, leak detection
- **Storage**: Efficient JSON persistence with atomic writes

## 🎨 Modern UI/UX (Bootstrap 5.3.8)

### Features
- **Responsive Design**: Mobile-first with breakpoint optimization
- **Accessibility**: WCAG 2.1 Level AA targeted with ARIA attributes
- **Real-time Updates**: WebSocket-powered live status
- **Progressive Enhancement**: Graceful degradation
- **Optimized Assets**: gzip compression, immutable caching

### Browser Support
- Modern browsers (Chrome 90+, Firefox 88+, Safari 14+)
- Mobile responsive (iOS Safari, Chrome Mobile)
- Keyboard navigation support
- Screen reader compatibility

## 📋 Common Development Patterns

### Adding New Features
1. **Design API**: Define request/response structures
2. **Implement Handler**: Use `pkg/middleware` and `pkg/httputil`
3. **Add State Management**: Extend `pkg/state` with new fields
4. **Update UI**: Add Bootstrap components with accessibility
5. **Write Tests**: Unit and integration coverage
6. **Document**: Update CLAUDE.md and create user docs

### Security Best Practices
1. **Input Validation**: Use `pkg/validation` for all inputs
2. **Rate Limiting**: Apply appropriate limits for all endpoints
3. **Authentication**: Verify auth for sensitive operations
4. **Path Safety**: Use `pkg/httputil.ValidatePath()` for file operations
5. **Logging**: Log security events without exposing sensitive data

### Performance Optimization
1. **Benchmarking**: Add benchmarks for new critical paths
2. **Profiling**: Use Go's built-in profiling tools
3. **Caching**: Apply appropriate cache headers
4. **Compression**: Enable gzip for text assets
5. **Connection Pooling**: Reuse connections where possible

### Testing Guidelines
1. **Test Pyramid**: Favor unit tests, supplement with integration tests
2. **Table-Driven Tests**: Use for multiple input scenarios
3. **Benchmarks**: Add for performance-critical code
4. **Mocking**: Use testify/mock for external dependencies
5. **Coverage**: Aim for 80%+ on new code

### Documentation Guidelines
1. **Repository Root**: ONLY README.md and CLAUDE.md belong in the root directory
2. **Package Documentation**: Put specific docs in relevant `/pkg/*/` directories
3. **Avoid Over-Documentation**: Don't create summary files, index files, or redundant explanations
4. **Code Comments**: Prefer clear code with minimal comments over external documentation
5. **Essential Only**: Only create documentation that provides unique value and will be maintained

## 🔧 Development Tools

### Code Quality
```bash
# Static analysis
go vet ./...
staticcheck ./...
golangci-lint run

# Security scanning
govulncheck ./...
gosec ./...

# Dependency management
go mod tidy
go mod verify
```

### Performance Analysis
```bash
# CPU profiling
go test -cpuprofile=cpu.prof -bench=. ./pkg/state/
go tool pprof cpu.prof

# Memory profiling
go test -memprofile=mem.prof -bench=. ./pkg/state/
go tool pprof mem.prof

# Race detection
go test -race ./...
```

## 📚 Documentation Navigation

### Essential Documentation
- `README.md` - Project overview and quick start
- `CLAUDE.md` - This file - comprehensive development guide

### Package-Specific Documentation
Documentation is kept close to the code in relevant package directories. Look for README.md files in specific packages for detailed implementation guidance.

## 🚀 Deployment Considerations

### Gokrazy-Specific
- Self-contained binaries with embedded assets
- `/perm` filesystem for persistent data
- No traditional package managers or systemd
- Minimal attack surface (no shell, SSH, etc.)
- Over-the-air updates via web interface

### Production Recommendations
1. **TLS**: Configure HTTPS with valid certificates
2. **Firewall**: Restrict access to management ports
3. **Monitoring**: Set up health checks and alerting
4. **Backups**: Regular backup of `/perm` partition
5. **Updates**: Regular security updates via OTA

### Performance Tuning
- **Memory**: 1GB+ recommended for large photo libraries
- **Storage**: Fast SD card (Class 10+) for better performance
- **Network**: Stable internet for cloud sync operations
- **Power**: Quality power supply to prevent corruption

## 🔄 Migration and Updates

### Version Compatibility
- Settings migration handled automatically
- Backward-compatible API changes
- Graceful degradation for unsupported features
- Database schema evolution support

### Breaking Changes
Breaking changes are documented in commit messages and include migration guides. Major version updates may require manual intervention.

---

*Last updated: May 2026 - Reflects comprehensive modernization with 15 agent collaboration effort*
