# Quick Start Guide

## What You've Built

A complete Gokrazy-based photo backup appliance that automatically syncs SD cards to cloud storage.

## Project Structure

```
pictures-sync-s3/
├── cmd/
│   ├── pictures-sync/     # Main daemon (SD monitoring & sync)
│   └── webui/             # Web UI server (API + WebSocket)
├── pkg/
│   ├── state/             # State management & persistence
│   ├── sdmonitor/         # SD card detection
│   ├── syncmanager/       # rclone sync operations
│   ├── ledcontroller/     # LED status indicators
│   └── wifimanager/       # WiFi configuration
├── web/                   # Web UI assets (embedded)
│   ├── index.html
│   ├── style.css
│   └── app.js
├── config/
│   └── rclone.conf.template
├── setup-gokrazy.sh       # Setup helper script
└── README.md              # Full documentation
```

## Quick Setup Steps

### 1. Install Prerequisites

```bash
# Install gok tool
go install github.com/gokrazy/tools/cmd/gok@main

# Verify installation
gok --version
```

### 2. Get Tailscale Auth Key

1. Visit https://login.tailscale.com/admin/settings/keys
2. Generate new auth key (disable expiry)
3. Save for next step

### 3. Run Setup Script

```bash
./setup-gokrazy.sh
```

Follow the prompts to configure:
- Instance name (default: photo-backup)
- Tailscale auth key
- WiFi credentials (optional)
- Rclone remote settings

### 4. Flash SD Card

```bash
# Find your SD card device
lsblk

# Write to SD card (CAREFUL - this erases the card!)
gok -i photo-backup overwrite --full /dev/sdX
```

### 5. Boot & Configure

1. Insert SD card into Raspberry Pi 4
2. Power on and wait ~1 minute
3. Access web UI:
   - Local: `https://photo-backup.local`
   - Tailscale: `https://photo-backup`

4. Configure rclone in the Configuration tab:
   ```bash
   # On your computer, generate rclone config:
   rclone config

   # Copy the config content to web UI
   ```

## Testing Locally (Without Hardware)

```bash
# Build the services
go build ./cmd/pictures-sync
go build ./cmd/webui

# Run web UI (in one terminal)
PORT=8080 ./webui

# Access at http://localhost:8080
```

Note: SD card detection and LED control require actual hardware.

## Next Steps

1. **Configure Cloud Storage**: Set up B2/S3 credentials in web UI
2. **Test Connection**: Use "Test Connection" button
3. **Insert SD Card**: Insert a camera SD card with DCIM folder
4. **Monitor Progress**: Watch sync progress in web UI
5. **Check History**: View completed syncs in History tab

## Common Commands

```bash
# Update instance after code changes
gok -i photo-backup update

# Edit instance configuration
gok -i photo-backup edit

# Build for testing
go build ./...

# Run tests
go test ./...
```

## Architecture Overview

### How It Works

1. **SD Card Detection** (`sdmonitor`):
   - Polls `/dev/sd*` and `/dev/mmcblk*` every 2 seconds
   - Detects USB storage devices
   - Auto-mounts at `/perm/pictures-sync/mounts/sdcard`

2. **Sync Process** (`syncmanager`):
   - Runs rclone as subprocess
   - Parses JSON progress output
   - Updates state in real-time
   - Handles errors and cancellation

3. **LED Feedback** (`ledcontroller`):
   - Monitors state changes
   - Controls `/sys/class/leds/ACT/` (green LED)
   - Different patterns for different states

4. **Web Interface** (`webui`):
   - Embedded static assets
   - REST API for configuration
   - WebSocket for real-time updates
   - WiFi network management

5. **State Persistence** (`state`):
   - Stores data in `/perm/pictures-sync/`
   - Sync history JSON database
   - Current state tracking
   - Atomic file writes

### Communication Flow

```
SD Card Inserted
    ↓
sdmonitor detects
    ↓
state.SetStatus(Detected)
    ↓
ledcontroller updates LED
    ↓
pictures-sync starts sync
    ↓
syncmanager runs rclone
    ↓
Progress → state → WebSocket → Web UI
    ↓
Sync complete
    ↓
state.FinishSync()
    ↓
LED shows success
```

## Customization

### Change LED Patterns

Edit `pkg/ledcontroller/ledcontroller.go`:

```go
var (
    PatternSlowBlink  = LEDPattern{...}
    PatternFastBlink  = LEDPattern{...}
)
```

### Change Sync Behavior

Edit `cmd/pictures-sync/main.go`:

```go
// Wait time before syncing
time.Sleep(2 * time.Second)

// Sync options
syncMgr.Sync(dcimPath, fileCount, totalBytes)
```

### Modify Web UI

Edit files in `web/` directory:
- `index.html` - Structure
- `style.css` - Styling
- `app.js` - Functionality

Rebuild: `go build ./cmd/webui`

## Troubleshooting

### "gok: command not found"
```bash
go install github.com/gokrazy/tools/cmd/gok@main
export PATH=$PATH:$(go env GOPATH)/bin
```

### "Cannot access web UI"
- Check device is powered on
- Verify network connection: `ping photo-backup.local`
- Try IP address directly
- Check Tailscale status

### "SD card not detected"
- Verify USB card reader works: `lsblk`
- Check card has DCIM folder
- Review logs in web UI

### "Sync fails"
- Test rclone config locally: `rclone ls remote:`
- Verify network connectivity
- Check cloud storage credentials
- Review error in History tab

## Resources

- Full documentation: `README.md`
- Gokrazy docs: https://gokrazy.org/
- rclone docs: https://rclone.org/
- Tailscale setup: https://tailscale.com/kb/

## Support

For issues specific to this project, check:
1. README.md troubleshooting section
2. Gokrazy documentation
3. rclone documentation

Have fun building your photo backup station!
