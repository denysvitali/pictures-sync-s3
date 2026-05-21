#!/bin/bash

# Gokrazy Setup Script for Photo Backup Station
# This script helps set up a new Gokrazy instance

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Source shared package list / helpers (defines gok_packages, gok_config_packages, emit_packages_json).
# shellcheck source=scripts/gok-common.sh
source "$SCRIPT_DIR/scripts/gok-common.sh"

echo "=== Photo Backup Station - Gokrazy Setup ==="
echo ""

# Check if gok is installed
if ! command -v gok &> /dev/null; then
    echo "Error: gok command not found!"
    echo "Please install the forked gok CLI with the commands in README.md"
    exit 1
fi

# Get user input
read -p "Enter instance name (default: photo-backup): " INSTANCE_NAME
INSTANCE_NAME=${INSTANCE_NAME:-photo-backup}

read -p "Enter Tailscale auth key file path (default: /perm/tailscale/authkey): " TAILSCALE_AUTHKEY_PATH
TAILSCALE_AUTHKEY_PATH=${TAILSCALE_AUTHKEY_PATH:-/perm/tailscale/authkey}

read -p "Enter WiFi SSID (or press Enter to skip): " WIFI_SSID
if [ -n "$WIFI_SSID" ]; then
    read -sp "Enter WiFi password: " WIFI_PASSWORD
    echo ""
fi

HOSTAPD_BINARY=${HOSTAPD_BINARY:-$(command -v hostapd || true)}
if [ -z "$HOSTAPD_BINARY" ] || [ ! -x "$HOSTAPD_BINARY" ]; then
    echo "Error: HOSTAPD_BINARY must point to an executable hostapd binary for the target architecture"
    exit 1
fi

EXFAT_MKFS_BINARY=${EXFAT_MKFS_BINARY:-$(command -v mkfs.exfat || true)}
if [ -z "$EXFAT_MKFS_BINARY" ] || [ ! -x "$EXFAT_MKFS_BINARY" ]; then
    echo "Error: EXFAT_MKFS_BINARY must point to an executable mkfs.exfat binary for the target architecture"
    exit 1
fi

MKE2FS_BINARY=${MKE2FS_BINARY:-$(command -v mke2fs || true)}
if [ -z "$MKE2FS_BINARY" ] || [ ! -x "$MKE2FS_BINARY" ]; then
    echo "Error: MKE2FS_BINARY must point to an executable mke2fs binary for the target architecture"
    exit 1
fi

GOKRAZY_MODULE_REPLACE=${GOKRAZY_MODULE_REPLACE:-}
if [ -z "$GOKRAZY_MODULE_REPLACE" ] && [ -f "$SCRIPT_DIR/../gokrazy/go.mod" ]; then
    GOKRAZY_MODULE_REPLACE="$SCRIPT_DIR/../gokrazy"
fi
if [ -n "$GOKRAZY_MODULE_REPLACE" ]; then
    if [ ! -f "$GOKRAZY_MODULE_REPLACE/go.mod" ]; then
        echo "Error: GOKRAZY_MODULE_REPLACE must point to a github.com/gokrazy/gokrazy checkout"
        exit 1
    fi
    GOKRAZY_MODULE_REPLACE="$(cd "$GOKRAZY_MODULE_REPLACE" && pwd -P)"
fi

# Note: Remote name and path are now configured via web UI and persisted to /perm/pictures-sync/settings.json
# No need to set environment variables for these

echo ""
echo "Setting up Gokrazy instance: $INSTANCE_NAME"
echo ""

INSTANCE_DIR="$HOME/gokrazy/$INSTANCE_NAME"

# Initialize instance if it doesn't exist
if [ ! -d "$INSTANCE_DIR" ]; then
    echo "Creating new instance..."
    gok -i "$INSTANCE_NAME" new
fi

# Create go.mod with replace directive for private module
# Use absolute path to ensure it works regardless of where gok is run from
ABSOLUTE_PROJECT_PATH="$(cd "$SCRIPT_DIR" && pwd)"
echo "Creating go.mod with replace directive..."
cat > "$INSTANCE_DIR/go.mod" <<EOF
module gokrazy-instance

go 1.26

replace github.com/denysvitali/pictures-sync-s3 => $ABSOLUTE_PROJECT_PATH
EOF

echo "go.mod created with replace directive pointing to: $ABSOLUTE_PROJECT_PATH"
if [ -n "$GOKRAZY_MODULE_REPLACE" ]; then
    cat >> "$INSTANCE_DIR/go.mod" <<EOF
replace github.com/gokrazy/gokrazy => $GOKRAZY_MODULE_REPLACE
EOF
    echo "go.mod created with gokrazy runtime replace directive pointing to: $GOKRAZY_MODULE_REPLACE"
else
    echo "Warning: GOKRAZY_MODULE_REPLACE is not set; persistent TLS certificate support requires a gokrazy runtime fork with TLS storage markers"
fi
echo ""

# Add packages from the shared list (scripts/gok-packages.txt).
echo "Adding packages..."
for pkg in "${gok_packages[@]}"; do
    gok -i "$INSTANCE_NAME" add "$pkg"
done

echo "Public packages added successfully!"
echo "Note: Private packages will be added via config.json"
echo ""

# Create config.json
CONFIG_FILE="$INSTANCE_DIR/config.json"
echo "Creating configuration at: $CONFIG_FILE"

cat > "$CONFIG_FILE" <<EOF
{
  "Hostname": "$INSTANCE_NAME",
  "Update": {
    "HTTPPort": "80",
    "HTTPSPort": "443",
    "HTTPPassword": "photo-backup",
    "UseTLS": "self-signed",
    "TLSCertificateStorage": "perm-self-signed"
  },
  "Packages": [
$(emit_packages_json '    ')
  ],
  "PackageConfig": {
    "github.com/gokrazy/gokrazy/cmd/dhcp": {
      "DontStart": true
    },
    "github.com/gokrazy/wifi": {
      "DontStart": true
    },
    "github.com/gokrazy/breakglass": {
      "CommandLineFlags": [
        "-authorized_keys=/perm/breakglass/authorized_keys"
      ]
    },
EOF

cat >> "$CONFIG_FILE" <<EOF
    "github.com/denysvitali/pictures-sync-s3/cmd/pictures-sync": {
      "ExtraFilePaths": {
        "/usr/bin/mkfs.exfat": "$EXFAT_MKFS_BINARY"
      }
    },
    "github.com/denysvitali/pictures-sync-s3/cmd/perm-init": {
      "ExtraFilePaths": {
        "/usr/local/bin/mke2fs": "$MKE2FS_BINARY"
      }
    },
    "tailscale.com/cmd/tailscale": {
      "DontStart": true
    },
    "github.com/denysvitali/pictures-sync-s3/cmd/tailscale-init": {
      "Environment": [
        "TS_AUTH_KEY_PATH=$TAILSCALE_AUTHKEY_PATH",
        "TS_HOSTNAME=$INSTANCE_NAME",
        "TS_TAILSCALE_UP_ARGS=--ssh --accept-dns=false"
      ]
    },
EOF

cat >> "$CONFIG_FILE" <<EOF
    "github.com/denysvitali/pictures-sync-s3/cmd/webui": {
      "Environment": [
        "PORT=8080"
      ]
    },
    "github.com/denysvitali/pictures-sync-s3/cmd/wifi-init": {
      "Environment": [
        "WIFI_COUNTRY=${WIFI_COUNTRY:-US}"
      ]
    },
    "github.com/denysvitali/pictures-sync-s3/cmd/provision-ap": {
      "Environment": [
        "HOSTAPD_PATH=/usr/bin/hostapd",
        "WIFI_COUNTRY=${WIFI_COUNTRY:-US}"
      ],
      "ExtraFilePaths": {
        "/usr/bin/hostapd": "$HOSTAPD_BINARY"
      }
    }
  }
}
EOF

echo "Configuration created!"
echo ""

# Create WiFi config if provided
if [ -n "$WIFI_SSID" ]; then
    WIFI_FILE="$INSTANCE_DIR/wifi.json"
    echo "Creating WiFi configuration at: $WIFI_FILE"
    cat > "$WIFI_FILE" <<EOF
{
  "ssid": "$WIFI_SSID",
  "psk": "$WIFI_PASSWORD"
}
EOF
    echo "WiFi configuration created!"
    echo "Note: Copy this file to /perm/wifi.json on the device after first boot"
    echo ""
fi

echo "=== Setup Complete ==="
echo ""
echo "Next steps:"
echo "1. Insert an SD card into your computer"
echo "2. Identify the device (e.g., /dev/sdb) - BE CAREFUL!"
echo "3. Run: gok -i $INSTANCE_NAME overwrite --full /dev/sdX"
echo "4. Insert the SD card into your Raspberry Pi 4 and power on"
echo "5. Access the web UI at: https://$INSTANCE_NAME.local or http://$INSTANCE_NAME.local:8080"
echo "6. Configure rclone settings (remote name, path, and credentials) via the web UI"
echo ""
echo "Configuration files:"
echo "  - Config: $CONFIG_FILE"
echo "  - Go module: $INSTANCE_DIR/go.mod (with replace directive for local code)"
echo "  - hostapd: $HOSTAPD_BINARY"
echo "  - mkfs.exfat: $EXFAT_MKFS_BINARY"
echo "  - mke2fs: $MKE2FS_BINARY"
if [ -n "$WIFI_SSID" ]; then
    echo "  - WiFi: $WIFI_FILE (copy to device /perm/wifi.json)"
fi
echo ""
echo "All runtime settings (remote name/path) are configured via web UI and persist automatically."
echo "For more information, see README.md"
