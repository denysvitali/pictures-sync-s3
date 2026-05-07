#!/usr/bin/env bash

set -euo pipefail

GOKRAZY_INSTANCE="${GOKRAZY_INSTANCE:-photo-backup}"
GOKRAZY_PARENT_DIR="${GOKRAZY_PARENT_DIR:-$HOME/.gokrazy/$GOKRAZY_INSTANCE}"
GOKRAZY_MODULE_REPLACE="${GOKRAZY_MODULE_REPLACE:-}"
IMAGE_DIR="${IMAGE_DIR:-$PWD/ota}"
IMAGE_NAME="${IMAGE_NAME:-photo-backup-ota.squashfs}"
GOKRAZY_IMAGE_MODE="${GOKRAZY_IMAGE_MODE:-ota}"
TARGET_STORAGE_BYTES="${TARGET_STORAGE_BYTES:-}"
IMAGE_PATH="${IMAGE_DIR}/${IMAGE_NAME}"
HOSTAPD_BINARY="${HOSTAPD_BINARY:-}"
EXFAT_MKFS_BINARY="${EXFAT_MKFS_BINARY:-}"
BUILD_DATE="${BUILD_DATE:-$(date -u '+%Y-%m-%dT%H:%M:%SZ')}"
REPO_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
INSTANCE_DIR="$GOKRAZY_PARENT_DIR/$GOKRAZY_INSTANCE"

export GOKRAZY_PARENT_DIR

if [ -z "$GOKRAZY_MODULE_REPLACE" ] && [ -f "$REPO_DIR/../gokrazy/go.mod" ]; then
  GOKRAZY_MODULE_REPLACE="$REPO_DIR/../gokrazy"
fi

if [ -n "${VERSION:-}" ]; then
  BUILD_VERSION="$VERSION"
elif [ -n "${TAG_NAME:-}" ]; then
  BUILD_VERSION="$TAG_NAME"
elif [ "${GITHUB_REF:-}" = "refs/heads/master" ] && [ -n "${GITHUB_SHA:-}" ]; then
  BUILD_VERSION="master-${GITHUB_SHA}"
elif [[ "${GITHUB_REF:-}" == refs/tags/* ]] && [ -n "${GITHUB_REF_NAME:-}" ]; then
  BUILD_VERSION="$GITHUB_REF_NAME"
else
  BUILD_VERSION="$(git describe --tags --always --dirty 2>/dev/null || echo dev)"
fi

VERSION_LDFLAGS="-s -w -X github.com/denysvitali/pictures-sync-s3/pkg/version.Version=${BUILD_VERSION} -X github.com/denysvitali/pictures-sync-s3/pkg/version.BuildDate=${BUILD_DATE}"

if [ -z "$HOSTAPD_BINARY" ]; then
  HOSTAPD_BINARY="$(command -v hostapd || true)"
fi
if [ -z "$HOSTAPD_BINARY" ] || [ ! -x "$HOSTAPD_BINARY" ]; then
  echo "Error: HOSTAPD_BINARY must point to an executable hostapd binary for the target architecture"
  exit 1
fi

if [ -z "$EXFAT_MKFS_BINARY" ]; then
  EXFAT_MKFS_BINARY="$(command -v mkfs.exfat || true)"
fi
if [ -z "$EXFAT_MKFS_BINARY" ] || [ ! -x "$EXFAT_MKFS_BINARY" ]; then
  echo "Error: EXFAT_MKFS_BINARY must point to an executable mkfs.exfat binary for the target architecture"
  exit 1
fi

if [ -n "$GOKRAZY_MODULE_REPLACE" ]; then
  if [ ! -f "$GOKRAZY_MODULE_REPLACE/go.mod" ]; then
    echo "Error: GOKRAZY_MODULE_REPLACE must point to a github.com/gokrazy/gokrazy checkout"
    exit 1
  fi
  GOKRAZY_MODULE_REPLACE="$(cd "$GOKRAZY_MODULE_REPLACE" && pwd -P)"
fi

mkdir -p "$GOKRAZY_PARENT_DIR"
mkdir -p "$IMAGE_DIR"

if [ -d "$INSTANCE_DIR" ]; then
  rm -rf "$INSTANCE_DIR"
fi

gok -i "$GOKRAZY_INSTANCE" new

cat > "$INSTANCE_DIR/go.mod" <<EOF
module gokrazy-instance

go 1.26

replace github.com/denysvitali/pictures-sync-s3 => $REPO_DIR
EOF

if [ -n "$GOKRAZY_MODULE_REPLACE" ]; then
  cat >> "$INSTANCE_DIR/go.mod" <<EOF
replace github.com/gokrazy/gokrazy => $GOKRAZY_MODULE_REPLACE
EOF
fi

gok -i "$GOKRAZY_INSTANCE" add github.com/gokrazy/fbstatus
gok -i "$GOKRAZY_INSTANCE" add github.com/gokrazy/wifi
gok -i "$GOKRAZY_INSTANCE" add github.com/gokrazy/serial-busybox
gok -i "$GOKRAZY_INSTANCE" add github.com/gokrazy/breakglass
gok -i "$GOKRAZY_INSTANCE" add tailscale.com/cmd/tailscaled
gok -i "$GOKRAZY_INSTANCE" add tailscale.com/cmd/tailscale
gok -i "$GOKRAZY_INSTANCE" add ./cmd/wifi-init
gok -i "$GOKRAZY_INSTANCE" add ./cmd/pictures-sync
gok -i "$GOKRAZY_INSTANCE" add ./cmd/webui
gok -i "$GOKRAZY_INSTANCE" add ./cmd/provision-ap

cat > "$INSTANCE_DIR/config.json" <<EOF
{
  "Hostname": "$GOKRAZY_INSTANCE",
  "Update": {
    "HTTPPort": "80",
    "HTTPSPort": "443",
    "UseTLS": "self-signed",
    "TLSCertificateStorage": "perm-self-signed",
    "HTTPPassword": "photo-backup"
  },
  "Packages": [
    "github.com/gokrazy/fbstatus",
    "github.com/gokrazy/wifi",
    "github.com/gokrazy/serial-busybox",
    "github.com/gokrazy/breakglass",
    "tailscale.com/cmd/tailscaled",
    "tailscale.com/cmd/tailscale",
    "github.com/denysvitali/pictures-sync-s3/cmd/wifi-init",
    "github.com/denysvitali/pictures-sync-s3/cmd/pictures-sync",
    "github.com/denysvitali/pictures-sync-s3/cmd/webui",
    "github.com/denysvitali/pictures-sync-s3/cmd/provision-ap"
  ],
  "PackageConfig": {
    "github.com/denysvitali/pictures-sync-s3/cmd/pictures-sync": {
      "GoBuildFlags": [
        "-trimpath",
        "-ldflags=${VERSION_LDFLAGS}"
      ],
      "ExtraFilePaths": {
        "/usr/bin/mkfs.exfat": "$EXFAT_MKFS_BINARY"
      }
    },
    "github.com/denysvitali/pictures-sync-s3/cmd/webui": {
      "GoBuildFlags": [
        "-trimpath",
        "-ldflags=${VERSION_LDFLAGS}"
      ],
      "Environment": [
        "PORT=8080"
      ]
    },
    "github.com/denysvitali/pictures-sync-s3/cmd/wifi-init": {
      "GoBuildFlags": [
        "-trimpath",
        "-ldflags=${VERSION_LDFLAGS}"
      ]
    },
    "github.com/gokrazy/breakglass": {
      "CommandLineFlags": [
        "-authorized_keys=/perm/breakglass/authorized_keys"
      ]
    },
    "github.com/denysvitali/pictures-sync-s3/cmd/provision-ap": {
      "GoBuildFlags": [
        "-trimpath",
        "-ldflags=${VERSION_LDFLAGS}"
      ],
      "Environment": [
        "HOSTAPD_PATH=/usr/bin/hostapd"
      ],
      "ExtraFilePaths": {
        "/usr/bin/hostapd": "$HOSTAPD_BINARY"
      }
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
    gok -i "$GOKRAZY_INSTANCE" overwrite --full="$IMAGE_PATH" --target_storage_bytes="$TARGET_STORAGE_BYTES"
    if [ ! -s "$IMAGE_PATH" ]; then
      echo "Error: expected full image at $IMAGE_PATH, but the file was not created"
      exit 1
    fi
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
