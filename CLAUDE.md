# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

A Gokrazy-based photo backup appliance for Raspberry Pi 4 that automatically syncs SD card photos to cloud storage (Backblaze B2, S3, etc.) using rclone. The system uses a unique card ID system to organize backups and detect reformatted cards.

## Build and Development Commands

### Local Development
```bash
# Build both services
go build ./cmd/pictures-sync
go build ./cmd/webui

# Run tests
go test ./...
go test ./pkg/state
go test ./pkg/syncmanager

# Run locally (webui only - SD monitoring requires hardware)
PORT=8080 ./webui
```

### Gokrazy Deployment
```bash
# Initial setup
./setup-gokrazy.sh

# Deploy to SD card (DESTRUCTIVE - erases card!)
gok -i photo-backup overwrite --full /dev/sdX

# Over-the-air update (after initial deployment)
gok -i photo-backup update

# Edit instance configuration
gok -i photo-backup edit
```

The setup script creates a Gokrazy instance at `~/gokrazy/<instance-name>` with a `go.mod` containing a `replace` directive pointing to this local repository. This allows deploying private code without publishing to GitHub.

## Architecture

### Service Model
Two main services run as Gokrazy packages:

1. **pictures-sync** (`cmd/pictures-sync/main.go`):
   - Main daemon that monitors for SD card insertion
   - Orchestrates sync operations
   - Controls LED feedback
   - Event-driven architecture using channels

2. **webui** (`cmd/webui/main.go`):
   - HTTP server on port 8080
   - REST API for configuration
   - WebSocket for real-time status updates
   - Embeds complete web UI as inline HTML/CSS/JS

### Package Structure

- **pkg/state**: Centralized state management with file-based persistence
  - Manages current sync status and history
  - Publisher/subscriber pattern for real-time updates
  - Atomic writes to `/perm/pictures-sync/` directory
  - Thread-safe with RWMutex

- **pkg/sdmonitor**: SD card detection and mounting
  - Polls `/dev/sd*` and `/dev/mmcblk*` every 2 seconds
  - Mounts cards read-only to prevent corruption
  - Manages unique card IDs via `.pictures-sync-id` file on card root
  - Detects USB vs built-in SD devices via `/sys/block/*/device/uevent`

- **pkg/syncmanager**: rclone integration
  - Spawns rclone as subprocess with JSON logging
  - Parses progress from `--use-json-log` output
  - Syncs to card-specific folders: `remote:/photos/{cardID}/DCIM/`
  - Real-time progress updates via channels

- **pkg/settings**: Runtime configuration persistence
  - Stores remote name/path and reformat threshold
  - Automatically saves to `/perm/pictures-sync/settings.json`
  - Thread-safe with atomic file writes

- **pkg/ledcontroller**: LED status indicators
  - Controls Raspberry Pi ACT LED via `/sys/class/leds/ACT/`
  - Different blink patterns for different states
  - Watches state manager for status changes

- **pkg/wifimanager**: WiFi configuration
  - Manages `/perm/wifi.json` for gokrazy/wifi package
  - Network scanning via `iwlist` or similar tools
  - Add/remove networks through web UI

### Card ID System

Critical feature for organizing backups:

1. Each SD card gets a unique ID stored in `.pictures-sync-id` on card root
2. First insertion: Generate random ID (e.g., `card-a1b2c3d4`)
3. Subsequent insertions: Read existing ID from card
4. Reformat detection: If file count < 30% of last sync, generate new ID
5. Remote structure: `remote:/photos/card-{id}/DCIM/`

See `sdmonitor.GetOrCreateCardID()` and `main.handleCardInserted()` in `cmd/pictures-sync/main.go:98-198`

### State Flow

```
SD Card Inserted
  → sdmonitor detects (sdmonitor.go:96)
  → state.SetStatus(StatusDetected)
  → LED controller updates (ledcontroller.go)
  → Count photos and get/create card ID
  → Check for reformat (main.go:142-165)
  → state.StartSync(cardID, ...)
  → syncmanager.Sync(dcimPath, cardID, ...)
  → rclone subprocess with JSON progress
  → Progress → state.UpdateSyncProgress() → WebSocket → Web UI
  → state.FinishSync()
  → Append to sync history
```

### Persistence

All state persists to `/perm/pictures-sync/` (Gokrazy permanent partition):
- `rclone.conf` - Rclone backend credentials
- `settings.json` - Remote name/path, thresholds
- `sync-history.json` - Array of all past syncs with card IDs
- `state.json` - Current system state
- `mounts/sdcard/` - SD card mount point

Settings are configured via web UI and automatically persist - no need for environment variables in Gokrazy config.

## Key Implementation Details

### Thread Safety
- All shared state uses `sync.RWMutex`
- File writes are atomic (write to `.tmp`, then rename)
- Channel-based communication between packages

### Error Handling
- LED blinks red on errors
- Sync history records errors with messages
- Web UI displays errors in history tab
- Services log errors but don't crash on individual failures

### WebSocket Protocol
Real-time status updates sent as JSON matching `state.CurrentState`:
```go
{
  "status": "syncing",
  "current_sync": {
    "files_synced": 150,
    "files_total": 500,
    "bytes_transferred": 1234567890
  },
  "sdcard_mounted": true,
  "sdcard_path": "/perm/pictures-sync/mounts/sdcard"
}
```

### Rclone Integration
- Runs `rclone sync` with `--use-json-log` flag
- Parses JSON log lines for stats updates
- Config path: `/perm/pictures-sync/rclone.conf`
- Destination format: `{remoteName}:{remotePath}/{cardID}/DCIM/`
- Read-only source mount prevents card corruption

## Common Development Patterns

### Adding a New Setting
1. Add field to `settings.Settings` struct
2. Add getter method with RWMutex
3. Add setter method that calls `Save()`
4. Update `ToJSON()` for API
5. Add form field in web UI (webui/main.go inline HTML)
6. Wire up in API handlers (`handleSettings`)

### Modifying Sync Behavior
- Core logic: `cmd/pictures-sync/main.go:handleCardInserted()`
- Sync execution: `pkg/syncmanager/syncmanager.go:Sync()`
- Progress parsing: `syncmanager.processLogLine()`
- State updates: `pkg/state/state.go`

### Testing Locally Without Hardware
- Build and run `webui` service standalone
- Mock SD card events by manually calling state manager methods
- Test web UI at `http://localhost:8080`
- LED and SD monitor will fail gracefully without hardware

## Gokrazy-Specific Notes

- Services run as separate processes managed by Gokrazy init
- No systemd or traditional init system
- Web UI must be self-contained (no external asset files)
- `/perm` is the only writable filesystem that persists across reboots
- Use `gok -i <instance>` for all deployment commands
- The `replace` directive in instance's `go.mod` is critical for private repos
