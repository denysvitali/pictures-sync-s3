#!/bin/bash

# Gokrazy Setup Script for Photo Backup Station
# This script helps set up a new Gokrazy instance

set -e

echo "=== Photo Backup Station - Gokrazy Setup ==="
echo ""

# Check if gok is installed
if ! command -v gok &> /dev/null; then
    echo "Error: gok command not found!"
    echo "Please install it with: go install github.com/gokrazy/tools/cmd/gok@main"
    exit 1
fi

# Get user input
read -p "Enter instance name (default: photo-backup): " INSTANCE_NAME
INSTANCE_NAME=${INSTANCE_NAME:-photo-backup}

read -p "Enter Tailscale auth key (or press Enter to skip): " TAILSCALE_KEY

read -p "Enter WiFi SSID (or press Enter to skip): " WIFI_SSID
if [ -n "$WIFI_SSID" ]; then
    read -sp "Enter WiFi password: " WIFI_PASSWORD
    echo ""
fi

# Note: Remote name and path are now configured via web UI and persisted to /perm/pictures-sync/settings.json
# No need to set environment variables for these

echo ""
echo "Setting up Gokrazy instance: $INSTANCE_NAME"
echo ""

# Initialize instance if it doesn't exist
if [ ! -d "$HOME/gokrazy/$INSTANCE_NAME" ]; then
    echo "Creating new instance..."
    gok -i "$INSTANCE_NAME" new
fi

# Add required packages
echo "Adding packages..."
gok -i "$INSTANCE_NAME" add github.com/gokrazy/fbstatus
gok -i "$INSTANCE_NAME" add github.com/gokrazy/mkfs
gok -i "$INSTANCE_NAME" add github.com/gokrazy/wifi
gok -i "$INSTANCE_NAME" add tailscale.com/cmd/tailscaled
gok -i "$INSTANCE_NAME" add tailscale.com/cmd/tailscale
gok -i "$INSTANCE_NAME" add github.com/yourusername/pictures-sync-s3/cmd/pictures-sync
gok -i "$INSTANCE_NAME" add github.com/yourusername/pictures-sync-s3/cmd/webui

echo "Packages added successfully!"
echo ""

# Create config.json
CONFIG_FILE="$HOME/gokrazy/$INSTANCE_NAME/config.json"
echo "Creating configuration at: $CONFIG_FILE"

cat > "$CONFIG_FILE" <<EOF
{
  "Hostname": "$INSTANCE_NAME",
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
    "github.com/yourusername/pictures-sync-s3/cmd/pictures-sync",
    "github.com/yourusername/pictures-sync-s3/cmd/webui"
  ],
  "PackageConfig": {
EOF

if [ -n "$TAILSCALE_KEY" ]; then
cat >> "$CONFIG_FILE" <<EOF
    "tailscale.com/cmd/tailscale": {
      "CommandLineFlags": [
        "up",
        "--auth-key=$TAILSCALE_KEY",
        "--hostname=$INSTANCE_NAME",
        "--ssh"
      ]
    },
EOF
fi

cat >> "$CONFIG_FILE" <<EOF
    "github.com/yourusername/pictures-sync-s3/cmd/webui": {
      "Environment": {
        "PORT": "8080"
      }
    }
  }
}
EOF

echo "Configuration created!"
echo ""

# Create WiFi config if provided
if [ -n "$WIFI_SSID" ]; then
    WIFI_FILE="$HOME/gokrazy/$INSTANCE_NAME/wifi.json"
    echo "Creating WiFi configuration at: $WIFI_FILE"
    cat > "$WIFI_FILE" <<EOF
[
  {
    "ssid": "$WIFI_SSID",
    "psk": "$WIFI_PASSWORD"
  }
]
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
echo "Configuration file: $CONFIG_FILE"
if [ -n "$WIFI_SSID" ]; then
    echo "WiFi config: $WIFI_FILE (copy to device /perm/wifi.json)"
fi
echo ""
echo "All runtime settings (remote name/path) are configured via web UI and persist automatically."
echo "For more information, see README.md"
