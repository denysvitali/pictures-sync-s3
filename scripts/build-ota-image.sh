#!/usr/bin/env bash

set -euo pipefail

GOKRAZY_INSTANCE="${GOKRAZY_INSTANCE:-photo-backup}"
GOKRAZY_PARENT_DIR="${GOKRAZY_PARENT_DIR:-$HOME/.gokrazy/$GOKRAZY_INSTANCE}"
IMAGE_DIR="${IMAGE_DIR:-$PWD/ota}"
IMAGE_NAME="${IMAGE_NAME:-photo-backup-ota.squashfs}"
GOKRAZY_IMAGE_MODE="${GOKRAZY_IMAGE_MODE:-ota}"
TARGET_STORAGE_BYTES="${TARGET_STORAGE_BYTES:-}"
IMAGE_PATH="${IMAGE_DIR}/${IMAGE_NAME}"

mkdir -p "$GOKRAZY_PARENT_DIR"
mkdir -p "$IMAGE_DIR"

if [ -d "$GOKRAZY_PARENT_DIR/$GOKRAZY_INSTANCE" ]; then
  rm -rf "$GOKRAZY_PARENT_DIR/$GOKRAZY_INSTANCE"
fi

gok -i "$GOKRAZY_INSTANCE" new
gok -i "$GOKRAZY_INSTANCE" add github.com/gokrazy/fbstatus
gok -i "$GOKRAZY_INSTANCE" add github.com/gokrazy/mkfs
gok -i "$GOKRAZY_INSTANCE" add github.com/gokrazy/wifi
gok -i "$GOKRAZY_INSTANCE" add tailscale.com/cmd/tailscaled
gok -i "$GOKRAZY_INSTANCE" add tailscale.com/cmd/tailscale
gok -i "$GOKRAZY_INSTANCE" add ./cmd/pictures-sync
gok -i "$GOKRAZY_INSTANCE" add ./cmd/webui

cat > "$GOKRAZY_PARENT_DIR/$GOKRAZY_INSTANCE/config.json" <<EOF
{
  "Hostname": "$GOKRAZY_INSTANCE",
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
    "github.com/denysvitali/pictures-sync-s3/cmd/webui": {
      "Environment": [
        "PORT=8080"
      ]
    }
  }
}
EOF

case "$GOKRAZY_IMAGE_MODE" in
  ota)
    gok -i "$GOKRAZY_INSTANCE" overwrite --root "$IMAGE_PATH"
    ;;
  full)
    if [ -z "$TARGET_STORAGE_BYTES" ]; then
      echo "Error: TARGET_STORAGE_BYTES is required when GOKRAZY_IMAGE_MODE=full"
      exit 1
    fi
    gok -i "$GOKRAZY_INSTANCE" overwrite --full --target_storage_bytes "$TARGET_STORAGE_BYTES" "$IMAGE_PATH"
    ;;
  *)
    echo "Error: invalid GOKRAZY_IMAGE_MODE '$GOKRAZY_IMAGE_MODE' (expected 'ota' or 'full')"
    exit 1
    ;;
esac

if [ -n "${GITHUB_OUTPUT:-}" ]; then
  echo "image_path=$IMAGE_PATH" >> "$GITHUB_OUTPUT"
fi

echo "Built image: $IMAGE_PATH"
