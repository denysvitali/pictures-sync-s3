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
2. Install the forked gok CLI tool. The forked tools and runtime are required
   for persistent self-signed TLS certificates:
   ```bash
   git clone https://github.com/denysvitali/gokrazy-tools.git /tmp/gokrazy-tools
   git -C /tmp/gokrazy-tools fetch --depth=1 origin 242f3106842c380a1c9a4a4473e3e3c7090f29f2
   git -C /tmp/gokrazy-tools checkout --detach FETCH_HEAD

   git clone https://github.com/denysvitali/gokrazy-internal.git /tmp/gokrazy-internal
   git -C /tmp/gokrazy-internal fetch --depth=1 origin 516186dbfc01bdae7730e451d2b8a7c7a24e5452
   git -C /tmp/gokrazy-internal checkout --detach FETCH_HEAD

   go -C /tmp/gokrazy-tools mod edit -replace=github.com/gokrazy/internal=/tmp/gokrazy-internal
   go -C /tmp/gokrazy-tools install ./cmd/gok

   git clone https://github.com/denysvitali/gokrazy.git ../gokrazy
   git -C ../gokrazy fetch --depth=1 origin cf5d4d8891039b441392ea3676f594e6d9588477
   git -C ../gokrazy checkout --detach FETCH_HEAD
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
   - Create `go.mod` with replace directives pointing to your local code and the Gokrazy runtime fork
   - Configure hostname and Tailscale (optional)
   - Set up WiFi (optional)

   **Note**: The key to making this work with a private repository is the `replace` directive in `~/gokrazy/<instance-name>/go.mod` that tells gokrazy to use your local code instead of fetching from GitHub.
   The TLS certificate persistence fix also depends on a `replace github.com/gokrazy/gokrazy => <fork checkout>` directive. `./setup-gokrazy.sh` auto-detects a sibling `../gokrazy` checkout or uses `GOKRAZY_MODULE_REPLACE=/path/to/gokrazy`.

3. Alternatively, manually configure the instance:

   ```json
   {
     "Hostname": "photo-backup",
     "DeviceType": "raspberrypi4b",
     "Update": {
       "HTTPPort": "80",
       "HTTPSPort": "443",
       "UseTLS": "self-signed",
       "TLSCertificateStorage": "perm-self-signed"
     },
     "Packages": [
       "github.com/gokrazy/fbstatus",
       "github.com/gokrazy/wifi",
       "github.com/gokrazy/serial-busybox",
       "github.com/gokrazy/breakglass",
       "tailscale.com/cmd/tailscaled",
       "tailscale.com/cmd/tailscale",
       "github.com/denysvitali/pictures-sync-s3/cmd/perm-init",
       "github.com/denysvitali/pictures-sync-s3/cmd/wifi-init",
       "github.com/denysvitali/pictures-sync-s3/cmd/tailscale-init",
       "github.com/denysvitali/pictures-sync-s3/cmd/pictures-sync",
       "github.com/denysvitali/pictures-sync-s3/cmd/webui",
       "github.com/denysvitali/pictures-sync-s3/cmd/provision-ap"
     ],
     "PackageConfig": {
       "github.com/gokrazy/gokrazy/cmd/ntp": {
         "CommandLineFlags": [
           "162.159.200.1",
           "162.159.200.123"
         ]
       },
       "github.com/gokrazy/wifi": {
         "DontStart": true
       },
       "github.com/gokrazy/breakglass": {
         "CommandLineFlags": [
           "-authorized_keys=/perm/breakglass/authorized_keys"
         ]
       },
       "github.com/denysvitali/pictures-sync-s3/cmd/pictures-sync": {
         "ExtraFilePaths": {
           "/usr/bin/mkfs.exfat": "/path/to/target-arm64/mkfs.exfat"
         }
       },
       "github.com/denysvitali/pictures-sync-s3/cmd/perm-init": {
         "ExtraFilePaths": {
           "/usr/local/bin/mke2fs": "/path/to/target-arm64/mke2fs"
         }
       },
       "tailscale.com/cmd/tailscale": {
         "DontStart": true
       },
       "github.com/denysvitali/pictures-sync-s3/cmd/tailscale-init": {
         "CommandLineFlags": [],
         "Environment": [
           "TS_AUTH_KEY_PATH=/perm/tailscale/authkey",
           "TS_HOSTNAME=photo-backup",
           "TS_TAILSCALE_UP_ARGS=--ssh --accept-dns=false"
         ]
       },
       "github.com/denysvitali/pictures-sync-s3/cmd/webui": {
         "Environment": [
           "PORT=8080"
         ]
       },
       "github.com/denysvitali/pictures-sync-s3/cmd/wifi-init": {
         "Environment": [
           "WIFI_COUNTRY=US"
         ]
       },
       "github.com/denysvitali/pictures-sync-s3/cmd/provision-ap": {
         "Environment": [
           "HOSTAPD_PATH=/usr/bin/hostapd",
           "WIFI_COUNTRY=US"
         ],
         "ExtraFilePaths": {
           "/usr/bin/hostapd": "/path/to/target-arm64/hostapd"
         }
       }
     }
   }
   ```

4. Get a Tailscale auth key:
   - Visit https://login.tailscale.com/admin/settings/keys
   - Generate a new auth key (disable key expiry for persistent access)
   - Copy it to `/perm/tailscale/authkey` before first boot or during provisioning.

4. Build and write to SD card:
   ```bash
   # Insert SD card and identify device (e.g., /dev/sdb)
   # These binaries must point to target-compatible executables.
   export HOSTAPD_BINARY=/path/to/hostapd
   export EXFAT_MKFS_BINARY=/path/to/mkfs.exfat
   gok -i <instance-name> overwrite --full /dev/sdX
   ```

   The `gok` tool will use the `go.mod` with the replace directive from `~/gokrazy/<instance-name>/` to build your local code.

5. Boot the Raspberry Pi with the new SD card

### Emergency SSH Access

The generated Gokrazy configuration includes `github.com/gokrazy/breakglass`
and `github.com/gokrazy/serial-busybox` for emergency/debugging access.
Breakglass reads authorized keys from `/perm/breakglass/authorized_keys`, so
one shared image can be provisioned with per-device keys after first boot.
Install the host-side wrapper with:

```bash
go install github.com/gokrazy/breakglass/cmd/breakglass@latest
```

Start breakglass from the Gokrazy web interface, then open a shell with:

```bash
breakglass <instance-name>
```

Breakglass is intended for emergency access and is not started automatically
on boot.

## Configuration

### First-Time Setup

1. **WiFi Configuration** (if not using Ethernet):
   - Ethernet uses Gokrazy's built-in DHCP client on boot. Wi-Fi startup is gated by `cmd/wifi-init`: it waits briefly for carrier on `eth0` and only starts `github.com/gokrazy/wifi` when Ethernet has no carrier.
   - On first boot with no configured networks, the device starts a provisioning hotspot:
     - SSID: `PhotoBackup-Setup`
     - Password: `photo-backup-setup`
     - Setup address: `http://192.168.44.1:8080`
   - Override hotspot defaults with env vars:
     - `SETUP_WIFI_SSID`
     - `SETUP_WIFI_PSK`
     - `WIFI_COUNTRY` (defaults to `US`; set this to your deployment country for 5 GHz regulatory support)
   - Or pre-seed `/perm/wifi.json` before first boot with your network. Gokrazy expects a single client network object:
     ```json
     {"ssid": "YourNetwork", "psk": "YourPassword"}
     ```

2. **Access the Web UI**:
   - Local network: `https://photo-backup.local:443` or `http://photo-backup.local:8080`
   - Via Tailscale: `https://photo-backup:443` (after Tailscale connects)
   - Accept the self-signed certificate warning
   - Hosted React UI: point your browser at the GitHub Pages build and set the device URL in the UI

4. **Hosted UI connection configuration**:
   - Set `WEBUI_ALLOWED_ORIGINS` in the device environment when exposing the on-device API to GitHub Pages.
   - Example:
     ```
     WEBUI_ALLOWED_ORIGINS=https://<your-org>.github.io
     ```
   - Optional port included (example for local dev): `https://<your-org>.github.io:8080`.
   - Leave empty to keep API accessible only from same-origin requests.

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

4. **Google Photos (optional)**:
   - You can sync photos from your cloud storage (B2/S3) directly to Google Photos albums
   - This is done via native OAuth — no rclone Google Photos remote needed
   - See [Google Photos OAuth Setup](#google-photos-oauth-setup) below

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

### Google Photos OAuth Setup

This feature lets you sync **all existing photos** from your cloud storage (B2/S3/etc.)
directly to Google Photos, organized into albums by card ID.

#### 1. Create a Google Cloud OAuth App

1. Go to [Google Cloud Console](https://console.cloud.google.com/) and create a project
2. Enable the **Google Photos Library API**:
   - APIs & Services > Library > Search "Photos Library API" > Enable
3. Create OAuth 2.0 credentials:
   - APIs & Services > Credentials > Create Credentials > **OAuth client ID**
   - Application type: **Web application**
   - Name: `Photo Backup Station`
   - Authorized redirect URIs: add your device's callback URL(s)
     - Local access: `http://photo-backup.local:8080/api/googlephotos/auth/callback`
     - Tailscale access: `https://photo-backup:443/api/googlephotos/auth/callback`
     - Add any other IPs/hostnames you use to reach the device
   - Click **Create**
   - Copy the **Client ID** and **Client Secret**

#### 2. Configure the Device

1. Open the Web UI and go to **Settings** > **Sync Settings**
2. Enable **"Google Photos native OAuth"**
3. Paste your **Client ID** and **Client Secret**
4. Click **Save Settings**

#### 3. Connect Your Account

1. Go to the **Google Photos** tab in the Web UI
2. Click **Connect Google Photos**
3. A popup will open asking you to authorize the app
4. Grant permission — the popup will close automatically
5. Your account is now connected

#### 4. Run a Sync

1. Click **Sync to Google Photos**
2. The device will:
   - List all card folders from your B2/S3 storage
   - Create a Google Photos album for each card (e.g. "Card abc123")
   - Download each photo/video from B2, upload to Google Photos
   - Add each photo to its card's album
   - Skip RAW files automatically (.CR2, .NEF, .ARW, etc.)
3. Monitor real-time progress on the same page

**Note**: Only JPG, PNG, HEIC, MP4, MOV, and other common photo/video formats
are uploaded. RAW files are filtered out.

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

/perm/wifi.json          # Active WiFi client profile (managed by gokrazy/wifi)
/perm/extra-wifi.json    # Web UI list of saved WiFi networks
/perm/tailscale/authkey  # Tailscale auth key used by tailscale-init
```

**Note**: The `.pictures-sync-id` file is written to the root of each SD card and persists across insertions. If the card is reformatted, this file is lost and a new ID is generated.

### Settings Persistence

All configuration changes made through the web UI are automatically persisted:

- **Remote Name & Path**: Saved to `/perm/pictures-sync/settings.json`
- **Rclone Configuration**: Saved to `/perm/pictures-sync/rclone.conf`
- **WiFi Networks**: Saved to `/perm/extra-wifi.json`; the active network is mirrored to `/perm/wifi.json` for `github.com/gokrazy/wifi`

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

GitHub Actions builds a flashable Gokrazy image on every push to `master`, producing `photo-backup-rpi4b.img` as a workflow artifact. The same workflow also runs for version tags (`v*`) and publishes the flash image to GitHub Releases as a compressed `photo-backup-rpi4b.img.gz` asset to stay within the release asset size limit.

The workflow also publishes `photo-backup-rpi4b-root.squashfs.gz`, which is the gokrazy-compatible OTA root image used by the web UI updater. The updater checks GitHub Releases by publish time, downloads the newest matching root image, streams it to the inactive gokrazy root partition, switches partitions, and requests a reboot.

Workflow:
`.github/workflows/ota-image.yml`

To flash a SD card for Raspberry Pi 4:

1. Download `photo-backup-rpi4b.img.gz` from the latest successful `master` workflow run or release asset.
2. Decompress it before flashing:
   ```bash
   gunzip photo-backup-rpi4b.img.gz
   ```
3. Insert the target SD card and identify it (for example `/dev/sdb`), then unmount any mounted partitions before flashing.
4. Unmount any mounted partitions for the card (for example `/dev/sdb1`, `/dev/sdb2`).
5. Flash and flush the image:
   ```bash
   sudo dd if=photo-backup-rpi4b.img of=/dev/sdX bs=4M status=progress conv=fsync
   ```
   Replace `/dev/sdX` with your actual SD card device.
6. Remove the card safely:
   ```bash
   sync
   ```
7. Verify the write completed:
   ```bash
   lsblk /dev/sdX
   ```
8. Remove the card safely and insert into Raspberry Pi 4:
   ```bash
   sync
   ```
9. Boot the device.

Notes:
- This image is a full `overwrite --full` Gokrazy image intended for initial provisioning on SD media.
- Use the web UI configuration page for in-place OTA updates. The full `photo-backup-rpi4b.img.gz` asset is not written onto a running device.
- The initial Gokrazy/web UI credentials are `gokrazy` / `photo-backup`; change the password after first boot.
- Double-check the device path before running `dd` (it will destroy the selected disk).

Run the same locally with:
```bash
GOKRAZY_IMAGE_MODE=full TARGET_STORAGE_BYTES=2147483648 make ota
```

`TARGET_STORAGE_BYTES` controls the size of the flash image when building a full SD-card image. Adjust it to match the target media size you want to provision.

Or, if you only need the OTA squashfs image:
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

   To install an already-built OTA root image:
   ```bash
   go run ./cmd/ota-upload \
     -target http://gokrazy:<password>@<device-ip>/ \
     -image photo-backup-rpi4b-root.squashfs.gz
   ```
   Use `-insecure` when connecting to a self-signed HTTPS gokrazy endpoint by IP address.
   The web UI updater uses the same client behavior; set `OTA_GOKRAZY_INSECURE=true`
   with `OTA_GOKRAZY_UPDATE_URL=https://...` for self-signed non-loopback updater endpoints.

   The `replace` directive in `~/gokrazy/<instance-name>/go.mod` ensures your local changes are always used.
   For local OTA image builds, `scripts/build-ota-image.sh` auto-detects a sibling `../gokrazy` checkout; otherwise set `GOKRAZY_MODULE_REPLACE=/path/to/gokrazy` so the image uses the runtime fork that honors persistent TLS certificates.

## Security Considerations

- The web UI uses self-signed TLS by default
- Rclone configuration contains sensitive credentials - stored in `/perm` (persistent partition)
- WiFi passwords stored in `/perm/wifi.json` with 0600 permissions
- Tailscale provides secure remote access without exposing ports
- Breakglass provides emergency SSH/SCP access when explicitly started from the Gokrazy web interface
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
