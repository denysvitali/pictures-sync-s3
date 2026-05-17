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
MKE2FS_BINARY="${MKE2FS_BINARY:-}"
BUILD_DATE="${BUILD_DATE:-$(date -u '+%Y-%m-%dT%H:%M:%SZ')}"
REPO_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
INSTANCE_DIR="$GOKRAZY_PARENT_DIR/$GOKRAZY_INSTANCE"

# Source shared package list / helpers.
# shellcheck source=gok-common.sh
source "$REPO_DIR/scripts/gok-common.sh"

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

if [ -z "$MKE2FS_BINARY" ]; then
  MKE2FS_BINARY="$(command -v mke2fs || true)"
fi
if [ -z "$MKE2FS_BINARY" ] || [ ! -x "$MKE2FS_BINARY" ]; then
  echo "Error: MKE2FS_BINARY must point to an executable mke2fs binary for the target architecture"
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

if ! command -v pnpm >/dev/null 2>&1; then
  echo "Error: pnpm is required to build embedded webui assets"
  exit 1
fi

echo "Building embedded WebUI bundle..."
(cd "$REPO_DIR/webui" && pnpm install --frozen-lockfile)
(cd "$REPO_DIR/webui" && pnpm build)
if [ ! -f "$REPO_DIR/pkg/webui/dist/index.html" ]; then
  echo "Error: pkg/webui/dist/index.html is missing after webui build"
  exit 1
fi

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

for pkg in "${gok_packages[@]}"; do
  gok -i "$GOKRAZY_INSTANCE" add "$pkg"
done

cat > "$INSTANCE_DIR/config.json" <<EOF
{
  "Hostname": "$GOKRAZY_INSTANCE",
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
    "github.com/denysvitali/pictures-sync-s3/cmd/pictures-sync": {
      "GoBuildFlags": [
        "-trimpath",
        "-ldflags=${VERSION_LDFLAGS}"
      ],
      "ExtraFilePaths": {
        "/usr/bin/mkfs.exfat": "$EXFAT_MKFS_BINARY"
      }
    },
    "github.com/denysvitali/pictures-sync-s3/cmd/perm-init": {
      "GoBuildFlags": [
        "-trimpath",
        "-ldflags=${VERSION_LDFLAGS}"
      ],
      "ExtraFilePaths": {
        "/usr/local/bin/mke2fs": "$MKE2FS_BINARY"
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
      ],
      "Environment": [
        "WIFI_COUNTRY=${WIFI_COUNTRY:-US}"
      ]
    },
    "github.com/denysvitali/pictures-sync-s3/cmd/tailscale-init": {
      "GoBuildFlags": [
        "-trimpath",
        "-ldflags=${VERSION_LDFLAGS}"
      ],
      "Environment": [
        "TS_AUTH_KEY_PATH=/perm/tailscale/authkey",
        "TS_HOSTNAME=${GOKRAZY_INSTANCE}",
        "TS_TAILSCALE_UP_ARGS=--ssh --accept-dns=false"
      ]
    },
    "tailscale.com/cmd/tailscale": {
      "DontStart": true
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
