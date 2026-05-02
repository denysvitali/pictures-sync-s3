# Photo Backup Station

An automated SD card photo backup appliance built with [Gokrazy](https://gokrazy.org/) for Raspberry Pi 4. This appliance automatically syncs photos from inserted SD cards to cloud storage (Backblaze B2, S3, etc.) using rclone, with real-time progress monitoring via a web interface.

## Features

- **Automatic SD Card Detection**: Automatically detects when an SD card is inserted and begins syncing
- **Intelligent Card ID System**: Each SD card gets a unique ID and its own folder in cloud storage
- **Reformat Detection**: Automatically detects reformatted cards and creates new backup folders
- **Cloud Backup**: Syncs photos to any rclone-supported backend (B2, S3, Google Drive, etc.)
- **Real-time Web UI**: Monitor sync progress, configure WiFi, and manage settings via a web interface
- **LED Status Indicators**: Visual feedback using Raspberry Pi LEDs
- **WiFi Management**: Configure and manage WiFi networks through the web UI
- **Remote Access**: Built-in Tailscale support for secure remote management
- **Persistent State**: Maintains sync history and configuration across reboots

## How Card IDs Work

The system uses a unique identifier for each SD card to organize backups intelligently:

### Card ID Assignment

1. **First Insertion**: When a new SD card is inserted, the system:
   - Creates a unique ID (e.g., `card-a1b2c3d4`)
   - Writes this ID to `.pictures-sync-id` file on the card root
   - Creates a dedicated folder: `remote:/photos/card-a1b2c3d4/DCIM/`

2. **Subsequent Insertions**: When the same card is re-inserted:
   - Reads the existing `.pictures-sync-id` from the card
   - Syncs to the same folder: `remote:/photos/card-a1b2c3d4/DCIM/`
   - Uses rclone's incremental sync (only uploads new/changed files)

3. **Reformatted Cards**: When a card is reformatted:
   - System detects significant file count drop (< 30% of last sync)
   - Automatically generates a NEW card ID
   - Creates a fresh folder: `remote:/photos/card-e5f6g7h8/DCIM/`
   - Previous backups remain intact in the old folder

### Remote Storage Structure

```
remote:/photos/
├── card-a1b2c3d4/          # First SD card
│   └── DCIM/
│       ├── 100CANON/
│       └── 101CANON/
├── card-e5f6g7h8/          # Same card after reformat
│   └── DCIM/
│       └── 100CANON/
└── card-xyz789ab/          # Different SD card
    └── DCIM/
        └── 100NIKON/
```

### Benefits

- **No Duplicate Uploads**: Same photos won't be uploaded again
- **Organized Backups**: Each card/session has its own folder
- **Safe Reformatting**: Old photos preserved even after card is formatted
- **Multiple Cards**: Support for multiple SD cards from different cameras
- **Incremental Sync**: Only new files are transferred

## Hardware Requirements

- Raspberry Pi 4 (tested, other models may work)
- MicroSD card (for Gokrazy OS)
- USB SD card reader (for photo SD cards)
- Power supply
- Optional: WiFi network (Ethernet also supported)

## Architecture

The system consists of two main services:

1. **pictures-sync**: Main daemon that monitors for SD cards and triggers syncs
2. **webui**: Web server providing the user interface and REST API

Supporting packages:
- `pkg/state`: Manages persistent state and sync history
- `pkg/sdmonitor`: Detects SD card insertion/removal
- `pkg/syncmanager`: Manages rclone sync operations
- `pkg/ledcontroller`: Controls Raspberry Pi LEDs for status indication
- `pkg/wifimanager`: Manages WiFi configuration

## Installation

### Prerequisites

1. Install Go (1.21 or later)
2. Install gok CLI tool:
   ```bash
   go install github.com/gokrazy/tools/cmd/gok@main
   ```

### Initial Setup

1. Clone this repository:
   ```bash
   git clone <repository-url>
   cd pictures-sync-s3
   ```

2. Run the setup script to configure the instance:
   ```bash
   ./setup-gokrazy.sh
   ```

   The script will:
   - Create a new gokrazy instance in `~/gokrazy/<instance-name>`
   - Add all required packages
   - Create `go.mod` with replace directive pointing to your local code
   - Configure hostname and Tailscale (optional)
   - Set up WiFi (optional)

   **Note**: The key to making this work with a private repository is the `replace` directive in `~/gokrazy/<instance-name>/go.mod` that tells gokrazy to use your local code instead of fetching from GitHub.

3. Alternatively, manually configure the instance:

   ```json
   {
     "Hostname": "photo-backup",
     "DeviceType": "raspberrypi4b",
     "Update": {
       "HTTPPort": "80",
       "HTTPSPort": "443",
       "UseTLS": "self-signed"
     },
     "Packages": [
       "github.com/gokrazy/fbstatus",
       "github.com/gokrazy/mkfs",
       "github.com/gokrazy/wifi",
       "tailscale.com/cmd/tailscaled",
       "tailscale.com/cmd/tailscale",
       "github.com/denysvitali/pictures-sync-s3/cmd/pictures-sync",
       "github.com/denysvitali/pictures-sync-s3/cmd/webui"
     ],
     "PackageConfig": {
       "tailscale.com/cmd/tailscale": {
         "CommandLineFlags": [
           "up",
           "--auth-key=YOUR_TAILSCALE_AUTH_KEY",
           "--hostname=photo-backup",
           "--ssh"
         ]
       },
       "github.com/denysvitali/pictures-sync-s3/cmd/webui": {
         "Environment": [
           "PORT=8080"
         ]
       }
     }
   }
   ```

4. Get a Tailscale auth key:
   - Visit https://login.tailscale.com/admin/settings/keys
   - Generate a new auth key (disable key expiry for persistent access)
   - Replace `YOUR_TAILSCALE_AUTH_KEY` in config.json

4. Build and write to SD card:
   ```bash
   # Insert SD card and identify device (e.g., /dev/sdb)
   gok -i <instance-name> overwrite --full /dev/sdX
   ```

   The `gok` tool will use the `go.mod` with the replace directive from `~/gokrazy/<instance-name>/` to build your local code.

5. Boot the Raspberry Pi with the new SD card

## Configuration

### First-Time Setup

1. **WiFi Configuration** (if not using Ethernet):
   - On first boot with no configured networks, the device seeds a setup network:
     ```json
     [
       {"ssid": "PhotoBackup-Setup", "psk": "photo-backup-setup"}
     ]
     ```
     (`/perm/wifi.json` is expected to contain an array of network objects.)
   - Override defaults with env vars:
     - `SETUP_WIFI_SSID`
     - `SETUP_WIFI_PSK`
   - Or pre-seed `/perm/wifi.json` before first boot with your network:
     ```json
     [
       {"ssid": "YourNetwork", "psk": "YourPassword"}
     ]
     ```

2. **Access the Web UI**:
   - Local network: `https://photo-backup.local:443` or `http://photo-backup.local:8080`
   - Via Tailscale: `https://photo-backup:443` (after Tailscale connects)
   - Accept the self-signed certificate warning

3. **Configure Cloud Storage**:
   - Navigate to the "Configuration" tab
   - Generate rclone config on your computer:
     ```bash
     rclone config
     ```
   - Copy the generated config content
   - Paste into the web UI configuration form
   - Set Remote Name (e.g., "b2backup") and Remote Path (e.g., "/photos")
   - Click "Test Connection" to verify

### Rclone Configuration Examples

#### Backblaze B2
```ini
[b2backup]
type = b2
account = <application_key_id>
key = <application_key>
hard_delete = false
```

#### Amazon S3
```ini
[s3backup]
type = s3
provider = AWS
access_key_id = <your_access_key>
secret_access_key = <your_secret_key>
region = us-east-1
```

#### S3-Compatible (MinIO, Wasabi, etc.)
```ini
[s3compatible]
type = s3
provider = Other
access_key_id = <your_access_key>
secret_access_key = <your_secret_key>
endpoint = https://s3.example.com
region = auto
```

## Usage

### Basic Workflow

1. Insert an SD card with photos (must have a DCIM directory)
2. The system automatically:
   - Detects the SD card
   - Mounts it read-only
   - Reads or creates card ID (`.pictures-sync-id` file)
   - Checks if card was previously synced
   - Detects if card was reformatted (creates new ID if so)
   - Counts photos and total size
   - Syncs to card-specific folder in cloud storage
   - Updates LED status
   - Records sync history with card ID
3. Web UI shows real-time progress and card ID
4. When complete, safely remove the SD card
5. Next insertion of same card will sync only new/changed files

### LED Status Indicators

- **Steady Green (ACT)**: Idle, waiting for SD card
- **Slow Blink**: SD card detected, preparing to sync
- **Fast Blink**: Sync in progress
- **3x Rapid Blink**: Sync completed successfully
- **Red Blink (PWR)**: Error occurred

### Web UI Features

#### Status Dashboard
- Current sync status
- Real-time progress bar
- File count and data transferred
- Transfer speed and ETA
- SD card information

#### History Tab
- View all past sync operations
- See file counts, data sizes, and durations
- Error messages for failed syncs

#### WiFi Tab
- Scan for available networks
- Add new WiFi networks
- View saved networks
- See currently connected network

#### Configuration Tab
- Configure rclone backend
- Set remote name and path
- Test connection
- View configuration status

## File Structure on Device

```
/perm/pictures-sync/
├── rclone.conf          # Rclone configuration
├── settings.json        # Application settings (remote name/path, thresholds)
├── sync-history.json    # Past sync records (includes card IDs)
├── state.json           # Current system state
└── mounts/
    └── sdcard/          # SD card mount point
        └── .pictures-sync-id  # Card ID file (created on card)

/perm/wifi.json          # WiFi configurations (managed by gokrazy/wifi)
```

**Note**: The `.pictures-sync-id` file is written to the root of each SD card and persists across insertions. If the card is reformatted, this file is lost and a new ID is generated.

### Settings Persistence

All configuration changes made through the web UI are automatically persisted:

- **Remote Name & Path**: Saved to `/perm/pictures-sync/settings.json`
- **Rclone Configuration**: Saved to `/perm/pictures-sync/rclone.conf`
- **WiFi Networks**: Saved to `/perm/wifi.json`

Settings survive reboots and are automatically loaded on startup. No need to set environment variables or edit Gokrazy config files for runtime settings.

## Troubleshooting

### SD Card Not Detected

1. Check if the card is properly inserted
2. Verify USB card reader is working: `ls /dev/sd*` or `ls /dev/mmcblk*`
3. Check system logs in the web UI

### Sync Fails

1. Test rclone connection in Configuration tab
2. Verify rclone config is correct
3. Check network connectivity
4. Review sync history for error messages

### WiFi Not Connecting

1. Verify SSID and password are correct
2. Check if network is in range
3. Try removing and re-adding the network
4. Use Ethernet as fallback

### Cannot Access Web UI

1. Check if device is on network: `ping photo-backup.local`
2. Try IP address directly: `http://<raspberry-pi-ip>:8080`
3. Use Tailscale if configured: Check Tailscale admin console for device
4. Verify services are running via SSH/Tailscale

## Remote Management

### Via Tailscale

Once Tailscale is configured, you can:

1. Access web UI from anywhere: `https://photo-backup:443`
2. SSH into device: `ssh photo-backup` (if --ssh flag enabled)
3. Monitor status remotely

### Updates

To update the system:

```bash
# On your development machine
gok update
```

Or use the Gokrazy web interface (separate from this app's UI).

## Development

### Building Locally

```bash
# Build both services
go build ./cmd/pictures-sync
go build ./cmd/webui

# Run locally (for testing)
./pictures-sync
./webui
```

### Testing

```bash
# Run tests
go test ./...

# Test individual packages
go test ./pkg/state
go test ./pkg/syncmanager
```

### CI - OTA image

GitHub Actions builds a flashable Gokrazy image on every push to `master`, producing `photo-backup-rpi4b.img` as a workflow artifact. The same workflow also runs for version tags (`v*`) and preserves OTA release publishing behavior for tags with `photo-backup-ota.squashfs`.

Workflow:
`.github/workflows/ota-image.yml`

Run the same locally with:
```bash
make ota
```

Or build + publish a release in one command (with GitHub token and repository context):
```bash
GITHUB_REPOSITORY=<owner>/<repo> GH_TOKEN=<token> TAG_NAME=v1.2.3 make ota-release
```
Required for `ota-release`:
- `GH_TOKEN` (or `GITHUB_TOKEN`)
- `GITHUB_REPOSITORY` (for example `owner/repo`)
- `TAG_NAME` (or run from a checked-out tag)

### Making Changes

1. Modify code in your project directory
2. Test locally if possible
3. Deploy to device:
   ```bash
   gok -i <instance-name> overwrite /dev/sdX  # Full overwrite for SD card
   # OR
   gok -i <instance-name> update              # Over-the-air update if device is already running
   ```

   The `replace` directive in `~/gokrazy/<instance-name>/go.mod` ensures your local changes are always used.

## Security Considerations

- The web UI uses self-signed TLS by default
- Rclone configuration contains sensitive credentials - stored in `/perm` (persistent partition)
- WiFi passwords stored in `/perm/wifi.json` with 0600 permissions
- Tailscale provides secure remote access without exposing ports
- SD cards are mounted read-only to prevent accidental modifications

## Performance

- Sync speed depends on:
  - Network bandwidth
  - Cloud storage provider
  - Number and size of files
  - SD card read speed

Typical performance:
- 100-500 files: 1-5 minutes
- 1000+ files: 10-30 minutes

## License

[Your License Here]

## Acknowledgments

- Built with [Gokrazy](https://gokrazy.org/)
- Uses [rclone](https://rclone.org/) for cloud sync
- WebSocket support via [gorilla/websocket](https://github.com/gorilla/websocket)
- Tailscale for secure remote access

## Contributing

Contributions welcome! Please:

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Submit a pull request

## Support

For issues and questions:
- GitHub Issues: [Your repo URL]
- Documentation: [Your docs URL]
